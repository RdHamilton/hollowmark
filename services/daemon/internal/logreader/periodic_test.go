package logreader

// periodic_test.go — TDD tests for #1344: ParsePeriodicEntry must read the
// top-level "_dailyRewardSequenceId" and "_weeklyRewardSequenceId" keys from a
// PeriodicRewardsGetStatus log entry and return a contract.PeriodicUpdatedPayload.
//
// These keys appear DIRECTLY at the top level of the parsed log entry, not
// nested under a "ClientPeriodicRewards" wrapper (the wrapper format is a dead
// historical variant; see docs/engineering/reference/mtga-log-research.md).

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// periodicEntry builds a synthetic LogEntry that mirrors the real
// PeriodicRewardsGetStatus Arena API response with sequence IDs present.
func periodicEntry(daily, weekly int) *LogEntry {
	return &LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"_dailyRewardSequenceId":         float64(daily),
			"_weeklyRewardSequenceId":        float64(weekly),
			"_dailyRewardResetTimestamp":     "2026-06-12T09:00:00Z",
			"_weeklyRewardResetTimestamp":    "2026-06-09T09:00:00Z",
			"_dailyRewardChestDescriptions":  map[string]interface{}{},
			"_weeklyRewardChestDescriptions": map[string]interface{}{},
		},
	}
}

// TestIsPeriodicEntry_True verifies that an entry with _dailyRewardSequenceId
// at the top level is identified as a periodic entry.
func TestIsPeriodicEntry_True(t *testing.T) {
	assert.True(t, IsPeriodicEntry(periodicEntry(4, 7)))
}

// TestIsPeriodicEntry_WeeklyOnly verifies that _weeklyRewardSequenceId alone
// is enough to classify the entry as periodic.
func TestIsPeriodicEntry_WeeklyOnly(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"_weeklyRewardSequenceId": float64(3)},
	}
	assert.True(t, IsPeriodicEntry(entry))
}

// TestIsPeriodicEntry_False_NoKeys verifies that an entry without either
// sequence ID key is not a periodic entry.
func TestIsPeriodicEntry_False_NoKeys(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"_dailyRewardChestDescriptions": map[string]interface{}{}},
	}
	assert.False(t, IsPeriodicEntry(entry))
}

// TestIsPeriodicEntry_False_Nil verifies nil input returns false.
func TestIsPeriodicEntry_False_Nil(t *testing.T) {
	assert.False(t, IsPeriodicEntry(nil))
}

// TestIsPeriodicEntry_False_NotJSON verifies non-JSON entry returns false.
func TestIsPeriodicEntry_False_NotJSON(t *testing.T) {
	assert.False(t, IsPeriodicEntry(&LogEntry{IsJSON: false, Raw: "plain text"}))
}

// TestParsePeriodicEntry_PopulatesBothFields verifies that ParsePeriodicEntry
// correctly reads both sequence IDs into the payload.
func TestParsePeriodicEntry_PopulatesBothFields(t *testing.T) {
	p, err := ParsePeriodicEntry(periodicEntry(4, 7))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, 4, p.DailyWins, "DailyWins must equal _dailyRewardSequenceId")
	assert.Equal(t, 7, p.WeeklyWins, "WeeklyWins must equal _weeklyRewardSequenceId")
}

// TestParsePeriodicEntry_ZeroValues verifies that zero sequence IDs are
// preserved rather than treated as absent.
func TestParsePeriodicEntry_ZeroValues(t *testing.T) {
	p, err := ParsePeriodicEntry(periodicEntry(0, 0))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, 0, p.DailyWins)
	assert.Equal(t, 0, p.WeeklyWins)
}

// TestParsePeriodicEntry_MaxValues verifies the maximum track position (15/15).
func TestParsePeriodicEntry_MaxValues(t *testing.T) {
	p, err := ParsePeriodicEntry(periodicEntry(15, 15))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, 15, p.DailyWins)
	assert.Equal(t, 15, p.WeeklyWins)
}

// TestParsePeriodicEntry_DailyOnlyPresent verifies that when only the daily
// sequence ID is present, weekly defaults to zero.
func TestParsePeriodicEntry_DailyOnlyPresent(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"_dailyRewardSequenceId": float64(5)},
	}
	p, err := ParsePeriodicEntry(entry)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, 5, p.DailyWins)
	assert.Equal(t, 0, p.WeeklyWins)
}

// TestParsePeriodicEntry_Error_NotPeriodicEntry verifies that passing a
// non-periodic entry returns an error.
func TestParsePeriodicEntry_Error_NotPeriodicEntry(t *testing.T) {
	entry := &LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"quests": []interface{}{}},
	}
	p, err := ParsePeriodicEntry(entry)
	assert.Error(t, err)
	assert.Nil(t, p)
}

// TestParsePeriodicEntry_Error_Nil verifies that nil input returns an error.
func TestParsePeriodicEntry_Error_Nil(t *testing.T) {
	p, err := ParsePeriodicEntry(nil)
	assert.Error(t, err)
	assert.Nil(t, p)
}
