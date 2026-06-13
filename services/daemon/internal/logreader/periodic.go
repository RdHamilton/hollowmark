package logreader

import (
	"fmt"

	"github.com/RdHamilton/hollowmark/services/contract"
)

// IsPeriodicEntry reports whether the log entry is a PeriodicRewardsGetStatus
// response. Detection key: the presence of "_dailyRewardSequenceId" OR
// "_weeklyRewardSequenceId" as a direct top-level key in the parsed JSON.
//
// These keys appear at the top level of the entry (NOT nested under
// "ClientPeriodicRewards"). The chest-description keys alone are not
// sufficient — they appear in the same response but not in the stripped variant.
func IsPeriodicEntry(entry *LogEntry) bool {
	if entry == nil || !entry.IsJSON {
		return false
	}
	_, hasDaily := entry.JSON["_dailyRewardSequenceId"]
	_, hasWeekly := entry.JSON["_weeklyRewardSequenceId"]
	return hasDaily || hasWeekly
}

// ParsePeriodicEntry parses a PeriodicRewardsGetStatus log entry into a
// contract.PeriodicUpdatedPayload. Returns an error if the entry is nil,
// non-JSON, or lacks both sequence ID keys.
func ParsePeriodicEntry(entry *LogEntry) (*contract.PeriodicUpdatedPayload, error) {
	if entry == nil || !entry.IsJSON {
		return nil, fmt.Errorf("entry is not JSON")
	}
	if !IsPeriodicEntry(entry) {
		return nil, fmt.Errorf("entry does not contain _dailyRewardSequenceId or _weeklyRewardSequenceId")
	}

	p := &contract.PeriodicUpdatedPayload{}

	if v, ok := entry.JSON["_dailyRewardSequenceId"].(float64); ok {
		p.DailyWins = int(v)
	}
	if v, ok := entry.JSON["_weeklyRewardSequenceId"].(float64); ok {
		p.WeeklyWins = int(v)
	}

	return p, nil
}
