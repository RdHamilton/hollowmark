package logreader

import (
	"fmt"

	"github.com/RdHamilton/hollowmark/services/contract"
)

// IsMasteryEntry reports whether the log entry is an InventoryInfo entry that
// carries a MasteryPass nested object. Used by handleEntry to emit a standalone
// "mastery.updated" event alongside "inventory.updated" for the same log line,
// making mastery progression independently replayable.
func IsMasteryEntry(entry *LogEntry) bool {
	if entry == nil || !entry.IsJSON {
		return false
	}
	invMap, ok := entry.JSON["InventoryInfo"].(map[string]interface{})
	if !ok {
		return false
	}
	_, hasMastery := invMap["MasteryPass"]
	return hasMastery
}

// ParseMasteryUpdatedEntry parses an InventoryInfo log entry's MasteryPass
// object into a contract.MasteryUpdatedPayload. Returns an error if the entry
// is nil, not a JSON entry, lacks InventoryInfo, or InventoryInfo lacks MasteryPass.
func ParseMasteryUpdatedEntry(entry *LogEntry) (*contract.MasteryUpdatedPayload, error) {
	if entry == nil || !entry.IsJSON {
		return nil, fmt.Errorf("entry is not JSON")
	}
	invMap, ok := entry.JSON["InventoryInfo"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("entry does not contain InventoryInfo")
	}
	mpRaw, ok := invMap["MasteryPass"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("InventoryInfo does not contain MasteryPass")
	}

	p := &contract.MasteryUpdatedPayload{}
	if v, ok := mpRaw["CurrentLevel"].(float64); ok {
		p.Level = int(v)
	}
	if v, ok := mpRaw["PassType"].(string); ok {
		p.PassType = v
	}
	if v, ok := mpRaw["MaxLevel"].(float64); ok {
		p.Max = int(v)
	}

	return p, nil
}
