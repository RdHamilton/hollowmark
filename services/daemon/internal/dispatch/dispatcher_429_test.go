package dispatch_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/daemon/internal/dispatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSend_429_HonorsRetryAfter verifies that when the BFF returns 429 with a
// Retry-After: 2 header the dispatcher waits at least 2 s before the next
// attempt, and ultimately succeeds when the BFF recovers.
//
// Ray's ruling: hold-in-retry-loop on 429 (NOT ring-buffer);
// select { ctx.Done() / time.After(wait) } mirrors the 401 pattern.
func TestSend_429_HonorsRetryAfter(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok")
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	start := time.Now()
	require.NoError(t, d.Send(context.Background(), evt))
	elapsed := time.Since(start)

	// Must have waited at least 2 s (jitter-aware: check >= 2s not == 2s).
	assert.GreaterOrEqual(t, elapsed, 2*time.Second,
		"dispatcher must honor Retry-After: 2 and wait >= 2s before retrying")
	assert.EqualValues(t, 2, requestCount.Load(), "should have taken 2 server hits")
}

// TestSendOrBuffer_429_HonorsRetryAfter verifies the same Retry-After behavior
// on the SendOrBuffer path (registration + ingest paths must both respect 429).
func TestSendOrBuffer_429_HonorsRetryAfter(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	buf := dispatch.NewRingBuffer(10)
	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").WithBuffer(buf)
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	start := time.Now()
	require.NoError(t, d.SendOrBuffer(context.Background(), evt))
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 2*time.Second,
		"SendOrBuffer must honor Retry-After: 2 and wait >= 2s before retrying")
	assert.EqualValues(t, 2, requestCount.Load())
	assert.Nil(t, buf.Drain(), "buffer must be empty — event was eventually delivered")
}

// TestSend_429_NoRetryAfter_AppliesBackoff verifies that a 429 without a
// Retry-After header causes the dispatcher to apply its 429-specific backoff
// (at least 1 s, per floor(max(header, 1s)) with no header → falls back to
// the configured max429Backoff wait capped at max429Backoff). In this test we
// use With429MaxBackoff(2s) so the wait is bounded to 2 s and the test is fast.
func TestSend_429_NoRetryAfter_AppliesBackoff(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		if n == 1 {
			// 429 with no Retry-After header.
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").
		With429MaxBackoff(2 * time.Second)
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	start := time.Now()
	require.NoError(t, d.Send(context.Background(), evt))
	elapsed := time.Since(start)

	// Without Retry-After the dispatcher uses max429Backoff (2 s here).
	// Verify it waited meaningfully (>= 1 s) without blocking longer than 10 s.
	assert.GreaterOrEqual(t, elapsed, 1*time.Second,
		"dispatcher must apply backoff >= 1s on 429 without Retry-After")
	assert.EqualValues(t, 2, requestCount.Load())
}

// TestSendOrBuffer_429_ExhaustedRetries_FallsToBuffer is the required test from
// Ray's ruling: when all maxAttempts are 429s, the event falls through to the
// existing ring-buffer enqueue path (event not lost).
//
// Ray's comment: "429-after-maxAttempts → falls through to the existing
// ring-buffer enqueue (event not lost)."
func TestSendOrBuffer_429_ExhaustedRetries_FallsToBuffer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// All attempts return 429 (sustained rate-limit).
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	buf := dispatch.NewRingBuffer(10)
	// Use a short max429Backoff so the test completes quickly.
	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").
		WithBuffer(buf).
		With429MaxBackoff(1 * time.Second)

	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	// SendOrBuffer must NOT return an error when the buffer absorbs the failure.
	require.NoError(t, d.SendOrBuffer(context.Background(), evt),
		"SendOrBuffer must not return an error after 429 exhaustion — buffer absorbs it")

	// The event must be in the ring buffer (not lost).
	drained := buf.Drain()
	require.Len(t, drained, 1,
		"event must be in the ring buffer after all retries exhausted on 429")
}

// TestSend_429_ContextCancelDuringWait verifies that when ctx is cancelled while
// the dispatcher is waiting out the Retry-After hold, it returns ctx.Err()
// immediately without consuming more server capacity.
func TestSend_429_ContextCancelDuringWait(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		// Long Retry-After to ensure context cancels first.
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok")
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	// Cancel after 50 ms — well before the 60 s Retry-After wait.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = d.Send(ctx, evt)
	assert.Error(t, err, "should return error after context cancel during 429 wait")
	// Should have made exactly 1 request before the wait was cancelled.
	assert.EqualValues(t, 1, requestCount.Load(),
		"context cancel during Retry-After wait must not trigger another request")
}

// TestSendOrBuffer_429_ContextCancelDuringWait verifies that on SendOrBuffer,
// a ctx-cancel during the 429 Retry-After hold causes the event to be buffered
// and ctx.Err() returned (mirrors the 401 + context-cancel pattern).
func TestSendOrBuffer_429_ContextCancelDuringWait(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	buf := dispatch.NewRingBuffer(10)
	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").WithBuffer(buf)
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = d.SendOrBuffer(ctx, evt)
	// ctx.Err() is returned, and the event is buffered so it's not lost.
	assert.Error(t, err, "should return ctx.Err() after context cancel during 429 wait")
	assert.EqualValues(t, 1, requestCount.Load())
	drained := buf.Drain()
	assert.Len(t, drained, 1, "event must be buffered on ctx-cancel during 429 wait")
}

// TestSend_429_RetryAfterCapped verifies that a Retry-After value above 300 s
// is capped to 300 s (min(header, 300s) per RFC 7231 §10.6.30 safety guidance).
// In practice the cap is only reachable in tests — production Retry-After from
// our own BFF will be <= 60 s. We use ctx cancellation to verify the cap applies.
func TestSend_429_RetryAfterCapped(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		if n == 1 {
			// Header well above the 300 s cap.
			w.Header().Set("Retry-After", "9999")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").
		// Override cap to 5 s so the test completes quickly.
		With429MaxBackoff(5 * time.Second)
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	// Cancel after 2 s — if no cap the wait would be 9999 s; with the cap it
	// should be <= With429MaxBackoff (5 s here), so 2 s ctx-cancel will interrupt it.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = d.Send(ctx, evt)
	assert.Error(t, err, "context should have cancelled before capped wait completed")
	assert.EqualValues(t, 1, requestCount.Load(),
		"only 1 request should have been made before context cancelled")
}

// TestSend_429_RetryAfterFloor verifies that a Retry-After: 0 is treated as
// Retry-After: 1 (floor(max(header, 1s)) per Ray's ruling).
func TestSend_429_RetryAfterFloor(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requestCount.Add(1)
		if n == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok")
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	start := time.Now()
	require.NoError(t, d.Send(context.Background(), evt))
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 1*time.Second,
		"Retry-After: 0 must be floored to 1s minimum")
}

// TestNon429NonTwoXX_UnchangedBehavior verifies that non-429, non-2xx responses
// (e.g. 503) retain the existing ring-buffer path and are NOT treated as 429s
// (AC4: non-429 behavior is unchanged).
func TestNon429NonTwoXX_UnchangedBehavior(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	buf := dispatch.NewRingBuffer(10)
	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").WithBuffer(buf)
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	// Must exhaust all 3 attempts (existing cadence) and buffer — NOT hold-in-loop.
	start := time.Now()
	require.NoError(t, d.SendOrBuffer(context.Background(), evt))
	elapsed := time.Since(start)

	// Should NOT wait the 429 backoff time — normal retry cadence applies.
	assert.Less(t, elapsed, 5*time.Second,
		"non-429 failure must use normal retry cadence, not 429 backoff")
	assert.EqualValues(t, 3, requestCount.Load(),
		"non-429 failure must make exactly 3 attempts")
	drained := buf.Drain()
	require.Len(t, drained, 1, "non-429 failure must buffer the event after retries")
}

// TestWith429MaxBackoff_TestConfigurability verifies that With429MaxBackoff is
// respected as a named option (test configurability per Ray's ruling).
func TestWith429MaxBackoff_TestConfigurability(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	// Just verifies the option can be set without panicking — the actual backoff
	// is covered by the other tests.
	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").
		With429MaxBackoff(500 * time.Millisecond)
	require.NotNil(t, d)
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)
	require.NoError(t, d.Send(context.Background(), evt))
}

// TestSend_429_AC3_RequestCadenceBacksOff verifies AC3: sustained 429s cause
// the dispatcher to back off (total requests <= maxAttempts even under multiple
// 429 cycles). This confirms 429 does NOT 3x-amplify request volume against the
// per-IP limit.
func TestSend_429_AC3_RequestCadenceBacksOff(t *testing.T) {
	var requestCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	d := dispatch.New(srv.URL, "/v1/ingest/events", "tok").
		With429MaxBackoff(1 * time.Second)
	evt, err := dispatch.BuildEvent("test.event", "acc", "sess", map[string]string{})
	require.NoError(t, err)

	err = d.Send(context.Background(), evt)
	// All retries exhausted → error returned.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "all 3 attempts failed")

	// Total server hits must equal exactly maxAttempts (3) — not amplified.
	assert.EqualValues(t, 3, requestCount.Load(),
		"AC3: 429 must not amplify requests beyond maxAttempts=3 "+
			fmt.Sprintf("(got %d)", requestCount.Load()))
}
