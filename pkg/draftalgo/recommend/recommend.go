// Package recommend provides the generative draft recommendation surface.
//
// It answers the question "which card in this pack should I take, and why?"
// using 17Lands GIHWR data injected via the same
// draftalgo.RatingsLookup / CardLookup interfaces pickquality.Analyze uses.
//
// Phase A (v0.3.7 MH-ML1):
//   - GIHWR-ranked TopPicks (top 3) + Alternatives (remainder)
//   - Plain-English Reason strings derived from rank tier — NO raw GIHWR %
//     in the primary Reason field (Prof gate requirement)
//   - Graceful N/A degrade when ratings cache is empty
//   - Archetype field is empty in Phase A (data not retained yet)
//
// Phase B (v0.3.8): extend with color-fit, ALSA, pool signals.
// Phase C (post-beta): archetype-conditioned ML win rates.
//
// This package does NOT call pickquality.Analyze — doing so per-card would
// be O(n²) and semantically wrong (Analyze grades a pick, not a pack).
// Instead both surfaces share the pkg/draftalgo/rank.ByGIHWR primitive so
// they agree on pack ordering.
package recommend

import (
	"github.com/RdHamilton/vault-mtg/pkg/draftalgo"
	"github.com/RdHamilton/vault-mtg/pkg/draftalgo/rank"
)

// Recommendation is one card recommendation entry.
//
// Reason is plain English — never a raw GIHWR percentage (Prof gate).
// GIHWR is available for callers that surface it in a secondary/tooltip
// field only. Archetype is empty in Phase A.
type Recommendation struct {
	CardID    string  // Arena card ID (string form)
	CardName  string  // Display name from CardLookup
	Priority  int     // 1–5, where 5 is the strongest recommendation
	Reason    string  // Plain-English explanation; never contains a raw "%" literal
	Archetype string  // Phase A: always empty; Phase B/C will populate this
	GIHWR     float64 // For callers that surface GIHWR in a secondary/tooltip display only
	HasGIHWR  bool    // false when no rating data is available
}

// Recommendations holds the full recommendation output for one pack.
type Recommendations struct {
	TopPicks     []Recommendation // Up to 3 top-ranked picks
	Alternatives []Recommendation // Remaining pack cards in rank order
}

// topPickCount is the number of entries in TopPicks. The rest go to
// Alternatives. This mirrors the original logparse stub's intent (top 3).
const topPickCount = 3

// Recommend ranks every card in packCardIDs by GIHWR descending and
// returns a Recommendations populated with plain-English Reason strings.
//
//   - format is the draft format string (e.g. "PremierDraft") forwarded to
//     ratings.GIHWR; pass "" when format is unknown.
//   - pool is the list of card IDs already in the player's draft pool
//     (previously picked cards). Phase A uses it only for pool size; Phase B
//     will use it for color-fit signals.
//   - ratings / cards satisfy the same interfaces pickquality.Analyze uses;
//     nil values produce graceful N/A results.
func Recommend(
	format string,
	packCardIDs []string,
	pool []string,
	ratings draftalgo.RatingsLookup,
	cards draftalgo.CardLookup,
) Recommendations {
	if len(packCardIDs) == 0 {
		return Recommendations{}
	}

	ranked := rank.ByGIHWR(format, packCardIDs, ratings, cards)

	// Detect whether any card in the pack has real rating data.
	hasAnyRatings := false
	for _, r := range ranked {
		if r.HasGIHWR {
			hasAnyRatings = true
			break
		}
	}

	altCap := len(ranked) - topPickCount
	if altCap < 0 {
		altCap = 0
	}
	out := Recommendations{
		TopPicks:     make([]Recommendation, 0, topPickCount),
		Alternatives: make([]Recommendation, 0, altCap),
	}

	for _, r := range ranked {
		rec := Recommendation{
			CardID:    r.CardID,
			CardName:  r.CardName,
			GIHWR:     r.GIHWR,
			HasGIHWR:  r.HasGIHWR,
			Archetype: "", // Phase A: no archetype data available
		}
		rec.Priority = priorityForRank(r.Rank, len(ranked))
		rec.Reason = reasonForRank(r.Rank, len(ranked), r.HasGIHWR, hasAnyRatings)

		if len(out.TopPicks) < topPickCount {
			out.TopPicks = append(out.TopPicks, rec)
		} else {
			out.Alternatives = append(out.Alternatives, rec)
		}
	}

	return out
}

// priorityForRank converts a 1-based pack rank to a 1–5 priority value.
// Rank 1 → 5 (highest), rank 2–3 → 4, rank 4–5 → 3, rank 6–8 → 2,
// rank 9+ → 1. Matches the original stub's 1-5 scale.
func priorityForRank(rank, _ int) int {
	switch {
	case rank == 1:
		return 5
	case rank <= 3:
		return 4
	case rank <= 5:
		return 3
	case rank <= 8:
		return 2
	default:
		return 1
	}
}

// reasonForRank produces a plain-English reason string for a card's rank.
//
// Contract: the returned string MUST NOT contain a "%" character — this
// is enforced by the test suite to satisfy the Prof PLAYER_VERDICT gate.
// GIHWR numbers are available via the GIHWR field for secondary display.
//
// Reason copy is intentionally kept in one place here so it can be revised
// against Prof feedback before the surface is considered player-final.
func reasonForRank(cardRank, packSize int, hasGIHWR, anyRatings bool) string {
	// Graceful N/A when no ratings are available for this format/set.
	if !anyRatings {
		return "No rating data available for this set"
	}
	// Single card in the pack is trivially the only option.
	if packSize == 1 {
		return "Only card in the pack"
	}
	if !hasGIHWR {
		return "No rating data for this card"
	}
	switch {
	case cardRank == 1:
		return "Best pick in the pack"
	case cardRank <= 3:
		return "Strong standalone pick"
	case cardRank <= 5:
		return "Solid pick"
	case cardRank <= 8:
		return "Situational pick"
	default:
		return "Low-rated for this format"
	}
}
