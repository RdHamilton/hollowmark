package analytics_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestTaxonomyNotStale verifies that the checked-in generated files
// (taxonomy.gen.go and frontend/src/services/analytics-taxonomy.gen.ts) are
// up-to-date with the YAML source.  Runs both codegen scripts and diffs the
// output against the committed files.  Fails with a clear message when they
// diverge, matching the CI `make check-taxonomy-stale` gate.
//
// This test is the Go-native staleness gate; the Makefile/CI target is the
// identical check run outside the test binary.  Both must pass.
func TestTaxonomyNotStale(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping taxonomy staleness check in -short mode")
	}

	// Derive the repo root from this test file's location — environment-independent.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	// thisFile = .../services/bff/internal/analytics/taxonomy_stale_test.go
	// filepath.Join(thisFile, "..") = analytics dir
	// Walk up 5 total: analytics -> internal -> bff -> services -> repoRoot
	repoRoot := filepath.Clean(filepath.Join(thisFile, "..", "..", "..", "..", ".."))

	// Run the Go codegen script and capture its output.
	goScript := filepath.Join(repoRoot, "scripts", "gen-analytics-taxonomy.go")
	goCmd := exec.Command("go", "run", goScript, "--stdout")
	goCmd.Dir = repoRoot
	goOut, goErr := goCmd.Output()
	if goErr != nil {
		t.Fatalf("go codegen script failed: %v\nstdout: %s\nstderr: %s",
			goErr, goOut, exitErrStderr(goErr))
	}

	// Compare with checked-in taxonomy.gen.go.
	committed := filepath.Join(repoRoot, "services", "bff", "internal", "analytics", "taxonomy.gen.go")
	committedContent, err := os.ReadFile(committed)
	if err != nil {
		t.Fatalf("cannot read committed taxonomy.gen.go: %v", err)
	}
	if !bytes.Equal(goOut, committedContent) {
		t.Error("taxonomy.gen.go is stale — run: make gen-taxonomy")
	}

	// Run the TS codegen script and capture its output.
	tsScript := filepath.Join(repoRoot, "scripts", "gen-analytics-taxonomy.ts")
	tsCmd := exec.Command("npx", "tsx", tsScript, "--stdout")
	tsCmd.Dir = repoRoot
	tsOut, tsErr := tsCmd.Output()
	if tsErr != nil {
		t.Fatalf("ts codegen script failed: %v\nstdout: %s\nstderr: %s",
			tsErr, tsOut, exitErrStderr(tsErr))
	}

	// Compare with checked-in analytics-taxonomy.gen.ts.
	committedTS := filepath.Join(repoRoot, "frontend", "src", "services", "analytics-taxonomy.gen.ts")
	committedTSContent, err := os.ReadFile(committedTS)
	if err != nil {
		t.Fatalf("cannot read committed analytics-taxonomy.gen.ts: %v", err)
	}
	if !bytes.Equal(tsOut, committedTSContent) {
		t.Error("analytics-taxonomy.gen.ts is stale — run: make gen-taxonomy")
	}
}

// exitErrStderr extracts stderr from exec.ExitError.
func exitErrStderr(err error) []byte {
	var ee *exec.ExitError
	if ok := false; !ok {
		_ = ok
	}
	if ee2, ok2 := err.(*exec.ExitError); ok2 {
		ee = ee2
	}
	if ee == nil {
		return nil
	}
	return ee.Stderr
}
