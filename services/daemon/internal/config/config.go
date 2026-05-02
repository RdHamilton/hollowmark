// Package config loads daemon configuration from a local file and environment variables.
// The BFF URL is never hardcoded — it must be supplied via config file or environment.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Config holds all daemon configuration.
type Config struct {
	// BFFURL is the base URL of the Backend for Frontend service.
	// Required. Never hardcoded. Read from config file or MTGA_DAEMON_BFF_URL env var.
	BFFURL string `json:"bff_url"`

	// DaemonJWT is the bearer token used to authenticate with the BFF.
	// Read from config file or MTGA_DAEMON_JWT env var.
	DaemonJWT string `json:"daemon_jwt"`

	// LogPath is the path to the MTGA Player.log file.
	// Optional: auto-detected from the platform if empty.
	LogPath string `json:"log_path"`

	// PollInterval is how often the poller checks for new log entries.
	// Default: 2 seconds.
	PollInterval time.Duration `json:"poll_interval"`

	// UseFSNotify enables fsnotify-based file watching instead of pure polling.
	// Default: true.
	UseFSNotify bool `json:"use_fs_notify"`

	// AccountID is the MTGA account ID used to tag events sent to BFF.
	AccountID string `json:"account_id"`

	// IngestPath is the BFF endpoint path for event ingestion.
	// Default: /v1/ingest/events.
	IngestPath string `json:"ingest_path"`
}

// Load reads daemon configuration. Sources in priority order:
//  1. JSON config file at path (if non-empty)
//  2. Environment variables
//  3. Defaults
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path != "" {
		if err := loadFile(cfg, path); err != nil {
			return nil, fmt.Errorf("load config file %q: %w", path, err)
		}
	}

	applyEnv(cfg)

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

func defaults() *Config {
	return &Config{
		PollInterval: 2 * time.Second,
		UseFSNotify:  true,
		IngestPath:   "/v1/ingest/events",
	}
}

func loadFile(cfg *Config, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := json.NewDecoder(f).Decode(cfg); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("MTGA_DAEMON_BFF_URL"); v != "" {
		cfg.BFFURL = v
	}
	if v := os.Getenv("MTGA_DAEMON_JWT"); v != "" {
		cfg.DaemonJWT = v
	}
	if v := os.Getenv("MTGA_DAEMON_LOG_PATH"); v != "" {
		cfg.LogPath = v
	}
	if v := os.Getenv("MTGA_DAEMON_ACCOUNT_ID"); v != "" {
		cfg.AccountID = v
	}
}

func (c *Config) validate() error {
	if c.BFFURL == "" {
		return fmt.Errorf("bff_url is required (set MTGA_DAEMON_BFF_URL or provide config file)")
	}
	return nil
}
