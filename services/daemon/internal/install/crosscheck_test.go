//go:build !windows

// Package install_test — cross-check test: shell common.sh vs Go Identity()
//
// ADR-049 fitness function 2: reads common.sh with CHANNEL=stable and
// CHANNEL=staging (via bash -c) and asserts each derived constant matches
// the Go-side derivation from internal/install.Identity.
//
// Runs on macOS and Linux CI; skipped on Windows (bash not guaranteed).
package install_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RdHamilton/vault-mtg/services/daemon/internal/install"
	"github.com/stretchr/testify/require"
)

// commonSHPath returns the absolute path to common.sh, resolving from the
// Go source tree.  Uses runtime.Caller to find the location of this test file,
// then walks up to the install/macos directory.
func commonSHPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// This file lives at .../services/daemon/internal/install/crosscheck_test.go
	// common.sh lives at .../services/daemon/install/macos/common.sh
	daemonRoot := filepath.Join(filepath.Dir(file), "..", "..")
	p := filepath.Join(daemonRoot, "install", "macos", "common.sh")
	abs, err := filepath.Abs(p)
	require.NoError(t, err)
	return abs
}

// shellConst sources common.sh with the given CHANNEL and returns the named
// variable's value.  Skips the test if bash is unavailable.
func shellConst(t *testing.T, commonSH, channel, varName string) string {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash not found — skipping shell cross-check: %v", err)
	}
	// source common.sh with the given CHANNEL, then print the requested var
	script := fmt.Sprintf("CHANNEL=%s source '%s' && printf '%%s' \"${%s}\"", channel, commonSH, varName)
	cmd := exec.Command("bash", "-c", script)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("shellConst(%q, %q): bash error: %v\nstderr: %s", channel, varName, err, errBuf.String())
	}
	return strings.TrimSpace(out.String())
}

// TestShellGoAgreement_StableChannel runs the ADR-049 fitness function 2 cross-check
// for the stable channel: shell common.sh constants must match Go Identity() derivation.
func TestShellGoAgreement_StableChannel(t *testing.T) {
	sh := commonSHPath(t)
	if _, err := os.Stat(sh); err != nil {
		t.Skipf("common.sh not found at %q — install scripts not yet in this build: %v", sh, err)
	}

	id := install.Identity(install.ChannelStable)

	cases := []struct {
		shellVar string
		goVal    string
	}{
		{"BINARY_NAME", id.BinaryName},
		{"PLIST_LABEL", id.PlistLabel},
		{"KEYCHAIN_SERVICE", id.KeychainService},
		{"APP_BUNDLE_PATH", id.AppBundlePath},
		{"TRAY_LABEL", id.TrayLabel},
	}

	for _, tc := range cases {
		t.Run(tc.shellVar, func(t *testing.T) {
			shVal := shellConst(t, sh, install.ChannelStable, tc.shellVar)
			if shVal != tc.goVal {
				t.Errorf("CHANNEL=stable: shell %s=%q != Go %q — shell and Go have drifted (ADR-049 §2 violation)",
					tc.shellVar, shVal, tc.goVal)
			}
		})
	}
}

// TestShellGoAgreement_StagingChannel runs the ADR-049 fitness function 2 cross-check
// for the staging channel.
func TestShellGoAgreement_StagingChannel(t *testing.T) {
	sh := commonSHPath(t)
	if _, err := os.Stat(sh); err != nil {
		t.Skipf("common.sh not found at %q — install scripts not yet in this build: %v", sh, err)
	}

	id := install.Identity(install.ChannelStaging)

	cases := []struct {
		shellVar string
		goVal    string
	}{
		{"BINARY_NAME", id.BinaryName},
		{"PLIST_LABEL", id.PlistLabel},
		{"KEYCHAIN_SERVICE", id.KeychainService},
		{"APP_BUNDLE_PATH", id.AppBundlePath},
		{"TRAY_LABEL", id.TrayLabel},
	}

	for _, tc := range cases {
		t.Run(tc.shellVar, func(t *testing.T) {
			shVal := shellConst(t, sh, install.ChannelStaging, tc.shellVar)
			if shVal != tc.goVal {
				t.Errorf("CHANNEL=staging: shell %s=%q != Go %q — shell and Go have drifted (ADR-049 §2 violation)",
					tc.shellVar, shVal, tc.goVal)
			}
		})
	}
}
