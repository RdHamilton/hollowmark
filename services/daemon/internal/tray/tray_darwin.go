//go:build cgo && darwin

package tray

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// setUIElementPolicy promotes the calling process to NSApplicationActivationPolicyAccessory.
//
// Why this is needed:
// When the VaultMTG daemon is launched by launchd from a LaunchAgent whose
// Program/ProgramArguments key points at a bare executable (not a .app bundle),
// launchd assigns spawn-type "daemon" (type 3). Processes in spawn-type daemon do
// not get full Aqua / WindowServer session access; NSStatusBar systemStatusBar
// cannot place a menu-bar status item and silently drops the icon.
//
// Calling setActivationPolicy:NSApplicationActivationPolicyAccessory before
// [NSApp run] (i.e., before systray.Run) transitions the process into the
// UIElement activation policy.  UIElement processes:
//   - gain access to the WindowServer session (status bar works)
//   - do NOT get a Dock icon (Accessory policy, not Regular)
//   - do NOT show an "App" menu bar (correct for a background daemon)
//
// This is the canonical pattern recommended by Apple for non-bundled menu-bar
// agents (TSI/DTS confirmed; also used by popular status-bar apps such as
// Bartender and iStat Menus when running outside a bundle).
//
// Calling this function on a headless machine (no WindowServer, e.g. SSH,
// headless CI) is a safe no-op: +sharedApplication succeeds but the policy
// change has no visible effect.
static void setUIElementPolicy(void) {
    NSApplication *app = [NSApplication sharedApplication];
    [app setActivationPolicy:NSApplicationActivationPolicyAccessory];
}
*/
import "C"

// ensureUIElementPolicy promotes the current process to NSApplicationActivationPolicyAccessory
// so that NSStatusBar can place a menu-bar icon when the daemon is launched by
// launchd with spawn type "daemon" (a bare executable LaunchAgent, not a .app bundle).
//
// Must be called on the main OS thread, before systray.Run().
// On headless machines (no WindowServer) the call is a silent no-op.
// On non-Darwin platforms this file is not compiled; see tray_nondarwin.go.
func ensureUIElementPolicy() {
	C.setUIElementPolicy()
}
