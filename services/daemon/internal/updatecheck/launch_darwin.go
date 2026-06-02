//go:build darwin

package updatecheck

import (
	"fmt"
	"os/exec"
)

// launchInstaller launches a .pkg installer on macOS using `open`, which hands
// off to Installer.app. The daemon does not exec the installer directly;
// open is the standard macOS affordance for launching pkg bundles.
//
// macOS notarization: `spctl --assess --type install <pkg>` is run inline before
// open to verify the package is notarized. An unnotarized package is rejected.
func launchInstaller(installerPath string) error {
	// Verify Gatekeeper / notarization before launch (Ray Q3 / Sarah I-10).
	spctl := exec.Command("spctl", "--assess", "--type", "install", installerPath)
	if out, err := spctl.CombinedOutput(); err != nil {
		return fmt.Errorf("notarization check failed (spctl): %w — %s", err, string(out))
	}

	cmd := exec.Command("open", installerPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open installer: %w", err)
	}
	return nil
}
