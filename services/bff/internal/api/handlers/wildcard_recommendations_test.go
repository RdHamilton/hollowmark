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
	"github.com/RdHamilton/vault-mtg/services/bff/internal/storage/repository"
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

type stubInventoryReader struct {
	counts repository.WildcardCounts
}

func (s *stubInventoryReader) GetWildcardCounts(_ context.Context, _ int64) (repository.WildcardCounts, error) {
	return s.counts, nil
}

type stubCardInventoryChecker struct {
	has bool
}

func (s *stubCardInventoryChecker) HasCardInventory(_ context.Context, _ int64) (bool, error) {
	return s.has, nil
}

type stubDraftRatingsMaxCacheChecker struct {
	cachedAt *time.Time
}

func (s *stubDraftRatingsMaxCacheChecker) GetMaxCachedAtByFormat(_ context.Context, _ string) (*time.Time, error) {
	return s.cachedAt, nil
}

type stubMetaFreshnessChecker struct {
	lastUpdated *time.Time
}

func (s *stubMetaFreshnessChecker) GetMetaLastUpdated(_ context.Context, _ string) (*time.Time, error) {
	return s.lastUpdated, nil
}

type stubGapQueryRunner struct {
	rows []repository.WildcardGapRow
}

func (s *stubGapQueryRunner) GetWildcardGapRows(_ context.Context, _ int64, _ string) ([]repository.WildcardGapRow, error) {
	return s.rows, nil
}

func (s *stubGapQueryRunner) CountCardInventory(_ context.Context, _ int64) (int, error) {
	// Return a count above the sparse threshold so no data quality warning is emitted
	// in standard test cases.
	return 100, nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// recentMeta returns a *time.Time 1 hour ago — a "fresh" meta timestamp that
// passes the 7-day staleness guard in the handler.
func recentMeta() *time.Time {
	t := time.Now().Add(-1 * time.Hour)
	return &t
}

func authedWildcardRequest(t *testing.T, method, target string, userID int64) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	return req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
}

func newWildcardHandler(accounts handlers.AccountLookup) *handlers.WildcardRecommendationsHandler {
	return handlers.NewWildcardRecommendationsHandler(
		accounts,
		&stubInventoryReader{},
		&stubCardInventoryChecker{has: true},
		&stubDraftRatingsMaxCacheChecker{},
		&stubMetaFreshnessChecker{lastUpdated: recentMeta()},
		&stubGapQueryRunner{},
		&stubGapQueryRunner{},
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/recommendations/wildcards", nil)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d body=%s", rr.Code, rr.Body.String())
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

// TestWildcardRecommendations_Returns200 verifies the full implementation
// returns HTTP 200 (not 501) with the complete ADR-045 JSON shape.
func TestWildcardRecommendations_Returns200(t *testing.T) {
	h := newWildcardHandler(&wildcardAccountLookup{accountID: 7, found: true})
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d body=%s", rr.Code, rr.Body.String())
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

	// ADR-045 shape: recommendations must be a JSON array (never null).
	recs, ok := resp["recommendations"]
	if !ok {
		t.Fatal("recommendations field missing from response")
	}
	recsSlice, ok := recs.([]any)
	if !ok {
		t.Fatalf("recommendations must be a JSON array, got %T: %v", recs, recs)
	}
	// Empty stub gap query → empty recommendations.
	if len(recsSlice) != 0 {
		t.Errorf("expected empty recommendations with stub gap query, got %d entries", len(recsSlice))
	}
}

// TestWildcardRecommendations_FormatParamAllowlist verifies the format allowlist:
// valid values pass through; invalid values default to Standard.
func TestWildcardRecommendations_FormatParamAllowlist(t *testing.T) {
	cases := []struct {
		param  string
		wantOK bool // expect 200 (not rejected)
	}{
		{"Standard", true},
		{"Historic", true},
		{"Alchemy", true},
		{"Explorer", true},
		{"Pauper", true},   // invalid — defaults to Standard, still 200
		{"", true},         // empty — defaults to Standard, still 200
		{"standard", true}, // wrong case — defaults to Standard, still 200
	}

	for _, tc := range cases {
		t.Run("format="+tc.param, func(t *testing.T) {
			h := newWildcardHandler(&wildcardAccountLookup{accountID: 7, found: true})
			url := "/api/v1/recommendations/wildcards"
			if tc.param != "" {
				url += "?format=" + tc.param
			}
			req := authedWildcardRequest(t, http.MethodGet, url, 42)
			rr := httptest.NewRecorder()
			h.GetWildcardRecommendations(rr, req)
			if tc.wantOK && rr.Code != http.StatusOK {
				t.Errorf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
			}
		})
	}
}

// TestWildcardRecommendations_ZeroCollection_Returns409 verifies that the
// handler returns 409 when card_inventory is empty (ADR-045 §4 guard).
func TestWildcardRecommendations_ZeroCollection_Returns409(t *testing.T) {
	h := handlers.NewWildcardRecommendationsHandler(
		&wildcardAccountLookup{accountID: 7, found: true},
		&stubInventoryReader{},
		&stubCardInventoryChecker{has: false}, // zero collection
		&stubDraftRatingsMaxCacheChecker{},
		&stubMetaFreshnessChecker{lastUpdated: recentMeta()},
		&stubGapQueryRunner{},
		&stubGapQueryRunner{},
	)
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)
	if rr.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict for zero collection, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestWildcardRecommendations_StaleMeta_Returns503 verifies that the handler
// returns 503 when meta last_updated is older than 7 days (ADR-045 §5).
func TestWildcardRecommendations_StaleMeta_Returns503(t *testing.T) {
	stale := time.Now().Add(-8 * 24 * time.Hour) // 8 days ago
	h := handlers.NewWildcardRecommendationsHandler(
		&wildcardAccountLookup{accountID: 7, found: true},
		&stubInventoryReader{},
		&stubCardInventoryChecker{has: true},
		&stubDraftRatingsMaxCacheChecker{},
		&stubMetaFreshnessChecker{lastUpdated: &stale},
		&stubGapQueryRunner{},
		&stubGapQueryRunner{},
	)
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for stale meta, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestWildcardRecommendations_NilMeta_Returns503 verifies that the handler
// returns 503 when GetMetaLastUpdated returns nil (no archetypes for format).
func TestWildcardRecommendations_NilMeta_Returns503(t *testing.T) {
	h := handlers.NewWildcardRecommendationsHandler(
		&wildcardAccountLookup{accountID: 7, found: true},
		&stubInventoryReader{},
		&stubCardInventoryChecker{has: true},
		&stubDraftRatingsMaxCacheChecker{},
		&stubMetaFreshnessChecker{lastUpdated: nil}, // nil = no archetypes
		&stubGapQueryRunner{},
		&stubGapQueryRunner{},
	)
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for nil meta, got %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestWildcardRecommendations_StaleRatings_StillReturns200 verifies that stale
// card ratings (>48h) produce a 200 with data_freshness.stale=true, not a 503.
// Stale GIHWR data is still useful per ADR-045 §5.
// Also verifies stale_reason is set to "card_ratings_older_than_48h" (M2 — ADR-045 §5).
func TestWildcardRecommendations_StaleRatings_StillReturns200(t *testing.T) {
	staleRatings := time.Now().Add(-72 * time.Hour) // 3 days ago
	h := handlers.NewWildcardRecommendationsHandler(
		&wildcardAccountLookup{accountID: 7, found: true},
		&stubInventoryReader{},
		&stubCardInventoryChecker{has: true},
		&stubDraftRatingsMaxCacheChecker{cachedAt: &staleRatings},
		&stubMetaFreshnessChecker{lastUpdated: recentMeta()},
		&stubGapQueryRunner{},
		&stubGapQueryRunner{},
	)
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 even with stale ratings, got %d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeWildcardEnvelope(t, rr.Body.Bytes())
	freshness, ok := resp["data_freshness"].(map[string]any)
	if !ok {
		t.Fatal("data_freshness missing")
	}
	stale, _ := freshness["stale"].(bool)
	if !stale {
		t.Error("expected data_freshness.stale=true for ratings older than 48h")
	}
	// M2: stale_reason must be set when stale.
	staleReason, _ := freshness["stale_reason"].(string)
	if staleReason != "card_ratings_older_than_48h" {
		t.Errorf("expected stale_reason=%q, got %q", "card_ratings_older_than_48h", staleReason)
	}
}

// TestWildcardRecommendations_FreshRatings_NoStaleReason verifies that
// stale_reason is absent (omitempty) when ratings are fresh (ADR-045 §5 M2).
func TestWildcardRecommendations_FreshRatings_NoStaleReason(t *testing.T) {
	freshRatings := time.Now().Add(-1 * time.Hour) // 1 hour ago — fresh
	h := handlers.NewWildcardRecommendationsHandler(
		&wildcardAccountLookup{accountID: 7, found: true},
		&stubInventoryReader{},
		&stubCardInventoryChecker{has: true},
		&stubDraftRatingsMaxCacheChecker{cachedAt: &freshRatings},
		&stubMetaFreshnessChecker{lastUpdated: recentMeta()},
		&stubGapQueryRunner{},
		&stubGapQueryRunner{},
	)
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 with fresh ratings, got %d body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeWildcardEnvelope(t, rr.Body.Bytes())
	freshness, ok := resp["data_freshness"].(map[string]any)
	if !ok {
		t.Fatal("data_freshness missing")
	}
	stale, _ := freshness["stale"].(bool)
	if stale {
		t.Error("expected data_freshness.stale=false for fresh ratings")
	}
	// stale_reason must be absent (omitempty) when not stale.
	if val, exists := freshness["stale_reason"]; exists {
		t.Errorf("stale_reason must be omitted when not stale, got %v", val)
	}
}

// TestWildcardRecommendations_WildcardBudgetPopulated verifies that the
// wildcard_budget in the response matches the values returned by the inventory
// reader.
func TestWildcardRecommendations_WildcardBudgetPopulated(t *testing.T) {
	h := handlers.NewWildcardRecommendationsHandler(
		&wildcardAccountLookup{accountID: 7, found: true},
		&stubInventoryReader{counts: repository.WildcardCounts{
			Common: 12, Uncommon: 8, Rare: 4, Mythic: 1,
		}},
		&stubCardInventoryChecker{has: true},
		&stubDraftRatingsMaxCacheChecker{},
		&stubMetaFreshnessChecker{lastUpdated: recentMeta()},
		&stubGapQueryRunner{},
		&stubGapQueryRunner{},
	)
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	resp := decodeWildcardEnvelope(t, rr.Body.Bytes())
	budget, ok := resp["wildcard_budget"].(map[string]any)
	if !ok {
		t.Fatal("wildcard_budget missing")
	}
	wantCommon := float64(12)
	if budget["common"] != wantCommon {
		t.Errorf("common: got %v want %v", budget["common"], wantCommon)
	}
	if budget["rare"] != float64(4) {
		t.Errorf("rare: got %v want 4", budget["rare"])
	}
}

// TestWildcardRecommendations_TierIsString verifies that the tier field in
// recommendation items is a string, not an integer (Ray's ruling).
func TestWildcardRecommendations_TierIsString(t *testing.T) {
	tier := "1"
	gihwr := 0.623
	rows := []repository.WildcardGapRow{
		{
			ArchetypeID:    1,
			ArchetypeName:  "Mono White Aggro",
			Format:         "Standard",
			Tier:           &tier,
			CopiesRequired: 4,
			CopiesOwned:    4,
			CopiesMissing:  0,
			Rarity:         "rare",
			ArenaID:        12345,
			CardName:       "Sunfall",
			GIHWR:          &gihwr,
		},
	}

	h := handlers.NewWildcardRecommendationsHandler(
		&wildcardAccountLookup{accountID: 7, found: true},
		&stubInventoryReader{},
		&stubCardInventoryChecker{has: true},
		&stubDraftRatingsMaxCacheChecker{},
		&stubMetaFreshnessChecker{lastUpdated: recentMeta()},
		&stubGapQueryRunner{rows: rows},
		&stubGapQueryRunner{},
	)
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeWildcardEnvelope(t, rr.Body.Bytes())
	recs := resp["recommendations"].([]any)
	if len(recs) == 0 {
		t.Fatal("expected at least one recommendation")
	}
	rec := recs[0].(map[string]any)

	// tier must be a string.
	tierVal, ok := rec["tier"].(string)
	if !ok {
		t.Fatalf("tier must be a string, got %T: %v", rec["tier"], rec["tier"])
	}
	if tierVal != "1" {
		t.Errorf("tier: got %q want %q", tierVal, "1")
	}

	// tier_score must be a float64.
	if _, ok := rec["tier_score"].(float64); !ok {
		t.Fatalf("tier_score must be float64, got %T: %v", rec["tier_score"], rec["tier_score"])
	}
}

// TestWildcardRecommendations_RecommendationsNeverNull verifies that the
// recommendations field is always a JSON array, never null, even when empty.
func TestWildcardRecommendations_RecommendationsNeverNull(t *testing.T) {
	h := newWildcardHandler(&wildcardAccountLookup{accountID: 7, found: true})
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)

	// Unmarshal raw body to detect a literal null.
	var raw struct {
		Data struct {
			Recommendations json.RawMessage `json:"recommendations"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(raw.Data.Recommendations) == "null" {
		t.Error("recommendations must be [] not null")
	}
}

// TestGIHWRFractionalGuard verifies that the ranking formula treats gihwr as a
// fractional value (0.0–1.0) and NOT as a percentage (0–100). A value like
// 0.623 must produce a gihwr_percentile in [0.0, 1.0], not 62.3.
// This is the #787 class of bug — see pkg/draftalgo for the original fix.
func TestGIHWRFractionalGuard(t *testing.T) {
	tier := "1"
	// Fractional GIHWR values: 0.623 and 0.550.
	gihwr1 := 0.623
	gihwr2 := 0.550

	rows := []repository.WildcardGapRow{
		{
			ArchetypeID: 1, ArchetypeName: "Mono White",
			Format: "Standard", Tier: &tier,
			CopiesRequired: 4, CopiesOwned: 4, CopiesMissing: 0,
			Rarity: "rare", ArenaID: 100, CardName: "Card A", GIHWR: &gihwr1,
		},
		{
			ArchetypeID: 2, ArchetypeName: "Mono Red",
			Format: "Standard", Tier: &tier,
			CopiesRequired: 4, CopiesOwned: 4, CopiesMissing: 0,
			Rarity: "rare", ArenaID: 200, CardName: "Card B", GIHWR: &gihwr2,
		},
	}

	h := handlers.NewWildcardRecommendationsHandler(
		&wildcardAccountLookup{accountID: 7, found: true},
		&stubInventoryReader{},
		&stubCardInventoryChecker{has: true},
		&stubDraftRatingsMaxCacheChecker{},
		&stubMetaFreshnessChecker{lastUpdated: recentMeta()},
		&stubGapQueryRunner{rows: rows},
		&stubGapQueryRunner{},
	)
	req := authedWildcardRequest(t, http.MethodGet, "/api/v1/recommendations/wildcards", 42)
	rr := httptest.NewRecorder()
	h.GetWildcardRecommendations(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	resp := decodeWildcardEnvelope(t, rr.Body.Bytes())
	recs := resp["recommendations"].([]any)
	if len(recs) < 2 {
		t.Fatalf("expected 2 recommendations, got %d", len(recs))
	}

	// Verify the higher-GIHWR archetype ranked first.
	first := recs[0].(map[string]any)
	if first["archetype_name"] != "Mono White" {
		t.Errorf("expected Mono White (gihwr=0.623) to rank above Mono Red (gihwr=0.550), got %q first", first["archetype_name"])
	}

	// Verify the tier_score is a valid float between 0 and 1, not >1 (would
	// indicate gihwr was treated as percentage and leaked into the score).
	tierScore := first["tier_score"].(float64)
	if tierScore > 1.0 || tierScore < 0.0 {
		t.Errorf("tier_score out of [0,1] range: %v (indicates *100 bug)", tierScore)
	}
}
