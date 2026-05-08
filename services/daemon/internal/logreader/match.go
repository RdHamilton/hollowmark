package logreader

import (
	"fmt"

	"github.com/RdHamilton/MTGA-Companion/services/contract"
)

// IsMatchCompletedEntry reports whether the log entry is a
// matchGameRoomStateChangedEvent with stateType
// "MatchGameRoomStateType_MatchCompleted".
//
// This is a stateless classifier — no GRE session buffering is required.
// Arena emits a single log line containing the full match result when the
// match ends.
func IsMatchCompletedEntry(entry *LogEntry) bool {
	if entry == nil || !entry.IsJSON {
		return false
	}
	event, ok := entry.JSON["matchGameRoomStateChangedEvent"].(map[string]interface{})
	if !ok {
		return false
	}
	gameRoomInfo, ok := event["gameRoomInfo"].(map[string]interface{})
	if !ok {
		return false
	}
	stateType, _ := gameRoomInfo["stateType"].(string)
	return stateType == "MatchGameRoomStateType_MatchCompleted"
}

// ParseMatchCompletedEntry parses a matchGameRoomStateChangedEvent log entry
// into a contract.MatchCompletedPayload.
//
// playerUserID is the local player's MTGA userId (from daemon config). It is
// used to identify the opponent in reservedPlayers. When empty the opponent
// name is omitted.
//
// Returns an error if the entry is not a valid match-completed event.
func ParseMatchCompletedEntry(entry *LogEntry, playerUserID string) (*contract.MatchCompletedPayload, error) {
	if entry == nil || !entry.IsJSON {
		return nil, fmt.Errorf("entry is not JSON")
	}

	event, ok := entry.JSON["matchGameRoomStateChangedEvent"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("entry does not contain matchGameRoomStateChangedEvent")
	}

	gameRoomInfo, ok := event["gameRoomInfo"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("matchGameRoomStateChangedEvent has no gameRoomInfo")
	}

	stateType, _ := gameRoomInfo["stateType"].(string)
	if stateType != "MatchGameRoomStateType_MatchCompleted" {
		return nil, fmt.Errorf("stateType is %q, expected MatchGameRoomStateType_MatchCompleted", stateType)
	}

	p := &contract.MatchCompletedPayload{
		ResultList: []contract.MatchResult{},
	}

	// --- finalMatchResult ---
	if fmr, ok := gameRoomInfo["finalMatchResult"].(map[string]interface{}); ok {
		if matchID, ok := fmr["matchId"].(string); ok {
			p.MatchID = matchID
		}

		if rl, ok := fmr["resultList"].([]interface{}); ok {
			for _, item := range rl {
				rm, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				r := contract.MatchResult{}
				if v, ok := rm["scope"].(string); ok {
					r.Scope = v
				}
				if v, ok := rm["result"].(string); ok {
					r.Result = v
				}
				if v, ok := rm["winningTeamId"].(float64); ok {
					r.WinningTeamID = int(v)
				}
				if v, ok := rm["reason"].(string); ok {
					r.Reason = v
				}
				p.ResultList = append(p.ResultList, r)

				// Capture top-level winner from the match-scope result.
				if r.Scope == "MatchScope_Match" {
					p.WinningTeamID = r.WinningTeamID
				}
			}
		}
	}

	// --- gameRoomConfig (format + opponent name) ---
	if cfg, ok := gameRoomInfo["gameRoomConfig"].(map[string]interface{}); ok {
		// Format comes from eventId (e.g. "Ladder", "QuickDraft_SOS_20260430").
		if eventID, ok := cfg["eventId"].(string); ok {
			p.Format = eventID
		}

		// Opponent: iterate reservedPlayers and take the player whose userId
		// does NOT match the local player.
		if players, ok := cfg["reservedPlayers"].([]interface{}); ok {
			for _, pl := range players {
				pm, ok := pl.(map[string]interface{})
				if !ok {
					continue
				}
				uid, _ := pm["userId"].(string)
				name, _ := pm["playerName"].(string)
				if uid == "" {
					continue
				}
				// If we know the local user, skip their entry.
				if playerUserID != "" && uid == playerUserID {
					continue
				}
				if name != "" {
					p.OpponentName = name
					break
				}
			}
		}
	}

	return p, nil
}
