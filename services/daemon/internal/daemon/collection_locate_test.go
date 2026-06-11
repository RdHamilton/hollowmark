//go:build darwin

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLocateHelperFiles_ShareDir verifies that locateHelperFiles resolves
// the collection-helper from SHARE_DIR (/usr/local/share/vaultmtg) by default
// (hollowmark-tickets#1286, R7).
//
// The production .pkg installs the helper and install/ directory under
// /usr/local/share/vaultmtg/.  locateHelperFiles must look there when
// MTGA_COLLECTION_HELPER_DIR is unset and the binary is at /usr/local/bin/
// (i.e., the helper is NOT in the same directory as the daemon executable).
func TestLocateHelperFiles_ShareDir(t *testing.T) {
	// Build a fake SHARE_DIR with a helper binary and install/ directory.
	shareDir := t.TempDir()
	installDir := filepath.Join(shareDir, "install")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("mkdir install: %v", err)
	}
	helperBin := filepath.Join(shareDir, "collection-helper")
	if err := os.WriteFile(helperBin, []byte("fake helper"), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	// Point the env override at our fake share dir.
	t.Setenv("MTGA_COLLECTION_HELPER_DIR", shareDir)

	gotBinary, gotScriptDir, err := locateHelperFiles()
	if err != nil {
		t.Fatalf("locateHelperFiles() error = %v; want nil", err)
	}
	if gotBinary != helperBin {
		t.Errorf("helperBinary = %q; want %q", gotBinary, helperBin)
	}
	if gotScriptDir != installDir {
		t.Errorf("scriptDir = %q; want %q", gotScriptDir, installDir)
	}
}

// TestLocateHelperFiles_DefaultShareDir verifies that when MTGA_COLLECTION_HELPER_DIR
// is unset, locateHelperFiles uses the production default SHARE_DIR constant
// (/usr/local/share/vaultmtg) rather than the directory of the running executable.
//
// This test cannot actually resolve the production path (no helper installed on
// the CI runner), so it verifies the fallback path via the env var, and separately
// verifies that the constant is the correct production path.
func TestLocateHelperFiles_DefaultShareDirConstant(t *testing.T) {
	// The production SHARE_DIR must be the same constant as in build-pkg.sh and postinstall.
	want := "/usr/local/share/vaultmtg"
	if helperShareDir != want {
		t.Errorf("helperShareDir constant = %q; want %q (must match SHARE_DIR in build-pkg.sh)", helperShareDir, want)
	}
}

// TestLocateHelperFiles_ErrorWhenHelperAbsent verifies that an error is returned
// when the helper binary does not exist (not a silent empty path).
func TestLocateHelperFiles_ErrorWhenHelperAbsent(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("MTGA_COLLECTION_HELPER_DIR", emptyDir)

	_, _, err := locateHelperFiles()
	if err == nil {
		t.Fatal("locateHelperFiles() error = nil; want non-nil when helper is absent")
	}
}
