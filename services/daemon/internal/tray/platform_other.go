//go:build cgo && !windows

package tray

func isWindows() bool { return false }
