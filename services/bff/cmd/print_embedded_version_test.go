package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage"
)

// TestPrintEmbeddedVersionFlag exercises the --print-embedded-version flag via
// a subprocess: builds the binary, runs it with the flag set (no env/SSM),
// and asserts it:
//   - exits 0
//   - prints the bare integer returned by storage.EmbeddedMaxVersion() to stdout
//   - prints nothing else (no BFF startup noise)
//
// This is the regression test that proves the mid-deploy invocation used by
// restart-bff.sh works without a DB connection or any SSM/env configuration
// (ticket #1151).
func TestPrintEmbeddedVersionFlag(t *testing.T) {
	t.Helper()

	// Build the binary into a temp dir.
	dir := t.TempDir()
	binary := filepath.Join(dir, "mtga-bff-test")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = "/tmp/claude-worktrees/agent-ae4ed16c9c733271d/services/bff/cmd"
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	// Determine the expected version from storage — same call the flag impl will use.
	expectedVersion, err := storage.EmbeddedMaxVersion()
	if err != nil {
		t.Fatalf("storage.EmbeddedMaxVersion: %v", err)
	}

	// Run the binary with --print-embedded-version and NO env vars set (isolation
	// test — must work mid-deploy with no SSM/DB).
	cmd := exec.Command(binary, "--print-embedded-version")
	cmd.Env = []string{} // deliberately empty — no DATABASE_URL, no CLERK_*, no SSM
	out, err := cmd.Output()
	// Expect exit 0.
	if err != nil {
		t.Fatalf("--print-embedded-version exited non-zero: %v\nstdout: %s", err, out)
	}

	// Expect stdout to be exactly the bare integer (trimming any trailing newline).
	got := strings.TrimSpace(string(out))
	expected := fmt.Sprintf("%d", expectedVersion)
	if got != expected {
		t.Fatalf("--print-embedded-version printed %q; want bare integer %q", got, expected)
	}

	// Parseable as a non-zero integer sanity check.
	n, parseErr := strconv.Atoi(got)
	if parseErr != nil {
		t.Fatalf("output %q is not a valid integer: %v", got, parseErr)
	}
	if n == 0 {
		t.Fatalf("embedded version printed as 0 — EmbeddedMaxVersion returned 0, which is unexpected")
	}
}
