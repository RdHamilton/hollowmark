package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/bff/internal/api/handlers"
	"github.com/RdHamilton/vault-mtg/services/bff/internal/api/middleware"
	"github.com/RdHamilton/vault-mtg/services/bff/internal/storage/repository"
)

// stubSummaryReader implements HistorySummaryReader for unit tests.
type stubSummaryReader struct {
	result repository.HistorySummaryResult
	err    error
}

func (s *stubSummaryReader) GetHistorySummary(_ context.Context, _ int64, _ time.Time) (repository.HistorySummaryResult, error) {
	return s.result, s.err
}

// authedSummaryHandler injects userID into context and delegates to GetSummary.
func authedSummaryHandler(h *handlers.HistorySummaryHandler, userID int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.WithUserID(r.Context(), userID)
		r = r.WithContext(ctx)
		h.GetSummary(w, r)
	})
}

// ─── Shape tests ─────────────────────────────────────────────────────────────

// TestGetSummary_ResponseShape verifies that all required JSON fields are
// present and have the right types.
func TestGetSummary_ResponseShape(t *testing.T) {
	arch := "RW Aggro"
	summary := &stubSummaryReader{
		result: repository.HistorySummaryResult{
			Today:    repository.HistorySummaryPeriod{Wins: 2, Losses: 1, WinRate: 2.0 / 3.0, Matches: 3},
			ThisWeek: repository.HistorySummaryPeriod{Wins: 5, Losses: 3, WinRate: 5.0 / 8.0, Matches: 8},
			AllTime:  repository.HistorySummaryPeriod{Wins: 50, Losses: 30, WinRate: 50.0 / 80.0, Matches: 80},
			Streak: repository.HistoryStreakInfo{
				CurrentStreak: 3,
				StreakType:    "W",
			},
			LastMatch: &repository.LastMatchInfo{
				Result:            "win",
				OpponentArchetype: &arch,
				ElapsedSeconds:    90,
			},
		},
	}

	h := handlers.NewHistorySummaryHandler(&stubAccountLookup{accountID: 42, found: true}, summary)
	handler := authedSummaryHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/summary", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// today
	today := resp["today"].(map[string]interface{})
	if today["wins"].(float64) != 2 {
		t.Errorf("today.wins: want 2, got %v", today["wins"])
	}
	if today["losses"].(float64) != 1 {
		t.Errorf("today.losses: want 1, got %v", today["losses"])
	}
	if today["win_rate"] == nil {
		t.Error("today.win_rate: missing")
	}

	// this_week
	week := resp["this_week"].(map[string]interface{})
	if week["wins"].(float64) != 5 {
		t.Errorf("this_week.wins: want 5, got %v", week["wins"])
	}
	if week["losses"].(float64) != 3 {
		t.Errorf("this_week.losses: want 3, got %v", week["losses"])
	}
	if week["matches"].(float64) != 8 {
		t.Errorf("this_week.matches: want 8, got %v", week["matches"])
	}

	// all_time
	allTime := resp["all_time"].(map[string]interface{})
	if allTime["wins"].(float64) != 50 {
		t.Errorf("all_time.wins: want 50, got %v", allTime["wins"])
	}
	if allTime["current_streak"].(float64) != 3 {
		t.Errorf("all_time.current_streak: want 3, got %v", allTime["current_streak"])
	}
	if allTime["streak_type"].(string) != "W" {
		t.Errorf("all_time.streak_type: want W, got %v", allTime["streak_type"])
	}

	// last_match
	lm := resp["last_match"].(map[string]interface{})
	if lm["result"].(string) != "win" {
		t.Errorf("last_match.result: want win, got %v", lm["result"])
	}
	if lm["opponent_archetype"].(string) != "RW Aggro" {
		t.Errorf("last_match.opponent_archetype: want RW Aggro, got %v", lm["opponent_archetype"])
	}
	if lm["elapsed_seconds"].(float64) != 90 {
		t.Errorf("last_match.elapsed_seconds: want 90, got %v", lm["elapsed_seconds"])
	}
}

// TestGetSummary_LastMatchNullWhenNoMatches verifies last_match is JSON null
// (not an absent key) when there are no matches.
func TestGetSummary_LastMatchNullWhenNoMatches(t *testing.T) {
	summary := &stubSummaryReader{
		result: repository.HistorySummaryResult{},
	}

	h := handlers.NewHistorySummaryHandler(&stubAccountLookup{accountID: 42, found: true}, summary)
	handler := authedSummaryHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/summary", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := resp["last_match"]; !ok {
		t.Error("last_match key must be present in response even when null")
	}
	if resp["last_match"] != nil {
		t.Errorf("last_match: want JSON null, got %v", resp["last_match"])
	}
}

// TestGetSummary_WinRateZeroWhenEmpty verifies win_rate=0.0 on empty account.
func TestGetSummary_WinRateZeroWhenEmpty(t *testing.T) {
	summary := &stubSummaryReader{
		result: repository.HistorySummaryResult{},
	}

	h := handlers.NewHistorySummaryHandler(&stubAccountLookup{accountID: 42, found: true}, summary)
	handler := authedSummaryHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/summary", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range []string{"today", "this_week", "all_time"} {
		period := resp[key].(map[string]interface{})
		if period["win_rate"].(float64) != 0.0 {
			t.Errorf("%s.win_rate: want 0.0, got %v", key, period["win_rate"])
		}
	}
}

// TestGetSummary_Unauthorized verifies 401 when user is not authenticated.
func TestGetSummary_Unauthorized(t *testing.T) {
	h := handlers.NewHistorySummaryHandler(
		&stubAccountLookup{accountID: 42, found: true},
		&stubSummaryReader{},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/summary", nil)
	rr := httptest.NewRecorder()
	h.GetSummary(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("want 401, got %d", rr.Code)
	}
}

// TestGetSummary_NoAccountReturnsEmptyShape verifies that a user without an
// account row gets a 200 with all-zero values rather than a 404.
func TestGetSummary_NoAccountReturnsEmptyShape(t *testing.T) {
	h := handlers.NewHistorySummaryHandler(
		&stubAccountLookup{accountID: 0, found: false},
		&stubSummaryReader{},
	)
	handler := authedSummaryHandler(h, 999)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/summary", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["last_match"] != nil {
		t.Errorf("last_match: want nil for no-account user, got %v", resp["last_match"])
	}
}

// TestGetSummary_OpponentArchetypeNullWhenAbsent verifies that
// opponent_archetype is JSON null when the repo returns nil.
func TestGetSummary_OpponentArchetypeNullWhenAbsent(t *testing.T) {
	summary := &stubSummaryReader{
		result: repository.HistorySummaryResult{
			LastMatch: &repository.LastMatchInfo{
				Result:            "loss",
				OpponentArchetype: nil,
				ElapsedSeconds:    300,
			},
		},
	}

	h := handlers.NewHistorySummaryHandler(&stubAccountLookup{accountID: 42, found: true}, summary)
	handler := authedSummaryHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/history/summary", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	lm := resp["last_match"].(map[string]interface{})
	if lm["opponent_archetype"] != nil {
		t.Errorf("opponent_archetype: want JSON null, got %v", lm["opponent_archetype"])
	}
}
