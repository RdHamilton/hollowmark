package main

// svcrun_degrade.go — ADR-083 SH-3: tray-mode degrade-and-retry for svc.Run errors.
//
// When svc.Run returns a non-nil error in tray mode the daemon must NOT call
// app.Quit or stopLaunchAgent. Instead it transitions the tray to an error
// state and waits for the user to click "Try Again". On each retry it re-invokes
// svcRun. After maxRetries total attempts it transitions to a "stopped" state
// so the user knows the daemon has given up, but the PROCESS stays alive (the
// tray icon remains). This satisfies AC1–AC6 and the ADR-081 idle-degrade tie-in.
//
// The headless run-error path (os.Exit + logAndExitHeadlessKeychain) is unchanged
// — it lives in main.go and is only reached when headless==true. This file covers
// the TRAY (CGO) branch only.

import (
	"context"
	"time"
)

// runDegradeHooks is the interface the retry loop needs from the tray.
// It is intentionally narrow: only the state transitions and channels
// required by the degrade loop. The real implementation is provided by
// app (*tray.App) wired in main(); tests supply a recorder.
type runDegradeHooks interface {
	// SetRunError transitions the tray to the error state (AC1).
	SetRunError()
	// SetRunStopped transitions the tray to the permanently-stopped state (AC3).
	SetRunStopped()
	// SetConnected restores the tray to the healthy connected state (AC6).
	SetConnected()
	// Quit tears down the tray. The degrade loop never calls this; it is
	// included so tests can assert it was NOT called.
	Quit()
	// TryAgainCh returns the channel signalled when the user clicks "Try Again".
	TryAgainCh() <-chan struct{}
}

// backoffFn returns the duration to sleep before attempt number n (0-indexed).
type backoffFn func(attempt int) time.Duration

// defaultBackoff returns exponential backoff: 2^attempt seconds, capped at 60s.
// Attempt 0 → 1s, attempt 1 → 2s, attempt 2 → 4s, etc.
func defaultBackoff(attempt int) time.Duration {
	d := time.Duration(1<<uint(attempt)) * time.Second
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

// runWithDegrade runs svcRun inside a degrade-and-retry loop. It is the
// tray-mode successor to the bare "svc.Run(ctx); if err { app.Quit() }" block.
//
// Loop behaviour:
//  1. Call svcRun(ctx).
//  2. On nil return (clean exit) or ctx cancellation: call hooks.SetConnected()
//     and return nil — the tray stays alive in a healthy state.
//  3. On non-nil error AND attempt < maxRetries:
//     a. hooks.SetRunError() — tray shows error indicator.
//     b. Wait for TryAgainCh signal or ctx.Done().
//     c. On TryAgain: sleep backoff(attempt), increment attempt, go to 1.
//     d. On ctx.Done(): return nil (clean context-cancel exit).
//  4. On non-nil error AND attempt == maxRetries:
//     hooks.SetRunStopped() — tray shows permanently-stopped state.
//     Return the last error (for logging by the caller).
//
// maxRetries is the total number of svcRun invocations allowed (first call +
// retries). Callers should pass 3 (first attempt + 2 retries), matching AC2/AC3.
//
// backoff is called with the current zero-indexed attempt number to determine
// the sleep duration before each retry. Pass defaultBackoff in production;
// tests pass zeroBackoff to avoid delays.
//
// INVARIANT: this function NEVER calls hooks.Quit() or stopLaunchAgent() —
// those are reserved for the SIGTERM and tray-quit paths respectively (ADR-083).
func runWithDegrade(
	ctx context.Context,
	svcRun func(context.Context) error,
	hooks runDegradeHooks,
	maxRetries int,
	backoff backoffFn,
) error {
	for attempt := 0; attempt < maxRetries; attempt++ {
		err := svcRun(ctx)

		// Clean exit — restore connected state and return.
		if err == nil {
			hooks.SetConnected()
			return nil
		}

		// svcRun returned a non-nil error. Always surface the error state (AC1),
		// then decide whether to wait for a retry or exit immediately.
		//
		// The ctx-cancel check comes AFTER recording the error state so that a
		// svcRun that calls cancel() AND returns an error still records the
		// error transition (AC1 requirement).

		if attempt == maxRetries-1 {
			// All attempts exhausted — show permanently-stopped state (AC3).
			hooks.SetRunStopped()
			return err
		}

		// Transition to error state and wait for user retry (AC1, AC2).
		hooks.SetRunError()

		// If context is already cancelled at this point, exit without waiting.
		if ctx.Err() != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			// Context cancelled while waiting for retry — exit cleanly.
			return nil
		case <-hooks.TryAgainCh():
			// User clicked "Try Again" — apply backoff then loop.
			if d := backoff(attempt); d > 0 {
				t := time.NewTimer(d)
				select {
				case <-ctx.Done():
					t.Stop()
					return nil
				case <-t.C:
				}
			}
		}
	}

	return nil
}
