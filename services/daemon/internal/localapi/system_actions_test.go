// Tests for POST /api/v1/system/sync-now and POST /api/v1/system/grant-access.
//
// Coverage:
//   - 202 Accepted on a registered trigger (happy path)
//   - 409 Conflict when the action is already in flight (concurrent guard)
//   - 503 Service Unavailable when no trigger has been registered
//   - 405 Method Not Allowed for non-POST requests
//   - Lifecycle-context: the goroutine uses the server lifecycle ctx, not the
//     request ctx, so a fire-and-forget 202 does NOT cancel when the response
//     is sent.
//
// Ray's implementation notes:
//   - `defer atomicBool.Store(false)` runs in the WORK goroutine, not the
//     handler — tests verify the in-flight flag resets after the goroutine
//     completes, not after the 202 is sent.
//   - The 409 body must match the flat shape used in replay.go:65:
//     {"error":"sync already in progress"} / {"error":"grant-access already in progress"}.

package localapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/localapi"
)

// startTestServerWithContext starts a test localapi.Server with the given
// lifecycle context (or context.Background() when nil) and registers
// t.Cleanup to stop it.
func startTestServerWithContext(t *testing.T, ctx context.Context, state *localapi.State) *localapi.Server {
	t.Helper()
	if ctx == nil {
		ctx = context.Background()
	}
	var s localapi.State
	if state != nil {
		s = *state
	}
	srv := localapi.New(0, s)
	srv.WithContext(ctx)
	if err := srv.Start(); err != nil {
		t.Fatalf("startTestServerWithContext: Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv
}

// ────────────────────────────────────────────────────────────────────────────
// POST /api/v1/system/sync-now
// ────────────────────────────────────────────────────────────────────────────

// TestSyncNow_NoTrigger_Returns503 verifies that when no SyncNowFunc has been
// registered, the endpoint returns 503 with an error body.
func TestSyncNow_NoTrigger_Returns503(t *testing.T) {
	srv := startTestServer(t, nil) // no SetSyncNowTrigger call

	resp := postJSON(t, srv, "/api/v1/system/sync-now", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", resp.StatusCode)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error == "" {
		t.Error("expected non-empty error field in 503 response")
	}
}

// TestSyncNow_HappyPath_Returns202 verifies that a registered SyncNowFunc
// results in 202 Accepted and the trigger is called asynchronously.
func TestSyncNow_HappyPath_Returns202(t *testing.T) {
	var triggered atomic.Bool
	done := make(chan struct{})

	srv := startTestServer(t, nil)
	srv.SetSyncNowTrigger(func(ctx context.Context) {
		triggered.Store(true)
		close(done)
	})

	resp := postJSON(t, srv, "/api/v1/system/sync-now", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want 202", resp.StatusCode)
	}

	// The goroutine must be called within a short window.
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("sync-now trigger was not called within 200ms")
	}
	if !triggered.Load() {
		t.Error("SyncNowFunc was not called")
	}
}

// TestSyncNow_ConcurrentCall_Returns409 verifies that a second POST while the
// first goroutine is still running returns 409 Conflict with the expected flat
// error body {"error":"sync already in progress"}.
func TestSyncNow_ConcurrentCall_Returns409(t *testing.T) {
	// Block the first goroutine so the second call arrives while it is in-flight.
	block := make(chan struct{})
	done := make(chan struct{})

	srv := startTestServer(t, nil)
	srv.SetSyncNowTrigger(func(ctx context.Context) {
		<-block // hold until released
		close(done)
	})

	// First call — returns 202 and goroutine blocks.
	resp1 := postJSON(t, srv, "/api/v1/system/sync-now", nil)
	defer func() { _ = resp1.Body.Close() }()
	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first call: got %d, want 202", resp1.StatusCode)
	}

	// Give the goroutine a moment to set the in-flight flag.
	time.Sleep(10 * time.Millisecond)

	// Second call — should see the in-flight flag and return 409.
	resp2 := postJSON(t, srv, "/api/v1/system/sync-now", nil)
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusConflict {
		t.Errorf("concurrent call: got %d, want 409", resp2.StatusCode)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&body); err != nil {
		t.Fatalf("decode 409 body: %v", err)
	}
	if body.Error != "sync already in progress" {
		t.Errorf("error field: got %q, want %q", body.Error, "sync already in progress")
	}

	// Release the first goroutine so the test cleanup does not leak.
	close(block)
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("first goroutine did not finish after release")
	}
}

// TestSyncNow_InFlightFlagResetAfterGoroutine verifies that the atomic in-flight
// flag is reset inside the work goroutine (defer), not inside the handler — so a
// subsequent call AFTER the first goroutine finishes succeeds with 202, not 409.
func TestSyncNow_InFlightFlagResetAfterGoroutine(t *testing.T) {
	done1 := make(chan struct{})

	srv := startTestServer(t, nil)
	var calls atomic.Int32
	srv.SetSyncNowTrigger(func(ctx context.Context) {
		calls.Add(1)
		if calls.Load() == 1 {
			close(done1)
		}
	})

	// First call — fire and wait for goroutine to complete.
	resp1 := postJSON(t, srv, "/api/v1/system/sync-now", nil)
	defer func() { _ = resp1.Body.Close() }()
	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first call: got %d, want 202", resp1.StatusCode)
	}
	select {
	case <-done1:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("first goroutine did not complete")
	}
	// Wait a tick to let the defer run.
	time.Sleep(5 * time.Millisecond)

	// Second call after the goroutine has fully completed — must be 202.
	resp2 := postJSON(t, srv, "/api/v1/system/sync-now", nil)
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusAccepted {
		t.Errorf("second call after goroutine done: got %d, want 202", resp2.StatusCode)
	}
}

// TestSyncNow_MethodNotAllowed_Returns405 verifies that GET requests to the
// sync-now endpoint return 405.
func TestSyncNow_MethodNotAllowed_Returns405(t *testing.T) {
	srv := startTestServer(t, nil)

	resp, err := http.Get("http://" + srv.Addr() + "/api/v1/system/sync-now")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want 405", resp.StatusCode)
	}
}

// TestSyncNow_LifecycleContext verifies that the fire-and-forget goroutine uses
// the server lifecycle context (passed via WithContext) and NOT the request
// context. The goroutine must see a live context even after the HTTP response has
// been returned (the request context would be cancelled at that point).
func TestSyncNow_LifecycleContext(t *testing.T) {
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	defer lifecycleCancel()

	srv := startTestServerWithContext(t, lifecycleCtx, nil)

	// The channel receives a non-nil ctx.Err() if the goroutine sees a
	// cancelled context when it begins running; nil means context is live.
	ctxErrCh := make(chan error, 1)
	srv.SetSyncNowTrigger(func(ctx context.Context) {
		// Briefly yield so the HTTP response has time to be sent and the
		// request context to be cancelled before we sample ctx.Err().
		time.Sleep(20 * time.Millisecond)
		ctxErrCh <- ctx.Err()
	})

	resp := postJSON(t, srv, "/api/v1/system/sync-now", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want 202", resp.StatusCode)
	}

	select {
	case err := <-ctxErrCh:
		if err != nil {
			t.Errorf("goroutine received a cancelled context: %v — must use lifecycle ctx, not request ctx", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Error("goroutine did not call the trigger within 300ms")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// POST /api/v1/system/grant-access
// ────────────────────────────────────────────────────────────────────────────

// TestGrantAccess_NoTrigger_Returns503 verifies 503 when no GrantAccessFunc is
// registered.
func TestGrantAccess_NoTrigger_Returns503(t *testing.T) {
	srv := startTestServer(t, nil) // no SetGrantAccessTrigger call

	resp := postJSON(t, srv, "/api/v1/system/grant-access", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: got %d, want 503", resp.StatusCode)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error == "" {
		t.Error("expected non-empty error field in 503 response")
	}
}

// TestGrantAccess_HappyPath_Returns202 verifies 202 Accepted on all platforms —
// the API is OS-agnostic (authorizeCollectionHelper short-circuits on non-darwin
// internally; the handler always returns 202 when a trigger is registered).
func TestGrantAccess_HappyPath_Returns202(t *testing.T) {
	var triggered atomic.Bool
	done := make(chan struct{})

	srv := startTestServer(t, nil)
	srv.SetGrantAccessTrigger(func() {
		triggered.Store(true)
		close(done)
	})

	resp := postJSON(t, srv, "/api/v1/system/grant-access", nil)
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want 202", resp.StatusCode)
	}

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Error("grant-access trigger was not called within 200ms")
	}
	if !triggered.Load() {
		t.Error("GrantAccessFunc was not called")
	}
}

// TestGrantAccess_ConcurrentCall_Returns409 verifies 409 Conflict with the flat
// body {"error":"grant-access already in progress"} when the action is in-flight.
func TestGrantAccess_ConcurrentCall_Returns409(t *testing.T) {
	block := make(chan struct{})
	done := make(chan struct{})

	srv := startTestServer(t, nil)
	srv.SetGrantAccessTrigger(func() {
		<-block
		close(done)
	})

	resp1 := postJSON(t, srv, "/api/v1/system/grant-access", nil)
	defer func() { _ = resp1.Body.Close() }()
	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first call: got %d, want 202", resp1.StatusCode)
	}

	time.Sleep(10 * time.Millisecond)

	resp2 := postJSON(t, srv, "/api/v1/system/grant-access", nil)
	defer func() { _ = resp2.Body.Close() }()

	if resp2.StatusCode != http.StatusConflict {
		t.Errorf("concurrent call: got %d, want 409", resp2.StatusCode)
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&body); err != nil {
		t.Fatalf("decode 409 body: %v", err)
	}
	if body.Error != "grant-access already in progress" {
		t.Errorf("error field: got %q, want %q", body.Error, "grant-access already in progress")
	}

	close(block)
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Error("first goroutine did not finish after release")
	}
}

// TestGrantAccess_InFlightFlagResetAfterGoroutine verifies the in-flight flag
// resets in the goroutine so a subsequent call after completion returns 202.
func TestGrantAccess_InFlightFlagResetAfterGoroutine(t *testing.T) {
	done1 := make(chan struct{})

	srv := startTestServer(t, nil)
	var calls atomic.Int32
	srv.SetGrantAccessTrigger(func() {
		calls.Add(1)
		if calls.Load() == 1 {
			close(done1)
		}
	})

	resp1 := postJSON(t, srv, "/api/v1/system/grant-access", nil)
	defer func() { _ = resp1.Body.Close() }()
	if resp1.StatusCode != http.StatusAccepted {
		t.Fatalf("first call: got %d, want 202", resp1.StatusCode)
	}
	select {
	case <-done1:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("first goroutine did not complete")
	}
	time.Sleep(5 * time.Millisecond)

	resp2 := postJSON(t, srv, "/api/v1/system/grant-access", nil)
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusAccepted {
		t.Errorf("second call after goroutine done: got %d, want 202", resp2.StatusCode)
	}
}

// TestGrantAccess_MethodNotAllowed_Returns405 verifies 405 for GET.
func TestGrantAccess_MethodNotAllowed_Returns405(t *testing.T) {
	srv := startTestServer(t, nil)

	resp, err := http.Get("http://" + srv.Addr() + "/api/v1/system/grant-access")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status: got %d, want 405", resp.StatusCode)
	}
}

// TestGrantAccess_LifecycleContext verifies the goroutine uses the server
// lifecycle context, not the request context, so a 202 response does not cancel
// the background work.
//
// NOTE: GrantAccessFunc takes no ctx (authorizeCollectionHelper signature).
// The lifecycle-context requirement applies to sync-now. For grant-access,
// the test verifies that the goroutine is launched and runs independently of
// the request lifecycle (no premature cancellation on the http response side).
// Since GrantAccessFunc takes no ctx, we verify the goroutine runs to completion
// after the response is flushed.
func TestGrantAccess_RunsAfterResponseFlushed(t *testing.T) {
	done := make(chan struct{})

	srv := startTestServer(t, nil)
	srv.SetGrantAccessTrigger(func() {
		// Delay ensures this runs after the HTTP response is sent.
		time.Sleep(20 * time.Millisecond)
		close(done)
	})

	resp := postJSON(t, srv, "/api/v1/system/grant-access", nil)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status: got %d, want 202", resp.StatusCode)
	}

	// Response has been received; goroutine should still complete.
	select {
	case <-done:
		// goroutine ran to completion after response was sent — correct.
	case <-time.After(300 * time.Millisecond):
		t.Error("goroutine did not complete within 300ms after 202 response")
	}
}
