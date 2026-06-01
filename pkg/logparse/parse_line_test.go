package logparse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseLine_NonJSON verifies ParseLine returns a non-nil LogEntry with IsJSON=false
// for a plain-text line that contains no JSON.
func TestParseLine_NonJSON(t *testing.T) {
	entry := ParseLine("this is a plain text line with no JSON")
	require.NotNil(t, entry)
	assert.False(t, entry.IsJSON)
	assert.Equal(t, "this is a plain text line with no JSON", entry.Raw)
}

// TestParseLine_ValidJSON verifies ParseLine parses the JSON portion and sets IsJSON=true.
func TestParseLine_ValidJSON(t *testing.T) {
	raw := `[UnityCrossThreadLogger]2024-01-15 {"greToClientEvent":{"greToClientMessages":[]}}`
	entry := ParseLine(raw)
	require.NotNil(t, entry)
	assert.True(t, entry.IsJSON)
	_, ok := entry.JSON["greToClientEvent"]
	assert.True(t, ok, "expected greToClientEvent key in parsed JSON")
	assert.Equal(t, raw, entry.Raw)
}

// TestParseLine_EmptyString verifies ParseLine handles an empty string without panicking.
func TestParseLine_EmptyString(t *testing.T) {
	entry := ParseLine("")
	require.NotNil(t, entry)
	assert.False(t, entry.IsJSON)
	assert.Equal(t, "", entry.Raw)
}

// TestGamePlayEvent_TeamIDField verifies that GamePlayEvent has a TeamID field and
// that detectLifeChanges populates it from GREPlayerState.TeamID.
func TestGamePlayEvent_TeamIDField(t *testing.T) {
	// Build a minimal two-message sequence: message 0 sets up the initial life
	// total; message 1 has a changed life total so detectLifeChanges fires.
	// Both messages include a player with TeamID=2 at SeatID=1.
	msg0 := &GREGameStateMessage{
		Players: []GREPlayerState{
			{SeatID: 1, LifeTotal: 20, TeamID: 2},
			{SeatID: 2, LifeTotal: 20, TeamID: 1},
		},
	}
	msg1 := &GREGameStateMessage{
		MatchID:    "match-abc",
		GameNumber: 1,
		TurnInfo:   &GRETurnInfo{TurnNumber: 3, Phase: "Phase_Main1"},
		Players: []GREPlayerState{
			{SeatID: 1, LifeTotal: 17, TeamID: 2}, // took 3 damage
			{SeatID: 2, LifeTotal: 20, TeamID: 1},
		},
	}

	// playerConn: local player is seat 1
	playerConn := &GREConnection{SeatID: 1, TeamID: 2}

	// Seed lifeTotals with msg0 values so detectLifeChanges can compare
	lifeTotals := map[int]int{
		1: 20,
		2: 20,
	}

	events := detectLifeChanges(msg0, msg1, playerConn, lifeTotals)
	require.Len(t, events, 1, "expected exactly one life change event")

	evt := events[0]
	assert.Equal(t, "life_change", evt.ActionType)
	assert.Equal(t, "player", evt.PlayerType)
	assert.Equal(t, 2, evt.TeamID, "TeamID must be populated from GREPlayerState.TeamID")
	assert.Equal(t, 20, evt.LifeFrom)
	assert.Equal(t, 17, evt.LifeTo)
}

// TestGamePlayEvent_TeamID_Opponent verifies TeamID is populated for opponent events too.
func TestGamePlayEvent_TeamID_Opponent(t *testing.T) {
	msg0 := &GREGameStateMessage{
		Players: []GREPlayerState{
			{SeatID: 1, LifeTotal: 20, TeamID: 2},
			{SeatID: 2, LifeTotal: 20, TeamID: 1},
		},
	}
	msg1 := &GREGameStateMessage{
		MatchID:    "match-abc",
		GameNumber: 1,
		TurnInfo:   &GRETurnInfo{TurnNumber: 4, Phase: "Phase_Main1"},
		Players: []GREPlayerState{
			{SeatID: 1, LifeTotal: 20, TeamID: 2},
			{SeatID: 2, LifeTotal: 15, TeamID: 1}, // opponent took 5 damage
		},
	}

	// playerConn: local player is seat 1
	playerConn := &GREConnection{SeatID: 1, TeamID: 2}
	lifeTotals := map[int]int{1: 20, 2: 20}

	events := detectLifeChanges(msg0, msg1, playerConn, lifeTotals)
	require.Len(t, events, 1)

	evt := events[0]
	assert.Equal(t, "opponent", evt.PlayerType)
	assert.Equal(t, 1, evt.TeamID, "opponent TeamID must be 1")
}
