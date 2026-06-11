package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
)

const (
	// AccountDeletionRateLimitMax is the maximum number of DELETE /api/v1/account
	// calls allowed per authenticated user per AccountDeletionRateLimitWindow.
	// Exported so handler tests can reference it without hardcoding the limit.
	// Legitimate usage is one-shot; 5/10 min is generous defense-in-depth.
	AccountDeletionRateLimitMax = 5

	// accountDeletionRateLimitWindow is the sliding window for per-user rate
	// limiting on the account-deletion endpoint.
	accountDeletionRateLimitWindow = 10 * time.Minute
)

// deletionRateEntry tracks DELETE /api/v1/account call timestamps for one user.
type deletionRateEntry struct {
	mu        sync.Mutex
	callTimes []time.Time
}

// allow returns (true, 0) if the call is within the rate limit.
// Returns (false, retryAfterSeconds) when the limit is exhausted; retryAfterSeconds
// is the number of seconds until the oldest call ages out of the window.
func (e *deletionRateEntry) allow() (ok bool, retryAfter int) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-accountDeletionRateLimitWindow)

	// Prune stale timestamps.
	filtered := e.callTimes[:0]
	for _, t := range e.callTimes {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	e.callTimes = filtered

	if len(e.callTimes) >= AccountDeletionRateLimitMax {
		// Retry-After = seconds until the oldest call in the window expires.
		oldest := e.callTimes[0]
		retryAfter = int(oldest.Add(accountDeletionRateLimitWindow).Sub(now).Seconds()) + 1
		if retryAfter < 1 {
			retryAfter = 1
		}
		return false, retryAfter
	}
	e.callTimes = append(e.callTimes, now)
	return true, 0
}

// userAndAccountResolver resolves a Clerk user ID to internal database IDs.
type userAndAccountResolver interface {
	ResolveUserAndAccount(ctx context.Context, clerkUserID string) (userID, accountID int64, err error)
}

// erasureJobStarter creates the audit log entry and dispatches the background
// erasure goroutine.  Returns the job_id for the 202 response body.
type erasureJobStarter interface {
	StartErasureJob(ctx context.Context, userID, accountID int64) (jobID string, err error)
}

// AccountDeletionHandler handles DELETE /api/v1/account (Art.17 erasure entry
// point).
//
// The route MUST be mounted inside the ClerkAuthMiddleware group.  The handler
// returns 202 Accepted immediately; the erasure cascade runs asynchronously in
// a background goroutine dispatched from the BFF root context.
//
// This handler is the #887 entry point that invokes the #891 erasure cascade.
// Both must ship together per BROADCAST rule 5 (Coupled-With #887).
//
// Rate limit: AccountDeletionRateLimitMax requests per accountDeletionRateLimitWindow
// per authenticated Clerk user ID, enforced before any deletion logic (#1160).
type AccountDeletionHandler struct {
	resolver userAndAccountResolver
	starter  erasureJobStarter

	rateMu     sync.Mutex
	rateByUser map[string]*deletionRateEntry
}

// NewAccountDeletionHandler returns an AccountDeletionHandler.
func NewAccountDeletionHandler(resolver userAndAccountResolver, starter erasureJobStarter) *AccountDeletionHandler {
	return &AccountDeletionHandler{
		resolver:   resolver,
		starter:    starter,
		rateByUser: make(map[string]*deletionRateEntry),
	}
}

// rateAllow checks and records a rate-limit call for the given Clerk user ID.
func (h *AccountDeletionHandler) rateAllow(clerkUserID string) (ok bool, retryAfter int) {
	h.rateMu.Lock()
	entry, exists := h.rateByUser[clerkUserID]
	if !exists {
		entry = &deletionRateEntry{}
		h.rateByUser[clerkUserID] = entry
	}
	h.rateMu.Unlock()
	return entry.allow()
}

// accountDeletionResponse is the JSON body returned by DELETE /api/v1/account.
type accountDeletionResponse struct {
	JobID   string `json:"job_id"`
	Message string `json:"message"`
}

// Delete handles DELETE /api/v1/account.
//
// Returns 202 Accepted with a job_id that the client can poll via
// GET /api/v1/account/deletion-status/{job_id}.
//
// Guard order (AC3 from #1160 — rate-limit fires first):
//  1. Clerk user ID present (401 if missing)
//  2. Per-user rate-limit check (429 + Retry-After if exhausted)
//  3. Resolve user + account IDs (500 on DB error)
//  4. Start erasure job (500 on error)
//  5. Return 202 with job_id
func (h *AccountDeletionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	clerkUserID, ok := bffmiddleware.ClerkUserIDFromContext(r)
	if !ok || clerkUserID == "" {
		log.Printf("[account_deletion] missing Clerk user ID — ClerkAuthMiddleware not applied")
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// AC3 (#1160): rate-limit check fires before any deletion logic.
	if allowed, retryAfter := h.rateAllow(clerkUserID); !allowed {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
		writeJSONError(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	userID, accountID, err := h.resolver.ResolveUserAndAccount(r.Context(), clerkUserID)
	if err != nil {
		log.Printf("[account_deletion] ResolveUserAndAccount clerk_user_id=%s error=%v", clerkUserID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	jobID, err := h.starter.StartErasureJob(r.Context(), userID, accountID)
	if err != nil {
		log.Printf("[account_deletion] StartErasureJob user_id=%d account_id=%d error=%v", userID, accountID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(accountDeletionResponse{
		JobID:   jobID,
		Message: "Your account deletion request has been accepted and is being processed.",
	})
}
