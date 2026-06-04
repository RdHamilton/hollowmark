package pickquality_test

import (
	"testing"

	"github.com/RdHamilton/vault-mtg/pkg/draftalgo"
	"github.com/RdHamilton/vault-mtg/pkg/draftalgo/pickquality"
)

// stubLookups satisfies CardLookup and RatingsLookup for the tests.
type stubLookups struct {
	names   map[string]string
	ratings map[string]float64
}

func (s stubLookups) CardName(id string) string {
	return s.names[id]
}

func (s stubLookups) GIHWR(id, _ string) (float64, bool) {
	v, ok := s.ratings[id]
	return v, ok
}

var (
	_ draftalgo.CardLookup    = stubLookups{}
	_ draftalgo.RatingsLookup = stubLookups{}
)

func TestAnalyze_EmptyPackErrors(t *testing.T) {
	_, err := pickquality.Analyze("PremierDraft", nil, "x", stubLookups{}, stubLookups{})
	if err == nil {
		t.Fatal("expected error for empty pack")
	}
}

func TestAnalyze_PickedNotInPackErrors(t *testing.T) {
	_, err := pickquality.Analyze("PremierDraft", []string{"1"}, "missing", stubLookups{}, stubLookups{})
	if err == nil {
		t.Fatal("expected error when picked card is absent from pack")
	}
}

func TestAnalyze_NoRatingDataGradesNA(t *testing.T) {
	pack := []string{"1", "2", "3"}
	q, err := pickquality.Analyze("PremierDraft", pack, "1", stubLookups{}, stubLookups{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if q.Grade != "N/A" {
		t.Errorf("Grade = %q, want N/A", q.Grade)
	}
	if q.PickedCardGIHWR != 0 {
		t.Errorf("PickedCardGIHWR = %v, want 0", q.PickedCardGIHWR)
	}
}

func TestAnalyze_GradeBucketsByRank(t *testing.T) {
	// Pack of 14 cards with descending GIHWR so rank is deterministic.
	// GIHWR values are FRACTIONS (0.0–1.0) — the canonical BFF unit (#787).
	// Ranking is scale-invariant so the rank/grade buckets are unaffected,
	// but the fixtures must reflect the real wire unit rather than percent.
	names := map[string]string{}
	ratings := map[string]float64{}
	pack := make([]string, 0, 14)
	for i := 1; i <= 14; i++ {
		id := string(rune('a' + i - 1))
		pack = append(pack, id)
		names[id] = id
		ratings[id] = float64(70-i) / 100 // 0.69, 0.68, 0.67, ...
	}
	lookups := stubLookups{names: names, ratings: ratings}

	cases := []struct {
		picked    string
		wantGrade string
		wantRank  int
	}{
		{"a", "A+", 1},
		{"b", "A", 2},
		{"c", "A", 3},
		{"d", "B", 4},
		{"e", "B", 5},
		{"f", "C", 6},
		{"h", "C", 8},
		{"i", "D", 9},
		{"j", "D", 10},
		{"k", "F", 11},
		{"n", "F", 14},
	}
	for _, c := range cases {
		t.Run(c.picked, func(t *testing.T) {
			q, err := pickquality.Analyze("PremierDraft", pack, c.picked, lookups, lookups)
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if q.Grade != c.wantGrade {
				t.Errorf("Grade = %q, want %q", q.Grade, c.wantGrade)
			}
			if q.Rank != c.wantRank {
				t.Errorf("Rank = %d, want %d", q.Rank, c.wantRank)
			}
		})
	}
}

func TestAnalyze_AlternativesCappedAt5AndExcludePicked(t *testing.T) {
	names := map[string]string{}
	ratings := map[string]float64{}
	pack := []string{}
	for i := 1; i <= 8; i++ {
		id := string(rune('a' + i - 1))
		pack = append(pack, id)
		names[id] = id
		ratings[id] = float64(70-i) / 100 // fractions: 0.69, 0.68, ...
	}
	lookups := stubLookups{names: names, ratings: ratings}
	q, err := pickquality.Analyze("PremierDraft", pack, "a", lookups, lookups)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(q.Alternatives) != 5 {
		t.Fatalf("Alternatives len = %d, want 5", len(q.Alternatives))
	}
	for _, alt := range q.Alternatives {
		if alt.CardID == "a" {
			t.Errorf("picked card %q must not appear in Alternatives", alt.CardID)
		}
	}
	// #787: the GIHWR fields pass through as the original FRACTION, unscaled.
	// 'a' is the best card at fraction 0.69 — not 69 (percent) and not 0.0069.
	if q.PackBestGIHWR != 0.69 {
		t.Errorf("PackBestGIHWR = %v, want 0.69 (fraction, unscaled)", q.PackBestGIHWR)
	}
	if q.Alternatives[0].GIHWR != 0.68 {
		t.Errorf("Alternatives[0].GIHWR = %v, want 0.68 (fraction, unscaled)", q.Alternatives[0].GIHWR)
	}
}

func TestAnalyze_FallsBackToUnknownCardNameWhenLookupEmpty(t *testing.T) {
	q, err := pickquality.Analyze(
		"PremierDraft",
		[]string{"x", "y"},
		"x",
		stubLookups{ratings: map[string]float64{"x": 0.60, "y": 0.50}},
		stubLookups{},
	)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(q.Alternatives) == 0 || q.Alternatives[0].CardName != "Unknown Card" {
		t.Errorf("expected Unknown Card placeholder, got %v", q.Alternatives)
	}
}

func TestSerializeRoundTrip(t *testing.T) {
	in := []pickquality.Alternative{
		{CardID: "1", CardName: "A", GIHWR: 0.60, Rank: 1},
		{CardID: "2", CardName: "B", GIHWR: 0.55, Rank: 2},
	}
	encoded, err := pickquality.SerializeAlternatives(in)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	out, err := pickquality.DeserializeAlternatives(encoded)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if len(out) != len(in) || out[0].CardName != "A" || out[1].Rank != 2 {
		t.Errorf("round-trip mismatch: %v -> %v", in, out)
	}
}
