// Package grading computes overall draft grades from a session's picks.
//
// The grading model is the same 4-component, 100-point breakdown the
// Wails app shipped with:
//
//	pick quality      (0–40)  — average 17Lands GIHWR of picked cards
//	color discipline  (0–20)  — placeholder until color-pair winrate data lands
//	deck composition  (0–25)  — bomb count + curve heuristics
//	strategic         (0–15)  — % of A/A+ graded picks
//
// Restored from internal/mtga/draft/grading/calculator.go (deleted in
// commit 783cf66). The original depended on the Wails-era SQLite
// repositories; this version takes its data through the small
// CardLookup interface from the parent package and does its work over
// a []draftalgo.Pick slice the caller supplies.
package grading

import (
	"fmt"
	"math"
	"sort"

	"github.com/RdHamilton/vault-mtg/pkg/draftalgo"
)

// DraftGrade is the wire-shape the SPA's grading.DraftGrade type consumes.
// JSON keys stay snake_case to match the existing Wails-generated SPA
// model (frontend/src/types/models.ts > grading.DraftGrade).
type DraftGrade struct {
	OverallGrade         string   `json:"overall_grade"`          // A+, A, A-, B+, etc.
	OverallScore         int      `json:"overall_score"`          // 0-100
	PickQualityScore     float64  `json:"pick_quality_score"`     // 0-40
	ColorDisciplineScore float64  `json:"color_discipline_score"` // 0-20
	DeckCompositionScore float64  `json:"deck_composition_score"` // 0-25
	StrategicScore       float64  `json:"strategic_score"`        // 0-15
	BestPicks            []string `json:"best_picks"`             // Top 3 card names
	WorstPicks           []string `json:"worst_picks"`            // Bottom 3 card names
	Suggestions          []string `json:"suggestions"`            // Improvement suggestions
}

// Calculate produces a DraftGrade for the supplied session + picks. cards
// is consulted for human-readable names in the best/worst lists; pass nil
// (or an implementation that always returns "") to fall back to the
// "Card <id>" placeholder.
//
// Returns an error only when picks is empty — every other defensive case
// degrades gracefully to a default sub-score.
func Calculate(session draftalgo.SessionInfo, picks []draftalgo.Pick, cards draftalgo.CardLookup) (*DraftGrade, error) {
	if len(picks) == 0 {
		return nil, fmt.Errorf("no picks supplied")
	}

	pickQualityScore := pickQualityScore(picks)
	colorScore := colorDisciplineScore(session, picks)
	deckScore := deckCompositionScore(picks)
	strategicScore := strategicScore(picks)

	overallScore := int(math.Round(pickQualityScore + colorScore + deckScore + strategicScore))
	if overallScore > 100 {
		overallScore = 100
	}
	if overallScore < 0 {
		overallScore = 0
	}

	bestPicks, worstPicks := bestAndWorstPicks(picks, cards)
	suggestions := generateSuggestions(pickQualityScore, colorScore, deckScore, strategicScore)

	return &DraftGrade{
		OverallGrade:         draftalgo.LetterGrade(overallScore),
		OverallScore:         overallScore,
		PickQualityScore:     pickQualityScore,
		ColorDisciplineScore: colorScore,
		DeckCompositionScore: deckScore,
		StrategicScore:       strategicScore,
		BestPicks:            bestPicks,
		WorstPicks:           worstPicks,
		Suggestions:          suggestions,
	}, nil
}

// pickQualityScore — average GIHWR mapped to 0–40 with the same bucket
// boundaries the Wails calculator used.
func pickQualityScore(picks []draftalgo.Pick) float64 {
	var sum float64
	var count int
	for _, p := range picks {
		if p.PickedCardGIHWR != nil {
			sum += *p.PickedCardGIHWR
			count++
		}
	}
	if count == 0 {
		return 20.0 // Default to C grade if no quality data.
	}

	avgGIHWR := sum / float64(count)

	var score float64
	switch {
	case avgGIHWR >= 58:
		score = 36 + (avgGIHWR-58)*2
	case avgGIHWR >= 54:
		score = 32 + (avgGIHWR - 54)
	case avgGIHWR >= 50:
		score = 28 + (avgGIHWR - 50)
	case avgGIHWR >= 46:
		score = 24 + (avgGIHWR - 46)
	default:
		score = avgGIHWR / 2
	}

	if score > 40 {
		score = 40
	}
	if score < 0 {
		score = 0
	}
	return score
}

// colorDisciplineScore — placeholder constant (B-grade default) until
// the algorithm grows real color-pair winrate analysis. Carried over
// verbatim from the Wails-era stub so the overall score remains stable
// for users between the old and new implementations.
func colorDisciplineScore(_ draftalgo.SessionInfo, _ []draftalgo.Pick) float64 {
	return 15.0
}

// deckCompositionScore — bomb count + curve heuristic (0–25).
// Bombs are picks graded A+ or A.
func deckCompositionScore(picks []draftalgo.Pick) float64 {
	if len(picks) == 0 {
		return 0
	}

	bombCount := 0
	for _, p := range picks {
		if p.PickQualityGrade != nil {
			g := *p.PickQualityGrade
			if g == "A+" || g == "A" {
				bombCount++
			}
		}
	}

	score := 0.0
	switch {
	case bombCount >= 2 && bombCount <= 4:
		score += 10
	case bombCount == 1 || bombCount == 5:
		score += 7
	default:
		score += 4
	}

	// Curve + removal are placeholders pending a real archetype model;
	// award the same flat 10 the Wails calculator used so totals stay
	// comparable.
	score += 10
	return score
}

// strategicScore — share of A/A+ graded picks * 15.
func strategicScore(picks []draftalgo.Pick) float64 {
	if len(picks) == 0 {
		return 0
	}

	excellent := 0
	graded := 0
	for _, p := range picks {
		if p.PickQualityGrade != nil {
			graded++
			g := *p.PickQualityGrade
			if g == "A+" || g == "A" {
				excellent++
			}
		}
	}
	if graded == 0 {
		return 10.0
	}
	return float64(excellent) / float64(graded) * 15
}

// bestAndWorstPicks returns the top-3 + bottom-3 picks by GIHWR. Card
// names come from the CardLookup; missing names fall back to
// "Card <id>".
func bestAndWorstPicks(picks []draftalgo.Pick, cards draftalgo.CardLookup) ([]string, []string) {
	type scored struct {
		cardID string
		gihwr  float64
	}

	var rated []scored
	for _, p := range picks {
		if p.PickedCardGIHWR == nil {
			continue
		}
		rated = append(rated, scored{cardID: p.CardID, gihwr: *p.PickedCardGIHWR})
	}
	if len(rated) == 0 {
		return []string{}, []string{}
	}

	sort.SliceStable(rated, func(i, j int) bool {
		return rated[i].gihwr > rated[j].gihwr
	})

	max := 3
	if len(rated) < max {
		max = len(rated)
	}

	name := func(id string) string {
		if cards != nil {
			if n := cards.CardName(id); n != "" {
				return n
			}
		}
		return fmt.Sprintf("Card %s", id)
	}

	best := make([]string, 0, max)
	for i := 0; i < max; i++ {
		best = append(best, name(rated[i].cardID))
	}

	worst := make([]string, 0, max)
	for i := 0; i < max; i++ {
		worst = append(worst, name(rated[len(rated)-1-i].cardID))
	}
	return best, worst
}

// generateSuggestions returns improvement suggestions based on
// sub-scores. Thresholds match the Wails-era heuristics.
func generateSuggestions(pickQuality, color, deck, strategic float64) []string {
	var suggestions []string

	if pickQuality < 28 {
		suggestions = append(suggestions, "Focus on selecting higher-quality cards based on GIHWR data")
	}
	if pickQuality < 24 {
		suggestions = append(suggestions, "Review 17Lands tier list before drafting to identify strong cards")
	}
	if color < 14 {
		suggestions = append(suggestions, "Stay in your colors earlier in the draft")
	}
	if deck < 18 {
		suggestions = append(suggestions, "Prioritize building a proper mana curve (2-3-4-5 drops)")
		suggestions = append(suggestions, "Include 3-5 removal spells in your deck")
	}
	if strategic < 10 {
		suggestions = append(suggestions, "Take bombs and removal higher priority")
		suggestions = append(suggestions, "Read signals from pack 1 to identify open colors")
	}

	if len(suggestions) == 0 {
		suggestions = append(suggestions, "Excellent draft! Keep up the strong decision-making")
	}
	return suggestions
}
