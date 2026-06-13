//go:build darwin

// Package main — ADR-059 one-time admin authorization for task_for_pid.
//
// RequestOneTimeAuthorization surfaces the standard macOS admin-password dialog
// for the com.apple.TaskForPid-allow authorization right.  The user completes
// this once on first enhanced-mode enable; subsequent calls return immediately
// if the right is already cached in the system policy database.
//
// Under the new model (ADR-059) the helper binary carries the
// com.apple.security.cs.debugger entitlement (collection-agent-helper.entitlements)
// and runs as the logged-in user — it no longer requires a root LaunchDaemon.
// The task_for_pid call in mem_darwin.go succeeds because:
//
//  1. The signed+notarized binary carries com.apple.security.cs.debugger.
//  2. The user has previously clicked Allow in the admin dialog triggered here.
//
// Re-authorization behavior (ADR-059 §Consequences):
//
//	The one-time grant persists across relaunches and normal macOS updates.
//	After a major macOS upgrade or a clean-slate ~/Library wipe, macOS may
//	clear the cached policy entry and re-prompt once (matching Untapped.gg
//	behavior — verified AC9).  This is accepted behavior; no cross-upgrade
//	persistence engineering is attempted (Ray's Q3 ruling, 2026-06-13).
package main

/*
#cgo LDFLAGS: -framework Security
#include <Security/Authorization.h>
#include <stdlib.h>

// authorizeCopyRights calls AuthorizationCopyRights requesting
// com.apple.TaskForPid-allow with kAuthorizationFlagInteractionAllowed |
// kAuthorizationFlagExtendRights so the system admin-password dialog appears
// when the right is not yet cached.
//
// Returns errAuthorizationDenied (−60005) when the user cancels, or
// errAuthorizationSuccess (0) on success.
static OSStatus authorizeCopyRights(void) {
    AuthorizationRef authRef = NULL;
    OSStatus status = AuthorizationCreate(NULL, kAuthorizationEmptyEnvironment,
                                          kAuthorizationFlagDefaults, &authRef);
    if (status != errAuthorizationSuccess) {
        return status;
    }

    AuthorizationItem item = {
        .name  = "com.apple.TaskForPid-allow",
        .valueLength = 0,
        .value = NULL,
        .flags = 0,
    };
    AuthorizationRights rights = {
        .count = 1,
        .items = &item,
    };
    AuthorizationFlags flags =
        kAuthorizationFlagDefaults
        | kAuthorizationFlagInteractionAllowed
        | kAuthorizationFlagExtendRights
        | kAuthorizationFlagPreAuthorize;

    status = AuthorizationCopyRights(authRef, &rights,
                                     kAuthorizationEmptyEnvironment,
                                     flags, NULL);
    AuthorizationFree(authRef, kAuthorizationFlagDestroyRights);
    return status;
}
*/
import "C"

import (
	"errors"
	"fmt"
)

// errAuthDenied is the sentinel returned when the user cancels the admin dialog
// or the system policy denies the com.apple.TaskForPid-allow right.
var errAuthDenied = errors.New("com.apple.TaskForPid-allow authorization denied or cancelled by user")

// authorizeTaskForPid is the authorization function called by
// RequestOneTimeAuthorization.  It is a package-level variable so tests can
// override it without CGO.
//
// The default implementation calls authorizeCopyRights (CGO → Security.framework).
var authorizeTaskForPid = func() error {
	status := C.authorizeCopyRights()
	if status != 0 {
		return fmt.Errorf("%w (OSStatus=%d)", errAuthDenied, int(status))
	}
	return nil
}

// RequestOneTimeAuthorization presents the macOS admin-password dialog for the
// com.apple.TaskForPid-allow authorization right.  It returns nil if the user
// clicks Allow (or if the right was already cached from a prior session), or a
// non-nil error wrapping errAuthDenied if the user cancels or the policy
// database denies the request.
//
// Callers should invoke this once on first enhanced-mode enable, before the
// first call to scanProcess.  Subsequent calls are no-ops when the right is
// already cached (kAuthorizationFlagPreAuthorize behavior).
func RequestOneTimeAuthorization() error {
	return authorizeTaskForPid()
}
