package tray

import (
	"testing"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/install"
	"github.com/stretchr/testify/assert"
)

// TestNewWithLabel_StableDefaultLabel verifies that NewWithLabel using the
// stable channel produces the bare "Hollowmark" label (no suffix).
func TestNewWithLabel_StableDefaultLabel(t *testing.T) {
	id := install.Identity(install.ChannelStable)
	a := NewWithLabel("https://app.hollowmark.app", "dev",
		func(string) error { return nil }, func() {}, id.TrayLabel)
	assert.Equal(t, "Hollowmark", a.appLabel)
}

// TestNewWithLabel_StagingLabel verifies that NewWithLabel using the
// staging channel produces "Hollowmark (Staging)".
func TestNewWithLabel_StagingLabel(t *testing.T) {
	id := install.Identity(install.ChannelStaging)
	a := NewWithLabel("https://stg-app.vaultmtg.app", "dev",
		func(string) error { return nil }, func() {}, id.TrayLabel)
	assert.Equal(t, "Hollowmark (Staging)", a.appLabel)
}

// TestNewWithLabel_LabelDistinct verifies the two channels produce different labels.
func TestNewWithLabel_LabelDistinct(t *testing.T) {
	stableID := install.Identity(install.ChannelStable)
	stagingID := install.Identity(install.ChannelStaging)
	stableApp := NewWithLabel("", "dev", nil, nil, stableID.TrayLabel)
	stagingApp := NewWithLabel("", "dev", nil, nil, stagingID.TrayLabel)
	assert.NotEqual(t, stableApp.appLabel, stagingApp.appLabel)
}
