package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/RdHamilton/vault-mtg/services/bff/internal/config"
	contract "github.com/RdHamilton/vault-mtg/services/contract"
)

// VersionConfig is the minimal interface DaemonVersionHandler needs from the BFF
// config. Used as a fallback when the live fetcher is unavailable or errors.
type VersionConfig interface {
	GetDaemonLatestVersion() string
	GetDaemonReleasedAt() string
}

// configAdapter adapts *config.Config to the VersionConfig interface.
type configAdapter struct {
	cfg *config.Config
}

func (a *configAdapter) GetDaemonLatestVersion() string { return a.cfg.DaemonLatestVersion }
func (a *configAdapter) GetDaemonReleasedAt() string    { return a.cfg.DaemonReleasedAt }

// DaemonVersionHandler serves GET /api/v1/daemon/version.
// No authentication is required — version metadata is public.
type DaemonVersionHandler struct {
	vcfg    VersionConfig
	fetcher *ReleaseFetcher
}

// NewDaemonVersionHandler constructs a DaemonVersionHandler using the BFF config
// as a fallback source. The live GitHub Releases fetcher is wired in separately
// via WithFetcher so main.go and tests can each supply their own.
func NewDaemonVersionHandler(cfg *config.Config) *DaemonVersionHandler {
	if cfg == nil {
		return &DaemonVersionHandler{}
	}
	return &DaemonVersionHandler{vcfg: &configAdapter{cfg: cfg}}
}

// NewDaemonVersionHandlerWithCfg constructs a DaemonVersionHandler with an
// arbitrary VersionConfig implementation (used in tests).
func NewDaemonVersionHandlerWithCfg(vcfg VersionConfig) *DaemonVersionHandler {
	return &DaemonVersionHandler{vcfg: vcfg}
}

// WithFetcher attaches a live GitHub Releases fetcher. When set, the fetcher is
// the primary source; the static config is used as a fallback only.
func (h *DaemonVersionHandler) WithFetcher(f *ReleaseFetcher) {
	h.fetcher = f
}

// GetDaemonVersion handles GET /api/v1/daemon/version.
// Returns the latest published daemon version. When a ReleaseFetcher is wired in,
// it calls the GitHub Releases API (with a 5-minute cache). On fetcher error or
// when no fetcher is configured, it falls back to the static BFF config.
func (h *DaemonVersionHandler) GetDaemonVersion(w http.ResponseWriter, r *http.Request) {
	var resp contract.DaemonVersionResponse

	if h.fetcher != nil {
		if result, err := h.fetcher.LatestDaemonRelease(); err == nil && result.Latest != "" {
			resp = *result
		} else if err != nil {
			log.Printf("[daemon-version] fetcher error (falling back to config): %v", err)
		}
	}

	// Fallback: use static config when fetcher is absent, errored, or returned empty.
	if resp.Latest == "" && h.vcfg != nil {
		version := h.vcfg.GetDaemonLatestVersion()
		releasedAt := h.vcfg.GetDaemonReleasedAt()
		resp = contract.DaemonVersionResponse{
			Latest:      version,
			ReleasedAt:  releasedAt,
			DownloadURL: "https://github.com/RdHamilton/vault-mtg/releases/tag/daemon/v" + version,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// Response headers already sent; log only.
		_ = err
	}
}
