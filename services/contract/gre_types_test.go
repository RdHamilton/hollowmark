package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/RdHamilton/vault-mtg/services/contract"
)

// TestCardPlayEntry_WireKeys verifies CardPlayEntry marshals to the correct JSON keys
// and that all fields round-trip correctly.
func TestCardPlayEntry_WireKeys(t *testing.T) {
	entry := contract.CardPlayEntry{
		GameNumber: 1,
		TurnNumber: 4,
		Phase:      "Main1",
		ArenaID:    12345,
		PlayerType: "player",
		ActionType: "cast_spell",
		ZoneFrom:   "hand",
		ZoneTo:     "stack",
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal CardPlayEntry: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"game_number", "turn_number", "phase", "arena_id", "player_type", "action_type", "zone_from", "zone_to"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in CardPlayEntry wire format", key)
		}
	}

	var decoded contract.CardPlayEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal CardPlayEntry: %v", err)
	}

	if decoded.GameNumber != entry.GameNumber {
		t.Errorf("GameNumber: got %d, want %d", decoded.GameNumber, entry.GameNumber)
	}
	if decoded.ArenaID != entry.ArenaID {
		t.Errorf("ArenaID: got %d, want %d", decoded.ArenaID, entry.ArenaID)
	}
	if decoded.ActionType != entry.ActionType {
		t.Errorf("ActionType: got %q, want %q", decoded.ActionType, entry.ActionType)
	}
	if decoded.ZoneFrom != entry.ZoneFrom {
		t.Errorf("ZoneFrom: got %q, want %q", decoded.ZoneFrom, entry.ZoneFrom)
	}
	if decoded.ZoneTo != entry.ZoneTo {
		t.Errorf("ZoneTo: got %q, want %q", decoded.ZoneTo, entry.ZoneTo)
	}
}

// TestGameSnapshotEntry_WireKeys verifies GameSnapshotEntry marshals to the correct JSON keys.
func TestGameSnapshotEntry_WireKeys(t *testing.T) {
	entry := contract.GameSnapshotEntry{
		GameNumber:          1,
		TurnNumber:          5,
		PlayerLife:          14,
		OpponentLife:        17,
		PlayerCardsInHand:   3,
		OpponentCardsInHand: 5,
		PlayerLandsInPlay:   4,
		OpponentLandsInPlay: 3,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal GameSnapshotEntry: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{
		"game_number", "turn_number",
		"player_life", "opponent_life",
		"player_cards_in_hand", "opponent_cards_in_hand",
		"player_lands_in_play", "opponent_lands_in_play",
	} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in GameSnapshotEntry wire format", key)
		}
	}

	var decoded contract.GameSnapshotEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal GameSnapshotEntry: %v", err)
	}

	if decoded.PlayerLife != entry.PlayerLife {
		t.Errorf("PlayerLife: got %d, want %d", decoded.PlayerLife, entry.PlayerLife)
	}
	if decoded.OpponentLife != entry.OpponentLife {
		t.Errorf("OpponentLife: got %d, want %d", decoded.OpponentLife, entry.OpponentLife)
	}
	if decoded.PlayerLandsInPlay != entry.PlayerLandsInPlay {
		t.Errorf("PlayerLandsInPlay: got %d, want %d", decoded.PlayerLandsInPlay, entry.PlayerLandsInPlay)
	}
}

// TestOpponentCardEntry_WireKeys verifies OpponentCardEntry marshals to the correct JSON keys.
func TestOpponentCardEntry_WireKeys(t *testing.T) {
	entry := contract.OpponentCardEntry{
		ArenaID:       78901,
		ZoneObserved:  "battlefield",
		TurnFirstSeen: 3,
		TimesSeen:     5,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal OpponentCardEntry: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"arena_id", "zone_observed", "turn_first_seen", "times_seen"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in OpponentCardEntry wire format", key)
		}
	}

	var decoded contract.OpponentCardEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal OpponentCardEntry: %v", err)
	}

	if decoded.ArenaID != entry.ArenaID {
		t.Errorf("ArenaID: got %d, want %d", decoded.ArenaID, entry.ArenaID)
	}
	if decoded.ZoneObserved != entry.ZoneObserved {
		t.Errorf("ZoneObserved: got %q, want %q", decoded.ZoneObserved, entry.ZoneObserved)
	}
	if decoded.TurnFirstSeen != entry.TurnFirstSeen {
		t.Errorf("TurnFirstSeen: got %d, want %d", decoded.TurnFirstSeen, entry.TurnFirstSeen)
	}
	if decoded.TimesSeen != entry.TimesSeen {
		t.Errorf("TimesSeen: got %d, want %d", decoded.TimesSeen, entry.TimesSeen)
	}
}

// TestGamePlayPayload_OmitemptySlices verifies that the new CardPlays, Snapshots,
// and OpponentCards slices on GamePlayPayload are omitted when empty (omitempty).
func TestGamePlayPayload_OmitemptySlices(t *testing.T) {
	// Payload with no card plays / snapshots / opponent cards
	payload := contract.GamePlayPayload{
		MatchID:       "match-omit-001",
		GameNumber:    1,
		WinningTeamID: 0,
		TurnCount:     5,
		Partial:       true,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// These fields must be absent when the slices are nil/empty (omitempty)
	for _, key := range []string{"card_plays", "snapshots", "opponent_cards"} {
		if _, ok := raw[key]; ok {
			t.Errorf("JSON key %q should be absent (omitempty) when slice is empty, but it was present", key)
		}
	}
}

// TestGamePlayPayload_WithGRESlices verifies that CardPlays, Snapshots, and
// OpponentCards are correctly marshaled and unmarshaled when populated.
func TestGamePlayPayload_WithGRESlices(t *testing.T) {
	payload := contract.GamePlayPayload{
		MatchID:    "match-gre-001",
		GameNumber: 2,
		TurnCount:  8,
		CardPlays: []contract.CardPlayEntry{
			{GameNumber: 2, TurnNumber: 2, Phase: "Main1", ArenaID: 11111, PlayerType: "player", ActionType: "land_drop", ZoneFrom: "hand", ZoneTo: "battlefield"},
		},
		Snapshots: []contract.GameSnapshotEntry{
			{GameNumber: 2, TurnNumber: 2, PlayerLife: 20, OpponentLife: 18, PlayerCardsInHand: 4, OpponentCardsInHand: 3, PlayerLandsInPlay: 2, OpponentLandsInPlay: 2},
		},
		OpponentCards: []contract.OpponentCardEntry{
			{ArenaID: 99999, ZoneObserved: "battlefield", TurnFirstSeen: 2, TimesSeen: 3},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"card_plays", "snapshots", "opponent_cards"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q to be present when slice is populated", key)
		}
	}

	var decoded contract.GamePlayPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.CardPlays) != 1 {
		t.Fatalf("CardPlays: got %d, want 1", len(decoded.CardPlays))
	}
	if decoded.CardPlays[0].ArenaID != 11111 {
		t.Errorf("CardPlays[0].ArenaID: got %d, want 11111", decoded.CardPlays[0].ArenaID)
	}

	if len(decoded.Snapshots) != 1 {
		t.Fatalf("Snapshots: got %d, want 1", len(decoded.Snapshots))
	}
	if decoded.Snapshots[0].PlayerLife != 20 {
		t.Errorf("Snapshots[0].PlayerLife: got %d, want 20", decoded.Snapshots[0].PlayerLife)
	}

	if len(decoded.OpponentCards) != 1 {
		t.Fatalf("OpponentCards: got %d, want 1", len(decoded.OpponentCards))
	}
	if decoded.OpponentCards[0].ArenaID != 99999 {
		t.Errorf("OpponentCards[0].ArenaID: got %d, want 99999", decoded.OpponentCards[0].ArenaID)
	}
}
