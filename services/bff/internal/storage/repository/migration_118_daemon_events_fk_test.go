package repository_test

// migration_118_daemon_events_fk_test.go — integration tests for migration 000118
// (ticket #2546: add FK daemon_events.user_id → users(id) ON DELETE CASCADE).
//
// Verifies the post-migration schema invariants and the erasure cascade path
// that this FK adds (user-delete side, distinct from the existing account_id-keyed
// DELETE path in DeleteTextKeyedRows / deletion_repo.go:111).
//
// The daemon_events table is created by migration 000061 on both the fresh-install
// and incremental paths; migration 000054 does NOT create daemon_events. There is
// therefore no two-path idempotency issue — the FK is new on both paths.
//
// Tests follow the migration_113_test.go pattern: they assert post-migration
// state against the live test DB (which is at the current migration version).
//
// All tests skip when DATABASE_URL is not set (same as the rest of this package).

import (
	"context"
	"testing"
)

// TestMigration118DaemonEventsFk_ConstraintExists verifies that the FK constraint
// fk_daemon_events_user_id exists on daemon_events with constraint_type FOREIGN KEY.
func TestMigration118DaemonEventsFk_ConstraintExists(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var exists bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.table_constraints
			WHERE constraint_name = 'fk_daemon_events_user_id'
			  AND table_name      = 'daemon_events'
			  AND constraint_type = 'FOREIGN KEY'
		)`).Scan(&exists); err != nil {
		t.Fatalf("information_schema query: %v", err)
	}
	if !exists {
		t.Fatal("fk_daemon_events_user_id FK constraint not found on daemon_events — migration 000118 was not applied or failed")
	}
}

// TestMigration118DaemonEventsFk_DeleteRuleIsCascade verifies that the FK
// delete rule is CASCADE (not NO ACTION or SET NULL).
func TestMigration118DaemonEventsFk_DeleteRuleIsCascade(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var deleteRule string
	if err := db.QueryRowContext(ctx, `
		SELECT delete_rule
		FROM information_schema.referential_constraints
		WHERE constraint_name = 'fk_daemon_events_user_id'`).Scan(&deleteRule); err != nil {
		t.Fatalf("referential_constraints query: %v", err)
	}
	if deleteRule != "CASCADE" {
		t.Errorf("fk_daemon_events_user_id delete_rule: want CASCADE, got %q", deleteRule)
	}
}

// TestMigration118DaemonEventsFk_UserDeleteCascades is the primary GDPR erasure
// regression guard for the user-delete path.
//
// It seeds a users row, an accounts row linked to that user, and two daemon_events
// rows for that user_id, deletes the user, and asserts that both daemon_events rows
// are removed by the FK cascade.
//
// This exercises the NEW user-delete erasure path added by migration 000118.
// The existing account_id-keyed path (DeletionRepository.DeleteTextKeyedRows,
// deletion_repo.go:111) deletes by MTGA Arena account_id TEXT and is unaffected.
// The two paths are additive: account-delete via DeleteTextKeyedRows, and
// user-delete via this FK cascade (HardDeleteUser, deletion_repo.go:172).
func TestMigration118DaemonEventsFk_UserDeleteCascades(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Seed a users row with a unique clerk_user_id to avoid conflicts.
	var userID int64
	if err := db.QueryRowContext(
		ctx, `
		INSERT INTO users (email, clerk_user_id, subscription_tier)
		VALUES ($1, $2, 'free')
		RETURNING id`,
		"mig118-cascade-test@test.local",
		"mig118-clerk-cascade-uid",
	).Scan(&userID); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	// Defensive cleanup: if the cascade test itself doesn't delete the user row,
	// remove it here so the test leaves no residue.
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})

	// Seed an accounts row linked to the user (accounts.user_id FK).
	var accountID int64
	if err := db.QueryRowContext(
		ctx, `
		INSERT INTO accounts (name, user_id) VALUES ($1, $2) RETURNING id`,
		"mig118-cascade-account", userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("insert accounts: %v", err)
	}
	// accounts row is cleaned up by the users → accounts ON DELETE CASCADE.

	// Seed two daemon_events rows for this user_id.
	// account_id is TEXT (MTGA Arena client_id string, not a FK into accounts.id).
	for i, payload := range []string{`{"mig118_row":1}`, `{"mig118_row":2}`} {
		if _, err := db.ExecContext(
			ctx, `
			INSERT INTO daemon_events
				(user_id, account_id, event_type, payload, occurred_at, received_at)
			VALUES ($1, 'mig118-mtga-client-id', 'match_result', $2::jsonb, NOW(), NOW())`,
			userID, payload,
		); err != nil {
			t.Fatalf("insert daemon_events row %d: %v", i, err)
		}
	}

	// Confirm two rows exist before the delete.
	var beforeCount int
	if err := db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM daemon_events WHERE user_id = $1`, userID,
	).Scan(&beforeCount); err != nil {
		t.Fatalf("count daemon_events before delete: %v", err)
	}
	if beforeCount != 2 {
		t.Fatalf("pre-delete: want 2 daemon_events rows, got %d", beforeCount)
	}

	// Delete the user — FK cascade must remove all daemon_events rows for this user_id.
	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("DELETE FROM users: %v", err)
	}

	// Assert all daemon_events rows for this user_id are gone.
	var afterCount int
	if err := db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM daemon_events WHERE user_id = $1`, userID,
	).Scan(&afterCount); err != nil {
		t.Fatalf("count daemon_events after delete: %v", err)
	}
	if afterCount != 0 {
		t.Errorf("FK cascade on user delete: want 0 daemon_events rows, got %d", afterCount)
	}
}

// TestMigration118DaemonEventsFk_InsertRejectsBadUserID verifies that inserting
// a daemon_events row with a non-existent user_id fails with an FK violation.
func TestMigration118DaemonEventsFk_InsertRejectsBadUserID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	const bogusUserID = int64(999_999_997)
	_, err := db.ExecContext(
		ctx, `
		INSERT INTO daemon_events
			(user_id, account_id, event_type, payload, occurred_at, received_at)
		VALUES ($1, 'mig118-baduid-account', 'match_result', '{"test":true}'::jsonb, NOW(), NOW())`,
		bogusUserID,
	)
	if err == nil {
		t.Error("insert with non-existent user_id must fail with FK violation, but succeeded")
		// Clean up the spuriously inserted row so the DB remains consistent.
		_, _ = db.ExecContext(ctx, `DELETE FROM daemon_events WHERE user_id = $1`, bogusUserID)
	}
}

// TestMigration118DaemonEventsFk_DownMigration simulates rolling back migration
// 000118 inside a transaction and verifies the FK constraint is removed.
// The transaction is always rolled back so the live test DB state is unchanged.
// The user_id column itself (from migration 000061) must survive the down migration.
func TestMigration118DaemonEventsFk_DownMigration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Apply the down migration SQL.
	if _, err := tx.ExecContext(
		ctx,
		`ALTER TABLE daemon_events DROP CONSTRAINT IF EXISTS fk_daemon_events_user_id`,
	); err != nil {
		t.Fatalf("apply down migration in transaction: %v", err)
	}

	// PostgreSQL DDL inside a transaction is visible to subsequent queries within
	// the same transaction. Verify constraint is gone within the txn.
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.table_constraints
			WHERE constraint_name = 'fk_daemon_events_user_id'
			  AND table_name      = 'daemon_events'
		)`).Scan(&exists); err != nil {
		t.Fatalf("constraint check after down migration: %v", err)
	}
	if exists {
		t.Error("down migration did not remove fk_daemon_events_user_id")
	}

	// user_id column (from 000061, predates this migration) must still exist.
	var colExists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name  = 'daemon_events'
			  AND column_name = 'user_id'
		)`).Scan(&colExists); err != nil {
		t.Fatalf("column check user_id after down migration: %v", err)
	}
	if !colExists {
		t.Error("daemon_events.user_id column was removed by down migration — it must survive (column predates 000118)")
	}
	// Transaction is rolled back by defer — test DB is unchanged.
}
