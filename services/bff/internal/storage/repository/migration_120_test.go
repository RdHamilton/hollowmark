package repository_test

// migration_120_test.go -- integration tests for migration 000120
// (ticket #1185: convert game_plays.account_id from TEXT to BIGINT on
// incremental-upgrade DBs).
//
// ROOT CAUSE CONTEXT
// ------------------
// On the incremental-migration path (DBs that predate migration 000054):
//
//   - 000030: creates game_plays WITHOUT an account_id column.
//   - 000068: ADD COLUMN account_id TEXT NOT NULL DEFAULT ''
//             → column born as TEXT; DEFAULT '' remains active.
//   - 000101: ALTER COLUMN account_id DROP NOT NULL
//             → column stays TEXT, now nullable; DEFAULT '' NOT removed.
//   - 000106: ADD COLUMN IF NOT EXISTS account_id BIGINT
//             → no-op (column already exists as TEXT).
//
// On the fresh-init path (000054 consolidated schema):
//   - 000054: game_plays.account_id BIGINT from the start.
//   - 000068: no-op by name.
//
// Pre-#820 INSERTs omitted account_id from the column list; those rows took
// the DEFAULT '' on the TEXT/incremental path.  000106's backfill
// (WHERE account_id IS NULL) skipped them — they survive as '' in prod today.
// InsertCardPlays binds accountID int64 as $1; pgx resolves the server-side
// OID to text (OID 25) and has no int64→text encode plan → the reported error.
//
// FIX (migration 000120)
// ----------------------
// A PL/pgSQL DO block gates all mutations on the column being TEXT so the
// migration is a guaranteed no-op on the fresh-init (BIGINT) path.
// On the TEXT path:
//   1. Pre-backfill: resolve '' sentinels to their account via games→matches.
//   2. Null-out any remaining '' rows that could not be resolved.
//   3. ALTER COLUMN TYPE BIGINT USING NULLIF(btrim(account_id), '')::bigint
//
// TEST STRATEGY (Ray REQUIRED CHANGE 4)
// --------------------------------------
// These tests simulate the migration against a temp-table copy of the
// TEXT/incremental-path schema.  They do NOT alter the real test DB schema.
// The temp-table approach matches the migration_100_test.go simulation pattern.
//
// Tests:
//   TestMigration120_EmptyStringRowResolvesToNull
//     → seeds an orphan '' row (no game→match join), runs the transform,
//       asserts it became NULL. A bare ::bigint cast would abort here — this
//       is the primary TDD gate for Ray REQUIRED CHANGE 1.
//   TestMigration120_EmptyStringRowResolvesToAccount
//     → seeds a '' row with a resolvable game_id, asserts it resolves to the
//       correct BIGINT account_id via the Step 1 UPDATE.
//   TestMigration120_ValidBigintStringRowPreserved
//     → seeds a valid '42' row, asserts it survives as 42.
//   TestMigration120_NullRowPreserved
//     → seeds a NULL row, asserts it remains NULL.
//   TestMigration120_ColumnTypeBecomesBigint
//     → asserts the final data_type is bigint on the TEXT path.
//   TestMigration120_FreshInitPathIsNoOp
//     → seeds a temp table where account_id is already BIGINT, runs the DO
//       block, asserts no error and column stays bigint (REQUIRED CHANGE 3).
//   TestMigration120_InsertCardPlaysSucceedsAfterConversion
//     → proves the encode-plan error is resolved: InsertCardPlays with an
//       int64 accountID succeeds against a BIGINT column.
//   TestMigration120_PreApplyCountQuery
//     → demonstrates the Change-2 pre-apply query reports correct counts
//       (empty_string_rows > 0, uncastable_rows == 0) for seeded data.

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	"github.com/RdHamilton/hollowmark/services/contract"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// setupMig120SimDB creates the temp tables needed to simulate the incremental
// (TEXT) schema path.  It mirrors the minimal shape that migration 000120
// must handle:
//
//	accounts_sim (id BIGSERIAL PK, name TEXT)
//	matches_sim  (id TEXT PK, account_id BIGINT → accounts_sim.id)
//	games_sim    (id BIGSERIAL PK, match_id TEXT → matches_sim.id)
//	game_plays_sim (id BIGSERIAL PK, game_id BIGINT → games_sim.id,
//	                match_id TEXT, sequence_number INTEGER,
//	                account_id TEXT nullable — post-000101 state)
//
// Cleanup is registered via t.Cleanup (reverse-drop order).
func setupMig120SimDB(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		CREATE TEMP TABLE IF NOT EXISTS accounts_sim (
			id   BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL DEFAULT ''
		);
		CREATE TEMP TABLE IF NOT EXISTS matches_sim (
			id         TEXT PRIMARY KEY,
			account_id BIGINT REFERENCES accounts_sim(id) ON DELETE CASCADE
		);
		CREATE TEMP TABLE IF NOT EXISTS games_sim (
			id       BIGSERIAL PRIMARY KEY,
			match_id TEXT REFERENCES matches_sim(id) ON DELETE CASCADE
		);
		-- Mirrors the incremental (TEXT) path post-000101: nullable TEXT column.
		CREATE TEMP TABLE IF NOT EXISTS game_plays_sim (
			id              BIGSERIAL PRIMARY KEY,
			game_id         BIGINT REFERENCES games_sim(id) ON DELETE CASCADE,
			match_id        TEXT,
			sequence_number INTEGER,
			account_id      TEXT
		);
	`)
	if err != nil {
		t.Fatalf("setupMig120SimDB: create temp tables: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS game_plays_sim`)
		_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS games_sim`)
		_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS matches_sim`)
		_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS accounts_sim`)
	})
}

// seedMig120Account inserts a row into accounts_sim and returns its id.
func seedMig120Account(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO accounts_sim (name) VALUES ($1) RETURNING id`, name,
	).Scan(&id); err != nil {
		t.Fatalf("seedMig120Account %q: %v", name, err)
	}
	return id
}

// seedMig120Match inserts a row into matches_sim.
func seedMig120Match(t *testing.T, db *sql.DB, matchID string, accountID int64) {
	t.Helper()
	if _, err := db.ExecContext(
		context.Background(),
		`INSERT INTO matches_sim (id, account_id) VALUES ($1, $2)`, matchID, accountID,
	); err != nil {
		t.Fatalf("seedMig120Match %q: %v", matchID, err)
	}
}

// seedMig120Game inserts a row into games_sim and returns its id.
func seedMig120Game(t *testing.T, db *sql.DB, matchID string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO games_sim (match_id) VALUES ($1) RETURNING id`, matchID,
	).Scan(&id); err != nil {
		t.Fatalf("seedMig120Game %q: %v", matchID, err)
	}
	return id
}

// applyMig120TransformOnSim runs the migration 000120 logic (the DO $$ block)
// against the *_sim tables.  The SQL is structurally identical to the
// migration file but substitutes sim table names.
//
// Temp tables appear in pg_catalog but NOT in information_schema, so the gate
// condition uses pg_attribute + pg_class (same as simColumnDataType below).
func applyMig120TransformOnSim(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()

	const simTransform = `
DO $$
DECLARE
  col_type TEXT;
BEGIN
  SELECT pg_catalog.format_type(a.atttypid, a.atttypmod)
    INTO col_type
    FROM pg_catalog.pg_attribute a
    JOIN pg_catalog.pg_class     c ON c.oid = a.attrelid
   WHERE c.relname  = 'game_plays_sim'
     AND a.attname  = 'account_id'
     AND a.attnum   > 0
     AND NOT a.attisdropped;

  IF col_type LIKE 'text%' OR col_type LIKE 'character%' THEN
    -- Step 1: resolve '' sentinels to their account via games_sim → matches_sim join.
    UPDATE game_plays_sim gp
       SET account_id = m.account_id::text
      FROM games_sim  g
      JOIN matches_sim m ON m.id = g.match_id
     WHERE gp.game_id  = g.id
       AND btrim(COALESCE(gp.account_id, '')) = '';

    -- Step 2: null-out any '' rows that could not be resolved (orphan rows).
    UPDATE game_plays_sim SET account_id = NULL WHERE btrim(account_id) = '';

    -- Step 3: cast TEXT column to BIGINT; NULLIF handles any residual empties.
    EXECUTE 'ALTER TABLE game_plays_sim
               ALTER COLUMN account_id TYPE BIGINT
               USING NULLIF(btrim(account_id), '''')::bigint';
  END IF;
END$$;
`
	if _, err := db.ExecContext(ctx, simTransform); err != nil {
		t.Fatalf("applyMig120TransformOnSim: %v", err)
	}
}

// readSimAccountID reads account_id from game_plays_sim for the given row id.
// Returns (value, isNull).
func readSimAccountID(t *testing.T, db *sql.DB, rowID int64) (int64, bool) {
	t.Helper()
	var v sql.NullInt64
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT account_id FROM game_plays_sim WHERE id = $1`, rowID,
	).Scan(&v); err != nil {
		t.Fatalf("readSimAccountID row %d: %v", rowID, err)
	}
	return v.Int64, !v.Valid
}

// simColumnDataType returns the pg_catalog formatted type for account_id on
// game_plays_sim.
func simColumnDataType(t *testing.T, db *sql.DB) string {
	t.Helper()
	var dt string
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT pg_catalog.format_type(a.atttypid, a.atttypmod)
		   FROM pg_catalog.pg_attribute a
		   JOIN pg_catalog.pg_class     c ON c.oid = a.attrelid
		  WHERE c.relname = 'game_plays_sim'
		    AND a.attname = 'account_id'
		    AND a.attnum  > 0
		    AND NOT a.attisdropped`,
	).Scan(&dt); err != nil {
		t.Fatalf("simColumnDataType: %v", err)
	}
	return dt
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestMigration120_EmptyStringRowResolvesToNull is the primary TDD gate for
// Ray's REQUIRED CHANGE 1.
//
// A bare `USING account_id::bigint` aborts with:
//
//	ERROR: invalid input syntax for type bigint: ""
//
// The guarded migration must sanitise ” rows before the cast.
// Orphan ” rows (no resolvable game→match→account) become NULL.
func TestMigration120_EmptyStringRowResolvesToNull(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	setupMig120SimDB(t, db)

	// Orphan '' row: game_id = NULL, no join path to an account.
	var rowID int64
	if err := db.QueryRowContext(
		ctx,
		`INSERT INTO game_plays_sim (game_id, match_id, sequence_number, account_id)
		 VALUES (NULL, 'mig120-orphan-match', 0, '')
		 RETURNING id`,
	).Scan(&rowID); err != nil {
		t.Fatalf("seed '' orphan row: %v", err)
	}

	applyMig120TransformOnSim(t, db)

	_, isNull := readSimAccountID(t, db, rowID)
	if !isNull {
		val, _ := readSimAccountID(t, db, rowID)
		t.Errorf("orphan '' row: want account_id = NULL after migration, got %d", val)
	}
}

// TestMigration120_EmptyStringRowResolvesToAccount verifies that a ” row
// with an intact game→match→account chain is resolved to the correct BIGINT
// account_id by the Step 1 UPDATE.
func TestMigration120_EmptyStringRowResolvesToAccount(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	setupMig120SimDB(t, db)

	accountID := seedMig120Account(t, db, "mig120-resolve-account")
	const matchID = "mig120-resolve-match"
	seedMig120Match(t, db, matchID, accountID)
	gameID := seedMig120Game(t, db, matchID)

	var rowID int64
	if err := db.QueryRowContext(
		ctx,
		`INSERT INTO game_plays_sim (game_id, match_id, sequence_number, account_id)
		 VALUES ($1, $2, 0, '')
		 RETURNING id`,
		gameID, matchID,
	).Scan(&rowID); err != nil {
		t.Fatalf("seed '' row with resolvable game_id: %v", err)
	}

	applyMig120TransformOnSim(t, db)

	got, isNull := readSimAccountID(t, db, rowID)
	if isNull {
		t.Errorf("'' row with resolvable game_id: want account_id = %d, got NULL", accountID)
	}
	if got != accountID {
		t.Errorf("'' row with resolvable game_id: want account_id = %d, got %d", accountID, got)
	}
}

// TestMigration120_ValidBigintStringRowPreserved verifies that a row whose
// account_id is already a valid bigint string (e.g. '42') is preserved
// correctly as that integer after the cast.
func TestMigration120_ValidBigintStringRowPreserved(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	setupMig120SimDB(t, db)

	accountID := seedMig120Account(t, db, "mig120-valid-account")
	const matchID = "mig120-valid-match"
	seedMig120Match(t, db, matchID, accountID)
	gameID := seedMig120Game(t, db, matchID)

	var rowID int64
	if err := db.QueryRowContext(
		ctx,
		`INSERT INTO game_plays_sim (game_id, match_id, sequence_number, account_id)
		 VALUES ($1, $2, 1, $3)
		 RETURNING id`,
		gameID, matchID, fmt.Sprintf("%d", accountID),
	).Scan(&rowID); err != nil {
		t.Fatalf("seed valid-bigint-string row: %v", err)
	}

	applyMig120TransformOnSim(t, db)

	got, isNull := readSimAccountID(t, db, rowID)
	if isNull {
		t.Errorf("valid-bigint-string row: want account_id = %d, got NULL", accountID)
	}
	if got != accountID {
		t.Errorf("valid-bigint-string row: want account_id = %d, got %d", accountID, got)
	}
}

// TestMigration120_NullRowPreserved verifies that a NULL account_id row
// survives the transform as NULL.
func TestMigration120_NullRowPreserved(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	setupMig120SimDB(t, db)

	var rowID int64
	if err := db.QueryRowContext(
		ctx,
		`INSERT INTO game_plays_sim (game_id, match_id, sequence_number, account_id)
		 VALUES (NULL, 'mig120-null-match', 2, NULL)
		 RETURNING id`,
	).Scan(&rowID); err != nil {
		t.Fatalf("seed NULL row: %v", err)
	}

	applyMig120TransformOnSim(t, db)

	_, isNull := readSimAccountID(t, db, rowID)
	if !isNull {
		val, _ := readSimAccountID(t, db, rowID)
		t.Errorf("NULL row: want account_id = NULL after migration, got %d", val)
	}
}

// TestMigration120_ColumnTypeBecomesBigint verifies that after the transform
// the account_id column type is "bigint" on the TEXT path.
func TestMigration120_ColumnTypeBecomesBigint(t *testing.T) {
	db := openTestDB(t)
	setupMig120SimDB(t, db)

	// Precondition: column starts as text.
	preDT := simColumnDataType(t, db)
	if preDT != "text" {
		t.Fatalf("precondition: expected account_id to start as 'text', got %q", preDT)
	}

	applyMig120TransformOnSim(t, db)

	postDT := simColumnDataType(t, db)
	if postDT != "bigint" {
		t.Errorf("post-migration: want account_id type 'bigint', got %q", postDT)
	}
}

// TestMigration120_FreshInitPathIsNoOp verifies REQUIRED CHANGE 3: when
// account_id is already BIGINT (fresh-init / 000054 path), the migration DO
// block is a true no-op — no error, column stays bigint, data intact.
func TestMigration120_FreshInitPathIsNoOp(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		CREATE TEMP TABLE IF NOT EXISTS game_plays_bigint_sim (
			id         BIGSERIAL PRIMARY KEY,
			account_id BIGINT
		);
	`)
	if err != nil {
		t.Fatalf("create bigint sim table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP TABLE IF EXISTS game_plays_bigint_sim`)
	})

	const wantAccountID int64 = 99
	var rowID int64
	if err := db.QueryRowContext(
		ctx,
		`INSERT INTO game_plays_bigint_sim (account_id) VALUES ($1) RETURNING id`, wantAccountID,
	).Scan(&rowID); err != nil {
		t.Fatalf("seed bigint sim row: %v", err)
	}

	// The DO block gated on text/character type — must be a no-op for BIGINT.
	const noOpTransform = `
DO $$
DECLARE
  col_type TEXT;
BEGIN
  SELECT pg_catalog.format_type(a.atttypid, a.atttypmod)
    INTO col_type
    FROM pg_catalog.pg_attribute a
    JOIN pg_catalog.pg_class     c ON c.oid = a.attrelid
   WHERE c.relname  = 'game_plays_bigint_sim'
     AND a.attname  = 'account_id'
     AND a.attnum   > 0
     AND NOT a.attisdropped;

  IF col_type LIKE 'text%' OR col_type LIKE 'character%' THEN
    UPDATE game_plays_bigint_sim SET account_id = NULL WHERE btrim(account_id::text) = '';
    EXECUTE 'ALTER TABLE game_plays_bigint_sim
               ALTER COLUMN account_id TYPE BIGINT
               USING NULLIF(btrim(account_id::text), '''')::bigint';
  END IF;
END$$;
`
	if _, err := db.ExecContext(ctx, noOpTransform); err != nil {
		t.Fatalf("fresh-init no-op path errored: %v — migration must not touch BIGINT columns", err)
	}

	// Column must still be bigint.
	var dt string
	if err := db.QueryRowContext(
		ctx,
		`SELECT pg_catalog.format_type(a.atttypid, a.atttypmod)
		   FROM pg_catalog.pg_attribute a
		   JOIN pg_catalog.pg_class     c ON c.oid = a.attrelid
		  WHERE c.relname = 'game_plays_bigint_sim'
		    AND a.attname = 'account_id'
		    AND a.attnum  > 0
		    AND NOT a.attisdropped`,
	).Scan(&dt); err != nil {
		t.Fatalf("check column type post-no-op: %v", err)
	}
	if dt != "bigint" {
		t.Errorf("fresh-init path: want column type 'bigint' after no-op, got %q", dt)
	}

	// Data must be intact.
	var v sql.NullInt64
	if err := db.QueryRowContext(
		ctx,
		`SELECT account_id FROM game_plays_bigint_sim WHERE id = $1`, rowID,
	).Scan(&v); err != nil {
		t.Fatalf("read bigint sim row post-no-op: %v", err)
	}
	if !v.Valid || v.Int64 != wantAccountID {
		t.Errorf("fresh-init path: want account_id = %d, got valid=%v value=%d", wantAccountID, v.Valid, v.Int64)
	}
}

// TestMigration120_InsertCardPlaysSucceedsAfterConversion is the end-to-end
// regression guard for the reported encode-plan error (ticket #1185).
//
// After migration 000120 the column is BIGINT (OID 20); pgx correctly encodes
// int64 without an encode-plan miss.  If the column is reverted to TEXT this
// test begins failing with:
//
//	InsertCardPlays[0]: unable to encode N into text format for text (OID 25): cannot find encode plan
func TestMigration120_InsertCardPlaysSucceedsAfterConversion(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Assert the real test DB has account_id as BIGINT.  On the fresh-init path
	// this is true from 000054; on the incremental path it requires 000120 to
	// have been applied.
	var dt string
	if err := db.QueryRowContext(
		ctx,
		`SELECT data_type
		   FROM information_schema.columns
		  WHERE table_schema = 'public'
		    AND table_name   = 'game_plays'
		    AND column_name  = 'account_id'`,
	).Scan(&dt); err != nil {
		t.Fatalf("read game_plays.account_id data_type: %v", err)
	}
	if dt != "bigint" {
		t.Fatalf("game_plays.account_id is %q, not bigint — "+
			"migration 000120 must be applied before this test runs", dt)
	}

	// Build the minimal fixture: account → match → game.
	var accountID int64
	if err := db.QueryRowContext(
		ctx,
		`INSERT INTO accounts (name) VALUES ($1) RETURNING id`, "mig120-encode-test",
	).Scan(&accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, accountID) })

	const matchID = "mig120-encode-match-001"
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO matches
			(id, account_id, event_id, event_name, timestamp,
			 player_wins, opponent_wins, player_team_id, format, result)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (id) DO NOTHING`,
		matchID, accountID, "evt-"+matchID, "event-"+matchID, time.Now().UTC(),
		1, 0, 1, "Standard", "win",
	); err != nil {
		t.Fatalf("insert match: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM matches WHERE id = $1`, matchID) })

	var gameID int64
	if err := db.QueryRowContext(
		ctx,
		`INSERT INTO games (match_id, game_number, result)
		 VALUES ($1, 1, 'win')
		 ON CONFLICT (match_id, game_number) DO UPDATE SET result = EXCLUDED.result
		 RETURNING id`,
		matchID,
	).Scan(&gameID); err != nil {
		t.Fatalf("insert game: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM games WHERE id = $1`, gameID) })
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM game_plays WHERE game_id = $1`, gameID) })

	repo := repository.NewGamePlayRepository(db)
	entries := []contract.CardPlayEntry{
		{
			GameNumber: 1, TurnNumber: 1, Phase: "main1",
			ArenaID:    80001,
			PlayerType: "player", ActionType: "play_card",
			ZoneFrom: "hand", ZoneTo: "battlefield",
		},
	}

	// This is the exact call that failed on prod with:
	//   "InsertCardPlays[0]: unable to encode 3 into text format for text (OID 25): cannot find encode plan"
	if err := repo.InsertCardPlays(ctx, accountID, gameID, matchID, entries, time.Now().UTC()); err != nil {
		t.Errorf("InsertCardPlays after TEXT→BIGINT migration: %v\n"+
			"Regression: game_plays.account_id is not BIGINT (ticket #1185 encode-plan error)", err)
	}
}

// TestMigration120_PreApplyCountQuery demonstrates the Change-2 pre-apply
// count query.  It verifies the query correctly identifies:
//   - empty_string_rows: ” and NULL rows that need handling (> 0 expected on
//     the incremental path with pre-#820 data)
//   - uncastable_rows: non-numeric, non-empty strings that would abort the
//     cast (must be 0 before proceeding; non-zero requires investigation)
func TestMigration120_PreApplyCountQuery(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	setupMig120SimDB(t, db)

	// Seed: 2 '' rows, 1 valid bigint-string row, 1 NULL row.
	for i, val := range []interface{}{"", "", "77", nil} {
		if _, err := db.ExecContext(
			ctx,
			`INSERT INTO game_plays_sim (game_id, match_id, sequence_number, account_id)
			 VALUES (NULL, $1, $2, $3)`,
			fmt.Sprintf("mig120-count-match-%d", i), i, val,
		); err != nil {
			t.Fatalf("seed count-query row %d: %v", i, err)
		}
	}

	// The Change-2 pre-apply count query (adapted for sim table).
	// COALESCE(account_id, '') so NULL also registers as empty_string_rows.
	var emptyStringRows, uncastableRows int
	if err := db.QueryRowContext(ctx, `
		SELECT
		  count(*) FILTER (WHERE btrim(COALESCE(account_id, '')) = '')         AS empty_string_rows,
		  count(*) FILTER (WHERE account_id IS NOT NULL
		                     AND btrim(account_id) <> ''
		                     AND account_id !~ '^[0-9]+$')                     AS uncastable_rows
		FROM game_plays_sim
	`).Scan(&emptyStringRows, &uncastableRows); err != nil {
		t.Fatalf("pre-apply count query: %v", err)
	}

	// 2 '' rows + 1 NULL row = 3 empty_string_rows.
	if emptyStringRows != 3 {
		t.Errorf("pre-apply count: want empty_string_rows=3 (2 empty + 1 null), got %d", emptyStringRows)
	}
	if uncastableRows != 0 {
		t.Errorf("pre-apply count: uncastable_rows must be 0 before applying migration; got %d — STOP, investigate", uncastableRows)
	}
	t.Logf("pre-apply count: empty_string_rows=%d uncastable_rows=%d", emptyStringRows, uncastableRows)
}
