package dispatch_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/contract"
	"github.com/RdHamilton/vault-mtg/services/daemon/internal/dispatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDispatcherSendsValidDaemonEvent verifies that the dispatcher POSTs a correctly
// structured contract.DaemonEvent to the BFF /v1/ingest/events endpoint.
func TestDispatcherSendsValidDaemonEvent(t *testing.T) {
	var received contract.DaemonEvent
	var authHeader string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/ingest/events", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		authHeader = r.Header.Get("Authorization")

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, &received))

		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "test-api-key")

	payload := map[string]interface{}{"draftPack": []string{"card1", "card2"}}
	evt, err := dispatch.BuildEvent("draft.pack", "account-123", "session-abc", payload)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, d.Send(ctx, evt))

	assert.Equal(t, "Bearer test-api-key", authHeader)
	assert.Equal(t, "draft.pack", received.Type)
	assert.Equal(t, "account-123", received.AccountID)
	assert.Equal(t, "session-abc", received.SessionID)
	assert.False(t, received.OccurredAt.IsZero())
	assert.NotEmpty(t, received.Payload)
	// First Send from a new Dispatcher must assign sequence=1 (ADR-013).
	assert.Equal(t, uint64(1), received.Sequence, "first event must have sequence=1")
}

// TestDispatcherSequenceMonotonicallyIncreases verifies that consecutive Send
// calls on the same Dispatcher assign strictly increasing sequence numbers
// starting at 1 (ADR-013).
func TestDispatcherSequenceMonotonicallyIncreases(t *testing.T) {
	var mu sync.Mutex
	var sequences []uint64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var evt contract.DaemonEvent
		require.NoError(t, json.Unmarshal(body, &evt))
		mu.Lock()
		sequences = append(sequences, evt.Sequence)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok")

	const n = 5
	for i := range n {
		evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]int{"i": i})
		require.NoError(t, err)
		require.NoError(t, d.Send(context.Background(), evt))
	}

	require.Len(t, sequences, n)
	for i, seq := range sequences {
		want := uint64(i + 1)
		assert.Equal(t, want, seq, "event %d: sequence mismatch", i)
	}
}

// TestDispatcherHandlesBFFError verifies that non-2xx responses are returned as errors.
// With retry logic the dispatcher will attempt 3 times before returning an error.
func TestDispatcherHandlesBFFError(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "")
	evt, err := dispatch.BuildEvent("test.event", "", "sess", map[string]string{})
	require.NoError(t, err)

	err = d.Send(context.Background(), evt)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
	assert.EqualValues(t, 3, requestCount.Load(), "expected 3 attempts before giving up")
}

// TestDispatcherRetriesOnFailure verifies the dispatcher retries exactly 3 times on
// server errors before returning an error, and that the server received all 3 requests.
func TestDispatcherRetriesOnFailure(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "test-api-key")
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{"k": "v"})
	require.NoError(t, err)

	err = d.Send(context.Background(), evt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all 3 attempts failed")
	assert.EqualValues(t, 3, requestCount.Load(), "server should have received exactly 3 requests")
}

// TestBuildEvent verifies that BuildEvent correctly populates all fields.
func TestBuildEvent(t *testing.T) {
	payload := map[string]interface{}{"key": "value"}
	evt, err := dispatch.BuildEvent("match.completed", "acc-1", "sess-1", payload)
	require.NoError(t, err)

	assert.Equal(t, "match.completed", evt.Type)
	assert.Equal(t, "acc-1", evt.AccountID)
	assert.Equal(t, "sess-1", evt.SessionID)
	assert.WithinDuration(t, time.Now().UTC(), evt.OccurredAt, 5*time.Second)

	// Payload should contain the marshalled data
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(evt.Payload, &decoded))
	assert.Equal(t, "value", decoded["key"])
}

// ---- 401 re-registration ----

// mockRefresher is a test double for dispatch.Refresher.
type mockRefresher struct {
	token string
	err   error
	calls int
}

func (m *mockRefresher) Refresh(_ context.Context) (string, error) {
	m.calls++
	return m.token, m.err
}

// TestDispatcher401TriggersRefresh verifies that a single 401 causes the
// dispatcher to call Refresh and swap in the new token, then succeed on retry.
func TestDispatcher401TriggersRefresh(t *testing.T) {
	var requestCount atomic.Int32
	var authHeaders []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		if n == 1 {
			// First request: return 401 to trigger refresh.
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Second request: succeed.
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	ref := &mockRefresher{token: "refreshed-jwt"}
	d := dispatch.New(srv.URL, "/v1/ingest/events", "old-token").WithRefresher(ref)

	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	require.NoError(t, d.Send(context.Background(), evt))
	assert.Equal(t, 1, ref.calls, "Refresh should have been called exactly once")
	assert.EqualValues(t, 2, requestCount.Load())
	// Second request should carry the refreshed token.
	if len(authHeaders) >= 2 {
		assert.Equal(t, "Bearer refreshed-jwt", authHeaders[1])
	}
}

// TestDispatcher401WithoutRefresherRetriesWithoutTokenChange verifies that when no
// Refresher is set, a 401 is retried normally (without any token swap).
func TestDispatcher401WithoutRefresherRetriesWithoutTokenChange(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "key")
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	err = d.Send(context.Background(), evt)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.EqualValues(t, 3, requestCount.Load(), "should retry all 3 times")
}

// TestDispatcher401RefreshFailureContinuesRetry verifies that if Refresh returns
// an error, the dispatcher still retries with the old token.
func TestDispatcher401RefreshFailureContinuesRetry(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	ref := &mockRefresher{err: errors.New("registration unavailable")}
	d := dispatch.New(srv.URL, "/v1/ingest/events", "key").WithRefresher(ref)

	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	err = d.Send(context.Background(), evt)
	require.Error(t, err)
	// All 3 attempts exhausted despite refresh failure.
	assert.EqualValues(t, 3, requestCount.Load())
}

// TestDispatcher_ErrReauthRequiredBreaksRetryLoop verifies that when a Refresher
// returns ErrReauthRequired the dispatcher breaks the retry loop immediately
// after the first BFF hit and surfaces ErrReauthRequired to the caller.
// The BFF must receive exactly 1 request — no retries.
func TestDispatcher_ErrReauthRequiredBreaksRetryLoop(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	ref := &mockRefresher{err: dispatch.ErrReauthRequired}
	d := dispatch.New(srv.URL, "/v1/ingest/events", "old-token").WithRefresher(ref)

	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	sendErr := d.Send(context.Background(), evt)
	require.Error(t, sendErr)
	assert.True(t, errors.Is(sendErr, dispatch.ErrReauthRequired),
		"error must wrap ErrReauthRequired")
	// Sentinel breaks after 1 attempt — no retries.
	assert.EqualValues(t, 1, requestCount.Load(),
		"BFF must be hit exactly once when refresher returns ErrReauthRequired")
	// Refresher called exactly once.
	assert.Equal(t, 1, ref.calls, "Refresh must be called exactly once")
}

// ---------------------------------------------------------------------------
// OnBFFFailure callback tests (#2139)
// ---------------------------------------------------------------------------

// TestDispatcher_OnBFFFailure_FiredOnTerminalFailure verifies that the
// onBFFFailure callback is invoked exactly once when all retries are exhausted
// and the event is buffered. The last HTTP status code is passed to the callback.
func TestDispatcher_OnBFFFailure_FiredOnTerminalFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	buf := dispatch.NewRingBuffer(10)
	var callbackCount atomic.Int32
	var capturedStatus int

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").
		WithBuffer(buf).
		WithOnBFFFailure(func(statusCode int) {
			callbackCount.Add(1)
			capturedStatus = statusCode
		})

	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)
	require.NoError(t, d.SendOrBuffer(context.Background(), evt))

	assert.EqualValues(t, 1, callbackCount.Load(),
		"onBFFFailure must be called exactly once on terminal failure")
	assert.Equal(t, http.StatusServiceUnavailable, capturedStatus,
		"callback must receive the last BFF status code")
}

// TestDispatcher_OnBFFFailure_NotFiredOnContextCancel verifies that the
// onBFFFailure callback is NOT invoked when the buffer path is taken due to
// context cancellation (graceful shutdown) — only terminal retry exhaustion
// should fire the callback.
func TestDispatcher_OnBFFFailure_NotFiredOnContextCancel(t *testing.T) {
	// Slow server — context will cancel before any response.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	buf := dispatch.NewRingBuffer(10)
	var callbackCount atomic.Int32

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").
		WithBuffer(buf).
		WithOnBFFFailure(func(_ int) {
			callbackCount.Add(1)
		})

	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	_ = d.SendOrBuffer(ctx, evt)

	assert.EqualValues(t, 0, callbackCount.Load(),
		"onBFFFailure must NOT be called on context-cancellation buffering")
}

// TestDispatcher_OnBFFFailure_NotFiredOnSuccess verifies that the failure
// callback is NOT invoked when SendOrBuffer succeeds on the first attempt.
func TestDispatcher_OnBFFFailure_NotFiredOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	buf := dispatch.NewRingBuffer(10)
	var callbackCount atomic.Int32

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").
		WithBuffer(buf).
		WithOnBFFFailure(func(_ int) {
			callbackCount.Add(1)
		})

	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)
	require.NoError(t, d.SendOrBuffer(context.Background(), evt))

	assert.EqualValues(t, 0, callbackCount.Load(),
		"onBFFFailure must NOT be called on success")
}

// TestDispatcher_OnBFFSuccess_FiredOnSuccess verifies that the success callback
// is invoked exactly once when SendOrBuffer delivers the event successfully.
func TestDispatcher_OnBFFSuccess_FiredOnSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	buf := dispatch.NewRingBuffer(10)
	var successCalls atomic.Int32

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").
		WithBuffer(buf).
		WithOnBFFSuccess(func() {
			successCalls.Add(1)
		})

	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)
	require.NoError(t, d.SendOrBuffer(context.Background(), evt))

	assert.EqualValues(t, 1, successCalls.Load(),
		"onBFFSuccess must be called exactly once on successful delivery")
}

// TestDispatcher_OnBFFSuccess_NotFiredOnFailure verifies that the success
// callback is NOT invoked when SendOrBuffer exhausts retries and buffers.
func TestDispatcher_OnBFFSuccess_NotFiredOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	buf := dispatch.NewRingBuffer(10)
	var successCalls atomic.Int32

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").
		WithBuffer(buf).
		WithOnBFFSuccess(func() {
			successCalls.Add(1)
		})

	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)
	require.NoError(t, d.SendOrBuffer(context.Background(), evt))

	assert.EqualValues(t, 0, successCalls.Load(),
		"onBFFSuccess must NOT be called when buffering on terminal failure")
}

// ---------------------------------------------------------------------------
// SendBatch tests (#788 L1-b)
// ---------------------------------------------------------------------------

// TestDispatcher_SendBatch_Success verifies that SendBatch POSTs a JSON array
// to the BFF, fires onBFFSuccess, and returns nil on a 202 response.
func TestDispatcher_SendBatch_Success(t *testing.T) {
	var receivedBody []byte
	var requestCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		body, _ := io.ReadAll(r.Body)
		receivedBody = body
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	var successCalls atomic.Int32
	buf := dispatch.NewRingBuffer(10)
	d := dispatch.New(srv.URL, "/v1/ingest/events", "test-tok").
		WithBuffer(buf).
		WithOnBFFSuccess(func() { successCalls.Add(1) })

	events := []contract.DaemonEvent{
		{Type: "match.started", AccountID: "acc", SessionID: "sess", Sequence: 1},
		{Type: "draft.pick", AccountID: "acc", SessionID: "sess", Sequence: 2},
		{Type: "match.game_ended", AccountID: "acc", SessionID: "sess", Sequence: 3},
	}

	err := d.SendBatch(context.Background(), events)
	require.NoError(t, err)
	assert.EqualValues(t, 1, requestCount.Load(), "BFF must be hit exactly once")
	assert.EqualValues(t, 1, successCalls.Load(), "onBFFSuccess must fire exactly once on success")

	// Verify the body is a JSON array containing all 3 events.
	var decoded []contract.DaemonEvent
	require.NoError(t, json.Unmarshal(receivedBody, &decoded),
		"body must be a valid JSON array")
	require.Len(t, decoded, 3)
	assert.Equal(t, "match.started", decoded[0].Type)
	assert.Equal(t, "draft.pick", decoded[1].Type)
	assert.Equal(t, "match.game_ended", decoded[2].Type)
}

// TestDispatcher_SendBatch_RetryExhaustion_ReEnqueuesPerEvent verifies that
// when SendBatch exhausts all retries, each event in the batch is individually
// re-enqueued in the RingBuffer (not as a single batch blob).
func TestDispatcher_SendBatch_RetryExhaustion_ReEnqueuesPerEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	buf := dispatch.NewRingBuffer(10)
	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").WithBuffer(buf)

	events := []contract.DaemonEvent{
		{Type: "event.a", AccountID: "acc", SessionID: "sess", Sequence: 1},
		{Type: "event.b", AccountID: "acc", SessionID: "sess", Sequence: 2},
		{Type: "event.c", AccountID: "acc", SessionID: "sess", Sequence: 3},
	}

	err := d.SendBatch(context.Background(), events)
	// SendBatch returns an error on retry exhaustion (unlike SendOrBuffer which silences it).
	assert.Error(t, err, "SendBatch must return an error on retry exhaustion")

	// Buffer must contain 3 individual event slots — not 1 batch blob.
	drained := buf.Drain()
	require.Len(t, drained, 3, "each event must be individually re-enqueued in the ring buffer")

	// Each slot must decode as a single DaemonEvent (not an array).
	for i, b := range drained {
		var e contract.DaemonEvent
		require.NoError(t, json.Unmarshal(b, &e), "slot %d must be a single DaemonEvent", i)
		assert.NotEmpty(t, e.Type, "slot %d must have a non-empty event type", i)
	}
}

// TestDispatcher_SendBatch_InheritsDoSend429Backoff verifies that SendBatch
// inherits the 429-aware backoff from doSend — on a 429 with Retry-After: 1,
// the dispatcher waits ~1s before retrying, and the second attempt succeeds.
func TestDispatcher_SendBatch_InheritsDoSend429Backoff(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		if n == 1 {
			// First attempt: return 429 with Retry-After: 1s.
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Second attempt: succeed.
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	buf := dispatch.NewRingBuffer(10)
	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").
		WithBuffer(buf).
		With429MaxBackoff(2 * time.Second)

	events := []contract.DaemonEvent{
		{Type: "test.event", AccountID: "acc", SessionID: "sess", Sequence: 1},
	}

	start := time.Now()
	err := d.SendBatch(context.Background(), events)
	elapsed := time.Since(start)

	require.NoError(t, err, "SendBatch must succeed on 2nd attempt after 429 backoff")
	assert.EqualValues(t, 2, requestCount.Load(), "BFF must be hit exactly twice")
	assert.GreaterOrEqual(t, elapsed, 1*time.Second,
		"429 backoff must hold for at least 1s (Retry-After: 1)")
}

// TestSetToken verifies that SetToken updates the bearer token used on next send.
func TestSetToken(t *testing.T) {
	var lastAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "original")
	d.SetToken("updated-token")

	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)
	require.NoError(t, d.Send(context.Background(), evt))
	assert.Equal(t, "Bearer updated-token", lastAuth)
}
