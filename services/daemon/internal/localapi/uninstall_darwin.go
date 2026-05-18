//go:build darwin

package localapi

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runPlatformUninstall performs the macOS uninstall steps.
//
// ADR-022 Phase 2 — label / path reconciliation:
//   - Primary plist label is now "com.vaultmtg.daemon".
//   - Legacy label "com.mtga-companion.daemon" is also unloaded when present
//     to handle upgrades from the old daemon without leaving orphaned launchd jobs.
//   - Config-dir purge targets ~/.vaultmtg (the canonical new path).
//     Legacy candidates (~/.mtga-companion, ~/.config/mtga-companion) are also
//     removed when purge=true so no stale directories are left behind.
//
// Order: launchctl unload first (stops the supervised process + deregisters the
// job), then plist removal (prevents re-registration at next login).  Reversing
// the order leaves the job orphaned in launchd's in-memory state.
func runPlatformUninstall(purge bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}

	// ── Primary label (current brand) ────────────────────────────────────────
	plistLabel := "com.vaultmtg.daemon"
	plistPath := filepath.Join(home, "Library", "LaunchAgents", plistLabel+".plist")

	if err := unloadPlist(plistLabel, plistPath); err != nil {
		return "", err
	}
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("remove plist %s: %w", plistPath, err)
	}

	// ── Legacy label (pre-rename) — unload if still registered ───────────────
	// This handles the upgrade scenario where the user had the old daemon
	// installed and is now running the new uninstaller.  Without this step the
	// old label would remain registered with launchd and the old binary would
	// keep running.
	legacyLabel := "com.mtga-companion.daemon"
	legacyPlistPath := filepath.Join(home, "Library", "LaunchAgents", legacyLabel+".plist")
	// Silently ignore errors — the legacy label may never have been installed.
	_ = unloadPlist(legacyLabel, legacyPlistPath)
	_ = os.Remove(legacyPlistPath) // idempotent; ignore missing-file errors

	if purge {
		// ── Primary config dir (new brand) ───────────────────────────────────
		newConfigDir := filepath.Join(home, ".vaultmtg")
		if err := os.RemoveAll(newConfigDir); err != nil {
			return "", fmt.Errorf("remove config dir %s: %w", newConfigDir, err)
		}

		// ── Legacy config dirs (old brand) ───────────────────────────────────
		// ~/.mtga-companion — the actual old location used by pre-v0.3.2 daemons.
		// ~/.config/mtga-companion — erroneous path that appeared in a prior version
		// of this file (see #1761 path-inconsistency note); cleaned up defensively.
		legacyCandidates := []string{
			filepath.Join(home, ".mtga-companion"),
			filepath.Join(home, ".config", "mtga-companion"),
		}
		for _, dir := range legacyCandidates {
			if err := os.RemoveAll(dir); err != nil {
				return "", fmt.Errorf("remove legacy config dir %s: %w", dir, err)
			}
		}
	}

	msg := "Daemon stopped and removed from launchd. Drag VaultMTG to the Trash to remove the app bundle."
	if purge {
		msg = "Daemon stopped, removed from launchd, and config wiped. Drag VaultMTG to the Trash to remove the app bundle."
	}
	return msg, nil
}

// unloadPlist calls `launchctl unload -w <plistPath>` to stop the launchd job
// identified by plistLabel.  It returns nil when the job was never loaded or
// the plist is already gone — those cases are idempotent non-errors.
func unloadPlist(plistLabel, plistPath string) error {
	// `launchctl unload` exits non-zero in two distinct cases:
	//   - job was never loaded / plist already gone  → treat as no-op
	//   - real failure (permission, malformed plist) → propagate
	// We disambiguate by inspecting stderr rather than the exit code,
	// because launchctl uses the same non-zero code for both.
	cmd := exec.Command("launchctl", "unload", "-w", plistPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		out := strings.ToLower(stderr.String())
		switch {
		case strings.Contains(out, "no such file"),
			strings.Contains(out, "not loaded"),
			strings.Contains(out, "could not find"):
			// Idempotent — the job wasn't registered. Continue.
			return nil
		default:
			return fmt.Errorf("launchctl unload %s (%s): %w (stderr: %s)",
				plistLabel, plistPath, err, strings.TrimSpace(stderr.String()))
		}
	}
	return nil
}
