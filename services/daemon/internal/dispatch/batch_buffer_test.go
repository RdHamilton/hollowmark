package dispatch_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/contract"
	"github.com/RdHamilton/vault-mtg/services/daemon/internal/dispatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildTestEvent is a helper that builds a DaemonEvent for batch_buffer tests.
// Sequence is intentionally left at zero — the stamp function wires in the real value.
func buildTestEvent(t *testing.T, eventType string) contract.DaemonEvent {
	t.Helper()
	evt, err := dispatch.BuildEvent(eventType, "acc-test", "sess-test", map[string]string{"k": "v"})
	require.NoError(t, err)
	return evt
}

// TestBatchBuffer_SizeFlush verifies that when 25 events are added to a
// BatchBuffer whose interval is longer than the test, the flush fires on the
// size trigger — not the timer.
func TestBatchBuffer_SizeFlush(t *testing.T) {
	const N = 25
	var (
		mu          sync.Mutex
		flushed     [][]contract.DaemonEvent
		flushCalled = make(chan struct{}, 1)
	)
	flushFn := func(_ context.Context, batch []contract.DaemonEvent) error {
		mu.Lock()
		flushed = append(flushed, batch)
		mu.Unlock()
		select {
		case flushCalled <- struct{}{}:
		default:
		}
		return nil
	}

	var seq atomic.Uint64
	stamp := func(e *contract.DaemonEvent) { e.Sequence = seq.Add(1) }

	bb := dispatch.NewBatchBuffer(dispatch.BatchBufferConfig{
		Size:     N,
		Interval: 2 * time.Second, // long enough to not fire during test
		FlushFn:  flushFn,
		Stamp:    stamp,
	})
	bb.Start(context.Background())
	defer bb.Close(context.Background())

	for i := 0; i < N; i++ {
		bb.Add(buildTestEvent(t, "test.event"))
	}

	select {
	case <-flushCalled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected size-triggered flush within 500ms, got none")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, flushed, 1, "expected exactly 1 flush call")
	assert.Len(t, flushed[0], N, "flush batch must contain all 25 events")
}

// TestBatchBuffer_IntervalFlush verifies that events enqueued below the size
// threshold are flushed when the interval timer fires.
func TestBatchBuffer_IntervalFlush(t *testing.T) {
	const n = 5
	var (
		mu      sync.Mutex
		flushed [][]contract.DaemonEvent
	)
	done := make(chan struct{})
	flushFn := func(_ context.Context, batch []contract.DaemonEvent) error {
		mu.Lock()
		flushed = append(flushed, batch)
		mu.Unlock()
		close(done)
		return nil
	}

	var seq atomic.Uint64
	stamp := func(e *contract.DaemonEvent) { e.Sequence = seq.Add(1) }

	bb := dispatch.NewBatchBuffer(dispatch.BatchBufferConfig{
		Size:     25,
		Interval: 100 * time.Millisecond, // short interval for test speed
		FlushFn:  flushFn,
		Stamp:    stamp,
	})
	bb.Start(context.Background())
	defer bb.Close(context.Background())

	for i := 0; i < n; i++ {
		bb.Add(buildTestEvent(t, "test.event"))
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected interval-triggered flush within 500ms, got none")
	}

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(flushed), 1, "expected at least 1 flush")
	assert.Len(t, flushed[0], n, "flush batch must contain all 5 events")
}

// TestBatchBuffer_ForcedFlush verifies that FlushNow triggers an immediate
// flush without waiting for the interval or N=25.
func TestBatchBuffer_ForcedFlush(t *testing.T) {
	const n = 10
	var (
		mu      sync.Mutex
		flushed [][]contract.DaemonEvent
	)
	done := make(chan struct{})
	flushFn := func(_ context.Context, batch []contract.DaemonEvent) error {
		mu.Lock()
		flushed = append(flushed, batch)
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	}

	var seq atomic.Uint64
	stamp := func(e *contract.DaemonEvent) { e.Sequence = seq.Add(1) }

	bb := dispatch.NewBatchBuffer(dispatch.BatchBufferConfig{
		Size:     25,
		Interval: 5 * time.Second, // long — must not fire during test
		FlushFn:  flushFn,
		Stamp:    stamp,
	})
	bb.Start(context.Background())
	defer bb.Close(context.Background())

	for i := 0; i < n; i++ {
		bb.Add(buildTestEvent(t, "test.event"))
	}
	bb.FlushNow()

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected FlushNow to trigger flush within 200ms, got none")
	}

	mu.Lock()
	defer mu.Unlock()
	require.GreaterOrEqual(t, len(flushed), 1, "expected at least 1 flush after FlushNow")
	totalEvents := 0
	for _, b := range flushed {
		totalEvents += len(b)
	}
	assert.Equal(t, n, totalEvents, "all enqueued events must appear in flush output")
}

// TestBatchBuffer_Sequential verifies that the flush function is never called
// re-entrantly — at most one in-flight flush call at any moment.
func TestBatchBuffer_Sequential(t *testing.T) {
	var inFlightCount atomic.Int32
	var maxInFlight atomic.Int32

	flushFn := func(_ context.Context, batch []contract.DaemonEvent) error {
		cur := inFlightCount.Add(1)
		// Track the highest observed concurrent call count.
		for {
			old := maxInFlight.Load()
			if cur <= old {
				break
			}
			if maxInFlight.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond) // simulate slow BFF
		inFlightCount.Add(-1)
		return nil
	}

	var seq atomic.Uint64
	stamp := func(e *contract.DaemonEvent) { e.Sequence = seq.Add(1) }

	bb := dispatch.NewBatchBuffer(dispatch.BatchBufferConfig{
		Size:     5,
		Interval: 50 * time.Millisecond,
		FlushFn:  flushFn,
		Stamp:    stamp,
	})
	bb.Start(context.Background())
	defer bb.Close(context.Background())

	// Two goroutines each add 20 events concurrently.
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				bb.Add(buildTestEvent(t, "test.event"))
				time.Sleep(5 * time.Millisecond)
			}
		}()
	}
	wg.Wait()

	// Give the flush goroutine time to drain any remaining queued events.
	time.Sleep(500 * time.Millisecond)

	assert.EqualValues(t, 1, maxInFlight.Load(),
		"flushFn must never be called concurrently — max in-flight must be 1")
}

// TestBatchBuffer_GracefulShutdown verifies that Close drains all queued
// events before returning.
func TestBatchBuffer_GracefulShutdown(t *testing.T) {
	const n = 5
	var (
		mu      sync.Mutex
		flushed []contract.DaemonEvent
	)
	flushFn := func(_ context.Context, batch []contract.DaemonEvent) error {
		mu.Lock()
		flushed = append(flushed, batch...)
		mu.Unlock()
		return nil
	}

	var seq atomic.Uint64
	stamp := func(e *contract.DaemonEvent) { e.Sequence = seq.Add(1) }

	bb := dispatch.NewBatchBuffer(dispatch.BatchBufferConfig{
		Size:     25,
		Interval: 10 * time.Second, // long — should not fire; Close must drain
		FlushFn:  flushFn,
		Stamp:    stamp,
	})
	bb.Start(context.Background())

	for i := 0; i < n; i++ {
		bb.Add(buildTestEvent(t, "test.event"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	bb.Close(ctx)

	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, flushed, n, "all %d events must be flushed before Close returns", n)
}

// TestBatchBuffer_SequenceMonotonic verifies that events added in order carry
// monotonically increasing sequence values when received by the flush sink.
func TestBatchBuffer_SequenceMonotonic(t *testing.T) {
	const n = 10
	var (
		mu      sync.Mutex
		flushed []contract.DaemonEvent
	)
	done := make(chan struct{})
	flushFn := func(_ context.Context, batch []contract.DaemonEvent) error {
		mu.Lock()
		flushed = append(flushed, batch...)
		mu.Unlock()
		if len(flushed) >= n {
			select {
			case done <- struct{}{}:
			default:
			}
		}
		return nil
	}

	var seq atomic.Uint64
	stamp := func(e *contract.DaemonEvent) { e.Sequence = seq.Add(1) }

	bb := dispatch.NewBatchBuffer(dispatch.BatchBufferConfig{
		Size:     n, // flush when all n events arrive
		Interval: 5 * time.Second,
		FlushFn:  flushFn,
		Stamp:    stamp,
	})
	bb.Start(context.Background())
	defer bb.Close(context.Background())

	for i := 0; i < n; i++ {
		evt := buildTestEvent(t, "test.event")
		evt.Sequence = 0 // ensure the stamp function is what sets it
		bb.Add(evt)
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected flush within 500ms for n events at size=n")
	}

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, flushed, n)
	for i, e := range flushed {
		want := uint64(i + 1)
		assert.Equal(t, want, e.Sequence,
			"event at index %d must have sequence=%d, got %d", i, want, e.Sequence)
	}
}

// TestBatchBuffer_MarshaledBatchShape verifies that when flushFn marshals its
// batch as JSON, the result is a top-level array (the shape the BFF expects).
func TestBatchBuffer_MarshaledBatchShape(t *testing.T) {
	const n = 3
	var received []byte
	done := make(chan struct{})

	flushFn := func(_ context.Context, batch []contract.DaemonEvent) error {
		b, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		received = b
		close(done)
		return nil
	}

	var seq atomic.Uint64
	stamp := func(e *contract.DaemonEvent) { e.Sequence = seq.Add(1) }

	bb := dispatch.NewBatchBuffer(dispatch.BatchBufferConfig{
		Size:     n,
		Interval: 5 * time.Second,
		FlushFn:  flushFn,
		Stamp:    stamp,
	})
	bb.Start(context.Background())
	defer bb.Close(context.Background())

	for i := 0; i < n; i++ {
		bb.Add(buildTestEvent(t, "test.event"))
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected flush within 500ms")
	}

	// The marshaled batch must begin with '[' (JSON array).
	require.NotEmpty(t, received)
	assert.Equal(t, byte('['), received[0],
		"marshaled batch payload must be a JSON array (starts with '[')")
}
