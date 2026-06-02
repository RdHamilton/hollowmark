package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	contract "github.com/RdHamilton/vault-mtg/services/contract"
)

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
	httpClient  *http.Client

	mu        sync.Mutex
	cached    *contract.DaemonVersionResponse
	fetchedAt time.Time
}

// NewReleaseFetcher creates a ReleaseFetcher. releasesURL is the GitHub Releases
// API endpoint (e.g. "https://api.github.com/repos/RdHamilton/vault-mtg/releases").
// ttl is how long the cached result is valid. httpClient may be nil; a default
// client with a 10-second timeout is used.
func NewReleaseFetcher(releasesURL string, ttl time.Duration, httpClient *http.Client) *ReleaseFetcher {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &ReleaseFetcher{
		releasesURL: releasesURL,
		ttl:         ttl,
		httpClient:  httpClient,
	}
}

// LatestDaemonRelease returns the latest non-prerelease, non-draft daemon/v*
// release. Results are cached for the configured TTL.
func (f *ReleaseFetcher) LatestDaemonRelease() (*contract.DaemonVersionResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.cached != nil && time.Since(f.fetchedAt) < f.ttl {
		return f.cached, nil
	}

	result, err := f.fetchLatestDaemonRelease()
	if err != nil {
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

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github releases fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases API returned %d", resp.StatusCode)
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode github releases: %w", err)
	}

	return parseDaemonRelease(releases), nil
}

// parseDaemonRelease picks the first non-prerelease, non-draft release whose
// tag starts with "daemon/" and extracts the version metadata.
func parseDaemonRelease(releases []githubRelease) *contract.DaemonVersionResponse {
	for _, r := range releases {
		if !strings.HasPrefix(r.TagName, "daemon/") {
			continue
		}
		if r.Prerelease || r.Draft {
			continue
		}

		// Strip the "daemon/v" prefix to get the bare semver string.
		version := strings.TrimPrefix(r.TagName, "daemon/v")

		result := &contract.DaemonVersionResponse{
			Latest:      version,
			ReleasedAt:  r.PublishedAt.UTC().Format(time.RFC3339),
			DownloadURL: r.HTMLURL,
		}

		// Populate SHA256SUMS and signature URLs from assets.
		for _, asset := range r.Assets {
			switch asset.Name {
			case "SHA256SUMS":
				result.Sha256SumsURL = asset.BrowserDownloadURL
			case "SHA256SUMS.minisig":
				result.AttestationURL = asset.BrowserDownloadURL
			}
		}

		return result
	}

	// No matching daemon release found.
	return &contract.DaemonVersionResponse{}
}
