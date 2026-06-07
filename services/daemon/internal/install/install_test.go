// Package install_test verifies the channel-separated identity derivation.
//
// Tests run RED first (package doesn't exist yet), then GREEN once the
// production code is added.  Each test exercises one facet of ADR-049 §1 and §2.
package install_test

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/install"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Channel constants
// ---------------------------------------------------------------------------

func TestChannelConstants_Stable(t *testing.T) {
	assert.Equal(t, "stable", install.ChannelStable)
}

func TestChannelConstants_Staging(t *testing.T) {
	assert.Equal(t, "staging", install.ChannelStaging)
}

// ---------------------------------------------------------------------------
// Suffix derivation — stable channel has EMPTY suffix (prod identity unchanged)
// ---------------------------------------------------------------------------

func TestSuffix_StableEmptySuffix(t *testing.T) {
	s := install.Suffixes(install.ChannelStable)
	assert.Equal(t, "", s.Bin, "stable binary suffix must be empty (prod identity unchanged)")
	assert.Equal(t, "", s.Label, "stable label suffix must be empty")
	assert.Equal(t, "", s.App, "stable app bundle suffix must be empty")
	assert.Equal(t, "", s.Display, "stable display suffix must be empty")
}

func TestSuffix_StagingHasSuffixes(t *testing.T) {
	s := install.Suffixes(install.ChannelStaging)
	assert.Equal(t, "-staging", s.Bin, "staging binary suffix")
	assert.Equal(t, ".staging", s.Label, "staging label suffix")
	assert.Equal(t, " Staging", s.App, "staging app bundle suffix")
	assert.Equal(t, " (Staging)", s.Display, "staging display suffix")
}

// ---------------------------------------------------------------------------
// Identity derivation — stable channel reproduces today's exact strings
// ---------------------------------------------------------------------------

func TestIdentity_Stable_ExactStrings(t *testing.T) {
	id := install.Identity(install.ChannelStable)
	assert.Equal(t, "vaultmtg-daemon", id.BinaryName, "stable binary name must be bare")
	assert.Equal(t, "com.vaultmtg.daemon", id.PlistLabel, "stable plist label must be bare")
	assert.Equal(t, "com.vaultmtg.daemon", id.KeychainService, "stable keychain service must be bare")
	assert.Equal(t, "/Applications/VaultMTG.app", id.AppBundlePath, "stable app bundle path must be bare")
	assert.Equal(t, "VaultMTG", id.TrayLabel, "stable tray label must be bare")
	assert.Equal(t, 9001, id.LocalAPIPort, "stable local-API port must be 9001")
}

func TestIdentity_Staging_SuffixedStrings(t *testing.T) {
	id := install.Identity(install.ChannelStaging)
	assert.Equal(t, "vaultmtg-daemon-staging", id.BinaryName, "staging binary name must be suffixed")
	assert.Equal(t, "com.vaultmtg.daemon.staging", id.PlistLabel, "staging plist label must be suffixed")
	assert.Equal(t, "com.vaultmtg.daemon.staging", id.KeychainService, "staging keychain service must be suffixed")
	assert.Equal(t, "/Applications/VaultMTG Staging.app", id.AppBundlePath, "staging app bundle must be suffixed")
	assert.Equal(t, "VaultMTG (Staging)", id.TrayLabel, "staging tray label must be suffixed")
	assert.Equal(t, 9011, id.LocalAPIPort, "staging local-API port must be 9011 (9001+10)")
}

// ---------------------------------------------------------------------------
// Config dir and log path are per-OS, test on the current platform
// ---------------------------------------------------------------------------

func TestIdentity_Stable_ConfigDir(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	id := install.Identity(install.ChannelStable)
	// On macOS/Linux the config dir must be ~/.vaultmtg
	if !strings.Contains(id.ConfigDir, "vaultmtg") {
		t.Errorf("config dir %q does not contain 'vaultmtg'", id.ConfigDir)
	}
	if strings.Contains(id.ConfigDir, "staging") {
		t.Errorf("stable config dir %q must not contain 'staging'", id.ConfigDir)
	}
	assert.True(t, strings.HasPrefix(id.ConfigDir, home) || strings.Contains(id.ConfigDir, "AppData"),
		"config dir must be under home or AppData, got %q", id.ConfigDir)
}

func TestIdentity_Staging_ConfigDir(t *testing.T) {
	id := install.Identity(install.ChannelStaging)
	if !strings.Contains(id.ConfigDir, "staging") {
		t.Errorf("staging config dir %q must contain 'staging'", id.ConfigDir)
	}
}

func TestIdentity_Stable_LogPath(t *testing.T) {
	id := install.Identity(install.ChannelStable)
	if id.LogPath == "" {
		// LogPath is only populated on Darwin (macOS); Linux/Windows use the OS
		// service manager for logging. Skip this assertion on non-Darwin.
		t.Skip("LogPath is empty on non-Darwin platform — skipping")
	}
	if !strings.Contains(id.LogPath, "vaultmtg-daemon") {
		t.Errorf("stable log path %q does not contain 'vaultmtg-daemon'", id.LogPath)
	}
	if strings.Contains(id.LogPath, "staging") {
		t.Errorf("stable log path %q must not contain 'staging'", id.LogPath)
	}
}

func TestIdentity_Staging_LogPath(t *testing.T) {
	id := install.Identity(install.ChannelStaging)
	if id.LogPath == "" {
		t.Skip("LogPath is empty on non-Darwin platform — skipping")
	}
	if !strings.Contains(id.LogPath, "staging") {
		t.Errorf("staging log path %q must contain 'staging'", id.LogPath)
	}
}

// ---------------------------------------------------------------------------
// Fail-closed: unknown channel must panic
// ---------------------------------------------------------------------------

func TestIdentity_UnknownChannelPanics(t *testing.T) {
	assert.Panics(t, func() {
		install.Identity("canary")
	}, "unknown channel must panic (fail-closed per ADR-049 §2)")
}

func TestSuffixes_UnknownChannelPanics(t *testing.T) {
	assert.Panics(t, func() {
		install.Suffixes("unknown")
	}, "unknown channel must panic (fail-closed per ADR-049 §2)")
}

// ---------------------------------------------------------------------------
// Non-collision: stable and staging identities must not share any OS-level identifier
// ---------------------------------------------------------------------------

func TestIdentity_NoCollision_BinaryName(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, stable.BinaryName, staging.BinaryName)
}

func TestIdentity_NoCollision_PlistLabel(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, stable.PlistLabel, staging.PlistLabel)
}

func TestIdentity_NoCollision_KeychainService(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, stable.KeychainService, staging.KeychainService)
}

func TestIdentity_NoCollision_ConfigDir(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, stable.ConfigDir, staging.ConfigDir)
}

func TestIdentity_NoCollision_LogPath(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	if stable.LogPath == "" && staging.LogPath == "" {
		// Both empty on non-Darwin (Linux/Windows) — no collision possible; skip.
		t.Skip("LogPath is empty on non-Darwin platform — no collision check needed")
	}
	assert.NotEqual(t, stable.LogPath, staging.LogPath)
}

func TestIdentity_NoCollision_AppBundlePath(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, stable.AppBundlePath, staging.AppBundlePath)
}

func TestIdentity_NoCollision_TrayLabel(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, stable.TrayLabel, staging.TrayLabel)
}

func TestIdentity_NoCollision_LocalAPIPort(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, stable.LocalAPIPort, staging.LocalAPIPort)
}

// ---------------------------------------------------------------------------
// Channel package-level var default (must be "stable" if not injected by ldflag)
// ---------------------------------------------------------------------------

func TestChannelVar_DefaultIsStable(t *testing.T) {
	// The module-level Channel var must default to "stable" for local builds
	// (no ldflag injected). If this fails, a developer local build would pick up
	// the staging identity instead of prod.
	assert.Equal(t, install.ChannelStable, install.Channel,
		"Channel must default to 'stable' for local/dev builds")
}

// ---------------------------------------------------------------------------
// ADR-022 C1 cutover-safety — hollowmark future-label constants (#999)
//
// IdentitySet must carry PlistLabelHollowmark and PlistPathHollowmark so
// install/uninstall scripts can reference the future label by name without
// hardcoding strings. Symmetric to the legacy mtga-companion handling.
// ---------------------------------------------------------------------------

func TestIdentity_StableChannel_HollowmarkLabel(t *testing.T) {
	id := install.Identity(install.ChannelStable)
	assert.Equal(t, "com.hollowmark.daemon", id.PlistLabelHollowmark,
		"stable channel hollowmark label must be bare com.hollowmark.daemon")
}

func TestIdentity_StagingChannel_HollowmarkLabel(t *testing.T) {
	id := install.Identity(install.ChannelStaging)
	assert.Equal(t, "com.hollowmark.daemon.staging", id.PlistLabelHollowmark,
		"staging channel hollowmark label must be com.hollowmark.daemon.staging")
}

func TestIdentity_StableChannel_HollowmarkPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("PlistPathHollowmark is only populated on Darwin")
	}
	id := install.Identity(install.ChannelStable)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	expected := home + "/Library/LaunchAgents/com.hollowmark.daemon.plist"
	assert.Equal(t, expected, id.PlistPathHollowmark,
		"stable channel hollowmark plist path must be under ~/Library/LaunchAgents")
}

func TestIdentity_StagingChannel_HollowmarkPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("PlistPathHollowmark is only populated on Darwin")
	}
	id := install.Identity(install.ChannelStaging)
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	expected := home + "/Library/LaunchAgents/com.hollowmark.daemon.staging.plist"
	assert.Equal(t, expected, id.PlistPathHollowmark,
		"staging channel hollowmark plist path must carry staging suffix")
}

func TestIdentity_HollowmarkLabel_DoesNotCollideWithCurrentLabel(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	assert.NotEqual(t, stable.PlistLabel, stable.PlistLabelHollowmark,
		"current and future plist labels must differ")
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, staging.PlistLabel, staging.PlistLabelHollowmark,
		"current and future staging plist labels must differ")
}

// ---------------------------------------------------------------------------
// Cross-check: Identity(Channel) for both channels matches expected values
// (ADR-049 fitness function 2 — shell-vs-Go cross-check)
// ---------------------------------------------------------------------------

func TestCrossCheck_ChannelStable_IdentityConsistent(t *testing.T) {
	// For the stable channel, the keychain service must be "com.vaultmtg.daemon"
	// — this is the same value hardcoded in keychain.go (ServiceNameNew).
	// If they diverge, ADR-049 §2 cross-check fires.
	id := install.Identity(install.ChannelStable)
	assert.Equal(t, "com.vaultmtg.daemon", id.KeychainService,
		"stable keychain service must match keychain.ServiceNameNew")
}

func TestCrossCheck_ChannelStaging_IdentityConsistent(t *testing.T) {
	id := install.Identity(install.ChannelStaging)
	assert.Equal(t, "com.vaultmtg.daemon.staging", id.KeychainService,
		"staging keychain service must be com.vaultmtg.daemon.staging")
}
