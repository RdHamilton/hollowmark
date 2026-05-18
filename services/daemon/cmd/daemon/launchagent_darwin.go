//go:build darwin

package main

import (
	"log"
	"os/exec"
)

// plistLabel is the LaunchAgent label registered by the installer
// (services/daemon/install/macos/pkg/postinstall and install/macos/uninstall.sh).
// It must stay in sync with those scripts.
// ADR-022 Phase 2: renamed from "com.mtga-companion.daemon" to "com.vaultmtg.daemon".
const plistLabel = "com.vaultmtg.daemon"

// plistLabelLegacy is the pre-rename LaunchAgent label.  The installer detects
// and unloads this label before registering plistLabel so two daemon instances
// never run simultaneously (ADR-022 Constraint 1).
const plistLabelLegacy = "com.mtga-companion.daemon"

// stopLaunchAgent tells launchd to stop the service so it does not restart
// after the process exits. This must be called before systray.Quit() / cancel().
//
// `launchctl stop` sends SIGTERM to the process and marks the job as stopped
// intentionally, preventing launchd from immediately respawning it per the
// KeepAlive=true directive in the plist. The agent is still registered and will
// restart on the next user login — this is the correct "Quit" semantic (stop
// now, not never-start-again).
//
// Failure is non-fatal: if launchctl is unavailable or the job is not loaded,
// the error is logged and the quit sequence continues.
//
// ADR-022 Phase 2: also attempts to stop the legacy label (plistLabelLegacy)
// in case an upgrade scenario left the old registration active. This is a
// best-effort no-op on machines that have already been migrated.
func stopLaunchAgent() {
	cmd := exec.Command("launchctl", "stop", plistLabel)
	if err := cmd.Run(); err != nil {
		log.Printf("[vaultmtg-daemon] launchctl stop %s: %v (non-fatal)", plistLabel, err)
	}

	// Best-effort: stop any running instance registered under the legacy label.
	// Silently ignore errors — a fully migrated machine has no legacy label.
	_ = exec.Command("launchctl", "stop", plistLabelLegacy).Run()
}
