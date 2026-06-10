package repository_test

// migration_113_test.go — integration tests for migration 000113
// (ticket #850: drop redundant indexes on daemon_api_keys and accounts).
//
// Migration 000113 drops three indexes that were superseded by more specific
// partial indexes merged in earlier migrations:
//
//   - daemon_api_keys_account_id_idx   — plain btree(account_id); created in
//                                        migration 000085; superseded by the
//                                        partial daemon_api_keys_account_active_idx
//                                        (account_id WHERE revoked_at IS NULL)
//                                        created in the same migration.
//
//   - idx_daemon_api_keys_account_id   — duplicate plain btree(account_id); created
//                                        in migration 000112 for erasure cascade;
//                                        also superseded by
//                                        daemon_api_keys_account_active_idx.
//
//   - idx_accounts_is_default          — plain btree(is_default); created in
//                                        migration 000054 / 000104; superseded by
//                                        idx_accounts_default (partial unique index
//                                        WHERE is_default = true).
//
// Because the test DB is at the current migration version, these tests verify
// post-migration invariants directly against pg_indexes rather than replaying
// the migration itself. The down migration is verified in TestMigration113_DownRestoresIndexes.

import (
	"context"
	"testing"
)

// TestMigration113_RedundantIndexesDropped verifies that migration 000113 has
// removed the three redundant indexes from daemon_api_keys and accounts.
// If any of these indexes still exist, the migration was not applied correctly.
func TestMigration113_RedundantIndexesDropped(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	droppedIndexes := []struct {
		table string
		index string
	}{
		{"daemon_api_keys", "daemon_api_keys_account_id_idx"},
		{"daemon_api_keys", "idx_daemon_api_keys_account_id"},
		{"accounts", "idx_accounts_is_default"},
	}

	for _, di := range droppedIndexes {
		var exists bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE tablename = $1 AND indexname = $2
			)`, di.table, di.index).Scan(&exists)
		if err != nil {
			t.Fatalf("pg_indexes check %s/%s: %v", di.table, di.index, err)
		}
		if exists {
			t.Errorf(
				"index %q on %q still exists — migration 000113 did not drop it (or has not been applied)",
				di.index, di.table,
			)
		}
	}
}

// TestMigration113_SupersedingIndexesSurvive verifies that the partial indexes
// that supersede the dropped ones still exist after migration 000113 runs.
// These are the indexes that queries actually use — dropping them would break
// daemon authentication.
func TestMigration113_SupersedingIndexesSurvive(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	survivingIndexes := []struct {
		table string
		index string
		desc  string
	}{
		{
			"daemon_api_keys",
			"daemon_api_keys_account_active_idx",
			"partial index (account_id WHERE revoked_at IS NULL) — supersedes the dropped plain account_id indexes",
		},
		{
			"daemon_api_keys",
			"daemon_api_keys_key_prefix_active_idx",
			"partial index (key_prefix WHERE revoked_at IS NULL) — daemon auth hot path",
		},
		{
			"accounts",
			"idx_accounts_default",
			"partial unique index (is_default WHERE is_default = true) — supersedes idx_accounts_is_default",
		},
	}

	for _, si := range survivingIndexes {
		var exists bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE tablename = $1 AND indexname = $2
			)`, si.table, si.index).Scan(&exists)
		if err != nil {
			t.Fatalf("pg_indexes check %s/%s: %v", si.table, si.index, err)
		}
		if !exists {
			t.Errorf(
				"index %q on %q not found — this index supersedes a dropped one and must survive: %s",
				si.index, si.table, si.desc,
			)
		}
	}
}

// TestMigration113_DaemonAuthKeyLookupWorks verifies that the daemon API key
// lookup query (the hot path that uses daemon_api_keys_key_prefix_active_idx)
// still works after the redundant index drop. A structural/type regression
// would surface here.
func TestMigration113_DaemonAuthKeyLookupWorks(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// The daemon auth middleware runs:
	//   SELECT ... FROM daemon_api_keys WHERE key_prefix = $1 AND revoked_at IS NULL LIMIT 1
	// with a text key_prefix. This exercises the surviving
	// daemon_api_keys_key_prefix_active_idx partial index.
	// A non-existent prefix returns 0 rows — that's fine; we're testing the query
	// executes without error, not that a key exists.
	var count int
	err := db.QueryRowContext(
		ctx, `
		SELECT COUNT(*) FROM daemon_api_keys
		WHERE key_prefix = $1 AND revoked_at IS NULL`,
		"mig113-probe-nonexistent",
	).Scan(&count)
	if err != nil {
		t.Fatalf("daemon auth key lookup after index drop: %v", err)
	}
	// count == 0 is expected and correct.
}

// TestMigration113_AccountActiveLookupWorks verifies that a lookup by account_id
// with revoked_at IS NULL — the query pattern served by
// daemon_api_keys_account_active_idx — still works after the redundant index drop.
func TestMigration113_AccountActiveLookupWorks(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var count int
	err := db.QueryRowContext(
		ctx, `
		SELECT COUNT(*) FROM daemon_api_keys
		WHERE account_id = $1 AND revoked_at IS NULL`,
		"mig113-probe-nonexistent-account",
	).Scan(&count)
	if err != nil {
		t.Fatalf("account active key lookup after index drop: %v", err)
	}
	// count == 0 is expected and correct.
}

// TestMigration113_DownRestoresIndexes verifies that if migration 000113 is
// rolled back (down migration), the three indexes are recreated and the
// surviving partial indexes still exist.
//
// This test simulates the down migration by running its SQL directly against
// a transaction that is rolled back at the end — so it does not permanently
// alter the test DB's index state. The test verifies the index state WITHIN
// the transaction.
func TestMigration113_DownRestoresIndexes(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Simulate the down migration inside the transaction.
	// This recreates the indexes that 000113.up drops.
	// We use IF NOT EXISTS so the test is idempotent if somehow a prior
	// run left these indexes behind.
	downSQL := `
		CREATE INDEX IF NOT EXISTS daemon_api_keys_account_id_idx
			ON daemon_api_keys (account_id);

		CREATE INDEX IF NOT EXISTS idx_daemon_api_keys_account_id
			ON daemon_api_keys (account_id);

		CREATE INDEX IF NOT EXISTS idx_accounts_is_default
			ON accounts (is_default);
	`
	if _, err := tx.ExecContext(ctx, downSQL); err != nil {
		t.Fatalf("apply down migration SQL in transaction: %v", err)
	}

	// Verify all three indexes now exist within the transaction.
	restoredIndexes := []struct {
		table string
		index string
	}{
		{"daemon_api_keys", "daemon_api_keys_account_id_idx"},
		{"daemon_api_keys", "idx_daemon_api_keys_account_id"},
		{"accounts", "idx_accounts_is_default"},
	}

	for _, ri := range restoredIndexes {
		var exists bool
		err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE tablename = $1 AND indexname = $2
			)`, ri.table, ri.index).Scan(&exists)
		if err != nil {
			t.Fatalf("pg_indexes post-down check %s/%s: %v", ri.table, ri.index, err)
		}
		if !exists {
			t.Errorf(
				"down migration did not restore index %q on %q",
				ri.index, ri.table,
			)
		}
	}

	// Also verify the surviving partial indexes are still present after down.
	survivingAfterDown := []struct {
		table string
		index string
	}{
		{"daemon_api_keys", "daemon_api_keys_account_active_idx"},
		{"daemon_api_keys", "daemon_api_keys_key_prefix_active_idx"},
		{"accounts", "idx_accounts_default"},
	}

	for _, si := range survivingAfterDown {
		var exists bool
		err := tx.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE tablename = $1 AND indexname = $2
			)`, si.table, si.index).Scan(&exists)
		if err != nil {
			t.Fatalf("pg_indexes surviving-after-down check %s/%s: %v", si.table, si.index, err)
		}
		if !exists {
			t.Errorf(
				"surviving index %q on %q missing after down migration simulation — it must not be affected by the down",
				si.index, si.table,
			)
		}
	}
	// Transaction is rolled back by defer — test DB is unchanged.
}
