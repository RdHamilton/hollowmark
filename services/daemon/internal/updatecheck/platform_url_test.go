package updatecheck

import (
	"testing"
)

// TestSelectInstallerURL_Darwin verifies that darwin selects MacOSInstallerURL.
func TestSelectInstallerURL_Darwin(t *testing.T) {
	vr := VersionResponse{
		Latest:              "0.3.7",
		MacOSInstallerURL:   "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-darwin-universal.pkg",
		WindowsInstallerURL: "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-windows-amd64.exe",
	}

	got := SelectInstallerURL(&vr, "darwin")
	want := vr.MacOSInstallerURL
	if got != want {
		t.Errorf("darwin: got %q, want %q", got, want)
	}
}

// TestSelectInstallerURL_Windows verifies that windows/amd64 selects WindowsInstallerURL.
func TestSelectInstallerURL_Windows(t *testing.T) {
	vr := VersionResponse{
		Latest:              "0.3.7",
		MacOSInstallerURL:   "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-darwin-universal.pkg",
		WindowsInstallerURL: "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-windows-amd64.exe",
	}

	got := SelectInstallerURL(&vr, "windows")
	want := vr.WindowsInstallerURL
	if got != want {
		t.Errorf("windows: got %q, want %q", got, want)
	}
}

// TestSelectInstallerURL_UnknownOS verifies that an unknown OS returns an empty
// string so handleInstallUpdate aborts cleanly rather than downloading an HTML page.
func TestSelectInstallerURL_UnknownOS(t *testing.T) {
	vr := VersionResponse{
		Latest:              "0.3.7",
		MacOSInstallerURL:   "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-darwin-universal.pkg",
		WindowsInstallerURL: "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-windows-amd64.exe",
	}

	got := SelectInstallerURL(&vr, "linux")
	if got != "" {
		t.Errorf("unknown OS: expected empty, got %q", got)
	}
}

// TestSelectInstallerURL_DarwinMissingURL verifies that a missing MacOSInstallerURL
// returns empty (BFF not yet updated or asset missing from release).
func TestSelectInstallerURL_DarwinMissingURL(t *testing.T) {
	vr := VersionResponse{
		Latest: "0.3.7",
		// No MacOSInstallerURL
		WindowsInstallerURL: "https://github.com/RdHamilton/vault-mtg/releases/download/daemon%2Fv0.3.7/vaultmtg-daemon-windows-amd64.exe",
	}

	got := SelectInstallerURL(&vr, "darwin")
	if got != "" {
		t.Errorf("missing macOS URL: expected empty, got %q", got)
	}
}
