package recovery_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/recovery"
)

// captureStore is a thread-safe record of all exceptions passed to a fake
// Sentry capture function, so tests can assert without importing sentry-go.
type captureStore struct {
	mu   sync.Mutex
	errs []error
}

func (c *captureStore) capture(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errs = append(c.errs, err)
}

func (c *captureStore) all() []error {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]error, len(c.errs))
	copy(out, c.errs)
	return out
}

// TestRecoverGoroutine_PanicDoesNotCrashProcess verifies that a goroutine
// that panics with RecoverGoroutine deferred does NOT crash the process and
// that the capture function is called with a non-nil error.
func TestRecoverGoroutine_PanicDoesNotCrashProcess(t *testing.T) {
	store := &captureStore{}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer recovery.RecoverGoroutine("test-goroutine", store.capture)
		panic("deliberate test panic")
	}()
	wg.Wait() // if the panic propagates, this test binary crashes — which is a test failure

	captured := store.all()
	if len(captured) != 1 {
		t.Fatalf("expected 1 captured error, got %d", len(captured))
	}
	if captured[0] == nil {
		t.Fatal("expected non-nil captured error")
	}
}

// TestRecoverGoroutine_ErrorValueContainsPanicMessage verifies that the
// captured error contains the original panic value's string representation.
func TestRecoverGoroutine_ErrorValueContainsPanicMessage(t *testing.T) {
	store := &captureStore{}
	const panicMsg = "something went wrong in poller"

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer recovery.RecoverGoroutine("poller", store.capture)
		panic(panicMsg)
	}()
	wg.Wait()

	captured := store.all()
	if len(captured) == 0 {
		t.Fatal("expected captured error, got none")
	}
	if captured[0].Error() != panicMsg {
		t.Errorf("expected error %q, got %q", panicMsg, captured[0].Error())
	}
}

// TestRecoverGoroutine_NoPanicNoCaptureCall verifies that when a goroutine
// exits cleanly (no panic), the capture function is NOT called.
func TestRecoverGoroutine_NoPanicNoCaptureCall(t *testing.T) {
	store := &captureStore{}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer recovery.RecoverGoroutine("no-panic-goroutine", store.capture)
		// do nothing — no panic
	}()
	wg.Wait()

	if len(store.all()) != 0 {
		t.Errorf("expected no captures for a clean exit, got %d", len(store.all()))
	}
}

// TestRecoverGoroutine_ErrorPanic verifies that panicking with an error value
// (not a string) is correctly captured.
func TestRecoverGoroutine_ErrorPanic(t *testing.T) {
	store := &captureStore{}
	panicErr := fmt.Errorf("io read error: EOF")

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer recovery.RecoverGoroutine("log-tailer", store.capture)
		panic(panicErr)
	}()
	wg.Wait()

	captured := store.all()
	if len(captured) == 0 {
		t.Fatal("expected captured error, got none")
	}
	if captured[0].Error() != panicErr.Error() {
		t.Errorf("expected %q, got %q", panicErr.Error(), captured[0].Error())
	}
}
