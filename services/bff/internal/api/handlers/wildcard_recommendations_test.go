package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/bff/internal/api/handlers"
	bffmiddleware "github.com/RdHamilton/vault-mtg/services/bff/internal/api/middleware"
)

// ─── stubs ───────────────────────────────────────────────────────────────────

type wildcardAccountLookup struct {
	accountID int64
	found     bool
	err       error
}

func (w *wildcardAccountLookup) GetAccountIDByUserID(_ context.Context, _ int64) (int64, bool, error) {
	return w.accountID, w.found, w.err
}

type stubInventoryReader struct{}

func (s *stubInventoryReader) GetWildcardCounts(_ context.Context, _ int64) (handlers.WildcardCounts, error) {
	return handlers.WildcardCounts{}, nil
}

type stubCardInventoryChecker struct{}

func (s *stubCardInventoryChecker) HasCardInventory(_ context.Context, _ int64) (bool, error) {
	return true, nil
}

type stubDraftRatingsMaxCacheChecker struct{}

func (s *stubDraftRatingsMaxCacheChecker) GetMaxCachedAt(_ context.Context, _ string) (*time.Time, error) {
	return nil, nil
}

type stubMetaFreshnessChecker struct{}

func (s *stubMetaFreshnessChecker) GetMetaLastUpdated(_ context.Context, _ string) (*time.Time, error) {
	return nil, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func authedWildcardRequest(t *testing.T, method, target string, userID int64) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	return req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
}

func newWildcardHandler(accounts handlers.AccountLookup) *handlers.WildcardRecommendationsHandler {
	return handlers.NewWildcardRecommendationsHandler(
		accounts,
		&stubInventoryReader{},
		&stubCardInventoryChecker{},
		&stubDraftRatingsMaxCacheChecker{},
		&stubMetaFreshnessChecker{},
	)
}

func decodeWildcardEnvelope(t *testing.T, body []byte) map[string]any {
	t.Helper()
	wrapper := struct {
		Data json.RawMessage `json:"data"`
	}{}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		t.Fatalf("envelope decode: %v body=%s", err, string(body))
	}
	var out map[string]any
	if err := json.Unmarshal(wrapper.Data, &out); err != nil {
		t.Fatalf("payload decode: %v data=%s", err, string(wrapper.Data))
	}
	return out
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestWildcardRecommendations_Unauthorized verifies the handler returns 401
// when no user ID is present in context (i.e., auth middleware stripped it out).
func TestWildcardRecommendations_Unauthorized(t *testing.T) {
	h := newWildcardHandler(&wildcardAccountLookup{accountID: 7, found: true})
	// No userID injected into context — simulates a request that bypassed auth.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/recommendations/wildcards", nil)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestWildcardRecommendations_StubReturns501 verifies the 501 stub response
// matches the complete ADR-045 JSON shape with an empty recommendations slice.
func TestWildcardRecommendations_StubReturns501(t *testing.T) {
	h := newWildcardHandler(&wildcardAccountLookup{accountID: 7, found: true})
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501 Not Implemented, got %d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeWildcardEnvelope(t, rr.Body.Bytes())

	// ADR-045 shape: wildcard_budget
	budget, ok := resp["wildcard_budget"].(map[string]any)
	if !ok {
		t.Fatalf("wildcard_budget missing or wrong type: %v", resp)
	}
	for _, field := range []string{"common", "uncommon", "rare", "mythic"} {
		if _, exists := budget[field]; !exists {
			t.Errorf("wildcard_budget.%s missing", field)
		}
	}

	// ADR-045 shape: data_freshness
	freshness, ok := resp["data_freshness"].(map[string]any)
	if !ok {
		t.Fatalf("data_freshness missing or wrong type: %v", resp)
	}
	for _, field := range []string{"card_ratings_cached_at", "meta_last_updated", "stale"} {
		if _, exists := freshness[field]; !exists {
			t.Errorf("data_freshness.%s missing", field)
		}
	}

	// ADR-045 shape: recommendations must be an empty slice (not nil/absent).
	recs, ok := resp["recommendations"]
	if !ok {
		t.Fatal("recommendations field missing from response")
	}
	recsSlice, ok := recs.([]any)
	if !ok {
		t.Fatalf("recommendations must be a JSON array, got %T: %v", recs, recs)
	}
	if len(recsSlice) != 0 {
		t.Errorf("stub must return empty recommendations slice, got %d entries", len(recsSlice))
	}
}

// TestWildcardRecommendations_FormatParamAccepted verifies that an explicit
// ?format= query parameter is accepted without error (value propagation is a
// #420 concern; here we only verify the stub does not reject it).
func TestWildcardRecommendations_FormatParamAccepted(t *testing.T) {
	h := newWildcardHandler(&wildcardAccountLookup{accountID: 7, found: true})
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards?format=Historic", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)

	if rr.Code != http.StatusNotImplemented {
		t.Errorf("expected 501 with format param, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestWildcardRecommendations_AccountNotFound verifies the handler returns 404
// when the user has no account row provisioned yet.
func TestWildcardRecommendations_AccountNotFound(t *testing.T) {
	h := newWildcardHandler(&wildcardAccountLookup{accountID: 0, found: false})
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d body=%s", rr.Code, rr.Body.String())
	}
}
