package localapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/daemon/internal/draftstate"
	"github.com/RdHamilton/vault-mtg/services/daemon/internal/localapi"
	"github.com/RdHamilton/vault-mtg/services/daemon/internal/logreader"
)

// Ensure json is referenced (used in TestCurrentPack_ContractSnakeCaseFields).
var _ = json.Marshal

// stubCards / stubRatings satisfy draftalgo.CardLookup / RatingsLookup.
// Test-injected lookups make the assertions deterministic.
type stubCards map[string]string

func (s stubCards) CardName(id string) string { return s[id] }

type stubRatings map[string]float64

func (s stubRatings) GIHWR(id, _ string) (float64, bool) {
	v, ok := s[id]
	return v, ok
}

// newDraftTestServer wires a localapi server + draftstate Store + the
// test-supplied lookups. Caller is responsible for Stop().
func newDraftTestServer(t *testing.T, prep func(*draftstate.Store)) *localapi.Server {
	t.Helper()
	srv := localapi.New(0, localapi.State{Version: "test", StartedAt: time.Now()})
	store := draftstate.New()
	if prep != nil {
		prep(store)
	}
	srv.SetDraftStore(store)
	if err := srv.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv
}

func mustDecode[T any](t *testing.T, body io.Reader) T {
	t.Helper()
	var out T
	if err := json.NewDecoder(body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

// ─── current-pack ──────────────────────────────────────────────────────────

func TestCurrentPack_NoSessionReturns404(t *testing.T) {
	srv := newDraftTestServer(t, nil)

	resp, err := http.Get("http://" + srv.Addr() + "/api/v1/drafts/anything/current-pack")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestCurrentPack_ReturnsLiveSession(t *testing.T) {
	srv := newDraftTestServer(t, func(store *draftstate.Store) {
		store.HandlePack(&logreader.DraftPackPayload{
			CourseName: "PremierDraft_BLB",
			DraftPack:  logreader.DraftPackDetail{PackCards: []int{100, 200, 300}, SelfPick: 1},
		})
	})
	srv.SetDraftLookups(
		stubCards{"100": "Card A", "200": "Card B", "300": "Card C"},
		stubRatings{"100": 60.0, "200": 50.0, "300": 45.0},
	)

	resp, err := http.Get("http://" + srv.Addr() + "/api/v1/drafts/current/current-pack")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	// Decode using the snake_case SPA contract (gui.CurrentPackResponse).
	type card struct {
		ArenaID       string  `json:"arena_id"`
		Name          string  `json:"name"`
		GIHWR         float64 `json:"gihwr"`
		IsRecommended bool    `json:"is_recommended"`
		Score         float64 `json:"score"`
		Reasoning     string  `json:"reasoning"`
	}
	type recCard struct {
		ArenaID   string  `json:"arena_id"`
		Name      string  `json:"name"`
		Reasoning string  `json:"reasoning"`
		Score     float64 `json:"score"`
	}
	body := mustDecode[struct {
		SessionID       string   `json:"session_id"`
		PackNumber      int      `json:"pack_number"`
		PickNumber      int      `json:"pick_number"`
		PackLabel       string   `json:"pack_label"`
		Cards           []card   `json:"cards"`
		RecommendedCard *recCard `json:"recommended_card"`
		PoolSize        int      `json:"pool_size"`
	}](t, resp.Body)

	if body.PackNumber != 1 || body.PickNumber != 1 {
		t.Errorf("pack_number/pick_number = %d/%d, want 1/1 (1-based)", body.PackNumber, body.PickNumber)
	}
	if body.PackLabel != "Pack 1, Pick 1" {
		t.Errorf("pack_label = %q, want %q", body.PackLabel, "Pack 1, Pick 1")
	}
	if len(body.Cards) != 3 {
		t.Fatalf("Cards len = %d, want 3", len(body.Cards))
	}
	// Card A has highest GIHWR (60.0) so it must be recommended.
	// The response preserves the original pack card order, so verify by
	// scanning for arena_id "100".
	foundA := false
	for _, c := range body.Cards {
		if c.ArenaID == "100" {
			if c.Name != "Card A" {
				t.Errorf("card 100 Name = %q, want %q", c.Name, "Card A")
			}
			if c.GIHWR != 60.0 {
				t.Errorf("card 100 GIHWR = %v, want 60.0", c.GIHWR)
			}
			if !c.IsRecommended {
				t.Errorf("card 100 (Card A, highest GIHWR) must have is_recommended=true")
			}
			if c.Reasoning == "" {
				t.Errorf("card 100 Reasoning must not be empty")
			}
			foundA = true
		}
	}
	if !foundA {
		t.Errorf("card arena_id=100 not found in response")
	}
}

// TestCurrentPack_RecommendedCardPopulated verifies the top-level
// recommended_card field is set and matches the highest-GIHWR card.
func TestCurrentPack_RecommendedCardPopulated(t *testing.T) {
	srv := newDraftTestServer(t, func(store *draftstate.Store) {
		store.HandlePack(&logreader.DraftPackPayload{
			CourseName: "PremierDraft_BLB",
			DraftPack:  logreader.DraftPackDetail{PackCards: []int{100, 200, 300}, SelfPick: 1},
		})
	})
	srv.SetDraftLookups(
		stubCards{"100": "Lightning Bolt", "200": "Bear", "300": "Elf"},
		stubRatings{"100": 72.0, "200": 55.0, "300": 48.0},
	)

	resp, err := http.Get("http://" + srv.Addr() + "/api/v1/drafts/current/current-pack")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	type recCard struct {
		ArenaID       string  `json:"arena_id"`
		Name          string  `json:"name"`
		IsRecommended bool    `json:"is_recommended"`
		Score         float64 `json:"score"`
		Reasoning     string  `json:"reasoning"`
	}
	body := mustDecode[struct {
		RecommendedCard *recCard `json:"recommended_card"`
	}](t, resp.Body)

	if body.RecommendedCard == nil {
		t.Fatal("recommended_card must not be nil when pack has rated cards")
	}
	if body.RecommendedCard.ArenaID != "100" {
		t.Errorf("recommended_card.arena_id = %q, want %q (highest GIHWR card)", body.RecommendedCard.ArenaID, "100")
	}
	if body.RecommendedCard.Name != "Lightning Bolt" {
		t.Errorf("recommended_card.name = %q, want %q", body.RecommendedCard.Name, "Lightning Bolt")
	}
	if body.RecommendedCard.Reasoning == "" {
		t.Errorf("recommended_card.reasoning must not be empty")
	}
}

// TestCurrentPack_NoRatingsGracefulDegrade verifies that when no ratings
// are available for the current set/format, the response still populates
// cards with N/A reasoning rather than erroring out.
func TestCurrentPack_NoRatingsGracefulDegrade(t *testing.T) {
	srv := newDraftTestServer(t, func(store *draftstate.Store) {
		store.HandlePack(&logreader.DraftPackPayload{
			CourseName: "PremierDraft_NEW",
			DraftPack:  logreader.DraftPackDetail{PackCards: []int{1, 2, 3}, SelfPick: 1},
		})
	})
	// No ratings injected — daemon falls back to noopRatings.

	resp, err := http.Get("http://" + srv.Addr() + "/api/v1/drafts/current/current-pack")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (graceful degrade)", resp.StatusCode)
	}

	type card struct {
		Reasoning string `json:"reasoning"`
	}
	body := mustDecode[struct {
		Cards []card `json:"cards"`
	}](t, resp.Body)

	if len(body.Cards) == 0 {
		t.Fatal("expected cards even with no ratings")
	}
	for i, c := range body.Cards {
		if c.Reasoning == "" {
			t.Errorf("cards[%d].reasoning is empty — must have N/A placeholder", i)
		}
	}
}

// TestCurrentPack_PoolSizeReflectsPicks verifies that pool_size reflects
// the number of previously picked cards in the session.
func TestCurrentPack_PoolSizeReflectsPicks(t *testing.T) {
	srv := newDraftTestServer(t, func(store *draftstate.Store) {
		// First pack — player picks card 100.
		store.HandlePack(&logreader.DraftPackPayload{
			CourseName: "PremierDraft_BLB",
			DraftPack:  logreader.DraftPackDetail{PackCards: []int{100, 200, 300}, SelfPick: 1},
		})
		store.HandlePick(&logreader.DraftPickPayload{
			CourseName: "PremierDraft_BLB", PickedCards: []int{100},
			PackNumber: 0, PickNumber: 0,
		})
		// Second pack — player is now looking at it (pool size = 1).
		store.HandlePack(&logreader.DraftPackPayload{
			CourseName: "PremierDraft_BLB",
			DraftPack:  logreader.DraftPackDetail{PackCards: []int{400, 500}, SelfPick: 2},
		})
	})

	resp, err := http.Get("http://" + srv.Addr() + "/api/v1/drafts/current/current-pack")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body := mustDecode[struct {
		PoolSize int `json:"pool_size"`
	}](t, resp.Body)

	if body.PoolSize != 1 {
		t.Errorf("pool_size = %d, want 1 (one pick made before this pack)", body.PoolSize)
	}
}

// TestCurrentPack_ContractSnakeCaseFields verifies that the JSON response
// uses snake_case keys matching the SPA's gui.CurrentPackResponse type.
// This is the cross-component contract audit required before the PR.
func TestCurrentPack_ContractSnakeCaseFields(t *testing.T) {
	srv := newDraftTestServer(t, func(store *draftstate.Store) {
		store.HandlePack(&logreader.DraftPackPayload{
			CourseName: "PremierDraft_BLB",
			DraftPack:  logreader.DraftPackDetail{PackCards: []int{10}, SelfPick: 1},
		})
	})

	resp, err := http.Get("http://" + srv.Addr() + "/api/v1/drafts/current/current-pack")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Decode into a raw map so we can check key names directly.
	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Top-level snake_case fields required by gui.CurrentPackResponse.
	for _, key := range []string{"session_id", "pack_number", "pick_number", "pack_label", "cards", "pool_colors", "pool_size"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("response missing required snake_case field %q", key)
		}
	}
	// Per-card snake_case fields required by gui.PackCardWithRating.
	cards, ok := raw["cards"].([]interface{})
	if !ok || len(cards) == 0 {
		t.Fatal("cards must be a non-empty array")
	}
	card, ok := cards[0].(map[string]interface{})
	if !ok {
		t.Fatal("cards[0] must be an object")
	}
	for _, key := range []string{"arena_id", "name", "gihwr", "is_recommended", "score", "reasoning"} {
		if _, ok := card[key]; !ok {
			t.Errorf("cards[0] missing required snake_case field %q", key)
		}
	}
	// Must NOT have the old camelCase keys.
	for _, key := range []string{"arenaId", "cardName", "sessionId", "packNumber", "pickNumber"} {
		if _, ok := raw[key]; ok {
			t.Errorf("response contains legacy camelCase field %q — must use snake_case", key)
		}
	}
}

func TestCurrentPack_404OnMalformedPath(t *testing.T) {
	srv := newDraftTestServer(t, nil)
	resp, err := http.Get("http://" + srv.Addr() + "/api/v1/drafts/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// ─── grade-pick ────────────────────────────────────────────────────────────

func TestGradePick_ExplicitAvailableCards(t *testing.T) {
	srv := newDraftTestServer(t, nil)
	srv.SetDraftLookups(
		stubCards{"100": "A", "200": "B", "300": "C"},
		stubRatings{"100": 60.0, "200": 55.0, "300": 50.0},
	)

	body := map[string]any{
		"session_id":         "s",
		"picked_card_id":     100,
		"available_card_ids": []int{100, 200, 300},
		"pick_number":        1,
	}
	b, _ := json.Marshal(body)

	resp, err := http.Post(
		"http://"+srv.Addr()+"/api/v1/drafts/grade-pick",
		"application/json", bytes.NewReader(b),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got := mustDecode[struct {
		Grade         string  `json:"grade"`
		Rank          int     `json:"rank"`
		PackBestGIHWR float64 `json:"pack_best_gihwr"`
	}](t, resp.Body)
	if got.Grade != "A+" || got.Rank != 1 {
		t.Errorf("expected A+ rank 1 (picked the best card), got %+v", got)
	}
	if got.PackBestGIHWR != 60.0 {
		t.Errorf("PackBestGIHWR = %v, want 60.0", got.PackBestGIHWR)
	}
}

func TestGradePick_BadBodyReturns400(t *testing.T) {
	srv := newDraftTestServer(t, nil)
	resp, err := http.Post(
		"http://"+srv.Addr()+"/api/v1/drafts/grade-pick",
		"application/json", bytes.NewReader([]byte("not json")),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestGradePick_RejectsNonPOST(t *testing.T) {
	srv := newDraftTestServer(t, nil)
	resp, err := http.Get("http://" + srv.Addr() + "/api/v1/drafts/grade-pick")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// ─── win-probability ───────────────────────────────────────────────────────

func TestWinProbability_NoSessionDefaultsToBaseline(t *testing.T) {
	srv := newDraftTestServer(t, nil)

	body, _ := json.Marshal(map[string]string{"session_id": "anything"})
	resp, err := http.Post(
		"http://"+srv.Addr()+"/api/v1/drafts/win-probability",
		"application/json", bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got := mustDecode[struct {
		Probability float64 `json:"probability"`
	}](t, resp.Body)
	if got.Probability != 0.50 {
		t.Errorf("Probability = %v, want 0.50 (baseline)", got.Probability)
	}
}

func TestWinProbability_ComputesFromSession(t *testing.T) {
	srv := newDraftTestServer(t, func(store *draftstate.Store) {
		// Seed a session with one recorded pick so the predictor has
		// something to chew on (even with no ratings; uses defaults).
		store.HandlePack(&logreader.DraftPackPayload{
			CourseName: "PremierDraft_BLB",
			DraftPack:  logreader.DraftPackDetail{PackCards: []int{1, 2, 3}, SelfPick: 1},
		})
		store.HandlePick(&logreader.DraftPickPayload{
			CourseName: "PremierDraft_BLB", PickedCards: []int{1},
			PackNumber: 0, PickNumber: 0,
		})
	})
	srv.SetDraftLookups(
		stubCards{"1": "Card"},
		stubRatings{"1": 50.0},
	)

	body, _ := json.Marshal(map[string]string{"session_id": "current"})
	resp, err := http.Post(
		"http://"+srv.Addr()+"/api/v1/drafts/win-probability",
		"application/json", bytes.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	got := mustDecode[struct {
		Probability float64 `json:"probability"`
	}](t, resp.Body)
	// Predictor clamps to [0.30, 0.70]. We don't pin a specific value
	// because the heuristic touches several knobs — assert range only.
	if got.Probability < 0.30 || got.Probability > 0.70 {
		t.Errorf("Probability = %v, want in [0.30, 0.70]", got.Probability)
	}
}

func TestWinProbability_RejectsNonPOST(t *testing.T) {
	srv := newDraftTestServer(t, nil)
	resp, err := http.Get("http://" + srv.Addr() + "/api/v1/drafts/win-probability")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d", resp.StatusCode)
	}
}
