package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/bff/internal/api/handlers"
	"github.com/RdHamilton/vault-mtg/services/bff/internal/api/middleware"
	"github.com/RdHamilton/vault-mtg/services/bff/internal/storage/repository"
	contract "github.com/RdHamilton/vault-mtg/services/contract"
)

// batchRequest builds a multipart-free request with either a JSON array body
// (batch) or leaves body construction to the caller.
func batchRequest(t *testing.T, token string, body []byte) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, httptest.NewRecorder()
}

func makeBatchBody(t *testing.T, events []contract.DaemonEvent) []byte {
	t.Helper()
	b, err := json.Marshal(events)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}
	return b
}

// TestIngestBatch_TwoEvents_BroadcastsBoth verifies that a JSON-array body with
// two events broadcasts both to the EventBroadcaster (ADR-053 §5 batch path).
func TestIngestBatch_TwoEvents_BroadcastsBoth(t *testing.T) {
	const token = "batch-two-token"
	const wantUserID int64 = 500

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 100, KeyHash: mustHash(t, token), UserID: wantUserID},
	}}
	broadcaster := &mockBroadcaster{}
	ih := handlers.NewIngestHandler(broadcaster)
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	events := []contract.DaemonEvent{
		makeEvent("match.completed"),
		makeEvent("draft.pick"),
	}
	body := makeBatchBody(t, events)

	req, rr := batchRequest(t, token, body)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}

	if len(broadcaster.calls) != 2 {
		t.Fatalf("expected 2 broadcast calls for 2-event batch, got %d", len(broadcaster.calls))
	}

	gotTypes := []string{broadcaster.calls[0].event.Type, broadcaster.calls[1].event.Type}
	wantTypes := []string{"match.completed", "draft.pick"}
	for i, gt := range gotTypes {
		if gt != wantTypes[i] {
			t.Errorf("event[%d] type=%q, want %q", i, gt, wantTypes[i])
		}
	}
}

// TestIngestBatch_AllEventsCarryAuthenticatedUserID verifies that every event
// in a batch is broadcast with the authenticated userID from the API key, not
// any caller-supplied value (security property).
func TestIngestBatch_AllEventsCarryAuthenticatedUserID(t *testing.T) {
	const token = "batch-uid-token"
	const wantUserID int64 = 501

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 101, KeyHash: mustHash(t, token), UserID: wantUserID},
	}}
	broadcaster := &mockBroadcaster{}
	ih := handlers.NewIngestHandler(broadcaster)
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	events := []contract.DaemonEvent{makeEvent("a"), makeEvent("b"), makeEvent("c")}
	body := makeBatchBody(t, events)

	req, rr := batchRequest(t, token, body)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
	if len(broadcaster.calls) != 3 {
		t.Fatalf("expected 3 broadcast calls, got %d", len(broadcaster.calls))
	}
	for i, bc := range broadcaster.calls {
		if bc.userID != wantUserID {
			t.Errorf("event[%d]: broadcast userID=%d, want %d", i, bc.userID, wantUserID)
		}
	}
}

// TestIngestBatch_PersistsAllEvents verifies that each event in a batch is
// inserted into the repository with the correct userID.
func TestIngestBatch_PersistsAllEvents(t *testing.T) {
	const token = "batch-persist-token"
	const wantUserID int64 = 502

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 102, KeyHash: mustHash(t, token), UserID: wantUserID},
	}}
	eventsRepo := &mockDaemonEventsRepo{}
	broadcaster := &mockBroadcaster{}
	ih := handlers.NewIngestHandler(broadcaster).WithRepository(eventsRepo)
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	events := []contract.DaemonEvent{
		makeEvent("draft.pack"),
		makeEvent("match.completed"),
	}
	body := makeBatchBody(t, events)

	req, rr := batchRequest(t, token, body)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(eventsRepo.calls) != 2 {
		t.Fatalf("expected 2 Insert calls for 2-event batch, got %d", len(eventsRepo.calls))
	}
	for i, c := range eventsRepo.calls {
		if c.userID != wantUserID {
			t.Errorf("Insert[%d] userID=%d, want %d", i, c.userID, wantUserID)
		}
	}
}

// TestIngestBatch_OverCap_Returns413 verifies that a batch with more than
// maxBatchSize events is rejected with 413 (defence against malformed/hostile
// daemon per ADR-053 §5).
func TestIngestBatch_OverCap_Returns413(t *testing.T) {
	const token = "batch-cap-token"

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 103, KeyHash: mustHash(t, token), UserID: 503},
	}}
	ih := handlers.NewIngestHandler(&mockBroadcaster{})
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	// 101 events — one over the 100-event cap.
	events := make([]contract.DaemonEvent, 101)
	for i := range events {
		events[i] = makeEvent("test.event")
	}
	body := makeBatchBody(t, events)

	req, rr := batchRequest(t, token, body)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 for over-cap batch, got %d", rr.Code)
	}
}

// TestIngestBatch_ExactlyCap_Accepted verifies that a batch of exactly
// maxBatchSize (100) events is accepted.
func TestIngestBatch_ExactlyCap_Accepted(t *testing.T) {
	const token = "batch-exact-cap-token"

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 104, KeyHash: mustHash(t, token), UserID: 504},
	}}
	ih := handlers.NewIngestHandler(&mockBroadcaster{})
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	events := make([]contract.DaemonEvent, 100)
	for i := range events {
		events[i] = makeEvent("test.event")
	}
	body := makeBatchBody(t, events)

	req, rr := batchRequest(t, token, body)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202 for exactly-cap batch, got %d", rr.Code)
	}
}

// TestIngestBatch_SingleObjectBodyStillWorks verifies backward compatibility:
// a single-event JSON object body (existing daemon wire format) is still
// accepted without change on the same endpoint (ADR-053 §5 backward-compat).
func TestIngestBatch_SingleObjectBodyStillWorks(t *testing.T) {
	const token = "batch-compat-token"

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 105, KeyHash: mustHash(t, token), UserID: 505},
	}}
	broadcaster := &mockBroadcaster{}
	ih := handlers.NewIngestHandler(broadcaster)
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	event := makeEvent("match.completed")
	req, rr := ingestRequest(t, token, event)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for single-object body, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(broadcaster.calls) != 1 {
		t.Fatalf("expected 1 broadcast call for single-object body, got %d", len(broadcaster.calls))
	}
}

// TestIngestBatch_EmptyBatch_Returns400 verifies that an empty JSON array body
// is rejected with 400 (no events to ingest is a client error).
func TestIngestBatch_EmptyBatch_Returns400(t *testing.T) {
	const token = "batch-empty-token"

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 106, KeyHash: mustHash(t, token), UserID: 506},
	}}
	ih := handlers.NewIngestHandler(&mockBroadcaster{})
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	body := []byte(`[]`)
	req, rr := batchRequest(t, token, body)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty batch, got %d", rr.Code)
	}
}

// TestIngestBatch_SkipsMalformedEvents verifies that events in a batch that
// have an empty Type are accepted-and-skipped (ADR-039 accept-and-skip policy).
// The valid events in the same batch must still be processed.
func TestIngestBatch_SkipsMalformedEvents(t *testing.T) {
	const token = "batch-skip-token"

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 107, KeyHash: mustHash(t, token), UserID: 507},
	}}
	broadcaster := &mockBroadcaster{}
	ih := handlers.NewIngestHandler(broadcaster)
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	payload, _ := json.Marshal(map[string]string{})
	events := []contract.DaemonEvent{
		{Type: "valid.event", AccountID: "acc", OccurredAt: time.Now().UTC(), Payload: payload},
		{Type: "", AccountID: "acc", OccurredAt: time.Now().UTC(), Payload: payload}, // malformed — skip
		{Type: "another.valid", AccountID: "acc", OccurredAt: time.Now().UTC(), Payload: payload},
	}
	body := makeBatchBody(t, events)

	req, rr := batchRequest(t, token, body)
	handler.ServeHTTP(rr, req)

	// Handler must still accept (202) despite the malformed event.
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202 (accept-and-skip malformed), got %d: %s", rr.Code, rr.Body.String())
	}
	// Only the 2 valid events are broadcast; the malformed one is skipped.
	if len(broadcaster.calls) != 2 {
		t.Errorf("expected 2 broadcast calls (skipped malformed), got %d", len(broadcaster.calls))
	}
}

// TestIngestBatch_GapDetectionFiresAcrossBatch verifies that the gap detector
// runs for each event in a batch independently. A gap within a batch
// (seq 1 then seq 5) should fire the gap log just as it would for individual events.
func TestIngestBatch_GapDetectionFiresAcrossBatch(t *testing.T) {
	const token = "batch-gap-token"

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 108, KeyHash: mustHash(t, token), UserID: 508},
	}}
	phClient := &mockPostHogClient{}
	broadcaster := &mockBroadcaster{}
	ih := handlers.NewIngestHandler(broadcaster).WithPostHogClient(phClient)
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	payload := json.RawMessage(`{}`)
	events := []contract.DaemonEvent{
		{Type: "match.completed", AccountID: "acct_batch_gap", SessionID: "sess", Sequence: 1, OccurredAt: time.Now().UTC(), Payload: payload},
		{Type: "match.completed", AccountID: "acct_batch_gap", SessionID: "sess", Sequence: 5, OccurredAt: time.Now().UTC(), Payload: payload}, // gap
	}
	body := makeBatchBody(t, events)

	req, rr := batchRequest(t, token, body)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
	// The gap (seq 1 → 5) must trigger 1 PostHog capture.
	if len(phClient.calls) != 1 {
		t.Errorf("expected 1 PostHog gap capture from batch, got %d", len(phClient.calls))
	}
}

// TestIngestBatch_BodyTooLarge_Returns413 verifies that a body exceeding
// maxIngestBodyBytes is rejected with 413.
func TestIngestBatch_BodyTooLarge_Returns413(t *testing.T) {
	const token = "batch-large-token"

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 109, KeyHash: mustHash(t, token), UserID: 509},
	}}
	ih := handlers.NewIngestHandler(&mockBroadcaster{})
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	// Build a body larger than 1MB (~1.1MB of JSON).
	// A single large string field makes it trivially oversized.
	bigPayload := strings.Repeat("x", 1100*1024)
	body := []byte(`[{"type":"test.event","account_id":"acc","occurred_at":"2026-01-01T00:00:00Z","payload":{"x":"` + bigPayload + `"}}]`)

	req, rr := batchRequest(t, token, body)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 for over-size body, got %d", rr.Code)
	}
}

// TestIngestBatch_InvalidJSON_Returns400 verifies that an invalid JSON body
// returns 400 on the batch path.
func TestIngestBatch_InvalidJSON_Returns400(t *testing.T) {
	const token = "batch-badjson-token"

	keyRepo := &mockKeyLister{keys: []repository.APIKey{
		{ID: 110, KeyHash: mustHash(t, token), UserID: 510},
	}}
	ih := handlers.NewIngestHandler(&mockBroadcaster{})
	handler := middleware.APIKeyAuth(keyRepo)(http.HandlerFunc(ih.IngestEvent))

	body := []byte(`[{"type":}]`) // invalid JSON
	req, rr := batchRequest(t, token, body)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON batch, got %d", rr.Code)
	}
}
