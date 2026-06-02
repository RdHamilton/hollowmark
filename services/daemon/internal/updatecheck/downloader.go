package updatecheck

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// DownloaderConfig holds the configuration for a Downloader.
type DownloaderConfig struct {
	// AllowedHosts is the list of hostnames (host:port or just host) that are
	// permitted as download or redirect destinations. Every redirect hop is
	// re-validated against this list.
	//
	// The production default (used when NewDownloader is called with an empty
	// AllowedHosts) is the GitHub release host and the GitHub CDN redirect target:
	//   - "github.com"
	//   - "objects.githubusercontent.com"
	//   - "codeload.github.com"
	//
	// Tests may override this to point at local httptest servers.
	AllowedHosts []string

	// AllowHTTPForTesting permits plain http:// for the initial request URL in tests.
	// The redirect-hop validator still rejects http:// redirects unless this is set.
	// MUST NOT be set in production.
	AllowHTTPForTesting bool
}

// productionAllowedHosts is the allow-list of hosts that may appear in any
// redirect hop during a GitHub release download. GitHub serves the initial
// request from github.com and redirects to the CDN for the actual binary.
var productionAllowedHosts = []string{
	"github.com",
	"objects.githubusercontent.com",
	"codeload.github.com",
}

// Downloader downloads release artifacts with a per-redirect allow-list check.
type Downloader struct {
	cfg DownloaderConfig
}

// NewDownloader creates a Downloader. When cfg.AllowedHosts is empty, the
// production allow-list (github.com + CDN hosts) is used.
func NewDownloader(cfg DownloaderConfig) *Downloader {
	if len(cfg.AllowedHosts) == 0 {
		cfg.AllowedHosts = productionAllowedHosts
	}
	return &Downloader{cfg: cfg}
}

// allowedHostSet converts the AllowedHosts slice to a set for O(1) lookup.
func (d *Downloader) allowedHostSet() map[string]struct{} {
	m := make(map[string]struct{}, len(d.cfg.AllowedHosts))
	for _, h := range d.cfg.AllowedHosts {
		m[h] = struct{}{}
	}
	return m
}

// validateURL checks that u uses https (or http when AllowHTTPForTesting is set)
// and that its host is in the allow-list.
func (d *Downloader) validateURL(u *url.URL) error {
	if u.Scheme != "https" {
		if !d.cfg.AllowHTTPForTesting || u.Scheme != "http" {
			return fmt.Errorf("redirect rejected: scheme %q is not https", u.Scheme)
		}
	}
	allowed := d.allowedHostSet()
	// url.Host may be "host:port" or just "host".
	host := u.Hostname()
	hostPort := u.Host
	if _, ok := allowed[host]; ok {
		return nil
	}
	if _, ok := allowed[hostPort]; ok {
		return nil
	}
	return fmt.Errorf("redirect rejected: host %q is not in allow-list", host)
}

// buildHTTPClient constructs an http.Client with a CheckRedirect that validates
// every redirect hop against the allow-list.
func (d *Downloader) buildHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 5 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			u, err := url.Parse(req.URL.String())
			if err != nil {
				return fmt.Errorf("redirect parse: %w", err)
			}
			if err := d.validateURL(u); err != nil {
				return err
			}
			return nil
		},
	}
}

// DownloadToTempDir downloads the resource at sourceURL into a newly created
// directory under parentDir and returns the path to the downloaded file. The
// created directory is immediately chmod'd to 0700. The temp dir name is based
// on a fixed prefix ("vaultmtg-update-") so CleanStaleTempDirs can identify it.
//
// The source URL is validated against the allow-list before any connection is
// made. Temp path never appears in returned errors.
func (d *Downloader) DownloadToTempDir(sourceURL, parentDir string) (string, error) {
	u, err := url.Parse(sourceURL)
	if err != nil {
		return "", fmt.Errorf("parse source URL: %w", err)
	}
	if err := d.validateURL(u); err != nil {
		return "", err
	}

	// Create a uniquely-named temp dir inside parentDir.
	dir, err := os.MkdirTemp(parentDir, "vaultmtg-update-")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	// Immediately restrict permissions — no temp path in error messages below.
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("chmod temp dir: %w", err)
	}

	client := d.buildHTTPClient()
	req, err := http.NewRequest(http.MethodGet, sourceURL, nil)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", "vaultmtg-daemon")

	resp, err := client.Do(req)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	// Derive filename from URL path.
	filename := filepath.Base(u.Path)
	if filename == "" || filename == "." || filename == "/" {
		filename = "installer"
	}
	destPath := filepath.Join(dir, filename)

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("create dest file: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("write download: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("close dest file: %w", err)
	}

	return destPath, nil
}

// VerifyChecksum reads sumsPath (GNU-format: "<hex>  <filename>" per line), finds
// the entry for filename, and verifies that the SHA-256 of installerPath matches.
// Returns an error if the file is missing from the sums file or the hash mismatches.
func (d *Downloader) VerifyChecksum(installerPath, filename, sumsPath string) error {
	sumsData, err := os.ReadFile(sumsPath)
	if err != nil {
		return fmt.Errorf("read SHA256SUMS: %w", err)
	}

	// Look for the line matching filename.
	var expectedHex string
	scanner := bufio.NewScanner(strings.NewReader(string(sumsData)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.TrimSpace(parts[1]) == filename {
			expectedHex = strings.TrimSpace(parts[0])
			break
		}
	}
	if expectedHex == "" {
		return fmt.Errorf("checksum: %q not found in SHA256SUMS", filename)
	}

	// Compute actual hash.
	data, err := os.ReadFile(installerPath)
	if err != nil {
		return fmt.Errorf("read installer for checksum: %w", err)
	}
	actualSum := sha256.Sum256(data)
	actualHex := hex.EncodeToString(actualSum[:])

	if actualHex != expectedHex {
		return fmt.Errorf("checksum mismatch for %q: got %s, want %s", filename, actualHex, expectedHex)
	}
	return nil
}

// CheckDowngrade returns an error if latest <= current (downgrade or no-op).
// Versions are compared as semver strings (a "v" prefix is added if absent).
// This guards against both actual downgrades and repeated install of the same version.
func (d *Downloader) CheckDowngrade(current, latest string) error {
	cur := normalizeSemver(current)
	lat := normalizeSemver(latest)

	cmp := semver.Compare(lat, cur)
	if cmp <= 0 {
		return fmt.Errorf("downgrade blocked: latest %s is not newer than current %s", latest, current)
	}
	return nil
}

// normalizeSemver ensures the version string has a "v" prefix for semver.Compare.
func normalizeSemver(v string) string {
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

// BuildWindowsLaunchArgs returns the argument list for launching the Windows
// installer via cmd /c start, with the installer path properly double-quoted to
// handle spaces. The first argument "" is the window title (required by start).
func BuildWindowsLaunchArgs(installerPath string) []string {
	return []string{
		"cmd",
		"/c",
		"start",
		"",
		`"` + installerPath + `"`,
	}
}

// CleanStaleTempDirs removes subdirectories of parentDir whose name starts with
// prefix and whose modification time is older than maxAge. Non-matching dirs
// and errors on individual entries are silently skipped.
//
// This is called on daemon startup to clean up temp dirs left by a previous
// download session (the installer kills the daemon before cleanup can run).
func CleanStaleTempDirs(parentDir, prefix string, maxAge time.Duration) {
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.RemoveAll(filepath.Join(parentDir, e.Name()))
		}
	}
}

// LaunchInstaller launches the downloaded installer binary without executing it
// in-process. The daemon is the trigger, never the executor.
//
// On macOS: uses `open <pkg>` which hands off to Installer.app.
// On Windows: uses `cmd /c start "" "<path>"` — mandatory shell-level execution
// because Windows SmartScreen blocks direct exec of unsigned binaries but allows
// the user to acknowledge via the start dialog.
//
// The daemon is intentionally not waiting for the installer to complete; the
// NSIS installer will kill the running daemon via schtasks /End mid-session
// (this is intentional — see service.go comments). The caller should NOT try
// to do post-launch cleanup in a defer; use CleanStaleTempDirs on next startup.
//
// Windows note: the binary is UNSIGNED (Azure signing is blocked — vmt-t#255).
// The tray notification shown before this call MUST warn the user about the
// SmartScreen dialog.
//
// Implemented per-platform in launch_darwin.go / launch_windows.go / launch_other.go.
var LaunchInstaller = launchInstaller
