//go:build cgo && !darwin

package tray

// ensureUIElementPolicy is a no-op on non-Darwin platforms.
// The launchd spawn-type mismatch that requires NSApplicationActivationPolicyAccessory
// is macOS-specific; Windows and Linux have no equivalent restriction.
func ensureUIElementPolicy() {}
