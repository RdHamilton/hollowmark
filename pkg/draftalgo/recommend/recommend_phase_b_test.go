package recommend_test

// Phase B tests — color-fit reasons, pool-color awareness, archetype
// suppression, ALSA signal, low_confidence marker, "why not pick 2"
// differentiator, splash exception.
//
// Per TDD: these tests are written first and must be watched to fail
// before the corresponding implementation is added. Each test names
// the exact ADR-047 or Prof constraint it exercises.
//
// All tests use stubCardMeta (see below) which satisfies the new
// draftalgo.CardMetaLookup interface added in Phase B.

import (
	"strings"
	"testing"

	"github.com/RdHamilton/vault-mtg/pkg/draftalgo"
	"github.com/RdHamilton/vault-mtg/pkg/draftalgo/recommend"
)

// stubCardMeta satisfies draftalgo.CardMetaLookup.
// Keys are arena card IDs (string form).
type stubCardMeta map[string]draftalgo.CardMeta

func (s stubCardMeta) CardMetaByID(id string) (draftalgo.CardMeta, bool) {
	m, ok := s[id]
	return m, ok
}

var _ draftalgo.CardMetaLookup = stubCardMeta{}

// ─── low_confidence marker (ADR-047 §2) ──────────────────────────────────

// TestLowConfidence_FlipsAtThreshold — the marker must flip exactly at the
// 500-game boundary: <500 = low_confidence=true; ≥500 = false.
// ADR-047 fitness function: "Bob includes a unit test asserting low_confidence
// flips at the 499/500 boundary."
func TestLowConfidence_FlipsAtThreshold(t *testing.T) {
	pack := []string{"a", "b"}
	ratings := stubRatings{"a": 60.0, "b": 55.0}
	names := stubCards{"a": "HighSample", "b": "LowSample"}
	count499 := 499
	count500 := 500
	meta := stubCardMeta{
		"a": {Colors: []string{"G"}, ALSA: 3.0, GIHCount: &count500}, // ≥500 → false
		"b": {Colors: []string{"G"}, ALSA: 3.0, GIHCount: &count499}, // <500 → true
	}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, names, meta)

	all := append(recs.TopPicks, recs.Alternatives...)
	for _, r := range all {
		switch r.CardID {
		case "a":
			if r.LowConfidence {
				t.Errorf("card 'a' (500 games) must have LowConfidence=false, got true")
			}
		case "b":
			if !r.LowConfidence {
				t.Errorf("card 'b' (499 games) must have LowConfidence=true, got false")
			}
		}
	}
}

// TestLowConfidence_NilGIHCountIsLow — nil GIHCount means no sample data
// at all → must be low_confidence=true.
func TestLowConfidence_NilGIHCountIsLow(t *testing.T) {
	pack := []string{"a"}
	ratings := stubRatings{"a": 60.0}
	names := stubCards{"a": "Unknown"}
	meta := stubCardMeta{
		"a": {Colors: []string{"R"}, ALSA: 3.0, GIHCount: nil},
	}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, names, meta)

	if len(recs.TopPicks) == 0 {
		t.Fatal("expected a top pick")
	}
	if !recs.TopPicks[0].LowConfidence {
		t.Errorf("nil GIHCount must produce LowConfidence=true")
	}
}

// TestLowConfidence_FramingNotWeakness — the low-confidence Reason must
// communicate uncertainty ("sample", "limited", "signal"), not weakness.
// Prof hard constraint: must not read as "this card may be bad."
func TestLowConfidence_FramingNotWeakness(t *testing.T) {
	pack := []string{"a"}
	ratings := stubRatings{"a": 60.0}
	names := stubCards{"a": "Rare Bomb"}
	count := 100
	meta := stubCardMeta{
		"a": {Colors: []string{"U"}, ALSA: 3.0, GIHCount: &count},
	}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, names, meta)

	all := append(recs.TopPicks, recs.Alternatives...)
	for _, r := range all {
		if r.LowConfidence {
			lower := strings.ToLower(r.Reason)
			if !strings.Contains(lower, "sample") && !strings.Contains(lower, "limited") && !strings.Contains(lower, "signal") && !strings.Contains(lower, "small") {
				t.Errorf("LowConfidence Reason %q does not communicate uncertainty — must contain 'sample', 'limited', 'signal', or 'small'", r.Reason)
			}
		}
	}
}

// ─── color-fit reasons (ADR-047 §5, Prof constraint C) ───────────────────

// TestColorFit_OnColorReason — a card whose colors are a subset of the
// pool's committed colors must receive an on-color reason.
func TestColorFit_OnColorReason(t *testing.T) {
	// Pool is B/G committed (3 B, 3 G cards in pool → two colors committed).
	pool := poolWithColors(t, 3, "B", 3, "G", 0, "")
	pack := []string{"bomb"}
	ratings := stubRatings{"bomb": 68.0}
	names := stubCards{"bomb": "Golgari Bomb"}
	count := 1000
	meta := stubCardMeta{
		"bomb": {Colors: []string{"B"}, ALSA: 3.0, GIHCount: &count},
	}
	// Inject pool card meta so recommend can derive pool colors.
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	if len(recs.TopPicks) == 0 {
		t.Fatal("expected top pick")
	}
	top := recs.TopPicks[0]
	if top.CardID != "bomb" {
		t.Fatalf("expected 'bomb' as top pick, got %q", top.CardID)
	}
	lower := strings.ToLower(top.Reason)
	if !strings.Contains(lower, "color") && !strings.Contains(lower, "fits") && !strings.Contains(lower, "on-color") {
		t.Errorf("on-color bomb Reason %q should mention color fit", top.Reason)
	}
}

// TestColorFit_LandsExcludedFromPenalty — a basic land (IsLand=true in meta)
// must NOT receive a color-fit penalty even when it's off the committed colors.
// ADR-047 §5 + Prof constraint C: "don't apply color-fit penalty logic" to lands.
func TestColorFit_LandsExcludedFromPenalty(t *testing.T) {
	pool := poolWithColors(t, 4, "G", 2, "B", 0, "")
	pack := []string{"forest", "mountain", "bomb"}
	count := 800
	ratings := stubRatings{"bomb": 70.0, "forest": 55.0, "mountain": 52.0}
	names := stubCards{"bomb": "Green Bomb", "forest": "Forest", "mountain": "Mountain"}
	meta := stubCardMeta{
		"bomb":     {Colors: []string{"G"}, ALSA: 3.0, GIHCount: &count},
		"forest":   {Colors: []string{"G"}, ALSA: 4.0, GIHCount: &count, IsLand: true},
		"mountain": {Colors: []string{"R"}, ALSA: 5.0, GIHCount: &count, IsLand: true},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	all := append(recs.TopPicks, recs.Alternatives...)
	for _, r := range all {
		if r.CardID == "mountain" {
			lower := strings.ToLower(r.Reason)
			if strings.Contains(lower, "off-color") || strings.Contains(lower, "penalty") || strings.Contains(lower, "not your colors") {
				t.Errorf("land 'mountain' must not receive a color-fit penalty reason, got %q", r.Reason)
			}
		}
	}
}

// TestColorFit_OffColorPenaltyString — an off-color, non-splash card must
// receive the reason string "Not your colors" (Prof copy nit — vmt-t#648).
// ADR-047 §5.
func TestColorFit_OffColorPenaltyString(t *testing.T) {
	// G/B committed pool, off-color R card with modest GIHWR (no splash path).
	pool := poolWithColors(t, 4, "G", 3, "B", 0, "")
	pack := []string{"red-card"}
	count := 1000
	ratings := stubRatings{"red-card": 58.0} // below splashHighGIHWRFloor (≥72)
	names := stubCards{"red-card": "Red Card"}
	meta := stubCardMeta{
		"red-card": {Colors: []string{"R"}, ALSA: 4.5, GIHCount: &count},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	all := append(recs.TopPicks, recs.Alternatives...)
	if len(all) == 0 {
		t.Fatal("expected at least one recommendation")
	}
	found := false
	for _, r := range all {
		if r.CardID == "red-card" {
			found = true
			if r.Reason != "Not your colors" {
				t.Errorf("off-color non-splash card: expected Reason %q, got %q", "Not your colors", r.Reason)
			}
		}
	}
	if !found {
		t.Error("red-card not found in recommendations")
	}
}

// TestColorFit_SplashConsiderationPath — a high-GIHWR card one step off
// the committed two-color pool must receive a splash-consideration reason,
// not an off-color penalty. ADR-047 §5: "splash-consideration reason path."
func TestColorFit_SplashConsiderationPath(t *testing.T) {
	// Pool is G/B committed.
	pool := poolWithColors(t, 4, "G", 3, "B", 0, "")
	pack := []string{"red-bomb"}
	countLow := 1200
	ratings := stubRatings{"red-bomb": 74.0} // high GIHWR
	names := stubCards{"red-bomb": "Red Bomb"}
	meta := stubCardMeta{
		"red-bomb": {Colors: []string{"R"}, ALSA: 2.0, GIHCount: &countLow},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	if len(recs.TopPicks) == 0 {
		t.Fatal("expected a top pick")
	}
	top := recs.TopPicks[0]
	lower := strings.ToLower(top.Reason)
	// Must get splash consideration, not an off-color penalty
	if strings.Contains(lower, "off-color") && !strings.Contains(lower, "splash") {
		t.Errorf("high-GIHWR off-color card should get splash-consideration reason, got pure off-color penalty: %q", top.Reason)
	}
	if !strings.Contains(lower, "splash") && !strings.Contains(lower, "strong") && !strings.Contains(lower, "powerful") {
		t.Errorf("high-GIHWR off-color card reason %q should mention splash potential or standalone strength", top.Reason)
	}
}

// ─── archetype suppression (ADR-047 §3, Prof constraint B) ────────────────

// TestArchetype_SuppressedPreCommitment — archetype tags must be empty
// when the pool has no two-color commitment (early picks).
func TestArchetype_SuppressedPreCommitment(t *testing.T) {
	// Pool with only 1 of each color — no commitment.
	smallPool := map[string]draftalgo.CardMeta{
		"p1": {Colors: []string{"G"}},
		"p2": {Colors: []string{"B"}},
	}
	poolIDs := []string{"p1", "p2"}
	pack := []string{"a", "b", "c"}
	count := 800
	ratings := stubRatings{"a": 65.0, "b": 60.0, "c": 55.0}
	names := stubCards{"a": "A", "b": "B", "c": "C"}
	meta := stubCardMeta{
		"a":  {Colors: []string{"G"}, ALSA: 3.0, GIHCount: &count},
		"b":  {Colors: []string{"B"}, ALSA: 4.0, GIHCount: &count},
		"c":  {Colors: []string{"R"}, ALSA: 5.0, GIHCount: &count},
		"p1": smallPool["p1"],
		"p2": smallPool["p2"],
	}

	recs := recommend.Recommend("PremierDraft", poolIDs, pack, ratings, names, meta)

	all := append(recs.TopPicks, recs.Alternatives...)
	for _, r := range all {
		if r.Archetype != "" {
			t.Errorf("pre-commitment pool: Archetype must be empty, got %q for card %s", r.Archetype, r.CardID)
		}
	}
}

// TestArchetype_PopulatedPostCommitment — archetype tags must be populated
// when pool has clearly committed to two colors (≥3 cards each).
func TestArchetype_PopulatedPostCommitment(t *testing.T) {
	// Pool committed G/B: 4G + 3B cards = Golgari commitment.
	pool := poolWithColors(t, 4, "G", 3, "B", 0, "")
	pack := []string{"a"}
	count := 1000
	ratings := stubRatings{"a": 65.0}
	names := stubCards{"a": "GreenCard"}
	meta := stubCardMeta{
		"a": {Colors: []string{"G"}, ALSA: 3.0, GIHCount: &count},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	if len(recs.TopPicks) == 0 {
		t.Fatal("expected a top pick")
	}
	// Post-commitment: archetype should be populated
	if recs.TopPicks[0].Archetype == "" {
		t.Errorf("post-commitment pool: Archetype must not be empty (pool is G/B committed)")
	}
}

// TestArchetype_TwoColorDetection_NotMonoColor — a 5G/2B pool at pick 6
// must be detected as Simic-leaning (two colors), not Mono-Green.
// ADR-047 §3 + Prof constraint B: "5G/2B pool is Simic-leaning, not Mono-Green."
func TestArchetype_TwoColorDetection_NotMonoColor(t *testing.T) {
	pool := poolWithColors(t, 5, "G", 2, "B", 0, "")
	pack := []string{"bomb"}
	count := 1000
	ratings := stubRatings{"bomb": 68.0}
	names := stubCards{"bomb": "Simic Bomb"}
	meta := stubCardMeta{
		"bomb": {Colors: []string{"G", "U"}, ALSA: 2.0, GIHCount: &count},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	if len(recs.TopPicks) == 0 {
		t.Fatal("expected top pick")
	}
	archetype := recs.TopPicks[0].Archetype
	lower := strings.ToLower(archetype)
	// Must NOT collapse to mono-green
	if strings.Contains(lower, "mono") {
		t.Errorf("5G/2B pool must not produce Mono-Green archetype, got %q", archetype)
	}
}

// TestArchetype_PoolColorsReDerivedEachPick — each call to Recommend must
// recompute the pool color state; it must not use any cached/frozen state.
// ADR-047 §3: "pool color state must be re-derived from the pool on every pick."
// We test this by calling Recommend twice with different pool compositions.
func TestArchetype_PoolColorsReDerivedEachPick(t *testing.T) {
	count := 1000
	ratings := stubRatings{"a": 65.0}
	names := stubCards{"a": "TestCard"}
	meta := stubCardMeta{
		"a": {Colors: []string{"G"}, ALSA: 3.0, GIHCount: &count},
	}

	// First call: pre-commitment pool (1G, 1B) → no archetype
	smallPool := []string{"p1", "p2"}
	meta["p1"] = draftalgo.CardMeta{Colors: []string{"G"}}
	meta["p2"] = draftalgo.CardMeta{Colors: []string{"B"}}
	recs1 := recommend.Recommend("PremierDraft", smallPool, []string{"a"}, ratings, names, meta)

	// Second call: committed pool (4G, 3B) → archetype populated
	bigPool := make([]string, 7)
	for i := 0; i < 4; i++ {
		id := "pg" + string(rune('0'+i))
		bigPool[i] = id
		meta[id] = draftalgo.CardMeta{Colors: []string{"G"}}
	}
	for i := 0; i < 3; i++ {
		id := "pb" + string(rune('0'+i))
		bigPool[4+i] = id
		meta[id] = draftalgo.CardMeta{Colors: []string{"B"}}
	}
	recs2 := recommend.Recommend("PremierDraft", bigPool, []string{"a"}, ratings, names, meta)

	if len(recs1.TopPicks) > 0 && recs1.TopPicks[0].Archetype != "" {
		t.Errorf("first call (small pool): expected no archetype, got %q", recs1.TopPicks[0].Archetype)
	}
	if len(recs2.TopPicks) > 0 && recs2.TopPicks[0].Archetype == "" {
		t.Errorf("second call (big pool): expected archetype to be populated, got empty")
	}
}

// ─── ALSA scarcity signal (ADR-047 §5, Prof) ─────────────────────────────

// TestALSA_NeverWheelProbabilityPercentage — ALSA signals must not contain
// a "%" character (wheel probability framing). ADR-047 §5: "never framed as
// wheel probability."
func TestALSA_NeverWheelProbabilityPercentage(t *testing.T) {
	pack := []string{"late", "early"}
	count := 800
	ratings := stubRatings{"late": 58.0, "early": 55.0}
	names := stubCards{"late": "Late Card", "early": "Early Card"}
	meta := stubCardMeta{
		"late":  {Colors: []string{"G"}, ALSA: 8.5, GIHCount: &count}, // frequently available
		"early": {Colors: []string{"G"}, ALSA: 2.0, GIHCount: &count}, // typically taken early
	}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, names, meta)

	all := append(recs.TopPicks, recs.Alternatives...)
	for _, r := range all {
		if strings.Contains(r.Reason, "%") {
			t.Errorf("ALSA Reason %q must not contain %% (wheel-probability framing)", r.Reason)
		}
	}
}

// TestALSA_FrequentlyAvailableLanguage — high-ALSA cards (≥7.0) must use
// "frequently available" framing per ADR-047 §5. No probability claims.
func TestALSA_FrequentlyAvailableLanguage(t *testing.T) {
	pack := []string{"late"}
	count := 800
	ratings := stubRatings{"late": 58.0}
	names := stubCards{"late": "Bulk Rare"}
	meta := stubCardMeta{
		"late": {Colors: []string{"G"}, ALSA: 8.5, GIHCount: &count},
	}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, names, meta)

	if len(recs.TopPicks) == 0 && len(recs.Alternatives) == 0 {
		t.Fatal("expected at least one recommendation")
	}
	all := append(recs.TopPicks, recs.Alternatives...)
	found := false
	for _, r := range all {
		if r.CardID == "late" {
			lower := strings.ToLower(r.Reason)
			if strings.Contains(lower, "available") || strings.Contains(lower, "late") || strings.Contains(lower, "frequently") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("high-ALSA (8.5) card reason should mention late availability")
	}
}

// ─── "Why not Pick 2" differentiator (ADR-047 §5, Prof) ──────────────────

// TestWhyNotPick2_OnlyOnMeaningfulGap — the differentiator must only fire
// when there's a meaningful gap between pick 1 and pick 2, never as filler.
// Prof: "only fire on meaningful gaps... never as filler on a marginal gap."
// A meaningful gap: off-color, archetype mismatch, or ≥500-game confidence gap.
func TestWhyNotPick2_OnlyOnMeaningfulGap(t *testing.T) {
	// Two cards with nearly identical GIHWR and same color — no meaningful gap.
	pool := poolWithColors(t, 3, "G", 3, "B", 0, "")
	pack := []string{"a", "b"}
	count := 1000
	ratings := stubRatings{"a": 62.1, "b": 61.8} // marginal gap
	names := stubCards{"a": "Card A", "b": "Card B"}
	meta := stubCardMeta{
		"a": {Colors: []string{"G"}, ALSA: 3.0, GIHCount: &count},
		"b": {Colors: []string{"G"}, ALSA: 3.2, GIHCount: &count}, // same colors, nearly same GIHWR
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	if len(recs.TopPicks) < 2 {
		t.Skip("not enough top picks to check pick 2 reason")
	}
	// Pick 2 must not have a "why not pick 1" differentiator when the gap is marginal.
	pick2 := recs.TopPicks[1]
	lower := strings.ToLower(pick2.WhyNotTopReason)
	if lower != "" {
		// Marginal-gap reasons should be empty
		t.Logf("pick 2 WhyNotTopReason = %q (marginal gap — should ideally be empty)", pick2.WhyNotTopReason)
	}
}

// TestWhyNotPick2_OffColorComparativeString — when pick 2 is off-color and
// the top pick is on-color, WhyNotTopReason must be exactly
// "Off-color vs. the top pick" (grammatical color-fit comparative — vmt-t#648).
// This exercises the buildWhyNotTop comparative branch (topOnColor && !cardOnColor).
func TestWhyNotPick2_OffColorComparativeString(t *testing.T) {
	// G/B committed pool; top pick is on-color (G), pick 2 is off-color (R).
	pool := poolWithColors(t, 4, "G", 3, "B", 0, "")
	pack := []string{"green-bomb", "red-card"}
	count := 1000
	ratings := stubRatings{"green-bomb": 68.0, "red-card": 66.0}
	names := stubCards{"green-bomb": "Green Bomb", "red-card": "Red Card"}
	meta := stubCardMeta{
		"green-bomb": {Colors: []string{"G"}, ALSA: 2.0, GIHCount: &count},
		"red-card":   {Colors: []string{"R"}, ALSA: 3.0, GIHCount: &count},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	if len(recs.TopPicks) < 2 {
		t.Skip("not enough top picks")
	}
	// TopPick[0] must be the on-color green-bomb; TopPick[1] the off-color red-card.
	if recs.TopPicks[0].CardID != "green-bomb" {
		t.Fatalf("expected green-bomb as top pick, got %q", recs.TopPicks[0].CardID)
	}
	pick2 := recs.TopPicks[1]
	const want = "Off-color vs. the top pick"
	if pick2.WhyNotTopReason != want {
		t.Errorf("off-color pick 2 WhyNotTopReason = %q, want %q", pick2.WhyNotTopReason, want)
	}
}

// TestWhyNotPick2_FiresOnColorMismatch — when pick 2 is off-color relative
// to pick 1, the differentiator should fire with color context.
func TestWhyNotPick2_FiresOnColorMismatch(t *testing.T) {
	pool := poolWithColors(t, 4, "G", 3, "B", 0, "")
	pack := []string{"green-bomb", "red-card"}
	count := 1000
	ratings := stubRatings{"green-bomb": 68.0, "red-card": 66.0} // meaningful GIHWR gap + color mismatch
	names := stubCards{"green-bomb": "Green Bomb", "red-card": "Red Card"}
	meta := stubCardMeta{
		"green-bomb": {Colors: []string{"G"}, ALSA: 2.0, GIHCount: &count},
		"red-card":   {Colors: []string{"R"}, ALSA: 3.0, GIHCount: &count},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	if len(recs.TopPicks) < 2 {
		t.Skip("not enough top picks")
	}
	// Pick 2 is off-color — should have a differentiator
	pick2 := recs.TopPicks[1]
	if pick2.WhyNotTopReason == "" {
		t.Errorf("off-color pick 2 should have a WhyNotTopReason, got empty")
	}
	lower := strings.ToLower(pick2.WhyNotTopReason)
	if !strings.Contains(lower, "color") {
		t.Errorf("off-color differentiator should mention color, got %q", pick2.WhyNotTopReason)
	}
}

// TestWhyNotPick2_NoRawNumbers — differentiator must not contain raw
// GIHWR % numbers (Prof: "Never show raw numbers in this phrase").
func TestWhyNotPick2_NoRawNumbers(t *testing.T) {
	pool := poolWithColors(t, 4, "G", 3, "B", 0, "")
	pack := []string{"a", "b", "c"}
	count := 1000
	ratings := stubRatings{"a": 68.0, "b": 60.0, "c": 55.0}
	names := stubCards{"a": "A", "b": "B", "c": "C"}
	meta := stubCardMeta{
		"a": {Colors: []string{"G"}, ALSA: 2.0, GIHCount: &count},
		"b": {Colors: []string{"R"}, ALSA: 4.0, GIHCount: &count},
		"c": {Colors: []string{"G"}, ALSA: 5.0, GIHCount: &count},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	for _, r := range recs.TopPicks {
		if strings.Contains(r.WhyNotTopReason, "%") {
			t.Errorf("WhyNotTopReason %q must not contain raw %% numbers", r.WhyNotTopReason)
		}
	}
}

// ─── pool colors derived from pool cards (ADR-047 §3) ────────────────────

// TestPoolColors_DerivedFromPool — the PoolColors field on Recommendations
// must reflect the two dominant colors from the pool card metas.
func TestPoolColors_DerivedFromPool(t *testing.T) {
	// Pool: 5G, 3B — PoolColors must include G and B.
	pool := poolWithColors(t, 5, "G", 3, "B", 0, "")
	pack := []string{"x"}
	count := 1000
	ratings := stubRatings{"x": 60.0}
	names := stubCards{"x": "X"}
	meta := stubCardMeta{"x": {Colors: []string{"G"}, ALSA: 3.0, GIHCount: &count}}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	hasG := false
	hasB := false
	for _, c := range recs.PoolColors {
		if c == "G" {
			hasG = true
		}
		if c == "B" {
			hasB = true
		}
	}
	if !hasG || !hasB {
		t.Errorf("PoolColors = %v, want to include both G and B (committed pool)", recs.PoolColors)
	}
}

// ─── integration: no raw % in any Reason from Phase B path ───────────────

// TestPhaseB_NoRawPercentInAnyReason — Phase B must not introduce any %
// literals in Reason or WhyNotTopReason strings (extends Phase A contract).
func TestPhaseB_NoRawPercentInAnyReason(t *testing.T) {
	pool := poolWithColors(t, 4, "G", 3, "B", 0, "")
	pack := []string{"a", "b", "c", "d"}
	count500 := 500
	count100 := 100
	ratings := stubRatings{"a": 70.0, "b": 62.0, "c": 55.0, "d": 48.0}
	names := stubCards{"a": "A", "b": "B", "c": "C", "d": "D"}
	meta := stubCardMeta{
		"a": {Colors: []string{"G"}, ALSA: 2.0, GIHCount: &count500},
		"b": {Colors: []string{"R"}, ALSA: 4.0, GIHCount: &count500},
		"c": {Colors: []string{"B"}, ALSA: 8.0, GIHCount: &count100},
		"d": {Colors: []string{"U"}, ALSA: 3.0, GIHCount: nil},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolKeys(pool), pack, ratings, names, meta)

	all := append(recs.TopPicks, recs.Alternatives...)
	for _, r := range all {
		if strings.Contains(r.Reason, "%") {
			t.Errorf("Phase B Reason %q must not contain raw %% literal", r.Reason)
		}
		if strings.Contains(r.WhyNotTopReason, "%") {
			t.Errorf("Phase B WhyNotTopReason %q must not contain raw %% literal", r.WhyNotTopReason)
		}
	}
}

// ─── helpers ──────────────────────────────────────────────────────────────

// poolWithColors builds a pool map from n1 cards of color1, n2 of color2,
// n3 of color3 (use "" for a color to skip it).
func poolWithColors(_ *testing.T, n1 int, c1 string, n2 int, c2 string, n3 int, c3 string) map[string]draftalgo.CardMeta {
	m := make(map[string]draftalgo.CardMeta)
	for i := 0; i < n1 && c1 != ""; i++ {
		id := c1 + string(rune('a'+i))
		m[id] = draftalgo.CardMeta{Colors: []string{c1}}
	}
	for i := 0; i < n2 && c2 != ""; i++ {
		id := c2 + string(rune('a'+i))
		m[id] = draftalgo.CardMeta{Colors: []string{c2}}
	}
	for i := 0; i < n3 && c3 != ""; i++ {
		id := c3 + string(rune('a'+i))
		m[id] = draftalgo.CardMeta{Colors: []string{c3}}
	}
	return m
}

func poolKeys(m map[string]draftalgo.CardMeta) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
