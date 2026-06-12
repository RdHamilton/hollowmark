package handlers_test

// Tests for ingest-side account_id mismatch detection (#1336).
//
// When event.AccountID != the key-bound account_id set on context by
// DaemonAPIKeyAuth, the handler must:
//   - AC1: emit a WARN log with hashed IDs (not raw Clerk IDs per ADR-056)
//   - AC2: capture a daemon_account_id_mismatch analytics event
//   - AC3: NOT reject the event — projection continues unchanged (observability only)
//
// When the IDs match, no mismatch event must be emitted.
// When key_bound_account_id is absent (legacy APIKeyAuth path), no false
// positive must fire.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/RdHamilton/hollowmark/services/bff/internal/analytics"
	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	"github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	contract "github.com/RdHamilton/hollowmark/services/contract"
	"github.com/posthog/posthog-go"
)

// ─── DaemonAPIKeyAuth test doubles ───────────────────────────────────────────
// These implement the unexported middleware interfaces via duck-typing.
// The middleware-package variants live in middleware_test and cannot be imported.

type stubDaemonKeyRepoForMismatch struct {
	rows []repository.DaemonAPIKey
}

func (s *stubDaemonKeyRepoForMismatch) GetByPrefix(_ context.Context, _ string) ([]repository.DaemonAPIKey, error) {
	return s.rows, nil
}

func (s *stubDaemonKeyRepoForMismatch) UpdateLastUsed(_ context.Context, _ string) error {
	return nil
}

type stubDaemonUserRepoForMismatch struct {
	user *repository.User
}

func (s *stubDaemonUserRepoForMismatch) GetByClerkUserID(_ context.Context, _ string) (*repository.User, error) {
	return s.user, nil
}

// buildMismatchHandler wraps ih with DaemonAPIKeyAuth using the provided
// plaintext token (prefix = first 16 bytes), the Clerk account_id bound to
// that key, and the users.id to resolve to.
func buildMismatchHandler(
	t *testing.T,
	ih *handlers.IngestHandler,
	plaintext string,
	keyBoundAccountID string,
	resolvedUserID int64,
) http.Handler {
	t.Helper()

	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}
	prefix := plaintext
	if len(prefix) > 16 {
		prefix = prefix[:16]
	}
	keyRepo := &stubDaemonKeyRepoForMismatch{
		rows: []repository.DaemonAPIKey{
			{
				ID:        "key-mm-1",
				AccountID: keyBoundAccountID,
				KeyHash:   string(hash),
				KeyPrefix: prefix,
			},
		},
	}
	userRepo := &stubDaemonUserRepoForMismatch{user: &repository.User{ID: resolvedUserID}}
	return middleware.DaemonAPIKeyAuth(keyRepo, userRepo)(http.HandlerFunc(ih.IngestEvent))
}

// ─── tests ────────────────────────────────────────────────────────────────────

// TestIngestHandler_AccountIDMismatch_EmitsAnalytics verifies AC1-AC3:
//   - a daemon_account_id_mismatch analytics capture is emitted (AC2)
//   - distinct_id and properties use hashed IDs, not raw values (AC1)
//   - the event is still accepted and broadcast, not rejected (AC3)
func TestIngestHandler_AccountIDMismatch_EmitsAnalytics(t *testing.T) {
	const plaintext = "1234567890abcdef_mismatch_key_"
	const keyBoundID = "user_clerk_BOUND"
	const eventAccountID = "user_clerk_STALE" // deliberate mismatch

	phClient := &mockPostHogClient{}
	broadcaster := &mockBroadcaster{}
	ih := handlers.NewIngestHandler(broadcaster).WithPostHogClient(phClient)
	handler := buildMismatchHandler(t, ih, plaintext, keyBoundID, 42)

	payload, _ := json.Marshal(map[string]string{"k": "v"})
	event := contract.DaemonEvent{
		Type:       "match.completed",
		AccountID:  eventAccountID, // does NOT match keyBoundID
		SessionID:  "sess_mismatch",
		Sequence:   1,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	}
	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// AC3: mismatch must NOT cause a rejection.
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 (mismatch is observability-only, must not reject), got %d: %s",
			rr.Code, rr.Body.String())
	}

	// AC2: daemon_account_id_mismatch must be captured exactly once.
	var mismatchCapture *posthog.Capture
	for i := range phClient.calls {
		c, ok := phClient.calls[i].(posthog.Capture)
		if ok && c.Event == analytics.EventDaemonAccountIdMismatch {
			cp := c
			mismatchCapture = &cp
			break
		}
	}
	if mismatchCapture == nil {
		t.Fatalf("expected %q PostHog capture, got %d calls: %v",
			analytics.EventDaemonAccountIdMismatch, len(phClient.calls), phClient.calls)
	}

	// AC1: PII — distinct_id must be hashed, never raw.
	if mismatchCapture.DistinctId == keyBoundID || mismatchCapture.DistinctId == eventAccountID {
		t.Errorf("distinct_id must be hashed, got raw %q", mismatchCapture.DistinctId)
	}
	if len(mismatchCapture.DistinctId) != 16 {
		t.Errorf("distinct_id must be 16-char hash, got len=%d: %q",
			len(mismatchCapture.DistinctId), mismatchCapture.DistinctId)
	}

	// AC1: properties must carry hashes, not raw IDs.
	if v, ok := mismatchCapture.Properties["event_account_id_hash"]; !ok {
		t.Error("event_account_id_hash property missing from mismatch capture")
	} else if v == eventAccountID {
		t.Errorf("event_account_id_hash must be hashed, got raw %q", eventAccountID)
	}
	if v, ok := mismatchCapture.Properties["key_bound_account_id_hash"]; !ok {
		t.Error("key_bound_account_id_hash property missing from mismatch capture")
	} else if v == keyBoundID {
		t.Errorf("key_bound_account_id_hash must be hashed, got raw %q", keyBoundID)
	}

	// AC3: SSE broadcast still occurred (event was not dropped).
	if len(broadcaster.calls) == 0 {
		t.Error("event must still be broadcast even on account_id mismatch (observability only, not a gate)")
	}
}

// TestIngestHandler_AccountIDMatch_NoMismatchCapture verifies that matching IDs
// produce no mismatch capture — the expected steady-state once daemons have been
// updated per ADR-080.
func TestIngestHandler_AccountIDMatch_NoMismatchCapture(t *testing.T) {
	const plaintext = "1234567890abcdef_match_key_pad"
	const sharedAccountID = "user_clerk_SAME"

	phClient := &mockPostHogClient{}
	broadcaster := &mockBroadcaster{}
	ih := handlers.NewIngestHandler(broadcaster).WithPostHogClient(phClient)
	handler := buildMismatchHandler(t, ih, plaintext, sharedAccountID, 77)

	payload, _ := json.Marshal(map[string]string{"k": "v"})
	event := contract.DaemonEvent{
		Type:       "draft.pick",
		AccountID:  sharedAccountID, // matches key-bound ID — no mismatch
		SessionID:  "sess_match",
		Sequence:   1,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	}
	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
	for _, msg := range phClient.calls {
		if c, ok := msg.(posthog.Capture); ok && c.Event == analytics.EventDaemonAccountIdMismatch {
			t.Errorf("%q must NOT be emitted when account IDs match", analytics.EventDaemonAccountIdMismatch)
		}
	}
}

// TestIngestHandler_AccountIDMismatch_BatchPath_EmitsPerEvent verifies the
// mismatch detection fires once per mismatched event on the batch ingest path.
func TestIngestHandler_AccountIDMismatch_BatchPath_EmitsPerEvent(t *testing.T) {
	const plaintext = "1234567890abcdef_batch_mismtch"
	const keyBoundID = "user_clerk_BOUND_BATCH"
	const eventAccountID = "user_clerk_STALE_BATCH"

	phClient := &mockPostHogClient{}
	broadcaster := &mockBroadcaster{}
	ih := handlers.NewIngestHandler(broadcaster).WithPostHogClient(phClient)
	handler := buildMismatchHandler(t, ih, plaintext, keyBoundID, 55)

	payload, _ := json.Marshal(map[string]string{"k": "v"})
	events := []contract.DaemonEvent{
		{Type: "match.completed", AccountID: eventAccountID, SessionID: "s1", Sequence: 1, OccurredAt: time.Now().UTC(), Payload: payload},
		{Type: "draft.pick", AccountID: eventAccountID, SessionID: "s1", Sequence: 2, OccurredAt: time.Now().UTC(), Payload: payload},
	}
	body, _ := json.Marshal(events)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+plaintext)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	var mismatchCount int
	for _, msg := range phClient.calls {
		if c, ok := msg.(posthog.Capture); ok && c.Event == analytics.EventDaemonAccountIdMismatch {
			mismatchCount++
		}
	}
	if mismatchCount != 2 {
		t.Errorf("expected 2 mismatch captures for 2 mismatched events in batch, got %d", mismatchCount)
	}
}

// TestIngestHandler_AccountIDMismatch_LegacyMiddleware_NoMismatch verifies that
// when the legacy APIKeyAuth middleware is used (no key_bound_account_id set on
// context), no mismatch warning fires.  Prevents false positives during the
// transition window.
func TestIngestHandler_AccountIDMismatch_LegacyMiddleware_NoMismatch(t *testing.T) {
	const token = "legacy-token-no-bound-id-mm"

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 1, KeyHash: mustHash(t, token), UserID: 42},
	}}

	phClient := &mockPostHogClient{}
	ih := handlers.NewIngestHandler(&mockBroadcaster{}).WithPostHogClient(phClient)
	// Deliberately use old APIKeyAuth (no key_bound_account_id on context).
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	payload, _ := json.Marshal(map[string]string{"k": "v"})
	event := contract.DaemonEvent{
		Type:       "match.completed",
		AccountID:  "any_account_id",
		SessionID:  "sess_legacy",
		Sequence:   1,
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	}
	body, _ := json.Marshal(event)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
	for _, msg := range phClient.calls {
		if c, ok := msg.(posthog.Capture); ok && c.Event == analytics.EventDaemonAccountIdMismatch {
			t.Error("daemon_account_id_mismatch must NOT fire when key_bound_account_id is absent (legacy APIKeyAuth path)")
		}
	}
}
