//go:build cgo

package tray

// Fork-swap guard (getlantern/systray v1.2.2 -> fyne.io/systray v1.11.0).
//
// The swap to the fyne.io/systray hard-fork relies entirely on the public API
// being source-compatible with the abandoned getlantern package — same package
// name (systray), same top-level funcs, same *MenuItem type and methods that
// tray.go drives. There is no behavioral Go-level contract to assert (the
// NSStatusItem render path is native and headless-untestable), so the contract
// this swap depends on IS the API surface.
//
// This file compiles ONLY against fyne.io/systray. If anyone reverts the
// dependency to a module that does not expose this exact surface — or removes
// it — the daemon build (and this test package) fails to compile, which is the
// guard. It is deliberately a compile-time assertion, not a runtime test:
// invoking these would require systray.Run on the main thread, which the
// headless test runner cannot do.

import (
	"testing"

	"fyne.io/systray"
)

// _forkTopLevelAPI references every top-level systray function tray.go calls.
// Taking the function values (no invocation) forces the compiler to resolve
// each symbol against fyne.io/systray; a missing or renamed symbol is a build
// failure.
var _forkTopLevelAPI = []any{
	systray.Run,
	systray.Quit,
	systray.SetIcon,
	systray.SetTitle,
	systray.SetTooltip,
	systray.AddMenuItem,
	systray.AddSeparator,
}

// _forkMenuItemAPI references every *systray.MenuItem method tray.go drives.
// A method removed or renamed by a future fork bump breaks this and the
// daemon build together, surfacing the regression in CI rather than at the
// menu bar.
func _forkMenuItemAPI(mi *systray.MenuItem) {
	_ = mi.ClickedCh
	mi.SetTitle("")
	mi.SetTooltip("")
	mi.SetIcon(nil)
	mi.Hide()
	mi.Show()
	mi.Disable()
}

// TestTrayFork_PublicAPIContract is a no-op runtime assertion; the real guard
// is that this file compiles against fyne.io/systray. The test exists so the
// intent is greppable and so `go test` reports the fork-contract package as
// covered rather than the references being flagged as unused.
func TestTrayFork_PublicAPIContract(t *testing.T) {
	if len(_forkTopLevelAPI) == 0 {
		t.Fatal("fork top-level API reference set is empty")
	}
	// _forkMenuItemAPI is referenced here only to keep it live without calling
	// any method on a nil *MenuItem (which would panic / require systray.Run).
	_ = _forkMenuItemAPI
}
