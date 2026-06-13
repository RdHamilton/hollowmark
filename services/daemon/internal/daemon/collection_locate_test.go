//go:build darwin

package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLocateHelperBinary_ShareDir verifies that locateHelperBinary resolves
// the collection-helper from SHARE_DIR (/usr/local/share/vaultmtg) by default
// (hollowmark-tickets#1286, R7; ADR-059).
//
// Under ADR-059 there is no companion install/ directory alongside the binary.
// The env var MTGA_COLLECTION_HELPER_DIR overrides the default for development
// and tests.
func TestLocateHelperBinary_ShareDir(t *testing.T) {
	// Build a fake SHARE_DIR with only the helper binary (no install/ — ADR-059).
	shareDir := t.TempDir()
	helperBin := filepath.Join(shareDir, "collection-helper")
	if err := os.WriteFile(helperBin, []byte("fake helper"), 0o755); err != nil {
		t.Fatalf("write helper: %v", err)
	}

	// Point the env override at our fake share dir.
	t.Setenv("MTGA_COLLECTION_HELPER_DIR", shareDir)

	gotBinary, err := locateHelperBinary()
	if err != nil {
		t.Fatalf("locateHelperBinary() error = %v; want nil", err)
	}
	if gotBinary != helperBin {
		t.Errorf("helperBinary = %q; want %q", gotBinary, helperBin)
	}
}

// TestLocateHelperBinary_DefaultShareDirConstant verifies that when
// MTGA_COLLECTION_HELPER_DIR is unset, locateHelperBinary uses the production
// default SHARE_DIR constant (/usr/local/share/vaultmtg) rather than the
// directory of the running executable.
//
// This test cannot actually resolve the production path (no helper installed on
// the CI runner), so it verifies the constant value matches the build-pkg.sh
// SHARE_DIR definition.
func TestLocateHelperBinary_DefaultShareDirConstant(t *testing.T) {
	// The production SHARE_DIR must be the same constant as in build-pkg.sh and postinstall.
	want := "/usr/local/share/vaultmtg"
	if helperShareDir != want {
		t.Errorf("helperShareDir constant = %q; want %q (must match SHARE_DIR in build-pkg.sh)", helperShareDir, want)
	}
}

// TestLocateHelperBinary_ErrorWhenHelperAbsent verifies that an error is returned
// when the helper binary does not exist (not a silent empty path).
func TestLocateHelperBinary_ErrorWhenHelperAbsent(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("MTGA_COLLECTION_HELPER_DIR", emptyDir)

	_, err := locateHelperBinary()
	if err == nil {
		t.Fatal("locateHelperBinary() error = nil; want non-nil when helper is absent")
	}
}
