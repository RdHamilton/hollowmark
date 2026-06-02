package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/bff/internal/api/handlers"
	contract "github.com/RdHamilton/vault-mtg/services/contract"
)

// githubRelease mirrors the fields from the GitHub Releases API used by
// the fetcher. Tests use this to build stub responses.
type githubRelease struct {
	TagName     string    `json:"tag_name"`
	PublishedAt time.Time `json:"published_at"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	Assets      []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

// makeGitHubReleasesServer returns a test server that serves a list of
// releases and records request count for cache verification.
func makeGitHubReleasesServer(t *testing.T, releases []githubRelease) (srv *httptest.Server, requestCount func() int) {
	t.Helper()
	var count int
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(releases)
	}))
	t.Cleanup(srv.Close)
	return srv, func() int { return count }
}

func TestReleaseFetcher_ParsesDaemonTag(t *testing.T) {
	releases := []githubRelease{
		{
			TagName:     "daemon/v0.3.7",
			PublishedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			HTMLURL:     "https://github.com/RdHamilton/vault-mtg/releases/tag/daemon%2Fv0.3.7",
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			}{
				{Name: "SHA256SUMS", BrowserDownloadURL: "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/SHA256SUMS"},
				{Name: "SHA256SUMS.minisig", BrowserDownloadURL: "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/SHA256SUMS.minisig"},
			},
		},
		// app release should be ignored
		{TagName: "app/v0.3.7", PublishedAt: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)},
		// prerelease should be skipped
		{TagName: "daemon/v0.3.8-rc1", Prerelease: true},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, nil)

	result, err := f.LatestDaemonRelease()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Latest != "0.3.7" {
		t.Errorf("latest: got %q, want %q", result.Latest, "0.3.7")
	}
	if result.ReleasedAt != "2026-06-01T12:00:00Z" {
		t.Errorf("released_at: got %q, want %q", result.ReleasedAt, "2026-06-01T12:00:00Z")
	}
	if result.DownloadURL != "https://github.com/RdHamilton/vault-mtg/releases/tag/daemon%2Fv0.3.7" {
		t.Errorf("download_url: got %q", result.DownloadURL)
	}
	if result.Sha256SumsURL != "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/SHA256SUMS" {
		t.Errorf("sha256sums_url: got %q", result.Sha256SumsURL)
	}
	if result.AttestationURL != "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/SHA256SUMS.minisig" {
		t.Errorf("attestation_url: got %q", result.AttestationURL)
	}
}

func TestReleaseFetcher_SkipsPrereleasesAndDrafts(t *testing.T) {
	releases := []githubRelease{
		{TagName: "daemon/v0.3.8-rc1", Prerelease: true, PublishedAt: time.Now()},
		{TagName: "daemon/v0.3.8", Draft: true, PublishedAt: time.Now()},
		{TagName: "daemon/v0.3.7", PublishedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, nil)

	result, err := f.LatestDaemonRelease()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Latest != "0.3.7" {
		t.Errorf("latest: got %q, want %q (prerelease and draft should be skipped)", result.Latest, "0.3.7")
	}
}

func TestReleaseFetcher_CachePreventsDuplicateRequests(t *testing.T) {
	releases := []githubRelease{
		{TagName: "daemon/v0.3.7", PublishedAt: time.Now()},
	}

	srv, reqCount := makeGitHubReleasesServer(t, releases)
	ttl := 5 * time.Minute
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", ttl, nil)

	// First call — should hit the server.
	if _, err := f.LatestDaemonRelease(); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Second call — should use the cache.
	if _, err := f.LatestDaemonRelease(); err != nil {
		t.Fatalf("second call: %v", err)
	}

	if reqCount() != 1 {
		t.Errorf("expected 1 request to GitHub API (cache hit), got %d", reqCount())
	}
}

func TestReleaseFetcher_CacheExpiresAfterTTL(t *testing.T) {
	releases := []githubRelease{
		{TagName: "daemon/v0.3.7", PublishedAt: time.Now()},
	}

	srv, reqCount := makeGitHubReleasesServer(t, releases)
	// Very short TTL so we can test expiry without sleeping long.
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 50*time.Millisecond, nil)

	if _, err := f.LatestDaemonRelease(); err != nil {
		t.Fatalf("first call: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if _, err := f.LatestDaemonRelease(); err != nil {
		t.Fatalf("second call after expiry: %v", err)
	}

	if reqCount() != 2 {
		t.Errorf("expected 2 requests (cache expired), got %d", reqCount())
	}
}

func TestReleaseFetcher_NoDaemonReleaseFound(t *testing.T) {
	// All releases are app/ releases, no daemon/ releases.
	releases := []githubRelease{
		{TagName: "app/v0.3.7", PublishedAt: time.Now()},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, nil)

	result, err := f.LatestDaemonRelease()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// When no daemon release exists, return an empty response rather than an error.
	if result.Latest != "" {
		t.Errorf("expected empty latest, got %q", result.Latest)
	}
}

func TestReleaseFetcher_NetworkError_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close without writing anything.
		panic(http.ErrAbortHandler)
	}))
	srv.Close() // Already closed — any request fails.

	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, nil)

	_, err := f.LatestDaemonRelease()
	if err == nil {
		t.Error("expected error from closed server, got nil")
	}
}

func TestReleaseFetcher_Non200_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, nil)

	_, err := f.LatestDaemonRelease()
	if err == nil {
		t.Error("expected error for 403 response, got nil")
	}
}

// --- Integration test for the handler using the fetcher ---

// TestGetDaemonVersion_LiveFetch verifies the handler returns version data from the
// live fetcher rather than from the static BFF config. This is the integration test
// required by #635 AC1: the handler must reflect the latest published daemon version.
func TestGetDaemonVersion_LiveFetch(t *testing.T) {
	releases := []githubRelease{
		{
			TagName:     "daemon/v0.3.7",
			PublishedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			HTMLURL:     "https://github.com/RdHamilton/vault-mtg/releases/tag/daemon%2Fv0.3.7",
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			}{
				{Name: "SHA256SUMS", BrowserDownloadURL: "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/SHA256SUMS"},
				{Name: "SHA256SUMS.minisig", BrowserDownloadURL: "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/SHA256SUMS.minisig"},
			},
		},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, nil)
	h := handlers.NewDaemonVersionHandler(nil) // nil cfg — fetcher takes precedence
	h.WithFetcher(f)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemon/version", nil)
	rec := httptest.NewRecorder()
	h.GetDaemonVersion(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want %d", res.StatusCode, http.StatusOK)
	}

	var resp contract.DaemonVersionResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if resp.Latest != "0.3.7" {
		t.Errorf("latest: got %q, want %q", resp.Latest, "0.3.7")
	}
	if resp.Sha256SumsURL == "" {
		t.Error("sha256sums_url must not be empty")
	}
	if resp.AttestationURL == "" {
		t.Error("attestation_url must not be empty")
	}
}

// TestGetDaemonVersion_FetcherFallsBackToConfig verifies that when the fetcher
// returns an error the handler falls back to the static config. This ensures the
// BFF remains operational even when GitHub is unreachable.
func TestGetDaemonVersion_FetcherFallsBackToConfig(t *testing.T) {
	// Fetcher pointing at a closed server — will always error.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, nil)

	// Config has a fallback version.
	cfg := &testCfg{version: "0.3.5", releasedAt: "2026-05-01T00:00:00Z"}
	h := handlers.NewDaemonVersionHandlerWithCfg(cfg)
	h.WithFetcher(f)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemon/version", nil)
	rec := httptest.NewRecorder()
	h.GetDaemonVersion(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}

	var resp contract.DaemonVersionResponse
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Should fall back to the config value.
	if resp.Latest != "0.3.5" {
		t.Errorf("fallback latest: got %q, want %q", resp.Latest, "0.3.5")
	}
}

// testCfg satisfies the VersionConfig interface for tests.
type testCfg struct {
	version    string
	releasedAt string
}

func (c *testCfg) GetDaemonLatestVersion() string { return c.version }
func (c *testCfg) GetDaemonReleasedAt() string    { return c.releasedAt }
