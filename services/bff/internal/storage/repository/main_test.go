package repository_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage"
)

// TestMain runs the full migration set once before any test in this package
// executes, then hands off to m.Run.
//
// Without this, tests that match "go test -run Integration" (integration.yml)
// run against a fresh empty database container: the migrations embedded in the
// storage package are never applied, so tables like accounts, draft_sessions,
// etc. are absent and the tests fail with "relation … does not exist".
//
// The top-level services/bff TestMain (integration_test.go) is in a separate
// test binary (package bff_integration_test) and therefore does not apply
// migrations for this package's binary.
//
// When DATABASE_URL is absent the process exits 0 (clean skip), matching the
// behaviour of openTestDB, which calls t.Skip in that case.
func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		// No DB configured — all tests in this package call openTestDB, which
		// already calls t.Skip.  Exit 0 so the package is reported as
		// "[no tests to run]" rather than as a failure.
		os.Exit(0)
	}

	if err := storage.RunMigrations(dbURL); err != nil {
		fmt.Fprintf(os.Stderr, "[repository_test] RunMigrations: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}
