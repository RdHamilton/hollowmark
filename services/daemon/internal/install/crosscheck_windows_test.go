//go:build !darwin && !windows

// Package install_test — cross-check test: Windows common.ps1 vs Go Identity()
//
// ADR-049 fitness function 2 (Windows variant): reads common.ps1 with
// -Channel stable and -Channel staging (via pwsh) and asserts each derived
// constant matches the Go-side derivation from internal/install.Identity.
//
// Runs on Linux CI (ubuntu-latest) where pwsh (PowerShell Core) is available.
// Skipped on Darwin (pwsh not guaranteed) and skipped at runtime if pwsh is
// absent from PATH.
//
// Note: the credential-related constants crosschecked here are:
//   - $CredService — the bare service name used as the go-keyring target prefix.
//     Must equal Identity(channel).KeychainService.
//
// This file is the Windows-side counterpart to crosscheck_test.go (which covers
// common.sh / macOS/Linux bash constants).
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

	"github.com/RdHamilton/hollowmark/services/daemon/internal/install"
	"github.com/stretchr/testify/require"
)

// commonPS1Path returns the absolute path to common.ps1, resolving from the
// Go source tree via runtime.Caller.
func commonPS1Path(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	// This file lives at .../services/daemon/internal/install/crosscheck_windows_test.go
	// common.ps1 lives at .../services/daemon/install/windows/common.ps1
	daemonRoot := filepath.Join(filepath.Dir(file), "..", "..")
	p := filepath.Join(daemonRoot, "install", "windows", "common.ps1")
	abs, err := filepath.Abs(p)
	require.NoError(t, err)
	return abs
}

// pwshConst sources common.ps1 with the given -Channel and returns the named
// PowerShell variable's value. Skips the test if pwsh is unavailable.
func pwshConst(t *testing.T, commonPS1, channel, varName string) string {
	t.Helper()
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skipf("pwsh not found in PATH — skipping Windows common.ps1 cross-check: %v", err)
	}
	// Dot-source common.ps1 with the given channel, then print the requested variable.
	script := fmt.Sprintf(`. '%s' -Channel '%s'; Write-Host -NoNewline $%s`, commonPS1, channel, varName)
	cmd := exec.Command(pwsh, "-NonInteractive", "-NoProfile", "-Command", script)
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		t.Fatalf("pwshConst(%q, %q): pwsh error: %v\nstderr: %s", channel, varName, err, errBuf.String())
	}
	return strings.TrimSpace(out.String())
}

// TestShellGoAgreement_Windows_StableChannel verifies that the Windows common.ps1
// $CredService constant matches Go Identity(ChannelStable).KeychainService.
//
// $CredService is the go-keyring service name prefix stored in Windows Credential
// Manager (target = "$CredService:api-key"). After ADR-022 Phase 3 it must equal
// "com.hollowmark.daemon" (stable) / "com.hollowmark.daemon.staging" (staging).
func TestShellGoAgreement_Windows_StableChannel(t *testing.T) {
	ps1 := commonPS1Path(t)
	if _, err := os.Stat(ps1); err != nil {
		t.Skipf("common.ps1 not found at %q — install scripts not present in this build: %v", ps1, err)
	}

	id := install.Identity(install.ChannelStable)

	cases := []struct {
		psVar string
		goVal string
	}{
		// ADR-022 Phase 3: $CredService must advance to hollowmark namespace.
		{"CredService", id.KeychainService},
	}

	for _, tc := range cases {
		t.Run(tc.psVar, func(t *testing.T) {
			psVal := pwshConst(t, ps1, install.ChannelStable, tc.psVar)
			if psVal != tc.goVal {
				t.Errorf("Channel=stable: common.ps1 $%s=%q != Go KeychainService=%q — common.ps1 and Go have drifted (ADR-049 §2 / ADR-022 Phase 3)",
					tc.psVar, psVal, tc.goVal)
			}
		})
	}
}

// TestShellGoAgreement_Windows_StagingChannel verifies the staging channel variant.
func TestShellGoAgreement_Windows_StagingChannel(t *testing.T) {
	ps1 := commonPS1Path(t)
	if _, err := os.Stat(ps1); err != nil {
		t.Skipf("common.ps1 not found at %q — install scripts not present in this build: %v", ps1, err)
	}

	id := install.Identity(install.ChannelStaging)

	cases := []struct {
		psVar string
		goVal string
	}{
		{"CredService", id.KeychainService},
	}

	for _, tc := range cases {
		t.Run(tc.psVar, func(t *testing.T) {
			psVal := pwshConst(t, ps1, install.ChannelStaging, tc.psVar)
			if psVal != tc.goVal {
				t.Errorf("Channel=staging: common.ps1 $%s=%q != Go KeychainService=%q — common.ps1 and Go have drifted (ADR-049 §2 / ADR-022 Phase 3)",
					tc.psVar, psVal, tc.goVal)
			}
		})
	}
}
