package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
)

// stubFreshnessChecker is a test double for CardRatingsFreshnessChecker.
type stubFreshnessChecker struct {
	ts  time.Time
	err error
}

func (s *stubFreshnessChecker) GetGlobalMaxCachedAt(_ context.Context) (time.Time, error) {
	return s.ts, s.err
}

// ─── handler unit tests (no DB) ──────────────────────────────────────────────

func TestAdminDataFreshnessHandler_FreshData_ReturnsFreshStatus(t *testing.T) {
	// MAX(cached_at) is 1 hour ago — well within the 48-hour threshold.
	ts := time.Now().UTC().Add(-1 * time.Hour)
	h := handlers.NewAdminDataFreshnessHandler(&stubFreshnessChecker{ts: ts}, 48)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/data-freshness", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["status"] != "fresh" {
		t.Errorf("status: want %q, got %q", "fresh", body["status"])
	}
	if _, ok := body["max_cached_at"]; !ok {
		t.Error("response missing max_cached_at")
	}
	if _, ok := body["age_hours"]; !ok {
		t.Error("response missing age_hours")
	}
	if _, ok := body["threshold_hours"]; !ok {
		t.Error("response missing threshold_hours")
	}
}

func TestAdminDataFreshnessHandler_StaleData_ReturnsStaleThenStatus(t *testing.T) {
	// MAX(cached_at) is 72 hours ago — exceeds the 48-hour threshold.
	ts := time.Now().UTC().Add(-72 * time.Hour)
	h := handlers.NewAdminDataFreshnessHandler(&stubFreshnessChecker{ts: ts}, 48)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/data-freshness", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 even for stale data, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["status"] != "stale" {
		t.Errorf("status: want %q, got %q", "stale", body["status"])
	}
}

func TestAdminDataFreshnessHandler_NoRows_ReturnsNoData(t *testing.T) {
	// Zero time means no rows exist in draft_card_ratings.
	h := handlers.NewAdminDataFreshnessHandler(&stubFreshnessChecker{ts: time.Time{}}, 48)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/data-freshness", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 when no data, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["status"] != "no_data" {
		t.Errorf("status: want %q, got %q", "no_data", body["status"])
	}
}

func TestAdminDataFreshnessHandler_DBError_Returns500(t *testing.T) {
	h := handlers.NewAdminDataFreshnessHandler(
		&stubFreshnessChecker{err: errors.New("db exploded")},
		48,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/data-freshness", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d", rr.Code)
	}
}

func TestAdminDataFreshnessHandler_ContentTypeIsJSON(t *testing.T) {
	ts := time.Now().UTC().Add(-1 * time.Hour)
	h := handlers.NewAdminDataFreshnessHandler(&stubFreshnessChecker{ts: ts}, 48)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/data-freshness", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type: want %q, got %q", "application/json", ct)
	}
}

func TestAdminDataFreshnessHandler_ResponseContainsNoPII(t *testing.T) {
	ts := time.Now().UTC().Add(-1 * time.Hour)
	h := handlers.NewAdminDataFreshnessHandler(&stubFreshnessChecker{ts: ts}, 48)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/data-freshness", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	for _, badKey := range []string{"account_id", "accounts", "user_id", "email"} {
		if _, ok := body[badKey]; ok {
			t.Errorf("response must not contain PII key %q", badKey)
		}
	}
}

func TestAdminDataFreshnessHandler_ThresholdBoundary_FreshAtExactBoundary(t *testing.T) {
	// Exactly at the threshold boundary (47h59m old with 48h threshold) = fresh.
	ts := time.Now().UTC().Add(-47*time.Hour - 59*time.Minute)
	h := handlers.NewAdminDataFreshnessHandler(&stubFreshnessChecker{ts: ts}, 48)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/data-freshness", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["status"] != "fresh" {
		t.Errorf("status: want %q (below threshold), got %q", "fresh", body["status"])
	}
}
