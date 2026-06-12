//go:build cgo

// Package tray manages the system tray (menu bar) icon and menu for the
// Hollowmark daemon. systray.Run must be called on the main OS thread; callers
// must invoke App.Run from main() and start the daemon event loop inside the
// onReady callback.
package tray

import (
	_ "embed"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"fyne.io/systray"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/install"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/recovery"
	"github.com/getsentry/sentry-go"
)

// NSApplicationActivationPolicy values, mirrored from AppKit so the
// cross-platform setup() logging can name the policy returned by
// applyAccessoryPolicy without importing Cocoa. Only macOS uses these at
// runtime; the non-darwin stub reports activationPolicyAccessory.
const (
	activationPolicyRegular    = 0 // NSApplicationActivationPolicyRegular
	activationPolicyAccessory  = 1 // NSApplicationActivationPolicyAccessory (desired)
	activationPolicyProhibited = 2 // NSApplicationActivationPolicyProhibited (icon will NOT render)
)

// activationPolicyName returns a human-readable name for an
// NSApplicationActivationPolicy value, for log output.
func activationPolicyName(p int) string {
	switch p {
	case activationPolicyRegular:
		return "Regular"
	case activationPolicyAccessory:
		return "Accessory"
	case activationPolicyProhibited:
		return "Prohibited"
	default:
		return fmt.Sprintf("Unknown(%d)", p)
	}
}

//go:embed assets/icon.png
var prodIconData []byte

//go:embed assets/staging_icon.png
var stagingIconData []byte

// iconBytes returns the tray icon bytes for the current build channel.
// Returns stagingIconData when install.Channel is "staging"; prodIconData
// for all other channels (including the default "stable" channel).
func iconBytes() []byte {
	if install.Channel == install.ChannelStaging {
		return stagingIconData
	}
	return prodIconData
}

// Status represents the daemon's connection state shown in the menu bar.
type Status int

const (
	StatusStarting Status = iota
	// StatusConnected is the healthy ingest state. The label renders as
	// "● Tracking" (Prof UX sign-off, #1234) — the constant name is unchanged
	// to avoid blast-radius across callers; only the visible label changed.
	StatusConnected
	StatusWaitingForArena
	StatusError
	StatusKeychainError
	StatusSetupRequired
	// StatusSyncIssues indicates Arena is detected but consecutive BFF ingest
	// failures have reached dispatchDegradedThreshold. The ingest-health axis
	// (Connected ↔ SyncIssues) is orthogonal to all other status values —
	// this state is set only from SetSyncDegraded and only when the current
	// status is StatusConnected. It never clobbers StatusError, StatusKeychainError,
	// StatusSetupRequired, StatusStarting, or StatusWaitingForArena. (#1234)
	StatusSyncIssues
)

func (s Status) label() string {
	switch s {
	case StatusConnected:
		// Label changed to "Tracking" per Prof UX sign-off (#1234).
		// The constant name stays StatusConnected to avoid caller blast-radius.
		return "● Tracking"
	case StatusWaitingForArena:
		return "◌ Waiting for Arena..."
	case StatusSyncIssues:
		return "⚠ Sync issues — games may not be saving"
	case StatusError:
		return "✕ Error — check logs"
	case StatusKeychainError:
		return "Keychain unavailable — unlock to continue"
	case StatusSetupRequired:
		return "⚠ Setup required — auth failed"
	default:
		return "◌ Starting..."
	}
}

// App manages the tray icon, menu items, and status state.
type App struct {
	appURL  string
	version string
	// appLabel is the user-visible title shown next to the tray icon (macOS) and
	// in the tooltip. "Hollowmark" for the stable channel; "Hollowmark (Staging)"
	// for the staging channel. Set via NewWithLabel; defaults to "Hollowmark".
	appLabel string
	openURL  func(string) error
	onQuit   func()

	// statusMu guards status and syncDegraded. These fields are written from the
	// dispatcher callback goroutine (SetSyncDegraded, called by recordBFFFailure /
	// clearBFFFailureCounter) and read/written from the idle-loop goroutine
	// (SetWaitingForArena) — a genuine cross-goroutine data race without the lock.
	// SetStatus acquires statusMu; callers that already hold statusMu call
	// setStatusLocked. (#1234 Ray amendment §5)
	statusMu sync.Mutex
	// status is the current tray status. Guard with statusMu.
	status Status
	// syncDegraded is true when the ingest-health axis is degraded
	// (consecutiveBFFFailures >= dispatchDegradedThreshold). It is the
	// persistent ingest-health state that SetWaitingForArena consults when
	// restoring the tray after Arena re-opens, so the two axes never clobber
	// each other. Guard with statusMu. (#1234)
	syncDegraded bool
	// lastSync and helperInstalled are single-goroutine (loop) only after setup().
	lastSync        time.Time
	helperInstalled bool

	miStatus          *systray.MenuItem
	miAbout           *systray.MenuItem
	miCheckForUpdates *systray.MenuItem
	miUpdateAvailable *systray.MenuItem
	miLastSync        *systray.MenuItem
	miSyncNow         *systray.MenuItem
	// miSyncIssues is a disabled informational item shown below miStatus when
	// the tray is in StatusSyncIssues. It displays the last-saved timestamp so
	// the player can see how long events have been failing. (#1234)
	miSyncIssues  *systray.MenuItem
	miGrantAccess *systray.MenuItem
	miTryAgain    *systray.MenuItem
	miRetrySetup  *systray.MenuItem
	miOpenApp     *systray.MenuItem
	miQuit        *systray.MenuItem

	// syncMu guards syncInFlight.
	syncMu sync.Mutex
	// syncInFlight is true while a Sync Now operation is in progress.
	// Concurrent clicks are dropped until the current sync completes (AC4).
	syncInFlight bool

	// SyncNow is signalled when the user clicks "Sync Now".
	SyncNow chan struct{}
	// GrantAccess is signalled when the user clicks "Grant Access".
	GrantAccess chan struct{}
	// TryAgain is signalled when the user clicks "Try Again" (keychain retry).
	TryAgain chan struct{}
	// RetrySetup is signalled when the user clicks "Retry Setup…". The handler
	// opens https://vaultmtg.app/setup in the browser and re-runs the PKCE flow.
	// Buffered cap=1 so a second click before the first is handled is dropped.
	RetrySetup chan struct{}
	// InstallUpdate is signalled when the user clicks "Update available: vX.Y.Z".
	// Buffered cap=1; a second click while install is in progress is dropped.
	InstallUpdate chan struct{}
}

// New creates an App with the default "Hollowmark" label. appURL is opened when
// "Open Hollowmark" is clicked. version is the daemon build version (injected via
// -ldflags -X main.Version=<ver>; defaults to "dev" for local builds) and is
// displayed in the "About" menu item. openURL is the platform open-browser
// function. onQuit is called when the tray exits (Quit clicked or process
// terminated). For channel-aware label use NewWithLabel.
func New(appURL, version string, openURL func(string) error, onQuit func()) *App {
	return NewWithLabel(appURL, version, openURL, onQuit, "Hollowmark")
}

// NewWithLabel creates an App with an explicit tray label (ADR-049 Ticket 4).
// Pass install.Identity(channel).TrayLabel as the label argument so the tray
// title reflects the channel: "Hollowmark" (stable) or "Hollowmark (Staging)" (staging).
func NewWithLabel(appURL, version string, openURL func(string) error, onQuit func(), label string) *App {
	return &App{
		appURL:        appURL,
		version:       version,
		appLabel:      label,
		openURL:       openURL,
		onQuit:        onQuit,
		status:        StatusStarting,
		SyncNow:       make(chan struct{}, 1),
		GrantAccess:   make(chan struct{}, 1),
		TryAgain:      make(chan struct{}, 1),
		RetrySetup:    make(chan struct{}, 1),
		InstallUpdate: make(chan struct{}, 1),
	}
}

// Run blocks the calling goroutine (must be the main OS thread on macOS).
// onReady is called after the menu bar icon is ready; start the daemon event
// loop inside it (in a new goroutine).
func (a *App) Run(onReady func()) {
	// Best-effort first attempt at the UIElement activation policy before
	// entering the NSRunLoop inside systray.Run. NOTE: on macOS 13–15 a policy
	// set this early (before applicationDidFinishLaunching / the run loop) is
	// silently dropped, so this call is NOT authoritative — the authoritative
	// set happens inside the onReady callback in setup() via applyAccessoryPolicy.
	// Retained as a harmless first attempt. No-op on non-Darwin platforms
	// (tray_nondarwin.go) and on headless machines (no WindowServer session).
	ensureUIElementPolicy()
	systray.Run(func() {
		a.setup()
		if onReady != nil {
			onReady()
		}
		go a.loop()
	}, func() {
		if a.onQuit != nil {
			a.onQuit()
		}
	})
}

// Quit tears down the tray icon and unblocks Run. Safe to call from any goroutine.
func (a *App) Quit() {
	systray.Quit()
}

// SetStatus updates the status label in the menu. Safe to call from any goroutine.
func (a *App) SetStatus(s Status) {
	a.statusMu.Lock()
	a.setStatusLocked(s)
	a.statusMu.Unlock()
}

// setStatusLocked applies a status update. Must be called with statusMu held.
func (a *App) setStatusLocked(s Status) {
	a.status = s
	if a.miStatus != nil {
		a.miStatus.SetTitle(s.label())
	}
}

// SetHelperInstalled shows or hides the "Grant Access" menu item.
// Call with true once the helper is confirmed running; false shows the install prompt.
func (a *App) SetHelperInstalled(installed bool) {
	a.helperInstalled = installed
	if a.miGrantAccess == nil || a.miSyncNow == nil {
		return
	}
	if installed {
		a.miGrantAccess.Hide()
		a.miSyncNow.Show()
	} else {
		a.miGrantAccess.Show()
		a.miSyncNow.Hide()
	}
}

// SetSetupRequired shows or hides the "Retry Setup…" menu item and updates the
// status label to StatusSetupRequired. Call with true when PKCE auth fails in
// onReady; false to hide the item once setup completes.
func (a *App) SetSetupRequired(show bool) {
	if show {
		a.SetStatus(StatusSetupRequired)
		if a.miRetrySetup != nil {
			a.miRetrySetup.Show()
		}
	} else {
		if a.miRetrySetup != nil {
			a.miRetrySetup.Hide()
		}
	}
}

// SetKeychainError shows or hides the "Try Again" item and updates the status label.
// Call with true when keychain is unavailable; false to restore normal state.
func (a *App) SetKeychainError(show bool) {
	if show {
		a.SetStatus(StatusKeychainError)
		if a.miTryAgain != nil {
			a.miTryAgain.Show()
		}
	} else {
		if a.miTryAgain != nil {
			a.miTryAgain.Hide()
		}
	}
}

// NotifyUpdateAvailable shows the "Update available: vX.Y.Z — Click to Install"
// menu item. On Windows the tooltip also notes the binary is unsigned (beta).
// Safe to call from any goroutine.
func (a *App) NotifyUpdateAvailable(version, _ string) {
	if a.miUpdateAvailable == nil {
		return
	}
	title := "Update available: v" + version + " — Click to Install"
	// Windows: warn about unsigned binary per Sarah PR-3.
	if isWindows() {
		title += " (unverified by Microsoft — beta)"
	}
	a.miUpdateAvailable.SetTitle(title)
	a.miUpdateAvailable.Show()
}

// SetWaitingForArena switches the tray status to StatusWaitingForArena (waiting=true)
// or restores the correct ingest-health state (waiting=false). When the ingest axis
// is degraded (syncDegraded=true), restoring from WaitingForArena shows SyncIssues
// rather than Connected, so the two axes never clobber each other. (#1234)
// Safe to call from any goroutine.
func (a *App) SetWaitingForArena(waiting bool) {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	if waiting {
		a.setStatusLocked(StatusWaitingForArena)
	} else if a.syncDegraded {
		a.setStatusLocked(StatusSyncIssues)
	} else {
		a.setStatusLocked(StatusConnected)
	}
}

// SetSyncDegraded updates the ingest-health axis of the tray (Connected ↔ SyncIssues).
// When degraded=true and Arena is running (status == StatusConnected), transitions to
// StatusSyncIssues and shows the miSyncIssues informational item. When degraded=false
// and the tray is currently showing StatusSyncIssues, transitions back to StatusConnected.
//
// Orthogonality rule (Ray amendment §2, #1234): this method ONLY toggles
// StatusConnected ↔ StatusSyncIssues. It never clobbers StatusError,
// StatusKeychainError, StatusSetupRequired, StatusStarting, or StatusWaitingForArena.
// When Arena is not running (status == StatusWaitingForArena), the syncDegraded field
// is updated but the visible tray status is left unchanged; SetWaitingForArena(false)
// will restore the correct health state when Arena re-opens.
//
// Safe to call from any goroutine.
func (a *App) SetSyncDegraded(degraded bool) {
	a.statusMu.Lock()
	a.syncDegraded = degraded
	currentStatus := a.status
	a.statusMu.Unlock()

	switch {
	case degraded && currentStatus == StatusConnected:
		a.statusMu.Lock()
		a.setStatusLocked(StatusSyncIssues)
		a.statusMu.Unlock()
		if a.miSyncIssues != nil {
			a.miSyncIssues.Show()
		}
	case !degraded && currentStatus == StatusSyncIssues:
		a.statusMu.Lock()
		a.setStatusLocked(StatusConnected)
		a.statusMu.Unlock()
		if a.miSyncIssues != nil {
			a.miSyncIssues.Hide()
		}
		// All other statuses (WaitingForArena, Error, KeychainError, SetupRequired,
		// Starting) are not touched — syncDegraded field is already updated above
		// and will be consulted by SetWaitingForArena on Arena re-open.
	}
}

// SetLastSync updates the "last synced" timestamp label. Safe to call from any goroutine.
func (a *App) SetLastSync(t time.Time) {
	a.lastSync = t
	if a.miLastSync != nil {
		if t.IsZero() {
			a.miLastSync.SetTitle("Collection: never synced")
		} else {
			a.miLastSync.SetTitle(fmt.Sprintf("Collection: synced %s", t.Format("3:04 PM")))
		}
	}
	// Keep the sync-issues informational item current when degraded.
	if a.miSyncIssues != nil {
		if t.IsZero() {
			a.miSyncIssues.SetTitle("Last saved: —")
		} else {
			a.miSyncIssues.SetTitle(fmt.Sprintf("Last saved: %s", t.Format("3:04 PM")))
		}
	}
}

func (a *App) setup() {
	log.Printf("[tray] setup: entering onReady (channel=%s label=%q)", install.Channel, a.appLabel)

	// Authoritative activation-policy set. This runs inside the systray onReady
	// window — i.e. during/after applicationDidFinishLaunching with the
	// NSApplication run loop already up — which is the only point at which
	// setActivationPolicy:NSApplicationActivationPolicyAccessory reliably takes
	// effect on macOS 13–15. A policy set before systray.Run (ensureUIElementPolicy)
	// is silently dropped on those versions, leaving the process effectively
	// .prohibited; a .prohibited process cannot place an NSStatusItem, so the
	// icon is created but stays invisible. We set it here and log the resulting
	// policy so the tray lifecycle is greppable in the daemon log.
	// On non-Darwin platforms applyAccessoryPolicy is a no-op returning Accessory.
	policy := applyAccessoryPolicy()
	if policy == activationPolicyAccessory {
		log.Printf("[tray] setup: activation policy set OK -> %s (icon can render)", activationPolicyName(policy))
	} else {
		log.Printf("[tray] setup: WARN activation policy is %s after set attempt — menu-bar icon may not render", activationPolicyName(policy))
	}

	systray.SetIcon(iconBytes())
	log.Printf("[tray] setup: icon set (%d bytes)", len(iconBytes()))
	// Tooltip shows the channel-specific label so users know which channel is running.
	systray.SetTooltip(a.appLabel)

	// On macOS the menu bar title is shown next to the icon.
	if runtime.GOOS == "darwin" {
		systray.SetTitle(a.appLabel)
	}

	// About item — disabled (informational label showing the running version).
	// Positioned at the top so the version is immediately visible without scrolling.
	a.miAbout = systray.AddMenuItem(a.appLabel+" Daemon "+a.version, "Running version")
	a.miAbout.Disable()

	// Check for Updates — opens the GitHub Releases page for the daemon.
	a.miCheckForUpdates = systray.AddMenuItem("Check for Updates", "Opens GitHub Releases page for the Hollowmark daemon")

	// Update available — hidden until the update-check loop finds a newer version.
	a.miUpdateAvailable = systray.AddMenuItem("Update available", "A new daemon version is available")
	a.miUpdateAvailable.Hide()

	systray.AddSeparator()

	a.miStatus = systray.AddMenuItem(a.status.label(), "Daemon status")
	a.miStatus.Disable()

	// miSyncIssues shows the last-saved timestamp while the tray is in
	// StatusSyncIssues, so the player can see how stale their data is.
	// Hidden until SetSyncDegraded(true) fires. (#1234)
	a.miSyncIssues = systray.AddMenuItem("Last saved: —", "")
	a.miSyncIssues.Disable()
	a.miSyncIssues.Hide()

	systray.AddSeparator()

	a.miLastSync = systray.AddMenuItem("Collection: never synced", "")
	a.miLastSync.Disable()
	a.miSyncNow = systray.AddMenuItem("Sync Now", "Read collection from MTGA")
	a.miGrantAccess = systray.AddMenuItem("Grant Access…", "Install the collection helper (requires admin password)")
	// Show whichever is appropriate; default to showing Grant Access until the
	// daemon confirms the helper is running.
	a.miSyncNow.Hide()

	a.miTryAgain = systray.AddMenuItem("Try Again", "Retry reading from macOS keychain")
	a.miTryAgain.Hide()

	a.miRetrySetup = systray.AddMenuItem("Retry Setup…", "Re-open setup page and retry authentication")
	a.miRetrySetup.Hide()

	systray.AddSeparator()

	a.miOpenApp = systray.AddMenuItem("Open "+a.appLabel, "Open the Hollowmark web app")

	systray.AddSeparator()

	a.miQuit = systray.AddMenuItem("Quit", "Stop the "+a.appLabel+" daemon")
}

// openCheckForUpdates opens the GitHub Releases page for the VaultMTG daemon
// in the default browser. Extracted so it can be tested without systray.
func (a *App) openCheckForUpdates() {
	if a.openURL != nil {
		_ = a.openURL("https://github.com/RdHamilton/hollowmark/releases?q=daemon")
	}
}

// tryStartSync attempts to claim the sync lock. Returns true if the sync may
// proceed (syncInFlight was false and is now set to true), false if a sync is
// already in flight (debounce — AC4). Extracted for testability.
func (a *App) tryStartSync() bool {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	if a.syncInFlight {
		return false
	}
	a.syncInFlight = true
	return true
}

// NotifySyncResult is called by the daemon after a Sync Now operation completes.
// It updates the tray item label to show success ("Synced") or failure
// ("Sync failed"), holds it briefly, then resets to "Sync Now" and clears the
// in-flight flag so subsequent clicks are accepted again.
//
// When miSyncNow is nil (headless / pre-setup), the label steps are skipped but
// the in-flight flag is still cleared — matching the nil-guard pattern used by
// SetKeychainError and SetSetupRequired.
//
// Safe to call from any goroutine.
func (a *App) NotifySyncResult(err error) {
	if a.miSyncNow != nil {
		if err != nil {
			a.miSyncNow.SetTitle("Sync failed")
			time.Sleep(3 * time.Second)
		} else {
			a.miSyncNow.SetTitle("Synced")
			time.Sleep(2 * time.Second)
		}
		a.miSyncNow.SetTitle("Sync Now")
	}
	a.syncMu.Lock()
	a.syncInFlight = false
	a.syncMu.Unlock()
}

func (a *App) loop() {
	defer recovery.RecoverGoroutine("tray-loop", recovery.CaptureFn(sentry.CurrentHub().CaptureException))
	for {
		select {
		case <-a.miCheckForUpdates.ClickedCh:
			a.openCheckForUpdates()
		case <-a.miUpdateAvailable.ClickedCh:
			select {
			case a.InstallUpdate <- struct{}{}:
			default:
			}
		case <-a.miSyncNow.ClickedCh:
			if a.tryStartSync() {
				a.miSyncNow.SetTitle("Syncing...")
				select {
				case a.SyncNow <- struct{}{}:
				default: // channel full — daemon is busy; clear in-flight so the next click works
					a.syncMu.Lock()
					a.syncInFlight = false
					a.syncMu.Unlock()
					a.miSyncNow.SetTitle("Sync Now")
				}
			}
		case <-a.miGrantAccess.ClickedCh:
			select {
			case a.GrantAccess <- struct{}{}:
			default:
			}
		case <-a.miTryAgain.ClickedCh:
			select {
			case a.TryAgain <- struct{}{}:
			default:
			}
		case <-a.miRetrySetup.ClickedCh:
			select {
			case a.RetrySetup <- struct{}{}:
			default:
			}
		case <-a.miOpenApp.ClickedCh:
			if a.openURL != nil {
				_ = a.openURL(a.appURL)
			}
		case <-a.miQuit.ClickedCh:
			systray.Quit()
		}
	}
}
