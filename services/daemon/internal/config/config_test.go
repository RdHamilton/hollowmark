package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ramonehamilton/mtga-daemon/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("MTGA_DAEMON_CLOUD_API_URL", "http://localhost:8080")
	t.Setenv("MTGA_DAEMON_API_KEY", "my-token")
	t.Setenv("MTGA_DAEMON_ACCOUNT_ID", "acc-999")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", cfg.CloudAPIURL)
	assert.Equal(t, "my-token", cfg.APIKey)
	assert.Equal(t, "acc-999", cfg.AccountID)
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	content := `{"cloud_api_url":"http://bff.example.com","api_key":"file-key","account_id":"acc-file"}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "http://bff.example.com", cfg.CloudAPIURL)
	assert.Equal(t, "file-key", cfg.APIKey)
}

func TestLoadMissingCloudAPIURL(t *testing.T) {
	// Clear env var to ensure validation triggers
	t.Setenv("MTGA_DAEMON_CLOUD_API_URL", "")
	_, err := config.Load("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cloud_api_url")
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	content := `{"cloud_api_url":"http://from-file.example.com"}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	t.Setenv("MTGA_DAEMON_CLOUD_API_URL", "http://from-env.example.com")

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "http://from-env.example.com", cfg.CloudAPIURL)
}

func TestDefaults(t *testing.T) {
	t.Setenv("MTGA_DAEMON_CLOUD_API_URL", "http://localhost:9000")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "/v1/ingest/events", cfg.IngestPath)
	assert.True(t, cfg.UseFSNotify)
}

func TestSyncEnabledDefaultsTrue(t *testing.T) {
	t.Setenv("MTGA_DAEMON_CLOUD_API_URL", "http://localhost:9000")
	t.Setenv("MTGA_DAEMON_API_KEY", "some-key")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.True(t, cfg.SyncEnabled)
}

func TestSyncEnabledWithMissingAPIKeyNoError(t *testing.T) {
	// sync_enabled=true with no api_key should warn but NOT return an error
	t.Setenv("MTGA_DAEMON_CLOUD_API_URL", "http://localhost:9000")
	t.Setenv("MTGA_DAEMON_API_KEY", "")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.True(t, cfg.SyncEnabled)
	assert.Empty(t, cfg.APIKey)
}

func TestDefaultLogPreserveOnStart(t *testing.T) {
	t.Setenv("MTGA_DAEMON_CLOUD_API_URL", "http://localhost:9000")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.True(t, cfg.LogPreserveOnStart)
}

func TestDefaultLogArchiveMaxAge(t *testing.T) {
	t.Setenv("MTGA_DAEMON_CLOUD_API_URL", "http://localhost:9000")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, 7*24*time.Hour, cfg.LogArchiveMaxAge)
}

func TestDefaultLogArchiveDirNonEmpty(t *testing.T) {
	t.Setenv("MTGA_DAEMON_CLOUD_API_URL", "http://localhost:9000")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.NotEmpty(t, cfg.LogArchiveDir)
}

func TestLogArchiveDirEnvOverride(t *testing.T) {
	t.Setenv("MTGA_DAEMON_CLOUD_API_URL", "http://localhost:9000")
	t.Setenv("MTGA_DAEMON_LOG_ARCHIVE_DIR", "/custom/archive/dir")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "/custom/archive/dir", cfg.LogArchiveDir)
}

// TestLoadFromFileAllFields verifies the canonical JSON round-trip for every
// key name documented in config.go.  This is the format written by the install
// scripts on both platforms.
func TestLoadFromFileAllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	content := `{
		"cloud_api_url":         "https://bff.example.com",
		"api_key":               "tok-abc123",
		"sync_enabled":          false,
		"log_path":              "/tmp/Player.log",
		"ingest_path":           "/v1/ingest/events",
		"account_id":            "acc-42",
		"log_archive_dir":       "/tmp/archives",
		"log_preserve_on_start": false
	}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "https://bff.example.com", cfg.CloudAPIURL)
	assert.Equal(t, "tok-abc123", cfg.APIKey)
	assert.False(t, cfg.SyncEnabled)
	assert.Equal(t, "/tmp/Player.log", cfg.LogPath)
	assert.Equal(t, "/v1/ingest/events", cfg.IngestPath)
	assert.Equal(t, "acc-42", cfg.AccountID)
	assert.Equal(t, "/tmp/archives", cfg.LogArchiveDir)
	assert.False(t, cfg.LogPreserveOnStart)
}

// TestOldKeyNamesIgnored confirms that the old installer key names (bff_url,
// daemon_auth_token) written by the pre-fix Windows installer are silently
// ignored by the JSON decoder — they do not satisfy cloud_api_url/api_key.
// The test captures that these stale configs should fail validation so users
// get a clear error rather than a silent misconfiguration.
func TestOldKeyNamesIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	// Simulate a JSON file with the old key names (bff_url, daemon_auth_token).
	// Note: plain YAML (e.g. "bff_url: foo") is not valid JSON, so this tests
	// the JSON-with-wrong-keys scenario.
	content := `{"bff_url":"https://bff.example.com","daemon_auth_token":"tok-old"}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	_, err := config.Load(path)
	// cloud_api_url will be empty because "bff_url" is unknown; validation fails.
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cloud_api_url")
}
