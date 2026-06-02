//go:build !darwin && !windows

package updatecheck

import "fmt"

// launchInstaller is unsupported on non-macOS, non-Windows platforms.
func launchInstaller(installerPath string) error {
	return fmt.Errorf("LaunchInstaller: unsupported platform")
}
