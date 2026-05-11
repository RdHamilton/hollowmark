package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/ramonehamilton/mtga-bff/internal/api/handlers"
	bffmiddleware "github.com/ramonehamilton/mtga-bff/internal/api/middleware"
	"github.com/ramonehamilton/mtga-bff/internal/storage/repository"
)

// ─── stubs ──────────────────────────────────────────────────────────────────

// stubAccountLookup is already declared in history_test.go; reuse it via
// matchesAccountLookup to avoid the duplicate declaration error.
type matchesAccountLookup struct {
	accountID int64
	found     bool
	err       error
}

func (s *matchesAccountLookup) GetAccountIDByUserID(_ context.Context, _ int64) (int64, bool, error) {
	return s.accountID, s.found, s.err
}

type stubMatchesReader struct {
	listRows   []repository.MatchRow
	listTotal  int
	listFilter repository.MatchFilter
	listErr    error

	getRow *repository.MatchRow
	getErr error

	formats   []string
	formatErr error
}

func (s *stubMatchesReader) ListByAccountIDFiltered(_ context.Context, _ int64, f repository.MatchFilter) ([]repository.MatchRow, int, error) {
	s.listFilter = f
	return s.listRows, s.listTotal, s.listErr
}

func (s *stubMatchesReader) GetByID(_ context.Context, _ int64, _ string) (*repository.MatchRow, error) {
	return s.getRow, s.getErr
}

func (s *stubMatchesReader) DistinctFormats(_ context.Context, _ int64) ([]string, error) {
	return s.formats, s.formatErr
}

// ─── helpers ────────────────────────────────────────────────────────────────

// requestWithUserID builds an authenticated request — UserIDFromContext picks
// up the user id the same way DaemonAPIKeyAuth would have set it.
func requestWithUserID(t *testing.T, method, target string, body []byte, userID int64) *http.Request {
	t.Helper()
	var r *http.Request
	if body != nil {
		r = httptest.NewRequest(method, target, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	return r.WithContext(bffmiddleware.WithUserID(r.Context(), userID))
}

// ─── List ───────────────────────────────────────────────────────────────────

func TestMatchesList_HappyPath(t *testing.T) {
	timestamp := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	dur := 480
	deck := "deck-abc"
	reader := &stubMatchesReader{
		listRows: []repository.MatchRow{
			{ID: "m1", Format: "standard_bo1", Result: "win", Timestamp: timestamp, DurationSeconds: &dur, DeckID: &deck, PlayerWins: 2, OpponentWins: 1},
			{ID: "m2", Format: "draft_bo1", Result: "loss", Timestamp: timestamp.Add(-time.Hour), PlayerWins: 0, OpponentWins: 2},
		},
		listTotal: 2,
	}
	accts := &matchesAccountLookup{accountID: 7, found: true}
	h := handlers.NewMatchesHandler(reader, accts)

	body, _ := json.Marshal(map[string]any{"format": "standard_bo1", "page": 1, "limit": 50})
	req := requestWithUserID(t, http.MethodPost, "/api/v1/matches", body, 168)
	rr := httptest.NewRecorder()
	h.List(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200, body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Matches []map[string]any `json:"matches"`
		Total   int              `json:"total"`
		Page    int              `json:"page"`
		Limit   int              `json:"limit"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 2 || len(resp.Matches) != 2 {
		t.Fatalf("totals: total=%d matches=%d body=%s", resp.Total, len(resp.Matches), rr.Body.String())
	}
	// camelCase keys in the response
	first := resp.Matches[0]
	if _, ok := first["id"]; !ok {
		t.Errorf("missing 'id' key in camelCase response: %v", first)
	}
	if _, ok := first["durationSeconds"]; !ok {
		t.Errorf("missing 'durationSeconds' key in camelCase response: %v", first)
	}
	if first["playerWins"].(float64) != 2 {
		t.Errorf("playerWins: got %v", first["playerWins"])
	}
	// filter was forwarded
	if reader.listFilter.Format != "standard_bo1" {
		t.Errorf("filter.Format: got %q", reader.listFilter.Format)
	}
}

func TestMatchesList_Unauthorized(t *testing.T) {
	reader := &stubMatchesReader{}
	h := handlers.NewMatchesHandler(reader, &matchesAccountLookup{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/matches", bytes.NewReader([]byte(`{}`)))
	rr := httptest.NewRecorder()
	h.List(rr, req) // no user_id on context

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", rr.Code)
	}
}

func TestMatchesList_NoAccountReturnsEmptyPage(t *testing.T) {
	reader := &stubMatchesReader{}
	accts := &matchesAccountLookup{found: false}
	h := handlers.NewMatchesHandler(reader, accts)

	req := requestWithUserID(t, http.MethodPost, "/api/v1/matches", []byte(`{}`), 168)
	rr := httptest.NewRecorder()
	h.List(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rr.Code)
	}
	var resp struct {
		Matches []any `json:"matches"`
		Total   int   `json:"total"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp.Total != 0 || len(resp.Matches) != 0 {
		t.Errorf("expected empty page, got total=%d matches=%d", resp.Total, len(resp.Matches))
	}
}

func TestMatchesList_RejectsInvalidResult(t *testing.T) {
	reader := &stubMatchesReader{}
	accts := &matchesAccountLookup{accountID: 7, found: true}
	h := handlers.NewMatchesHandler(reader, accts)

	body, _ := json.Marshal(map[string]any{"result": "bogus"})
	req := requestWithUserID(t, http.MethodPost, "/api/v1/matches", body, 168)
	rr := httptest.NewRecorder()
	h.List(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rr.Code)
	}
}

// ─── Get ────────────────────────────────────────────────────────────────────

func TestMatchesGet_HappyPath(t *testing.T) {
	row := repository.MatchRow{ID: "m1", Format: "standard_bo1", Result: "win", Timestamp: time.Now().UTC(), PlayerWins: 2, OpponentWins: 0}
	reader := &stubMatchesReader{getRow: &row}
	h := handlers.NewMatchesHandler(reader, &matchesAccountLookup{accountID: 7, found: true})

	req := requestWithUserID(t, http.MethodGet, "/api/v1/matches/m1", nil, 168)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("matchId", "m1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	h.Get(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if resp["id"] != "m1" {
		t.Errorf("id: %v", resp["id"])
	}
}

func TestMatchesGet_NotFound(t *testing.T) {
	reader := &stubMatchesReader{getRow: nil}
	h := handlers.NewMatchesHandler(reader, &matchesAccountLookup{accountID: 7, found: true})

	req := requestWithUserID(t, http.MethodGet, "/api/v1/matches/missing", nil, 168)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("matchId", "missing")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	h.Get(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rr.Code)
	}
}

// ─── Formats ────────────────────────────────────────────────────────────────

func TestMatchesFormats_HappyPath(t *testing.T) {
	reader := &stubMatchesReader{formats: []string{"standard_bo1", "draft_bo1"}}
	h := handlers.NewMatchesHandler(reader, &matchesAccountLookup{accountID: 7, found: true})

	req := requestWithUserID(t, http.MethodGet, "/api/v1/matches/formats", nil, 168)
	rr := httptest.NewRecorder()
	h.Formats(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var formats []string
	_ = json.NewDecoder(rr.Body).Decode(&formats)
	if len(formats) != 2 {
		t.Errorf("expected 2 formats, got %v", formats)
	}
}
