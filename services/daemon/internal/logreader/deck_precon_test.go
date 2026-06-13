package logreader

// deck_precon_test.go — TDD tests for the precon-deck filter and cleanDeckName
// port, part of the deck-defect fix (#1341).
//
// Defect 1: ParseDeckEntry and ParseInventoryEntry must silently skip precon /
// system decks identified by the name prefix "?=?Loc/Decks/Precon/".
// Defect 2: ParseDeckEntry must resolve any remaining "?=?Loc/…" name key by
// stripping the prefix and converting the last path segment into a
// human-readable string (e.g. "?=?Loc/Decks/Precon/FNM_Brawl_Kaza" →
// "FNM Brawl Kaza").  The precon filter runs BEFORE cleanDeckName so loc-key
// precon decks are dropped, not cleaned and kept.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// isPreconDeck helper
// ---------------------------------------------------------------------------

func TestIsPreconDeck_PreconPrefix_ReturnsTrue(t *testing.T) {
	assert.True(t, isPreconDeck("?=?Loc/Decks/Precon/FNM_Brawl_Kaza"))
}

func TestIsPreconDeck_PreconPrefixVariants_ReturnTrue(t *testing.T) {
	cases := []string{
		"?=?Loc/Decks/Precon/Precon_EPP2024_UW",
		"?=?Loc/Decks/Precon/Precon_TLA_StoryDeck_Book1",
		"?=?Loc/Decks/Precon/StarterKit_2025",
		"?=?Loc/Decks/Precon/",
	}
	for _, name := range cases {
		assert.True(t, isPreconDeck(name), "expected precon: %q", name)
	}
}

func TestIsPreconDeck_PlayerDeck_ReturnsFalse(t *testing.T) {
	assert.False(t, isPreconDeck("Mono-White Lifegain"))
}

func TestIsPreconDeck_OtherLocPrefix_ReturnsFalse(t *testing.T) {
	// A different Loc/ path should NOT be treated as precon.
	assert.False(t, isPreconDeck("?=?Loc/Decks/Starter/SomeDeck"))
}

func TestIsPreconDeck_EmptyString_ReturnsFalse(t *testing.T) {
	assert.False(t, isPreconDeck(""))
}

// ---------------------------------------------------------------------------
// cleanDeckName helper
// ---------------------------------------------------------------------------

func TestCleanDeckName_NoLocPrefix_Unchanged(t *testing.T) {
	assert.Equal(t, "Mono-White Lifegain", cleanDeckName("Mono-White Lifegain"))
}

func TestCleanDeckName_LocPrefix_LastSegmentUnderscoreToSpace(t *testing.T) {
	// The precon filter drops "?=?Loc/Decks/Precon/…" before this runs,
	// but we still port cleanDeckName for any other Loc key paths.
	assert.Equal(t, "My Deck Name", cleanDeckName("?=?Loc/Some/Other/My_Deck_Name"))
}

func TestCleanDeckName_PreconLocKey_LastSegmentUnderscoreToSpace(t *testing.T) {
	// Defensive: if somehow a precon name reaches cleanDeckName, it still
	// produces a readable string rather than a raw key.
	assert.Equal(t, "FNM Brawl Kaza", cleanDeckName("?=?Loc/Decks/Precon/FNM_Brawl_Kaza"))
}

func TestCleanDeckName_NoSlashAfterPrefix_ReturnsOriginal(t *testing.T) {
	// Edge: "?=?Loc/" with no further path — last segment is empty, return original.
	assert.Equal(t, "?=?Loc/", cleanDeckName("?=?Loc/"))
}

func TestCleanDeckName_EmptyString_Unchanged(t *testing.T) {
	assert.Equal(t, "", cleanDeckName(""))
}

// ---------------------------------------------------------------------------
// ParseDeckEntry — precon filtering
// ---------------------------------------------------------------------------

// preconDeckEntry builds a synthetic DeckUpsertDeckV2 entry whose deck name
// carries the precon loc prefix.
func preconDeckEntry() *LogEntry {
	req := `{"Summary":{"DeckId":"some-uuid","Name":"?=?Loc/Decks/Precon/FNM_Brawl_Kaza","Attributes":[{"name":"Format","value":"Brawl"}]},"Deck":{"MainDeck":[{"cardId":11111,"quantity":4}],"Sideboard":[]}}`
	return &LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": req},
	}
}

// TestParseDeckEntry_PreconDeck_ReturnsNilNoError verifies that a deck whose
// name starts with "?=?Loc/Decks/Precon/" is silently skipped: both return
// values are nil/nil so the caller knows to discard it.
func TestParseDeckEntry_PreconDeck_ReturnsNilNoError(t *testing.T) {
	p, err := ParseDeckEntry(preconDeckEntry())
	require.NoError(t, err)
	assert.Nil(t, p, "precon deck must be skipped (nil payload, no error)")
}

// TestParseDeckEntry_NonPreconDeck_IsNotSkipped verifies that a normal player
// deck still parses successfully after the precon filter is added.
func TestParseDeckEntry_NonPreconDeck_IsNotSkipped(t *testing.T) {
	req := `{"Summary":{"DeckId":"player-deck-001","Name":"Mono-White Lifegain","Attributes":[{"name":"Format","value":"Standard"}]},"Deck":{"MainDeck":[],"Sideboard":[]}}`
	entry := &LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": req},
	}
	p, err := ParseDeckEntry(entry)
	require.NoError(t, err)
	require.NotNil(t, p, "player deck must not be skipped")
	assert.Equal(t, "player-deck-001", p.DeckID)
}

// ---------------------------------------------------------------------------
// ParseDeckEntry — cleanDeckName for non-precon loc keys
// ---------------------------------------------------------------------------

// TestParseDeckEntry_LocKeyName_IsCleaned verifies that a non-precon deck with
// a "?=?Loc/" name (edge case) has its name resolved by cleanDeckName rather
// than stored raw.
func TestParseDeckEntry_LocKeyName_IsCleaned(t *testing.T) {
	req := `{"Summary":{"DeckId":"some-deck","Name":"?=?Loc/Decks/Event/My_Event_Deck","Attributes":[{"name":"Format","value":"Standard"}]},"Deck":{"MainDeck":[],"Sideboard":[]}}`
	entry := &LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": req},
	}
	p, err := ParseDeckEntry(entry)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "My Event Deck", p.Name, "loc key name must be cleaned to human-readable")
}

// TestParseDeckEntry_NormalName_Unchanged verifies that a plain player deck
// name is stored unchanged after cleanDeckName runs.
func TestParseDeckEntry_NormalName_Unchanged(t *testing.T) {
	req := `{"Summary":{"DeckId":"player-deck","Name":"Izzet Tempo","Attributes":[{"name":"Format","value":"Alchemy"}]},"Deck":{"MainDeck":[],"Sideboard":[]}}`
	entry := &LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": req},
	}
	p, err := ParseDeckEntry(entry)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "Izzet Tempo", p.Name)
}

// ---------------------------------------------------------------------------
// ParseInventoryEntry — precon filtering in DeckSummaries
// ---------------------------------------------------------------------------

// inventoryEntryWithPreconDecks builds an entry whose DeckSummaries list
// contains a mix of player decks and precon system decks.
func inventoryEntryWithPreconDecks() *LogEntry {
	return &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems": float64(100),
				"Gold": float64(500),
			},
			"DeckSummaries": []interface{}{
				// Player deck — must be kept.
				map[string]interface{}{
					"DeckId": "player-deck-uuid-001",
					"Name":   "Mono-White Lifegain",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Format", "value": "Standard"},
					},
				},
				// Precon system deck — must be skipped.
				map[string]interface{}{
					"DeckId": "some-precon-uuid",
					"Name":   "?=?Loc/Decks/Precon/FNM_Brawl_Kaza",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Format", "value": "Brawl"},
					},
				},
				// Another precon — must be skipped.
				map[string]interface{}{
					"DeckId": "another-precon-uuid",
					"Name":   "?=?Loc/Decks/Precon/Precon_EPP2024_UW",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Format", "value": "Alchemy"},
					},
				},
				// Player deck — must be kept.
				map[string]interface{}{
					"DeckId": "player-deck-uuid-002",
					"Name":   "Izzet Tempo",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Format", "value": "Alchemy"},
					},
				},
			},
		},
	}
}

// TestParseInventoryEntry_PreconDecksAreFiltered verifies that DeckSummaries
// entries with the "?=?Loc/Decks/Precon/" name prefix are excluded from the
// returned Decks slice while valid player decks are preserved.
func TestParseInventoryEntry_PreconDecksAreFiltered(t *testing.T) {
	p, err := ParseInventoryEntry(inventoryEntryWithPreconDecks())
	require.NoError(t, err)
	require.NotNil(t, p)

	require.Len(t, p.Decks, 2, "must retain only the 2 player decks; 2 precon decks must be filtered")

	assert.Equal(t, "player-deck-uuid-001", p.Decks[0].DeckID)
	assert.Equal(t, "Mono-White Lifegain", p.Decks[0].Name)

	assert.Equal(t, "player-deck-uuid-002", p.Decks[1].DeckID)
	assert.Equal(t, "Izzet Tempo", p.Decks[1].Name)
}

// TestParseInventoryEntry_AllPrecon_ReturnsEmptyDecks verifies that when every
// DeckSummary is a precon deck the result is an empty (not nil) Decks slice.
func TestParseInventoryEntry_AllPrecon_ReturnsEmptyDecks(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{"Gems": float64(0)},
			"DeckSummaries": []interface{}{
				map[string]interface{}{
					"DeckId": "precon-only",
					"Name":   "?=?Loc/Decks/Precon/StarterKit_2025",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Format", "value": "Standard"},
					},
				},
			},
		},
	}
	p, err := ParseInventoryEntry(entry)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Empty(t, p.Decks, "all-precon DeckSummaries must result in empty Decks slice")
}

// TestParseInventoryEntry_NoPrecon_AllKept verifies that when no precon decks
// are present, all DeckSummaries entries are kept (regression guard for the
// filter not over-applying).
func TestParseInventoryEntry_NoPrecon_AllKept(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{"Gems": float64(0)},
			"DeckSummaries": []interface{}{
				map[string]interface{}{
					"DeckId": "deck-a",
					"Name":   "Aggro Red",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Format", "value": "Standard"},
					},
				},
				map[string]interface{}{
					"DeckId": "deck-b",
					"Name":   "Control Blue",
					"Attributes": []interface{}{
						map[string]interface{}{"name": "Format", "value": "Historic"},
					},
				},
			},
		},
	}
	p, err := ParseInventoryEntry(entry)
	require.NoError(t, err)
	require.Len(t, p.Decks, 2, "no precon decks means all entries kept")
}
