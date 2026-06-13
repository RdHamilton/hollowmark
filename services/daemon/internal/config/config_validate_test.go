package config_test

// config_validate_test.go — TDD tests for Fix B (#1327): validate() behaviour
// in keychain mode — hard-fail on empty token, suppress spurious warning.
//
// Tests written (and verified to FAIL) before implementation — per TDD protocol.

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidate_KeychainModeNoAPIKeyOrJWT_DoesNotWarnUnauthenticated verifies
// that when keychain=true and api_key/daemon_jwt are both empty (the expected
// state), validate() does NOT emit the "events will be sent without authentication"
// warning. The keychain token is the authentication mechanism; the absence of
// plaintext fields is correct, not a warning condition (AC5 of #1326).
func TestValidate_KeychainModeNoAPIKeyOrJWT_DoesNotWarnUnauthenticated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	// keychain=true, sync_enabled=true, api_key absent, daemon_jwt absent.
	content := `{"cloud_api_url":"http://bff.example.com","keychain":true,"sync_enabled":true,"account_id":"acc-123"}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	// Capture log output to assert no spurious warning.
	var buf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(origOutput) })

	cfg, err := config.Load(path)
	require.NoError(t, err, "keychain mode with no api_key/daemon_jwt must load without error")
	assert.True(t, cfg.Keychain)
	assert.Empty(t, cfg.APIKey)
	assert.Empty(t, cfg.DaemonJWT)

	// The spurious "events will be sent without authentication" warning must NOT appear.
	assert.NotContains(t, buf.String(), "without authentication",
		"keychain mode must not emit the spurious unauthenticated warning (AC5)")
}

// TestValidate_SyncEnabledNoKeychain_WarnsUnauthenticated verifies that the
// existing warning IS emitted for non-keychain mode with sync_enabled=true and
// no api_key or daemon_jwt — so we don't accidentally suppress the legitimate
// warning for the non-keychain case.
func TestValidate_SyncEnabledNoKeychain_WarnsUnauthenticated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	// keychain=false, sync_enabled=true, api_key absent — this IS a misconfiguration.
	content := `{"cloud_api_url":"http://bff.example.com","sync_enabled":true}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	var buf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(origOutput) })

	_, err := config.Load(path)
	require.NoError(t, err)

	// The warning MUST appear for non-keychain misconfigured mode.
	assert.Contains(t, buf.String(), "without authentication",
		"non-keychain sync_enabled with no credentials must warn about unauthenticated dispatch")
}
