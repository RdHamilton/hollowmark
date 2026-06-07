package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/contract"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/config"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/draftstate"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/logreader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClassifier_DraftEndedIsDraftCompleted locks in that a scene-change
// entry with fromSceneName="Draft" now produces "draft.completed" (not the
// old "draft.ended" which was silently dropped by the BFF).
func TestClassifier_DraftEndedIsDraftCompleted(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"toSceneName":   "Home",
			"fromSceneName": "Draft",
		},
	}
	got := classifyEntry(entry)
	assert.Equal(t, "draft.completed", got, "classifier must emit draft.completed (not draft.ended) when leaving the Draft scene")
}

// TestClassifier_DraftStartedUnchanged guards non-regression: entering the
// Draft scene still produces "draft.started".
func TestClassifier_DraftStartedUnchanged(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"toSceneName":   "Draft",
			"fromSceneName": "Home",
		},
	}
	assert.Equal(t, "draft.started", classifyEntry(entry))
}

// TestDraftPickPayload_SessionIDAttached verifies that when the daemon
// dispatches a draft.pick event, the emitted payload carries a non-empty
// session_id matching the active draftstate.Session.ID.
func TestDraftPickPayload_SessionIDAttached(t *testing.T) {
	// Seed draftstate with a recent time so the session is fresh.
	fixedNow := time.Now().UTC().Add(-2 * time.Hour)
	store := draftstate.New()
	store.SetClock(func() time.Time { return fixedNow })

	store.HandlePack(&logreader.DraftPackPayload{
		CourseName: "QuickDraft_SOS_20260526",
		DraftPack: logreader.DraftPackDetail{
			PackCards: []int{102470},
			SelfPick:  1,
		},
	})
	sess, ok := store.Get("current")
	require.True(t, ok, "expected active session after HandlePack")
	expectedSessionID := sess.ID

	// Capture what the daemon sends to the BFF.
	// Uses eventCapture for goroutine-safe access (ADR-053 async batch dispatch).
	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestServiceWithStore(t, srv.URL, store)

	// Emit a BotDraft pick entry. CardIds are strings on the wire.
	// draft.pick is a boundary event — FlushNow is called after Add so the
	// batch reaches the BFF without waiting for the 750ms interval.
	pickEntry := &logreader.LogEntry{
		IsJSON: true,
		Raw:    `{}`,
		JSON: map[string]interface{}{
			"id":      "pick1",
			"request": `{"PickInfo":{"CardIds":["102704"],"PackNumber":0,"PickNumber":0}}`,
		},
	}
	err := svc.handleEntry(context.Background(), pickEntry)
	require.NoError(t, err)

	// Allow the async batch flush to complete.
	time.Sleep(100 * time.Millisecond)

	captured := cap.get()
	require.Equal(t, "draft.pick", captured.Type)

	var pickPayload logreader.DraftPickPayload
	require.NoError(t, json.Unmarshal(captured.Payload, &pickPayload))
	// The key invariant: session_id is non-empty (the daemon attached whatever
	// the current draftstate session ID is after processing the pick).
	assert.NotEmpty(t, pickPayload.SessionID, "draft.pick payload must carry a non-empty session ID")
	// Verify it matches the draftstate store's current session.
	sess2, ok2 := store.Get("current")
	require.True(t, ok2, "store must have a current session after pick")
	assert.Equal(t, sess2.ID, pickPayload.SessionID, "draft.pick payload SessionID must match draftstate current session")
	_ = expectedSessionID // original seeded ID; pick may update currentID
}

// TestMatchCompletedPayload_DraftSessionIDAttached verifies that when the
// match Format matches the active session's CourseName, the emitted
// match.completed payload carries DraftSessionID.
func TestMatchCompletedPayload_DraftSessionIDAttached(t *testing.T) {
	// Use a recent time so the 48-hour window check in the daemon passes.
	fixedNow := time.Now().UTC().Add(-1 * time.Hour)
	store := draftstate.New()
	store.SetClock(func() time.Time { return fixedNow })

	store.HandlePack(&logreader.DraftPackPayload{
		CourseName: "QuickDraft_SOS_20260526",
		DraftPack: logreader.DraftPackDetail{
			PackCards: []int{102470},
			SelfPick:  1,
		},
	})
	sess, ok := store.Get("current")
	require.True(t, ok)
	expectedSessionID := sess.ID

	var cap1 eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap1.capture(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestServiceWithStore(t, srv.URL, store)

	// Emit a match.completed entry with a matching eventId.
	matchEntry := buildMatchCompletedEntry("QuickDraft_SOS_20260526")
	err := svc.handleEntry(context.Background(), matchEntry)
	require.NoError(t, err)

	// Allow the async batch flush to complete (match.completed is not a
	// forced-flush event; wait for the 750ms interval or trigger explicitly).
	svc.batchBuffer.FlushNow()
	time.Sleep(100 * time.Millisecond)

	captured1 := cap1.get()
	require.Equal(t, "match.completed", captured1.Type)
	var matchPayload contract.MatchCompletedPayload
	require.NoError(t, json.Unmarshal(captured1.Payload, &matchPayload))
	require.NotNil(t, matchPayload.DraftSessionID, "match.completed must carry DraftSessionID when format matches active session")
	assert.Equal(t, expectedSessionID, *matchPayload.DraftSessionID)
}

// TestMatchCompletedPayload_NoDraftSessionID_NonDraftFormat verifies that a
// non-draft match (Format="Ladder") gets nil DraftSessionID even when a draft
// session is active in memory.
func TestMatchCompletedPayload_NoDraftSessionID_NonDraftFormat(t *testing.T) {
	fixedNow := time.Now().UTC().Add(-30 * time.Minute)
	store := draftstate.New()
	store.SetClock(func() time.Time { return fixedNow })

	// A quick draft session is active.
	store.HandlePack(&logreader.DraftPackPayload{
		CourseName: "QuickDraft_SOS_20260526",
		DraftPack: logreader.DraftPackDetail{
			PackCards: []int{102470},
			SelfPick:  1,
		},
	})

	var cap2 eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap2.capture(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestServiceWithStore(t, srv.URL, store)

	// Emit a Ladder match entry — Format does NOT match session's CourseName.
	matchEntry := buildMatchCompletedEntry("Ladder")
	err := svc.handleEntry(context.Background(), matchEntry)
	require.NoError(t, err)

	// Flush and wait for async batch dispatch to complete.
	svc.batchBuffer.FlushNow()
	time.Sleep(100 * time.Millisecond)

	captured2 := cap2.get()
	var matchPayload contract.MatchCompletedPayload
	require.NoError(t, json.Unmarshal(captured2.Payload, &matchPayload))
	assert.Nil(t, matchPayload.DraftSessionID, "Ladder match must not carry DraftSessionID")
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// newTestServiceWithStore creates a minimal Service with a pre-populated
// draftstate.Store, suitable for handleEntry tests.
func newTestServiceWithStore(t *testing.T, bffURL string, store *draftstate.Store) *Service {
	t.Helper()
	cfg := &config.Config{
		CloudAPIURL: bffURL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "test-acct-id",
	}
	svc := New(cfg)
	svc.draftState = store
	return svc
}

// buildMatchCompletedEntry constructs a minimal matchGameRoomStateChangedEvent
// LogEntry for the given eventId (used as Format in the payload).
// The structure matches what IsMatchCompletedEntry and ParseMatchCompletedEntry
// expect. eventId lives inside each reservedPlayers[] entry per the real MTGA
// wire format (not as a top-level gameRoomConfig key).
func buildMatchCompletedEntry(eventName string) *logreader.LogEntry {
	// reservedPlayers carries eventId per entry — ParseMatchCompletedEntry
	// reads p.Format from the first player entry that has it.
	player := map[string]interface{}{
		"userId":     "player-uid",
		"playerName": "TestPlayer",
		"teamId":     float64(1),
		"eventId":    eventName,
	}
	gameRoomConfig := map[string]interface{}{
		"reservedPlayers": []interface{}{player},
	}
	gameRoomInfo := map[string]interface{}{
		"gameRoomConfig": gameRoomConfig,
		"stateType":      "MatchGameRoomStateType_MatchCompleted",
		"finalMatchResult": map[string]interface{}{
			"resultList": []interface{}{
				map[string]interface{}{
					"scope":         "MatchScope_Match",
					"result":        "ResultType_Win",
					"winningTeamId": float64(1),
				},
			},
		},
	}
	return &logreader.LogEntry{
		IsJSON: true,
		Raw:    `{}`,
		JSON: map[string]interface{}{
			"matchGameRoomStateChangedEvent": map[string]interface{}{
				"gameRoomInfo": gameRoomInfo,
			},
			"matchId": "test-match-id-" + eventName,
		},
	}
}
