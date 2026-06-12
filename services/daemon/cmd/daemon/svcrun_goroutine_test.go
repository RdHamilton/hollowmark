package main

// svcrun_goroutine_test.go — regression tests for the goroutine-funnel
// integration (Ray's required changes on PR #3269, ADR-083 SH-1/SH-3).
//
// svcrun_degrade_test.go already proves runWithDegrade never calls Quit.
// The defect Ray found is in the CALLER goroutine: defer app.Quit() fired
// unconditionally when the goroutine returned, including after a
// runWithDegrade permanent-failure return (SetRunStopped). That path must
// NOT reach stopLaunchAgentFn — the process stays alive in the tray
// "stopped" state (SH-3). These tests exercise runSvcWithQuit, the
// extracted helper that owns the headless/tray svc-run branching inside
// the goroutine — closing the gap the runWithDegrade unit tests could not see.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SH-3 regression: tray permanent-failure must NOT reach stopLaunchAgentFn
// ---------------------------------------------------------------------------

// TestTrayPermanentFailure_DoesNotBootout is the regression test for the
// defect Ray found: when runWithDegrade exhausts all retries (SetRunStopped),
// the goroutine returns, and the surrounding teardown must NOT fire
// stopLaunchAgentFn (which would launchctl bootout the daemon — violating
// SH-3 "process stays alive in stopped state" and SH-1 "bootout only on
// explicit tray-Quit").
//
// This test exercises runSvcWithQuit (the extracted goroutine-funnel
// function) — not runWithDegrade in isolation — so it catches exactly the
// class of defect that Ray identified.
func TestTrayPermanentFailure_DoesNotBootout(t *testing.T) {
	// Inject a bootout spy.
	bootoutCalled := false
	origStop := stopLaunchAgentFn
	t.Cleanup(func() { stopLaunchAgentFn = origStop })
	stopLaunchAgentFn = func() { bootoutCalled = true }

	ctx := context.Background()
	rec := newStateRecorder()

	// svcRun always fails → permanent failure after maxRetries.
	svcRun := func(_ context.Context) error {
		return errors.New("irrecoverable run error")
	}

	// Drive 2 TryAgain signals so all 3 attempts exhaust.
	go func() {
		for i := 0; i < 2; i++ {
			time.Sleep(5 * time.Millisecond)
			rec.signalTryAgain()
		}
	}()

	quitCalled := false
	quitFn := func() { quitCalled = true }

	// headless=false → tray path via runWithDegrade.
	runSvcWithQuit(ctx, svcRun, rec, false, quitFn)

	assert.False(t, bootoutCalled,
		"SH-3/SH-1: stopLaunchAgentFn must NOT be called when tray "+
			"runWithDegrade exhausts retries (process must stay alive in 'stopped' state)")
	assert.False(t, quitCalled,
		"SH-3: app.Quit must NOT be called on tray permanent-failure — "+
			"the process stays alive in the tray stopped state")

	// Confirm the tray did transition to stopped (runWithDegrade worked).
	require.NotEmpty(t, rec.statuses)
	assert.Equal(t, "stopped", rec.statuses[len(rec.statuses)-1],
		"final tray state must be 'stopped' after all retries exhausted")
}

// ---------------------------------------------------------------------------
// SH-2 regression: headless clean exit MUST call quitFn (no headless hang)
// ---------------------------------------------------------------------------

// TestHeadlessCleanExit_CallsQuit guards the headless-hang fix from #1354:
// when svc.Run returns nil in headless mode, quitFn must be called so the
// no-CGO app.Run unblocks. Regressing this would re-introduce the
// smoke-test hang (process never exits).
func TestHeadlessCleanExit_CallsQuit(t *testing.T) {
	bootoutCalled := false
	origStop := stopLaunchAgentFn
	t.Cleanup(func() { stopLaunchAgentFn = origStop })
	stopLaunchAgentFn = func() { bootoutCalled = true }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := newStateRecorder()

	svcRun := func(_ context.Context) error {
		return nil // clean exit
	}

	quitCalled := false
	quitFn := func() { quitCalled = true }

	// headless=true, clean exit.
	runSvcWithQuit(ctx, svcRun, rec, true, quitFn)

	assert.True(t, quitCalled,
		"SH-2: headless clean exit must call quitFn so app.Run unblocks "+
			"(regression guard for #1354 headless-hang fix)")
	assert.False(t, bootoutCalled,
		"headless clean exit must not call stopLaunchAgentFn")
}

// ---------------------------------------------------------------------------
// SH-2: ctx-cancel (SIGTERM drain) in tray mode must call quitFn
// ---------------------------------------------------------------------------

// TestTrayCtxCancel_CallsQuit verifies that when the context is cancelled
// (SIGTERM-induced drain) and runWithDegrade returns cleanly (nil), quitFn
// IS called so app.Run unblocks — this is the genuine teardown path.
func TestTrayCtxCancel_CallsQuit(t *testing.T) {
	bootoutCalled := false
	origStop := stopLaunchAgentFn
	t.Cleanup(func() { stopLaunchAgentFn = origStop })
	stopLaunchAgentFn = func() { bootoutCalled = true }

	ctx, cancel := context.WithCancel(context.Background())

	rec := newStateRecorder()

	svcRun := func(_ context.Context) error {
		cancel() // simulate SIGTERM-induced ctx cancel
		return nil
	}

	quitCalled := false
	quitFn := func() { quitCalled = true }

	// headless=false, context cancelled → clean exit from runWithDegrade.
	runSvcWithQuit(ctx, svcRun, rec, false, quitFn)

	assert.True(t, quitCalled,
		"SH-2: ctx-cancel (SIGTERM drain) in tray mode must call quitFn "+
			"so app.Run unblocks after the drain")
	assert.False(t, bootoutCalled,
		"ctx-cancel must not call stopLaunchAgentFn")
}
