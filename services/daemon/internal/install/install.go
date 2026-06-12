// Package install provides the channel-separated install identity for the
// VaultMTG daemon (ADR-049).
//
// # Channel separation
//
// The build channel is injected once via an ldflag at release time:
//
//	-X github.com/RdHamilton/hollowmark/services/daemon/internal/install.Channel=staging
//
// The stable (production) channel keeps the bare identity so existing installs
// are untouched (ADR-049 §5). The staging channel takes a "-staging" / ".staging"
// suffix across every OS-level identifier, so both daemons coexist on the same
// machine without colliding (the Chrome / Chrome-Canary model).
//
// # Source of truth
//
// This package is the Go-side single source of truth for all channel-derived
// identity constants — the exact mirror of common.sh / common.ps1 (ADR-036 I-4).
// Any constant that appears in an install/uninstall script must also be derived
// here, and the per-channel cross-check test in install_test.go enforces that
// shell and Go agree.
//
// # Fail-closed
//
// Calls to Identity or Suffixes with an unrecognised channel string panic with a
// clear message. This ensures a binary built with a typo'd ldflag fails loudly
// at startup rather than silently writing to the wrong identity (ADR-049 §2 risk
// mitigation).
package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Channel is the build-time channel string, injected via ldflag:
//
//	-X github.com/RdHamilton/hollowmark/services/daemon/internal/install.Channel=staging
//
// Defaults to ChannelStable ("stable") for local/dev builds. The release
// workflow sets this to the correct value for every build; an unknown value
// causes Identity/Suffixes to panic (fail-closed).
var Channel = ChannelStable

const (
	// ChannelStable is the production / stable release channel.
	// Binaries on this channel carry the bare identity (no suffix) so existing
	// installs are bit-for-bit unchanged (ADR-049 §5).
	ChannelStable = "stable"

	// ChannelStaging is the pre-release / staging channel.
	// Binaries on this channel carry the "-staging" suffix family so they can
	// coexist side-by-side with the stable daemon (ADR-049 §1).
	ChannelStaging = "staging"
)

// SuffixSet is the tuple of suffix strings for a single channel.
// Every identity element is derived as baseString + the appropriate suffix.
type SuffixSet struct {
	// Bin is appended to binary names: "" (stable) or "-staging".
	Bin string
	// Label is appended to reverse-DNS labels: "" (stable) or ".staging".
	Label string
	// App is appended to the macOS .app bundle name: "" (stable) or " Staging".
	App string
	// Display is appended to user-visible strings: "" (stable) or " (Staging)".
	Display string
}

// Suffixes returns the SuffixSet for the given channel.
// Panics on an unrecognised channel (fail-closed, ADR-049 §2).
func Suffixes(channel string) SuffixSet {
	switch channel {
	case ChannelStable:
		return SuffixSet{Bin: "", Label: "", App: "", Display: ""}
	case ChannelStaging:
		return SuffixSet{Bin: "-staging", Label: ".staging", App: " Staging", Display: " (Staging)"}
	default:
		panic(fmt.Sprintf("install: unknown channel %q — must be %q or %q", channel, ChannelStable, ChannelStaging))
	}
}

// IdentitySet is the complete set of OS-level identifiers for a daemon install.
// All fields are derived from the channel; none is a per-channel literal.
type IdentitySet struct {
	// BinaryName is the daemon executable name without extension.
	// "vaultmtg-daemon" (stable) or "vaultmtg-daemon-staging" (staging).
	BinaryName string

	// PlistLabel is the macOS LaunchAgent label / Windows Task Scheduler task name.
	// "com.vaultmtg.daemon" (stable) or "com.vaultmtg.daemon.staging" (staging).
	PlistLabel string

	// KeychainService is the OS keychain service name.
	// "com.hollowmark.daemon" (stable) or "com.hollowmark.daemon.staging" (staging).
	// This is the ADR-022 Phase 3 (v0.3.9) credential shim target — all new credential
	// writes land here; the legacy "com.vaultmtg.daemon" entries are migrated on first
	// startup via keychain.Get() and retained for downgrade safety until Phase 6.
	// On macOS this is the Keychain Services service; on Windows it is the
	// Windows Credential Manager target prefix (go-keyring format: "<service>:<account>").
	KeychainService string

	// ConfigDir is the directory that holds daemon.json and install-state.json.
	// macOS/Linux: ~/.vaultmtg (stable) or ~/.vaultmtg-staging (staging).
	// Windows: %APPDATA%\vaultmtg (stable) or %APPDATA%\vaultmtg-staging (staging).
	ConfigDir string

	// CredentialFile is the path of the 0600 credential file used on darwin
	// (ADR-081 §Decision 1, #1345). Derived as filepath.Join(ConfigDir, "credentials").
	// This is the authoritative per-channel path for the file-store backend;
	// callers must use this field rather than re-deriving to ensure staging and
	// prod never collide and uninstall scripts know exactly what to clean up.
	// Populated on all platforms (tooling / uninstall use), though only darwin
	// actively writes to it at runtime.
	CredentialFile string

	// LogPath is the daemon log file path (macOS).
	// ~/Library/Logs/vaultmtg-daemon.log (stable)
	// ~/Library/Logs/vaultmtg-daemon-staging.log (staging).
	// On non-Darwin platforms this field is an empty string (logging handled
	// by the OS service manager).
	LogPath string

	// AppBundlePath is the macOS launcher .app bundle path.
	// "/Applications/VaultMTG.app" (stable) or "/Applications/VaultMTG Staging.app" (staging).
	AppBundlePath string

	// TrayLabel is the user-visible tray icon title / tooltip.
	// "Hollowmark" (stable) or "Hollowmark (Staging)" (staging).
	TrayLabel string

	// LocalAPIPort is the loopback TCP port the daemon's local HTTP API listens on.
	// 9001 (stable) or 9011 (staging).
	// Using distinct ports allows both daemons to bind simultaneously (ADR-049 §5 risk).
	LocalAPIPort int

	// PlistLabelHollowmark is the future macOS LaunchAgent label used when the
	// bundle-ID renames to com.hollowmark.daemon at v0.4.0 (ADR-022 Phase 2).
	// "com.hollowmark.daemon" (stable) or "com.hollowmark.daemon.staging" (staging).
	//
	// In v0.3.9 this label is NOT loaded — it is present only so install/uninstall
	// scripts can defensively boot it out if a user has a v0.4.0+ daemon installed
	// and then rolls back, preventing double-launch (ADR-022 Constraint C1).
	// Symmetric to PlistLabelLegacy (com.mtga-companion.daemon) which handles the
	// past rename.
	PlistLabelHollowmark string

	// PlistPathHollowmark is the ~/Library/LaunchAgents path for PlistLabelHollowmark.
	// Only populated on Darwin; empty on other platforms (no launchd).
	PlistPathHollowmark string
}

const (
	// stablePort is the local-API port for the stable/prod daemon.
	stablePort = 9001
	// stagingPortOffset is added to stablePort for the staging daemon.
	stagingPortOffset = 10
)

// Identity returns the IdentitySet for the given channel.
// Panics on an unrecognised channel (fail-closed, ADR-049 §2).
//
// KeychainService uses the ADR-022 Phase 3 (v0.3.9) hollowmark credential shim
// name ("com.hollowmark.daemon") so all new PKCE logins write to the new slot.
// The v0.3.8 "com.vaultmtg.daemon" credentials are migrated transparently via
// keychain.Get() on first startup and retained for downgrade safety.
// PlistLabel and BinaryName intentionally retain their v0.3.8 values because the
// bundle-ID rename is v0.4.0 scope (PRD AC15).
func Identity(channel string) IdentitySet {
	s := Suffixes(channel) // panics on unknown channel

	home, _ := os.UserHomeDir()

	var configDir string
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			configDir = filepath.Join(appdata, "vaultmtg"+s.Bin)
		} else {
			configDir = filepath.Join(home, "AppData", "Roaming", "vaultmtg"+s.Bin)
		}
	} else {
		configDir = filepath.Join(home, ".vaultmtg"+s.Bin)
	}

	var logPath string
	if runtime.GOOS == "darwin" {
		logPath = filepath.Join(home, "Library", "Logs", "vaultmtg-daemon"+s.Bin+".log")
	}

	port := stablePort
	if channel == ChannelStaging {
		port = stablePort + stagingPortOffset
	}

	// ADR-022 C1 cutover-safety: future hollowmark label (v0.4.0 rename target).
	// Not loaded in v0.3.9 — present for defensive boot-out in install/uninstall.
	plistLabelHollowmark := "com.hollowmark.daemon" + s.Label
	var plistPathHollowmark string
	if runtime.GOOS == "darwin" {
		plistPathHollowmark = filepath.Join(home, "Library", "LaunchAgents", plistLabelHollowmark+".plist")
	}

	return IdentitySet{
		BinaryName:           "vaultmtg-daemon" + s.Bin,
		PlistLabel:           "com.vaultmtg.daemon" + s.Label,   // unchanged in v0.3.9 per PRD AC15
		KeychainService:      "com.hollowmark.daemon" + s.Label, // ADR-022 Phase 3 credential shim
		ConfigDir:            configDir,
		CredentialFile:       filepath.Join(configDir, "credentials"), // ADR-081 §Decision 1
		LogPath:              logPath,
		AppBundlePath:        "/Applications/VaultMTG" + s.App + ".app",
		TrayLabel:            "Hollowmark" + s.Display,
		LocalAPIPort:         port,
		PlistLabelHollowmark: plistLabelHollowmark,
		PlistPathHollowmark:  plistPathHollowmark,
	}
}
