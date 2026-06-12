package main

// svcrun_degrade_test.go — TDD RED-first tests for AC1–AC6 of #1356
// (ADR-083 SH-3: svc.Run error in tray mode degrades to error state + retry).
//
// These tests drive runWithDegrade, which does NOT exist yet.
// All tests must FAIL until the implementation is written.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runDegradeStateRecorder is a test double for runDegradeHooks that records
// every state-transition call so tests can assert the exact sequence.
type runDegradeStateRecorder struct {
	statuses   []string // "error", "stopped", "connected"
	tryAgainC  chan struct{}
	quitCalled bool
}

func newStateRecorder() *runDegradeStateRecorder {
	return &runDegradeStateRecorder{
		tryAgainC: make(chan struct{}, 10),
	}
}

func (r *runDegradeStateRecorder) SetRunError() { r.statuses = append(r.statuses, "error") }

func (r *runDegradeStateRecorder) SetRunStopped() { r.statuses = append(r.statuses, "stopped") }

func (r *runDegradeStateRecorder) SetConnected()               { r.statuses = append(r.statuses, "connected") }
func (r *runDegradeStateRecorder) Quit()                       { r.quitCalled = true }
func (r *runDegradeStateRecorder) TryAgainCh() <-chan struct{} { return r.tryAgainC }

// signalTryAgain injects a user "Try Again" click into the recorder.
func (r *runDegradeStateRecorder) signalTryAgain() {
	select {
	case r.tryAgainC <- struct{}{}:
	default:
	}
}

// ---------------------------------------------------------------------------
// AC5: svc.Run returning an error in tray mode must NOT call app.Quit or any
//      launchctl command — process stays alive.
// ---------------------------------------------------------------------------

func TestRunWithDegrade_SvcRunError_DoesNotQuit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newStateRecorder()

	// svc.Run always errors — cancel context after first call so loop exits.
	svcRun := func(c context.Context) error {
		cancel()
		return errors.New("keychain exhausted")
	}

	_ = runWithDegrade(ctx, svcRun, rec, 3, zeroBackoff)

	assert.False(t, rec.quitCalled,
		"AC5: app.Quit must NOT be called when svc.Run returns an error in tray mode")
}

// ---------------------------------------------------------------------------
// AC1: tray icon must transition to error state when svc.Run returns an error.
// ---------------------------------------------------------------------------

func TestRunWithDegrade_SvcRunError_SetsErrorState(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newStateRecorder()

	svcRun := func(c context.Context) error {
		cancel() // exit after first attempt
		return errors.New("run failed")
	}

	_ = runWithDegrade(ctx, svcRun, rec, 3, zeroBackoff)

	require.NotEmpty(t, rec.statuses, "AC1: at least one tray state must be recorded on error")
	assert.Equal(t, "error", rec.statuses[0],
		"AC1: first tray transition on svc.Run error must be to error state")
}

// ---------------------------------------------------------------------------
// AC2: daemon attempts at least one retry (max 3 total attempts).
// ---------------------------------------------------------------------------

func TestRunWithDegrade_TriesUpToMaxRetries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rec := newStateRecorder()

	attempts := 0
	svcRun := func(c context.Context) error {
		attempts++
		return errors.New("transient failure")
	}

	// Drive 2 TryAgain signals → 3 total attempts, then loop stops on max.
	go func() {
		for i := 0; i < 2; i++ {
			time.Sleep(5 * time.Millisecond)
			rec.signalTryAgain()
		}
	}()

	_ = runWithDegrade(ctx, svcRun, rec, 3, zeroBackoff)

	assert.Equal(t, 3, attempts,
		"AC2: with maxRetries=3, svc.Run must be called exactly 3 times")
}

// ---------------------------------------------------------------------------
// AC3: on permanent failure (all retries exhausted), tray shows "stopped" state;
//      app.Quit and stopLaunchAgent must NOT be called.
// ---------------------------------------------------------------------------

func TestRunWithDegrade_PermanentFailure_ShowsStoppedNotQuit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rec := newStateRecorder()

	svcRun := func(c context.Context) error {
		return errors.New("irrecoverable")
	}

	// Drive 2 TryAgain signals to exhaust all retries.
	go func() {
		for i := 0; i < 2; i++ {
			time.Sleep(5 * time.Millisecond)
			rec.signalTryAgain()
		}
	}()

	_ = runWithDegrade(ctx, svcRun, rec, 3, zeroBackoff)

	assert.False(t, rec.quitCalled,
		"AC3: app.Quit must NOT be called on permanent failure")
	require.NotEmpty(t, rec.statuses)
	assert.Equal(t, "stopped", rec.statuses[len(rec.statuses)-1],
		"AC3: final tray state after all retries exhausted must be 'stopped'")
}

// ---------------------------------------------------------------------------
// AC6: if svc.Run fails N times then succeeds, tray state ends at "connected".
// ---------------------------------------------------------------------------

func TestRunWithDegrade_FailNTimesThenSucceeds_StateTransitions(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newStateRecorder()

	attempts := 0
	svcRun := func(c context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("transient")
		}
		// Third attempt succeeds — cancel so the loop exits.
		cancel()
		return nil
	}

	// Provide 2 TryAgain signals for retries 1→2 and 2→3.
	go func() {
		for i := 0; i < 2; i++ {
			time.Sleep(5 * time.Millisecond)
			rec.signalTryAgain()
		}
	}()

	_ = runWithDegrade(ctx, svcRun, rec, 3, zeroBackoff)

	require.GreaterOrEqual(t, len(rec.statuses), 3,
		"AC6: must record error states for failures and connected on success")
	assert.Equal(t, "error", rec.statuses[0], "AC6: first state must be error (attempt 1 failed)")
	assert.Equal(t, "error", rec.statuses[1], "AC6: second state must be error (attempt 2 failed)")
	assert.Equal(t, "connected", rec.statuses[len(rec.statuses)-1],
		"AC6: final state after successful run must be connected")
	assert.Equal(t, 3, attempts, "AC6: svc.Run must be called 3 times (2 failures + 1 success)")
	assert.False(t, rec.quitCalled, "AC6: app.Quit must not be called when recovery succeeds")
}

// ---------------------------------------------------------------------------
// AC4: keychain-exhaustion errors are retriable, not immediately fatal.
// ---------------------------------------------------------------------------

func TestRunWithDegrade_KeychainExhaustionIsRetriable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rec := newStateRecorder()

	attempts := 0
	svcRun := func(c context.Context) error {
		attempts++
		return errors.New("keychain: errSecInteractionNotAllowed")
	}

	// Send 2 TryAgain signals — keychain errors must be retried.
	go func() {
		for i := 0; i < 2; i++ {
			time.Sleep(5 * time.Millisecond)
			rec.signalTryAgain()
		}
	}()

	_ = runWithDegrade(ctx, svcRun, rec, 3, zeroBackoff)

	assert.Equal(t, 3, attempts,
		"AC4: keychain-exhaustion errors must be treated as retriable (all 3 attempts consumed)")
	assert.False(t, rec.quitCalled,
		"AC4: process must not quit on keychain-exhaustion errors")
}

// ---------------------------------------------------------------------------
// Context-cancel exits the loop cleanly without calling Quit.
// ---------------------------------------------------------------------------

func TestRunWithDegrade_CtxCancelExitsWithoutQuit(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	rec := newStateRecorder()

	svcRun := func(c context.Context) error {
		cancel()
		return nil
	}

	_ = runWithDegrade(ctx, svcRun, rec, 3, zeroBackoff)

	assert.False(t, rec.quitCalled, "ctx cancel must not trigger app.Quit")
}

// ---------------------------------------------------------------------------
// Clean exit (svc.Run returns nil) leaves tray in connected state.
// ---------------------------------------------------------------------------

func TestRunWithDegrade_CleanExit_LeavesConnectedState(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	rec := newStateRecorder()

	svcRun := func(c context.Context) error {
		cancel()
		return nil
	}

	_ = runWithDegrade(ctx, svcRun, rec, 3, zeroBackoff)

	assert.False(t, rec.quitCalled, "clean exit must not call Quit")
	// No error state transitions on a clean run.
	for _, s := range rec.statuses {
		assert.NotEqual(t, "error", s, "clean exit must not set error state")
		assert.NotEqual(t, "stopped", s, "clean exit must not set stopped state")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// zeroBackoff is a no-op backoff for tests — eliminates sleep delays so tests
// complete in milliseconds.
var zeroBackoff backoffFn = func(attempt int) time.Duration { return 0 }
