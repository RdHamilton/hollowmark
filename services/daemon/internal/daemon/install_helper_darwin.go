//go:build darwin

package daemon

import (
	"fmt"
	"os/exec"
)

// triggerHelperAuthorization invokes the collection-helper binary with the
// --authorize flag, which calls RequestOneTimeAuthorization() inside the helper
// (AuthorizationCopyRights requesting com.apple.TaskForPid-allow) and exits.
//
// This surfaces the standard macOS admin-password dialog once, at first
// enhanced-mode enable.  Subsequent calls are no-ops because the authorization
// right is cached in the system policy database after the first Allow.
//
// Under ADR-059 no root-level install script (install-helper.sh / osascript)
// is used.  The helper binary already carries com.apple.security.cs.debugger
// (signed at build time); the only action required here is obtaining the
// com.apple.TaskForPid-allow right.
//
// helperBinary must be the path to the signed, notarized collection-helper binary
// (resolved by locateHelperBinary from SHARE_DIR or MTGA_COLLECTION_HELPER_DIR).
func triggerHelperAuthorization(helperBinary string) error {
	out, err := exec.Command(helperBinary, "--authorize").CombinedOutput()
	if err != nil {
		return fmt.Errorf("collection-helper --authorize: %w — %s", err, string(out))
	}
	return nil
}
