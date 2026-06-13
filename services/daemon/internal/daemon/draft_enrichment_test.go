package daemon

// draft_enrichment_test.go — TDD tests for PR-B (#1344 Defect 2b).
//
// Failing state (pre-fix):
//   - draft.started falls through to entry.JSON (no session_id) → projectDraftSession
//     permanent-rejects it → draft sessions are never opened via the started event.
//   - draft.completed falls through to entry.JSON (no session_id) → projectDraftSession
//     permanent-rejects it → draft sessions are NEVER closed (all formats, incl Premier).
//   - Neither event is in the FlushNow set, so they may wait up to 750 ms.
//
// Fix required:
//   - handleEntry enriches draft.started using the current draftstate session when
//     one is active, emitting session_id + set_code + draft_type.
//   - handleEntry enriches draft.completed the same way.
//   - Both events are added to the FlushNow boundary set.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/draftstate"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/logreader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// draftSessionEventPayload mirrors the draftPayload struct in worker.go so we can
// verify the enriched fields without importing the BFF package.
type draftSessionEventPayload struct {
	SessionID string `json:"session_id"`
	EventName string `json:"event_name"`
	SetCode   string `json:"set_code"`
	DraftType string `json:"draft_type"`
	Status    string `json:"status"`
}

// buildSceneChangeEntry returns a minimal log entry that classifies as
// draft.started (toSceneName=Draft) or draft.completed (fromSceneName=Draft).
func buildSceneChangeEntry(from, to string) *logreader.LogEntry {
	return &logreader.LogEntry{
		IsJSON: true,
		Raw:    `{}`,
		JSON: map[string]interface{}{
			"fromSceneName": from,
			"toSceneName":   to,
		},
	}
}

// seedDraftstate creates a draftstate.Store with one session for the given
// CourseName and returns both the store and the synthesized session ID.
func seedDraftstate(t *testing.T, courseName string, fixedNow time.Time) (*draftstate.Store, string) {
	t.Helper()
	store := draftstate.New()
	store.SetClock(func() time.Time { return fixedNow })
	store.HandlePack(&logreader.DraftPackPayload{
		CourseName: courseName,
		DraftPack: logreader.DraftPackDetail{
			PackCards: []int{102470},
			SelfPick:  1,
		},
	})
	sess, ok := store.Get("current")
	require.True(t, ok, "expected active session after HandlePack")
	return store, sess.ID
}

// TestDraftStarted_Enriched_EmitsSessionID verifies that when a draft.started
// scene-change entry fires and a draftstate session is active, the dispatched
// payload carries session_id, set_code, and draft_type.
//
// RED: before the fix, draft.started falls through to entry.JSON which has no
// session_id. The captured payload will have SessionID == "".
func TestDraftStarted_Enriched_EmitsSessionID(t *testing.T) {
	fixedNow := time.Now().UTC().Add(-5 * time.Minute)
	// Two-segment CourseName: splitCourse("QuickDraft_EOE") → Format="QuickDraft", SetCode="EOE".
	// Three-segment names (e.g. "QuickDraft_EOE_20260612") also work correctly after
	// the #1418 fix — both yield Format="QuickDraft", SetCode="EOE".
	store, wantSessionID := seedDraftstate(t, "QuickDraft_EOE", fixedNow)

	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestServiceWithStore(t, srv.URL, store)

	entry := buildSceneChangeEntry("Home", "Draft")
	err := svc.handleEntry(context.Background(), entry)
	require.NoError(t, err)

	// draft.started is a boundary event after the fix — FlushNow is called
	// immediately. Wait for async dispatch.
	time.Sleep(150 * time.Millisecond)

	captured := cap.get()
	require.Equal(t, "draft.started", captured.Type, "event type must be draft.started")

	var p draftSessionEventPayload
	require.NoError(t, json.Unmarshal(captured.Payload, &p))
	assert.Equal(t, wantSessionID, p.SessionID, "draft.started payload must carry the active draftstate session_id")
	assert.Equal(t, "EOE", p.SetCode, "draft.started payload must carry set_code derived from CourseName")
	// DraftType is the raw MTGA format prefix from draftstate.Session.Format
	// (e.g. "QuickDraft"), not the BFF-normalized snake_case form. The BFF's
	// deriveDraftFormatType derives format_type from event_name independently.
	assert.Equal(t, "QuickDraft", p.DraftType, "draft.started payload must carry draft_type (raw MTGA format prefix)")
	assert.Equal(t, "QuickDraft_EOE", p.EventName, "draft.started payload must carry event_name (CourseName)")
}

// TestDraftCompleted_Enriched_EmitsSessionID verifies that when a draft.completed
// scene-change entry fires and a draftstate session is active, the dispatched
// payload carries session_id (and status will be set to "completed" by the BFF
// projection worker).
//
// RED: before the fix, draft.completed falls through to entry.JSON which has no
// session_id, so projectDraftSession permanently rejects every draft.completed
// and NO draft session ever transitions to status=completed in production.
func TestDraftCompleted_Enriched_EmitsSessionID(t *testing.T) {
	fixedNow := time.Now().UTC().Add(-30 * time.Minute)
	store, wantSessionID := seedDraftstate(t, "PremierDraft_BLB", fixedNow)

	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestServiceWithStore(t, srv.URL, store)

	entry := buildSceneChangeEntry("Draft", "Home")
	err := svc.handleEntry(context.Background(), entry)
	require.NoError(t, err)

	// draft.completed is a boundary event after the fix — FlushNow fires.
	time.Sleep(150 * time.Millisecond)

	captured := cap.get()
	require.Equal(t, "draft.completed", captured.Type, "event type must be draft.completed")

	var p draftSessionEventPayload
	require.NoError(t, json.Unmarshal(captured.Payload, &p))
	assert.Equal(t, wantSessionID, p.SessionID, "draft.completed payload must carry the active draftstate session_id")
	assert.Equal(t, "BLB", p.SetCode, "draft.completed payload must carry set_code")
	assert.Equal(t, "PremierDraft", p.DraftType, "draft.completed payload must carry draft_type (raw MTGA format prefix)")
}

// TestDraftStarted_NoSession_FallsThrough verifies that when no draftstate session
// is active (e.g. daemon restarted before any pack event), draft.started is still
// dispatched — payload will lack session_id (soft-graceful, not dropped).
// projectDraftSession will permanent-reject it (existing behavior), but the event
// reaches the BFF rather than being silently dropped in the daemon.
func TestDraftStarted_NoSession_FallsThrough(t *testing.T) {
	store := draftstate.New() // empty — no sessions seeded

	var cap eventCapture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		cap.capture(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestServiceWithStore(t, srv.URL, store)

	entry := buildSceneChangeEntry("Home", "Draft")
	err := svc.handleEntry(context.Background(), entry)
	require.NoError(t, err)

	time.Sleep(150 * time.Millisecond)

	captured := cap.get()
	// The event still fires; the payload just lacks a session_id.
	assert.Equal(t, "draft.started", captured.Type, "draft.started must be dispatched even with no active session")
}

// TestDraftCompleted_FlushNow_FiresImmediately verifies that draft.completed
// triggers an immediate batch flush (not waiting for the 750 ms interval).
// Strategy: the server-side receives the request within 300 ms of handleEntry;
// that window is too narrow to be met by the background 750 ms timer alone.
func TestDraftCompleted_FlushNow_FiresImmediately(t *testing.T) {
	fixedNow := time.Now().UTC().Add(-10 * time.Minute)
	store, _ := seedDraftstate(t, "QuickDraft_SOS_20260612", fixedNow)

	received := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestServiceWithStore(t, srv.URL, store)

	entry := buildSceneChangeEntry("Draft", "Home")
	err := svc.handleEntry(context.Background(), entry)
	require.NoError(t, err)

	select {
	case <-received:
		// Good — FlushNow fired and the BFF received the batch promptly.
	case <-time.After(300 * time.Millisecond):
		t.Fatal("draft.completed did not flush promptly — FlushNow must be called for this event type")
	}
}

// TestDraftStarted_FlushNow_FiresImmediately verifies the same for draft.started.
func TestDraftStarted_FlushNow_FiresImmediately(t *testing.T) {
	fixedNow := time.Now().UTC().Add(-2 * time.Minute)
	store, _ := seedDraftstate(t, "QuickDraft_SOS_20260612", fixedNow)

	received := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := newTestServiceWithStore(t, srv.URL, store)

	entry := buildSceneChangeEntry("Home", "Draft")
	err := svc.handleEntry(context.Background(), entry)
	require.NoError(t, err)

	select {
	case <-received:
		// Good.
	case <-time.After(300 * time.Millisecond):
		t.Fatal("draft.started did not flush promptly — FlushNow must be called for this event type")
	}
}
