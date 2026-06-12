package logreader

// inventory_deck_summaries_test.go — TDD tests for #1337: ParseInventoryEntry
// must read the top-level DeckSummaries sibling of InventoryInfo and populate
// the Decks field on InventoryUpdatedPayload.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inventoryEntryWithDecks builds a synthetic LogEntry that mirrors the real
// Arena login blob: InventoryInfo and DeckSummaries are top-level siblings.
func inventoryEntryWithDecks() *LogEntry {
	return &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems":               float64(1200),
				"Gold":               float64(5000),
				"TotalVaultProgress": float64(75),
				"WildCardCommons":    float64(10),
				"WildCardUnCommons":  float64(5),
				"WildCardRares":      float64(3),
				"WildCardMythics":    float64(1),
			},
			"DeckSummaries": []interface{}{
				map[string]interface{}{
					"DeckId": "deck-uuid-001",
					"Name":   "Mono-White Lifegain",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Format", "value": "Standard"},
					},
				},
				map[string]interface{}{
					"DeckId": "deck-uuid-002",
					"Name":   "Izzet Tempo",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Version", "value": "5"},
						map[string]interface{}{"name": "Format", "value": "Alchemy"},
					},
				},
				map[string]interface{}{
					"DeckId": "deck-uuid-003",
					"Name":   "Deck Without Format",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Version", "value": "1"},
					},
				},
			},
		},
	}
}

// TestParseInventoryEntry_PopulatesDecks verifies that ParseInventoryEntry reads
// DeckSummaries from the top-level sibling key and maps DeckId, Name, and
// Format onto the returned payload's Decks slice.
func TestParseInventoryEntry_PopulatesDecks(t *testing.T) {
	p, err := ParseInventoryEntry(inventoryEntryWithDecks())
	require.NoError(t, err)
	require.NotNil(t, p)

	require.Len(t, p.Decks, 3, "must parse all 3 DeckSummaries entries")

	assert.Equal(t, "deck-uuid-001", p.Decks[0].DeckID)
	assert.Equal(t, "Mono-White Lifegain", p.Decks[0].Name)
	assert.Equal(t, "Standard", p.Decks[0].Format)

	assert.Equal(t, "deck-uuid-002", p.Decks[1].DeckID)
	assert.Equal(t, "Izzet Tempo", p.Decks[1].Name)
	assert.Equal(t, "Alchemy", p.Decks[1].Format)
}

// TestParseInventoryEntry_DeckWithoutFormat_HasEmptyFormat verifies that a
// DeckSummaries entry with no Format Attribute maps to an empty Format string.
// Ray's amendment 2: only overwrite format when the incoming value is non-empty
// — this is the source-side invariant for the BFF upsert guard.
func TestParseInventoryEntry_DeckWithoutFormat_HasEmptyFormat(t *testing.T) {
	p, err := ParseInventoryEntry(inventoryEntryWithDecks())
	require.NoError(t, err)
	require.NotNil(t, p)
	require.Len(t, p.Decks, 3)

	// Deck 3 has no Format attribute.
	assert.Equal(t, "", p.Decks[2].Format, "deck with no Format attribute must map to empty string")
}

// TestParseInventoryEntry_NoDeckSummaries_ReturnsEmptyDecks verifies that when
// the DeckSummaries key is absent (e.g. old Arena version), Decks is nil/empty
// rather than an error — the existing InventoryInfo parse still succeeds.
func TestParseInventoryEntry_NoDeckSummaries_ReturnsEmptyDecks(t *testing.T) {
	entry := inventoryEntry() // existing helper — no DeckSummaries key
	p, err := ParseInventoryEntry(entry)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Empty(t, p.Decks, "missing DeckSummaries key must not produce an error")
	// Existing fields must still be populated.
	assert.Equal(t, 1200, p.Gems)
}

// TestParseInventoryEntry_DeckSummaries_EmptySlice verifies that an empty
// DeckSummaries array results in an empty Decks slice, not a nil one.
func TestParseInventoryEntry_DeckSummaries_EmptySlice(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems": float64(100),
			},
			"DeckSummaries": []interface{}{},
		},
	}
	p, err := ParseInventoryEntry(entry)
	require.NoError(t, err)
	assert.Empty(t, p.Decks, "empty DeckSummaries array must produce empty Decks slice")
}

// TestParseInventoryEntry_DeckMissingDeckId_Skipped verifies that a
// DeckSummaries entry with no DeckId is skipped (not added to p.Decks).
func TestParseInventoryEntry_DeckMissingDeckId_Skipped(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems": float64(0),
			},
			"DeckSummaries": []interface{}{
				map[string]interface{}{
					// No DeckId
					"Name": "Invalid Deck",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Format", "value": "Standard"},
					},
				},
				map[string]interface{}{
					"DeckId": "valid-deck-uuid",
					"Name":   "Valid Deck",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Format", "value": "Historic"},
					},
				},
			},
		},
	}
	p, err := ParseInventoryEntry(entry)
	require.NoError(t, err)
	require.Len(t, p.Decks, 1, "entry missing DeckId must be skipped")
	assert.Equal(t, "valid-deck-uuid", p.Decks[0].DeckID)
}

// ---------------------------------------------------------------------------
// Real fixture golden test
// ---------------------------------------------------------------------------

// TestRealFixture_InventoryWithDeckSummaries_2026_60 asserts that the real
// fixture parses both InventoryInfo fields and DeckSummaries correctly.
// Fixture provenance: MANIFEST.md — REAL-DERIVED 2026-06-12, #1337.
func TestRealFixture_InventoryWithDeckSummaries_2026_60(t *testing.T) {
	entry := loadRealFixture(t, "inventory_with_decksummaries_2026.60.log")

	require.True(t, IsInventoryEntry(entry), "fixture must classify as inventory entry")

	p, err := ParseInventoryEntry(entry)
	require.NoError(t, err)
	require.NotNil(t, p)

	// InventoryInfo fields — real game values from fixture.
	assert.Equal(t, 3125, p.Gems)
	assert.Equal(t, 70225, p.Gold)
	assert.Equal(t, 184, p.TotalVaultProgress)
	assert.Equal(t, 40, p.WildCardCommons)
	assert.Equal(t, 25, p.WildCardUncommons)
	assert.Equal(t, 9, p.WildCardRares)
	assert.Equal(t, 20, p.WildCardMythics)
	require.Len(t, p.Boosters, 1)
	assert.Equal(t, "SOS", p.Boosters[0].SetCode)
	assert.Equal(t, 6, p.Boosters[0].Count)

	// DeckSummaries — 3 decks from the real fixture.
	require.Len(t, p.Decks, 3, "fixture must contain 3 DeckSummaries entries")

	assert.Equal(t, "00000002-0000-4000-8000-000000000001", p.Decks[0].DeckID)
	assert.Equal(t, "Deck 1 (Standard)", p.Decks[0].Name)
	assert.Equal(t, "Standard", p.Decks[0].Format)

	assert.Equal(t, "00000002-0000-4000-8000-000000000002", p.Decks[1].DeckID)
	assert.Equal(t, "Standard", p.Decks[1].Format)

	assert.Equal(t, "00000002-0000-4000-8000-000000000003", p.Decks[2].DeckID)
	assert.Equal(t, "Alchemy", p.Decks[2].Format)
}
