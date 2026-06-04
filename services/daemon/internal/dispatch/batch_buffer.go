package dispatch

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/RdHamilton/vault-mtg/services/contract"
)

// isErrReauthRequired reports whether err wraps ErrReauthRequired.
func isErrReauthRequired(err error) bool {
	return errors.Is(err, ErrReauthRequired)
}

// BatchBufferConfig holds the options for a BatchBuffer.
type BatchBufferConfig struct {
	// Size is the maximum number of events to accumulate before a flush is
	// triggered (ADR-053: N=25).
	Size int

	// Interval is the maximum time to hold events before flushing even if Size
	// has not been reached (ADR-053: 750ms).
	Interval time.Duration

	// FlushFn is called by the background goroutine to send an accumulated
	// batch.  It must not be nil.  It is always called from a single goroutine
	// so no additional synchronization is needed inside it.
	FlushFn func(ctx context.Context, batch []contract.DaemonEvent) error

	// Stamp is called inside Add to assign a monotonically increasing sequence
	// number to each event before it enters the queue.  The sequence counter
	// lives on the Dispatcher (via d.seq.Add(1)) and is injected here as a
	// closure so BatchBuffer stays independent of Dispatcher internals.
	// Must not be nil.
	Stamp func(e *contract.DaemonEvent)

	// OnErrReauthRequired is an optional callback invoked when FlushFn returns
	// dispatch.ErrReauthRequired.  It replaces the ErrReauthRequired handling
	// that previously lived in handleEntry's SendOrBuffer error branch.
	// If nil, ErrReauthRequired is logged but otherwise silent (the events are
	// still re-enqueued by SendBatch's reEnqueueBatch path).
	OnErrReauthRequired func()
}

// BatchBuffer coalesces contract.DaemonEvent values into batches before
// handing them to a flush function.  It maintains a background goroutine that
// monitors a size trigger and a periodic timer; either condition causes an
// immediate flush.  Forced flushes (for boundary events such as
// match.game_ended and draft.pick) can be requested via FlushNow.
//
// Sequential guarantee: the background goroutine is the only caller of
// FlushFn.  It does NOT hold the queue mutex during the flush call, which
// allows Add to continue filling the next batch while a send is in progress.
// A new flush cannot start until the previous FlushFn call returns.
//
// Graceful shutdown: Close drains all queued events via a final flush before
// returning.
//
// All exported methods are safe to call concurrently from multiple goroutines.
type BatchBuffer struct {
	cfg     BatchBufferConfig
	mu      sync.Mutex
	queue   []contract.DaemonEvent // guarded by mu
	flushCh chan struct{}          // buffered(1): signals background goroutine
	done    chan struct{}          // closed by Close to stop goroutine
	wg      sync.WaitGroup         // tracks background goroutine lifetime
}

// NewBatchBuffer creates a BatchBuffer from cfg.  Start must be called before
// Add or FlushNow will take effect.
func NewBatchBuffer(cfg BatchBufferConfig) *BatchBuffer {
	return &BatchBuffer{
		cfg:     cfg,
		queue:   make([]contract.DaemonEvent, 0, cfg.Size),
		flushCh: make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
}

// Start launches the background flush goroutine.  ctx is used only for the
// lifetime of the goroutine — it does not propagate into individual FlushFn
// calls; those receive a fresh context.Background() each time.
func (b *BatchBuffer) Start(_ context.Context) {
	b.wg.Add(1)
	go b.run()
}

// Add stamps seq on e, appends it to the internal queue, and — if the queue
// has reached the configured size — signals the background goroutine to flush
// immediately.
func (b *BatchBuffer) Add(e contract.DaemonEvent) {
	b.cfg.Stamp(&e)

	b.mu.Lock()
	b.queue = append(b.queue, e)
	full := len(b.queue) >= b.cfg.Size
	b.mu.Unlock()

	if full {
		// Non-blocking send: if a signal is already pending, a new one is not
		// needed.
		select {
		case b.flushCh <- struct{}{}:
		default:
		}
	}
}

// FlushNow signals the background goroutine to flush the current queue
// without waiting for the interval or the size trigger.  It is non-blocking:
// if a signal is already pending, the call is a no-op.
func (b *BatchBuffer) FlushNow() {
	select {
	case b.flushCh <- struct{}{}:
	default:
	}
}

// Close signals the background goroutine to stop and waits for it to drain
// any remaining queued events (final flush) before returning.  After Close
// returns, no further Add or FlushNow calls are meaningful.
func (b *BatchBuffer) Close(_ context.Context) {
	close(b.done)
	b.wg.Wait()
}

// run is the background goroutine body.  It owns all calls to FlushFn.
func (b *BatchBuffer) run() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-b.flushCh:
			b.flush()
		case <-ticker.C:
			b.flush()
		case <-b.done:
			// Final drain: flush any events still in the queue before exiting.
			b.flush()
			return
		}
	}
}

// flush swaps out the current queue under the lock (so Add can continue
// filling the next batch while the send is in flight) and calls FlushFn with
// the swapped-out batch.  It is called only from the single background
// goroutine, enforcing the sequential guarantee.
func (b *BatchBuffer) flush() {
	b.mu.Lock()
	if len(b.queue) == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.queue
	b.queue = make([]contract.DaemonEvent, 0, b.cfg.Size)
	b.mu.Unlock()

	if err := b.cfg.FlushFn(context.Background(), batch); err != nil {
		log.Printf("[batchbuffer] flush error (%d events): %v", len(batch), err)
		// If the error is ErrReauthRequired, notify the caller via the optional
		// callback so it can dispatch a daemon.auth_failed event (replacing the
		// ErrReauthRequired handling that previously lived in handleEntry's
		// SendOrBuffer error branch).
		if isErrReauthRequired(err) && b.cfg.OnErrReauthRequired != nil {
			b.cfg.OnErrReauthRequired()
		}
	}
}
