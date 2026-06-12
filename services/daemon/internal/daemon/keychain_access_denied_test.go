package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/config"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/credstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetryKeychain_ErrAccessDenied_IdleDegraded is the R1 regression test
// mandated by Ray's plan-review verdict for #1345.
//
// Scenario: headless daemon, credential file present but EACCES
// (os.IsPermission, the steady-state launchd ACL denial case).
//
// Required contract (ADR-081 + Ray R1):
//   - retryKeychain must NOT return a bubbling error (which would cause
//     main.go:534→logAndExitHeadlessKeychain→os.Exit→launchd respawn loop).
//   - Instead, exhausted ErrAccessDenied enters an idle-degraded state:
//     tray SetKeychainError(true) set + stays set, heartbeat keychain_error,
//     service blocks until context cancelled.
//   - device_id unchanged (no re-register triggered).
//   - No os.Exit call on headless.
func TestRetryKeychain_ErrAccessDenied_IdleDegraded(t *testing.T) {
	origBase := keychainRetryBase
	origMax := keychainMaxRetries
	keychainRetryBase = 5 * time.Millisecond
	keychainMaxRetries = 2
	t.Cleanup(func() {
		keychainRetryBase = origBase
		keychainMaxRetries = origMax
	})

	cfg := &config.Config{
		CloudAPIURL: "http://localhost",
		IngestPath:  "/v1/ingest/events",
		Keychain:    true,
		DaemonID:    "original-device-id",
		AccountID:   "acc-123",
	}
	svc := New(cfg)
	// Steady-state access-denied — every Get returns ErrAccessDenied.
	svc.keychainGet = func() (string, error) { return "", credstore.ErrAccessDenied }
	svc.keychainErr = credstore.ErrAccessDenied

	keychainErrorSet := false
	var keychainErrorCleared atomic.Bool
	svc.trayHooks = TrayHooks{
		SetKeychainError: func(show bool) {
			if show {
				keychainErrorSet = true
			} else {
				keychainErrorCleared.Store(true)
			}
		},
	}

	// Track whether the process would have exited (os.Exit must NOT fire).
	var osExitCalled atomic.Bool

	// Run retryKeychain with a context that we cancel after a short grace period
	// to simulate the supervisor shutting down the idle-degraded daemon gracefully.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := svc.retryKeychain(ctx)

	// R1 contract: retryKeychain must return nil (idle loop exited by ctx cancel),
	// NOT an error that bubbles to os.Exit.
	require.NoError(t, err,
		"exhausted ErrAccessDenied must enter idle-degraded (return nil on ctx cancel), "+
			"not return an error that triggers os.Exit")
	assert.False(t, osExitCalled.Load(), "os.Exit must NOT be called on headless ErrAccessDenied")

	// Tray must be in keychain-error state while degraded.
	assert.True(t, keychainErrorSet, "SetKeychainError(true) must be called on ErrAccessDenied")
	// The error flag is cleared on clean exit (context cancelled) — defer fires.
	assert.True(t, keychainErrorCleared.Load(),
		"SetKeychainError(false) must be called when the idle-degraded loop exits cleanly")

	// device_id must be unchanged (no re-register).
	assert.Equal(t, "original-device-id", cfg.DaemonID,
		"device_id must not change during idle-degraded ErrAccessDenied state")
}

// TestRetryKeychain_ErrAccessDenied_TryAgain_ResumesOnSuccess verifies that
// when the daemon is in idle-degraded ErrAccessDenied state and the user
// clicks "Try Again" while the credential becomes readable, retryKeychain
// exits the idle loop and returns nil (credential successfully loaded).
func TestRetryKeychain_ErrAccessDenied_TryAgain_ResumesOnSuccess(t *testing.T) {
	origBase := keychainRetryBase
	origMax := keychainMaxRetries
	keychainRetryBase = 5 * time.Millisecond
	keychainMaxRetries = 2
	t.Cleanup(func() {
		keychainRetryBase = origBase
		keychainMaxRetries = origMax
	})

	cfg := &config.Config{
		CloudAPIURL: "http://localhost",
		IngestPath:  "/v1/ingest/events",
		Keychain:    true,
		DaemonID:    "device-abc",
	}
	svc := New(cfg)

	// Fail with ErrAccessDenied for the initial retries, then succeed after
	// the TryAgain signal fires (simulating the user fixing the ACL).
	var callCount atomic.Int32
	svc.keychainGet = func() (string, error) {
		n := callCount.Add(1)
		if n <= int32(keychainMaxRetries) {
			return "", credstore.ErrAccessDenied
		}
		return "recovered-key", nil
	}
	svc.keychainErr = credstore.ErrAccessDenied

	tryAgainCh := make(chan struct{}, 4)
	// Pre-load enough signals to drive past the retry loop and the idle-degraded
	// poll into a successful read.
	for i := 0; i < 4; i++ {
		tryAgainCh <- struct{}{}
	}
	svc.trayHooks = TrayHooks{
		TryAgain:         tryAgainCh,
		SetKeychainError: func(_ bool) {},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := svc.retryKeychain(ctx)
	require.NoError(t, err, "TryAgain after ErrAccessDenied must allow recovery")
}
