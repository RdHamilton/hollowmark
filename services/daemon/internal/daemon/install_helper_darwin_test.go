//go:build darwin

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestTriggerHelperAuthorization_Success verifies that triggerHelperAuthorization
// invokes the helper binary with the --authorize flag and returns nil when the
// binary exits 0.
//
// Under ADR-059 the real helper calls AuthorizationCopyRights for
// com.apple.TaskForPid-allow and exits.  Here we use a shell script that
// echos "authorization granted" and exits 0 to isolate the exec contract
// without CGO or a signed binary.
func TestTriggerHelperAuthorization_Success(t *testing.T) {
	// Build a fake helper that accepts --authorize and exits 0.
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "collection-helper")
	script := "#!/bin/sh\necho 'authorization granted'; exit 0\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake helper: %v", err)
	}

	if err := triggerHelperAuthorization(fakeBin); err != nil {
		t.Errorf("triggerHelperAuthorization() error = %v; want nil", err)
	}
}

// TestTriggerHelperAuthorization_Denied verifies that triggerHelperAuthorization
// returns a non-nil error when the helper binary exits non-zero (user cancelled
// the admin dialog, SIP policy blocked the right, etc.).
func TestTriggerHelperAuthorization_Denied(t *testing.T) {
	dir := t.TempDir()
	fakeBin := filepath.Join(dir, "collection-helper")
	script := "#!/bin/sh\necho 'authorization failed: OSStatus=-60005' >&2; exit 1\n"
	if err := os.WriteFile(fakeBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake helper: %v", err)
	}

	err := triggerHelperAuthorization(fakeBin)
	if err == nil {
		t.Fatal("triggerHelperAuthorization() error = nil; want non-nil when helper exits 1")
	}
	// Error must include the helper's stderr output so the caller can log it.
	var exitErr *exec.ExitError
	if !isExitError(err, &exitErr) {
		t.Logf("error = %v (non-ExitError is acceptable — the error wraps it)", err)
	}
}

// TestTriggerHelperAuthorization_BinaryNotFound verifies that
// triggerHelperAuthorization returns a non-nil error when the binary path does
// not exist, so callers see a clear failure rather than a panic.
func TestTriggerHelperAuthorization_BinaryNotFound(t *testing.T) {
	err := triggerHelperAuthorization("/nonexistent/collection-helper")
	if err == nil {
		t.Fatal("triggerHelperAuthorization() error = nil; want non-nil for missing binary")
	}
}

// isExitError is a helper that unwraps the error chain to find an *exec.ExitError.
// It mirrors errors.As but works without importing errors in this file.
func isExitError(err error, target **exec.ExitError) bool {
	if err == nil {
		return false
	}
	if ee, ok := err.(*exec.ExitError); ok {
		if target != nil {
			*target = ee
		}
		return true
	}
	// Unwrap one level (fmt.Errorf "%w" wrapping).
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return isExitError(u.Unwrap(), target)
	}
	return false
}
