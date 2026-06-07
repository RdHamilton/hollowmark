package daemon

// Tests for GRE event pipeline wiring (vmt-t#612):
//   - classifyEntry recognises greToClientEvent lines
//   - handleEntry appends to greManager for greToClientEvent
//   - flushGREBuffer replaces stub with real parsers (see gre_flush_test.go)

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/RdHamilton/hollowmark/services/contract"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/config"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/logreader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClassifyEntry_GREToClientEvent verifies that classifyEntry returns
// "greToClientEvent" for a log entry containing a greToClientEvent key.
func TestClassifyEntry_GREToClientEvent(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"greToClientEvent": map[string]interface{}{
				"greToClientMessages": []interface{}{},
			},
		},
	}
	assert.Equal(t, "greToClientEvent", classifyEntry(entry))
}

// TestClassifyEntry_GREToClientEvent_NonRegression verifies that existing event
// types are not misclassified as greToClientEvent.
func TestClassifyEntry_GREToClientEvent_NonRegression(t *testing.T) {
	cases := []struct {
		name  string
		entry *logreader.LogEntry
		want  string
	}{
		{
			name: "BotDraft pack still draft.pack",
			entry: &logreader.LogEntry{
				IsJSON: true,
				JSON: map[string]interface{}{
					"CurrentModule": "BotDraft",
					"Payload":       `{"PackNumber":0,"PickNumber":0,"DraftPack":["1"]}`,
				},
			},
			want: "draft.pack",
		},
		{
			name: "authenticateResponse still player.authenticated",
			entry: &logreader.LogEntry{
				IsJSON: true,
				JSON: map[string]interface{}{
					"authenticateResponse": map[string]interface{}{
						"clientId": "FAKE0000000000000000000001",
					},
				},
			},
			want: "player.authenticated",
		},
		{
			name: "non-GRE line still returns empty",
			entry: &logreader.LogEntry{
				IsJSON: true,
				JSON: map[string]interface{}{
					"someUnknownKey": "someValue",
				},
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyEntry(tc.entry))
		})
	}
}

// TestHandleEntry_GREToClientEvent_AppendsToManager verifies that handleEntry
// appends the raw entry to the GRE manager when the event type is greToClientEvent,
// and does NOT dispatch it immediately to the BFF.
func TestHandleEntry_GREToClientEvent_AppendsToManager(t *testing.T) {
	var bffCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bffCalled = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL:              srv.URL,
		IngestPath:               "/v1/ingest/events",
		AccountID:                "acct-gre-test",
		GRESessionFlushThreshold: 500, // large threshold so entries don't auto-flush during the test
	}
	s := New(cfg)

	greEntry := &logreader.LogEntry{
		IsJSON: true,
		Raw:    `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m1","gameNumber":1},"players":[]}}]}}`,
		JSON: map[string]interface{}{
			"greToClientEvent": map[string]interface{}{},
		},
	}

	err := s.handleEntry(context.Background(), greEntry)
	require.NoError(t, err)

	// The manager must have buffered exactly 1 entry
	assert.Equal(t, 1, s.greManager.EntryCount(s.sessionID), "greManager should have 1 buffered entry")
	// The BFF must NOT have been called synchronously
	assert.False(t, bffCalled, "BFF should not be called for a greToClientEvent entry (buffered only)")
}

// TestHandleEntry_GREToClientEvent_MultipleEntries verifies that multiple
// greToClientEvent lines accumulate in the manager before a flush.
func TestHandleEntry_GREToClientEvent_MultipleEntries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL:              srv.URL,
		IngestPath:               "/v1/ingest/events",
		AccountID:                "acct-gre-test-2",
		GRESessionFlushThreshold: 500,
	}
	s := New(cfg)

	rawGRE := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"players":[]}}]}}`
	entry := &logreader.LogEntry{
		IsJSON: true,
		Raw:    rawGRE,
		JSON:   map[string]interface{}{"greToClientEvent": map[string]interface{}{}},
	}

	for i := 0; i < 3; i++ {
		require.NoError(t, s.handleEntry(context.Background(), entry))
	}

	assert.Equal(t, 3, s.greManager.EntryCount(s.sessionID))
}

// TestGREFlush_DispatchesMatchGameEnded verifies the full pipeline:
// After greManager accumulates entries and FlushAll is called, a
// "match.game_ended" event is dispatched to the BFF with a non-stub payload.
func TestGREFlush_DispatchesMatchGameEnded(t *testing.T) {
	// Track all received events — flush emits gre.game_started then match.game_ended.
	var mu sync.Mutex
	var receivedEvents []contract.DaemonEvent

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var evt contract.DaemonEvent
		if jsonErr := json.Unmarshal(body, &evt); jsonErr == nil {
			mu.Lock()
			receivedEvents = append(receivedEvents, evt)
			mu.Unlock()
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL:              srv.URL,
		IngestPath:               "/v1/ingest/events",
		AccountID:                "acct-gre-flush",
		GRESessionFlushThreshold: 500,
	}
	s := New(cfg)

	// Two GRE entries: first sets up life 20/20, second shows player took 3 damage
	// (17/20). This produces at least one life_change play.
	gre1 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-flush-1","gameNumber":1},"players":[{"seatId":1,"lifeTotal":20,"teamId":1},{"seatId":2,"lifeTotal":20,"teamId":2}],"turnInfo":{"turnNumber":1,"phase":"Phase_Main1","activePlayer":1},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"},{"zoneId":31,"type":"ZoneType_Hand","ownerSeatId":1}],"gameObjects":[]}}]}}`
	gre2 := `{"greToClientEvent":{"greToClientMessages":[{"type":"GREMessageType_GameStateMessage","gameStateMessage":{"gameInfo":{"matchID":"m-flush-1","gameNumber":1},"players":[{"seatId":1,"lifeTotal":17,"teamId":1},{"seatId":2,"lifeTotal":20,"teamId":2}],"turnInfo":{"turnNumber":3,"phase":"Phase_Main1","activePlayer":2},"zones":[{"zoneId":28,"type":"ZoneType_Battlefield"},{"zoneId":31,"type":"ZoneType_Hand","ownerSeatId":1}],"gameObjects":[]}}]}}`

	for _, raw := range []string{gre1, gre2} {
		entry := &logreader.LogEntry{
			IsJSON: true,
			Raw:    raw,
			JSON:   map[string]interface{}{"greToClientEvent": map[string]interface{}{}},
		}
		require.NoError(t, s.handleEntry(context.Background(), entry))
	}

	// Trigger flush
	s.greManager.FlushAll(context.Background())

	mu.Lock()
	defer mu.Unlock()

	// Find the match.game_ended event (gre.game_started is also emitted before it)
	var gameEndedEvt *contract.DaemonEvent
	for i := range receivedEvents {
		if receivedEvents[i].Type == "match.game_ended" {
			gameEndedEvt = &receivedEvents[i]
			break
		}
	}
	require.NotNil(t, gameEndedEvt, "expected a match.game_ended event to be dispatched")
	assert.Equal(t, "acct-gre-flush", gameEndedEvt.AccountID)

	// Assert: payload is non-stub (GameNumber populated, LifeChanges non-empty)
	var payload contract.GamePlayPayload
	require.NoError(t, json.Unmarshal(gameEndedEvt.Payload, &payload))
	assert.Equal(t, 1, payload.GameNumber, "GameNumber must be 1 from GRE messages")
	assert.NotEmpty(t, payload.LifeChanges, "LifeChanges must be populated (not stub empty slice)")
	assert.Equal(t, "m-flush-1", payload.MatchID)
}
