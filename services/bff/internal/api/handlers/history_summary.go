package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// HistorySummaryReader is the interface the handler uses for the summary repo.
type HistorySummaryReader interface {
	GetHistorySummary(ctx context.Context, accountID int64, now time.Time) (repository.HistorySummaryResult, error)
}

// HistorySummaryHandler handles GET /api/v1/history/summary.
type HistorySummaryHandler struct {
	accounts AccountLookup
	summary  HistorySummaryReader
}

// NewHistorySummaryHandler returns a HistorySummaryHandler.
func NewHistorySummaryHandler(accounts AccountLookup, summary HistorySummaryReader) *HistorySummaryHandler {
	return &HistorySummaryHandler{accounts: accounts, summary: summary}
}

// ── JSON response types ────────────────────────────────────────────────────────

type summaryPeriodResponse struct {
	Wins    int     `json:"wins"`
	Losses  int     `json:"losses"`
	WinRate float64 `json:"win_rate"`
}

type summaryWeekResponse struct {
	Wins    int     `json:"wins"`
	Losses  int     `json:"losses"`
	WinRate float64 `json:"win_rate"`
	Matches int     `json:"matches"`
}

type summaryAllTimeResponse struct {
	Wins          int     `json:"wins"`
	Losses        int     `json:"losses"`
	WinRate       float64 `json:"win_rate"`
	Matches       int     `json:"matches"`
	CurrentStreak int     `json:"current_streak"`
	StreakType    string  `json:"streak_type"`
}

// lastMatchResponse is used when there is at least one match; the entire field
// is null in JSON when there are no matches.
type lastMatchResponse struct {
	Result            string  `json:"result"`
	OpponentArchetype *string `json:"opponent_archetype"`
	ElapsedSeconds    int     `json:"elapsed_seconds"`
}

type historySummaryResponse struct {
	Today     summaryPeriodResponse  `json:"today"`
	ThisWeek  summaryWeekResponse    `json:"this_week"`
	AllTime   summaryAllTimeResponse `json:"all_time"`
	LastMatch *lastMatchResponse     `json:"last_match"`
}

// GetSummary handles GET /api/v1/history/summary.
// Returns per-window win/loss counts, a streak, and last-match metadata.
// Clerk-authenticated: the user id is read from context.
func (h *HistorySummaryHandler) GetSummary(w http.ResponseWriter, r *http.Request) {
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	accountID, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[HistorySummaryHandler.GetSummary] GetAccountIDByUserID userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if !found {
		// User exists in Clerk but has no MTGA account row yet. Return empty
		// summary rather than 404 so the SPA always gets a valid JSON shape.
		writeJSONSummary(w, emptyHistorySummaryResponse())
		return
	}

	data, err := h.summary.GetHistorySummary(r.Context(), accountID, time.Now().UTC())
	if err != nil {
		log.Printf("[HistorySummaryHandler.GetSummary] GetHistorySummary accountID=%d: %v", accountID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := historySummaryResponse{
		Today: summaryPeriodResponse{
			Wins:    data.Today.Wins,
			Losses:  data.Today.Losses,
			WinRate: data.Today.WinRate,
		},
		ThisWeek: summaryWeekResponse{
			Wins:    data.ThisWeek.Wins,
			Losses:  data.ThisWeek.Losses,
			WinRate: data.ThisWeek.WinRate,
			Matches: data.ThisWeek.Matches,
		},
		AllTime: summaryAllTimeResponse{
			Wins:          data.AllTime.Wins,
			Losses:        data.AllTime.Losses,
			WinRate:       data.AllTime.WinRate,
			Matches:       data.AllTime.Matches,
			CurrentStreak: data.Streak.CurrentStreak,
			StreakType:    data.Streak.StreakType,
		},
	}

	if data.LastMatch != nil {
		resp.LastMatch = &lastMatchResponse{
			Result:            data.LastMatch.Result,
			OpponentArchetype: data.LastMatch.OpponentArchetype,
			ElapsedSeconds:    data.LastMatch.ElapsedSeconds,
		}
	}

	writeJSONSummary(w, resp)
}

func emptyHistorySummaryResponse() historySummaryResponse {
	return historySummaryResponse{
		Today:     summaryPeriodResponse{},
		ThisWeek:  summaryWeekResponse{},
		AllTime:   summaryAllTimeResponse{},
		LastMatch: nil,
	}
}

func writeJSONSummary(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[writeJSONSummary] encode: %v", err)
	}
}
