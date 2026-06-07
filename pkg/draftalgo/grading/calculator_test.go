package grading_test

import (
	"testing"

	"github.com/RdHamilton/hollowmark/pkg/draftalgo"
	"github.com/RdHamilton/hollowmark/pkg/draftalgo/grading"
)

type stubCards map[string]string

func (s stubCards) CardName(id string) string {
	return s[id]
}

// gihwrPtr takes a GIHWR in PERCENT (e.g. 60 for a 60% card) for test
// readability and returns a pointer to the canonical FRACTION the grading
// code consumes (0.60). This models the real wire contract: PickedCardGIHWR
// is a fraction (#787). Tests express percent to keep the bucket boundaries
// (58/54/50/46) legible.
func gihwrPtr(percent float64) *float64 { f := percent / 100; return &f }
func gradePtr(v string) *string         { return &v }

func TestCalculate_EmptyPicksErrors(t *testing.T) {
	_, err := grading.Calculate(draftalgo.SessionInfo{}, nil, nil)
	if err == nil {
		t.Fatal("expected error for empty picks")
	}
}

func TestCalculate_DefaultsWhenNoRatingsOrGrades(t *testing.T) {
	picks := []draftalgo.Pick{
		{CardID: "1", PackNumber: 1, PickNumber: 1},
		{CardID: "2", PackNumber: 1, PickNumber: 2},
	}
	g, err := grading.Calculate(draftalgo.SessionInfo{}, picks, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// pickQuality: 20 (default), color: 15 (default), deck: 14 (4 bombless + 10 flat), strategic: 10 (default) = 59 → D
	if g.OverallScore != 59 {
		t.Errorf("OverallScore = %d, want 59", g.OverallScore)
	}
	if g.OverallGrade != "F" {
		t.Errorf("OverallGrade = %q, want F (score < 60)", g.OverallGrade)
	}
	if g.PickQualityScore != 20.0 {
		t.Errorf("PickQualityScore = %v, want 20.0 default", g.PickQualityScore)
	}
	if len(g.Suggestions) == 0 {
		t.Error("expected at least one suggestion")
	}
}

func TestCalculate_HighQualityPicksScoreA(t *testing.T) {
	picks := []draftalgo.Pick{
		{CardID: "1", PickedCardGIHWR: gihwrPtr(60), PickQualityGrade: gradePtr("A+")},
		{CardID: "2", PickedCardGIHWR: gihwrPtr(60), PickQualityGrade: gradePtr("A")},
		{CardID: "3", PickedCardGIHWR: gihwrPtr(60), PickQualityGrade: gradePtr("A")},
	}
	g, err := grading.Calculate(draftalgo.SessionInfo{}, picks, nil)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// pickQuality: 40 (60 GIHWR is well past 58), color: 15, deck: 20 (3 bombs + 10), strategic: 15 (100% excellent) = 90
	if g.OverallScore != 90 {
		t.Errorf("OverallScore = %d, want 90", g.OverallScore)
	}
	if g.OverallGrade != "A-" {
		t.Errorf("OverallGrade = %q, want A-", g.OverallGrade)
	}
}

func TestCalculate_PickQualityBucketsRespected(t *testing.T) {
	cases := []struct {
		name    string
		gihwr   float64
		wantMin float64
		wantMax float64
	}{
		{"<46 falls into the bottom bucket", 40, 0, 24},
		{"46-50 awards 24-28", 48, 24, 28},
		{"50-54 awards 28-32", 52, 28, 32},
		{"54-58 awards 32-36", 56, 32, 36},
		{"58+ awards 36-40", 60, 36, 40},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			picks := []draftalgo.Pick{{CardID: "1", PickedCardGIHWR: gihwrPtr(c.gihwr)}}
			g, err := grading.Calculate(draftalgo.SessionInfo{}, picks, nil)
			if err != nil {
				t.Fatalf("unexpected: %v", err)
			}
			if g.PickQualityScore < c.wantMin || g.PickQualityScore > c.wantMax {
				t.Errorf("PickQualityScore = %v, want in [%v,%v]", g.PickQualityScore, c.wantMin, c.wantMax)
			}
		})
	}
}

func TestCalculate_BestAndWorstSortedByGIHWR(t *testing.T) {
	picks := []draftalgo.Pick{
		{CardID: "low", PickedCardGIHWR: gihwrPtr(45)},
		{CardID: "mid", PickedCardGIHWR: gihwrPtr(52)},
		{CardID: "high", PickedCardGIHWR: gihwrPtr(60)},
		{CardID: "highest", PickedCardGIHWR: gihwrPtr(65)},
	}
	cards := stubCards{
		"low":     "Low Card",
		"mid":     "Mid Card",
		"high":    "High Card",
		"highest": "Highest Card",
	}
	g, err := grading.Calculate(draftalgo.SessionInfo{}, picks, cards)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(g.BestPicks) != 3 || g.BestPicks[0] != "Highest Card" {
		t.Errorf("BestPicks not sorted by GIHWR desc: %v", g.BestPicks)
	}
	if len(g.WorstPicks) != 3 || g.WorstPicks[0] != "Low Card" {
		t.Errorf("WorstPicks not sorted by GIHWR asc: %v", g.WorstPicks)
	}
}

func TestCalculate_BestAndWorstFallBackToPlaceholderWhenLookupMissing(t *testing.T) {
	picks := []draftalgo.Pick{
		{CardID: "123", PickedCardGIHWR: gihwrPtr(60)},
	}
	// CardLookup returns empty string — should fall back to "Card <id>".
	g, _ := grading.Calculate(draftalgo.SessionInfo{}, picks, stubCards{})
	if g.BestPicks[0] != "Card 123" {
		t.Errorf("BestPicks[0] = %q, want %q", g.BestPicks[0], "Card 123")
	}
}

func TestCalculate_BestAndWorstEmptyWhenNoRatings(t *testing.T) {
	picks := []draftalgo.Pick{{CardID: "1"}, {CardID: "2"}}
	g, _ := grading.Calculate(draftalgo.SessionInfo{}, picks, nil)
	if len(g.BestPicks) != 0 || len(g.WorstPicks) != 0 {
		t.Errorf("expected empty best/worst, got best=%v worst=%v", g.BestPicks, g.WorstPicks)
	}
}

func TestCalculate_SuggestionFloor(t *testing.T) {
	// Force the "excellent draft" path: high quality + high strategic.
	picks := []draftalgo.Pick{
		{CardID: "a", PickedCardGIHWR: gihwrPtr(62), PickQualityGrade: gradePtr("A+")},
		{CardID: "b", PickedCardGIHWR: gihwrPtr(60), PickQualityGrade: gradePtr("A")},
		{CardID: "c", PickedCardGIHWR: gihwrPtr(59), PickQualityGrade: gradePtr("A")},
	}
	g, _ := grading.Calculate(draftalgo.SessionInfo{}, picks, nil)
	if len(g.Suggestions) != 1 {
		t.Errorf("expected single 'excellent draft' suggestion, got %v", g.Suggestions)
	}
}

func TestLetterGrade_Buckets(t *testing.T) {
	cases := []struct {
		score int
		want  string
	}{
		{100, "A+"},
		{97, "A+"},
		{96, "A"},
		{93, "A"},
		{92, "A-"},
		{90, "A-"},
		{89, "B+"},
		{87, "B+"},
		{86, "B"},
		{83, "B"},
		{82, "B-"},
		{80, "B-"},
		{79, "C+"},
		{77, "C+"},
		{76, "C"},
		{73, "C"},
		{72, "C-"},
		{70, "C-"},
		{69, "D"},
		{60, "D"},
		{59, "F"},
		{0, "F"},
	}
	for _, c := range cases {
		if got := draftalgo.LetterGrade(c.score); got != c.want {
			t.Errorf("LetterGrade(%d) = %q, want %q", c.score, got, c.want)
		}
	}
}
