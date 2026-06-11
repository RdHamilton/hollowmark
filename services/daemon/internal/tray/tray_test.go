package tray

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Status.label
// ---------------------------------------------------------------------------

func TestStatusLabel(t *testing.T) {
	cases := []struct {
		s    Status
		want string
	}{
		{StatusStarting, "◌ Starting..."},
		// Label changed to "Tracking" per Prof UX sign-off (#1234).
		{StatusConnected, "● Tracking"},
		{StatusWaitingForArena, "◌ Waiting for Arena..."},
		// New status added for ingest-health truthfulness (#1234).
		{StatusSyncIssues, "⚠ Sync issues — games may not be saving"},
		{StatusError, "✕ Error — check logs"},
		{StatusKeychainError, "Keychain unavailable — unlock to continue"},
		{StatusSetupRequired, "⚠ Setup required — auth failed"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.s.label(), "Status(%d)", tc.s)
	}
}

// ---------------------------------------------------------------------------
// App state transitions (no real systray)
// ---------------------------------------------------------------------------

func newTestApp() *App {
	return New("https://app.vaultmtg.app", "dev", func(string) error { return nil }, func() {})
}

func TestAppInitialStatus(t *testing.T) {
	a := newTestApp()
	assert.Equal(t, StatusStarting, a.status)
}

func TestAppSetStatus(t *testing.T) {
	a := newTestApp()
	a.SetStatus(StatusConnected)
	assert.Equal(t, StatusConnected, a.status)

	a.SetStatus(StatusError)
	assert.Equal(t, StatusError, a.status)
}

func TestAppSetLastSync_Zero(t *testing.T) {
	a := newTestApp()
	a.SetLastSync(time.Time{})
	assert.True(t, a.lastSync.IsZero())
}

func TestAppSetLastSync_NonZero(t *testing.T) {
	a := newTestApp()
	ts := time.Date(2026, 5, 12, 14, 30, 0, 0, time.UTC)
	a.SetLastSync(ts)
	assert.Equal(t, ts, a.lastSync)
}

func TestAppSyncNowChannel_NonBlocking(t *testing.T) {
	a := newTestApp()
	// Sending twice without draining should not block (buffered, cap=1).
	a.SyncNow <- struct{}{}
	select {
	case a.SyncNow <- struct{}{}:
		// dropped — channel full, not a panic
	default:
	}
	assert.Len(t, a.SyncNow, 1)
}

func TestAppNew_SetsAppURL(t *testing.T) {
	url := "https://app.vaultmtg.app"
	a := New(url, "dev", nil, nil)
	assert.Equal(t, url, a.appURL)
}

// ---------------------------------------------------------------------------
// About / Check for Updates (ticket #2156)
// ---------------------------------------------------------------------------

func TestAppNew_SetsVersion(t *testing.T) {
	a := New("https://app.vaultmtg.app", "v0.3.4", nil, nil)
	assert.Equal(t, "v0.3.4", a.version)
}

func TestAppNew_SetsVersionDev(t *testing.T) {
	a := New("https://app.vaultmtg.app", "dev", nil, nil)
	assert.Equal(t, "dev", a.version)
}

func TestAppQuitCallback(t *testing.T) {
	called := false
	a := New("https://app.vaultmtg.app", "dev", nil, func() { called = true })
	// Simulate what onExit does inside Run.
	if a.onQuit != nil {
		a.onQuit()
	}
	assert.True(t, called)
}

func TestAppTryAgainChannel_NonBlocking(t *testing.T) {
	a := newTestApp()
	// Sending twice without draining should not block (buffered, cap=1).
	a.TryAgain <- struct{}{}
	select {
	case a.TryAgain <- struct{}{}:
		// dropped — channel full, not a panic
	default:
	}
	assert.Len(t, a.TryAgain, 1)
}

func TestAppSetStatus_KeychainError(t *testing.T) {
	a := newTestApp()
	a.SetStatus(StatusKeychainError)
	assert.Equal(t, StatusKeychainError, a.status)
}

// TestAppSetKeychainError_NoopWithoutMenu verifies that SetKeychainError does
// not panic when miTryAgain is nil (i.e. before setup() has run in tests).
func TestAppSetKeychainError_NoopWithoutMenu(t *testing.T) {
	a := newTestApp()
	// miTryAgain is nil — must not panic.
	assert.NotPanics(t, func() { a.SetKeychainError(true) })
	assert.Equal(t, StatusKeychainError, a.status)
	assert.NotPanics(t, func() { a.SetKeychainError(false) })
}

// ---------------------------------------------------------------------------
// StatusSetupRequired and SetSetupRequired (#2132)
// ---------------------------------------------------------------------------

func TestStatusLabel_SetupRequired(t *testing.T) {
	assert.Equal(t, "⚠ Setup required — auth failed", StatusSetupRequired.label())
}

func TestAppSetStatus_SetupRequired(t *testing.T) {
	a := newTestApp()
	a.SetStatus(StatusSetupRequired)
	assert.Equal(t, StatusSetupRequired, a.status)
}

// TestAppSetSetupRequired_NoopWithoutMenu verifies that SetSetupRequired does
// not panic when miRetrySetup is nil (i.e. before setup() has run in tests).
func TestAppSetSetupRequired_NoopWithoutMenu(t *testing.T) {
	a := newTestApp()
	// miRetrySetup is nil — must not panic.
	assert.NotPanics(t, func() { a.SetSetupRequired(true) })
	assert.Equal(t, StatusSetupRequired, a.status)
	assert.NotPanics(t, func() { a.SetSetupRequired(false) })
}

// TestAppRetrySetupChannel_InitialisedInNew verifies that New() initialises
// RetrySetup as a buffered channel with cap=1 (RC4).
func TestAppRetrySetupChannel_InitialisedInNew(t *testing.T) {
	a := newTestApp()
	assert.NotNil(t, a.RetrySetup, "RetrySetup channel must not be nil after New()")
	assert.Equal(t, 1, cap(a.RetrySetup), "RetrySetup must be buffered cap=1")
}

// TestAppRetrySetupChannel_NonBlocking verifies that sending twice without
// draining does not block (buffered cap=1 drops the second send).
func TestAppRetrySetupChannel_NonBlocking(t *testing.T) {
	a := newTestApp()
	a.RetrySetup <- struct{}{}
	select {
	case a.RetrySetup <- struct{}{}:
		// dropped — channel full, not a panic
	default:
	}
	assert.Len(t, a.RetrySetup, 1)
}

// ---------------------------------------------------------------------------
// InstallUpdate channel (auto-updater — #632)
// ---------------------------------------------------------------------------

// TestAppInstallUpdateChannel_InitialisedInNew verifies that New() initialises
// InstallUpdate as a buffered channel with cap=1. This is required for both
// the CGO tray (real menu item click) and the !CGO stub (headless — channel is
// still referenced by TrayHooks wiring in cmd/daemon/main.go).
func TestAppInstallUpdateChannel_InitialisedInNew(t *testing.T) {
	a := newTestApp()
	assert.NotNil(t, a.InstallUpdate, "InstallUpdate channel must not be nil after New()")
	assert.Equal(t, 1, cap(a.InstallUpdate), "InstallUpdate must be buffered cap=1")
}

// TestAppInstallUpdateChannel_NonBlocking verifies that sending twice without
// draining does not block (buffered cap=1 drops the second send).
func TestAppInstallUpdateChannel_NonBlocking(t *testing.T) {
	a := newTestApp()
	a.InstallUpdate <- struct{}{}
	select {
	case a.InstallUpdate <- struct{}{}:
		// dropped — channel full, not a panic
	default:
	}
	assert.Len(t, a.InstallUpdate, 1)
}

// TestAppNotifyUpdateAvailable_Noop verifies that NotifyUpdateAvailable does not
// panic in the stub (no tray items to update). This guards the method signature
// on the !CGO stub so CGO_ENABLED=0 lifecycle CI continues to compile.
func TestAppNotifyUpdateAvailable_Noop(t *testing.T) {
	a := newTestApp()
	assert.NotPanics(t, func() {
		a.NotifyUpdateAvailable("0.3.7", "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-darwin-universal.pkg")
	})
}

// ---------------------------------------------------------------------------
// StatusSyncIssues — ingest-health truthfulness (#1234)
// ---------------------------------------------------------------------------

// TestStatusLabel_Tracking verifies that StatusConnected now renders "● Tracking"
// (Prof UX sign-off, #1234) rather than the old "● Connected" label.
func TestStatusLabel_Tracking(t *testing.T) {
	assert.Equal(t, "● Tracking", StatusConnected.label())
}

// TestStatusLabel_SyncIssues verifies the new StatusSyncIssues label copy exactly
// matches the Prof-approved wording (#1234).
func TestStatusLabel_SyncIssues(t *testing.T) {
	assert.Equal(t, "⚠ Sync issues — games may not be saving", StatusSyncIssues.label())
}

// TestSetSyncDegraded_WhenArenaRunning verifies that calling SetSyncDegraded(true)
// while Arena is running (status == StatusConnected) transitions to StatusSyncIssues.
func TestSetSyncDegraded_WhenArenaRunning(t *testing.T) {
	a := newTestApp()
	a.status = StatusConnected
	a.SetSyncDegraded(true)
	assert.Equal(t, StatusSyncIssues, a.status)
	assert.True(t, a.syncDegraded)
}

// TestSetSyncDegraded_Recovery verifies that calling SetSyncDegraded(false) while
// degraded and Arena running transitions back to StatusConnected.
func TestSetSyncDegraded_Recovery(t *testing.T) {
	a := newTestApp()
	a.status = StatusSyncIssues
	a.syncDegraded = true
	a.SetSyncDegraded(false)
	assert.Equal(t, StatusConnected, a.status)
	assert.False(t, a.syncDegraded)
}

// TestSetWaitingForArena_RestoresDegraded verifies the two-axis orthogonality:
// when syncDegraded=true, SetWaitingForArena(false) restores StatusSyncIssues,
// not StatusConnected.
func TestSetWaitingForArena_RestoresDegraded(t *testing.T) {
	a := newTestApp()
	a.syncDegraded = true
	a.SetWaitingForArena(true)
	assert.Equal(t, StatusWaitingForArena, a.status)
	a.SetWaitingForArena(false)
	assert.Equal(t, StatusSyncIssues, a.status)
}

// TestSetWaitingForArena_RestoresHealthy verifies that when syncDegraded=false,
// SetWaitingForArena(false) restores StatusConnected as before.
func TestSetWaitingForArena_RestoresHealthy(t *testing.T) {
	a := newTestApp()
	a.syncDegraded = false
	a.SetWaitingForArena(true)
	assert.Equal(t, StatusWaitingForArena, a.status)
	a.SetWaitingForArena(false)
	assert.Equal(t, StatusConnected, a.status)
}

// TestSetSyncDegraded_SkipsWhenWaitingForArena verifies the guard: when Arena is
// not running (StatusWaitingForArena), SetSyncDegraded(true) records the field
// but does NOT override the visible tray status (WaitingForArena wins visually).
func TestSetSyncDegraded_SkipsWhenWaitingForArena(t *testing.T) {
	a := newTestApp()
	a.status = StatusWaitingForArena
	a.SetSyncDegraded(true)
	assert.Equal(t, StatusWaitingForArena, a.status, "WaitingForArena must not be overridden by SetSyncDegraded(true)")
	assert.True(t, a.syncDegraded, "syncDegraded field must be set even when status is not updated")
}

// TestSetSyncDegraded_NoopOnNonIngestStatuses verifies the orthogonality rule
// (Ray amendment §2): SetSyncDegraded must ONLY toggle Connected ↔ SyncIssues
// and must not clobber StatusError, StatusKeychainError, StatusSetupRequired,
// or StatusStarting.
func TestSetSyncDegraded_NoopOnNonIngestStatuses(t *testing.T) {
	noopStatuses := []Status{
		StatusError,
		StatusKeychainError,
		StatusSetupRequired,
		StatusStarting,
	}
	for _, s := range noopStatuses {
		a := newTestApp()
		a.status = s
		// SetSyncDegraded(true) must not clobber these states.
		a.SetSyncDegraded(true)
		assert.Equal(t, s, a.status, "SetSyncDegraded(true) must not clobber %v", s)
		// SetSyncDegraded(false) must also not clobber these states.
		a.SetSyncDegraded(false)
		assert.Equal(t, s, a.status, "SetSyncDegraded(false) must not clobber %v", s)
	}
}
