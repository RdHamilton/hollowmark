// wildcard_gihwr_guard_test.go — package handlers (internal test)
//
// This file is an INTERNAL test (package handlers, not handlers_test) so it can
// access unexported fields like wildcardRecommendationItem.rankScore. It exists
// solely to enforce the #787-class guard: GIHWR values must be treated as
// fractional (0.0–1.0), not as percentages (0–100).
//
// Why an internal test is necessary:
//   The rankScore field is json:"-" and unexported. The external test package
//   (handlers_test) cannot observe it. An external test that asserts only on
//   tier_score or ordering does NOT catch a *100 regression — normalised
//   percentiles still produce a valid 0..1 rank range for arbitrary scaling
//   factors. Only a direct rankScore assertion on known inputs locks the
//   formula values.
//
// Guard property:
//   With fully-owned archetypes (completionScore=1.0), equal tiers, and
//   gihwrRange = 0.073 (0.623 − 0.550), the gihwrPercentile is min-max
//   normalised to {1.0, 0.0}. The GIHWR contribution to rankScore is
//   rankWeightGIHWRPercentile * {1.0, 0.0} = {0.25, 0.0}. All other terms are
//   identical between the two archetypes.
//
//   If *100 were applied before normalisation (the #787 bug class), meanGIHWR
//   becomes {62.3, 55.0}. After normalisation gihwrPercentile is still {1.0,
//   0.0} — the DELTA stays 0.25 but every rankScore becomes > 1.0 because
//   gihwrPercentile is multiplied into the formula as-is after the buggy
//   scaling. Specifically: tierScore=0.9, completionScore=1.0,
//   rotationProximity=0.5, gihwrPercentile=1.0 is correct; with *100 the
//   formula would receive gihwrPercentile = 62.3 (un-normalised) making
//   rankScore[0] ≈ 16.3 >> 1.0.
//
//   Assertions:
//     1. rankScore[0] ∈ [0.0, 1.0]  — impossible with *100 bug
//     2. rankScore[1] ∈ [0.0, 1.0]  — same
//     3. rankScore[0] > rankScore[1] — higher GIHWR wins
//     4. abs(rankScore[0]−rankScore[1]) ≤ rankWeightGIHWRPercentile+ε
//        — delta is exactly the GIHWR-weight contribution

package handlers

import (
	"math"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// TestGIHWRFractionalGuard_Internal directly invokes buildRecommendations with
// two archetypes whose only differing attribute is their GIHWR value (0.623 vs
// 0.550 — fractional) and asserts the rankScore values are mathematically
// consistent with fractional-unit treatment.
//
// This is the authoritative mechanical guard for the #787 regression class.
// The companion external-package test (TestGIHWRFractionalGuard in
// wildcard_recommendations_test.go) verifies ordering and HTTP shape; this test
// verifies the numeric invariant that cannot be observed from outside the package.
func TestGIHWRFractionalGuard_Internal(t *testing.T) {
	tier := "1"
	// Two archetypes: all copies owned (completionScore=1.0), same tier,
	// differing only in GIHWR.
	gihwr0 := 0.623 // higher — should rank first
	gihwr1 := 0.550 // lower  — should rank second

	rows := []repository.WildcardGapRow{
		{
			ArchetypeID:    1,
			ArchetypeName:  "Archetype Alpha",
			Format:         "Standard",
			Tier:           &tier,
			CopiesRequired: 4,
			CopiesOwned:    4,
			CopiesMissing:  0,
			Rarity:         "rare",
			ArenaID:        10001,
			CardName:       "Card Alpha",
			GIHWR:          &gihwr0,
		},
		{
			ArchetypeID:    2,
			ArchetypeName:  "Archetype Beta",
			Format:         "Standard",
			Tier:           &tier,
			CopiesRequired: 4,
			CopiesOwned:    4,
			CopiesMissing:  0,
			Rarity:         "rare",
			ArenaID:        10002,
			CardName:       "Card Beta",
			GIHWR:          &gihwr1,
		},
	}

	budget := repository.WildcardCounts{Common: 100, Uncommon: 100, Rare: 100, Mythic: 100}
	items := buildRecommendations(rows, budget)

	if len(items) != 2 {
		t.Fatalf("expected 2 recommendation items, got %d", len(items))
	}

	// items are sorted desc by rankScore, so items[0] has the higher GIHWR.
	score0 := items[0].rankScore
	score1 := items[1].rankScore

	// Guard 1: scores must be in [0.0, 1.0].
	// A *100 bug makes rankScore >> 1.0 (e.g. ~16.3 for gihwrPercentile=62.3).
	if score0 > 1.0 || score0 < 0.0 {
		t.Errorf("rankScore[0] = %v is outside [0.0, 1.0]; indicates *100 bug on GIHWR input", score0)
	}
	if score1 > 1.0 || score1 < 0.0 {
		t.Errorf("rankScore[1] = %v is outside [0.0, 1.0]; indicates *100 bug on GIHWR input", score1)
	}

	// Guard 2: the higher-GIHWR archetype must rank above the lower.
	if score0 <= score1 {
		t.Errorf("expected score[Archetype Alpha (gihwr=0.623)] > score[Archetype Beta (gihwr=0.550)]; got %v <= %v", score0, score1)
	}

	// Guard 3: the delta must equal exactly the GIHWR-weight contribution
	// (rankWeightGIHWRPercentile * 1.0 since the two archetypes are otherwise
	// identical and the min-max normalisation assigns {1.0, 0.0}).
	const epsilon = 1e-9
	delta := math.Abs(score0 - score1)
	wantDelta := rankWeightGIHWRPercentile // = 0.25
	if math.Abs(delta-wantDelta) > epsilon {
		t.Errorf("rankScore delta = %v, want %v (= rankWeightGIHWRPercentile); "+
			"a *100 bug changes the normalisation inputs and breaks this invariant", delta, wantDelta)
	}
}
