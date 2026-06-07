package storage_test

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"

	// pgx stdlib driver must be imported here so that sql.Open("pgx", ...) works
	// in package-isolated test runs.  This mirrors the precedent in
	// draft_ratings_repo_test.go which has the same blank import.
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage"
)

// openTestDBForStorage opens a real PostgreSQL connection using DATABASE_URL.
// The test is skipped when that variable is not set, matching the pattern used
// in repository integration tests.
func openTestDBForStorage(t *testing.T) *sql.DB {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestCheckBinaryAheadOfDB_DBUnreachable verifies that an unreachable/invalid
// database URL causes CheckBinaryAheadOfDB to return nil (fail-open), per the
// spec: "If DB unreachable / table absent (query errors) → return nil".
func TestCheckBinaryAheadOfDB_DBUnreachable(t *testing.T) {
	// Use a guaranteed-unreachable DSN — nothing listens on 127.0.0.1:1.
	err := storage.CheckBinaryAheadOfDB("postgres://user:pass@127.0.0.1:1/dbname?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Errorf("expected nil (fail-open) for unreachable DB, got: %v", err)
	}
}

// TestCheckBinaryAheadOfDB_TableAbsent verifies that a reachable DB that lacks
// the schema_migrations table also returns nil (fail-open).
// This test requires a live DB and is skipped when DATABASE_URL is not set.
func TestCheckBinaryAheadOfDB_TableAbsent(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	// Use a different database that won't have schema_migrations.
	// We connect to the "postgres" system database which has no migrations.
	// Replace the database name in the DSN.
	pgDSN := replaceDatabaseInDSN(dsn, "postgres")
	if pgDSN == dsn {
		t.Skip("could not construct a postgres-DB DSN — skipping table-absent test")
	}

	err := storage.CheckBinaryAheadOfDB(pgDSN)
	if err != nil {
		t.Errorf("expected nil (fail-open) when schema_migrations absent, got: %v", err)
	}
}

// TestCheckBinaryAheadOfDB_DBAtBinaryVersion verifies that when the DB version
// equals the embedded binary version, CheckBinaryAheadOfDB returns nil (no error).
// This is the happy path — a normal, up-to-date deployment.
// Requires DATABASE_URL pointing at a database that has had migrations applied.
func TestCheckBinaryAheadOfDB_DBAtBinaryVersion(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	err := storage.CheckBinaryAheadOfDB(dsn)
	if err != nil {
		// In CI the DB is freshly migrated so db == binary version.
		// If err contains "binary" it means db > binary, which indicates
		// the test DB is ahead — a real misconfiguration to surface.
		t.Errorf("unexpected error for up-to-date DB: %v", err)
	}
}

// TestCheckBinaryAheadOfDB_DBVersionAheadOfBinary verifies that when the DB
// schema_migrations.version exceeds the embedded binary's max version, the
// function returns a non-nil error containing both version numbers.
// This exercises the primary guard: a rolled-back binary against a migrated DB.
// Requires a live DB (DATABASE_URL).
func TestCheckBinaryAheadOfDB_DBVersionAheadOfBinary(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	// Determine the real embedded max version so we can write a fake higher version.
	binaryMax, err := storage.EmbeddedMaxVersion()
	if err != nil {
		t.Fatalf("EmbeddedMaxVersion: %v", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()

	fakeVersion := int(binaryMax) + 9999

	// Insert a fake future migration row.
	_, insertErr := db.Exec(
		"INSERT INTO schema_migrations (version, dirty) VALUES ($1, false)",
		fakeVersion,
	)
	if insertErr != nil {
		t.Fatalf("insert fake migration: %v", insertErr)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DELETE FROM schema_migrations WHERE version = $1", fakeVersion)
	})

	gotErr := storage.CheckBinaryAheadOfDB(dsn)
	if gotErr == nil {
		t.Fatal("expected non-nil error when DB version > binary version, got nil")
	}

	msg := gotErr.Error()
	// Error must mention both version numbers so the operator knows exactly what happened.
	if !strings.Contains(msg, fmt.Sprintf("%d", fakeVersion)) {
		t.Errorf("error %q does not contain DB version %d", msg, fakeVersion)
	}
	if !strings.Contains(msg, fmt.Sprintf("%d", binaryMax)) {
		t.Errorf("error %q does not contain binary version %d", msg, binaryMax)
	}
}

// replaceDatabaseInDSN replaces the database component of a postgres:// DSN.
// Returns the original DSN unchanged if parsing fails.
func replaceDatabaseInDSN(dsn, newDB string) string {
	// Simple replacement: find the last path segment before any '?' query string.
	// postgres://user:pass@host:port/dbname?sslmode=require
	//                                      ^^^^^^^^^
	qIdx := strings.Index(dsn, "?")
	base := dsn
	query := ""
	if qIdx >= 0 {
		base = dsn[:qIdx]
		query = dsn[qIdx:]
	}
	slashIdx := strings.LastIndex(base, "/")
	if slashIdx < 0 {
		return dsn
	}
	return base[:slashIdx+1] + newDB + query
}
