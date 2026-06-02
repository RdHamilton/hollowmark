//go:build !cgo

// Headless (no-CGO) stub — used when the binary is cross-compiled without CGO
// (e.g. darwin targets built from the Linux GoReleaser runner). The daemon runs
// as a launchd service in that context and has no tray icon.
package tray

import "time"

// Status represents the daemon's connection state.
type Status int

const (
	StatusStarting Status = iota
	StatusConnected
	StatusWaitingForArena
	StatusError
	StatusKeychainError
	StatusSetupRequired
)

func (s Status) label() string {
	switch s {
	case StatusConnected:
		return "● Connected"
	case StatusWaitingForArena:
		return "◌ Waiting for Arena..."
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

// App is a no-op tray stub for headless builds.
type App struct {
	appURL  string
	version string
	// appLabel is the user-visible title shown next to the tray icon and in
	// the tooltip. "VaultMTG" for the stable channel; "VaultMTG (Staging)"
	// for the staging channel. Set via NewWithLabel; defaults to "VaultMTG".
	appLabel string
	onQuit   func()
	status   Status
	lastSync time.Time

	quit        chan struct{}
	SyncNow     chan struct{}
	GrantAccess chan struct{}
	TryAgain    chan struct{}
	// RetrySetup is signalled when the user requests setup retry. Always
	// buffered cap=1 so callers can send without blocking even in headless mode.
	RetrySetup chan struct{}
	// InstallUpdate is signalled when the user requests an update install.
	// Present on the stub so cmd/daemon/main.go can wire it into TrayHooks
	// without a build tag — buffered cap=1.
	InstallUpdate chan struct{}
}

// New creates a no-op App with the default "VaultMTG" label. version is stored
// but not rendered (headless stub). For channel-aware label use NewWithLabel.
func New(appURL, version string, openURL func(string) error, onQuit func()) *App {
	return NewWithLabel(appURL, version, openURL, onQuit, "VaultMTG")
}

// NewWithLabel creates a no-op App with an explicit tray label (ADR-049 Ticket 4).
// Pass install.Identity(channel).TrayLabel as the label argument so the tray
// title reflects the channel ("VaultMTG" vs "VaultMTG (Staging)").
func NewWithLabel(appURL, version string, openURL func(string) error, onQuit func(), label string) *App {
	return &App{
		appURL:        appURL,
		version:       version,
		appLabel:      label,
		onQuit:        onQuit,
		status:        StatusStarting,
		quit:          make(chan struct{}),
		SyncNow:       make(chan struct{}, 1),
		GrantAccess:   make(chan struct{}, 1),
		TryAgain:      make(chan struct{}, 1),
		RetrySetup:    make(chan struct{}, 1),
		InstallUpdate: make(chan struct{}, 1),
	}
}

// Run calls onReady immediately then blocks until Quit is called.
func (a *App) Run(onReady func()) {
	if onReady != nil {
		onReady()
	}
	<-a.quit
	if a.onQuit != nil {
		a.onQuit()
	}
}

// Quit unblocks Run. Safe to call from any goroutine.
func (a *App) Quit() {
	select {
	case <-a.quit:
	default:
		close(a.quit)
	}
}

func (a *App) SetStatus(s Status)        { a.status = s }
func (a *App) SetHelperInstalled(_ bool) {}
func (a *App) SetLastSync(t time.Time)   { a.lastSync = t }
func (a *App) SetKeychainError(show bool) {
	if show {
		a.status = StatusKeychainError
	}
}

func (a *App) SetSetupRequired(show bool) {
	if show {
		a.status = StatusSetupRequired
	}
}
func (a *App) SetWaitingForArena(_ bool)         {}
func (a *App) NotifySyncResult(_ error)          {} // headless stub — no tray label to update
func (a *App) NotifyUpdateAvailable(_, _ string) {} // headless stub — no tray item to show
