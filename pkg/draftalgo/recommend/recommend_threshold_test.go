package recommend_test

// Tests for ticket #1400 — color-commitment threshold fix.
//
// AC3: 3-card committed pool + off-color bomb (top-10% GIHWR) → bomb still recommended.
// AC4: 5-card committed pool + off-color junk → junk suppressed (threshold at 5 working).
//
// Written first per TDD — watch them fail before implementing.

import (
	"strings"
	"testing"

	"github.com/RdHamilton/hollowmark/pkg/draftalgo/recommend"
)

// TestColorCommitment_BombNotSuppressedAt3Cards — AC3.
//
// Pool has exactly 3 R cards and 3 G cards. Under the OLD threshold of 3, the
// pool was considered committed R/G. The off-color bomb (U) with GIHWR=64.0
// is below the existing splashHighGIHWRFloor (66.0) so under OLD code it receives
// a "Not your colors" reason (possibly with ALSA suffix).
//
// Under the NEW threshold of 5, the pool is NOT yet committed → the bomb must
// not receive the off-color suppression.
//
// Verifies AC3: "a 3-card committed pool where an off-color bomb (top-10% GIHWR)
// exists → bomb is still recommended."
func TestColorCommitment_BombNotSuppressedAt3Cards(t *testing.T) {
	// Pool: 3 R + 3 G cards. Under new threshold=5, NOT committed.
	pool := poolWithColors(t, 3, "R", 3, "G", 0, "")
	poolIDs := poolKeys(pool)

	count := 1200
	// Off-color bomb: U, GIHWR=64.0.
	//   - Below splashHighGIHWRFloor (66.0) → old code: "Not your colors[; ...]" when committed.
	//   - Above topQualityGIHWRFloor (63.0) → new quality modifier: surfaces.
	//   - Under new threshold=5: pool is not committed → bomb surfaces pre-commitment.
	ratings := stubRatings{"bomb": 64.0, "filler": 55.0}
	names := stubCards{"bomb": "Blue Bomb", "filler": "Red Filler"}
	meta := stubCardMeta{
		"bomb":   {Colors: []string{"U"}, ALSA: 2.5, GIHCount: &count},
		"filler": {Colors: []string{"R"}, ALSA: 5.0, GIHCount: &count},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolIDs, []string{"bomb", "filler"}, ratings, names, meta)

	allRecs := append(recs.TopPicks, recs.Alternatives...)
	if len(allRecs) == 0 {
		t.Fatal("expected at least one recommendation")
	}

	bombFound := false
	for _, r := range allRecs {
		if r.CardID == "bomb" {
			bombFound = true
			if r.Reason == "" {
				t.Errorf("bomb card has empty reason — must be present with a reason")
			}
			// Under new threshold (5), pool is NOT committed → bomb must NOT be suppressed
			// with the off-color penalty. Check for "Not your colors" prefix (there may be
			// an ALSA suffix appended: "Not your colors; typically taken early").
			if strings.HasPrefix(r.Reason, "Not your colors") {
				t.Errorf("off-color bomb in 3-card pool (below new threshold 5): must not be suppressed; got Reason %q — pool is not committed at new threshold", r.Reason)
			}
			break
		}
	}
	if !bombFound {
		t.Errorf("off-color bomb must surface in recommendations; pool has only 3 cards per color (below new threshold of 5)")
	}
}

// TestColorCommitment_JunkSuppressedAt5Cards — AC4.
//
// Pool has 5 R + 5 G cards (meets the new threshold of 5). An off-color junk
// card with modest GIHWR (52.0 — well below the top-10% quality floor of 63.0)
// must be suppressed — the threshold is working correctly at 5.
//
// Verifies AC4: "a 5-card committed pool where a junk off-color card exists
// → junk is suppressed (threshold working correctly at 5)."
func TestColorCommitment_JunkSuppressedAt5Cards(t *testing.T) {
	// Pool: 5 R + 5 G — committed at new threshold of 5.
	pool := poolWithColors(t, 5, "R", 5, "G", 0, "")
	poolIDs := poolKeys(pool)

	count := 1000
	// Junk card: off-color (U), modest GIHWR=52.0 — below top-10% quality floor.
	// ALSA=3.0 — no "frequently available late" / "typically taken early" suffix.
	// At full commitment with low GIHWR → suppressed.
	ratings := stubRatings{"junk": 52.0, "good": 65.0}
	names := stubCards{"junk": "Blue Junk", "good": "Red Good"}
	meta := stubCardMeta{
		"junk": {Colors: []string{"U"}, ALSA: 3.0, GIHCount: &count},
		"good": {Colors: []string{"R"}, ALSA: 3.0, GIHCount: &count},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolIDs, []string{"junk", "good"}, ratings, names, meta)

	allRecs := append(recs.TopPicks, recs.Alternatives...)
	if len(allRecs) == 0 {
		t.Fatal("expected at least one recommendation")
	}

	// Junk card must be in the output — all pack cards are present.
	// At 5-card commitment level + low GIHWR → suppressed: "Not your colors".
	junkFound := false
	for _, r := range allRecs {
		if r.CardID == "junk" {
			junkFound = true
			if !strings.HasPrefix(r.Reason, "Not your colors") {
				t.Errorf("off-color junk (GIHWR 52.0) in 5-card committed pool: expected Reason starting with %q, got %q", "Not your colors", r.Reason)
			}
			break
		}
	}
	if !junkFound {
		t.Errorf("junk card must appear in recommendations (suppressed cards still show in list)")
	}
}

// TestColorCommitment_ThresholdAt5_NotCommittedAt4 — regression guard.
//
// A pool with exactly 4 R + 4 G cards must NOT be considered committed
// (threshold is 5). Archetype must be empty.
func TestColorCommitment_ThresholdAt5_NotCommittedAt4(t *testing.T) {
	pool := poolWithColors(t, 4, "R", 4, "G", 0, "")
	poolIDs := poolKeys(pool)

	count := 1000
	ratings := stubRatings{"testcard": 62.0}
	names := stubCards{"testcard": "Test Card"}
	meta := stubCardMeta{
		"testcard": {Colors: []string{"U"}, ALSA: 3.0, GIHCount: &count},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolIDs, []string{"testcard"}, ratings, names, meta)

	if len(recs.TopPicks) == 0 {
		t.Fatal("expected a top pick")
	}
	// 4 cards each: below threshold of 5 → not committed → Archetype empty.
	if recs.TopPicks[0].Archetype != "" {
		t.Errorf("pool with 4 cards per color (below threshold 5): Archetype must be empty, got %q", recs.TopPicks[0].Archetype)
	}
}

// TestColorCommitment_ThresholdAt5_CommittedAt5 — regression guard.
//
// A pool with exactly 5 R + 5 G cards MUST be considered committed (threshold = 5).
// Archetype must be populated.
func TestColorCommitment_ThresholdAt5_CommittedAt5(t *testing.T) {
	pool := poolWithColors(t, 5, "R", 5, "G", 0, "")
	poolIDs := poolKeys(pool)

	count := 1000
	ratings := stubRatings{"testcard": 62.0}
	names := stubCards{"testcard": "Test Card"}
	meta := stubCardMeta{
		"testcard": {Colors: []string{"R"}, ALSA: 3.0, GIHCount: &count},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolIDs, []string{"testcard"}, ratings, names, meta)

	if len(recs.TopPicks) == 0 {
		t.Fatal("expected a top pick")
	}
	// 5 cards each: at threshold of 5 → committed → Archetype populated (R/G = Gruul).
	if recs.TopPicks[0].Archetype == "" {
		t.Errorf("pool with 5 cards per color (at threshold 5): Archetype must be populated, got empty")
	}
}

// TestQualityModifier_TopQualityBombSurfacesWhenCommitted — quality modifier (AC2).
//
// Even when the pool IS committed (5+ cards per color), an off-color card with
// top-10%-quality GIHWR (64.0 — above topQualityGIHWRFloor ~63.0, below
// splashHighGIHWRFloor 66.0) must NOT receive the "Not your colors" suppression.
// This is the quality modifier: top-quality cards bypass full suppression.
func TestQualityModifier_TopQualityBombSurfacesWhenCommitted(t *testing.T) {
	// Pool: 5 R + 5 G — fully committed at new threshold.
	pool := poolWithColors(t, 5, "R", 5, "G", 0, "")
	poolIDs := poolKeys(pool)

	count := 1200
	// Bomb: off-color (U), GIHWR=64.0.
	//   - Below splashHighGIHWRFloor (66.0) → would be "Not your colors" without quality modifier.
	//   - Above topQualityGIHWRFloor (63.0) → quality modifier must bypass full suppression.
	//   - ALSA=2.5 → "typically taken early" suffix fires; use HasPrefix for check.
	ratings := stubRatings{"bomb": 64.0, "filler": 55.0}
	names := stubCards{"bomb": "Blue Bomb", "filler": "Red Filler"}
	meta := stubCardMeta{
		"bomb":   {Colors: []string{"U"}, ALSA: 2.5, GIHCount: &count},
		"filler": {Colors: []string{"R"}, ALSA: 5.0, GIHCount: &count},
	}
	for id, m := range pool {
		meta[id] = m
	}

	recs := recommend.Recommend("PremierDraft", poolIDs, []string{"bomb", "filler"}, ratings, names, meta)

	allRecs := append(recs.TopPicks, recs.Alternatives...)
	bombFound := false
	for _, r := range allRecs {
		if r.CardID == "bomb" {
			bombFound = true
			// Quality modifier: top-10% GIHWR card must bypass full suppression.
			if strings.HasPrefix(r.Reason, "Not your colors") {
				t.Errorf("top-10%% quality bomb (GIHWR 64.0) in committed pool: Reason %q starts with \"Not your colors\" — quality modifier must bypass full suppression", r.Reason)
			}
			break
		}
	}
	if !bombFound {
		t.Error("bomb must appear in recommendations")
	}
}
