package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	contract "github.com/RdHamilton/hollowmark/services/contract"
)

// allowedAssetHosts is the set of hostnames permitted in release asset download
// URLs. Any asset whose BrowserDownloadURL resolves to a host not in this set is
// silently omitted from the response to protect the daemon from hostile URLs that
// could be injected into a compromised release.
//
// GitHub serves release assets from github.com; CDN-redirected downloads use
// objects.githubusercontent.com.
var allowedAssetHosts = map[string]struct{}{
	"github.com":                    {},
	"objects.githubusercontent.com": {},
}

// githubAPIResponseMaxBytes caps the GitHub Releases API response body at 2 MiB.
// This prevents a pathologically large (or malicious) payload from consuming
// excessive memory in the BFF.
const githubAPIResponseMaxBytes = 2 * 1024 * 1024

// isAllowedAssetHost returns true when the given raw URL has a host in the
// production allow-list. Malformed URLs and off-list hosts return false.
func isAllowedAssetHost(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	_, ok := allowedAssetHosts[host]
	return ok
}

// githubRelease is the subset of the GitHub Releases API response that the
// fetcher cares about.
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

// ReleaseFetcher fetches the latest daemon release from the GitHub Releases API
// and caches the result for the configured TTL.
type ReleaseFetcher struct {
	releasesURL string
	ttl         time.Duration
	token       string
	httpClient  *http.Client

	mu        sync.Mutex
	cached    *contract.DaemonVersionResponse
	fetchedAt time.Time
}

// NewReleaseFetcher creates a ReleaseFetcher. releasesURL is the GitHub Releases
// API endpoint (e.g. "https://api.github.com/repos/RdHamilton/hollowmark/releases").
// ttl is how long the cached result is valid. token is an optional GitHub token
// used to authenticate the request (5000 req/hr instead of the 60 req/hr
// anonymous limit); when empty, the request is sent anonymously. httpClient may
// be nil; a default client with a 10-second timeout is used.
func NewReleaseFetcher(releasesURL string, ttl time.Duration, token string, httpClient *http.Client) *ReleaseFetcher {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &ReleaseFetcher{
		releasesURL: releasesURL,
		ttl:         ttl,
		token:       strings.TrimSpace(token),
		httpClient:  httpClient,
	}
}

// LatestDaemonRelease returns the latest non-prerelease, non-draft daemon/v*
// release, selected by semantic-version order (the GREATEST version wins, not
// the first in GitHub's created_at-desc ordering). Results are cached for the
// configured TTL.
//
// Resilience (stale-OK): when a fetch fails AFTER a prior successful fetch
// populated the cache, the last-good cached result is served (with a loud log)
// instead of propagating the error. This prevents a transient GitHub failure
// (rate-limit 403, 5xx, network blip) from silently regressing the advertised
// version to the static config floor. On a COLD start (no cached result) a fetch
// error is propagated so the handler can fall back to the static config.
func (f *ReleaseFetcher) LatestDaemonRelease() (*contract.DaemonVersionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.cached != nil && time.Since(f.fetchedAt) < f.ttl {
		return f.cached, nil
	}

	result, err := f.fetchLatestDaemonRelease()
	if err != nil {
		// Stale-OK: serve the last-good result rather than regress to the
		// static floor. Log loudly so a persistent GitHub outage is visible.
		if f.cached != nil {
			log.Printf("[daemon-version] fetch failed (%v) — serving last-good cached version %q (age %s)",
				err, f.cached.Latest, time.Since(f.fetchedAt).Round(time.Second))
			return f.cached, nil
		}
		// Cold start with no cached result — propagate so the handler can fall
		// back to the static config.
		return nil, err
	}

	f.cached = result
	f.fetchedAt = time.Now()
	return result, nil
}

// fetchLatestDaemonRelease performs the actual HTTP call to the GitHub Releases API.
func (f *ReleaseFetcher) fetchLatestDaemonRelease() (*contract.DaemonVersionResponse, error) {
	req, err := http.NewRequest(http.MethodGet, f.releasesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "vaultmtg-bff")
	if f.token != "" {
		// Authenticated requests get the 5000 req/hr limit; anonymous get 60.
		req.Header.Set("Authorization", "Bearer "+f.token)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github releases fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases API returned %d", resp.StatusCode)
	}

	var releases []githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, githubAPIResponseMaxBytes)).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode github releases: %w", err)
	}

	return parseDaemonRelease(releases), nil
}

// parseDaemonRelease selects the SEMVER-GREATEST non-prerelease, non-draft
// release whose tag starts with "daemon/v" and extracts its version metadata.
//
// Why not first-by-created_at: GitHub returns releases in created_at-desc order,
// which usually tracks semver but NOT always. A re-cut or hotfix can create a
// LOWER semver with a LATER created_at (exactly what the GoReleaser-free
// tag-rename dance and the 0.3.x.N hotfix waves produce). Picking the first
// match would then serve a regression, so we compare versions and pick the
// greatest.
//
// Host validation: every asset URL is validated against allowedAssetHosts before
// being included in the response. Any asset whose host is not on the allow-list
// is silently omitted, defending against a compromised release injecting hostile
// download URLs that the daemon would otherwise trust.
func parseDaemonRelease(releases []githubRelease) *contract.DaemonVersionResponse {
	var best *githubRelease
	var bestVersion string

	for i := range releases {
		r := releases[i]
		if !strings.HasPrefix(r.TagName, "daemon/v") {
			continue
		}
		if r.Prerelease || r.Draft {
			continue
		}

		// Strip the "daemon/v" prefix to get the bare version string.
		version := strings.TrimPrefix(r.TagName, "daemon/v")
		if version == "" {
			continue
		}

		if best == nil || compareDaemonVersions(version, bestVersion) > 0 {
			best = &releases[i]
			bestVersion = version
		}
	}

	if best == nil {
		// No matching daemon release found.
		return &contract.DaemonVersionResponse{}
	}

	result := &contract.DaemonVersionResponse{
		Latest:      bestVersion,
		ReleasedAt:  best.PublishedAt.UTC().Format(time.RFC3339),
		DownloadURL: best.HTMLURL,
	}

	// Populate asset URLs from the release assets, validating every URL's
	// host against the allow-list before accepting it.
	for _, asset := range best.Assets {
		if !isAllowedAssetHost(asset.BrowserDownloadURL) {
			// Off-list host — skip this asset entirely.
			continue
		}
		switch asset.Name {
		case "SHA256SUMS":
			result.Sha256SumsURL = asset.BrowserDownloadURL
		case "SHA256SUMS.minisig":
			result.AttestationURL = asset.BrowserDownloadURL
		case "vaultmtg-daemon-darwin-universal.pkg":
			result.MacOSInstallerURL = asset.BrowserDownloadURL
		case "vaultmtg-daemon-windows-amd64.exe":
			result.WindowsInstallerURL = asset.BrowserDownloadURL
		}
	}

	return result
}

// compareDaemonVersions compares two bare daemon version strings (no leading
// "v"), returning -1 if a < b, 0 if equal, +1 if a > b.
//
// It handles the standard 3-segment semver (e.g. "0.3.6") AND the project's
// 4-segment hotfix-wave format (e.g. "0.3.6.1", per the "4th segment = hotfix
// wave" convention) by comparing dotted numeric segments left-to-right. A
// version with more segments but otherwise-equal leading segments sorts higher
// (so "0.3.6.1" > "0.3.6"). Any pre-release suffix (after "-") is stripped
// before comparison; pre-releases are already filtered out upstream, this is
// defence-in-depth. Non-numeric or malformed segments compare as 0, so a
// well-formed version always beats a malformed one.
func compareDaemonVersions(a, b string) int {
	as := versionSegments(a)
	bs := versionSegments(b)

	n := len(as)
	if len(bs) > n {
		n = len(bs)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

// versionSegments splits a bare version string into its numeric dotted segments,
// dropping any pre-release/build suffix introduced by "-" or "+". Non-numeric
// segments are treated as 0.
func versionSegments(v string) []int {
	// Drop pre-release ("-") and build ("+") metadata.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		out[i] = n
	}
	return out
}
