package contract_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/RdHamilton/MTGA-Companion/services/contract"
)

// TestDaemonEventRoundTrip verifies that a DaemonEvent can be marshaled and
// unmarshaled without data loss or type assertions.
func TestDaemonEventRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	payload := contract.SyncRatingsPayload{
		SetCode:      "BLB",
		CardsUpdated: 42,
		Source:       "17lands",
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	original := contract.DaemonEvent{
		Type:       "sync:ratings",
		AccountID:  "acct_abc123",
		EventID:    "evt_001",
		SessionID:  "sess_xyz789",
		Sequence:   7,
		OccurredAt: now,
		Payload:    rawPayload,
	}

	// Serialize.
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal DaemonEvent: %v", err)
	}

	// Assert expected wire-format JSON keys are present.
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"type", "account_id", "event_id", "session_id", "sequence", "occurred_at", "payload"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q to be present in DaemonEvent wire format", key)
		}
	}

	// Deserialize.
	var decoded contract.DaemonEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal DaemonEvent: %v", err)
	}

	// Validate envelope fields.
	if decoded.Type != original.Type {
		t.Errorf("Type: got %q, want %q", decoded.Type, original.Type)
	}
	if decoded.AccountID != original.AccountID {
		t.Errorf("AccountID: got %q, want %q", decoded.AccountID, original.AccountID)
	}
	if decoded.EventID != original.EventID {
		t.Errorf("EventID: got %q, want %q", decoded.EventID, original.EventID)
	}
	if decoded.SessionID != original.SessionID {
		t.Errorf("SessionID: got %q, want %q", decoded.SessionID, original.SessionID)
	}
	if decoded.Sequence != original.Sequence {
		t.Errorf("Sequence: got %d, want %d", decoded.Sequence, original.Sequence)
	}
	if !decoded.OccurredAt.Equal(original.OccurredAt) {
		t.Errorf("OccurredAt: got %v, want %v", decoded.OccurredAt, original.OccurredAt)
	}

	// Validate payload can be decoded without reflection or type assertions.
	var decodedPayload contract.SyncRatingsPayload
	if err := json.Unmarshal(decoded.Payload, &decodedPayload); err != nil {
		t.Fatalf("unmarshal SyncRatingsPayload: %v", err)
	}
	if decodedPayload.SetCode != payload.SetCode {
		t.Errorf("payload.SetCode: got %q, want %q", decodedPayload.SetCode, payload.SetCode)
	}
	if decodedPayload.CardsUpdated != payload.CardsUpdated {
		t.Errorf("payload.CardsUpdated: got %d, want %d", decodedPayload.CardsUpdated, payload.CardsUpdated)
	}
	if decodedPayload.Source != payload.Source {
		t.Errorf("payload.Source: got %q, want %q", decodedPayload.Source, payload.Source)
	}
}

// TestSyncCardMetadataPayloadRoundTrip validates SyncCardMetadataPayload.
func TestSyncCardMetadataPayloadRoundTrip(t *testing.T) {
	payload := contract.SyncCardMetadataPayload{
		SetCode:      "DSK",
		CardsAdded:   100,
		CardsUpdated: 5,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"set_code", "cards_added", "cards_updated"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in SyncCardMetadataPayload wire format", key)
		}
	}

	var decoded contract.SyncCardMetadataPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SetCode != payload.SetCode {
		t.Errorf("SetCode: got %q, want %q", decoded.SetCode, payload.SetCode)
	}
	if decoded.CardsAdded != payload.CardsAdded {
		t.Errorf("CardsAdded: got %d, want %d", decoded.CardsAdded, payload.CardsAdded)
	}
	if decoded.CardsUpdated != payload.CardsUpdated {
		t.Errorf("CardsUpdated: got %d, want %d", decoded.CardsUpdated, payload.CardsUpdated)
	}
}

// TestDaemonEventRoundTripDraftPayload validates nesting DraftEventPayload.
func TestDaemonEventRoundTripDraftPayload(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	inner := contract.DraftEventPayload{
		DraftID:    "draft_001",
		SetCode:    "BLB",
		PackNumber: 2,
		PickNumber: 7,
	}

	rawInner, err := json.Marshal(inner)
	if err != nil {
		t.Fatalf("marshal inner: %v", err)
	}

	event := contract.DaemonEvent{
		Type:       "draft:pick",
		AccountID:  "acct_draft",
		SessionID:  "sess_draft",
		OccurredAt: now,
		Payload:    rawInner,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}

	var decoded contract.DaemonEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}

	// Assert wire-format keys for DraftEventPayload.
	var rawInnerMap map[string]interface{}
	if err := json.Unmarshal(rawInner, &rawInnerMap); err != nil {
		t.Fatalf("unmarshal inner to raw map: %v", err)
	}

	for _, key := range []string{"draft_id", "set_code", "pack_number", "pick_number"} {
		if _, ok := rawInnerMap[key]; !ok {
			t.Errorf("expected JSON key %q in DraftEventPayload wire format", key)
		}
	}

	var decodedInner contract.DraftEventPayload
	if err := json.Unmarshal(decoded.Payload, &decodedInner); err != nil {
		t.Fatalf("unmarshal inner: %v", err)
	}

	if decodedInner.DraftID != inner.DraftID {
		t.Errorf("DraftID: got %q, want %q", decodedInner.DraftID, inner.DraftID)
	}
	if decodedInner.PackNumber != inner.PackNumber {
		t.Errorf("PackNumber: got %d, want %d", decodedInner.PackNumber, inner.PackNumber)
	}
	if decodedInner.PickNumber != inner.PickNumber {
		t.Errorf("PickNumber: got %d, want %d", decodedInner.PickNumber, inner.PickNumber)
	}
}

// TestInventoryUpdatedPayloadRoundTrip validates InventoryUpdatedPayload round-trip JSON.
func TestInventoryUpdatedPayloadRoundTrip(t *testing.T) {
	payload := contract.InventoryUpdatedPayload{
		Gems:               1200,
		Gold:               5000,
		TotalVaultProgress: 75,
		WildCardCommons:    10,
		WildCardUncommons:  5,
		WildCardRares:      3,
		WildCardMythics:    1,
		Boosters: []contract.InventoryBooster{
			{CollationID: 100078, SetCode: "BLB", Count: 2},
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

	for _, key := range []string{
		"gems", "gold", "total_vault_progress",
		"wild_card_commons", "wild_card_uncommons", "wild_card_rares", "wild_card_mythics",
		"boosters",
	} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in InventoryUpdatedPayload wire format", key)
		}
	}

	var decoded contract.InventoryUpdatedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Gems != payload.Gems {
		t.Errorf("Gems: got %d, want %d", decoded.Gems, payload.Gems)
	}
	if decoded.Gold != payload.Gold {
		t.Errorf("Gold: got %d, want %d", decoded.Gold, payload.Gold)
	}
	if decoded.WildCardCommons != payload.WildCardCommons {
		t.Errorf("WildCardCommons: got %d, want %d", decoded.WildCardCommons, payload.WildCardCommons)
	}
	if decoded.WildCardMythics != payload.WildCardMythics {
		t.Errorf("WildCardMythics: got %d, want %d", decoded.WildCardMythics, payload.WildCardMythics)
	}
	if len(decoded.Boosters) != 1 {
		t.Fatalf("Boosters: got %d, want 1", len(decoded.Boosters))
	}
	if decoded.Boosters[0].SetCode != "BLB" {
		t.Errorf("Boosters[0].SetCode: got %q, want %q", decoded.Boosters[0].SetCode, "BLB")
	}
	if decoded.Boosters[0].CollationID != 100078 {
		t.Errorf("Boosters[0].CollationID: got %d, want 100078", decoded.Boosters[0].CollationID)
	}

	// Assert InventoryBooster wire keys.
	boostersRaw, _ := json.Marshal(payload.Boosters[0])
	var boosterMap map[string]interface{}
	if err := json.Unmarshal(boostersRaw, &boosterMap); err != nil {
		t.Fatalf("unmarshal booster: %v", err)
	}

	for _, key := range []string{"collation_id", "set_code", "count"} {
		if _, ok := boosterMap[key]; !ok {
			t.Errorf("expected JSON key %q in InventoryBooster wire format", key)
		}
	}
}

// TestMatchEventPayloadRoundTrip validates MatchEventPayload.
func TestMatchEventPayloadRoundTrip(t *testing.T) {
	payload := contract.MatchEventPayload{
		MatchID:      "match_xyz",
		Format:       "Draft",
		OpponentName: "Opponent",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"match_id", "format", "opponent_name"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in MatchEventPayload wire format", key)
		}
	}

	var decoded contract.MatchEventPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.MatchID != payload.MatchID {
		t.Errorf("MatchID: got %q, want %q", decoded.MatchID, payload.MatchID)
	}
	if decoded.Format != payload.Format {
		t.Errorf("Format: got %q, want %q", decoded.Format, payload.Format)
	}
	if decoded.OpponentName != payload.OpponentName {
		t.Errorf("OpponentName: got %q, want %q", decoded.OpponentName, payload.OpponentName)
	}
}

// TestCollectionUpdatedPayloadRoundTrip validates CollectionUpdatedPayload round-trip JSON.
func TestCollectionUpdatedPayloadRoundTrip(t *testing.T) {
	payload := contract.CollectionUpdatedPayload{
		Cards: []contract.CollectionCard{
			{ArenaID: 84059, Count: 4},
			{ArenaID: 84060, Count: 2},
		},
		IsDelta: true,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"cards", "is_delta"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in CollectionUpdatedPayload wire format", key)
		}
	}

	var decoded contract.CollectionUpdatedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.IsDelta != payload.IsDelta {
		t.Errorf("IsDelta: got %v, want %v", decoded.IsDelta, payload.IsDelta)
	}
	if len(decoded.Cards) != len(payload.Cards) {
		t.Fatalf("Cards length: got %d, want %d", len(decoded.Cards), len(payload.Cards))
	}
	if decoded.Cards[0].ArenaID != payload.Cards[0].ArenaID {
		t.Errorf("Cards[0].ArenaID: got %d, want %d", decoded.Cards[0].ArenaID, payload.Cards[0].ArenaID)
	}
	if decoded.Cards[0].Count != payload.Cards[0].Count {
		t.Errorf("Cards[0].Count: got %d, want %d", decoded.Cards[0].Count, payload.Cards[0].Count)
	}

	// Assert CollectionCard wire keys.
	cardRaw, _ := json.Marshal(payload.Cards[0])

	var cardMap map[string]interface{}
	if err := json.Unmarshal(cardRaw, &cardMap); err != nil {
		t.Fatalf("unmarshal card: %v", err)
	}

	for _, key := range []string{"arena_id", "count"} {
		if _, ok := cardMap[key]; !ok {
			t.Errorf("expected JSON key %q in CollectionCard wire format", key)
		}
	}
}

// TestDeckUpdatedPayloadRoundTrip validates DeckUpdatedPayload round-trip JSON.
func TestDeckUpdatedPayloadRoundTrip(t *testing.T) {
	payload := contract.DeckUpdatedPayload{
		DeckID: "deck_abc",
		Name:   "Mono Red Aggro",
		Format: "Standard",
		Cards: []contract.DeckCard{
			{ArenaID: 84001, Quantity: 4},
			{ArenaID: 84002, Quantity: 2},
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

	for _, key := range []string{"deck_id", "name", "format", "cards"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in DeckUpdatedPayload wire format", key)
		}
	}

	var decoded contract.DeckUpdatedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.DeckID != payload.DeckID {
		t.Errorf("DeckID: got %q, want %q", decoded.DeckID, payload.DeckID)
	}
	if decoded.Name != payload.Name {
		t.Errorf("Name: got %q, want %q", decoded.Name, payload.Name)
	}
	if decoded.Format != payload.Format {
		t.Errorf("Format: got %q, want %q", decoded.Format, payload.Format)
	}
	if len(decoded.Cards) != len(payload.Cards) {
		t.Fatalf("Cards length: got %d, want %d", len(decoded.Cards), len(payload.Cards))
	}
	if decoded.Cards[0].ArenaID != payload.Cards[0].ArenaID {
		t.Errorf("Cards[0].ArenaID: got %d, want %d", decoded.Cards[0].ArenaID, payload.Cards[0].ArenaID)
	}
	if decoded.Cards[0].Quantity != payload.Cards[0].Quantity {
		t.Errorf("Cards[0].Quantity: got %d, want %d", decoded.Cards[0].Quantity, payload.Cards[0].Quantity)
	}

	// Assert DeckCard wire keys.
	cardRaw, _ := json.Marshal(payload.Cards[0])

	var cardMap map[string]interface{}
	if err := json.Unmarshal(cardRaw, &cardMap); err != nil {
		t.Fatalf("unmarshal card: %v", err)
	}

	for _, key := range []string{"arena_id", "quantity"} {
		if _, ok := cardMap[key]; !ok {
			t.Errorf("expected JSON key %q in DeckCard wire format", key)
		}
	}
}

// TestMatchCompletedPayloadRoundTrip validates MatchCompletedPayload round-trip JSON.
func TestMatchCompletedPayloadRoundTrip(t *testing.T) {
	payload := contract.MatchCompletedPayload{
		MatchID:       "match_complete_001",
		WinningTeamID: 1,
		ResultList: []contract.MatchResult{
			{Scope: "MatchScope_Game", Result: "ResultType_Win", WinningTeamID: 1, Reason: ""},
			{Scope: "MatchScope_Match", Result: "ResultType_Win", WinningTeamID: 1, Reason: ""},
		},
		Format:       "QuickDraft_BLB_20260430",
		OpponentName: "DragonSlayer42",
		Result:       "win",
		PlayerTeamID: 1,
		PlayerWins:   2,
		OpponentWins: 0,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{
		"match_id", "winning_team_id", "result_list",
		"format", "opponent_name", "result", "player_team_id", "player_wins", "opponent_wins",
	} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in MatchCompletedPayload wire format", key)
		}
	}

	var decoded contract.MatchCompletedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.MatchID != payload.MatchID {
		t.Errorf("MatchID: got %q, want %q", decoded.MatchID, payload.MatchID)
	}
	if decoded.WinningTeamID != payload.WinningTeamID {
		t.Errorf("WinningTeamID: got %d, want %d", decoded.WinningTeamID, payload.WinningTeamID)
	}
	if decoded.Result != payload.Result {
		t.Errorf("Result: got %q, want %q", decoded.Result, payload.Result)
	}
	if decoded.PlayerTeamID != payload.PlayerTeamID {
		t.Errorf("PlayerTeamID: got %d, want %d", decoded.PlayerTeamID, payload.PlayerTeamID)
	}
	if decoded.PlayerWins != payload.PlayerWins {
		t.Errorf("PlayerWins: got %d, want %d", decoded.PlayerWins, payload.PlayerWins)
	}
	if decoded.OpponentWins != payload.OpponentWins {
		t.Errorf("OpponentWins: got %d, want %d", decoded.OpponentWins, payload.OpponentWins)
	}
	if len(decoded.ResultList) != len(payload.ResultList) {
		t.Fatalf("ResultList length: got %d, want %d", len(decoded.ResultList), len(payload.ResultList))
	}
	if decoded.ResultList[1].Scope != "MatchScope_Match" {
		t.Errorf("ResultList[1].Scope: got %q, want %q", decoded.ResultList[1].Scope, "MatchScope_Match")
	}

	// Assert MatchResult wire keys.
	resultRaw, _ := json.Marshal(payload.ResultList[0])

	var resultMap map[string]interface{}
	if err := json.Unmarshal(resultRaw, &resultMap); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	for _, key := range []string{"scope", "result", "winning_team_id", "reason"} {
		if _, ok := resultMap[key]; !ok {
			t.Errorf("expected JSON key %q in MatchResult wire format", key)
		}
	}
}

// TestGamePlayPayloadRoundTrip validates GamePlayPayload round-trip JSON.
func TestGamePlayPayloadRoundTrip(t *testing.T) {
	payload := contract.GamePlayPayload{
		MatchID:       "match_gp_001",
		GameNumber:    2,
		WinningTeamID: 1,
		TurnCount:     12,
		DurationSecs:  480,
		LifeChanges: []contract.LifeChangeEntry{
			{TeamID: 1, LifeTotal: 20, Delta: 0, TurnNumber: 1},
			{TeamID: 2, LifeTotal: 17, Delta: -3, TurnNumber: 3},
			{TeamID: 1, LifeTotal: 14, Delta: -6, TurnNumber: 5},
		},
		Partial: false,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{
		"match_id", "game_number", "winning_team_id",
		"turn_count", "duration_secs", "life_changes", "partial",
	} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in GamePlayPayload wire format", key)
		}
	}

	var decoded contract.GamePlayPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.MatchID != payload.MatchID {
		t.Errorf("MatchID: got %q, want %q", decoded.MatchID, payload.MatchID)
	}
	if decoded.GameNumber != payload.GameNumber {
		t.Errorf("GameNumber: got %d, want %d", decoded.GameNumber, payload.GameNumber)
	}
	if decoded.WinningTeamID != payload.WinningTeamID {
		t.Errorf("WinningTeamID: got %d, want %d", decoded.WinningTeamID, payload.WinningTeamID)
	}
	if decoded.TurnCount != payload.TurnCount {
		t.Errorf("TurnCount: got %d, want %d", decoded.TurnCount, payload.TurnCount)
	}
	if decoded.DurationSecs != payload.DurationSecs {
		t.Errorf("DurationSecs: got %d, want %d", decoded.DurationSecs, payload.DurationSecs)
	}
	if decoded.Partial != payload.Partial {
		t.Errorf("Partial: got %v, want %v", decoded.Partial, payload.Partial)
	}
	if len(decoded.LifeChanges) != len(payload.LifeChanges) {
		t.Fatalf("LifeChanges length: got %d, want %d", len(decoded.LifeChanges), len(payload.LifeChanges))
	}
	if decoded.LifeChanges[1].Delta != -3 {
		t.Errorf("LifeChanges[1].Delta: got %d, want -3", decoded.LifeChanges[1].Delta)
	}

	// Assert LifeChangeEntry wire keys.
	lcRaw, _ := json.Marshal(payload.LifeChanges[0])

	var lcMap map[string]interface{}
	if err := json.Unmarshal(lcRaw, &lcMap); err != nil {
		t.Fatalf("unmarshal life change: %v", err)
	}

	for _, key := range []string{"team_id", "life_total", "delta", "turn_number"} {
		if _, ok := lcMap[key]; !ok {
			t.Errorf("expected JSON key %q in LifeChangeEntry wire format", key)
		}
	}
}

// TestQuestProgressPayloadRoundTrip validates QuestProgressPayload round-trip JSON.
func TestQuestProgressPayloadRoundTrip(t *testing.T) {
	payload := contract.QuestProgressPayload{
		Quests: []contract.QuestEntry{
			{QuestID: "q_001", QuestName: "Win 5 Games", Progress: 3, Goal: 5, CanSwap: true},
			{QuestID: "q_002", QuestName: "Play 10 Spells", Progress: 10, Goal: 10, CanSwap: false},
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

	if _, ok := raw["quests"]; !ok {
		t.Errorf("expected JSON key %q in QuestProgressPayload wire format", "quests")
	}

	var decoded contract.QuestProgressPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Quests) != len(payload.Quests) {
		t.Fatalf("Quests length: got %d, want %d", len(decoded.Quests), len(payload.Quests))
	}
	if decoded.Quests[0].QuestID != payload.Quests[0].QuestID {
		t.Errorf("Quests[0].QuestID: got %q, want %q", decoded.Quests[0].QuestID, payload.Quests[0].QuestID)
	}
	if decoded.Quests[0].Progress != payload.Quests[0].Progress {
		t.Errorf("Quests[0].Progress: got %d, want %d", decoded.Quests[0].Progress, payload.Quests[0].Progress)
	}
	if decoded.Quests[0].CanSwap != payload.Quests[0].CanSwap {
		t.Errorf("Quests[0].CanSwap: got %v, want %v", decoded.Quests[0].CanSwap, payload.Quests[0].CanSwap)
	}

	// Assert QuestEntry wire keys.
	entryRaw, _ := json.Marshal(payload.Quests[0])

	var entryMap map[string]interface{}
	if err := json.Unmarshal(entryRaw, &entryMap); err != nil {
		t.Fatalf("unmarshal entry: %v", err)
	}

	for _, key := range []string{"quest_id", "quest_name", "progress", "goal", "can_swap"} {
		if _, ok := entryMap[key]; !ok {
			t.Errorf("expected JSON key %q in QuestEntry wire format", key)
		}
	}
}

// TestQuestCompletedPayloadRoundTrip validates QuestCompletedPayload round-trip JSON.
func TestQuestCompletedPayloadRoundTrip(t *testing.T) {
	payload := contract.QuestCompletedPayload{
		QuestID:          "q_003",
		QuestName:        "Draft Champion",
		Progress:         3,
		Goal:             3,
		XPReward:         500,
		CompletionSource: "QuestGetQuests",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"quest_id", "quest_name", "progress", "goal", "xp_reward", "completion_source"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in QuestCompletedPayload wire format", key)
		}
	}

	var decoded contract.QuestCompletedPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.QuestID != payload.QuestID {
		t.Errorf("QuestID: got %q, want %q", decoded.QuestID, payload.QuestID)
	}
	if decoded.QuestName != payload.QuestName {
		t.Errorf("QuestName: got %q, want %q", decoded.QuestName, payload.QuestName)
	}
	if decoded.Progress != payload.Progress {
		t.Errorf("Progress: got %d, want %d", decoded.Progress, payload.Progress)
	}
	if decoded.Goal != payload.Goal {
		t.Errorf("Goal: got %d, want %d", decoded.Goal, payload.Goal)
	}
	if decoded.XPReward != payload.XPReward {
		t.Errorf("XPReward: got %d, want %d", decoded.XPReward, payload.XPReward)
	}
	if decoded.CompletionSource != payload.CompletionSource {
		t.Errorf("CompletionSource: got %q, want %q", decoded.CompletionSource, payload.CompletionSource)
	}
}

// TestDaemonVersionResponseRoundTrip validates DaemonVersionResponse round-trip JSON.
func TestDaemonVersionResponseRoundTrip(t *testing.T) {
	original := contract.DaemonVersionResponse{
		Latest:      "v1.4.2",
		ReleasedAt:  "2026-04-30T12:00:00Z",
		DownloadURL: "https://github.com/RdHamilton/MTGA-Companion/releases/download/daemon%2Fv1.4.2/mtga-companion-daemon-windows-amd64.exe",
		Changelog:   "Bug fixes and performance improvements.",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"latest", "released_at", "download_url", "changelog"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in DaemonVersionResponse wire format", key)
		}
	}

	var decoded contract.DaemonVersionResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Latest != original.Latest {
		t.Errorf("Latest: got %q, want %q", decoded.Latest, original.Latest)
	}
	if decoded.ReleasedAt != original.ReleasedAt {
		t.Errorf("ReleasedAt: got %q, want %q", decoded.ReleasedAt, original.ReleasedAt)
	}
	if decoded.DownloadURL != original.DownloadURL {
		t.Errorf("DownloadURL: got %q, want %q", decoded.DownloadURL, original.DownloadURL)
	}
	if decoded.Changelog != original.Changelog {
		t.Errorf("Changelog: got %q, want %q", decoded.Changelog, original.Changelog)
	}
}

// TestSyncRatingsPayloadRoundTrip validates SyncRatingsPayload as a standalone type.
func TestSyncRatingsPayloadRoundTrip(t *testing.T) {
	payload := contract.SyncRatingsPayload{
		SetCode:      "FDN",
		CardsUpdated: 15,
		Source:       "17lands",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to raw map: %v", err)
	}

	for _, key := range []string{"set_code", "cards_updated", "source"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("expected JSON key %q in SyncRatingsPayload wire format", key)
		}
	}

	var decoded contract.SyncRatingsPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SetCode != payload.SetCode {
		t.Errorf("SetCode: got %q, want %q", decoded.SetCode, payload.SetCode)
	}
	if decoded.CardsUpdated != payload.CardsUpdated {
		t.Errorf("CardsUpdated: got %d, want %d", decoded.CardsUpdated, payload.CardsUpdated)
	}
	if decoded.Source != payload.Source {
		t.Errorf("Source: got %q, want %q", decoded.Source, payload.Source)
	}
}
