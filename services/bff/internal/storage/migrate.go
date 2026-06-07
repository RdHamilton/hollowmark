package storage

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// MigrationStatusUpToDate is the value returned by MigrationStatus when the
// database schema is at the latest embedded migration version.
const MigrationStatusUpToDate = "up-to-date"

// MigrationStatusUnknown is returned when the status check fails or the schema
// is behind/dirty.  The caller should degrade gracefully — never return 500.
const MigrationStatusUnknown = "unknown"

// migrationFileRe matches golang-migrate up-migration filenames, e.g.
// "000067_add_daemon_events_projection_columns.up.sql".
var migrationFileRe = regexp.MustCompile(`^(\d+)_.*\.up\.sql$`)

//go:embed all:migrations/postgres
var migrationsFS embed.FS

// RunMigrations applies all pending PostgreSQL migrations embedded in the binary.
// It is a no-op if the schema is already up to date.
func RunMigrations(databaseURL string) error {
	sub, err := fs.Sub(migrationsFS, "migrations/postgres")
	if err != nil {
		return fmt.Errorf("migration source: %w", err)
	}

	src, err := iofs.New(sub, ".")
	if err != nil {
		return fmt.Errorf("migration iofs: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, normalizePgxURL(databaseURL))
	if err != nil {
		return fmt.Errorf("migration init: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migration up: %w", err)
	}
	return nil
}

// MigrationStatus returns MigrationStatusUpToDate if the database is reachable
// and its schema version matches the highest embedded migration version.
// It returns MigrationStatusUnknown if the DB is unreachable, dirty, or behind.
// It never returns an error — callers are expected to degrade gracefully.
func MigrationStatus(databaseURL string) string {
	if databaseURL == "" {
		return MigrationStatusUnknown
	}

	maxVersion, err := embeddedMaxVersion()
	if err != nil {
		return MigrationStatusUnknown
	}

	sub, err := fs.Sub(migrationsFS, "migrations/postgres")
	if err != nil {
		return MigrationStatusUnknown
	}

	src, err := iofs.New(sub, ".")
	if err != nil {
		return MigrationStatusUnknown
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, normalizePgxURL(databaseURL))
	if err != nil {
		return MigrationStatusUnknown
	}

	defer func() { _, _ = m.Close() }()

	// Current applied version in the database.
	current, dirty, err := m.Version()
	if err != nil || dirty {
		return MigrationStatusUnknown
	}

	if current == maxVersion {
		return MigrationStatusUpToDate
	}

	return MigrationStatusUnknown
}

// embeddedMaxVersion returns the highest migration version number found in the
// embedded migrations FS by scanning up-migration filenames.
func embeddedMaxVersion() (uint, error) {
	sub, err := fs.Sub(migrationsFS, "migrations/postgres")
	if err != nil {
		return 0, err
	}

	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return 0, err
	}

	var max uint

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		m := migrationFileRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}

		v, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			continue
		}

		if uint(v) > max {
			max = uint(v)
		}
	}

	if max == 0 {
		return 0, fmt.Errorf("no migration files found")
	}

	return max, nil
}

// EmbeddedMaxVersion returns the highest migration version number available in
// the embedded migrations FS.  Useful for health checks and diagnostics.
func EmbeddedMaxVersion() (uint, error) {
	return embeddedMaxVersion()
}

// CheckBinaryAheadOfDB is a pre-flight guard called at BFF startup, before
// RunMigrations, to detect a rolled-back binary deployed against a database
// that has already been migrated further.
//
// It opens a THROWAWAY connection (separate from the shared pool) solely to
// query schema_migrations, then closes it.  The shared pool is not open yet
// at the call site in cmd/main.go.
//
// Semantics:
//   - db version > binary embedded max version → return error with both numbers
//     (caller should log.Fatalf — this is a hard stop)
//   - db version == binary version, or db version < binary version → return nil
//     (normal operation; RunMigrations handles forward migrations)
//   - DB unreachable, table absent, or any query error → return nil (fail-open)
//     RunMigrations will deal with unreachable DBs via its own retry loop.
func CheckBinaryAheadOfDB(databaseURL string) error {
	binaryMax, err := embeddedMaxVersion()
	if err != nil {
		// Cannot determine the binary's own version — fail-open.
		return nil
	}

	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil
	}
	defer db.Close()

	// Short connect-timeout: we only need a quick read; don't block startup.
	db.SetConnMaxLifetime(5 * time.Second)
	db.SetMaxOpenConns(1)

	var dbMax int64
	queryErr := db.QueryRow(
		"SELECT COALESCE(MAX(version), 0) FROM schema_migrations",
	).Scan(&dbMax)
	if queryErr != nil {
		// Table absent, query error, or connection failure — fail-open.
		return nil
	}

	if uint(dbMax) > binaryMax {
		return fmt.Errorf(
			"startup aborted: database schema version %d is ahead of this binary's embedded version %d; "+
				"deploy a binary that includes migration %d or roll the database back",
			dbMax, binaryMax, dbMax,
		)
	}

	return nil
}

func normalizePgxURL(dsn string) string {
	switch {
	case len(dsn) >= 11 && dsn[:11] == "postgresql:":
		return "pgx5:" + dsn[11:]
	case len(dsn) >= 9 && dsn[:9] == "postgres:":
		return "pgx5:" + dsn[9:]
	case len(dsn) >= 5 && dsn[:5] == "pgx5:":
		return dsn
	default:
		return "pgx5://" + dsn
	}
}
