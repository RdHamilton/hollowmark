package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	contract "github.com/RdHamilton/hollowmark/services/contract"
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
			HTMLURL:     "https://github.com/RdHamilton/hollowmark/releases/tag/daemon%2Fv0.3.7",
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			}{
				{Name: "SHA256SUMS", BrowserDownloadURL: "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/SHA256SUMS"},
				{Name: "SHA256SUMS.minisig", BrowserDownloadURL: "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/SHA256SUMS.minisig"},
			},
		},
		// app release should be ignored
		{TagName: "app/v0.3.7", PublishedAt: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)},
		// prerelease should be skipped
		{TagName: "daemon/v0.3.8-rc1", Prerelease: true},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

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
	if result.DownloadURL != "https://github.com/RdHamilton/hollowmark/releases/tag/daemon%2Fv0.3.7" {
		t.Errorf("download_url: got %q", result.DownloadURL)
	}
	if result.Sha256SumsURL != "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/SHA256SUMS" {
		t.Errorf("sha256sums_url: got %q", result.Sha256SumsURL)
	}
	if result.AttestationURL != "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/SHA256SUMS.minisig" {
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
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

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
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", ttl, "", nil)

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
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 50*time.Millisecond, "", nil)

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
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

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

	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

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

	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

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
			HTMLURL:     "https://github.com/RdHamilton/hollowmark/releases/tag/daemon%2Fv0.3.7",
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			}{
				{Name: "SHA256SUMS", BrowserDownloadURL: "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/SHA256SUMS"},
				{Name: "SHA256SUMS.minisig", BrowserDownloadURL: "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/SHA256SUMS.minisig"},
			},
		},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)
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
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

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

// TestReleaseFetcher_PerPlatformAssetURLs verifies that parseDaemonRelease
// populates MacOSInstallerURL and WindowsInstallerURL from the GitHub release
// assets, allowing the daemon to select the correct binary by platform.
func TestReleaseFetcher_PerPlatformAssetURLs(t *testing.T) {
	releases := []githubRelease{
		{
			TagName:     "daemon/v0.3.7",
			PublishedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			HTMLURL:     "https://github.com/RdHamilton/hollowmark/releases/tag/daemon%2Fv0.3.7",
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			}{
				{Name: "SHA256SUMS", BrowserDownloadURL: "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/SHA256SUMS"},
				{Name: "SHA256SUMS.minisig", BrowserDownloadURL: "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/SHA256SUMS.minisig"},
				{Name: "vaultmtg-daemon-darwin-universal.pkg", BrowserDownloadURL: "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-darwin-universal.pkg"},
				{Name: "vaultmtg-daemon-windows-amd64.exe", BrowserDownloadURL: "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-windows-amd64.exe"},
			},
		},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

	result, err := f.LatestDaemonRelease()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-darwin-universal.pkg"
	if result.MacOSInstallerURL != want {
		t.Errorf("macos_installer_url: got %q, want %q", result.MacOSInstallerURL, want)
	}
	want = "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-windows-amd64.exe"
	if result.WindowsInstallerURL != want {
		t.Errorf("windows_installer_url: got %q, want %q", result.WindowsInstallerURL, want)
	}
}

// TestReleaseFetcher_HostValidation_RejectsOffListURL verifies that assets whose
// BrowserDownloadURL has a host not in {github.com, objects.githubusercontent.com}
// are omitted from the response rather than propagated to the daemon.
func TestReleaseFetcher_HostValidation_RejectsOffListURL(t *testing.T) {
	releases := []githubRelease{
		{
			TagName:     "daemon/v0.3.7",
			PublishedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			HTMLURL:     "https://github.com/RdHamilton/hollowmark/releases/tag/daemon%2Fv0.3.7",
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			}{
				// Evil host injected into SHA256SUMS asset
				{Name: "SHA256SUMS", BrowserDownloadURL: "https://evil.example.com/SHA256SUMS"},
				// Valid minisig
				{Name: "SHA256SUMS.minisig", BrowserDownloadURL: "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/SHA256SUMS.minisig"},
				// Evil host injected into macOS asset
				{Name: "vaultmtg-daemon-darwin-universal.pkg", BrowserDownloadURL: "https://evil.example.com/vaultmtg-daemon-darwin-universal.pkg"},
				// Valid Windows asset
				{Name: "vaultmtg-daemon-windows-amd64.exe", BrowserDownloadURL: "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-windows-amd64.exe"},
			},
		},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

	result, err := f.LatestDaemonRelease()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Off-list hosts must be rejected (empty string).
	if result.Sha256SumsURL != "" {
		t.Errorf("sha256sums_url: expected empty (off-list host rejected), got %q", result.Sha256SumsURL)
	}
	if result.MacOSInstallerURL != "" {
		t.Errorf("macos_installer_url: expected empty (off-list host rejected), got %q", result.MacOSInstallerURL)
	}
	// Valid host must pass through.
	if result.AttestationURL == "" {
		t.Errorf("attestation_url: expected non-empty (valid host), got empty")
	}
	if result.WindowsInstallerURL == "" {
		t.Errorf("windows_installer_url: expected non-empty (valid host), got empty")
	}
}

// TestReleaseFetcher_HostValidation_AllowsObjectsGithubusercontentHost verifies that
// the CDN redirect host (objects.githubusercontent.com) is accepted in addition to
// github.com. GitHub CDN assets often use this host.
func TestReleaseFetcher_HostValidation_AllowsObjectsGithubusercontentHost(t *testing.T) {
	releases := []githubRelease{
		{
			TagName:     "daemon/v0.3.7",
			PublishedAt: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
			HTMLURL:     "https://github.com/RdHamilton/hollowmark/releases/tag/daemon%2Fv0.3.7",
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
			}{
				{Name: "SHA256SUMS", BrowserDownloadURL: "https://objects.githubusercontent.com/github-production-release-asset/SHA256SUMS"},
				{Name: "SHA256SUMS.minisig", BrowserDownloadURL: "https://objects.githubusercontent.com/github-production-release-asset/SHA256SUMS.minisig"},
				{Name: "vaultmtg-daemon-darwin-universal.pkg", BrowserDownloadURL: "https://objects.githubusercontent.com/github-production-release-asset/vaultmtg-daemon-darwin-universal.pkg"},
				{Name: "vaultmtg-daemon-windows-amd64.exe", BrowserDownloadURL: "https://objects.githubusercontent.com/github-production-release-asset/vaultmtg-daemon-windows-amd64.exe"},
			},
		},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

	result, err := f.LatestDaemonRelease()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Sha256SumsURL == "" {
		t.Error("sha256sums_url: expected non-empty (objects.githubusercontent.com allowed)")
	}
	if result.AttestationURL == "" {
		t.Error("attestation_url: expected non-empty (objects.githubusercontent.com allowed)")
	}
	if result.MacOSInstallerURL == "" {
		t.Error("macos_installer_url: expected non-empty (objects.githubusercontent.com allowed)")
	}
	if result.WindowsInstallerURL == "" {
		t.Error("windows_installer_url: expected non-empty (objects.githubusercontent.com allowed)")
	}
}

// --- F2: semver-max selection (not first-by-created_at) ---

// TestReleaseFetcher_PicksSemverMax_NotFirstByCreatedAt is the F2 regression
// guard. GitHub returns releases in created_at-desc order, which USUALLY tracks
// semver but not always: a re-cut / hotfix can create a LOWER semver with a
// LATER created_at (exactly what the GoReleaser-free tag-rename dance and the
// 0.3.x.N hotfix waves produce). The fetcher must pick the semver-GREATEST
// daemon release, not the first one in API order.
//
// Ordering here: daemon/v0.3.5 has the LATEST published_at (appears first, as
// GitHub would return it after a re-cut), but daemon/v0.3.6 is the higher
// semver and must win.
func TestReleaseFetcher_PicksSemverMax_NotFirstByCreatedAt(t *testing.T) {
	releases := []githubRelease{
		// Re-cut 0.3.5 with a LATER created_at — first in API order, lower semver.
		{TagName: "daemon/v0.3.5", PublishedAt: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)},
		// The real latest, published earlier — must still win on semver.
		{TagName: "daemon/v0.3.6", PublishedAt: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)},
		// An even older, lower release.
		{TagName: "daemon/v0.3.4", PublishedAt: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

	result, err := f.LatestDaemonRelease()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Latest != "0.3.6" {
		t.Errorf("latest: got %q, want %q (must pick semver-max, not first-by-created_at)", result.Latest, "0.3.6")
	}
	// The released_at must correspond to the SELECTED release, not the first one.
	if result.ReleasedAt != "2026-06-01T09:00:00Z" {
		t.Errorf("released_at: got %q, want %q (must match the semver-max release)", result.ReleasedAt, "2026-06-01T09:00:00Z")
	}
}

// TestReleaseFetcher_PicksSemverMax_PatchAndMinor proves correct ordering across
// patch and minor boundaries that string comparison would get wrong
// (e.g. "0.3.10" > "0.3.9", "0.10.0" > "0.9.0").
func TestReleaseFetcher_PicksSemverMax_PatchAndMinor(t *testing.T) {
	releases := []githubRelease{
		{TagName: "daemon/v0.3.9", PublishedAt: time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)},
		{TagName: "daemon/v0.3.10", PublishedAt: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)},
		{TagName: "daemon/v0.10.0", PublishedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
		{TagName: "daemon/v0.9.0", PublishedAt: time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

	result, err := f.LatestDaemonRelease()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Latest != "0.10.0" {
		t.Errorf("latest: got %q, want %q (0.10.0 must beat 0.3.10/0.9.0 numerically)", result.Latest, "0.10.0")
	}
}

// TestReleaseFetcher_PicksSemverMax_FourSegmentHotfix proves the 4-segment
// hotfix wave format (e.g. 0.3.6.1, per the "4th segment = hotfix wave"
// convention) is parsed and ordered correctly — a hotfix 0.3.6.1 must beat the
// base 0.3.6 even when GitHub returns the base release later. This format is NOT
// valid strict semver, so a stdlib semver lib would silently drop it; the parser
// must handle it.
func TestReleaseFetcher_PicksSemverMax_FourSegmentHotfix(t *testing.T) {
	releases := []githubRelease{
		// Base 0.3.6 re-listed with a later created_at.
		{TagName: "daemon/v0.3.6", PublishedAt: time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)},
		// Hotfix 0.3.6.1 — must win even though it was published earlier.
		{TagName: "daemon/v0.3.6.1", PublishedAt: time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)},
	}

	srv, _ := makeGitHubReleasesServer(t, releases)
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)

	result, err := f.LatestDaemonRelease()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Latest != "0.3.6.1" {
		t.Errorf("latest: got %q, want %q (4-segment hotfix must beat the base release)", result.Latest, "0.3.6.1")
	}
}

// --- F3: authenticated fetch + stale-OK fallback ---

// TestReleaseFetcher_SendsAuthorizationHeader verifies that when a GitHub token
// is configured, the fetch sends "Authorization: Bearer <token>" so the request
// uses the 5000 req/hr authenticated rate limit instead of the 60 req/hr
// unauthenticated limit.
func TestReleaseFetcher_SendsAuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]githubRelease{
			{TagName: "daemon/v0.3.7", PublishedAt: time.Now()},
		})
	}))
	t.Cleanup(srv.Close)

	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "ghp_testtoken123", nil)
	if _, err := f.LatestDaemonRelease(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotAuth != "Bearer ghp_testtoken123" {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, "Bearer ghp_testtoken123")
	}
}

// TestReleaseFetcher_NoAuthHeaderWhenTokenEmpty verifies that with no token
// configured the fetch sends NO Authorization header (anonymous request) rather
// than an empty/malformed one.
func TestReleaseFetcher_NoAuthHeaderWhenTokenEmpty(t *testing.T) {
	var hadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadAuth = r.Header["Authorization"]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]githubRelease{
			{TagName: "daemon/v0.3.7", PublishedAt: time.Now()},
		})
	}))
	t.Cleanup(srv.Close)

	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)
	if _, err := f.LatestDaemonRelease(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hadAuth {
		t.Error("expected no Authorization header when token is empty")
	}
}

// TestReleaseFetcher_StaleOK_ServesLastGoodOnTransientFailure is the F3 core
// resilience guard. After a successful fetch caches a good result, a subsequent
// transient GitHub failure (403/5xx/network) must NOT cause the fetcher to error
// (which would make the handler regress to the static config floor). Instead it
// must serve the last-good cached result with nil error.
func TestReleaseFetcher_StaleOK_ServesLastGoodOnTransientFailure(t *testing.T) {
	var fail bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(http.StatusForbidden) // simulate rate-limit 403
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]githubRelease{
			{TagName: "daemon/v0.3.6", PublishedAt: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)},
		})
	}))
	t.Cleanup(srv.Close)

	// Short TTL so the second call re-fetches and hits the failing path.
	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 50*time.Millisecond, "", nil)

	first, err := f.LatestDaemonRelease()
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if first.Latest != "0.3.6" {
		t.Fatalf("first call latest: got %q, want %q", first.Latest, "0.3.6")
	}

	// Force the next fetch to fail and let the TTL expire.
	fail = true
	time.Sleep(80 * time.Millisecond)

	second, err := f.LatestDaemonRelease()
	if err != nil {
		t.Fatalf("stale-OK: expected nil error serving last-good, got %v", err)
	}
	if second.Latest != "0.3.6" {
		t.Errorf("stale-OK latest: got %q, want %q (must serve last-good, not regress)", second.Latest, "0.3.6")
	}
}

// TestReleaseFetcher_NoStaleCache_PropagatesErrorOnColdFailure verifies that when
// there is NO prior good result (cold start) and the first fetch fails, the
// fetcher still returns an error so the handler can fall back to the static
// config — stale-OK must not mask a genuine cold-start failure.
func TestReleaseFetcher_NoStaleCache_PropagatesErrorOnColdFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	f := handlers.NewReleaseFetcher(srv.URL+"/releases", 5*time.Minute, "", nil)
	if _, err := f.LatestDaemonRelease(); err == nil {
		t.Error("expected error on cold-start failure with no cached result, got nil")
	}
}

// testCfg satisfies the VersionConfig interface for tests.
type testCfg struct {
	version    string
	releasedAt string
}

func (c *testCfg) GetDaemonLatestVersion() string { return c.version }
func (c *testCfg) GetDaemonReleasedAt() string    { return c.releasedAt }
