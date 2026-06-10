package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ---------------------------------------------------------------------------
// Public types used by both the handler and the handler tests
// ---------------------------------------------------------------------------

// ExportPayload is the response shape for GET /api/v1/account/data-export.
// The handler writes this directly as the JSON body.
type ExportPayload = repository.UserExport

// ManifestEntry is the per-table entry in the export manifest.
type ManifestEntry = repository.ExportManifestEntry

// ClerkProfile is a re-export of repository.ClerkProfile for use by the handler layer.
type ClerkProfile = repository.ClerkProfile

// ---------------------------------------------------------------------------
// Handler interfaces (dependency injection for testability)
// ---------------------------------------------------------------------------

// exportRateLimiter checks and records data export requests for rate-limiting.
type exportRateLimiter interface {
	// CheckRecentExport returns (limited, retryAfterSecs, err).
	// When limited is true, retryAfterSecs is the number of seconds until the
	// 24-hour window expires (used for the Retry-After response header).
	CheckRecentExport(ctx context.Context, userID int64) (limited bool, retryAfterSecs int64, err error)
	// RecordExport writes a new dsr_access_log row after a successful export.
	RecordExport(ctx context.Context, userID int64) (exportID string, err error)
}

// dataGatherer gathers user-keyed personal data for the export.
// portableOnly=false → Art.15 access export; portableOnly=true → Art.20 portable subset.
type dataGatherer interface {
	GatherForUser(ctx context.Context, userID, accountID int64, portableOnly bool) (*ExportPayload, error)
}

// exportAccountLookup resolves the authenticated user's account ID.
// Satisfied by *repository.AccountRepository.
type exportAccountLookup interface {
	GetAccountIDByUserID(ctx context.Context, userID int64) (int64, bool, error)
}

// ---------------------------------------------------------------------------
// DataExportHandler
// ---------------------------------------------------------------------------

// DataExportHandler handles GET /api/v1/account/data-export.
//
// Art.15 — Right of Access: returns a synchronous JSON export of all personal
// data held about the authenticated user.
//
// Auth: MUST be mounted inside composeClerkAuth(clerkAuthMiddl, clerkUserResolver)
// — every request must carry a valid Clerk session JWT (Rule 9, CLAUDE.md).
//
// Rate-limit: one export per user per 24-hour window (Ray Q4 ruling).
// On a second request within the window: 429 Too Many Requests + Retry-After.
//
// IDOR posture: user ID is always read from context (set by ClerkUserResolver),
// never from the request body or query string.
type DataExportHandler struct {
	limiter  exportRateLimiter
	gatherer dataGatherer
	accounts exportAccountLookup
}

// NewDataExportHandler returns a DataExportHandler with its dependencies wired.
func NewDataExportHandler(limiter exportRateLimiter, gatherer dataGatherer, accounts exportAccountLookup) *DataExportHandler {
	return &DataExportHandler{
		limiter:  limiter,
		gatherer: gatherer,
		accounts: accounts,
	}
}

// Export handles GET /api/v1/account/data-export.
//
// Response codes:
//   - 200 OK — export JSON body with Content-Disposition: attachment
//   - 401 Unauthorized — no authenticated user ID on context
//   - 404 Not Found — user has no account row (daemon not yet paired)
//   - 429 Too Many Requests — second request within 24-hour window
//   - 500 Internal Server Error — database or gather failure
func (h *DataExportHandler) Export(w http.ResponseWriter, r *http.Request) {
	// Step 1: Resolve authenticated user ID.
	// auth.UserIDFromContext — never raw JWT (CLAUDE.md Rule 9).
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Step 2: Rate-limit check — 1 export per 24h per user (Ray Q4 ruling).
	limited, retryAfterSecs, err := h.limiter.CheckRecentExport(r.Context(), userID)
	if err != nil {
		log.Printf("[DataExportHandler] CheckRecentExport userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if limited {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfterSecs))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":       "data export rate limit exceeded",
			"retry_after": retryAfterSecs,
			"message":     "You may request one data export per 24-hour period.",
		})
		return
	}

	// Step 3: Resolve account_id. Every gather query scoped by account_id (Rule 2).
	accountID, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[DataExportHandler] GetAccountIDByUserID userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSONError(w, "account not found", http.StatusNotFound)
		return
	}

	// Step 4: Gather user-keyed personal data.
	// format=portable → Art.20 portability subset; any other value (or absent) → Art.15 access export.
	portableOnly := r.URL.Query().Get("format") == "portable"
	payload, err := h.gatherer.GatherForUser(r.Context(), userID, accountID, portableOnly)
	if err != nil {
		log.Printf("[DataExportHandler] GatherForUser userID=%d accountID=%d: %v", userID, accountID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Step 5: Record this export in dsr_access_log (opens the rate-limit window).
	// Non-fatal: if RecordExport fails we still return the export to the user —
	// a failed write should not deny the user their Art.15 right.  Log the error
	// for ops follow-up.
	if _, recErr := h.limiter.RecordExport(r.Context(), userID); recErr != nil {
		log.Printf("[DataExportHandler] RecordExport userID=%d: %v (non-fatal, export still served)", userID, recErr)
	}

	// Step 6: Write response.
	// Content-Disposition: attachment triggers browser file download (per plan §1).
	filename := fmt.Sprintf("vaultmtg-data-export-%s.json", payload.ExportedAt.Format(time.RFC3339))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("[DataExportHandler] encode userID=%d: %v", userID, err)
	}
}
