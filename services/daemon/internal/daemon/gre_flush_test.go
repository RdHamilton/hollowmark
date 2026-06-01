package daemon

// Unit tests for the flushGREBuffer function.
// These tests bypass handleEntry and invoke flushGREBuffer directly with a
// constructed slice of json.RawMessage entries, asserting that the dispatched
// GamePlayPayload is populated from the parsed GRE data.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RdHamilton/vault-mtg/services/contract"
	"github.com/RdHamilton/vault-mtg/services/daemon/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// greFlushTestEntries returns a slice of synthetic GRE json.RawMessage entries
// shaped to exercise ParseGamePlays (life change player→17), ExtractGameSnapshots,
// and ExtractOpponentCards.
//
// Entry 0: full game state — player (seat1/team1) at 20, opponent (seat2/team2) at 20.
// Entry 1: diff game state — player at 17 (took 3 damage), opponent card on battlefield.
func greFlushTestEntries() []json.RawMessage {
	e0 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-unit-001","gameNumber":2},"players":[{"seatId":1,"lifeTotal":20,"teamId":1},{"seatId":2,"lifeTotal":20,"teamId":2}],"turnInfo":{"turnNumber":1,"phase":"Phase_Main1","activePlayer":1},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"},{"zoneId":31,"type":"ZoneType_Hand","ownerSeatId":1},{"zoneId":35,"type":"ZoneType_Hand","ownerSeatId":2}],"gameObjects":[]}}]}}`
	e1 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-unit-001","gameNumber":2},"players":[{"seatId":1,"lifeTotal":17,"teamId":1},{"seatId":2,"lifeTotal":20,"teamId":2}],"turnInfo":{"turnNumber":3,"phase":"Phase_Main1","activePlayer":2},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"},{"zoneId":31,"type":"ZoneType_Hand","ownerSeatId":1},{"zoneId":35,"type":"ZoneType_Hand","ownerSeatId":2}],"gameObjects":[{"instanceId":200,"grpId":99001,"ownerSeatId":2,"controllerSeatId":2,"zoneId":28,"cardTypes":["CardType_Creature"]}]}}]}}`
	return []json.RawMessage{json.RawMessage(e0), json.RawMessage(e1)}
}

// TestFlushGREBuffer_LifeChangesPopulated verifies that flushGREBuffer emits a
// "match.game_ended" event with non-empty LifeChanges when the GRE entries
// contain a life total change between two game states.
func TestFlushGREBuffer_LifeChangesPopulated(t *testing.T) {
	var received contract.DaemonEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(&config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acct-unit-flush",
	})

	entries := greFlushTestEntries()
	err := s.flushGREBuffer(context.Background(), s.sessionID, entries, true)
	require.NoError(t, err)

	assert.Equal(t, "match.game_ended", received.Type)

	var payload contract.GamePlayPayload
	require.NoError(t, json.Unmarshal(received.Payload, &payload))

	assert.True(t, payload.Partial, "partial=true should be passed through")
	assert.Equal(t, 2, payload.GameNumber, "GameNumber must come from GRE messages (game 2)")
	assert.Equal(t, "m-unit-001", payload.MatchID)
	assert.NotEmpty(t, payload.LifeChanges, "LifeChanges must be populated from GRE life change")
	// The turn count should be at least 3 (the highest TurnNumber seen)
	assert.GreaterOrEqual(t, payload.TurnCount, 3, "TurnCount should be max turn number seen")
}

// TestFlushGREBuffer_CardPlaysPopulated verifies that CardPlays is populated when
// a game object moves from hand to battlefield between two GRE states.
func TestFlushGREBuffer_CardPlaysPopulated(t *testing.T) {
	// Entry 0: object in hand; Entry 1: same object on battlefield (zone change = play_card)
	e0 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-cardplay","gameNumber":1},"players":[{"seatId":1,"lifeTotal":20,"teamId":1},{"seatId":2,"lifeTotal":20,"teamId":2}],"turnInfo":{"turnNumber":2,"phase":"Phase_Main1","activePlayer":1},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"},{"zoneId":31,"type":"ZoneType_Hand","ownerSeatId":1}],"gameObjects":[{"instanceId":300,"grpId":77777,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31,"cardTypes":["CardType_Creature"]}]}}]}}`
	e1 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-cardplay","gameNumber":1},"players":[{"seatId":1,"lifeTotal":20,"teamId":1},{"seatId":2,"lifeTotal":20,"teamId":2}],"turnInfo":{"turnNumber":2,"phase":"Phase_Main1","activePlayer":1},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"},{"zoneId":31,"type":"ZoneType_Hand","ownerSeatId":1}],"gameObjects":[{"instanceId":300,"grpId":77777,"ownerSeatId":1,"controllerSeatId":1,"zoneId":28,"cardTypes":["CardType_Creature"]}]}}]}}`

	var received contract.DaemonEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(&config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acct-cardplay",
	})

	err := s.flushGREBuffer(context.Background(), s.sessionID,
		[]json.RawMessage{json.RawMessage(e0), json.RawMessage(e1)}, false)
	require.NoError(t, err)

	var payload contract.GamePlayPayload
	require.NoError(t, json.Unmarshal(received.Payload, &payload))

	assert.NotEmpty(t, payload.CardPlays, "CardPlays must be populated for hand→battlefield zone change")
	assert.Equal(t, 77777, payload.CardPlays[0].ArenaID, "ArenaID must come from GRPId")
	assert.Equal(t, "play_card", payload.CardPlays[0].ActionType)
}

// TestFlushGREBuffer_OpponentCardsPopulated verifies that OpponentCards is populated
// when an opponent's card is visible on the battlefield.
func TestFlushGREBuffer_OpponentCardsPopulated(t *testing.T) {
	// Entry 0: opponent card visible on battlefield
	e0 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-opp","gameNumber":1},"players":[{"seatId":1,"lifeTotal":20,"teamId":1},{"seatId":2,"lifeTotal":20,"teamId":2}],"turnInfo":{"turnNumber":3,"phase":"Phase_Main1","activePlayer":1},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"}],"gameObjects":[{"instanceId":400,"grpId":55555,"ownerSeatId":2,"controllerSeatId":2,"zoneId":28,"cardTypes":["CardType_Creature"]}]}}]}}`

	var received contract.DaemonEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(&config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acct-opp",
	})

	// Single entry: ExtractOpponentCards only needs one message (no diff required)
	err := s.flushGREBuffer(context.Background(), s.sessionID,
		[]json.RawMessage{json.RawMessage(e0)}, false)
	require.NoError(t, err)

	var payload contract.GamePlayPayload
	require.NoError(t, json.Unmarshal(received.Payload, &payload))

	assert.NotEmpty(t, payload.OpponentCards, "OpponentCards must be populated from opponent battlefield objects")
	assert.Equal(t, 55555, payload.OpponentCards[0].ArenaID, "ArenaID must be 55555 (GRPId from opponent card)")
}

// TestFlushGREBuffer_EmptyEntries verifies that an empty entry slice still dispatches
// a "match.game_ended" event (with zero values, no panic).
func TestFlushGREBuffer_EmptyEntries(t *testing.T) {
	var received contract.DaemonEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(&config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acct-empty",
	})

	err := s.flushGREBuffer(context.Background(), s.sessionID, nil, true)
	require.NoError(t, err)
	assert.Equal(t, "match.game_ended", received.Type)
}

// TestFlushGREBuffer_NilPlayerConn verifies that when GetPlayerSeatID returns nil
// (no connectResp in the entries), the flush still succeeds. PlayerType defaults
// to "opponent" for all zone/life events (nil-seat degradation per Ray's guidance).
func TestFlushGREBuffer_NilPlayerConn(t *testing.T) {
	// Entries have no connectResp — GetPlayerSeatID returns nil.
	// ParseGamePlays and ExtractGameSnapshots must handle nil playerConn gracefully.
	e0 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-nilseat","gameNumber":1},"players":[{"seatId":1,"lifeTotal":20,"teamId":1},{"seatId":2,"lifeTotal":20,"teamId":2}],"turnInfo":{"turnNumber":1,"phase":"Phase_Main1","activePlayer":1},"zones":[],"gameObjects":[]}}]}}`
	e1 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-nilseat","gameNumber":1},"players":[{"seatId":1,"lifeTotal":15,"teamId":1},{"seatId":2,"lifeTotal":20,"teamId":2}],"turnInfo":{"turnNumber":2,"phase":"Phase_Main1","activePlayer":2},"zones":[],"gameObjects":[]}}]}}`

	var received contract.DaemonEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(&config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acct-nilseat",
	})

	err := s.flushGREBuffer(context.Background(), s.sessionID,
		[]json.RawMessage{json.RawMessage(e0), json.RawMessage(e1)}, true)
	require.NoError(t, err)

	assert.Equal(t, "match.game_ended", received.Type)

	var payload contract.GamePlayPayload
	require.NoError(t, json.Unmarshal(received.Payload, &payload))

	// LifeChanges should be populated — nil playerConn defaults all to "opponent"
	// (Partial event, per the plan's nil-seat degradation note)
	assert.True(t, payload.Partial)
}

// TestFlushGREBuffer_SchemaVersion2 verifies that flushGREBuffer always sets
// SchemaVersion == 2 on the dispatched GamePlayPayload (ADR-046 A1.4, Ray Q3).
func TestFlushGREBuffer_SchemaVersion2(t *testing.T) {
	var received contract.DaemonEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(&config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acct-sv2",
	})

	// Even an empty flush must set SchemaVersion == 2.
	err := s.flushGREBuffer(context.Background(), s.sessionID, nil, false)
	require.NoError(t, err)

	assert.Equal(t, "match.game_ended", received.Type)
	var payload contract.GamePlayPayload
	require.NoError(t, json.Unmarshal(received.Payload, &payload))
	assert.Equal(t, 2, payload.SchemaVersion, "SchemaVersion must be 2 (first A1.4 implementation)")
}

// TestFlushGREBuffer_CounterChangesPopulated verifies that counter changes on
// permanents between consecutive GRE game state messages are captured in the
// dispatched GamePlayPayload.CounterChanges field (#613).
//
// Fixture: two GRE messages. The second message shows planeswalker (instance 501,
// grpId 88888, seat1/team1) losing one loyalty counter (4→3).
func TestFlushGREBuffer_CounterChangesPopulated(t *testing.T) {
	// e0: planeswalker starts on battlefield with loyalty=4.
	e0 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-counter","gameNumber":1},"players":[{"seatId":1,"lifeTotal":20,"teamId":1},{"seatId":2,"lifeTotal":20,"teamId":2}],"turnInfo":{"turnNumber":2,"phase":"Phase_Main1","activePlayer":1},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"}],"gameObjects":[{"instanceId":501,"grpId":88888,"ownerSeatId":1,"controllerSeatId":1,"zoneId":28,"cardTypes":["CardType_Planeswalker"],"counters":[{"type":"loyalty","count":4}]}]}}]}}`
	// e1: planeswalker has loyalty=3 (used -1 ability).
	e1 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-counter","gameNumber":1},"players":[{"seatId":1,"lifeTotal":20,"teamId":1},{"seatId":2,"lifeTotal":20,"teamId":2}],"turnInfo":{"turnNumber":2,"phase":"Phase_Combat","activePlayer":1},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"}],"gameObjects":[{"instanceId":501,"grpId":88888,"ownerSeatId":1,"controllerSeatId":1,"zoneId":28,"cardTypes":["CardType_Planeswalker"],"counters":[{"type":"loyalty","count":3}]}]}}]}}`

	var received contract.DaemonEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// Only capture the match.game_ended event (gre.game_started fires first).
		var evt contract.DaemonEvent
		if err := json.Unmarshal(body, &evt); err == nil && evt.Type == "match.game_ended" {
			received = evt
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(&config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acct-counter",
	})

	err := s.flushGREBuffer(context.Background(), s.sessionID,
		[]json.RawMessage{json.RawMessage(e0), json.RawMessage(e1)}, false)
	require.NoError(t, err)

	require.Equal(t, "match.game_ended", received.Type)
	var payload contract.GamePlayPayload
	require.NoError(t, json.Unmarshal(received.Payload, &payload))

	require.NotEmpty(t, payload.CounterChanges, "CounterChanges must be populated when a loyalty counter decrements")
	cc := payload.CounterChanges[0]
	assert.Equal(t, 501, cc.InstanceID)
	assert.Equal(t, 88888, cc.ArenaID)
	assert.Equal(t, "loyalty", cc.CounterType)
	assert.Equal(t, 3, cc.Count)
	assert.Equal(t, -1, cc.Delta)
	assert.Equal(t, 2, payload.SchemaVersion)
}

// TestFlushGREBuffer_MulliganPopulated verifies that the Mulligan field is populated
// when the GRE entries contain a pre-game hand state reflecting a single mulligan
// taken (maxHandSize dropped from 7 to 6 under London rules) (#614).
//
// Fixture: two pre-game messages (turnNumber absent / 0) showing player seat1
// maxHandSize going 7→6, followed by an in-game message on turn 1. The
// connectResp message identifies seat1 as the local player.
func TestFlushGREBuffer_MulliganPopulated(t *testing.T) {
	// connectResp: identifies local player as seat1.
	eConnect := `{"connectResp":{"systemSeatIds":[1],"teamId":1}}`
	// Pre-game message 1: player sees opening 7 (maxHandSize=7, turnNumber absent = 0).
	ePre0 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-mulligan","gameNumber":1},"players":[{"seatId":1,"lifeTotal":20,"teamId":1,"maxHandSize":7},{"seatId":2,"lifeTotal":20,"teamId":2,"maxHandSize":7}],"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"},{"zoneId":31,"type":"ZoneType_Hand","ownerSeatId":1}],"gameObjects":[{"instanceId":601,"grpId":10001,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31},{"instanceId":602,"grpId":10002,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31},{"instanceId":603,"grpId":10003,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31},{"instanceId":604,"grpId":10004,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31},{"instanceId":605,"grpId":10005,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31},{"instanceId":606,"grpId":10006,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31},{"instanceId":607,"grpId":10007,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31}]}}]}}`
	// Pre-game message 2: player took a mulligan → maxHandSize now 6, hand has 6 cards.
	ePre1 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-mulligan","gameNumber":1},"players":[{"seatId":1,"lifeTotal":20,"teamId":1,"maxHandSize":6},{"seatId":2,"lifeTotal":20,"teamId":2,"maxHandSize":7}],"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"},{"zoneId":31,"type":"ZoneType_Hand","ownerSeatId":1}],"gameObjects":[{"instanceId":601,"grpId":10001,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31},{"instanceId":602,"grpId":10002,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31},{"instanceId":603,"grpId":10003,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31},{"instanceId":604,"grpId":10004,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31},{"instanceId":605,"grpId":10005,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31},{"instanceId":606,"grpId":10006,"ownerSeatId":1,"controllerSeatId":1,"zoneId":31}]}}]}}`
	// In-game message: turn 1 — provides at least 2 total game-state messages so
	// ParseGamePlaysResult can complete its diff loop.
	eGame := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-mulligan","gameNumber":1},"players":[{"seatId":1,"lifeTotal":20,"teamId":1,"maxHandSize":6},{"seatId":2,"lifeTotal":20,"teamId":2,"maxHandSize":7}],"turnInfo":{"turnNumber":1,"phase":"Phase_Main1","activePlayer":1},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"},{"zoneId":31,"type":"ZoneType_Hand","ownerSeatId":1}],"gameObjects":[]}}]}}`

	var received contract.DaemonEvent
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var evt contract.DaemonEvent
		if err := json.Unmarshal(body, &evt); err == nil && evt.Type == "match.game_ended" {
			received = evt
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	s := New(&config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acct-mulligan",
	})

	entries := []json.RawMessage{
		json.RawMessage(eConnect),
		json.RawMessage(ePre0),
		json.RawMessage(ePre1),
		json.RawMessage(eGame),
	}
	err := s.flushGREBuffer(context.Background(), s.sessionID, entries, false)
	require.NoError(t, err)

	require.Equal(t, "match.game_ended", received.Type)
	var payload contract.GamePlayPayload
	require.NoError(t, json.Unmarshal(received.Payload, &payload))

	require.NotNil(t, payload.Mulligan, "Mulligan must be populated when pre-game mulligan sequence is present")
	assert.Equal(t, 1, payload.Mulligan.MulliganCount, "one mulligan taken (maxHandSize 7→6)")
	assert.Equal(t, 6, payload.Mulligan.OpeningHandSize, "kept hand size is 6")
	assert.Len(t, payload.Mulligan.KeptCardIDs, 6, "6 cards in kept hand")
	assert.Equal(t, 2, payload.SchemaVersion)
}
