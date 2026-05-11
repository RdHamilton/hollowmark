// Phase 2 PR #1 — /api/v1/matches handlers.
//
// Replaces the legacy daemonClient /matches surface with proper cloud-data
// endpoints under /api/v1/matches/*. All responses use camelCase JSON keys
// per the Phase 2 architecture lock-in
// (docs/product/milestones/v0.3.1/daemon-local-api-phase2-audit.md).
//
// Auth: every route is guarded by DaemonAPIKeyAuth (Bearer = daemon api_key
// from the OS keychain), which resolves to the int64 users.id on context.
// Match rows are scoped to that user's accounts.

package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	bffmiddleware "github.com/ramonehamilton/mtga-bff/internal/api/middleware"
	"github.com/ramonehamilton/mtga-bff/internal/storage/repository"
)

// matchesListReader is the minimal repo interface the handler needs.
// Defined here so tests can stub it without pulling in the SQL repo.
type matchesListReader interface {
	ListByAccountIDFiltered(ctx context.Context, accountID int64, filter repository.MatchFilter) ([]repository.MatchRow, int, error)
	GetByID(ctx context.Context, accountID int64, matchID string) (*repository.MatchRow, error)
	DistinctFormats(ctx context.Context, accountID int64) ([]string, error)
}

// MatchesHandler serves the cloud-data Phase 2 matches API. It depends on a
// matches list reader (for filtering + pagination + lookups) and an account
// lookup that resolves users.id → accounts.id.
type MatchesHandler struct {
	matches  matchesListReader
	accounts AccountLookup
}

// NewMatchesHandler returns a handler wired with the provided reader + account lookup.
func NewMatchesHandler(matches matchesListReader, accounts AccountLookup) *MatchesHandler {
	return &MatchesHandler{matches: matches, accounts: accounts}
}

// matchListItem is a single match in the list response. camelCase per the
// Phase 2 contract.
type matchListItem struct {
	ID              string    `json:"id"`
	Format          string    `json:"format"`
	Result          string    `json:"result"`
	Timestamp       time.Time `json:"timestamp"`
	DurationSeconds *int      `json:"durationSeconds,omitempty"`
	DeckID          *string   `json:"deckId,omitempty"`
	RankBefore      *string   `json:"rankBefore,omitempty"`
	RankAfter       *string   `json:"rankAfter,omitempty"`
	PlayerWins      int       `json:"playerWins"`
	OpponentWins    int       `json:"opponentWins"`
}

// matchListResponse wraps a page of matches.
type matchListResponse struct {
	Matches []matchListItem `json:"matches"`
	Total   int             `json:"total"`
	Page    int             `json:"page"`
	Limit   int             `json:"limit"`
}

// matchesListFilterRequest is the JSON body the SPA's getMatches() posts. All
// fields are optional; the handler treats missing fields as "no filter on that
// dimension". Mirrors StatsFilterRequest in frontend/src/services/api/matches.ts.
type matchesListFilterRequest struct {
	StartDate string   `json:"startDate,omitempty"`
	EndDate   string   `json:"endDate,omitempty"`
	Format    string   `json:"format,omitempty"`
	Formats   []string `json:"formats,omitempty"`
	DeckID    string   `json:"deckId,omitempty"`
	Result    string   `json:"result,omitempty"`
	Page      int      `json:"page,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

// List handles POST /api/v1/matches. Returns the paginated, filtered list of
// matches for the authenticated user. Uses POST (not GET) to match the SPA's
// existing call shape — bodies are easier than serialising filter[] params.
func (h *MatchesHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req matchesListFilterRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, "invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	filter, err := buildMatchFilter(req, page, limit)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	accountID, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[MatchesHandler.List] GetAccountIDByUserID userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		// No account row yet — return an empty page rather than 404.
		writeMatchesJSON(w, matchListResponse{Matches: []matchListItem{}, Total: 0, Page: page, Limit: limit})
		return
	}

	rows, total, err := h.matches.ListByAccountIDFiltered(r.Context(), accountID, filter)
	if err != nil {
		log.Printf("[MatchesHandler.List] ListByAccountIDFiltered accountID=%d: %v", accountID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	items := make([]matchListItem, 0, len(rows))
	for _, m := range rows {
		items = append(items, matchRowToListItem(m))
	}
	writeMatchesJSON(w, matchListResponse{Matches: items, Total: total, Page: page, Limit: limit})
}

// Get handles GET /api/v1/matches/{matchId}. Returns a single match scoped to
// the authenticated user. 404 when the match exists but belongs to another user
// (we don't leak that distinction; 404 covers both "not found" and "not yours").
func (h *MatchesHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	matchID := strings.TrimSpace(chi.URLParam(r, "matchId"))
	if matchID == "" {
		writeJSONError(w, "matchId is required", http.StatusBadRequest)
		return
	}

	accountID, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[MatchesHandler.Get] GetAccountIDByUserID userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSONError(w, "match not found", http.StatusNotFound)
		return
	}

	row, err := h.matches.GetByID(r.Context(), accountID, matchID)
	if err != nil {
		log.Printf("[MatchesHandler.Get] GetByID accountID=%d matchID=%s: %v", accountID, matchID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if row == nil {
		writeJSONError(w, "match not found", http.StatusNotFound)
		return
	}
	writeMatchesJSON(w, matchRowToListItem(*row))
}

// Formats handles GET /api/v1/matches/formats. Returns the distinct formats
// the user has match data for. Used by the SPA's format-filter dropdown.
func (h *MatchesHandler) Formats(w http.ResponseWriter, r *http.Request) {
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	accountID, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[MatchesHandler.Formats] GetAccountIDByUserID userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeMatchesJSON(w, []string{})
		return
	}
	formats, err := h.matches.DistinctFormats(r.Context(), accountID)
	if err != nil {
		log.Printf("[MatchesHandler.Formats] DistinctFormats accountID=%d: %v", accountID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if formats == nil {
		formats = []string{}
	}
	writeMatchesJSON(w, formats)
}

// buildMatchFilter validates the incoming request and shapes it into the repo
// filter struct. Validation errors get propagated to the handler as 400s.
func buildMatchFilter(req matchesListFilterRequest, page, limit int) (repository.MatchFilter, error) {
	f := repository.MatchFilter{
		Page:    page,
		Limit:   limit,
		Format:  strings.TrimSpace(req.Format),
		Formats: dedupeNonEmpty(req.Formats),
		DeckID:  strings.TrimSpace(req.DeckID),
		Result:  strings.TrimSpace(req.Result),
	}
	if req.StartDate != "" {
		t, err := parseFilterDate(req.StartDate)
		if err != nil {
			return f, err
		}
		f.StartDate = &t
	}
	if req.EndDate != "" {
		t, err := parseFilterDate(req.EndDate)
		if err != nil {
			return f, err
		}
		f.EndDate = &t
	}
	if f.Result != "" {
		switch strings.ToLower(f.Result) {
		case "win", "loss", "draw":
		default:
			return f, &fieldError{"result must be win|loss|draw"}
		}
	}
	return f, nil
}

// parseFilterDate accepts either an RFC3339 timestamp or a YYYY-MM-DD date.
// The SPA's matches.ts formatDateParam helper emits YYYY-MM-DD; older callers
// may send full ISO strings.
func parseFilterDate(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, &fieldError{"invalid date format (want RFC3339 or YYYY-MM-DD): " + s}
}

// dedupeNonEmpty returns a copy of in with empty strings dropped and
// duplicates removed (case-insensitive). Order is preserved.
func dedupeNonEmpty(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		k := strings.ToLower(v)
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, v)
	}
	return out
}

// matchRowToListItem converts a repository row into the API DTO. Keeps the
// field mapping in one place so callers (List, Get) stay terse.
func matchRowToListItem(m repository.MatchRow) matchListItem {
	return matchListItem{
		ID:              m.ID,
		Format:          m.Format,
		Result:          m.Result,
		Timestamp:       m.Timestamp,
		DurationSeconds: m.DurationSeconds,
		DeckID:          m.DeckID,
		RankBefore:      m.RankBefore,
		RankAfter:       m.RankAfter,
		PlayerWins:      m.PlayerWins,
		OpponentWins:    m.OpponentWins,
	}
}

// fieldError is a small typed error used for 400-class request validation.
type fieldError struct{ msg string }

func (e *fieldError) Error() string { return e.msg }

// writeMatchesJSON serialises payload as JSON with status 200. Centralised
// so we don't repeat the Content-Type dance.
func writeMatchesJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

// parseLimitDefault returns the integer value of the named query param, or
// fallback when the param is missing or invalid. Used by handlers that take
// pagination via query string rather than body.
func parseLimitDefault(r *http.Request, name string, fallback int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
