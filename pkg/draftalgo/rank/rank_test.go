package rank_test

import (
	"testing"

	"github.com/RdHamilton/hollowmark/pkg/draftalgo"
	"github.com/RdHamilton/hollowmark/pkg/draftalgo/rank"
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

func TestRankByGIHWR_EmptyPackReturnsEmpty(t *testing.T) {
	result := rank.ByGIHWR("PremierDraft", nil, stubRatings{}, stubCards{})
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(result))
	}
}

func TestRankByGIHWR_CorrectOrdering(t *testing.T) {
	// Cards with distinct GIHWR — result must be descending.
	cards := []string{"a", "b", "c"}
	ratings := stubRatings{"a": 55.0, "b": 70.0, "c": 40.0}
	names := stubCards{"a": "Alpha", "b": "Beta", "c": "Gamma"}

	result := rank.ByGIHWR("PremierDraft", cards, ratings, names)

	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	// Rank 1 must be Beta (highest GIHWR 70.0)
	if result[0].CardID != "b" || result[0].Rank != 1 {
		t.Errorf("rank 1 = %+v, want {CardID:b, Rank:1}", result[0])
	}
	if result[1].CardID != "a" || result[1].Rank != 2 {
		t.Errorf("rank 2 = %+v, want {CardID:a, Rank:2}", result[1])
	}
	if result[2].CardID != "c" || result[2].Rank != 3 {
		t.Errorf("rank 3 = %+v, want {CardID:c, Rank:3}", result[2])
	}
}

func TestRankByGIHWR_TiesByCardIDOrder(t *testing.T) {
	// When GIHWR is equal, sort should be stable (preserve input order).
	cards := []string{"x", "y", "z"}
	ratings := stubRatings{"x": 50.0, "y": 50.0, "z": 50.0}
	names := stubCards{}

	result := rank.ByGIHWR("PremierDraft", cards, ratings, names)

	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	// Input order preserved for ties.
	if result[0].CardID != "x" || result[1].CardID != "y" || result[2].CardID != "z" {
		t.Errorf("tie order broken: %v %v %v", result[0].CardID, result[1].CardID, result[2].CardID)
	}
}

func TestRankByGIHWR_NoRatingsAllZero(t *testing.T) {
	cards := []string{"a", "b"}
	result := rank.ByGIHWR("PremierDraft", cards, stubRatings{}, stubCards{})

	if len(result) != 2 {
		t.Fatalf("len = %d, want 2", len(result))
	}
	for _, r := range result {
		if r.GIHWR != 0 {
			t.Errorf("expected GIHWR 0 when not found, got %v", r.GIHWR)
		}
		if r.HasGIHWR {
			t.Errorf("expected HasGIHWR=false when not found")
		}
	}
}

func TestRankByGIHWR_CardNameFallback(t *testing.T) {
	cards := []string{"known", "unknown"}
	ratings := stubRatings{"known": 60.0, "unknown": 55.0}
	names := stubCards{"known": "Lightning Bolt"}

	result := rank.ByGIHWR("PremierDraft", cards, ratings, names)

	// "known" should be rank 1 (higher GIHWR)
	if result[0].CardName != "Lightning Bolt" {
		t.Errorf("CardName = %q, want %q", result[0].CardName, "Lightning Bolt")
	}
	// "unknown" should fall back to "Unknown Card"
	if result[1].CardName != "Unknown Card" {
		t.Errorf("CardName fallback = %q, want %q", result[1].CardName, "Unknown Card")
	}
}

func TestRankByGIHWR_RanksAreOneIndexed(t *testing.T) {
	cards := []string{"a", "b", "c"}
	ratings := stubRatings{"a": 60.0, "b": 55.0, "c": 50.0}

	result := rank.ByGIHWR("fmt", cards, ratings, stubCards{})

	for i, r := range result {
		if r.Rank != i+1 {
			t.Errorf("result[%d].Rank = %d, want %d", i, r.Rank, i+1)
		}
	}
}
