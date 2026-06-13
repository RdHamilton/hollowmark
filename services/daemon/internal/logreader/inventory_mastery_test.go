package logreader

// inventory_mastery_test.go — TDD tests for #1338: ParseInventoryEntry must
// read the MasteryPass object from InventoryInfo and populate the Mastery field
// on InventoryUpdatedPayload.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inventoryEntryWithMastery builds a synthetic LogEntry that mirrors the real
// Arena login blob with a MasteryPass nested object inside InventoryInfo.
func inventoryEntryWithMastery() *LogEntry {
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
				"MasteryPass": map[string]interface{}{
					"CurrentLevel": float64(42),
					"PassType":     "Standard",
					"MaxLevel":     float64(80),
				},
			},
		},
	}
}

// TestParseInventoryEntry_PopulatesMastery verifies that ParseInventoryEntry
// reads the MasteryPass nested object from InventoryInfo and maps CurrentLevel,
// PassType, and MaxLevel onto the returned payload's Mastery field.
func TestParseInventoryEntry_PopulatesMastery(t *testing.T) {
	p, err := ParseInventoryEntry(inventoryEntryWithMastery())
	require.NoError(t, err)
	require.NotNil(t, p)

	require.NotNil(t, p.Mastery, "Mastery must not be nil when MasteryPass is present")
	assert.Equal(t, 42, p.Mastery.Level, "Level must equal CurrentLevel")
	assert.Equal(t, "Standard", p.Mastery.PassType, "PassType must match")
	assert.Equal(t, 80, p.Mastery.Max, "Max must equal MaxLevel")
}

// TestParseInventoryEntry_NoMasteryPass_MasteryNil verifies that when the
// MasteryPass key is absent (e.g. older Arena versions or stripped fixtures),
// the Mastery field is nil — the existing InventoryInfo parse still succeeds.
func TestParseInventoryEntry_NoMasteryPass_MasteryNil(t *testing.T) {
	entry := inventoryEntry() // existing helper — no MasteryPass key
	p, err := ParseInventoryEntry(entry)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Nil(t, p.Mastery, "missing MasteryPass key must leave Mastery nil")
	// Existing fields must still be populated.
	assert.Equal(t, 1200, p.Gems)
}

// TestParseInventoryEntry_MasteryPass_AllZeroLevels verifies that a MasteryPass
// with zero values produces a non-nil Mastery with all zero/empty fields.
func TestParseInventoryEntry_MasteryPass_AllZeroLevels(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems": float64(100),
				"MasteryPass": map[string]interface{}{
					"CurrentLevel": float64(0),
					"PassType":     "Basic",
					"MaxLevel":     float64(0),
				},
			},
		},
	}
	p, err := ParseInventoryEntry(entry)
	require.NoError(t, err)
	require.NotNil(t, p)
	require.NotNil(t, p.Mastery)
	assert.Equal(t, 0, p.Mastery.Level)
	assert.Equal(t, "Basic", p.Mastery.PassType)
	assert.Equal(t, 0, p.Mastery.Max)
}

// TestParseInventoryEntry_MasteryAndDecks_BothPopulated verifies that an entry
// carrying both DeckSummaries and a MasteryPass correctly populates both the
// Decks and Mastery fields — they do not interfere with each other.
func TestParseInventoryEntry_MasteryAndDecks_BothPopulated(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems": float64(500),
				"MasteryPass": map[string]interface{}{
					"CurrentLevel": float64(10),
					"PassType":     "Premium",
					"MaxLevel":     float64(80),
				},
			},
			"DeckSummaries": []interface{}{
				map[string]interface{}{
					"DeckId": "deck-aaa",
					"Name":   "Test Deck",
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

	require.NotNil(t, p.Mastery)
	assert.Equal(t, 10, p.Mastery.Level)
	assert.Equal(t, "Premium", p.Mastery.PassType)
	assert.Equal(t, 80, p.Mastery.Max)

	require.Len(t, p.Decks, 1)
	assert.Equal(t, "deck-aaa", p.Decks[0].DeckID)
}
