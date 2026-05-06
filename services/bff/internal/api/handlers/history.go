package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	bffmiddleware "github.com/ramonehamilton/mtga-bff/internal/api/middleware"
	"github.com/ramonehamilton/mtga-bff/internal/storage/repository"
)

// knownFormats is the set of MTGA format strings the history endpoint accepts.
// Case-insensitive comparison is used during validation.
var knownFormats = map[string]struct{}{
	"standard":  {},
	"historic":  {},
	"brawl":     {},
	"limited":   {},
	"draft":     {},
	"sealed":    {},
	"alchemy":   {},
	"explorer":  {},
	"timeless":  {},
	"gladiator": {},
	"pauper":    {},
}

// AccountLookup is the minimal interface the history handlers need to resolve
// a user's account_id from their DB user_id.
type AccountLookup interface {
	GetAccountIDByUserID(ctx context.Context, userID int64) (int64, bool, error)
}

// MatchHistoryReader is the minimal interface for reading match history.
type MatchHistoryReader interface {
	ListByAccountID(ctx context.Context, accountID int64, format string, page int, limit int) ([]repository.MatchRow, int, error)
}

// DraftHistoryReader is the minimal interface for reading draft history.
type DraftHistoryReader interface {
	ListByAccountID(ctx context.Context, accountID int64, setCode string, page int, limit int) ([]repository.DraftSessionRow, int, error)
}

// HistoryHandler handles GET /api/v1/history/matches and /api/v1/history/drafts.
type HistoryHandler struct {
	accounts AccountLookup
	matches  MatchHistoryReader
	drafts   DraftHistoryReader
}

// NewHistoryHandler returns a HistoryHandler wired with the provided repos.
func NewHistoryHandler(accounts AccountLookup, matches MatchHistoryReader, drafts DraftHistoryReader) *HistoryHandler {
	return &HistoryHandler{accounts: accounts, matches: matches, drafts: drafts}
}

// matchResponse is the JSON shape for a single match in the history list.
type matchResponse struct {
	ID              string    `json:"id"`
	Format          string    `json:"format"`
	Result          string    `json:"result"`
	Timestamp       time.Time `json:"timestamp"`
	DurationSeconds *int      `json:"duration_seconds"`
	DeckID          *string   `json:"deck_id"`
	RankBefore      *string   `json:"rank_before"`
	RankAfter       *string   `json:"rank_after"`
	OpponentRank    *string   `json:"opponent_rank"`
	PlayerWins      int       `json:"player_wins"`
	OpponentWins    int       `json:"opponent_wins"`
}

// draftResponse is the JSON shape for a single draft session in the history list.
type draftResponse struct {
	ID          string     `json:"id"`
	SetCode     string     `json:"set_code"`
	Format      string     `json:"format"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Wins        int        `json:"wins"`
	Losses      int        `json:"losses"`
}

type paginatedResponse struct {
	Data  interface{} `json:"data"`
	Total int         `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
}

// GetMatches handles GET /api/v1/history/matches.
func (h *HistoryHandler) GetMatches(w http.ResponseWriter, r *http.Request) {
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	page, limit, err := parsePagination(r)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	format := r.URL.Query().Get("format")
	if format != "" {
		if _, ok := knownFormats[strings.ToLower(format)]; !ok {
			writeJSONError(w, "unknown format: "+format, http.StatusBadRequest)
			return
		}
	}

	accountID, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[HistoryHandler.GetMatches] GetAccountIDByUserID userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if !found {
		// User has no account yet — return empty result.
		writeHistoryJSON(w, paginatedResponse{Data: []matchResponse{}, Total: 0, Page: page, Limit: limit})
		return
	}

	rows, total, err := h.matches.ListByAccountID(r.Context(), accountID, format, page, limit)
	if err != nil {
		log.Printf("[HistoryHandler.GetMatches] ListByAccountID accountID=%d: %v", accountID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	data := make([]matchResponse, 0, len(rows))
	for _, m := range rows {
		data = append(data, matchResponse{
			ID:              m.ID,
			Format:          m.Format,
			Result:          m.Result,
			Timestamp:       m.Timestamp,
			DurationSeconds: m.DurationSeconds,
			DeckID:          m.DeckID,
			RankBefore:      m.RankBefore,
			RankAfter:       m.RankAfter,
			OpponentRank:    nil, // v0.2.0: not available
			PlayerWins:      m.PlayerWins,
			OpponentWins:    m.OpponentWins,
		})
	}

	writeHistoryJSON(w, paginatedResponse{Data: data, Total: total, Page: page, Limit: limit})
}

// GetDrafts handles GET /api/v1/history/drafts.
func (h *HistoryHandler) GetDrafts(w http.ResponseWriter, r *http.Request) {
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	page, limit, err := parsePagination(r)
	if err != nil {
		writeJSONError(w, err.Error(), http.StatusBadRequest)
		return
	}

	setCode := r.URL.Query().Get("set_code")
	if setCode != "" && !isValidSetCode(setCode) {
		writeJSONError(w, "invalid set_code: must be 3-5 uppercase letters", http.StatusBadRequest)
		return
	}

	accountID, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[HistoryHandler.GetDrafts] GetAccountIDByUserID userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if !found {
		writeHistoryJSON(w, paginatedResponse{Data: []draftResponse{}, Total: 0, Page: page, Limit: limit})
		return
	}

	rows, total, err := h.drafts.ListByAccountID(r.Context(), accountID, setCode, page, limit)
	if err != nil {
		log.Printf("[HistoryHandler.GetDrafts] ListByAccountID accountID=%d: %v", accountID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	data := make([]draftResponse, 0, len(rows))
	for _, d := range rows {
		data = append(data, draftResponse{
			ID:          d.ID,
			SetCode:     d.SetCode,
			Format:      draftTypeToFormat(d.DraftType),
			StartedAt:   d.StartTime,
			CompletedAt: d.EndTime,
			Wins:        d.Wins,
			Losses:      d.Losses,
		})
	}

	writeHistoryJSON(w, paginatedResponse{Data: data, Total: total, Page: page, Limit: limit})
}

// parsePagination parses and validates page and limit query params.
func parsePagination(r *http.Request) (page, limit int, err error) {
	page = 1
	limit = 20

	if s := r.URL.Query().Get("page"); s != "" {
		page, err = strconv.Atoi(s)
		if err != nil || page < 1 {
			return 0, 0, historyErrorf("invalid page: must be a positive integer")
		}
	}

	if s := r.URL.Query().Get("limit"); s != "" {
		limit, err = strconv.Atoi(s)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, historyErrorf("invalid limit: must be between 1 and 100")
		}
	}

	return page, limit, nil
}

// isValidSetCode validates that set_code is 3-5 uppercase ASCII letters.
func isValidSetCode(s string) bool {
	if len(s) < 3 || len(s) > 5 {
		return false
	}

	for _, r := range s {
		if !unicode.IsLetter(r) || !unicode.IsUpper(r) {
			return false
		}
	}

	return true
}

// draftTypeToFormat converts a draft_sessions.draft_type value to a display format string.
func draftTypeToFormat(draftType string) string {
	switch draftType {
	case "quick_draft":
		return "QuickDraft"
	case "premier_draft":
		return "PremierDraft"
	case "traditional_draft":
		return "TraditionalDraft"
	case "sealed":
		return "Sealed"
	case "traditional_sealed":
		return "TraditionalSealed"
	default:
		return draftType
	}
}

// writeHistoryJSON serialises v as JSON with a 200 status.
func writeHistoryJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[writeHistoryJSON] encode: %v", err)
	}
}

// historyErrorf returns an error with the given message.
func historyErrorf(msg string) error {
	return &historyError{msg: msg}
}

type historyError struct{ msg string }

func (e *historyError) Error() string { return e.msg }
