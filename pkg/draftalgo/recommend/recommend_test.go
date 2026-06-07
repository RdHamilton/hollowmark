package recommend_test

import (
	"strings"
	"testing"

	"github.com/RdHamilton/hollowmark/pkg/draftalgo"
	"github.com/RdHamilton/hollowmark/pkg/draftalgo/recommend"
)

// stubRatings satisfies draftalgo.RatingsLookup.
type stubRatings map[string]float64

func (s stubRatings) GIHWR(id, _ string) (float64, bool) {
	v, ok := s[id]
	return v, ok
}

// stubCards satisfies draftalgo.CardLookup.
type stubCards map[string]string

func (s stubCards) CardName(id string) string { return s[id] }

var (
	_ draftalgo.RatingsLookup = stubRatings{}
	_ draftalgo.CardLookup    = stubCards{}
)

// ─── empty pack / edge cases ───────────────────────────────────────────────

func TestRecommend_EmptyPackReturnsEmpty(t *testing.T) {
	recs := recommend.Recommend("PremierDraft", nil, nil, stubRatings{}, stubCards{}, nil)
	if len(recs.TopPicks) != 0 || len(recs.Alternatives) != 0 {
		t.Errorf("expected empty recs for empty pack, got %+v", recs)
	}
}

// ─── GIHWR ranking ────────────────────────────────────────────────────────

func TestRecommend_TopPicksRankedByGIHWR(t *testing.T) {
	pack := []string{"a", "b", "c", "d", "e"}
	// Ratings in non-rank order so we verify sorting, not insertion order.
	ratings := stubRatings{"a": 55.0, "b": 70.0, "c": 40.0, "d": 65.0, "e": 50.0}
	names := stubCards{"a": "A", "b": "B", "c": "C", "d": "D", "e": "E"}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, names, nil)

	// Top picks must be in descending GIHWR order.
	for i := 1; i < len(recs.TopPicks); i++ {
		if recs.TopPicks[i].Priority > recs.TopPicks[i-1].Priority {
			t.Errorf("TopPick[%d].Priority > TopPick[%d].Priority — not descending", i, i-1)
		}
	}
	// The highest-rated card (b, 70.0) must be the first top pick.
	if len(recs.TopPicks) == 0 || recs.TopPicks[0].CardID != "b" {
		t.Errorf("expected top pick to be 'b' (GIHWR 70.0), got %+v", recs.TopPicks)
	}
}

func TestRecommend_TopPicksLimitedToThree(t *testing.T) {
	pack := []string{"a", "b", "c", "d", "e", "f"}
	ratings := stubRatings{"a": 70.0, "b": 65.0, "c": 60.0, "d": 55.0, "e": 50.0, "f": 45.0}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, stubCards{}, nil)

	if len(recs.TopPicks) != 3 {
		t.Errorf("TopPicks len = %d, want 3", len(recs.TopPicks))
	}
}

func TestRecommend_AlternativesContainRemainder(t *testing.T) {
	pack := []string{"a", "b", "c", "d", "e"}
	ratings := stubRatings{"a": 70.0, "b": 65.0, "c": 60.0, "d": 55.0, "e": 50.0}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, stubCards{}, nil)

	total := len(recs.TopPicks) + len(recs.Alternatives)
	if total != 5 {
		t.Errorf("total picks = %d, want 5 (all pack cards covered)", total)
	}
}

func TestRecommend_SmallPackNoPanic(t *testing.T) {
	// Single card in pack — should return one TopPick, zero Alternatives.
	pack := []string{"a"}
	ratings := stubRatings{"a": 60.0}
	names := stubCards{"a": "Alpha"}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, names, nil)

	if len(recs.TopPicks) != 1 {
		t.Errorf("TopPicks len = %d, want 1", len(recs.TopPicks))
	}
	if len(recs.Alternatives) != 0 {
		t.Errorf("Alternatives len = %d, want 0", len(recs.Alternatives))
	}
}

// ─── graceful N/A (empty ratings cache) ───────────────────────────────────

func TestRecommend_EmptyCacheProducesNAReasons(t *testing.T) {
	pack := []string{"a", "b", "c"}
	// No ratings — all GIHWR = 0.

	recs := recommend.Recommend("PremierDraft", nil, pack, stubRatings{}, stubCards{}, nil)

	// Must still return entries (graceful degrade, not empty).
	if len(recs.TopPicks)+len(recs.Alternatives) == 0 {
		t.Errorf("expected entries even with no ratings")
	}
	for _, r := range recs.TopPicks {
		if r.Reason == "" {
			t.Errorf("TopPick.Reason is empty — should have N/A placeholder")
		}
	}
}

// ─── plain-English reasons, no raw GIHWR % in primary display ─────────────

func TestRecommend_NoRawGIHWRPercentInPrimaryReason(t *testing.T) {
	// Per Prof gate: raw GIHWR percentages must NOT appear in the primary
	// Reason field. A "%" literal in Reason is a compliance violation.
	pack := []string{"a", "b", "c"}
	ratings := stubRatings{"a": 62.1, "b": 59.4, "c": 50.0}
	names := stubCards{"a": "Alpha", "b": "Beta", "c": "Gamma"}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, names, nil)

	for _, r := range append(recs.TopPicks, recs.Alternatives...) {
		if strings.Contains(r.Reason, "%") {
			t.Errorf("Reason %q contains raw GIHWR percentage — violates Prof gate", r.Reason)
		}
	}
}

func TestRecommend_ReasonStringsAreNonEmpty(t *testing.T) {
	pack := []string{"a", "b", "c", "d"}
	ratings := stubRatings{"a": 70.0, "b": 65.0, "c": 60.0, "d": 55.0}
	names := stubCards{"a": "A", "b": "B", "c": "C", "d": "D"}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, names, nil)

	for _, r := range append(recs.TopPicks, recs.Alternatives...) {
		if r.Reason == "" {
			t.Errorf("Reason must not be empty (card %s)", r.CardID)
		}
	}
}

// ─── priority derivation ───────────────────────────────────────────────────

func TestRecommend_PriorityDecreasesWithRank(t *testing.T) {
	// Rank 1 card must have higher Priority than rank 2+.
	pack := []string{"a", "b", "c", "d", "e", "f"}
	ratings := stubRatings{"a": 70.0, "b": 65.0, "c": 60.0, "d": 55.0, "e": 50.0, "f": 45.0}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, stubCards{}, nil)

	if len(recs.TopPicks) < 2 {
		t.Skip("not enough top picks to compare")
	}
	if recs.TopPicks[0].Priority <= recs.TopPicks[1].Priority {
		t.Errorf("TopPick[0].Priority = %d, TopPick[1].Priority = %d — first must be higher",
			recs.TopPicks[0].Priority, recs.TopPicks[1].Priority)
	}
}

func TestRecommend_PriorityInRange(t *testing.T) {
	pack := []string{"a", "b", "c", "d", "e", "f"}
	ratings := stubRatings{"a": 70.0, "b": 65.0, "c": 60.0, "d": 55.0, "e": 50.0, "f": 45.0}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, stubCards{}, nil)

	for _, r := range append(recs.TopPicks, recs.Alternatives...) {
		if r.Priority < 1 || r.Priority > 5 {
			t.Errorf("Priority = %d out of [1,5] range (card %s)", r.Priority, r.CardID)
		}
	}
}

// ─── CardID present ────────────────────────────────────────────────────────

func TestRecommend_CardIDsPreserved(t *testing.T) {
	pack := []string{"card-1", "card-2", "card-3"}
	ratings := stubRatings{"card-1": 70.0, "card-2": 65.0, "card-3": 60.0}
	names := stubCards{"card-1": "One", "card-2": "Two", "card-3": "Three"}

	recs := recommend.Recommend("PremierDraft", nil, pack, ratings, names, nil)

	seen := map[string]bool{}
	for _, r := range append(recs.TopPicks, recs.Alternatives...) {
		seen[r.CardID] = true
	}
	for _, id := range pack {
		if !seen[id] {
			t.Errorf("pack card %q missing from recommendations", id)
		}
	}
}
