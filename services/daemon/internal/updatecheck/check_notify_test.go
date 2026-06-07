package updatecheck_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/updatecheck"
)

// TestCheck_UpdateAvailable_CallsNotifyHook verifies that when a newer version
// is available the UpdateAvailable callback (tray prompt) fires with the correct
// version string and download URL.
func TestCheck_UpdateAvailable_CallsNotifyHook(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(versionResponseFull{
			Latest:         "1.3.0",
			ReleasedAt:     "2026-06-01T12:00:00Z",
			DownloadURL:    "https://github.com/RdHamilton/hollowmark/releases/tag/daemon%2Fv1.3.0",
			Sha256SumsURL:  "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv1.3.0/SHA256SUMS",
			AttestationURL: "https://github.com/RdHamilton/hollowmark/releases/download/daemon%2Fv1.3.0/SHA256SUMS.minisig",
		})
	}))
	defer srv.Close()

	var gotVersion, gotURL string
	notified := make(chan struct{}, 1)

	opts := updatecheck.Options{
		NotifyUpdateAvailable: func(version, downloadURL string) {
			gotVersion = version
			gotURL = downloadURL
			notified <- struct{}{}
		},
	}

	updatecheck.CheckWithOptions(context.Background(), srv.URL, "1.2.0", opts)

	select {
	case <-notified:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("NotifyUpdateAvailable was not called within timeout")
	}

	if gotVersion != "1.3.0" {
		t.Errorf("notify version: got %q, want %q", gotVersion, "1.3.0")
	}
	if gotURL == "" {
		t.Error("notify URL must not be empty")
	}
}

// TestCheck_SameVersion_NoNotify verifies that NotifyUpdateAvailable is NOT
// called when the current version equals the latest.
func TestCheck_SameVersion_NoNotify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(versionResponseFull{Latest: "1.2.0"})
	}))
	defer srv.Close()

	called := false
	opts := updatecheck.Options{
		NotifyUpdateAvailable: func(version, downloadURL string) {
			called = true
		},
	}

	updatecheck.CheckWithOptions(context.Background(), srv.URL, "1.2.0", opts)

	if called {
		t.Error("NotifyUpdateAvailable should not be called when version matches")
	}
}

// TestCheck_NilNotify_NoopWhenNoCallback verifies that passing a nil
// NotifyUpdateAvailable does not panic even when an update is available.
func TestCheck_NilNotify_NoopWhenNoCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(versionResponseFull{Latest: "2.0.0"})
	}))
	defer srv.Close()

	// Must not panic.
	updatecheck.CheckWithOptions(context.Background(), srv.URL, "1.0.0", updatecheck.Options{})
}

// versionResponseFull mirrors the full contract.DaemonVersionResponse shape used by
// the test servers in this package.
type versionResponseFull struct {
	Latest         string `json:"latest"`
	ReleasedAt     string `json:"released_at"`
	DownloadURL    string `json:"download_url"`
	Sha256SumsURL  string `json:"sha256sums_url"`
	AttestationURL string `json:"attestation_url"`
}
