//go:build windows

package updatecheck

import (
	"fmt"
	"os/exec"
)

// launchInstaller launches a Windows NSIS installer via cmd /c start.
// The mandatory "" title argument is required by cmd's start command; without it,
// start interprets the quoted path as a window title and the binary never runs.
//
// Windows SmartScreen note: this binary is UNSIGNED (Azure signing is blocked —
// vmt-t#255). The tray notification shown before this call displays an
// "unverified by Microsoft (beta)" warning so the user is not surprised by
// the SmartScreen dialog.
//
// NSIS installer kill semantics: the installer will kill the running daemon via
// `schtasks /End` mid-install to replace the binary. This is intentional — not
// a bug. The daemon should NOT attempt to wait for or monitor the installer
// process after launch.
func launchInstaller(installerPath string) error {
	args := BuildWindowsLaunchArgs(installerPath)
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start installer: %w", err)
	}
	return nil
}
