//go:build cgo && !darwin

package tray

// ensureUIElementPolicy is a no-op on non-Darwin platforms.
// The launchd spawn-type mismatch that requires NSApplicationActivationPolicyAccessory
// is macOS-specific; Windows and Linux have no equivalent restriction.
func ensureUIElementPolicy() {}

// applyAccessoryPolicy is a no-op on non-Darwin platforms and reports the
// "accessory" policy value so the shared setup() logging in tray.go reads as
// the expected/applied state without a platform branch. The activation policy
// only exists on macOS; Windows and Linux place the tray icon without it.
func applyAccessoryPolicy() int { return activationPolicyAccessory }
