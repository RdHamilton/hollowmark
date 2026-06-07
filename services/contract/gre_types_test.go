package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/RdHamilton/hollowmark/services/contract"
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

// TestGamePlayPayload_SchemaVersion verifies that SchemaVersion is emitted
// with key "schema_version" and that it is present even when zero.
func TestGamePlayPayload_SchemaVersion(t *testing.T) {
	payload := contract.GamePlayPayload{
		MatchID:       "match-sv-001",
		GameNumber:    1,
		SchemaVersion: 2,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}

	if _, ok := raw["schema_version"]; !ok {
		t.Error("expected key \"schema_version\" in marshaled GamePlayPayload")
	}

	var decoded contract.GamePlayPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal GamePlayPayload: %v", err)
	}
	if decoded.SchemaVersion != 2 {
		t.Errorf("SchemaVersion: got %d, want 2", decoded.SchemaVersion)
	}
}

// TestCounterChangeEntry_WireKeys verifies CounterChangeEntry marshals to the
// expected JSON keys (ADR-046 A2.1) and round-trips correctly.
func TestCounterChangeEntry_WireKeys(t *testing.T) {
	entry := contract.CounterChangeEntry{
		InstanceID:  42,
		ArenaID:     99999,
		CounterType: "loyalty",
		Count:       3,
		Delta:       -1,
		Controller:  "player",
		TurnNumber:  5,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal CounterChangeEntry: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"instance_id", "arena_id", "counter_type", "count", "delta", "controller", "turn_number"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in CounterChangeEntry wire format", key)
		}
	}

	var decoded contract.CounterChangeEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal CounterChangeEntry: %v", err)
	}
	if decoded.InstanceID != entry.InstanceID {
		t.Errorf("InstanceID: got %d, want %d", decoded.InstanceID, entry.InstanceID)
	}
	if decoded.ArenaID != entry.ArenaID {
		t.Errorf("ArenaID: got %d, want %d", decoded.ArenaID, entry.ArenaID)
	}
	if decoded.CounterType != entry.CounterType {
		t.Errorf("CounterType: got %q, want %q", decoded.CounterType, entry.CounterType)
	}
	if decoded.Count != entry.Count {
		t.Errorf("Count: got %d, want %d", decoded.Count, entry.Count)
	}
	if decoded.Delta != entry.Delta {
		t.Errorf("Delta: got %d, want %d", decoded.Delta, entry.Delta)
	}
	if decoded.Controller != entry.Controller {
		t.Errorf("Controller: got %q, want %q", decoded.Controller, entry.Controller)
	}
	if decoded.TurnNumber != entry.TurnNumber {
		t.Errorf("TurnNumber: got %d, want %d", decoded.TurnNumber, entry.TurnNumber)
	}
}

// TestMulliganEntry_WireKeys verifies MulliganEntry marshals to the expected
// JSON keys (ADR-046 A2.2) and round-trips correctly.
func TestMulliganEntry_WireKeys(t *testing.T) {
	entry := contract.MulliganEntry{
		OpeningHandSize: 7,
		MulliganCount:   1,
		KeptCardIDs:     []int{11111, 22222, 33333, 44444, 55555, 66666},
		BottomedCardIDs: []int{77777},
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal MulliganEntry: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"opening_hand_size", "mulligan_count", "kept_card_ids", "bottomed_card_ids"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in MulliganEntry wire format", key)
		}
	}

	var decoded contract.MulliganEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal MulliganEntry: %v", err)
	}
	if decoded.OpeningHandSize != entry.OpeningHandSize {
		t.Errorf("OpeningHandSize: got %d, want %d", decoded.OpeningHandSize, entry.OpeningHandSize)
	}
	if decoded.MulliganCount != entry.MulliganCount {
		t.Errorf("MulliganCount: got %d, want %d", decoded.MulliganCount, entry.MulliganCount)
	}
	if len(decoded.KeptCardIDs) != len(entry.KeptCardIDs) {
		t.Errorf("KeptCardIDs length: got %d, want %d", len(decoded.KeptCardIDs), len(entry.KeptCardIDs))
	}
	if len(decoded.BottomedCardIDs) != len(entry.BottomedCardIDs) {
		t.Errorf("BottomedCardIDs length: got %d, want %d", len(decoded.BottomedCardIDs), len(entry.BottomedCardIDs))
	}
}

// TestGamePlayPayload_CounterChangesOmitempty verifies CounterChanges is omitted
// when nil (omitempty).
func TestGamePlayPayload_CounterChangesOmitempty(t *testing.T) {
	payload := contract.GamePlayPayload{
		MatchID:    "match-cc-001",
		GameNumber: 1,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := raw["counter_changes"]; ok {
		t.Error("counter_changes should be absent (omitempty) when nil")
	}
	if _, ok := raw["mulligan"]; ok {
		t.Error("mulligan should be absent (omitempty) when nil")
	}
}

// TestGamePlayPayload_WithCountersAndMulligan verifies both new fields marshal
// and unmarshal correctly when populated.
func TestGamePlayPayload_WithCountersAndMulligan(t *testing.T) {
	payload := contract.GamePlayPayload{
		MatchID:       "match-cm-001",
		GameNumber:    1,
		SchemaVersion: 2,
		CounterChanges: []contract.CounterChangeEntry{
			{InstanceID: 10, ArenaID: 555, CounterType: "+1/+1", Count: 2, Delta: 1, Controller: "player", TurnNumber: 3},
		},
		Mulligan: &contract.MulliganEntry{
			OpeningHandSize: 7,
			MulliganCount:   0,
			KeptCardIDs:     []int{1, 2, 3, 4, 5, 6, 7},
			BottomedCardIDs: []int{},
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded contract.GamePlayPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SchemaVersion != 2 {
		t.Errorf("SchemaVersion: got %d, want 2", decoded.SchemaVersion)
	}
	if len(decoded.CounterChanges) != 1 {
		t.Fatalf("CounterChanges: got %d, want 1", len(decoded.CounterChanges))
	}
	if decoded.CounterChanges[0].CounterType != "+1/+1" {
		t.Errorf("CounterType: got %q, want \"+1/+1\"", decoded.CounterChanges[0].CounterType)
	}
	if decoded.Mulligan == nil {
		t.Fatal("Mulligan should be non-nil")
	}
	if decoded.Mulligan.MulliganCount != 0 {
		t.Errorf("MulliganCount: got %d, want 0", decoded.Mulligan.MulliganCount)
	}
	if len(decoded.Mulligan.KeptCardIDs) != 7 {
		t.Errorf("KeptCardIDs length: got %d, want 7", len(decoded.Mulligan.KeptCardIDs))
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
