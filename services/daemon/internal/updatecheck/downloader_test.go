package updatecheck_test

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/updatecheck"
)

// --- Helper functions ---

// writeFakeInstaller writes a small fake installer file with deterministic content.
func writeFakeInstaller(t *testing.T, dir, filename string) (path string, sum [32]byte) {
	t.Helper()
	content := []byte("fake installer content for " + filename)
	path = filepath.Join(dir, filename)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write fake installer: %v", err)
	}
	sum = sha256.Sum256(content)
	return path, sum
}

// sha256HexOfFile returns the hex-encoded SHA-256 sum of the named file.
func sha256HexOfFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file for checksum: %v", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// makeMinisigKey creates a minimal minisig keypair (stub — just validates format, not real crypto).
// For real signature tests see TestDownloader_SignatureVerification_Valid.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Redirect allow-list tests ---

func TestDownloader_RejectsRedirectToNonAllowedHost(t *testing.T) {
	// Set up an attacker server that redirects to an evil host.
	evilCalled := false
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilCalled = true
		w.WriteHeader(http.StatusOK)
	}))
	defer evil.Close()

	legit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect to the evil server (not in allow-list).
		http.Redirect(w, r, evil.URL+"/evil-binary", http.StatusFound)
	}))
	defer legit.Close()

	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{
		AllowedHosts: []string{legit.Listener.Addr().String()},
	})

	dir := t.TempDir()
	_, err := d.DownloadToTempDir(legit.URL+"/installer.pkg", dir)
	if err == nil {
		t.Error("expected error for redirect to non-allowed host, got nil")
	}
	if evilCalled {
		t.Error("evil server should not have been called after redirect rejection")
	}
	// The error message must mention the blocked redirect.
	if !strings.Contains(err.Error(), "redirect") && !strings.Contains(err.Error(), "not in allow-list") {
		t.Errorf("expected redirect-related error, got: %v", err)
	}
}

func TestDownloader_AllowsRedirectToAllowedCDN(t *testing.T) {
	// Simulate a GitHub → CDN redirect: legit redirects to cdn (both allowed).
	payload := []byte("installer payload")

	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer cdn.Close()

	legit := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, cdn.URL+"/vaultmtg-installer.pkg", http.StatusFound)
	}))
	defer legit.Close()

	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{
		AllowedHosts: []string{
			legit.Listener.Addr().String(),
			cdn.Listener.Addr().String(),
		},
		AllowHTTPForTesting: true, // test servers use http://
	})

	dir := t.TempDir()
	dest, err := d.DownloadToTempDir(legit.URL+"/vaultmtg-installer.pkg", dir)
	if err != nil {
		t.Fatalf("expected redirect to allowed CDN to succeed, got: %v", err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("content mismatch: got %q, want %q", string(got), string(payload))
	}
}

func TestDownloader_RejectsNonHTTPS(t *testing.T) {
	// The allow-list check must reject http:// when AllowHTTPForTesting is false.
	// In production (no AllowHTTPForTesting), even the initial request must be https.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{
		AllowedHosts: []string{srv.Listener.Addr().String()},
		// AllowHTTPForTesting intentionally NOT set — production mode.
	})

	// Attempt a plain http:// request. Must be rejected before any connection.
	dir := t.TempDir()
	_, err := d.DownloadToTempDir(srv.URL+"/installer.pkg", dir)
	if err == nil {
		t.Error("expected error for http:// without AllowHTTPForTesting, got nil")
	}
	if !strings.Contains(err.Error(), "https") && !strings.Contains(err.Error(), "scheme") {
		t.Errorf("expected scheme-related error, got: %v", err)
	}
}

// --- Temp dir permission test ---

func TestDownloader_TempDirIsMode0700(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics differ on Windows")
	}

	payload := []byte("pkg content")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{
		AllowedHosts:        []string{srv.Listener.Addr().String()},
		AllowHTTPForTesting: true,
	})

	parentDir := t.TempDir()
	dest, err := d.DownloadToTempDir(srv.URL+"/installer.pkg", parentDir)
	if err != nil {
		t.Fatalf("download: %v", err)
	}

	dirInfo, err := os.Stat(filepath.Dir(dest))
	if err != nil {
		t.Fatalf("stat temp dir: %v", err)
	}
	// Verify the directory that was created has mode 0700.
	mode := dirInfo.Mode().Perm()
	if mode != 0o700 {
		t.Errorf("temp dir mode: got %04o, want 0700", mode)
	}
}

// --- SHA-256 checksum tests ---

func TestDownloader_VerifyChecksum_Valid(t *testing.T) {
	content := []byte("real installer binary")
	sum := sha256.Sum256(content)
	filename := "vaultmtg-daemon.pkg"
	sumsContent := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filename)

	dir := t.TempDir()
	installerPath := filepath.Join(dir, filename)
	if err := os.WriteFile(installerPath, content, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	sumsPath := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sumsPath, []byte(sumsContent), 0o600); err != nil {
		t.Fatalf("write sums: %v", err)
	}

	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{})
	if err := d.VerifyChecksum(installerPath, filename, sumsPath); err != nil {
		t.Errorf("expected valid checksum to pass, got: %v", err)
	}
}

func TestDownloader_VerifyChecksum_Invalid(t *testing.T) {
	dir := t.TempDir()
	filename := "vaultmtg-daemon.pkg"

	// Write a file whose hash won't match the sums file.
	installerPath := filepath.Join(dir, filename)
	if err := os.WriteFile(installerPath, []byte("tampered content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Write a sums file with a different hash.
	goodContent := []byte("real installer binary")
	sum := sha256.Sum256(goodContent)
	sumsContent := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), filename)
	sumsPath := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sumsPath, []byte(sumsContent), 0o600); err != nil {
		t.Fatalf("write sums: %v", err)
	}

	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{})
	err := d.VerifyChecksum(installerPath, filename, sumsPath)
	if err == nil {
		t.Error("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("expected checksum-related error message, got: %v", err)
	}
}

func TestDownloader_VerifyChecksum_FileMissingFromSums(t *testing.T) {
	dir := t.TempDir()
	filename := "vaultmtg-daemon.pkg"
	installerPath := filepath.Join(dir, filename)
	if err := os.WriteFile(installerPath, []byte("content"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Sums file does not mention this filename.
	sumsPath := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sumsPath, []byte("abc123  other-file.pkg\n"), 0o600); err != nil {
		t.Fatalf("write sums: %v", err)
	}

	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{})
	err := d.VerifyChecksum(installerPath, filename, sumsPath)
	if err == nil {
		t.Error("expected error when filename missing from sums, got nil")
	}
}

// --- Downgrade protection test ---

func TestDownloader_BlocksDowngrade(t *testing.T) {
	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{})

	// current >= latest should be blocked
	tests := []struct {
		current string
		latest  string
		blocked bool
	}{
		{"0.3.7", "0.3.6", true},
		{"0.3.7", "0.3.7", true},  // equal is also a downgrade (no-op)
		{"0.3.7", "0.3.8", false}, // upgrade is allowed
		// RC→GA: v0.3.7-rc1 < v0.3.7 — GA is an upgrade.
		{"0.3.7-rc1", "0.3.7", false},
		// GA→RC: v0.3.7 > v0.3.7-rc1 — RC is a downgrade.
		{"0.3.7", "0.3.7-rc1", true},
	}

	for _, tc := range tests {
		err := d.CheckDowngrade(tc.current, tc.latest)
		if tc.blocked && err == nil {
			t.Errorf("CheckDowngrade(%q, %q): expected downgrade block, got nil", tc.current, tc.latest)
		}
		if !tc.blocked && err != nil {
			t.Errorf("CheckDowngrade(%q, %q): expected upgrade allowed, got: %v", tc.current, tc.latest, err)
		}
	}
}

// --- Stale temp dir cleanup test ---

func TestCleanStaleTempDirs_RemovesOldDirs(t *testing.T) {
	parent := t.TempDir()

	// Create a "stale" dir (old mtime).
	stale := filepath.Join(parent, "vaultmtg-update-stale")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatalf("mkdir stale: %v", err)
	}
	// Backdating mtime to 2 hours ago.
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	// Create a "fresh" dir (recent mtime).
	fresh := filepath.Join(parent, "vaultmtg-update-fresh")
	if err := os.Mkdir(fresh, 0o700); err != nil {
		t.Fatalf("mkdir fresh: %v", err)
	}

	// Create a non-update dir that must NOT be removed.
	other := filepath.Join(parent, "other-dir")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}

	updatecheck.CleanStaleTempDirs(parent, "vaultmtg-update-", 1*time.Hour)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale dir should have been removed")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("fresh dir should NOT have been removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("non-update dir should NOT have been removed")
	}
}

// --- Windows launch with space-in-path test ---

func TestLauncherArgs_WindowsSpaceInPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows launch path test only runs on Windows")
	}
	// Path with space.
	installerPath := `C:\Program Files\VaultMTG\vaultmtg-daemon-0.3.7-setup.exe`
	args := updatecheck.BuildWindowsLaunchArgs(installerPath)

	// Must use cmd /c start with the path properly quoted.
	if len(args) < 3 {
		t.Fatalf("expected at least 3 args, got %v", args)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "cmd") {
		t.Errorf("expected cmd in args, got %v", args)
	}
	if !strings.Contains(joined, "start") {
		t.Errorf("expected start in args, got %v", args)
	}
	// The path must appear quoted so the space doesn't break the command.
	if !strings.Contains(joined, `"C:\Program Files\VaultMTG\vaultmtg-daemon-0.3.7-setup.exe"`) {
		t.Errorf("path with space must be quoted; got %v", args)
	}
}

// TestLauncherArgs_WindowsSpaceInPath_CrossPlatform verifies the arg-building
// logic on non-Windows without actually exec'ing anything.
func TestLauncherArgs_WindowsSpaceInPath_CrossPlatform(t *testing.T) {
	// This test always runs — it only checks BuildWindowsLaunchArgs output.
	installerPath := `C:\Program Files\VaultMTG\vaultmtg-daemon-0.3.7-setup.exe`
	args := updatecheck.BuildWindowsLaunchArgs(installerPath)

	if len(args) < 3 {
		t.Fatalf("expected at least 3 args, got %v", args)
	}
	// Must have "cmd", "/c", "start" and then the path quoted.
	if args[0] != "cmd" {
		t.Errorf("args[0]: got %q, want %q", args[0], "cmd")
	}
	if args[1] != "/c" {
		t.Errorf("args[1]: got %q, want %q", args[1], "/c")
	}
	if args[2] != "start" {
		t.Errorf("args[2]: got %q, want %q", args[2], "start")
	}
	// Verify the space-containing path is quoted.
	fullCmd := strings.Join(args, " ")
	if !strings.Contains(fullCmd, `"`+installerPath+`"`) {
		t.Errorf("space-containing path must be double-quoted; args: %v", args)
	}
}

// --- No-op test: ensure temp path never appears in log output ---

func TestDownloader_TempPathNotInErrorLog(t *testing.T) {
	// Verify that when download fails, the error does NOT contain the full temp path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{
		AllowedHosts:        []string{srv.Listener.Addr().String()},
		AllowHTTPForTesting: true,
	})

	dir := t.TempDir()
	_, err := d.DownloadToTempDir(srv.URL+"/installer.pkg", dir)
	if err == nil {
		// 404 should cause an error.
		t.Fatal("expected error for 404 response")
	}
	// The error must not leak the temp dir path.
	if strings.Contains(err.Error(), dir) {
		t.Errorf("error message leaks temp path %q: %v", dir, err)
	}
}

// --- Download writes file test ---

func TestDownloader_WritesDownloadedFile(t *testing.T) {
	payload := make([]byte, 1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.Writer(w).Write(payload)
	}))
	defer srv.Close()

	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{
		AllowedHosts:        []string{srv.Listener.Addr().String()},
		AllowHTTPForTesting: true,
	})

	dir := t.TempDir()
	dest, err := d.DownloadToTempDir(srv.URL+"/vaultmtg-daemon.pkg", dir)
	if err != nil {
		t.Fatalf("download: %v", err)
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(payload) {
		t.Errorf("content mismatch (len got=%d want=%d)", len(got), len(payload))
	}

	// Filename must match the URL's base.
	if filepath.Base(dest) != "vaultmtg-daemon.pkg" {
		t.Errorf("filename: got %q, want %q", filepath.Base(dest), "vaultmtg-daemon.pkg")
	}
}
