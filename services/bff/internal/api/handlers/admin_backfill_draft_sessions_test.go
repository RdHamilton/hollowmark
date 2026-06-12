package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// stubBackfiller is a test double for draftSessionsBackfiller.
type stubBackfiller struct {
	result repository.BackfillResult
	err    error
	called bool
	opts   repository.BackfillOptions
}

func (s *stubBackfiller) BackfillStaleDraftSessions(_ context.Context, opts repository.BackfillOptions) (repository.BackfillResult, error) {
	s.called = true
	s.opts = opts
	return s.result, s.err
}

func TestAdminBackfillDraftSessionsHandler_DefaultOptions(t *testing.T) {
	stub := &stubBackfiller{
		result: repository.BackfillResult{
			ClosedCompleted: []string{"ds-a"},
			ClosedAbandoned: []string{"ds-b", "ds-c"},
		},
	}

	h := handlers.NewAdminBackfillDraftSessionsHandler(stub)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ops/backfill-draft-sessions", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — body: %s", w.Code, w.Body.String())
	}

	var body struct {
		ClosedCompleted []string `json:"closed_completed"`
		ClosedAbandoned []string `json:"closed_abandoned"`
		TotalClosed     int      `json:"total_closed"`
	}

	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.TotalClosed != 3 {
		t.Errorf("total_closed: want 3, got %d", body.TotalClosed)
	}

	if len(body.ClosedCompleted) != 1 {
		t.Errorf("closed_completed: want 1, got %d", len(body.ClosedCompleted))
	}

	if len(body.ClosedAbandoned) != 2 {
		t.Errorf("closed_abandoned: want 2, got %d", len(body.ClosedAbandoned))
	}

	// Verify default opts were forwarded.
	if stub.opts.StalenessThreshold != 4*time.Hour {
		t.Errorf("default staleness_hours: want 4h, got %v", stub.opts.StalenessThreshold)
	}

	if stub.opts.MinPicksForStale != 42 {
		t.Errorf("default min_picks: want 42, got %d", stub.opts.MinPicksForStale)
	}

	if stub.opts.AccountID != 0 {
		t.Errorf("default account_id: want 0, got %d", stub.opts.AccountID)
	}
}

func TestAdminBackfillDraftSessionsHandler_QueryParamOverrides(t *testing.T) {
	stub := &stubBackfiller{}

	h := handlers.NewAdminBackfillDraftSessionsHandler(stub)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/ops/backfill-draft-sessions?staleness_hours=8&min_picks=45&account_id=99",
		nil,
	)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d — body: %s", w.Code, w.Body.String())
	}

	if stub.opts.StalenessThreshold != 8*time.Hour {
		t.Errorf("staleness_hours override: want 8h, got %v", stub.opts.StalenessThreshold)
	}

	if stub.opts.MinPicksForStale != 45 {
		t.Errorf("min_picks override: want 45, got %d", stub.opts.MinPicksForStale)
	}

	if stub.opts.AccountID != 99 {
		t.Errorf("account_id override: want 99, got %d", stub.opts.AccountID)
	}
}

func TestAdminBackfillDraftSessionsHandler_EmptyResult_NoNullArrays(t *testing.T) {
	stub := &stubBackfiller{
		result: repository.BackfillResult{}, // nil slices
	}

	h := handlers.NewAdminBackfillDraftSessionsHandler(stub)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ops/backfill-draft-sessions", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	// Both fields must be JSON arrays, not null, so the caller can iterate safely.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}

	for _, key := range []string{"closed_completed", "closed_abandoned"} {
		val := string(raw[key])
		if val == "null" || val == "" {
			t.Errorf("%s: want JSON array, got %q", key, val)
		}
	}
}
