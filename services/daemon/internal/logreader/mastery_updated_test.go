package logreader

// mastery_updated_test.go — TDD tests for #1344: ParseMasteryUpdatedEntry must
// extract InventoryInfo.MasteryPass into a contract.MasteryUpdatedPayload, and
// IsMasteryEntry must correctly detect which InventoryInfo entries carry MasteryPass.
//
// This is a STANDALONE mastery event parser, distinct from ParseInventoryEntry
// (which already populates InventoryUpdatedPayload.Mastery). The standalone
// version allows handleEntry to emit "mastery.updated" alongside
// "inventory.updated" for the same log line, making mastery independently
// replayable without re-projecting the full inventory.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// inventoryEntryWithLevel builds a LogEntry with InventoryInfo.MasteryPass.CurrentLevel=level.
func inventoryEntryWithLevel(level int) *LogEntry {
	return &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems": float64(1200),
				"Gold": float64(5000),
				"MasteryPass": map[string]interface{}{
					"CurrentLevel": float64(level),
					"PassType":     "Standard",
					"MaxLevel":     float64(80),
				},
			},
		},
	}
}

// TestParseMasteryUpdatedEntry_PopulatesFields verifies that
// ParseMasteryUpdatedEntry correctly extracts all three MasteryPass fields.
func TestParseMasteryUpdatedEntry_PopulatesFields(t *testing.T) {
	entry := inventoryEntryWithLevel(18)
	p, err := ParseMasteryUpdatedEntry(entry)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, 18, p.Level, "Level must equal MasteryPass.CurrentLevel")
	assert.Equal(t, "Standard", p.PassType)
	assert.Equal(t, 80, p.Max)
}

// TestParseMasteryUpdatedEntry_ZeroLevel verifies that CurrentLevel=0 produces
// a payload with Level=0 (not treated as absent).
func TestParseMasteryUpdatedEntry_ZeroLevel(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems": float64(100),
				"MasteryPass": map[string]interface{}{
					"CurrentLevel": float64(0),
					"PassType":     "Basic",
					"MaxLevel":     float64(80),
				},
			},
		},
	}
	p, err := ParseMasteryUpdatedEntry(entry)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, 0, p.Level)
	assert.Equal(t, "Basic", p.PassType)
}

// TestParseMasteryUpdatedEntry_Error_NoMasteryPass verifies that an InventoryInfo
// entry without a MasteryPass key returns an error.
func TestParseMasteryUpdatedEntry_Error_NoMasteryPass(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems": float64(1200),
			},
		},
	}
	p, err := ParseMasteryUpdatedEntry(entry)
	assert.Error(t, err)
	assert.Nil(t, p)
}

// TestParseMasteryUpdatedEntry_Error_Nil verifies that nil input returns an error.
func TestParseMasteryUpdatedEntry_Error_Nil(t *testing.T) {
	p, err := ParseMasteryUpdatedEntry(nil)
	assert.Error(t, err)
	assert.Nil(t, p)
}

// TestParseMasteryUpdatedEntry_Error_NonInventoryEntry verifies that a
// non-inventory entry (no InventoryInfo key) returns an error.
func TestParseMasteryUpdatedEntry_Error_NonInventoryEntry(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"quests": []interface{}{}},
	}
	p, err := ParseMasteryUpdatedEntry(entry)
	assert.Error(t, err)
	assert.Nil(t, p)
}
