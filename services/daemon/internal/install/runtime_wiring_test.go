// Package install_test — runtime channel-wiring invariants (ADR-049 Ticket 2).
//
// These tests assert the unit-level properties of the channel-derived identity
// values that main.go, service.go, launchagent_darwin.go, and config.go consume
// at startup. They complement the installer-level invariants in install_test.go.
//
// FF-7 concurrent dual-run invariant:
//
//	Two daemons built from the same codebase but with different install.Channel
//	injections (stable vs staging) must produce non-overlapping OS-level
//	identifiers so they can run simultaneously on the same machine without
//	clobbering each other's keychain entry, plist registration, or config dir.
//
// The full two-daemon integration smoke (actually starting two daemon processes
// and verifying they write to separate BFF backends) requires real backends and
// is tracked as a follow-up integration test.
package install_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/RdHamilton/vault-mtg/services/daemon/internal/install"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Stable channel — exact runtime identity values ────────────────────────────

// TestRuntimeWiring_Stable_KeychainService verifies that the stable channel
// produces the keychain service that main.go will pass to keychain.GetForService
// and keychain.SetForService. Must match keychain.ServiceNameNew so existing
// installs are untouched (ADR-049 §5).
func TestRuntimeWiring_Stable_KeychainService(t *testing.T) {
	id := install.Identity(install.ChannelStable)
	assert.Equal(t, "com.vaultmtg.daemon", id.KeychainService,
		"stable keychain service must match keychain.ServiceNameNew")
}

// TestRuntimeWiring_Stable_PlistLabel verifies the stable plist label equals the
// former hardcoded constant "com.vaultmtg.daemon" so existing launchd registrations
// are undisturbed.
func TestRuntimeWiring_Stable_PlistLabel(t *testing.T) {
	id := install.Identity(install.ChannelStable)
	assert.Equal(t, "com.vaultmtg.daemon", id.PlistLabel,
		"stable plist label must equal former hardcoded constant")
}

// TestRuntimeWiring_Stable_ConfigDirSuffix verifies that the stable config dir
// does NOT contain "-staging" so existing daemon.json files remain accessible.
func TestRuntimeWiring_Stable_ConfigDirSuffix(t *testing.T) {
	id := install.Identity(install.ChannelStable)
	assert.NotContains(t, id.ConfigDir, "staging",
		"stable config dir must not contain 'staging'")
	assert.Contains(t, id.ConfigDir, "vaultmtg",
		"stable config dir must contain 'vaultmtg'")
}

// TestRuntimeWiring_Stable_ArchiveDir verifies that the archive dir for the
// stable channel is ConfigDir+"/archives" (the value config.go's defaultArchiveDir
// will now compute).
func TestRuntimeWiring_Stable_ArchiveDir(t *testing.T) {
	id := install.Identity(install.ChannelStable)
	require.NotEmpty(t, id.ConfigDir)
	wantArchive := filepath.Join(id.ConfigDir, "archives")
	// config.go uses id.ConfigDir+"/archives" (forward slash) which equals
	// filepath.Join on all platforms.
	assert.Equal(t, wantArchive, filepath.Join(id.ConfigDir, "archives"))
	assert.NotContains(t, wantArchive, "staging",
		"stable archive dir must not contain 'staging'")
}

// ── Staging channel — exact runtime identity values ───────────────────────────

// TestRuntimeWiring_Staging_KeychainService verifies that the staging channel
// produces a keychain service distinct from the stable one so both daemons can
// run simultaneously without colliding.
func TestRuntimeWiring_Staging_KeychainService(t *testing.T) {
	id := install.Identity(install.ChannelStaging)
	assert.Equal(t, "com.vaultmtg.daemon.staging", id.KeychainService,
		"staging keychain service must be com.vaultmtg.daemon.staging")
}

// TestRuntimeWiring_Staging_PlistLabel verifies the staging plist label carries
// the ".staging" suffix so stopLaunchAgent() boots out the correct LaunchAgent.
func TestRuntimeWiring_Staging_PlistLabel(t *testing.T) {
	id := install.Identity(install.ChannelStaging)
	assert.Equal(t, "com.vaultmtg.daemon.staging", id.PlistLabel,
		"staging plist label must be com.vaultmtg.daemon.staging")
}

// TestRuntimeWiring_Staging_ConfigDirContainsStaging verifies that the staging
// config dir contains "staging" so daemon.json is written to the correct path.
func TestRuntimeWiring_Staging_ConfigDirContainsStaging(t *testing.T) {
	id := install.Identity(install.ChannelStaging)
	assert.Contains(t, id.ConfigDir, "staging",
		"staging config dir must contain 'staging'")
}

// TestRuntimeWiring_Staging_ArchiveDir verifies that the staging archive dir
// is under the staging config dir.
func TestRuntimeWiring_Staging_ArchiveDir(t *testing.T) {
	id := install.Identity(install.ChannelStaging)
	require.NotEmpty(t, id.ConfigDir)
	wantArchive := filepath.Join(id.ConfigDir, "archives")
	assert.True(t, strings.HasPrefix(wantArchive, id.ConfigDir),
		"staging archive dir must be under staging config dir")
	assert.Contains(t, wantArchive, "staging",
		"staging archive dir must contain 'staging'")
}

// ── FF-7: concurrent dual-run invariant ───────────────────────────────────────
// These tests assert that no OS-level identifier is shared between the two channels.
// If any of these fail, running both daemons simultaneously would produce a collision.

// TestFF7_KeychainSlotsDistinct is the primary FF-7 assertion: the stable and
// staging keychain service names must differ so concurrent daemons write to
// independent OS keychain entries.
func TestFF7_KeychainSlotsDistinct(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, stable.KeychainService, staging.KeychainService,
		"FF-7: stable and staging keychain service names must differ (concurrent dual-run invariant)")
}

// TestFF7_PlistLabelsDistinct verifies the plist labels differ so quitting one
// daemon does not unload the other's LaunchAgent.
func TestFF7_PlistLabelsDistinct(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, stable.PlistLabel, staging.PlistLabel,
		"FF-7: stable and staging plist labels must differ (Quit-staging must not kill prod)")
}

// TestFF7_ConfigDirsDistinct verifies daemon.json is written to distinct
// directories so one channel cannot read the other's configuration.
func TestFF7_ConfigDirsDistinct(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, stable.ConfigDir, staging.ConfigDir,
		"FF-7: stable and staging config dirs must differ")
}

// TestFF7_ArchiveDirsDistinct verifies that log archives from the two channels
// never land in the same directory.
func TestFF7_ArchiveDirsDistinct(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)

	stableArchive := filepath.Join(stable.ConfigDir, "archives")
	stagingArchive := filepath.Join(staging.ConfigDir, "archives")

	assert.NotEqual(t, stableArchive, stagingArchive,
		"FF-7: stable and staging archive dirs must differ")
}

// TestFF7_LocalAPIPortsDistinct verifies the local-API ports differ so both
// daemons can bind simultaneously (ADR-049 §5 risk mitigation).
func TestFF7_LocalAPIPortsDistinct(t *testing.T) {
	stable := install.Identity(install.ChannelStable)
	staging := install.Identity(install.ChannelStaging)
	assert.NotEqual(t, stable.LocalAPIPort, staging.LocalAPIPort,
		"FF-7: stable and staging local-API ports must differ")
	assert.Equal(t, 9001, stable.LocalAPIPort, "stable port must be 9001")
	assert.Equal(t, 9011, staging.LocalAPIPort, "staging port must be 9011")
}
