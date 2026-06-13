package projection

// worker_draft_enrichment_test.go — BFF projection tests for #1344 PR-B (Defect 2b).
//
// These tests document and cover the class defect:
//   - draft.completed payload WITHOUT session_id → projectDraftSession returns a
//     permanent error → row is marked projected but NO draft_sessions row is ever
//     set to status=completed. Every draft session stays in_progress forever.
//
// After PR-B the daemon enriches both draft.started and draft.completed with
// session_id (and set_code / draft_type) keyed from the draftstate synthetic ID,
// so the BFF projection worker can successfully call UpsertDraftSession with
// status=completed.
//
// The BFF side (projectDraftSession) does NOT change in PR-B — the enrichment is
// entirely on the daemon side. These tests serve as:
//   1. Regression guards confirming the existing permanent-reject guard still fires
//      for no-session_id payloads (if the daemon does not enrich).
//   2. Forward confirmation that enriched payloads (with session_id) DO reach
//      UpsertDraftSession and produce status=completed.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// TestProjectDraftSession_MissingSessionID_PermanentReject verifies that when
// draft.started or draft.completed arrives with no session_id in the payload
// (the pre-PR-B daemon behaviour), projectDraftSession returns a permanent error
// and no draft_sessions row is written.
//
// This is the regression guard for the root cause of Defect 2b: the daemon was
// dispatching scene-change entries as raw entry.JSON (no session_id), so every
// draft.completed was permanently rejected here and no session ever closed.
func TestProjectDraftSession_MissingSessionID_PermanentReject(t *testing.T) {
	for _, evtType := range []string{"draft.started", "draft.completed"} {
		t.Run(evtType, func(t *testing.T) {
			// Raw scene-change JSON — no session_id, just fromSceneName/toSceneName.
			rawScenePayload := json.RawMessage(`{"fromSceneName":"Home","toSceneName":"Draft"}`)

			events := &fakeEventStore{
				pending: []repository.DaemonEventRow{
					{ID: 900, UserID: 1, AccountID: "acct-no-enrich", EventType: evtType, Payload: rawScenePayload, OccurredAt: time.Now()},
				},
			}
			accounts := &fakeAccountStore{accountID: 60}
			drafts := &fakeDraftStore{}

			w := newWorker(events, accounts, &fakeMatchStore{}, drafts)
			w.RunOnce(context.Background())

			// Row MUST be marked projected (permanent error, not retried).
			if len(events.projected) != 1 || events.projected[0] != 900 {
				t.Errorf("permanent-reject must mark row projected; got %v", events.projected)
			}
			// No draft_sessions row must be written.
			if len(drafts.upserts) != 0 {
				t.Errorf("permanent-reject must not upsert draft session, got %d upserts", len(drafts.upserts))
			}
		})
	}
}

// TestProjectDraftCompleted_EnrichedPayload_SetsStatusCompleted is the forward
// confirmation: when the daemon enriches draft.completed with session_id (the
// PR-B fix), projectDraftSession successfully calls UpsertDraftSession with
// status=completed — closing the draft session in the database.
//
// RED: before PR-B this test cannot be reached in production because the daemon
// never enriches the payload. Once the daemon fix lands, this test confirms the
// full round-trip closes sessions.
func TestProjectDraftCompleted_EnrichedPayload_SetsStatusCompleted(t *testing.T) {
	// Enriched payload — exactly what the daemon emits after PR-B.
	// draft_type is the raw MTGA format prefix from draftstate.Session.Format;
	// the BFF derives format_type from event_name via deriveDraftFormatType.
	payload := makePayload(t, map[string]interface{}{
		"session_id": "QuickDraft_EOE_20260612:2026-06-12T10:00:00Z",
		"event_name": "QuickDraft_EOE_20260612",
		"set_code":   "EOE",
		"draft_type": "QuickDraft",
		// status is intentionally omitted — projectDraftSession fills it from EventType.
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 901, UserID: 1, AccountID: "acct-enrich-done", EventType: "draft.completed", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 61}
	drafts := &fakeDraftStore{winsForSession: 3} // 3 wins — not a trophy

	w := newWorker(events, accounts, &fakeMatchStore{}, drafts)
	w.RunOnce(context.Background())

	if len(events.projected) != 1 || events.projected[0] != 901 {
		t.Errorf("expected row 901 marked projected, got %v", events.projected)
	}
	if len(drafts.upserts) != 1 {
		t.Fatalf("expected 1 draft session upsert, got %d", len(drafts.upserts))
	}
	u := drafts.upserts[0]
	if u.ID != "QuickDraft_EOE_20260612:2026-06-12T10:00:00Z" {
		t.Errorf("upsert ID: want %q, got %q", "QuickDraft_EOE_20260612:2026-06-12T10:00:00Z", u.ID)
	}
	if u.Status != "completed" {
		t.Errorf("upsert Status: want %q, got %q (draft session must close)", "completed", u.Status)
	}
	if u.EndTime == nil {
		t.Error("upsert EndTime: must be set for draft.completed")
	}
}

// TestProjectDraftStarted_EnrichedPayload_SetsStatusInProgress is the forward
// confirmation for draft.started: an enriched payload with session_id must reach
// UpsertDraftSession with status=in_progress.
func TestProjectDraftStarted_EnrichedPayload_SetsStatusInProgress(t *testing.T) {
	payload := makePayload(t, map[string]interface{}{
		"session_id": "PremierDraft_BLB:2026-06-12T09:00:00Z",
		"event_name": "PremierDraft_BLB",
		"set_code":   "BLB",
		"draft_type": "PremierDraft", // raw MTGA format prefix; BFF derives format_type from event_name
		// No status field — projectDraftSession defaults to "in_progress" for draft.started.
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 902, UserID: 1, AccountID: "acct-enrich-start", EventType: "draft.started", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 62}
	drafts := &fakeDraftStore{}

	w := newWorker(events, accounts, &fakeMatchStore{}, drafts)
	w.RunOnce(context.Background())

	if len(events.projected) != 1 || events.projected[0] != 902 {
		t.Errorf("expected row 902 marked projected, got %v", events.projected)
	}
	if len(drafts.upserts) != 1 {
		t.Fatalf("expected 1 draft session upsert, got %d", len(drafts.upserts))
	}
	u := drafts.upserts[0]
	if u.ID != "PremierDraft_BLB:2026-06-12T09:00:00Z" {
		t.Errorf("upsert ID: want %q, got %q", "PremierDraft_BLB:2026-06-12T09:00:00Z", u.ID)
	}
	if u.Status != "in_progress" {
		t.Errorf("upsert Status: want %q, got %q", "in_progress", u.Status)
	}
	if u.SetCode != "BLB" {
		t.Errorf("upsert SetCode: want %q, got %q", "BLB", u.SetCode)
	}
}
