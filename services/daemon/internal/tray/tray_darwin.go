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
// HISTORICAL NOTE (do not remove the function — see applyAccessoryPolicy below):
// Calling setActivationPolicy *before* [NSApp run] (i.e., before systray.Run)
// is unreliable on macOS 13–15: a policy set that early — before
// applicationDidFinishLaunching and the NSApplication run loop is established —
// is silently dropped, leaving the process effectively .prohibited. A
// .prohibited process cannot place an NSStatusItem, so the icon is created but
// never becomes visible. This early call is retained only as a best-effort
// first attempt; the AUTHORITATIVE policy set now happens inside the systray
// onReady window (applyAccessoryPolicy), once the run loop is up.
//
// Calling this function on a headless machine (no WindowServer, e.g. SSH,
// headless CI) is a safe no-op: +sharedApplication succeeds but the policy
// change has no visible effect.
static void setUIElementPolicy(void) {
    NSApplication *app = [NSApplication sharedApplication];
    [app setActivationPolicy:NSApplicationActivationPolicyAccessory];
}

// applyAccessoryPolicy sets NSApplicationActivationPolicyAccessory and then
// activates the app, returning the resulting NSApplicationActivationPolicy as
// a long so the Go caller can log whether the policy actually took effect.
//
// This must be called from inside the systray onReady callback (i.e., during /
// after applicationDidFinishLaunching, on the main OS thread, with the
// NSApplication run loop already running). At that point setActivationPolicy
// is honored on macOS 13–15 and the process transitions to the UIElement
// (Accessory) policy that lets NSStatusBar place a menu-bar item.
//
// Returns app.activationPolicy after the set so the caller can confirm the
// change landed:
//   NSApplicationActivationPolicyRegular    = 0
//   NSApplicationActivationPolicyAccessory  = 1  (desired)
//   NSApplicationActivationPolicyProhibited = 2  (icon will NOT render)
//
// On a headless machine (no WindowServer) the set is a safe no-op; the returned
// policy reflects whatever the session allows.
static long applyAccessoryPolicy(void) {
    NSApplication *app = [NSApplication sharedApplication];
    [app setActivationPolicy:NSApplicationActivationPolicyAccessory];
    [app activateIgnoringOtherApps:YES];
    return (long)[app activationPolicy];
}
*/
import "C"

// ensureUIElementPolicy promotes the current process to NSApplicationActivationPolicyAccessory
// as a best-effort first attempt before systray.Run().
//
// IMPORTANT: on macOS 13–15 a policy set this early (before the NSApplication
// run loop / applicationDidFinishLaunching) is silently dropped. The
// authoritative set now happens inside the systray onReady window via
// applyAccessoryPolicy (called from App.setup). This pre-Run call is kept only
// as a harmless first attempt and must not be relied on alone.
//
// Must be called on the main OS thread. On headless machines (no WindowServer)
// the call is a silent no-op. On non-Darwin platforms this file is not
// compiled; see tray_nondarwin.go.
func ensureUIElementPolicy() {
	C.setUIElementPolicy()
}

// applyAccessoryPolicy sets the authoritative NSApplicationActivationPolicyAccessory
// from inside the systray onReady window (once the run loop is up), activates the
// app, and returns the resulting activation policy so the caller can log it:
//
//	0 = Regular, 1 = Accessory (desired), 2 = Prohibited (icon will NOT render).
//
// Must be called on the main OS thread from within App.setup (onReady). On
// non-Darwin platforms this file is not compiled; see tray_nondarwin.go.
func applyAccessoryPolicy() int {
	return int(C.applyAccessoryPolicy())
}
