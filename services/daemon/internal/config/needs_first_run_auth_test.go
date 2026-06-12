package config_test

import (
	"testing"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/config"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/credstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNeedsFirstRunAuth_KeychainPresent_ReturnsFalse verifies that when
// cfg.Keychain=true and the credential getter returns a non-empty key,
// NeedsFirstRunAuth returns (false, nil).
func TestNeedsFirstRunAuth_KeychainPresent_ReturnsFalse(t *testing.T) {
	cfg := cfgWithKeychain()
	needs, err := cfg.NeedsFirstRunAuth(func() (string, error) { return "my-api-key", nil })
	require.NoError(t, err)
	assert.False(t, needs)
}

// TestNeedsFirstRunAuth_KeychainNotFound_ReturnsTrue verifies that when
// cfg.Keychain=true and the getter returns ErrNotFound, NeedsFirstRunAuth
// returns (true, nil) — this is genuine first-run, PKCE needed.
func TestNeedsFirstRunAuth_KeychainNotFound_ReturnsTrue(t *testing.T) {
	cfg := cfgWithKeychain()
	needs, err := cfg.NeedsFirstRunAuth(func() (string, error) { return "", credstore.ErrNotFound })
	require.NoError(t, err, "ErrNotFound must not bubble as an error — it means first-run")
	assert.True(t, needs, "ErrNotFound must signal first-run PKCE needed")
}

// TestNeedsFirstRunAuth_ErrAccessDenied_ReturnsError verifies that when
// cfg.Keychain=true and the getter returns ErrAccessDenied, NeedsFirstRunAuth
// returns (false, ErrAccessDenied) — this MUST NOT be treated as first-run.
// This is the class of bug Ray's R1 is fixing: access-denied collapsed into
// "first run", causing PKCE + new device registration on every boot.
func TestNeedsFirstRunAuth_ErrAccessDenied_ReturnsError(t *testing.T) {
	cfg := cfgWithKeychain()
	needs, err := cfg.NeedsFirstRunAuth(func() (string, error) { return "", credstore.ErrAccessDenied })
	require.ErrorIs(t, err, credstore.ErrAccessDenied,
		"ErrAccessDenied must propagate as an error, not collapse into first-run")
	assert.False(t, needs,
		"ErrAccessDenied must NOT set needs=true (must not trigger PKCE re-register)")
}

// TestNeedsFirstRunAuth_UnexpectedError_ReturnsError verifies that any
// unexpected (non-sentinel) credential getter error also propagates as an
// error rather than collapsing into first-run.
func TestNeedsFirstRunAuth_UnexpectedError_ReturnsError(t *testing.T) {
	cfg := cfgWithKeychain()
	needs, err := cfg.NeedsFirstRunAuth(func() (string, error) {
		return "", &unexpectedCredErr{msg: "unexpected OS error"}
	})
	require.Error(t, err, "unexpected getter error must propagate")
	assert.False(t, needs, "unexpected error must not trigger PKCE")
}

// TestNeedsFirstRunAuth_EmptyKey_ReturnsTrue verifies that when cfg.Keychain=true
// and the getter returns a non-error empty string, NeedsFirstRunAuth returns
// (true, nil) — empty key is treated as missing.
func TestNeedsFirstRunAuth_EmptyKey_ReturnsTrue(t *testing.T) {
	cfg := cfgWithKeychain()
	needs, err := cfg.NeedsFirstRunAuth(func() (string, error) { return "", nil })
	require.NoError(t, err)
	assert.True(t, needs, "empty key (no error) must trigger first-run")
}

// TestNeedsFirstRunAuth_NonKeychain_HasAPIKey_ReturnsFalse verifies that a
// non-keychain config with a plaintext APIKey does not need first-run auth.
func TestNeedsFirstRunAuth_NonKeychain_HasAPIKey_ReturnsFalse(t *testing.T) {
	cfg := &config.Config{
		CloudAPIURL: "http://localhost",
		APIKey:      "plaintext-key",
	}
	needs, err := cfg.NeedsFirstRunAuth(nil)
	require.NoError(t, err)
	assert.False(t, needs)
}

// TestNeedsFirstRunAuth_NonKeychain_NoKey_ReturnsTrue verifies that a
// non-keychain config with no key needs first-run auth.
func TestNeedsFirstRunAuth_NonKeychain_NoKey_ReturnsTrue(t *testing.T) {
	cfg := &config.Config{
		CloudAPIURL: "http://localhost",
	}
	needs, err := cfg.NeedsFirstRunAuth(nil)
	require.NoError(t, err)
	assert.True(t, needs)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func cfgWithKeychain() *config.Config {
	return &config.Config{
		CloudAPIURL: "http://localhost",
		Keychain:    true,
	}
}

type unexpectedCredErr struct{ msg string }

func (e *unexpectedCredErr) Error() string { return e.msg }
