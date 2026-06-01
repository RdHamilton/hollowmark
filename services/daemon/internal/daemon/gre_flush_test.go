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
