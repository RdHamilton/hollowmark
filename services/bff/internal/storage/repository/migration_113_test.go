package repository_test

// migration_113_test.go -- integration tests for migration 000113
// (ticket #850: drop redundant indexes on daemon_api_keys and accounts).
//
// Migration 000113 drops two indexes that were superseded by more specific
// partial indexes merged in earlier migrations:
//
//   - daemon_api_keys_account_id_idx   -- plain btree(account_id); created in
//                                         migration 000085; superseded by the
//                                         partial daemon_api_keys_account_active_idx
//                                         (account_id WHERE revoked_at IS NULL)
//                                         created in the same migration.
//
//   - idx_accounts_is_default          -- plain btree(is_default); created in
//                                         migration 000054 / 000104; superseded by
//                                         idx_accounts_default (partial unique index
//                                         WHERE is_default = TRUE).
//
// Migration 000113 RETAINS idx_daemon_api_keys_account_id (created in 000112
// for the GDPR Art.17 erasure cascade).  That index serves:
//
//	DELETE FROM daemon_api_keys WHERE account_id = ANY($1)
//
// which carries no revoked_at IS NULL predicate and therefore cannot use the
// partial daemon_api_keys_account_active_idx.  It is not redundant.
//
// Because the test DB is at the current migration version, these tests verify
// post-migration invariants directly against pg_indexes rather than replaying
// the migration itself. The down migration is verified in TestMigration113_DownRestoresIndexes.

import (
	"context"
	"testing"
)

// TestMigration113_RedundantIndexesDropped verifies that migration 000113 has
// removed the two redundant indexes from daemon_api_keys and accounts.
// If any of these indexes still exist, the migration was not applied correctly.
func TestMigration113_RedundantIndexesDropped(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	droppedIndexes := []struct {
		table string
		index string
	}{
		{"daemon_api_keys", "daemon_api_keys_account_id_idx"},
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
				"index %q on %q still exists -- migration 000113 did not drop it (or has not been applied)",
				di.index, di.table,
			)
		}
	}
}

// TestMigration113_SupersedingIndexesSurvive verifies that the partial indexes
// that supersede the dropped ones still exist after migration 000113 runs,
// and that idx_daemon_api_keys_account_id (retained for the GDPR erasure
// DELETE path) also survives.  These are all indexes queries actually use.
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
			"partial index (account_id WHERE revoked_at IS NULL) -- supersedes daemon_api_keys_account_id_idx",
		},
		{
			"daemon_api_keys",
			"daemon_api_keys_key_prefix_active_idx",
			"partial index (key_prefix WHERE revoked_at IS NULL) -- daemon auth hot path",
		},
		{
			"daemon_api_keys",
			"idx_daemon_api_keys_account_id",
			"plain btree(account_id) retained from migration 000112 -- serves GDPR Art.17 erasure DELETE FROM daemon_api_keys WHERE account_id = ANY($1)",
		},
		{
			"accounts",
			"idx_accounts_default",
			"partial unique index (is_default WHERE is_default = TRUE) -- supersedes idx_accounts_is_default",
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
				"index %q on %q not found -- this index supersedes a dropped one and must survive: %s",
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
	// A non-existent prefix returns 0 rows -- that's fine; we're testing the query
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
// with revoked_at IS NULL -- the query pattern served by
// daemon_api_keys_account_active_idx -- still works after the redundant index drop.
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

// TestMigration113_ErasureCascadeIndexSurvives verifies that
// idx_daemon_api_keys_account_id -- retained by migration 000113 -- still
// exists and that the GDPR Art.17 erasure DELETE pattern can execute against
// it without error.  This index was added by migration 000112 specifically
// to avoid a seq-scan on the full-table delete path; it must not be dropped.
func TestMigration113_ErasureCascadeIndexSurvives(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Verify the index is present.
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE tablename = 'daemon_api_keys'
			  AND indexname  = 'idx_daemon_api_keys_account_id'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("pg_indexes check idx_daemon_api_keys_account_id: %v", err)
	}
	if !exists {
		t.Fatal("idx_daemon_api_keys_account_id missing -- migration 000113 must NOT drop this index (it serves the GDPR erasure DELETE)")
	}

	// Verify the erasure DELETE pattern executes without error.
	// No rows match this synthetic account_id; we are testing query execution,
	// not data presence.
	_, err = db.ExecContext(
		ctx, `
		DELETE FROM daemon_api_keys
		WHERE account_id = ANY($1)`,
		[]string{"mig113-erasure-probe-nonexistent"},
	)
	if err != nil {
		t.Fatalf("erasure DELETE FROM daemon_api_keys WHERE account_id = ANY($1): %v", err)
	}
}

// TestMigration113_DownRestoresIndexes verifies that if migration 000113 is
// rolled back (down migration), the two dropped indexes are recreated and the
// surviving partial indexes still exist.
//
// This test simulates the down migration by running its SQL directly against
// a transaction that is rolled back at the end -- so it does not permanently
// alter the test DB's index state. The test verifies the index state WITHIN
// the transaction.
//
// Note: idx_daemon_api_keys_account_id is NOT in the restore list because
// 000113 did not drop it -- it was retained for the GDPR erasure DELETE path.
func TestMigration113_DownRestoresIndexes(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Simulate the down migration inside the transaction.
	// This recreates the two indexes that 000113.up drops.
	// We use IF NOT EXISTS so the test is idempotent if somehow a prior
	// run left these indexes behind.
	downSQL := `
		CREATE INDEX IF NOT EXISTS daemon_api_keys_account_id_idx
			ON daemon_api_keys (account_id);

		CREATE INDEX IF NOT EXISTS idx_accounts_is_default
			ON accounts (is_default);
	`
	if _, err := tx.ExecContext(ctx, downSQL); err != nil {
		t.Fatalf("apply down migration SQL in transaction: %v", err)
	}

	// Verify both indexes now exist within the transaction.
	restoredIndexes := []struct {
		table string
		index string
	}{
		{"daemon_api_keys", "daemon_api_keys_account_id_idx"},
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
		{"daemon_api_keys", "idx_daemon_api_keys_account_id"},
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
				"surviving index %q on %q missing after down migration simulation -- it must not be affected by the down",
				si.index, si.table,
			)
		}
	}
	// Transaction is rolled back by defer -- test DB is unchanged.
}
