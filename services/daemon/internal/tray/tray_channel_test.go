package tray

import (
	"testing"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/install"
	"github.com/stretchr/testify/assert"
)

// TestNewWithLabel_StableDefaultLabel verifies that NewWithLabel using the
// stable channel produces the bare "VaultMTG" label (no suffix).
// This ensures existing stable installs see no change after ADR-049.
func TestNewWithLabel_StableDefaultLabel(t *testing.T) {
	id := install.Identity(install.ChannelStable)
	a := NewWithLabel("https://app.vaultmtg.app", "dev",
		func(string) error { return nil }, func() {}, id.TrayLabel)
	assert.Equal(t, "VaultMTG", a.appLabel)
}

// TestNewWithLabel_StagingLabel verifies that NewWithLabel using the
// staging channel produces "VaultMTG (Staging)".
func TestNewWithLabel_StagingLabel(t *testing.T) {
	id := install.Identity(install.ChannelStaging)
	a := NewWithLabel("https://stg-app.vaultmtg.app", "dev",
		func(string) error { return nil }, func() {}, id.TrayLabel)
	assert.Equal(t, "VaultMTG (Staging)", a.appLabel)
}

// TestNewWithLabel_LabelDistinct verifies the two channels produce different labels.
func TestNewWithLabel_LabelDistinct(t *testing.T) {
	stableID := install.Identity(install.ChannelStable)
	stagingID := install.Identity(install.ChannelStaging)
	stableApp := NewWithLabel("", "dev", nil, nil, stableID.TrayLabel)
	stagingApp := NewWithLabel("", "dev", nil, nil, stagingID.TrayLabel)
	assert.NotEqual(t, stableApp.appLabel, stagingApp.appLabel)
}
