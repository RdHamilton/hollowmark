package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ramonehamilton/mtga-daemon/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("MTGA_DAEMON_BFF_URL", "http://localhost:8080")
	t.Setenv("MTGA_DAEMON_JWT", "my-token")
	t.Setenv("MTGA_DAEMON_ACCOUNT_ID", "acc-999")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:8080", cfg.BFFURL)
	assert.Equal(t, "my-token", cfg.DaemonJWT)
	assert.Equal(t, "acc-999", cfg.AccountID)
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	content := `{"bff_url":"http://bff.example.com","daemon_jwt":"file-jwt","account_id":"acc-file"}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "http://bff.example.com", cfg.BFFURL)
	assert.Equal(t, "file-jwt", cfg.DaemonJWT)
}

func TestLoadMissingBFFURL(t *testing.T) {
	// Clear env var to ensure validation triggers
	t.Setenv("MTGA_DAEMON_BFF_URL", "")
	_, err := config.Load("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bff_url")
}

func TestEnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	content := `{"bff_url":"http://from-file.example.com"}`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	t.Setenv("MTGA_DAEMON_BFF_URL", "http://from-env.example.com")

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, "http://from-env.example.com", cfg.BFFURL)
}

func TestDefaults(t *testing.T) {
	t.Setenv("MTGA_DAEMON_BFF_URL", "http://localhost:9000")

	cfg, err := config.Load("")
	require.NoError(t, err)
	assert.Equal(t, "/v1/ingest/events", cfg.IngestPath)
	assert.True(t, cfg.UseFSNotify)
}
