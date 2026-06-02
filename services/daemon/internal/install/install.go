// Package install provides the channel-separated install identity for the
// VaultMTG daemon (ADR-049).
//
// # Channel separation
//
// The build channel is injected once via an ldflag at release time:
//
//	-X github.com/RdHamilton/vault-mtg/services/daemon/internal/install.Channel=staging
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
//	-X github.com/RdHamilton/vault-mtg/services/daemon/internal/install.Channel=staging
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
	// "com.vaultmtg.daemon" (stable) or "com.vaultmtg.daemon.staging" (staging).
	// On macOS this is the Keychain Services service; on Windows it is the
	// Windows Credential Manager target prefix (go-keyring format: "<service>:<account>").
	KeychainService string

	// ConfigDir is the directory that holds daemon.json and install-state.json.
	// macOS/Linux: ~/.vaultmtg (stable) or ~/.vaultmtg-staging (staging).
	// Windows: %APPDATA%\vaultmtg (stable) or %APPDATA%\vaultmtg-staging (staging).
	ConfigDir string

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
	// "VaultMTG" (stable) or "VaultMTG (Staging)" (staging).
	TrayLabel string

	// LocalAPIPort is the loopback TCP port the daemon's local HTTP API listens on.
	// 9001 (stable) or 9011 (staging).
	// Using distinct ports allows both daemons to bind simultaneously (ADR-049 §5 risk).
	LocalAPIPort int
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
// The stable channel returns the exact values that are hardcoded in the current
// codebase (keychain.ServiceNameNew, defaultConfigPath, DefaultPort) so there
// is zero behaviour change for existing prod installs when this package is adopted.
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

	return IdentitySet{
		BinaryName:      "vaultmtg-daemon" + s.Bin,
		PlistLabel:      "com.vaultmtg.daemon" + s.Label,
		KeychainService: "com.vaultmtg.daemon" + s.Label,
		ConfigDir:       configDir,
		LogPath:         logPath,
		AppBundlePath:   "/Applications/VaultMTG" + s.App + ".app",
		TrayLabel:       "VaultMTG" + s.Display,
		LocalAPIPort:    port,
	}
}
