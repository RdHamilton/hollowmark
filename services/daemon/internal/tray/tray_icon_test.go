//go:build cgo

package tray

import (
	"testing"

	"github.com/RdHamilton/vault-mtg/services/daemon/internal/install"
	"github.com/stretchr/testify/assert"
)

// TestIconBytes_StagingChannel verifies that iconBytes returns stagingIconData
// when install.Channel is set to "staging" (ADR-049 §1, vmt-t#657).
func TestIconBytes_StagingChannel(t *testing.T) {
	orig := install.Channel
	t.Cleanup(func() { install.Channel = orig })

	install.Channel = install.ChannelStaging
	assert.Equal(t, stagingIconData, iconBytes(), "staging channel must return stagingIconData")
}

// TestIconBytes_StableChannel verifies that iconBytes returns prodIconData when
// install.Channel is set to "stable" (the default production channel).
func TestIconBytes_StableChannel(t *testing.T) {
	orig := install.Channel
	t.Cleanup(func() { install.Channel = orig })

	install.Channel = install.ChannelStable
	assert.Equal(t, prodIconData, iconBytes(), "stable channel must return prodIconData")
}

// TestIconBytes_DefaultChannel verifies that iconBytes returns prodIconData
// when install.Channel holds its zero/default value ("stable"), confirming
// the fail-safe direction (unknown = prod icon, not staging).
func TestIconBytes_DefaultChannel(t *testing.T) {
	orig := install.Channel
	t.Cleanup(func() { install.Channel = orig })

	install.Channel = install.ChannelStable
	assert.NotEqual(t, stagingIconData, iconBytes(), "default channel must not return stagingIconData")
}

// TestIconBytes_DataNonEmpty verifies that both embedded icon byte slices are
// non-empty so a botched embed directive (missing asset file) is caught at test
// time rather than at runtime when the tray first renders.
func TestIconBytes_DataNonEmpty(t *testing.T) {
	assert.NotEmpty(t, prodIconData, "prodIconData must be non-empty (assets/icon.png must be embedded)")
	assert.NotEmpty(t, stagingIconData, "stagingIconData must be non-empty (assets/staging_icon.png must be embedded)")
}

// TestIconBytes_DistinctData verifies that the two embedded icons are not
// identical — a staging badge that is visually the same as the prod icon would
// defeat the purpose of the channel-conditional embed.
func TestIconBytes_DistinctData(t *testing.T) {
	assert.NotEqual(t, prodIconData, stagingIconData,
		"prod and staging icon data must differ — staging_icon.png must be a distinct asset")
}
