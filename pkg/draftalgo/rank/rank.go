// Package rank provides the shared GIHWR-ranking primitive used by both
// the pickquality grading surface and the recommend recommendation surface.
//
// Extracting the sort here guarantees that the two surfaces always agree
// on which card is best in a pack — they share one code path rather than
// two independently maintained sort loops.
package rank

import (
	"sort"

	"github.com/RdHamilton/hollowmark/pkg/draftalgo"
)

// Card is one entry in a ranked pack — a card ID with its name, GIHWR,
// 1-based rank, and a flag indicating whether rating data was available.
type Card struct {
	CardID   string
	CardName string
	GIHWR    float64
	Rank     int  // 1-indexed; 1 = highest GIHWR
	HasGIHWR bool // false when no rating exists for this card+format
}

// ByGIHWR ranks every card in packCardIDs by their 17Lands GIHWR
// descending. Cards with no rating are assigned GIHWR=0 and placed at
// the end of the list; within a tied GIHWR group the original input
// order is preserved (stable sort).
//
// This is the shared primitive: pickquality.Analyze uses it to compute
// pack rank for grading, and recommend.Recommend uses it to build the
// TopPicks/Alternatives split. Both surfaces call this function so that
// they can never disagree about which card is best.
func ByGIHWR(
	format string,
	packCardIDs []string,
	ratings draftalgo.RatingsLookup,
	cards draftalgo.CardLookup,
) []Card {
	if len(packCardIDs) == 0 {
		return nil
	}

	entries := make([]Card, 0, len(packCardIDs))
	for _, id := range packCardIDs {
		var gihwr float64
		var hasGIHWR bool
		if ratings != nil {
			if v, ok := ratings.GIHWR(id, format); ok {
				gihwr = v
				hasGIHWR = true
			}
		}
		name := ""
		if cards != nil {
			name = cards.CardName(id)
		}
		if name == "" {
			name = "Unknown Card"
		}
		entries = append(entries, Card{
			CardID:   id,
			CardName: name,
			GIHWR:    gihwr,
			HasGIHWR: hasGIHWR,
		})
	}

	// Stable sort descending by GIHWR. Stable preserves input order for
	// cards with equal GIHWR (no fabricated tiebreak signal).
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].GIHWR > entries[j].GIHWR
	})

	// Assign 1-based ranks after the sort.
	for i := range entries {
		entries[i].Rank = i + 1
	}

	return entries
}
