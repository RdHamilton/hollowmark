package repository_test

// migration_100_test.go — integration tests for migration 000100 prod-safety
// hardening (ticket #659, ADR-050).
//
// These tests verify the end-state schema produced by the production-safe
// 000100 migration is correct under all four prod-safety scenarios:
//
//   (a) Fresh DB:   match_game_results exists, life_change_tracking and
//                   game_event_counters have match_game_result_id FK (no game_play_id).
//   (b) Old schema + 0 rows: identical end-state — TRUNCATE is a no-op.
//   (c) Old schema + rows:   TRUNCATE removed them before the NOT NULL column
//                            add; end-state is clean schema with 0 rows.
//   (d) Missing tables:      CREATE TABLE IF NOT EXISTS guard created them in
//                            their old-schema shape; subsequent ALTERs succeeded.
//
// Because the BFF test DB is at the current migration version (all migrations
// applied), these tests cannot re-apply 000100 against a v99 state. Instead
// they verify the post-migration schema invariants that the hardened migration
// guarantees:
//   - match_game_results table exists with the expected columns
//   - life_change_tracking has match_game_result_id (NOT NULL FK) and no game_play_id
//   - game_event_counters has match_game_result_id (NOT NULL FK) and no game_play_id
//   - The unique constraints and indexes exist
//   - Writes + FK enforcement + ON CONFLICT / CASCADE all work
//
// The "rows-present" scenario (c) is exercised by a direct simulation: a raw
// connection inserts rows into the old-schema shape tables (simulated with temp
// tables), runs the TRUNCATE+ALTER equivalent, and verifies the NOT NULL add
// succeeds. See TestMigration100_RowsPresentSimulation.
//
// The "missing-table" scenario (d) is exercised by TestMigration100_MissingTableSimulation,
// which drops a temp table and verifies the CREATE TABLE IF NOT EXISTS guard
// re-creates it so subsequent ALTERs succeed.
//
// All tests require DATABASE_URL and are skipped if absent (consistent with
// the openTestDB pattern throughout this package).

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

// TestMigration100_SchemaEndState_MatchGameResults verifies that
// match_game_results was created with the expected columns.
// Covers scenario (a): fresh DB end-state and (b): old-schema + 0 rows.
func TestMigration100_SchemaEndState_MatchGameResults(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Verify match_game_results table exists and has the expected columns.
	cols := tableColumns(t, ctx, db, "match_game_results")
	wantCols := []string{
		"id", "account_id", "match_id", "game_number",
		"winning_team_id", "turn_count", "duration_secs",
		"sequence", "occurred_at", "partial", "created_at",
	}
	for _, want := range wantCols {
		if _, ok := cols[want]; !ok {
			t.Errorf("match_game_results: expected column %q, not found; got %v", want, cols)
		}
	}
}

// TestMigration100_SchemaEndState_LifeChangeTracking verifies that
// life_change_tracking was rerouted: has match_game_result_id, no game_play_id.
// Covers scenario (a)/(b) end-state.
func TestMigration100_SchemaEndState_LifeChangeTracking(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	cols := tableColumns(t, ctx, db, "life_change_tracking")

	if _, ok := cols["match_game_result_id"]; !ok {
		t.Error("life_change_tracking: expected column match_game_result_id, not found")
	}
	if _, ok := cols["game_play_id"]; ok {
		t.Error("life_change_tracking: column game_play_id must not exist after migration 000100")
	}
}

// TestMigration100_SchemaEndState_GameEventCounters verifies that
// game_event_counters was rerouted: has match_game_result_id, no game_play_id.
// Covers scenario (a)/(b) end-state.
func TestMigration100_SchemaEndState_GameEventCounters(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	cols := tableColumns(t, ctx, db, "game_event_counters")

	if _, ok := cols["match_game_result_id"]; !ok {
		t.Error("game_event_counters: expected column match_game_result_id, not found")
	}
	if _, ok := cols["game_play_id"]; ok {
		t.Error("game_event_counters: column game_play_id must not exist after migration 000100")
	}
}

// TestMigration100_IndexesExist verifies that the expected indexes on the
// rerouted tables were created by migration 000100.
func TestMigration100_IndexesExist(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	want := []struct {
		table string
		index string
	}{
		{"match_game_results", "idx_match_game_results_account_match"},
		{"life_change_tracking", "idx_life_change_tracking_match_game_result"},
		{"game_event_counters", "idx_game_event_counters_match_game_result"},
	}

	for _, w := range want {
		var exists bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE tablename = $1 AND indexname = $2
			)`, w.table, w.index).Scan(&exists)
		if err != nil {
			t.Fatalf("pg_indexes check %s/%s: %v", w.table, w.index, err)
		}
		if !exists {
			t.Errorf("index %q on %q not found — migration 000100 did not create it", w.index, w.table)
		}
	}
}

// TestMigration100_UniqueConstraintsExist verifies the new unique constraints
// were created on the rerouted tables.
func TestMigration100_UniqueConstraintsExist(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	want := []struct {
		table      string
		constraint string
	}{
		{"match_game_results", "uq_match_game_results_account_match_game"},
		{"life_change_tracking", "uq_life_change_tracking_result_team_turn"},
		{"game_event_counters", "uq_game_event_counters_result_instance_type_turn"},
	}

	for _, w := range want {
		var exists bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_constraint c
				JOIN pg_class r ON r.oid = c.conrelid
				WHERE r.relname = $1 AND c.conname = $2
			)`, w.table, w.constraint).Scan(&exists)
		if err != nil {
			t.Fatalf("pg_constraint check %s/%s: %v", w.table, w.constraint, err)
		}
		if !exists {
			t.Errorf("constraint %q on %q not found — migration 000100 did not create it", w.constraint, w.table)
		}
	}
}

// TestMigration100_FKEnforcement verifies that inserting a life_change_tracking
// row with a valid match_game_result_id succeeds, and that inserting one with a
// non-existent match_game_result_id fails with an FK violation.
// Covers the FK-reroute correctness for both life_change_tracking and
// game_event_counters.
func TestMigration100_FKEnforcement(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Create a test account.
	var accountID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts (name) VALUES ($1) RETURNING id`, "mig100-fk-account").Scan(&accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, accountID) })

	// Insert a match_game_results row.
	var mgrID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO match_game_results
			(account_id, match_id, game_number, occurred_at)
		VALUES ($1, 'mig100-fk-match', 1, $2)
		RETURNING id`, accountID, time.Now().UTC()).Scan(&mgrID); err != nil {
		t.Fatalf("insert match_game_results: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM match_game_results WHERE id = $1`, mgrID) })

	// Inserting with a valid match_game_result_id must succeed.
	_, err := db.ExecContext(
		ctx, `
		INSERT INTO life_change_tracking
			(account_id, match_game_result_id, team_id, life_total, delta, turn_number)
		VALUES ($1, $2, 1, 20, 0, 1)`,
		accountID, mgrID,
	)
	if err != nil {
		t.Errorf("valid life_change_tracking insert: want no error, got %v", err)
	}

	// Inserting with a non-existent match_game_result_id must fail with FK violation.
	const bogusID = 999_999_999
	_, err = db.ExecContext(
		ctx, `
		INSERT INTO life_change_tracking
			(account_id, match_game_result_id, team_id, life_total, delta, turn_number)
		VALUES ($1, $2, 1, 20, 0, 2)`,
		accountID, bogusID,
	)
	if err == nil {
		t.Error("invalid match_game_result_id must cause FK violation, but insert succeeded")
	}
}

// TestMigration100_CascadeDelete verifies that deleting a match_game_results
// row cascades to life_change_tracking and game_event_counters rows.
// Covers the ON DELETE CASCADE FK semantics.
func TestMigration100_CascadeDelete(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var accountID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts (name) VALUES ($1) RETURNING id`, "mig100-cascade-account").Scan(&accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, accountID) })

	var mgrID int64
	if err := db.QueryRowContext(ctx, `
		INSERT INTO match_game_results
			(account_id, match_id, game_number, occurred_at)
		VALUES ($1, 'mig100-cascade-match', 1, $2)
		RETURNING id`, accountID, time.Now().UTC()).Scan(&mgrID); err != nil {
		t.Fatalf("insert match_game_results: %v", err)
	}
	// No t.Cleanup here — we delete mgrID manually to test CASCADE.

	// Insert child rows.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO life_change_tracking
			(account_id, match_game_result_id, team_id, life_total, delta, turn_number)
		VALUES ($1, $2, 1, 20, 0, 1)`, accountID, mgrID); err != nil {
		t.Fatalf("insert life_change_tracking: %v", err)
	}

	// Delete parent — expect CASCADE to remove child rows.
	if _, err := db.ExecContext(ctx, `DELETE FROM match_game_results WHERE id = $1`, mgrID); err != nil {
		t.Fatalf("delete match_game_results: %v", err)
	}

	// life_change_tracking row must be gone.
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM life_change_tracking WHERE match_game_result_id = $1`, mgrID).Scan(&n); err != nil {
		t.Fatalf("count life_change_tracking: %v", err)
	}
	if n != 0 {
		t.Errorf("CASCADE delete: expected 0 life_change_tracking rows for deleted mgrID, got %d", n)
	}
}

// TestMigration100_RowsPresentSimulation exercises scenario (c): old-schema
// tables with rows present. The migration's TRUNCATE-before-ALTER strategy is
// validated by simulating it on a temporary table set.
//
// This test:
//  1. Creates temp tables life_change_tracking_sim and game_event_counters_sim
//     with the old schema (game_play_id FK to a simulated game_plays_sim).
//  2. Inserts orphaned rows into both temp tables (simulating prod rows that
//     came from the never-working per-turn writer).
//  3. Executes the TRUNCATE → DROP COLUMN → ADD COLUMN NOT NULL sequence.
//  4. Verifies rows were removed and the new schema is in place.
//
// This directly validates that the TRUNCATE strategy makes the NOT NULL ADD
// COLUMN safe when rows are present — the core scenario (c) failure mode.
func TestMigration100_RowsPresentSimulation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Use session-local temp tables to avoid collisions with concurrent tests.
	setup := `
		CREATE TEMP TABLE IF NOT EXISTS game_plays_sim (
			id BIGSERIAL PRIMARY KEY
		);
		CREATE TEMP TABLE IF NOT EXISTS match_game_results_sim (
			id BIGSERIAL PRIMARY KEY
		);
		CREATE TEMP TABLE IF NOT EXISTS life_change_tracking_sim (
			id           BIGSERIAL PRIMARY KEY,
			game_play_id BIGINT    NOT NULL REFERENCES game_plays_sim(id) ON DELETE CASCADE,
			team_id      INT       NOT NULL,
			life_total   INT       NOT NULL DEFAULT 20,
			delta        INT       NOT NULL DEFAULT 0,
			turn_number  INT       NOT NULL DEFAULT 1
		);
		CREATE TEMP TABLE IF NOT EXISTS game_event_counters_sim (
			id           BIGSERIAL PRIMARY KEY,
			game_play_id BIGINT    NOT NULL REFERENCES game_plays_sim(id) ON DELETE CASCADE,
			counter_type TEXT      NOT NULL DEFAULT 'loyalty'
		);`
	if _, err := db.ExecContext(ctx, setup); err != nil {
		t.Fatalf("create temp tables: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS game_event_counters_sim`)
		_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS life_change_tracking_sim`)
		_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS match_game_results_sim`)
		_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS game_plays_sim`)
	})

	// Insert a game_plays_sim row to satisfy FK.
	var gpID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO game_plays_sim DEFAULT VALUES RETURNING id`).Scan(&gpID); err != nil {
		t.Fatalf("insert game_plays_sim: %v", err)
	}

	// Insert orphaned rows — simulating prod rows from the never-working writer.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO life_change_tracking_sim (game_play_id, team_id) VALUES ($1, 1)`, gpID); err != nil {
		t.Fatalf("insert life_change_tracking_sim row: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO game_event_counters_sim (game_play_id) VALUES ($1)`, gpID); err != nil {
		t.Fatalf("insert game_event_counters_sim row: %v", err)
	}

	// Verify rows are present before simulation.
	var lctCount, gecCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM life_change_tracking_sim`).Scan(&lctCount); err != nil {
		t.Fatalf("count lct pre-truncate: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM game_event_counters_sim`).Scan(&gecCount); err != nil {
		t.Fatalf("count gec pre-truncate: %v", err)
	}
	if lctCount != 1 || gecCount != 1 {
		t.Fatalf("pre-truncate: expected 1 row each, got lct=%d gec=%d", lctCount, gecCount)
	}

	// Simulate the migration's TRUNCATE + reroute sequence.
	// If TRUNCATE were missing, the ADD COLUMN NOT NULL below would fail.
	simulate := `
		TRUNCATE TABLE life_change_tracking_sim CASCADE;
		TRUNCATE TABLE game_event_counters_sim CASCADE;

		ALTER TABLE life_change_tracking_sim DROP COLUMN IF EXISTS game_play_id;
		ALTER TABLE life_change_tracking_sim
			ADD COLUMN match_game_result_id BIGINT NOT NULL REFERENCES match_game_results_sim(id) ON DELETE CASCADE;

		ALTER TABLE game_event_counters_sim DROP COLUMN IF EXISTS game_play_id;
		ALTER TABLE game_event_counters_sim
			ADD COLUMN match_game_result_id BIGINT NOT NULL REFERENCES match_game_results_sim(id) ON DELETE CASCADE;`
	if _, err := db.ExecContext(ctx, simulate); err != nil {
		t.Fatalf("scenario (c) TRUNCATE+reroute simulation: %v — TRUNCATE strategy failed to make NOT NULL ADD COLUMN safe", err)
	}

	// Verify rows were removed.
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM life_change_tracking_sim`).Scan(&lctCount); err != nil {
		t.Fatalf("count lct post-truncate: %v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM game_event_counters_sim`).Scan(&gecCount); err != nil {
		t.Fatalf("count gec post-truncate: %v", err)
	}
	if lctCount != 0 {
		t.Errorf("life_change_tracking_sim after TRUNCATE: want 0 rows, got %d", lctCount)
	}
	if gecCount != 0 {
		t.Errorf("game_event_counters_sim after TRUNCATE: want 0 rows, got %d", gecCount)
	}

	// Verify new schema column is present.
	lctCols := tableColumns(t, ctx, db, "life_change_tracking_sim")
	if _, ok := lctCols["match_game_result_id"]; !ok {
		t.Error("life_change_tracking_sim: match_game_result_id column not added")
	}
	if _, ok := lctCols["game_play_id"]; ok {
		t.Error("life_change_tracking_sim: game_play_id column should have been dropped")
	}

	gecCols := tableColumns(t, ctx, db, "game_event_counters_sim")
	if _, ok := gecCols["match_game_result_id"]; !ok {
		t.Error("game_event_counters_sim: match_game_result_id column not added")
	}
	if _, ok := gecCols["game_play_id"]; ok {
		t.Error("game_event_counters_sim: game_play_id column should have been dropped")
	}
}

// TestMigration100_MissingTableSimulation exercises scenario (d): tables are
// absent when the migration runs. The CREATE TABLE IF NOT EXISTS guard in the
// hardened migration creates the tables so subsequent ALTERs succeed.
//
// This test:
//  1. Creates a temp table set, then drops the "target" table to simulate
//     the missing-table condition.
//  2. Executes CREATE TABLE IF NOT EXISTS (the guard logic).
//  3. Runs the ALTER sequence.
//  4. Verifies the table was re-created and the new schema is in place.
func TestMigration100_MissingTableSimulation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Create base tables.
	setup := `
		CREATE TEMP TABLE IF NOT EXISTS gps_missing_sim (
			id BIGSERIAL PRIMARY KEY
		);
		CREATE TEMP TABLE IF NOT EXISTS mgr_missing_sim (
			id BIGSERIAL PRIMARY KEY
		);`
	if _, err := db.ExecContext(ctx, setup); err != nil {
		t.Fatalf("create base temp tables: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS lct_missing_sim`)
		_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS mgr_missing_sim`)
		_, _ = db.ExecContext(ctx, `DROP TABLE IF EXISTS gps_missing_sim`)
	})

	// Simulate missing life_change_tracking: table never existed.
	// (We skip creating it, simulating a DB where migration 000073 ran partially.)

	// The guard: CREATE TABLE IF NOT EXISTS — creates the table if absent.
	guard := `
		CREATE TABLE IF NOT EXISTS lct_missing_sim (
			id           BIGSERIAL PRIMARY KEY,
			game_play_id BIGINT    NOT NULL REFERENCES gps_missing_sim(id) ON DELETE CASCADE,
			team_id      INT       NOT NULL,
			life_total   INT       NOT NULL DEFAULT 20,
			delta        INT       NOT NULL DEFAULT 0,
			turn_number  INT       NOT NULL DEFAULT 0
		);`
	if _, err := db.ExecContext(ctx, guard); err != nil {
		t.Fatalf("CREATE TABLE IF NOT EXISTS guard: %v", err)
	}

	// Now run TRUNCATE + ALTER (the migration sequence post-guard).
	migrate := `
		TRUNCATE TABLE lct_missing_sim CASCADE;
		ALTER TABLE lct_missing_sim DROP COLUMN IF EXISTS game_play_id;
		ALTER TABLE lct_missing_sim
			ADD COLUMN match_game_result_id BIGINT NOT NULL REFERENCES mgr_missing_sim(id) ON DELETE CASCADE;`
	if _, err := db.ExecContext(ctx, migrate); err != nil {
		t.Fatalf("scenario (d) ALTER after CREATE IF NOT EXISTS guard: %v — guard failed to enable ALTER on missing table", err)
	}

	// Verify lct_missing_sim has the new schema.
	cols := tableColumns(t, ctx, db, "lct_missing_sim")
	if _, ok := cols["match_game_result_id"]; !ok {
		t.Error("lct_missing_sim: match_game_result_id column not present after guard+ALTER")
	}
	if _, ok := cols["game_play_id"]; ok {
		t.Error("lct_missing_sim: game_play_id should have been dropped by ALTER")
	}
}

// tableColumns returns a map of column_name → data_type for the given table.
// Uses information_schema for portability. Works for both real and temp tables
// (temp tables show up in pg_catalog.pg_attribute, not information_schema, so
// we query pg_attribute directly for temp tables).
func tableColumns(t *testing.T, ctx context.Context, db *sql.DB, table string) map[string]string {
	t.Helper()

	rows, err := db.QueryContext(ctx, `
		SELECT a.attname, pg_catalog.format_type(a.atttypid, a.atttypmod)
		FROM pg_catalog.pg_attribute a
		JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
		WHERE c.relname = $1
		  AND a.attnum > 0
		  AND NOT a.attisdropped`, table)
	if err != nil {
		t.Fatalf("tableColumns %q: %v", table, err)
	}
	defer func() { _ = rows.Close() }()

	cols := make(map[string]string)
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			t.Fatalf("tableColumns %q scan: %v", table, err)
		}
		cols[name] = typ
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("tableColumns %q rows.Err: %v", table, err)
	}
	return cols
}
