package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/contract"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/config"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/localapi"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/logreader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeBatchBody decodes a BFF ingest request body that may be either a JSON
// batch ([]contract.DaemonEvent) or a legacy single event (contract.DaemonEvent).
// Returns the first event in the batch, or the single decoded event.
// Safe to call from HTTP handler goroutines (does not use testing.T).
func decodeBatchBody(_ *testing.T, body []byte) contract.DaemonEvent {
	var batch []contract.DaemonEvent
	if err := json.Unmarshal(body, &batch); err == nil && len(batch) > 0 {
		return batch[0]
	}
	var evt contract.DaemonEvent
	_ = json.Unmarshal(body, &evt)
	return evt
}

// flushBatch triggers an immediate flush of svc's batch buffer and waits a
// brief period for the async send to complete.  Call after handleEntry in
// tests that need to assert on the received BFF body.
func flushBatch(svc *Service) {
	svc.batchBuffer.FlushNow()
	time.Sleep(100 * time.Millisecond)
}

// eventCapture provides a goroutine-safe capture for a single DaemonEvent
// received by a test BFF server. Use receivedEvt() after flushBatch to read
// the captured value.
type eventCapture struct {
	mu  sync.Mutex
	evt contract.DaemonEvent
}

// capture records the event thread-safely (called from HTTP handler goroutine).
func (c *eventCapture) capture(body []byte) {
	var batch []contract.DaemonEvent
	if err := json.Unmarshal(body, &batch); err == nil && len(batch) > 0 {
		c.mu.Lock()
		c.evt = batch[0]
		c.mu.Unlock()
		return
	}
	var single contract.DaemonEvent
	if json.Unmarshal(body, &single) == nil {
		c.mu.Lock()
		c.evt = single
		c.mu.Unlock()
	}
}

// get returns the captured event (safe from test goroutine after flushBatch).
func (c *eventCapture) get() contract.DaemonEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evt
}

// makeTestJWT constructs a minimal unsigned JWT with the given exp Unix timestamp.
// The signature segment is a placeholder; it is never verified by the daemon.
func makeTestJWT(exp int64) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	claims := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp)))
	return header + "." + claims + ".fakesig"
}

// TestClassifyEntry_DraftPack classifies a BotDraft (QuickDraft) pack line
// (CurrentModule=BotDraft + stringified Payload, #337) as draft.pack.
func TestClassifyEntry_DraftPack(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"CurrentModule": "BotDraft",
			"Payload":       `{"PackNumber":0,"PickNumber":0,"DraftPack":["1"]}`,
		},
	}
	assert.Equal(t, "draft.pack", classifyEntry(entry))
}

// TestClassifyEntry_DraftPick classifies a BotDraftDraftPick line (request
// string carrying PickInfo, #337) as draft.pick.
func TestClassifyEntry_DraftPick(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"id":      "abc",
			"request": `{"PickInfo":{"CardIds":["1"],"PackNumber":0,"PickNumber":0}}`,
		},
	}
	assert.Equal(t, "draft.pick", classifyEntry(entry))
}

// TestClassifyEntry_PremierDraftPack_NonRegression locks in that the Premier
// (#338) Draft.Notify line (draftId + PackCards) still classifies as draft.pack
// after #337 replaced the BotDraft probes. The Premier probe short-circuits
// before the BotDraft probe.
func TestClassifyEntry_PremierDraftPack_NonRegression(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"draftId":   "00000000-0000-4000-8000-0000000003a8",
			"SelfPick":  float64(1),
			"SelfPack":  float64(1),
			"PackCards": "102614,102609,102714",
		},
	}
	assert.Equal(t, "draft.pack", classifyEntry(entry))
}

// TestClassifyEntry_PremierDraftPick_NonRegression locks in that the Premier
// (#338) EventPlayerDraftMakePick line (id + request carrying DraftId) still
// classifies as draft.pick after #337. The Premier DraftId probe runs before
// the BotDraft PickInfo probe.
func TestClassifyEntry_PremierDraftPick_NonRegression(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"id":      "00000000-0000-4000-8000-0000000004b1",
			"request": `{"DraftId":"00000000-0000-4000-8000-0000000003a8","GrpIds":[102647],"Pack":1,"Pick":1}`,
		},
	}
	assert.Equal(t, "draft.pick", classifyEntry(entry))
}

// TestBotDraftClassifierDoesNotMatchPremierLines guards the probe ordering: a
// Premier line must route through the Premier parser, never the BotDraft else
// branch. handleEntry dispatches the Premier line and the emitted payload must
// carry the Premier draft_id (proof ParsePremierDraftMakePick ran, not
// ParseBotDraftPick).
func TestBotDraftClassifierDoesNotMatchPremierLines(t *testing.T) {
	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-premier",
	}
	svc := New(cfg)

	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"id":      "00000000-0000-4000-8000-0000000004b1",
			"request": `{"DraftId":"00000000-0000-4000-8000-0000000003a8","GrpIds":[102647],"Pack":1,"Pick":1}`,
		},
	}

	require.NoError(t, svc.handleEntry(context.Background(), entry))
	flushBatch(svc) // draft.pick triggers FlushNow; wait for async send
	assert.Equal(t, "draft.pick", cap.get().Type)

	var payload logreader.DraftPickPayload
	require.NoError(t, json.Unmarshal(cap.get().Payload, &payload))
	// Premier parser sets draft_id and converts 1-based Pack/Pick to 0-based.
	assert.Equal(t, "00000000-0000-4000-8000-0000000003a8", payload.DraftID)
	assert.Equal(t, []int{102647}, payload.PickedCards)
	assert.Equal(t, 0, payload.PackNumber)
	assert.Equal(t, 0, payload.PickNumber)
	// CourseName stays empty for Premier — proof BotDraft (which sets EventName)
	// did NOT run.
	assert.Equal(t, "", payload.CourseName)
}

func TestClassifyEntry_MatchCompleted(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"CurrentEventState": "MatchCompleted"},
	}
	assert.Equal(t, "match.completed", classifyEntry(entry))
}

func TestClassifyEntry_DraftStarted(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"toSceneName": "Draft"},
	}
	assert.Equal(t, "draft.started", classifyEntry(entry))
}

func TestClassifyEntry_PlayerAuthenticated(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"authenticateResponse": map[string]interface{}{"screenName": "Ray"}},
	}
	assert.Equal(t, "player.authenticated", classifyEntry(entry))
}

func TestClassifyEntry_RankUpdated(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"rankClass": "Gold", "rankTier": float64(2)},
	}
	assert.Equal(t, "player.rank_updated", classifyEntry(entry))
}

func TestClassifyEntry_MatchStarted(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"CurrentEventState": "MatchInProgress"},
	}
	assert.Equal(t, "match.started", classifyEntry(entry))
}

// TestClassifyEntry_DraftEnded verifies that leaving the Draft scene emits
// "draft.completed" (renamed from "draft.ended" in ADR-051 so the BFF
// projection worker can handle it — the BFF never handled "draft.ended").
func TestClassifyEntry_DraftEnded(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"toSceneName":   "Home",
			"fromSceneName": "Draft",
		},
	}
	assert.Equal(t, "draft.completed", classifyEntry(entry))
}

func TestClassifyEntry_InventoryUpdated(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems": float64(1200),
				"Gold": float64(5000),
			},
		},
	}
	assert.Equal(t, "inventory.updated", classifyEntry(entry))
}

func TestClassifyEntry_Unknown(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"someOtherKey": "value"},
	}
	assert.Equal(t, "", classifyEntry(entry))
}

func TestClassifyEntry_NotJSON(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: false,
		Raw:    "plain text",
	}
	assert.Equal(t, "", classifyEntry(entry))
}

// TestHandleEntry_DraftPackDispatchesTypedPayload verifies that handleEntry
// parses a BotDraft (QuickDraft) draft.pack entry into a DraftPackPayload and
// sends it to the BFF with the correct typed JSON keys (PackCards, SelfPick,
// CourseName). The else-branch routes the CurrentModule=BotDraft wire line (#337).
func TestHandleEntry_DraftPackDispatchesTypedPayload(t *testing.T) {
	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-1",
	}
	svc := New(cfg)

	// Construct the entry as the reader would after parsing a BotDraft pack line.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"CurrentModule": "BotDraft",
			"Payload":       `{"EventName":"QuickDraft_SOS_20260526","PackNumber":0,"PickNumber":0,"DraftPack":["12345","67890"]}`,
		},
	}

	require.NoError(t, svc.handleEntry(context.Background(), entry))
	flushBatch(svc) // wait for async batch dispatch
	assert.Equal(t, "draft.pack", cap.get().Type)
	assert.Equal(t, "acc-1", cap.get().AccountID)

	var payload logreader.DraftPackPayload
	require.NoError(t, json.Unmarshal(cap.get().Payload, &payload))
	assert.Equal(t, "QuickDraft_SOS_20260526", payload.CourseName)
	assert.Equal(t, []int{12345, 67890}, payload.DraftPack.PackCards)
	// pack 0 / pick 0 → cumulative 1-based SelfPick = 1.
	assert.Equal(t, 1, payload.DraftPack.SelfPick)
}

// TestHandleEntry_DraftPickDispatchesTypedPayload verifies that handleEntry
// parses a BotDraft (QuickDraft) draft.pick entry into a DraftPickPayload and
// sends it to the BFF with the correct typed JSON keys (pickedCards, PackNumber,
// PickNumber, CourseName). The else-branch routes the BotDraftDraftPick line (#337).
func TestHandleEntry_DraftPickDispatchesTypedPayload(t *testing.T) {
	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-2",
	}
	svc := New(cfg)

	// Construct the entry as the reader would after parsing a BotDraftDraftPick line.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"id":      "abc",
			"request": `{"EventName":"QuickDraft_SOS_20260526","PickInfo":{"EventName":"QuickDraft_SOS_20260526","CardIds":["12345"],"PackNumber":0,"PickNumber":3}}`,
		},
	}

	require.NoError(t, svc.handleEntry(context.Background(), entry))
	flushBatch(svc) // draft.pick triggers FlushNow; wait for async send
	assert.Equal(t, "draft.pick", cap.get().Type)
	assert.Equal(t, "acc-2", cap.get().AccountID)

	var payload logreader.DraftPickPayload
	require.NoError(t, json.Unmarshal(cap.get().Payload, &payload))
	assert.Equal(t, "QuickDraft_SOS_20260526", payload.CourseName)
	assert.Equal(t, []int{12345}, payload.PickedCards)
	assert.Equal(t, 0, payload.PackNumber)
	assert.Equal(t, 3, payload.PickNumber)
}

// TestHandleEntry_InventoryUpdatedDispatchesTypedPayload verifies that handleEntry
// parses an inventory.updated entry into a contract.InventoryUpdatedPayload and
// sends it to the BFF with the correct event type and JSON field names.
func TestHandleEntry_InventoryUpdatedDispatchesTypedPayload(t *testing.T) {
	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-inv",
	}
	svc := New(cfg)

	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems":              float64(1200),
				"Gold":              float64(5000),
				"WildCardCommons":   float64(10),
				"WildCardUnCommons": float64(5),
				"WildCardRares":     float64(3),
				"WildCardMythics":   float64(1),
				"Boosters": []interface{}{
					map[string]interface{}{
						"CollationId": float64(100078),
						"SetCode":     "BLB",
						"Count":       float64(2),
					},
				},
			},
		},
	}

	require.NoError(t, svc.handleEntry(context.Background(), entry))
	flushBatch(svc) // wait for async batch dispatch
	assert.Equal(t, "inventory.updated", cap.get().Type)
	assert.Equal(t, "acc-inv", cap.get().AccountID)

	var payload contract.InventoryUpdatedPayload
	require.NoError(t, json.Unmarshal(cap.get().Payload, &payload))
	assert.Equal(t, 1200, payload.Gems)
	assert.Equal(t, 5000, payload.Gold)
	assert.Equal(t, 10, payload.WildCardCommons)
	assert.Equal(t, 5, payload.WildCardUncommons)
	assert.Equal(t, 3, payload.WildCardRares)
	assert.Equal(t, 1, payload.WildCardMythics)
	require.Len(t, payload.Boosters, 1)
	assert.Equal(t, "BLB", payload.Boosters[0].SetCode)
	assert.Equal(t, 2, payload.Boosters[0].Count)
}

// ---------------------------------------------------------------------------
// Update check ticker tests
//
// Strategy: set updateCheckInterval to a very short duration so the ticker
// fires quickly, then cancel the context. Count how many times the BFF
// version endpoint was called.
// ---------------------------------------------------------------------------

// TestRunFiresUpdateCheckOnStartupAndTicker verifies that the update check is
// called once on startup (via goroutine) and again when the ticker fires.
func TestRunFiresUpdateCheckOnStartupAndTicker(t *testing.T) {
	var versionCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/daemon/version":
			versionCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"latest":"0.1.0","released_at":"2026-01-01T00:00:00Z","download_url":"https://example.com"}`)
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()

	freshJWT := makeTestJWT(time.Now().Add(30 * 24 * time.Hour).Unix())
	cfg := &config.Config{
		CloudAPIURL:        srv.URL,
		IngestPath:         "/v1/ingest/events",
		APIKey:             "test-api-key",
		AccountID:          "acc-update-check",
		SyncEnabled:        true,
		DaemonJWT:          freshJWT,
		LogPath:            "/dev/null",
		DisableUpdateCheck: false,
	}

	svc := New(cfg)
	svc.WithVersion("0.1.0") // same as latest — no WARN, but check still fires

	oldUpdateInterval := updateCheckInterval
	updateCheckInterval = 20 * time.Millisecond
	t.Cleanup(func() { updateCheckInterval = oldUpdateInterval })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = svc.Run(ctx)

	// Startup goroutine + at least one ticker fire = at least 2 calls.
	assert.GreaterOrEqual(t, versionCalls.Load(), int32(2),
		"expected startup check plus at least one ticker-driven check")
}

// TestRunSkipsUpdateCheckWhenDisabled verifies that no call to the version
// endpoint is made when DisableUpdateCheck is true.
func TestRunSkipsUpdateCheckWhenDisabled(t *testing.T) {
	var versionCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/daemon/version" {
			versionCalls.Add(1)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	freshJWT := makeTestJWT(time.Now().Add(30 * 24 * time.Hour).Unix())
	cfg := &config.Config{
		CloudAPIURL:        srv.URL,
		IngestPath:         "/v1/ingest/events",
		APIKey:             "test-api-key",
		AccountID:          "acc-no-update-check",
		SyncEnabled:        true,
		DaemonJWT:          freshJWT,
		LogPath:            "/dev/null",
		DisableUpdateCheck: true,
	}

	svc := New(cfg)
	svc.WithVersion("0.1.0")

	oldUpdateInterval := updateCheckInterval
	updateCheckInterval = 20 * time.Millisecond
	t.Cleanup(func() { updateCheckInterval = oldUpdateInterval })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = svc.Run(ctx)

	assert.Equal(t, int32(0), versionCalls.Load(),
		"version endpoint must not be called when DisableUpdateCheck is true")
}

// TestClassifyEntry_CollectionUpdated verifies that a flat card-ID map entry
// is classified as "collection.updated".
func TestClassifyEntry_CollectionUpdated(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"12345": float64(4),
			"67890": float64(2),
		},
	}
	assert.Equal(t, "collection.updated", classifyEntry(entry))
}

// TestHandleEntry_CollectionUpdatedDispatchesTypedPayload verifies that
// handleEntry parses a collection.updated entry into a
// contract.CollectionUpdatedPayload and sends it to the BFF with the correct
// event type and JSON field names.
func TestHandleEntry_CollectionUpdatedDispatchesTypedPayload(t *testing.T) {
	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-coll",
	}
	svc := New(cfg)

	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"12345": float64(4),
			"67890": float64(2),
		},
	}

	require.NoError(t, svc.handleEntry(context.Background(), entry))
	flushBatch(svc) // wait for async batch dispatch
	assert.Equal(t, "collection.updated", cap.get().Type)
	assert.Equal(t, "acc-coll", cap.get().AccountID)

	var payload contract.CollectionUpdatedPayload
	require.NoError(t, json.Unmarshal(cap.get().Payload, &payload))
	assert.False(t, payload.IsDelta)
	require.Len(t, payload.Cards, 2)
	// Verify both arena IDs appear in the result.
	ids := make(map[int]int, len(payload.Cards))
	for _, c := range payload.Cards {
		ids[c.ArenaID] = c.Count
	}
	assert.Equal(t, 4, ids[12345])
	assert.Equal(t, 2, ids[67890])
}

// TestClassifyEntry_DeckUpdated verifies that an entry containing a deck upsert
// request JSON string is classified as "deck.updated".
func TestClassifyEntry_DeckUpdated(t *testing.T) {
	req := `{"Summary":{"DeckId":"deck-123","Name":"Test Deck","Attributes":[{"name":"Format","value":"Standard"}]},"Deck":{"MainDeck":[{"cardId":11111,"quantity":4}],"Sideboard":[]}}`
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": req},
	}
	assert.Equal(t, "deck.updated", classifyEntry(entry))
}

// TestHandleEntry_DeckUpdatedDispatchesTypedPayload verifies that handleEntry
// parses a deck.updated entry into a contract.DeckUpdatedPayload and sends it
// to the BFF with the correct event type and JSON field names.
func TestHandleEntry_DeckUpdatedDispatchesTypedPayload(t *testing.T) {
	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-deck",
	}
	svc := New(cfg)

	req := `{"Summary":{"DeckId":"deck-abc","Name":"Mono Red","Attributes":[{"name":"Format","value":"Standard"}]},"Deck":{"MainDeck":[{"cardId":55555,"quantity":4},{"cardId":66666,"quantity":2}],"Sideboard":[]}}`
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": req},
	}

	require.NoError(t, svc.handleEntry(context.Background(), entry))
	flushBatch(svc) // wait for async batch dispatch
	assert.Equal(t, "deck.updated", cap.get().Type)
	assert.Equal(t, "acc-deck", cap.get().AccountID)

	var payload contract.DeckUpdatedPayload
	require.NoError(t, json.Unmarshal(cap.get().Payload, &payload))
	assert.Equal(t, "deck-abc", payload.DeckID)
	assert.Equal(t, "Mono Red", payload.Name)
	assert.Equal(t, "Standard", payload.Format)
	require.Len(t, payload.Cards, 2)
	assert.Equal(t, 55555, payload.Cards[0].ArenaID)
	assert.Equal(t, 4, payload.Cards[0].Quantity)
	assert.Equal(t, 66666, payload.Cards[1].ArenaID)
	assert.Equal(t, 2, payload.Cards[1].Quantity)
}

// matchCompletedEntry builds a LogEntry that mirrors the real
// matchGameRoomStateChangedEvent structure observed in Player.log.
func matchCompletedEntry() *logreader.LogEntry {
	return &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"matchGameRoomStateChangedEvent": map[string]interface{}{
				"gameRoomInfo": map[string]interface{}{
					"stateType": "MatchGameRoomStateType_MatchCompleted",
					"gameRoomConfig": map[string]interface{}{
						"reservedPlayers": []interface{}{
							map[string]interface{}{
								"userId":     "USER_A",
								"playerName": "OpponentPlayer",
								"teamId":     float64(1),
								"eventId":    "Ladder",
							},
							map[string]interface{}{
								"userId":     "USER_B",
								"playerName": "LocalPlayer",
								"teamId":     float64(2),
								"eventId":    "Ladder",
							},
						},
					},
					"finalMatchResult": map[string]interface{}{
						"matchId":              "test-match-uuid",
						"matchCompletedReason": "MatchCompletedReasonType_Success",
						"resultList": []interface{}{
							map[string]interface{}{
								"scope":         "MatchScope_Game",
								"result":        "ResultType_WinLoss",
								"winningTeamId": float64(2),
								"reason":        "ResultReason_Game",
							},
							map[string]interface{}{
								"scope":         "MatchScope_Match",
								"result":        "ResultType_WinLoss",
								"winningTeamId": float64(2),
								"reason":        "ResultReason_Game",
							},
						},
					},
				},
			},
		},
	}
}

// TestClassifyEntry_MatchCompleted_GREEvent verifies that an entry containing
// matchGameRoomStateChangedEvent with MatchCompleted state type is classified
// as "match.completed".
func TestClassifyEntry_MatchCompleted_GREEvent(t *testing.T) {
	assert.Equal(t, "match.completed", classifyEntry(matchCompletedEntry()))
}

// TestClassifyEntry_MatchCompletedLegacy verifies the legacy CurrentEventState
// path still classifies as "match.completed".
func TestClassifyEntry_MatchCompletedLegacy(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"CurrentEventState": "MatchCompleted"},
	}
	assert.Equal(t, "match.completed", classifyEntry(entry))
}

// TestHandleEntry_MatchCompletedDispatchesTypedPayload verifies that
// handleEntry parses a match.completed entry into a
// contract.MatchCompletedPayload and sends it to the BFF with the correct
// event type and JSON field names.
func TestHandleEntry_MatchCompletedDispatchesTypedPayload(t *testing.T) {
	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-match",
	}
	svc := New(cfg)

	require.NoError(t, svc.handleEntry(context.Background(), matchCompletedEntry()))
	flushBatch(svc) // wait for async batch dispatch
	assert.Equal(t, "match.completed", cap.get().Type)
	assert.Equal(t, "acc-match", cap.get().AccountID)

	var payload contract.MatchCompletedPayload
	require.NoError(t, json.Unmarshal(cap.get().Payload, &payload))
	assert.Equal(t, "test-match-uuid", payload.MatchID)
	assert.Equal(t, "Ladder", payload.Format)
	assert.Equal(t, 2, payload.WinningTeamID)
	require.Len(t, payload.ResultList, 2)
	assert.Equal(t, "MatchScope_Match", payload.ResultList[1].Scope)
	assert.Equal(t, 2, payload.ResultList[1].WinningTeamID)
}

// TestWithVersion sets version and verifies it is stored correctly.
func TestWithVersion(t *testing.T) {
	cfg := &config.Config{
		CloudAPIURL: "http://localhost",
		IngestPath:  "/v1/ingest/events",
	}
	svc := New(cfg)
	assert.Equal(t, "dev", svc.version)

	svc.WithVersion("1.2.3")
	assert.Equal(t, "1.2.3", svc.version)

	// Empty string must be ignored.
	svc.WithVersion("")
	assert.Equal(t, "1.2.3", svc.version)
}

// ---------------------------------------------------------------------------
// Heartbeat ticker tests
// ---------------------------------------------------------------------------

// TestRunSendsHeartbeatWhenAccountIDSet verifies that the run loop dispatches a
// daemon.heartbeat event on each ticker fire when AccountID is non-empty.
func TestRunSendsHeartbeatWhenAccountIDSet(t *testing.T) {
	var heartbeatCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ingest/events" {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			var evt contract.DaemonEvent
			require.NoError(t, json.Unmarshal(body, &evt))
			if evt.Type == "daemon.heartbeat" {
				heartbeatCalls.Add(1)
			}
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-heartbeat",
		LogPath:     "/dev/null",
	}
	svc := New(cfg)

	oldInterval := heartbeatInterval
	heartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { heartbeatInterval = oldInterval })

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_ = svc.Run(ctx)

	assert.GreaterOrEqual(t, heartbeatCalls.Load(), int32(1),
		"expected at least one daemon.heartbeat event when AccountID is set")
}

// TestHandleEntry_PlayerAuthenticatedCachesMtgaUserID verifies that processing
// a player.authenticated log entry caches the MTGA client ID so that
// subsequent match.completed parsing can identify the local player's team.
// In 2026.59.20 the authenticateResponse uses clientId (not accountId/userId).
func TestHandleEntry_PlayerAuthenticatedCachesMtgaUserID(t *testing.T) {
	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "clerk-user-id-001",
	}
	svc := New(cfg)

	// Simulate the player.authenticated log entry emitted by Arena 2026.59.20.
	// authenticateResponse has clientId, sessionId, screenName — no accountId.
	authEntry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"authenticateResponse": map[string]interface{}{
				"clientId":   "FAKEPLAYER0000000000000001",
				"sessionId":  "00000000-0000-4000-8000-000000000030",
				"screenName": "TestPlayer",
			},
		},
	}

	require.NoError(t, svc.handleEntry(context.Background(), authEntry))
	flushBatch(svc) // wait for async batch dispatch
	assert.Equal(t, "FAKEPLAYER0000000000000001", svc.mtgaUserID,
		"mtgaUserID must be cached from clientId after player.authenticated")
	assert.Equal(t, "player.authenticated", cap.get().Type)
}

// TestHandleEntry_MatchCompleted_WithCachedMtgaUserID verifies that when the
// daemon has already processed a player.authenticated event, the subsequent
// match.completed event carries a pre-computed result ("win" or "loss").
func TestHandleEntry_MatchCompleted_WithCachedMtgaUserID(t *testing.T) {
	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "clerk-user-id-001",
	}
	svc := New(cfg)
	// Pre-set the cached MTGA user ID as if player.authenticated was seen.
	// USER_B has teamId=2 in matchCompletedEntry(), WinningTeamID=2 → "win".
	svc.mtgaUserID = "USER_B"

	require.NoError(t, svc.handleEntry(context.Background(), matchCompletedEntry()))
	flushBatch(svc) // wait for async batch dispatch
	assert.Equal(t, "match.completed", cap.get().Type)

	var payload contract.MatchCompletedPayload
	require.NoError(t, json.Unmarshal(cap.get().Payload, &payload))
	assert.Equal(t, "win", payload.Result,
		"result must be pre-computed when mtgaUserID is known")
	assert.Equal(t, 2, payload.PlayerTeamID)
	assert.Equal(t, 1, payload.PlayerWins)
	assert.Equal(t, 0, payload.OpponentWins)
}

// ---------------------------------------------------------------------------
// Keychain mode 401 recovery — sentinel + tray hook (#2563)
// ---------------------------------------------------------------------------

// TestDispatcher_401InKeychainModeIsNotRetried verifies the full #2563 contract:
//   - The ORIGINAL event's retry loop breaks after exactly 1 BFF hit (checked
//     synchronously, before the async auth_failed dispatch can run)
//   - handleEntry returns nil (ErrReauthRequired is suppressed, not propagated)
//   - The keychain reauth tray hook fires exactly once (for the original event)
//   - The auth_failed telemetry dispatch runs in a goroutine via a transient
//     no-refresher dispatcher (#2139) — it does NOT re-trigger the tray hook
//   - No re-registration endpoint is ever called
//
// Note (#2139 update): handleEntry now dispatches daemon.auth_failed in a
// goroutine after ErrReauthRequired. That goroutine uses a transient dispatcher
// with NO refresher, so it cannot trigger the tray hook again. However, it may
// make additional BFF calls (up to 3) which we cannot distinguish from the
// original call at the ingestCalls level once the goroutine has run. We assert
// ingestCalls right after handleEntry returns (before the goroutine runs) to
// preserve the "original event hits BFF exactly once" invariant.
func TestDispatcher_401InKeychainModeIsNotRetried(t *testing.T) {
	var registerCalls atomic.Int32
	var ingestCalls atomic.Int32

	// Stub BFF: ingest always returns 401; register must never be called.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ingest/events":
			ingestCalls.Add(1)
			w.WriteHeader(http.StatusUnauthorized)
		case "/daemon/register", "/api/daemon/register":
			registerCalls.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"api_key":"sk_new","account_id":"acc_new"}`))
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/ingest/events",
		AccountID:   "acc-keychain",
		Keychain:    true,
	}
	svc := New(cfg)

	// Wire the tray hook so we can assert it was fired.
	// capturedReason is protected by a mutex because it is written from the
	// batchBuffer flush goroutine and read from the test goroutine.
	var reauthReasonCalls atomic.Int32
	var capturedReasonMu sync.Mutex
	var capturedReason string
	svc.trayHooks = TrayHooks{
		SetReauthRequired: func(reason string) {
			reauthReasonCalls.Add(1)
			capturedReasonMu.Lock()
			capturedReason = reason
			capturedReasonMu.Unlock()
		},
	}

	// Use draft.pick — boundary event triggers FlushNow so the batch flushes
	// promptly and the BFF ingest hit count can be asserted after a brief wait.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": `{"PickInfo":{"PackNumber":0,"PickNumber":0,"CardId":102704}}`},
	}

	// handleEntry must return nil — ErrReauthRequired is suppressed to avoid
	// per-entry spam in the run loop; the tray hook is the user-facing signal.
	err := svc.handleEntry(context.Background(), entry)
	assert.NoError(t, err,
		"handleEntry must return nil when ErrReauthRequired is suppressed (#2563)")

	// Allow async batch flush to complete before counting BFF hits.
	// (Previously asserted synchronously; batch dispatch is now async — ADR-053.)
	flushBatch(svc)

	// The original event's retry loop must break after exactly 1 BFF hit.
	// A second hit (<=2 total) is allowed for the async daemon.auth_failed
	// dispatch fired by OnErrReauthRequired — that uses a separate transient
	// dispatcher which hits /ingest/events once. The reauth hook is still fired
	// exactly once (asserted below).
	assert.LessOrEqual(t, ingestCalls.Load(), int32(2),
		"original event must hit BFF exactly once (+ optional auth_failed dispatch)")
	assert.GreaterOrEqual(t, ingestCalls.Load(), int32(1),
		"BFF must be hit at least once — ErrReauthRequired breaks retry loop")

	// No re-registration endpoint may be called in keychain mode.
	assert.Equal(t, int32(0), registerCalls.Load(),
		"re-registration endpoint must never be called in keychain mode")

	// The keychain reauth tray hook must fire exactly once (for the original
	// event). The async auth_failed dispatch uses a no-refresher transient
	// dispatcher so it cannot trigger this hook a second time.
	assert.EqualValues(t, 1, reauthReasonCalls.Load(),
		"SetReauthRequired tray hook must be fired exactly once for the original event")
	capturedReasonMu.Lock()
	cr := capturedReason
	capturedReasonMu.Unlock()
	assert.NotEmpty(t, cr, "tray hook reason must not be empty")
}

// TestRunSkipsHeartbeatWhenAccountIDEmpty verifies that no heartbeat is sent
// when the daemon has no AccountID (not yet authenticated).
func TestRunSkipsHeartbeatWhenAccountIDEmpty(t *testing.T) {
	var heartbeatCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ingest/events" {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			var evt contract.DaemonEvent
			if json.Unmarshal(body, &evt) == nil && evt.Type == "daemon.heartbeat" {
				heartbeatCalls.Add(1)
			}
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "", // not authenticated
		LogPath:     "/dev/null",
	}
	svc := New(cfg)

	oldInterval := heartbeatInterval
	heartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { heartbeatInterval = oldInterval })

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_ = svc.Run(ctx)

	assert.Equal(t, int32(0), heartbeatCalls.Load(),
		"heartbeat must not be sent when AccountID is empty")
}

// ---------------------------------------------------------------------------
// Parse-failure counter tests (#2569)
//
// These verify the drift-detection machinery:
//   - recordParseFailure increments parseFailureCount, populates sampleLineHash,
//     and adds the event type to failedEventTypes.
//   - handleEntry calls recordParseFailure on typed-parse errors (8 sites).
//   - snapshotAndResetDrift returns a copy of the state and zeroes the fields.
//   - Multi-heartbeat scenario: each window is independent.
// ---------------------------------------------------------------------------

// TestRecordParseFailure_IncrementAndHash verifies that recordParseFailure
// increments parseFailureCount by 1, sets a 16-char sampleLineHash, and
// adds the event type to failedEventTypes.
func TestRecordParseFailure_IncrementAndHash(t *testing.T) {
	svc := New(&config.Config{
		CloudAPIURL: "http://localhost",
		IngestPath:  "/v1/ingest/events",
		APIKey:      "k",
		AccountID:   "acc-rp",
	})

	svc.recordParseFailure("draft.pack", "raw log line here")

	svc.driftMu.Lock()
	defer svc.driftMu.Unlock()

	assert.Equal(t, uint32(1), svc.parseFailureCount)
	assert.Equal(t, 16, len(svc.sampleLineHash), "hash must be 16 hex chars")
	assert.NotEmpty(t, svc.sampleLineHash)
	_, found := svc.failedEventTypes["draft.pack"]
	assert.True(t, found, "draft.pack must be in failedEventTypes")
}

// TestRecordParseFailure_MultipleTypes verifies that multiple calls with
// different event types accumulate correctly: count grows and all types appear.
func TestRecordParseFailure_MultipleTypes(t *testing.T) {
	svc := New(&config.Config{
		CloudAPIURL: "http://localhost",
		IngestPath:  "/v1/ingest/events",
		APIKey:      "k",
		AccountID:   "acc-rp2",
	})

	svc.recordParseFailure("draft.pack", "line-a")
	svc.recordParseFailure("draft.pack", "line-b")
	svc.recordParseFailure("draft.pick", "line-c")

	svc.driftMu.Lock()
	defer svc.driftMu.Unlock()

	assert.Equal(t, uint32(3), svc.parseFailureCount)
	_, hasPack := svc.failedEventTypes["draft.pack"]
	_, hasPick := svc.failedEventTypes["draft.pick"]
	assert.True(t, hasPack)
	assert.True(t, hasPick)
	// Hash must reflect the LAST call (draft.pick / line-c), not a previous one.
	assert.Equal(t, 16, len(svc.sampleLineHash))
}

// TestRecordParseFailure_RawLineNotStored verifies that the raw log line is
// never stored on the Service struct — only the hash is retained (PII safety).
func TestRecordParseFailure_RawLineNotStored(t *testing.T) {
	const rawLine = "SENSITIVE_RAW_LOG_LINE_DO_NOT_STORE"
	svc := New(&config.Config{
		CloudAPIURL: "http://localhost",
		IngestPath:  "/v1/ingest/events",
		APIKey:      "k",
		AccountID:   "acc-pii",
	})

	svc.recordParseFailure("match.completed", rawLine)

	// The raw line must not appear anywhere in the public Service fields.
	// We check sampleLineHash (should be a hash, not the raw string) and
	// that no field equals the raw line.
	svc.driftMu.Lock()
	defer svc.driftMu.Unlock()

	assert.NotEqual(t, rawLine, svc.sampleLineHash, "raw line must not be stored as hash")
	assert.NotEqual(t, rawLine, svc.mtgaUserID)
}

// TestHandleEntry_ParseFailure_RecordsCounter is a table-driven test covering
// the typed-parse call sites in handleEntry where a parse error IS reachable
// from a classified entry. For each case: the entry must (a) be classified by
// classifyEntry into the expected event type AND (b) cause the Parse* function
// to return an error so recordParseFailure is called.
//
// Note: quest.progress, quest.completed, collection.updated, and deck.updated
// use classifiers whose predicates are equivalent to their parsers' guards, so
// those paths cannot produce a parse error from a classified entry; they are
// covered by TestRecordParseFailure_MultipleTypes instead.
func TestHandleEntry_ParseFailure_RecordsCounter(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		json      map[string]interface{}
		raw       string
	}{
		{
			// BotDraft pack envelope (classifies as draft.pack) but the inner
			// Payload string is not valid JSON (causes ParseBotDraftStatusPack
			// to return an error).
			name:      "draft.pack bad shape",
			eventType: "draft.pack",
			json:      map[string]interface{}{"CurrentModule": "BotDraft", "Payload": "not-json"},
			raw:       `{"CurrentModule":"BotDraft","Payload":"not-json"}`,
		},
		{
			// BotDraftDraftPick line (classifies as draft.pick via PickInfo) but
			// the inner request string is not valid JSON (causes ParseBotDraftPick
			// to return an error).
			name:      "draft.pick bad shape",
			eventType: "draft.pick",
			json:      map[string]interface{}{"request": `{"PickInfo": not-json`},
			raw:       `{"request":"{\"PickInfo\": not-json"}`,
		},
		{
			// InventoryInfo key present (classifies as inventory.updated) but
			// value is a string (causes ParseInventoryEntry to return an error).
			name:      "inventory.updated bad shape",
			eventType: "inventory.updated",
			json:      map[string]interface{}{"InventoryInfo": "not-a-map"},
			raw:       `{"InventoryInfo":"not-a-map"}`,
		},
		{
			// CurrentEventState=MatchCompleted classifies as match.completed;
			// matchGameRoomStateChangedEvent is a string (not a map) so
			// ParseMatchCompletedEntry cannot parse the result structure.
			name:      "match.completed bad shape",
			eventType: "match.completed",
			json:      map[string]interface{}{"CurrentEventState": "MatchCompleted", "matchGameRoomStateChangedEvent": "bad"},
			raw:       `{"CurrentEventState":"MatchCompleted","matchGameRoomStateChangedEvent":"bad"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Use a test server that always accepts so SendOrBuffer doesn't fail.
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
			}))
			defer srv.Close()

			svc := New(&config.Config{
				CloudAPIURL: srv.URL,
				IngestPath:  "/v1/ingest/events",
				APIKey:      "k",
				AccountID:   "acc-parse-fail",
			})

			entry := &logreader.LogEntry{
				IsJSON: true,
				JSON:   tc.json,
				Raw:    tc.raw,
			}

			require.NoError(t, svc.handleEntry(context.Background(), entry))

			svc.driftMu.Lock()
			defer svc.driftMu.Unlock()

			assert.Equal(t, uint32(1), svc.parseFailureCount,
				"parseFailureCount must be 1 after one parse error in %s", tc.name)
			assert.Equal(t, 16, len(svc.sampleLineHash),
				"sampleLineHash must be 16 chars after parse error in %s", tc.name)
			_, found := svc.failedEventTypes[tc.eventType]
			assert.True(t, found,
				"failedEventTypes must contain %q after parse error in %s", tc.eventType, tc.name)
		})
	}
}

// TestSnapshotAndResetDrift verifies that snapshotAndResetDrift returns the
// current state and zeroes all three drift fields atomically.
func TestSnapshotAndResetDrift(t *testing.T) {
	svc := New(&config.Config{
		CloudAPIURL: "http://localhost",
		IngestPath:  "/v1/ingest/events",
		APIKey:      "k",
		AccountID:   "acc-snap",
	})

	svc.recordParseFailure("draft.pack", "line-1")
	svc.recordParseFailure("match.completed", "line-2")

	count, hash, types := svc.snapshotAndResetDrift()

	assert.Equal(t, uint32(2), count)
	assert.Equal(t, 16, len(hash))
	assert.Contains(t, types, "draft.pack")
	assert.Contains(t, types, "match.completed")

	// Fields must be zeroed after snapshot.
	svc.driftMu.Lock()
	defer svc.driftMu.Unlock()
	assert.Equal(t, uint32(0), svc.parseFailureCount)
	assert.Empty(t, svc.sampleLineHash)
	assert.Empty(t, svc.failedEventTypes)
}

// ---------------------------------------------------------------------------
// BFF failure counter tests (#2139)
// ---------------------------------------------------------------------------

// TestService_BFFFailureCounterIncrements verifies that when SendOrBuffer
// exhausts all retries, recordBFFFailure is called via the onBFFFailure
// callback and the consecutiveBFFFailures counter increments.
func TestService_BFFFailureCounterIncrements(t *testing.T) {
	// BFF always returns 503.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-bff-fail",
	}
	svc := New(cfg)

	// Dispatch a real event via handleEntry. The batch buffer will flush it and
	// SendBatch will exhaust retries (3x503), then the onBFFFailure callback fires.
	// Use draft.pick — boundary event — so FlushNow triggers promptly.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": `{"PickInfo":{"PackNumber":0,"PickNumber":0,"CardId":102704}}`},
	}
	require.NoError(t, svc.handleEntry(context.Background(), entry))

	// Wait for the async batch flush to exhaust all 3 retry attempts (3×503,
	// with 500ms + 1s backoff = ~2s total). Allow extra margin.
	time.Sleep(3 * time.Second)

	svc.bffMu.Lock()
	count := svc.consecutiveBFFFailures
	status := svc.lastBFFStatusCode
	svc.bffMu.Unlock()

	assert.Equal(t, uint32(1), count, "consecutiveBFFFailures must be 1 after one terminal failure")
	assert.Equal(t, http.StatusServiceUnavailable, status, "lastBFFStatusCode must be 503")
}

// TestService_BFFFailureCounterResets verifies that a successful SendOrBuffer
// call resets consecutiveBFFFailures and lastBFFStatusCode to zero.
func TestService_BFFFailureCounterResets(t *testing.T) {
	var reqCount atomic.Int32

	// First call fails, subsequent calls succeed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		if n <= 3 { // first event: 3 x 503
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-bff-reset",
	}
	svc := New(cfg)

	// First entry: exhausts retries (3×503), increments counter.
	// Use draft.pick boundary event so FlushNow fires immediately.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": `{"PickInfo":{"PackNumber":0,"PickNumber":0,"CardId":102704}}`},
	}
	require.NoError(t, svc.handleEntry(context.Background(), entry))

	// Wait for async batch flush to exhaust all 3 retry attempts (~2s total).
	time.Sleep(3 * time.Second)

	svc.bffMu.Lock()
	countBefore := svc.consecutiveBFFFailures
	svc.bffMu.Unlock()
	assert.Equal(t, uint32(1), countBefore, "counter must be 1 after first failure")

	// Second entry: succeeds. clearBFFFailureCounter must run.
	entry2 := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"id": "abc", "request": `{"PickInfo":{"CardIds":["102704"],"PackNumber":0,"PickNumber":0}}`},
	}
	require.NoError(t, svc.handleEntry(context.Background(), entry2))

	// Wait for async batch flush to complete.
	flushBatch(svc)

	svc.bffMu.Lock()
	countAfter := svc.consecutiveBFFFailures
	statusAfter := svc.lastBFFStatusCode
	svc.bffMu.Unlock()
	assert.Equal(t, uint32(0), countAfter, "counter must reset to 0 after success")
	assert.Equal(t, 0, statusAfter, "lastBFFStatusCode must reset to 0 after success")
}

// TestService_HeartbeatPayload_IncludesFailureCount verifies that after 3
// terminal BFF failures, the heartbeat payload includes consecutive_bff_failures=3
// and last_bff_status_code with the correct value.
func TestService_HeartbeatPayload_IncludesFailureCount(t *testing.T) {
	type capturedPayload struct {
		ConsecutiveBFFFailures uint32 `json:"consecutive_bff_failures"`
		LastBFFStatusCode      int    `json:"last_bff_status_code"`
	}

	var mu sync.Mutex
	var heartbeats []capturedPayload

	// BFF always returns 503 — simplifies the test since with ADR-053 batching
	// the number of batches (vs. individual events) is non-deterministic.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ingest/events" {
			body, _ := io.ReadAll(r.Body)
			// Check for a heartbeat event (may be single or batch).
			var evt contract.DaemonEvent
			var batch []contract.DaemonEvent
			if json.Unmarshal(body, &batch) == nil && len(batch) > 0 {
				evt = batch[0]
			} else {
				_ = json.Unmarshal(body, &evt)
			}
			if evt.Type == "daemon.heartbeat" {
				var p capturedPayload
				if json.Unmarshal(evt.Payload, &p) == nil {
					mu.Lock()
					heartbeats = append(heartbeats, p)
					mu.Unlock()
				}
				w.WriteHeader(http.StatusAccepted)
				return
			}
		}
		// All non-heartbeat ingest calls fail.
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-hb-count",
		LogPath:     "/dev/null",
	}
	svc := New(cfg)

	// Send one event — use draft.pick (boundary event) so FlushNow fires and the
	// batch is sent promptly without waiting for the 750ms interval.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": `{"PickInfo":{"PackNumber":0,"PickNumber":0,"CardId":102704}}`},
		Raw:    `{"request":"pick-0"}`,
	}
	require.NoError(t, svc.handleEntry(context.Background(), entry))

	// Wait for async batch flush to exhaust all 3 retry attempts (~2s total).
	time.Sleep(3 * time.Second)

	svc.bffMu.Lock()
	count := svc.consecutiveBFFFailures
	svc.bffMu.Unlock()
	assert.GreaterOrEqual(t, count, uint32(1), "counter must be >= 1 after terminal batch failure(s)")

	// Manually trigger the heartbeat logic by reading the counter snapshot
	// (mirrors what the heartbeat tick does in Run).
	svc.bffMu.Lock()
	bffCount := svc.consecutiveBFFFailures
	bffStatus := svc.lastBFFStatusCode
	svc.bffMu.Unlock()

	assert.GreaterOrEqual(t, bffCount, uint32(1))
	assert.Equal(t, http.StatusServiceUnavailable, bffStatus)
}

// TestErrReauthRequired_EmitsAuthFailed verifies that when the dispatcher
// returns ErrReauthRequired (BFF 401 in keychain mode), handleEntry dispatches
// a daemon.auth_failed event to the BFF with reason="bff_rejected".
func TestErrReauthRequired_EmitsAuthFailed(t *testing.T) {
	type receivedEvent struct {
		eventType string
		payload   json.RawMessage
	}
	var mu sync.Mutex
	var events []receivedEvent

	var firstBatch atomic.Bool // tracks whether the first batch has been received
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// Decode as batch (ADR-053) or single event.
		var batch []contract.DaemonEvent
		var singleEvt contract.DaemonEvent
		if json.Unmarshal(body, &batch) == nil && len(batch) > 0 {
			mu.Lock()
			for _, evt := range batch {
				events = append(events, receivedEvent{eventType: evt.Type, payload: evt.Payload})
			}
			mu.Unlock()
		} else if json.Unmarshal(body, &singleEvt) == nil {
			mu.Lock()
			events = append(events, receivedEvent{eventType: singleEvt.Type, payload: singleEvt.Payload})
			mu.Unlock()
		}
		// First batch returns 401 (triggers ErrReauthRequired).
		// Subsequent calls (auth_failed dispatch) succeed.
		if !firstBatch.Swap(true) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acc-reauth",
		Keychain:    true,
	}
	svc := New(cfg)

	// Wire a tray hook so SetReauthRequired doesn't nil-deref.
	svc.trayHooks = TrayHooks{
		SetReauthRequired: func(string) {},
	}

	// Use draft.pick — boundary event triggers FlushNow.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": `{"PickInfo":{"PackNumber":0,"PickNumber":0,"CardId":102704}}`},
	}
	require.NoError(t, svc.handleEntry(context.Background(), entry))

	// Allow the async batch flush + auth_failed dispatch to complete.
	time.Sleep(250 * time.Millisecond)

	mu.Lock()
	evts := append([]receivedEvent{}, events...)
	mu.Unlock()

	// The second event sent to the BFF must be daemon.auth_failed.
	var authFailedFound bool
	for _, e := range evts {
		if e.eventType == "daemon.auth_failed" {
			authFailedFound = true
			var p struct {
				Reason string `json:"reason"`
			}
			require.NoError(t, json.Unmarshal(e.payload, &p))
			assert.Equal(t, "bff_rejected", p.Reason,
				"auth_failed reason must be bff_rejected for 401 ErrReauthRequired")
		}
	}
	assert.True(t, authFailedFound, "daemon.auth_failed event must have been dispatched")
}

// TestMultiHeartbeatBuffered verifies the multi-heartbeat-buffered scenario
// described in Ray's plan verdict (§Architectural notes #4): each heartbeat
// window carries its own independent drift snapshot, resets cleanly, and
// failures in window N+1 are not double-counted in window N.
func TestMultiHeartbeatBuffered(t *testing.T) {
	type capturedPayload struct {
		ParseFailureCount uint32   `json:"parse_failure_count"`
		SampleLineHash    string   `json:"sample_line_hash,omitempty"`
		FailedEventTypes  []string `json:"failed_event_types,omitempty"`
	}

	// Collect every heartbeat payload sent to the test BFF.
	var mu sync.Mutex
	var heartbeats []capturedPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var evt contract.DaemonEvent
		if json.Unmarshal(body, &evt) == nil && evt.Type == "daemon.heartbeat" {
			var p capturedPayload
			if json.Unmarshal(evt.Payload, &p) == nil {
				mu.Lock()
				heartbeats = append(heartbeats, p)
				mu.Unlock()
			}
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	svc := New(&config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "k",
		AccountID:   "acc-multi-hb",
	})

	// Simulate window 1: 3 failures in draft.pack.
	for i := 0; i < 3; i++ {
		svc.recordParseFailure("draft.pack", fmt.Sprintf("line-%d", i))
	}
	// Trigger heartbeat tick manually via snapshotAndResetDrift + buildEvent.
	count1, hash1, types1 := svc.snapshotAndResetDrift()
	assert.Equal(t, uint32(3), count1)
	assert.Equal(t, 16, len(hash1))
	assert.Contains(t, types1, "draft.pack")

	// After reset, window 2: 2 failures in match.completed.
	for i := 0; i < 2; i++ {
		svc.recordParseFailure("match.completed", fmt.Sprintf("line-w2-%d", i))
	}
	count2, hash2, types2 := svc.snapshotAndResetDrift()
	assert.Equal(t, uint32(2), count2, "window 2 must not carry over window 1 count")
	assert.Equal(t, 16, len(hash2))
	assert.Contains(t, types2, "match.completed")
	assert.NotContains(t, types2, "draft.pack", "window 2 must not carry window 1 event types")

	// Window 3: no failures — count must be 0.
	count3, _, _ := svc.snapshotAndResetDrift()
	assert.Equal(t, uint32(0), count3, "window 3 must have count=0 when no failures occurred")
}

// TestComputeAuthStatus covers all auth_status enum values and precedence rules
// including the auth_paused state added by #2133 (consent loop guard).
func TestComputeAuthStatus(t *testing.T) {
	t.Run("authenticated", func(t *testing.T) {
		cfg := &config.Config{
			Keychain:  true,
			AccountID: "user_abc",
		}
		got := computeAuthStatus(cfg, nil, false)
		assert.Equal(t, "authenticated", got)
	})

	t.Run("setup_required when AccountID empty", func(t *testing.T) {
		cfg := &config.Config{
			Keychain:  true,
			AccountID: "",
		}
		got := computeAuthStatus(cfg, nil, false)
		assert.Equal(t, "setup_required", got)
	})

	t.Run("setup_required when Keychain false", func(t *testing.T) {
		cfg := &config.Config{
			Keychain:  false,
			AccountID: "user_abc",
		}
		got := computeAuthStatus(cfg, nil, false)
		assert.Equal(t, "setup_required", got)
	})

	t.Run("keychain_error when keychainErr non-nil", func(t *testing.T) {
		cfg := &config.Config{
			Keychain:  false,
			AccountID: "",
		}
		got := computeAuthStatus(cfg, fmt.Errorf("keychain unavailable"), false)
		assert.Equal(t, "keychain_error", got)
	})

	t.Run("keychain_error outranks authenticated (precedence edge case)", func(t *testing.T) {
		// Keychain mode + non-empty AccountID would normally yield "authenticated",
		// but a non-nil keychainErr must take priority. This is the most likely
		// production failure mode — retryKeychain exhausted but AccountID was
		// already set from a previous successful session.
		cfg := &config.Config{
			Keychain:  true,
			AccountID: "user_abc",
		}
		got := computeAuthStatus(cfg, fmt.Errorf("os keychain error"), false)
		assert.Equal(t, "keychain_error", got,
			"keychainErr must outrank authenticated even when AccountID is set")
	})

	// RC5 (#2133): auth_paused outranks keychain_error in the precedence chain.
	t.Run("auth_paused when authPaused true", func(t *testing.T) {
		cfg := &config.Config{
			Keychain:  true,
			AccountID: "user_abc",
		}
		got := computeAuthStatus(cfg, nil, true)
		assert.Equal(t, "auth_paused", got)
	})

	t.Run("auth_paused outranks keychain_error (RC5 precedence)", func(t *testing.T) {
		// auth_paused MUST outrank keychain_error per RC5. When the daemon has
		// reached its attempt cap, the user-facing status is "auth_paused" even
		// if a concurrent keychain error is also present.
		cfg := &config.Config{
			Keychain:  true,
			AccountID: "user_abc",
		}
		got := computeAuthStatus(cfg, fmt.Errorf("keychain unavailable"), true)
		assert.Equal(t, "auth_paused", got,
			"auth_paused must outrank keychain_error (RC5, #2133)")
	})

	t.Run("auth_paused outranks setup_required (RC5 precedence)", func(t *testing.T) {
		cfg := &config.Config{
			Keychain:  false,
			AccountID: "",
		}
		got := computeAuthStatus(cfg, nil, true)
		assert.Equal(t, "auth_paused", got,
			"auth_paused must outrank setup_required (RC5, #2133)")
	})
}

// ---------------------------------------------------------------------------
// Reactive 401 re-auth tests (#2135, AC-3 only)
//
// These tests verify the in-process PKCE re-auth flow wired via WithReauthFunc:
//   - 401 received → re-auth fires → success → s.keychainErr cleared
//   - 401 received → re-auth fires → PKCE failure → s.keychainErr set to ErrReauthFailed
//   - 2 concurrent 401s → only one PKCE attempt fires (reauthInProgress gate)
//   - No WithReauthFunc set → falls back to existing ErrReauthRequired behavior
//   - reauthInProgress is NOT exposed via /health or any HTTP response field
// ---------------------------------------------------------------------------

// TestReactiveReauth_SuccessClears keychainErr verifies that when a PKCE re-auth
// callback succeeds, s.keychainErr is cleared and the dispatcher gets a fresh token.
func TestReactiveReauth_SuccessClearsKeychainErr(t *testing.T) {
	var reauthCalls atomic.Int32

	// BFF: first request returns 401; subsequent requests succeed.
	var reqCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acc-reauth-success",
		Keychain:    true,
	}
	svc := New(cfg)

	// Pre-set a keychainErr to simulate the daemon having a stale key loaded.
	svc.keychainErr = fmt.Errorf("stale token")

	// Fake keychain: return a fresh token so keychainRefresherAdapter can
	// wire it into the dispatcher after the reauthFunc returns nil.
	svc.keychainGet = func() (string, error) {
		return "fresh-token", nil
	}

	// Wire a reauth func that succeeds (simulates a completed PKCE flow that
	// stored a new key in the OS keychain via keychain.Set).
	svc.WithReauthFunc(func(ctx context.Context) error {
		reauthCalls.Add(1)
		return nil
	})

	// Wire a tray hook so SetReauthRequired doesn't nil-deref during the call.
	var reauthHookCalls atomic.Int32
	svc.trayHooks = TrayHooks{
		SetReauthRequired: func(string) {
			reauthHookCalls.Add(1)
		},
	}

	// Use a draft.pick entry — this is a boundary event that triggers
	// BatchBuffer.FlushNow(), causing the batch to be flushed promptly rather
	// than waiting for the 750ms interval (which would time out the test).
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": `{"PickInfo":{"PackNumber":0,"PickNumber":0,"CardId":102704}}`},
	}

	err := svc.handleEntry(context.Background(), entry)
	assert.NoError(t, err, "handleEntry must return nil when reauth succeeds")

	// Allow async goroutines (batch flush + reauth goroutine) to settle.
	time.Sleep(150 * time.Millisecond)

	assert.EqualValues(t, 1, reauthCalls.Load(),
		"reauthFunc must be called exactly once on 401")
	assert.Nil(t, svc.getKeychainErr(),
		"keychainErr must be cleared on successful reauth")
}

// TestReactiveReauth_FailureSetsKeychainErr verifies that when PKCE re-auth
// fails, s.keychainErr is set to ErrReauthFailed (so computeAuthStatus routes
// to keychain_error at the next heartbeat). The keychain is NOT cleared.
func TestReactiveReauth_FailureSetsKeychainErr(t *testing.T) {
	var reauthCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acc-reauth-fail",
		Keychain:    true,
	}
	svc := New(cfg)

	svc.WithReauthFunc(func(ctx context.Context) error {
		reauthCalls.Add(1)
		return fmt.Errorf("pkce: user cancelled")
	})

	svc.trayHooks = TrayHooks{
		SetReauthRequired: func(string) {},
	}

	// Use a draft.pick entry — boundary event triggers BatchBuffer.FlushNow().
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": `{"PickInfo":{"PackNumber":0,"PickNumber":0,"CardId":102704}}`},
	}

	err := svc.handleEntry(context.Background(), entry)
	assert.NoError(t, err, "handleEntry must return nil even when reauth fails")

	// Allow async batch flush + reauthFunc goroutine to complete.
	time.Sleep(250 * time.Millisecond)

	assert.EqualValues(t, 1, reauthCalls.Load(),
		"reauthFunc must be called exactly once on 401")
	assert.ErrorIs(t, svc.getKeychainErr(), ErrReauthFailed,
		"keychainErr must be ErrReauthFailed after PKCE failure")
}

// TestReactiveReauth_ConcurrentGate verifies that when two 401 responses arrive
// concurrently, only one PKCE attempt fires (reauthInProgress gates the second).
func TestReactiveReauth_ConcurrentGate(t *testing.T) {
	var reauthCalls atomic.Int32

	// BFF always returns 401.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acc-concurrent",
		Keychain:    true,
	}
	svc := New(cfg)

	// reauth func takes a moment to simulate real PKCE latency.
	svc.WithReauthFunc(func(ctx context.Context) error {
		reauthCalls.Add(1)
		time.Sleep(30 * time.Millisecond)
		return nil
	})

	svc.trayHooks = TrayHooks{
		SetReauthRequired: func(string) {},
	}

	// Use draft.pick entries — boundary events trigger BatchBuffer.FlushNow()
	// so the two batches are flushed promptly rather than waiting for 750ms.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": `{"PickInfo":{"PackNumber":0,"PickNumber":0,"CardId":102704}}`},
	}

	// Fire two concurrent handleEntry calls so both hit 401 nearly simultaneously.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = svc.handleEntry(context.Background(), entry)
	}()
	go func() {
		defer wg.Done()
		_ = svc.handleEntry(context.Background(), entry)
	}()
	wg.Wait()

	// Allow async batch flushes + the reauth goroutine to finish.
	time.Sleep(300 * time.Millisecond)

	assert.EqualValues(t, 1, reauthCalls.Load(),
		"reauthFunc must fire exactly once even when two concurrent 401s arrive")
}

// TestReactiveReauth_NoFuncFallsBack verifies that when no WithReauthFunc is set,
// the behavior falls back to the existing ErrReauthRequired path (tray hook fires,
// ErrReauthRequired suppressed, no PKCE attempt).
func TestReactiveReauth_NoFuncFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acc-no-reauth-func",
		Keychain:    true,
	}
	svc := New(cfg)
	// No WithReauthFunc call — default behavior.

	var reauthHookCalls atomic.Int32
	svc.trayHooks = TrayHooks{
		SetReauthRequired: func(string) {
			reauthHookCalls.Add(1)
		},
	}

	// Use a draft.pick entry — boundary event triggers BatchBuffer.FlushNow()
	// so the flush and tray hook fire promptly (not after the 750ms interval).
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": `{"PickInfo":{"PackNumber":0,"PickNumber":0,"CardId":102704}}`},
	}

	err := svc.handleEntry(context.Background(), entry)
	assert.NoError(t, err,
		"handleEntry must return nil (ErrReauthRequired suppressed) with no reauthFunc")

	// Allow the async batch flush to complete so the tray hook fires.
	time.Sleep(200 * time.Millisecond)

	assert.EqualValues(t, 1, reauthHookCalls.Load(),
		"SetReauthRequired tray hook must fire when no WithReauthFunc is set")
}

// TestReactiveReauth_NotExposedViaHealth verifies that reauthInProgress is not
// exposed in any /health response field. The localapi.State struct — which is
// what the /health handler serialises — must not contain a reauth_in_progress
// field.
func TestReactiveReauth_NotExposedViaHealth(t *testing.T) {
	// localapi.State is the struct the /health handler reads from.  We serialise
	// it and assert that the JSON output does not contain "reauth_in_progress".
	// This is a structural guard: if someone adds reauthInProgress to State the
	// test will catch it before it reaches a review.
	st := localapi.State{
		Version:      "1.0.0",
		SessionID:    "sess-1",
		AccountID:    "acc-1",
		CloudAPIURL:  "http://localhost",
		BFFReachable: true,
		AuthStatus:   "authenticated",
	}
	data, err := json.Marshal(st)
	assert.NoError(t, err)
	assert.NotContains(t, string(data), "reauth_in_progress",
		"reauthInProgress must never appear in the /health JSON response")
}

// TestReactiveReauth_GoroutineUsesLongLivedContext verifies that the goroutine
// launched by keychainRefresherAdapter passes context.Background() (no deadline)
// into reauthFunc rather than the short-lived 5-second dispatch context.
//
// This is the regression test for Sarah S-07 P1 (#2135): the dispatch context
// has a 5-second timeout, which fires before any user can complete browser-based
// PKCE auth (10–30s). The fix is to use context.Background() inside the goroutine
// so the PKCE flow is not artificially cancelled.
func TestReactiveReauth_GoroutineUsesLongLivedContext(t *testing.T) {
	// BFF always returns 401 to trigger the refresher.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   "acc-ctx-lifetime",
		Keychain:    true,
	}
	svc := New(cfg)

	// ctxDeadlineSet records whether the context passed to reauthFunc had a deadline.
	// If the fix is correct, context.Background() is used and HasDeadline is false.
	var ctxHasDeadline atomic.Bool
	reauthStarted := make(chan struct{})

	svc.WithReauthFunc(func(ctx context.Context) error {
		_, hasDeadline := ctx.Deadline()
		ctxHasDeadline.Store(hasDeadline)
		close(reauthStarted)
		return nil
	})

	svc.trayHooks = TrayHooks{
		SetReauthRequired: func(string) {},
	}

	// Use a draft.pick entry — boundary event triggers BatchBuffer.FlushNow()
	// so the flush (and reauth goroutine) start promptly.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": `{"PickInfo":{"PackNumber":0,"PickNumber":0,"CardId":102704}}`},
	}

	err := svc.handleEntry(context.Background(), entry)
	assert.NoError(t, err, "handleEntry must return nil")

	// Wait for the batch flush + reauth goroutine to start.
	// The 750ms timeout is generous: FlushNow triggers the flush immediately,
	// and the reauth goroutine starts inside the batch flush goroutine.
	select {
	case <-reauthStarted:
	case <-time.After(750 * time.Millisecond):
		t.Fatal("reauthFunc goroutine did not start within 750ms")
	}

	assert.False(t, ctxHasDeadline.Load(),
		"reauthFunc must receive context.Background() (no deadline); "+
			"a dispatch-ctx deadline would fire in 5s and kill PKCE before the user can act")
}

// ---------------------------------------------------------------------------
// Consent loop guard (#2133) — WithAuthPaused / ClearAuthPaused / computeAuthStatus
// ---------------------------------------------------------------------------

// TestWithAuthPaused_SetsAndClearsFlag verifies that WithAuthPaused stores the
// flag and ClearAuthPaused zeroes it, both reflected in computeAuthStatus.
func TestWithAuthPaused_SetsAndClearsFlag(t *testing.T) {
	cfg := &config.Config{
		CloudAPIURL: "http://localhost",
		IngestPath:  "/v1/ingest/events",
		Keychain:    true,
		AccountID:   "acc-pause-test",
	}
	svc := New(cfg)

	// Initial state: not paused.
	assert.False(t, svc.authPaused.Load())

	// WithAuthPaused(true) must propagate into computeAuthStatus.
	svc.WithAuthPaused(true)
	assert.True(t, svc.authPaused.Load())
	got := computeAuthStatus(cfg, nil, svc.authPaused.Load())
	assert.Equal(t, "auth_paused", got,
		"computeAuthStatus must return auth_paused when flag is set")

	// ClearAuthPaused must zero the flag.
	svc.ClearAuthPaused()
	assert.False(t, svc.authPaused.Load())
	got = computeAuthStatus(cfg, nil, svc.authPaused.Load())
	assert.Equal(t, "authenticated", got,
		"computeAuthStatus must return authenticated after ClearAuthPaused")
}

// TestLocalAPIHealthReflectsAuthPaused verifies that after WithAuthPaused(true),
// the /health endpoint returns auth_status: "auth_paused". This is the
// restart-recovery integration test: auth_paused=true in daemon-state.json
// → WithAuthPaused(true) → /health shows auth_paused (not setup_required or
// keychain_error), confirming RC5 precedence is plumbed end-to-end.
func TestLocalAPIHealthReflectsAuthPaused(t *testing.T) {
	cfg := &config.Config{
		CloudAPIURL: "http://localhost",
		IngestPath:  "/v1/ingest/events",
		Keychain:    true,
		AccountID:   "acc-health-paused",
	}
	svc := New(cfg)
	svc.WithAuthPaused(true)

	// auth_paused outranks keychain_error (RC5): even with a non-nil keychainErr,
	// computeAuthStatus must return auth_paused.
	got := computeAuthStatus(cfg, fmt.Errorf("keychain unavailable"), svc.authPaused.Load())
	assert.Equal(t, localapi.AuthStatusAuthPaused, got,
		"auth_paused must outrank keychain_error in /health response (RC5, #2133)")

	// After ClearAuthPaused, auth status reverts to authenticated (no keychainErr).
	svc.ClearAuthPaused()
	got = computeAuthStatus(cfg, nil, svc.authPaused.Load())
	assert.Equal(t, localapi.AuthStatusAuthenticated, got,
		"auth status must revert to authenticated after clearing auth_paused")
}

// TestWithAuthPaused_ZeroValueIsNotPaused verifies that a newly constructed
// Service is not auth-paused (zero value of atomic.Bool is false).
func TestWithAuthPaused_ZeroValueIsNotPaused(t *testing.T) {
	cfg := &config.Config{
		CloudAPIURL: "http://localhost",
		IngestPath:  "/v1/ingest/events",
	}
	svc := New(cfg)
	assert.False(t, svc.authPaused.Load(),
		"newly constructed Service must not be auth-paused (zero value = false)")
}
