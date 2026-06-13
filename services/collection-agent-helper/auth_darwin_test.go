//go:build darwin

package main

import (
	"errors"
	"testing"
)

// testSetAuthorizeFunc swaps authorizeTaskForPid for fn for the duration of
// the test and restores the original in t.Cleanup. Must NOT be called from
// parallel subtests that share a parent — callers must omit t.Parallel() to
// prevent a data race on the package-level variable.
func testSetAuthorizeFunc(t *testing.T, fn func() error) {
	t.Helper()
	orig := authorizeTaskForPid
	authorizeTaskForPid = fn
	t.Cleanup(func() { authorizeTaskForPid = orig })
}

// TestRequestOneTimeAuthorization_PermissionDenied verifies that when the
// underlying authorization function returns a permission-denied error (e.g.
// the user cancelled the admin dialog, or SIP policies block the right),
// RequestOneTimeAuthorization propagates a non-nil error that wraps errAuthDenied.
//
// In CI and unit-test runs the test process does not hold
// com.apple.TaskForPid-allow, so the stub path is exercised.  The real
// authorization dialog fires only in an interactive macOS session after the
// user clicks Allow — that path is covered by the manual AC9 gate.
//
// Not t.Parallel(): swaps the package-level authorizeTaskForPid variable.
func TestRequestOneTimeAuthorization_PermissionDenied(t *testing.T) {
	testSetAuthorizeFunc(t, func() error { return errAuthDenied })

	err := RequestOneTimeAuthorization()
	if err == nil {
		t.Fatal("expected non-nil error when authorization is denied; got nil")
	}
	if !errors.Is(err, errAuthDenied) {
		t.Errorf("expected error to wrap errAuthDenied; got %v", err)
	}
}

// TestRequestOneTimeAuthorization_Success verifies that when the underlying
// authorization function succeeds (user clicked Allow), RequestOneTimeAuthorization
// returns nil.
//
// Not t.Parallel(): swaps the package-level authorizeTaskForPid variable.
func TestRequestOneTimeAuthorization_Success(t *testing.T) {
	testSetAuthorizeFunc(t, func() error { return nil })

	if err := RequestOneTimeAuthorization(); err != nil {
		t.Errorf("expected nil error on successful authorization; got %v", err)
	}
}

// TestRequestOneTimeAuthorization_AlreadyGranted verifies that a second call
// (simulating "already authorized from a previous session") is a no-op that
// returns nil without re-prompting. This models the one-time semantics: once
// the authorization right is in the system policy database the dialog is not
// shown again (ADR-059 §Consequences / AC7).
//
// In the real implementation, AuthorizationCopyRights with kAuthorizationFlagPreAuthorize
// returns errSecSuccess when the right is already cached — simulated here by
// the success stub.
//
// Not t.Parallel(): swaps the package-level authorizeTaskForPid variable.
func TestRequestOneTimeAuthorization_AlreadyGranted(t *testing.T) {
	callCount := 0
	testSetAuthorizeFunc(t, func() error {
		callCount++
		return nil // already granted — no dialog
	})

	for i := range 2 {
		if err := RequestOneTimeAuthorization(); err != nil {
			t.Errorf("call %d: expected nil; got %v", i+1, err)
		}
	}
	if callCount != 2 {
		t.Errorf("expected authorizeTaskForPid called 2 times; got %d", callCount)
	}
}

// TestErrAuthDenied_Message checks the sentinel error carries a useful message
// so log output is actionable without a stack trace.
// Safe to run in parallel — reads a package-level constant, no mutation.
func TestErrAuthDenied_Message(t *testing.T) {
	t.Parallel()

	msg := errAuthDenied.Error()
	if msg == "" {
		t.Error("errAuthDenied.Error() must not be empty")
	}
}
