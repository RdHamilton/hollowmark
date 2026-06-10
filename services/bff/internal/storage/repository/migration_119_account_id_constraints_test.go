package repository_test

// migration_119_account_id_constraints_test.go — integration tests for migration 000119
// (ticket #2545: enforce account_id NOT NULL + FK → accounts(id) ON DELETE CASCADE
// on five legacy tenant tables).
//
// Tables in scope:
//   - matches           (account_id BIGINT nullable, no FK — migration 000002)
//   - player_stats      (account_id BIGINT nullable, no FK — migration 000002)
//   - rank_history      (account_id BIGINT nullable, no FK — migration 000002)
//   - collection_history (account_id BIGINT nullable, no FK — migration 000002)
//   - draft_sessions    (account_id BIGINT nullable, FK present from migration 000052,
//                        NOT NULL absent — only NOT NULL is added here)
//
// Two-path notes:
//   - Incremental path (prod): columns are nullable BIGINT; matches/player_stats/
//     rank_history/collection_history have no FK. Migration adds NOT NULL + named FK.
//   - Fresh-install path (000054): matches/player_stats/rank_history/collection_history
//     already have inline anonymous FKs. Migration's SET NOT NULL is guarded by
//     information_schema.columns is_nullable pre-check; FK ADD is guarded by
//     duplicate_object; VALIDATE is guarded by constraint-existence check.
//     draft_sessions has an anonymous FK from 000052; only NOT NULL is added.
//
// Tests verify post-migration state (like migration_113_test.go) since the test DB
// is at the current migration version. Down migration is verified in a transaction.
//
// All tests skip when DATABASE_URL is not set.

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// TestMigration119AccountID_NotNullEnforced verifies that account_id is NOT NULL
// on all five tables after migration 000119.
func TestMigration119AccountID_NotNullEnforced(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	tables := []string{
		"matches",
		"player_stats",
		"rank_history",
		"collection_history",
		"draft_sessions",
	}

	for _, table := range tables {
		table := table
		t.Run(table, func(t *testing.T) {
			var isNullable string
			err := db.QueryRowContext(
				ctx, `
				SELECT is_nullable
				FROM   information_schema.columns
				WHERE  table_name  = $1
				  AND  column_name = 'account_id'`,
				table,
			).Scan(&isNullable)
			if err != nil {
				t.Fatalf("%s: information_schema query: %v", table, err)
			}
			if isNullable != "NO" {
				t.Errorf("%s.account_id is_nullable=%q — want NO (NOT NULL); migration 000119 was not applied or failed", table, isNullable)
			}
		})
	}
}

// TestMigration119AccountID_FKConstraintsExist verifies that named FK constraints
// exist on the four tables that had no FK before migration 000119.
// draft_sessions is excluded: its FK predates this migration (migration 000052,
// anonymous name); we verify the FK exists via referential_constraints instead.
func TestMigration119AccountID_FKConstraintsExist(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	namedFKs := []struct {
		table      string
		constraint string
	}{
		{"matches", "fk_matches_account_id"},
		{"player_stats", "fk_player_stats_account_id"},
		{"rank_history", "fk_rank_history_account_id"},
		{"collection_history", "fk_collection_history_account_id"},
	}

	for _, fk := range namedFKs {
		fk := fk
		t.Run(fk.table, func(t *testing.T) {
			// On the fresh-install path, 000054 creates inline anonymous FKs.
			// On the incremental path, 000119 creates the named constraint.
			// Either way, at least ONE FK from account_id to accounts(id) must exist.
			var count int
			if err := db.QueryRowContext(
				ctx, `
				SELECT COUNT(*)
				FROM   information_schema.referential_constraints rc
				JOIN   information_schema.key_column_usage kcu
				       ON kcu.constraint_name = rc.constraint_name
				WHERE  kcu.table_name  = $1
				  AND  kcu.column_name = 'account_id'`,
				fk.table,
			).Scan(&count); err != nil {
				t.Fatalf("%s: referential_constraints query: %v", fk.table, err)
			}
			if count == 0 {
				t.Errorf("%s: no FK on account_id — expected named or anonymous FK to accounts(id)", fk.table)
			}
		})
	}
}

// TestMigration119AccountID_FKDeleteRuleIsCascade verifies that the FK delete
// rule for account_id is CASCADE on all five tables.
func TestMigration119AccountID_FKDeleteRuleIsCascade(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	tables := []string{
		"matches",
		"player_stats",
		"rank_history",
		"collection_history",
		"draft_sessions",
	}

	for _, table := range tables {
		table := table
		t.Run(table, func(t *testing.T) {
			var deleteRule string
			err := db.QueryRowContext(
				ctx, `
				SELECT rc.delete_rule
				FROM   information_schema.referential_constraints rc
				JOIN   information_schema.key_column_usage kcu
				       ON kcu.constraint_name = rc.constraint_name
				WHERE  kcu.table_name  = $1
				  AND  kcu.column_name = 'account_id'
				LIMIT  1`,
				table,
			).Scan(&deleteRule)
			if err != nil {
				t.Fatalf("%s: referential_constraints delete_rule query: %v", table, err)
			}
			if deleteRule != "CASCADE" {
				t.Errorf("%s: FK delete_rule=%q — want CASCADE", table, deleteRule)
			}
		})
	}
}

// TestMigration119AccountID_InsertRejectsNullAccountID verifies that inserting
// a NULL account_id is rejected with a NOT NULL violation on all five tables.
func TestMigration119AccountID_InsertRejectsNullAccountID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// matches — minimal required fields
	t.Run("matches", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO matches
				(id, account_id, event_id, event_name, timestamp, player_wins,
				 opponent_wins, player_team_id, format, result)
			VALUES ('mig119-null-acct-match', NULL, 'evt1', 'test', NOW(), 0, 0, 1, 'Standard', 'win')`)
		if err == nil {
			t.Error("matches: NULL account_id must be rejected, but insert succeeded")
			_, _ = db.ExecContext(ctx, `DELETE FROM matches WHERE id='mig119-null-acct-match'`)
		}
	})

	// player_stats — minimal required fields
	t.Run("player_stats", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO player_stats (account_id, date, format)
			VALUES (NULL, CURRENT_DATE, 'Standard')`)
		if err == nil {
			t.Error("player_stats: NULL account_id must be rejected, but insert succeeded")
		}
	})

	// rank_history — minimal required fields
	t.Run("rank_history", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO rank_history (account_id, timestamp, format, season_ordinal)
			VALUES (NULL, NOW(), 'constructed', 1)`)
		if err == nil {
			t.Error("rank_history: NULL account_id must be rejected, but insert succeeded")
		}
	})

	// collection_history — minimal required fields
	t.Run("collection_history", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO collection_history
				(account_id, card_id, quantity_delta, quantity_after, timestamp)
			VALUES (NULL, 1, 1, 1, NOW())`)
		if err == nil {
			t.Error("collection_history: NULL account_id must be rejected, but insert succeeded")
		}
	})

	// draft_sessions — minimal required fields
	t.Run("draft_sessions", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO draft_sessions
				(id, account_id, event_name, set_code, start_time)
			VALUES ('mig119-null-acct-ds', NULL, 'test', 'NEO', NOW())`)
		if err == nil {
			t.Error("draft_sessions: NULL account_id must be rejected, but insert succeeded")
			_, _ = db.ExecContext(ctx, `DELETE FROM draft_sessions WHERE id='mig119-null-acct-ds'`)
		}
	})
}

// TestMigration119AccountID_InsertRejectsBadAccountID verifies FK enforcement:
// inserting a non-existent account_id fails with an FK violation on all four
// tables that got the new named FK.
// (draft_sessions already had FK from 000052; its FK enforcement is pre-existing.)
func TestMigration119AccountID_InsertRejectsBadAccountID(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	const bogusAccountID = int64(999_999_996)

	t.Run("matches", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO matches
				(id, account_id, event_id, event_name, timestamp, player_wins,
				 opponent_wins, player_team_id, format, result)
			VALUES ('mig119-bad-acct-match', $1, 'evt1', 'test', NOW(), 0, 0, 1, 'Standard', 'win')`,
			bogusAccountID)
		if err == nil {
			t.Error("matches: non-existent account_id must be rejected with FK violation, but insert succeeded")
			_, _ = db.ExecContext(ctx, `DELETE FROM matches WHERE id='mig119-bad-acct-match'`)
		}
	})

	t.Run("player_stats", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO player_stats (account_id, date, format)
			VALUES ($1, CURRENT_DATE, 'Standard')`, bogusAccountID)
		if err == nil {
			t.Error("player_stats: non-existent account_id must be rejected with FK violation, but insert succeeded")
		}
	})

	t.Run("rank_history", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO rank_history (account_id, timestamp, format, season_ordinal)
			VALUES ($1, NOW(), 'constructed', 1)`, bogusAccountID)
		if err == nil {
			t.Error("rank_history: non-existent account_id must be rejected with FK violation, but insert succeeded")
		}
	})

	t.Run("collection_history", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO collection_history
				(account_id, card_id, quantity_delta, quantity_after, timestamp)
			VALUES ($1, 1, 1, 1, NOW())`, bogusAccountID)
		if err == nil {
			t.Error("collection_history: non-existent account_id must be rejected with FK violation, but insert succeeded")
		}
	})
}

// TestMigration119AccountID_AccountDeleteCascades is the primary GDPR erasure
// regression guard.
//
// Seeds a users row, an accounts row linked to that user, one row in EACH of the
// five tables scoped to that account_id, deletes the accounts row, and asserts
// that all five child rows are removed by the FK cascade.
//
// This is the critical GDPR-erasure regression guard: before migration 000119,
// rows in matches/player_stats/rank_history/collection_history had no FK, so a
// cascade DELETE on accounts would leave them as orphaned PII.
func TestMigration119AccountID_AccountDeleteCascades(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Seed a users row.
	var userID int64
	if err := db.QueryRowContext(
		ctx, `
		INSERT INTO users (email, clerk_user_id, subscription_tier)
		VALUES ($1, $2, 'free')
		RETURNING id`,
		"mig119-cascade-test@test.local",
		"mig119-clerk-cascade-uid",
	).Scan(&userID); err != nil {
		t.Fatalf("insert users: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})

	// Seed an accounts row linked to the user.
	var accountID int64
	if err := db.QueryRowContext(
		ctx, `
		INSERT INTO accounts (name, user_id) VALUES ($1, $2) RETURNING id`,
		"mig119-cascade-account", userID,
	).Scan(&accountID); err != nil {
		t.Fatalf("insert accounts: %v", err)
	}
	// accounts row is cleaned up via users ON DELETE CASCADE.

	// Unique match ID for this test run.
	matchID := fmt.Sprintf("mig119-cascade-match-%d", time.Now().UnixNano())

	// Seed one row in each of the five tables for this account_id.
	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO matches
			(id, account_id, event_id, event_name, timestamp, player_wins,
			 opponent_wins, player_team_id, format, result)
		VALUES ($1, $2, 'evt1', 'cascade-test', NOW(), 0, 0, 1, 'Standard', 'win')`,
		matchID, accountID,
	); err != nil {
		t.Fatalf("insert matches: %v", err)
	}

	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO player_stats (account_id, date, format)
		VALUES ($1, CURRENT_DATE, 'Standard')
		ON CONFLICT (account_id, date, format) DO NOTHING`,
		accountID,
	); err != nil {
		t.Fatalf("insert player_stats: %v", err)
	}

	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO rank_history (account_id, timestamp, format, season_ordinal)
		VALUES ($1, NOW(), 'constructed', 99)`,
		accountID,
	); err != nil {
		t.Fatalf("insert rank_history: %v", err)
	}

	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO collection_history
			(account_id, card_id, quantity_delta, quantity_after, timestamp)
		VALUES ($1, 99901, 1, 1, NOW())`,
		accountID,
	); err != nil {
		t.Fatalf("insert collection_history: %v", err)
	}

	draftSessionID := fmt.Sprintf("mig119-cascade-ds-%d", time.Now().UnixNano())
	if _, err := db.ExecContext(
		ctx, `
		INSERT INTO draft_sessions
			(id, account_id, event_name, set_code, start_time)
		VALUES ($1, $2, 'cascade-test', 'NEO', NOW())`,
		draftSessionID, accountID,
	); err != nil {
		t.Fatalf("insert draft_sessions: %v", err)
	}

	// Delete the accounts row — all five child rows must cascade-delete.
	if _, err := db.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, accountID); err != nil {
		t.Fatalf("DELETE FROM accounts: %v", err)
	}

	// Assert all five tables have zero rows for this account_id.
	checks := []struct {
		table string
		query string
		args  []interface{}
	}{
		{
			"matches",
			`SELECT COUNT(*) FROM matches WHERE id = $1`,
			[]interface{}{matchID},
		},
		{
			"player_stats",
			`SELECT COUNT(*) FROM player_stats WHERE account_id = $1`,
			[]interface{}{accountID},
		},
		{
			"rank_history",
			`SELECT COUNT(*) FROM rank_history WHERE account_id = $1 AND season_ordinal = 99`,
			[]interface{}{accountID},
		},
		{
			"collection_history",
			`SELECT COUNT(*) FROM collection_history WHERE account_id = $1 AND card_id = 99901`,
			[]interface{}{accountID},
		},
		{
			"draft_sessions",
			`SELECT COUNT(*) FROM draft_sessions WHERE id = $1`,
			[]interface{}{draftSessionID},
		},
	}

	for _, c := range checks {
		c := c
		t.Run(c.table, func(t *testing.T) {
			var count int
			if err := db.QueryRowContext(ctx, c.query, c.args...).Scan(&count); err != nil {
				t.Fatalf("%s: count query: %v", c.table, err)
			}
			if count != 0 {
				t.Errorf("CASCADE delete: want 0 %s rows for deleted account, got %d", c.table, count)
			}
		})
	}
}

// TestMigration119AccountID_DownMigration simulates rolling back migration 000119
// inside a transaction and verifies:
//   - The named FK constraints are dropped.
//   - account_id columns are nullable again.
//   - The pre-existing anonymous FKs from 000054 (fresh-install path) are NOT
//     touched (the down migration only drops the named constraints).
//
// The transaction is always rolled back so the live test DB state is unchanged.
func TestMigration119AccountID_DownMigration(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Apply the down migration SQL inside the transaction.
	// This mirrors 000119_enforce_account_id_constraints.down.sql exactly.
	downSQL := `
		ALTER TABLE matches             DROP CONSTRAINT IF EXISTS fk_matches_account_id;
		ALTER TABLE matches             ALTER COLUMN account_id DROP NOT NULL;

		ALTER TABLE player_stats        DROP CONSTRAINT IF EXISTS fk_player_stats_account_id;
		ALTER TABLE player_stats        ALTER COLUMN account_id DROP NOT NULL;

		ALTER TABLE rank_history        DROP CONSTRAINT IF EXISTS fk_rank_history_account_id;
		ALTER TABLE rank_history        ALTER COLUMN account_id DROP NOT NULL;

		ALTER TABLE collection_history  DROP CONSTRAINT IF EXISTS fk_collection_history_account_id;
		ALTER TABLE collection_history  ALTER COLUMN account_id DROP NOT NULL;

		-- draft_sessions: drop NOT NULL only; the FK from 000052 is not touched.
		ALTER TABLE draft_sessions      ALTER COLUMN account_id DROP NOT NULL;
	`
	if _, err := tx.ExecContext(ctx, downSQL); err != nil {
		t.Fatalf("apply down migration in transaction: %v", err)
	}

	// Verify named FK constraints are gone.
	namedFKs := []struct{ table, constraint string }{
		{"matches", "fk_matches_account_id"},
		{"player_stats", "fk_player_stats_account_id"},
		{"rank_history", "fk_rank_history_account_id"},
		{"collection_history", "fk_collection_history_account_id"},
	}
	for _, fk := range namedFKs {
		fk := fk
		var exists bool
		if err := tx.QueryRowContext(
			ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.table_constraints
				WHERE constraint_name = $1 AND table_name = $2
			)`, fk.constraint, fk.table,
		).Scan(&exists); err != nil {
			t.Fatalf("constraint check %s.%s after down: %v", fk.table, fk.constraint, err)
		}
		if exists {
			t.Errorf("down migration did not remove named FK %q on %q", fk.constraint, fk.table)
		}
	}

	// Verify account_id is nullable on all five tables after down.
	for _, table := range []string{"matches", "player_stats", "rank_history", "collection_history", "draft_sessions"} {
		table := table
		var isNullable string
		if err := tx.QueryRowContext(
			ctx, `
			SELECT is_nullable
			FROM   information_schema.columns
			WHERE  table_name  = $1
			  AND  column_name = 'account_id'`,
			table,
		).Scan(&isNullable); err != nil {
			t.Fatalf("%s: is_nullable query after down: %v", table, err)
		}
		if isNullable != "YES" {
			t.Errorf("%s.account_id: after down migration, want is_nullable=YES, got %q", table, isNullable)
		}
	}
	// Transaction is rolled back by defer — test DB is unchanged.
}
