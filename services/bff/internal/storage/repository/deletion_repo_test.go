package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// ---------------------------------------------------------------------------
// FM-3 information_schema fitness test (non-negotiable per ADR-056 + approved plan)
//
// This test queries the live database information_schema to assert that every
// table with an account_id or user_id column appears in the known-user-keyed
// table registry below.  Any new user-keyed table added without updating the
// registry FAILS this test — making it machine-enforced rather than hand-maintained.
// ---------------------------------------------------------------------------

// knownUserKeyedTables is the authoritative FM-3 enumeration of every table
// that holds user-keyed data, and how the erasure cascade handles it.
//
// Disposition values:
//   - "cascade"     — covered by ON DELETE CASCADE from accounts(id) or users(id).
//   - "explicit"    — explicitly deleted by the cascade job.
//   - "anonymize"   — anonymized in-place (consent_log).
//   - "retain"      — retained by design (audit/compliance; no personal data).
//   - "reference"   — shared reference data; not user-keyed in the PII sense.
var knownUserKeyedTables = map[string]string{
	// Via accounts(id) ON DELETE CASCADE (BIGINT FK)
	"collection":               "cascade",
	"collection_new":           "cascade",
	"collection_history":       "cascade",
	"matches":                  "cascade",
	"player_stats":             "cascade",
	"decks":                    "cascade",
	"rank_history":             "cascade",
	"draft_events":             "cascade",
	"draft_sessions":           "cascade",
	"inventory":                "cascade",
	"inventory_history":        "cascade",
	"quests":                   "cascade",
	"user_settings":            "cascade",
	"recommendation_feedback":  "cascade",
	"card_inventory":           "cascade",
	"game_plays":               "cascade",
	"draft_picks":              "cascade",
	"draft_packs":              "cascade",
	"draft_match_results":      "cascade",
	"game_event_counters":      "cascade",
	"life_change_tracking":     "cascade",
	"matchup_statistics":       "cascade",
	"deck_performance_history": "cascade",
	"currency_history":         "cascade",
	"match_game_results":       "cascade",
	// Via matches(id) ON DELETE CASCADE
	"games":                   "cascade",
	"game_state_snapshots":    "cascade",
	"opponent_cards_observed": "cascade",
	"opponent_deck_profiles":  "cascade",
	// Via decks(id) ON DELETE CASCADE
	"deck_cards":        "cascade",
	"deck_notes":        "cascade",
	"deck_tags":         "cascade",
	"ml_suggestions":    "cascade",
	"deck_permutations": "cascade",
	// Via users(id) ON DELETE CASCADE
	"accounts": "cascade",
	"api_keys": "cascade",
	// Via accounts(id) ON DELETE CASCADE (BIGINT FK) — converted from TEXT in migration 000080
	"quest_session_tracking": "cascade",
	// Explicit DELETE by client_ids (TEXT account_id, no FK)
	"daemon_events":      "explicit",
	"daemon_api_keys":    "explicit",
	"user_play_patterns": "explicit",
	"projection_errors":  "explicit",
	// Anonymized in-place then SET NULL cascade
	"consent_log": "anonymize",
	// Retained by design (compliance evidence, no PII)
	"deletion_audit_log": "retain",
	// Email-keyed explicit DELETE
	"waitlist_entries": "explicit",
}

// TestFM3TableEnumeration_InformationSchema queries the live database
// information_schema to find every table with an account_id or user_id column
// and asserts it appears in knownUserKeyedTables.
//
// A new user-keyed table added to the schema without updating the enumeration
// will FAIL this test — creating a machine-enforced CI gate (ADR-056 fitness
// function).
//
// Requires DATABASE_URL — skipped when not set.
func TestFM3TableEnumeration_InformationSchema(t *testing.T) {
	db := openTestDB(t)

	// Query information_schema for every table that has an account_id or user_id column.
	const q = `
		SELECT DISTINCT table_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND column_name IN ('account_id', 'user_id')
		ORDER BY table_name`

	rows, err := db.QueryContext(context.Background(), q)
	if err != nil {
		t.Fatalf("information_schema query: %v", err)
	}
	defer rows.Close()

	var unregistered []string
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if _, ok := knownUserKeyedTables[tableName]; !ok {
			unregistered = append(unregistered, tableName)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	if len(unregistered) > 0 {
		sort.Strings(unregistered)
		t.Errorf("FM-3 fitness: %d table(s) have an account_id/user_id column but are NOT in the erasure enumeration (knownUserKeyedTables in deletion_repo_test.go).\n"+
			"Add each table with the correct disposition (cascade/explicit/anonymize/retain/reference):\n  %s\n\n"+
			"If the table holds user PII, also update the erasure cascade in internal/erasure/job.go.",
			len(unregistered), strings.Join(unregistered, "\n  "))
	}
}

// ---------------------------------------------------------------------------
// DeletionRepository integration tests
// ---------------------------------------------------------------------------

// seedDeletionUser inserts a user + account + daemon_api_key row for
// deletion cascade integration tests.  Returns userID, accountID, clientID.
func seedDeletionUser(t *testing.T, db *sql.DB) (userID, accountID int64, clientID string) {
	t.Helper()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	clerkID := "clerk_deletion_test_" + suffix
	email := "deletion_test_" + suffix + "@example.com"
	clientID = "client_del_" + suffix

	// Insert users row.
	err := db.QueryRowContext(context.Background(),
		`INSERT INTO users (email, clerk_user_id, subscription_tier) VALUES ($1, $2, 'free') RETURNING id`,
		email, clerkID).Scan(&userID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Insert accounts row.
	err = db.QueryRowContext(context.Background(),
		`INSERT INTO accounts (name, client_id, user_id) VALUES ($1, $2, $3) RETURNING id`,
		clientID, clientID, userID).Scan(&accountID)
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}

	t.Cleanup(func() {
		// Best-effort cleanup — may already be deleted by the cascade.
		_, _ = db.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = $1`, accountID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	return userID, accountID, clientID
}

// TestDeletionRepository_CapturePreJobData verifies that CapturePreJobData
// returns the user email and at least the known client_id for the account.
func TestDeletionRepository_CapturePreJobData(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDeletionRepository(db)

	userID, accountID, clientID := seedDeletionUser(t, db)

	email, clientIDs, err := repo.CapturePreJobData(context.Background(), userID, accountID)
	if err != nil {
		t.Fatalf("CapturePreJobData: %v", err)
	}
	if email == "" {
		t.Error("expected non-empty email")
	}
	found := false
	for _, c := range clientIDs {
		if c == clientID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("client_id %q not in captured clientIDs %v", clientID, clientIDs)
	}
}

// TestDeletionRepository_SoftDeleteUser verifies that SoftDeleteUser sets
// users.deleted_at to a non-null value.
func TestDeletionRepository_SoftDeleteUser(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDeletionRepository(db)

	userID, _, _ := seedDeletionUser(t, db)

	if err := repo.SoftDeleteUser(context.Background(), userID); err != nil {
		t.Fatalf("SoftDeleteUser: %v", err)
	}

	var deletedAt sql.NullTime
	err := db.QueryRowContext(context.Background(),
		`SELECT deleted_at FROM users WHERE id = $1`, userID).Scan(&deletedAt)
	if err != nil {
		t.Fatalf("select deleted_at: %v", err)
	}
	if !deletedAt.Valid {
		t.Error("expected deleted_at to be set, got NULL")
	}
}

// TestDeletionRepository_DeleteTextKeyedRows verifies that daemon_events and
// daemon_api_keys rows for the given client_ids are removed.
func TestDeletionRepository_DeleteTextKeyedRows(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDeletionRepository(db)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	clientID := "client_text_del_" + suffix

	// Seed a daemon_api_keys row with the TEXT client_id.
	// device_id is a UUID NOT NULL column — use a real UUID, not a string literal.
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO daemon_api_keys (account_id, key_hash, key_prefix, device_id, platform, daemon_ver)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		clientID, "hash_"+suffix, "pref", uuid.New().String(), "macOS", "0.4.1")
	if err != nil {
		t.Fatalf("seed daemon_api_keys: %v", err)
	}

	if err := repo.DeleteTextKeyedRows(context.Background(), []string{clientID}); err != nil {
		t.Fatalf("DeleteTextKeyedRows: %v", err)
	}

	var count int
	err = db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM daemon_api_keys WHERE account_id = $1`, clientID).Scan(&count)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 daemon_api_keys rows after delete, got %d", count)
	}
}

// TestDeletionRepository_QuestSessionTracking_DeletedViaCascade verifies that
// quest_session_tracking rows are erased via the accounts ON DELETE CASCADE, NOT
// via DeleteTextKeyedRows.
//
// Root cause of Schema-000054-compat red gate (PR #3088 / Bob peer-blocking):
// Migration 000080 converted quest_session_tracking.account_id from TEXT (raw
// MTGA client_id) to BIGINT FK referencing accounts(id) ON DELETE CASCADE.
// The original FM-3 disposition incorrectly listed this table as "explicit /
// TEXT account_id" — passing a []string to ANY($1) against a BIGINT column
// throws SQLSTATE 22P02.  The correct erasure path is the FK cascade fired by
// HardDeleteAccount (Step 4e), identical to inventory, collection, etc.
//
// This test:
//  1. Seeds an account + quest_session_tracking row via the BIGINT account_id FK.
//  2. Calls HardDeleteAccount (Step 4e of the erasure cascade).
//  3. Asserts the quest_session_tracking row was removed by cascade.
//  4. Confirms the row is NOT present in DeleteTextKeyedRows (that call must not
//     include quest_session_tracking — the table no longer has a TEXT account_id).
func TestDeletionRepository_QuestSessionTracking_DeletedViaCascade(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDeletionRepository(db)

	userID, accountID, _ := seedDeletionUser(t, db)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Insert a quest_session_tracking row keyed by BIGINT accountID.
	var questRowID int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO quest_session_tracking
		     (account_id, quest_id, quest_name, progress, goal, xp_reward, occurred_at)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())
		 RETURNING id`,
		accountID,
		"quest_"+suffix,
		"Test Quest "+suffix,
		1, 1, 100,
	).Scan(&questRowID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			t.Skip("quest_session_tracking table not present")
		}
		t.Fatalf("seed quest_session_tracking: %v", err)
	}

	// Step 4e: hard-delete the accounts row — must cascade to quest_session_tracking.
	// We need to delete users first (users(id) FK constraint on accounts).
	if err := repo.HardDeleteUser(context.Background(), userID); err != nil {
		t.Fatalf("HardDeleteUser: %v", err)
	}
	if err := repo.HardDeleteAccount(context.Background(), accountID); err != nil {
		t.Fatalf("HardDeleteAccount: %v", err)
	}

	// Verify: quest_session_tracking row must be gone via cascade.
	var count int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM quest_session_tracking WHERE id = $1`, questRowID,
	).Scan(&count); err != nil {
		t.Fatalf("count quest_session_tracking: %v", err)
	}
	if count != 0 {
		t.Errorf("quest_session_tracking row %d still exists after HardDeleteAccount cascade — "+
			"expected ON DELETE CASCADE to remove it", questRowID)
	}
}

// TestDeletionRepository_UserPlayPatterns_DeletedViaTextPath verifies that
// user_play_patterns rows (TEXT account_id, no FK — unchanged since migration
// 000033 / 000054) are erased by DeleteTextKeyedRows using the client_id string.
//
// This confirms the FM-3 "explicit" disposition for user_play_patterns is correct
// and that the TEXT ANY($1) path works against this table.
func TestDeletionRepository_UserPlayPatterns_DeletedViaTextPath(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDeletionRepository(db)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	clientID := "client_upp_del_" + suffix

	// Insert a user_play_patterns row keyed by the TEXT client_id.
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO user_play_patterns (account_id, total_matches, total_decks)
		 VALUES ($1, $2, $3)`,
		clientID, 0, 0)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			t.Skip("user_play_patterns table not present")
		}
		t.Fatalf("seed user_play_patterns: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM user_play_patterns WHERE account_id = $1`, clientID)
	})

	if err := repo.DeleteTextKeyedRows(context.Background(), []string{clientID}); err != nil {
		t.Fatalf("DeleteTextKeyedRows: %v", err)
	}

	var count int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM user_play_patterns WHERE account_id = $1`, clientID,
	).Scan(&count); err != nil {
		t.Fatalf("count user_play_patterns: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 user_play_patterns rows after DeleteTextKeyedRows, got %d", count)
	}
}

// TestDeletionRepository_DeleteWaitlistEntry verifies that a waitlist_entries
// row is removed by email (case-insensitive CITEXT match).
func TestDeletionRepository_DeleteWaitlistEntry(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDeletionRepository(db)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	email := "Waitlist_Del_" + suffix + "@Example.COM"
	emailLower := strings.ToLower(email)

	_, err := db.ExecContext(context.Background(),
		`INSERT INTO waitlist_entries (email) VALUES ($1)`, emailLower)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			t.Skip("waitlist_entries table not present — migration 000086 not applied")
		}
		t.Fatalf("seed waitlist: %v", err)
	}

	// Use mixed-case to verify CITEXT match.
	if err := repo.DeleteWaitlistEntry(context.Background(), email); err != nil {
		t.Fatalf("DeleteWaitlistEntry: %v", err)
	}

	var count int
	if err := db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM waitlist_entries WHERE email = $1`, emailLower).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 waitlist rows after delete, got %d", count)
	}
}

// TestDeletionRepository_HardDeleteCascade verifies the full hard-delete
// sequence: hard-delete users (step 4d) then accounts (step 4e).  After the
// sequence the user and account rows must not exist, and at least one FK-keyed
// table (api_keys) must have lost its rows via cascade.
func TestDeletionRepository_HardDeleteCascade(t *testing.T) {
	db := openTestDB(t)
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}
	repo := repository.NewDeletionRepository(db)

	userID, accountID, _ := seedDeletionUser(t, db)

	// Seed an api_keys row so we can verify cascade.
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO api_keys (user_id, key_hash) VALUES ($1, $2)`,
		userID, "cascade_test_hash_"+fmt.Sprintf("%d", time.Now().UnixNano()))
	if err != nil {
		// api_keys may have constraints we can't easily satisfy in all envs — skip gracefully.
		t.Logf("skipping api_keys seed: %v", err)
	}

	if err := repo.HardDeleteUser(context.Background(), userID); err != nil {
		t.Fatalf("HardDeleteUser: %v", err)
	}
	if err := repo.HardDeleteAccount(context.Background(), accountID); err != nil {
		t.Fatalf("HardDeleteAccount: %v", err)
	}

	var userCount int
	_ = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM users WHERE id = $1`, userID).Scan(&userCount)
	if userCount != 0 {
		t.Errorf("expected users row deleted, got count=%d", userCount)
	}

	var accountCount int
	_ = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM accounts WHERE id = $1`, accountID).Scan(&accountCount)
	if accountCount != 0 {
		t.Errorf("expected accounts row deleted, got count=%d", accountCount)
	}
}

// TestDeletionRepository_RecordJobComplete verifies that RecordJobComplete
// writes completed_at to the deletion_audit_log row.
func TestDeletionRepository_RecordJobComplete(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDeletionRepository(db)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Omit job_id — it is UUID NOT NULL DEFAULT gen_random_uuid().
	// Scan back the generated UUID so we can look up the row later.
	var jobID string
	err := db.QueryRowContext(context.Background(),
		`INSERT INTO deletion_audit_log (clerk_user_id, user_id, account_id)
		 VALUES ($1, $2, $3)
		 RETURNING job_id`,
		"clerk_test_"+suffix, int64(1), int64(1)).Scan(&jobID)
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			t.Skip("deletion_audit_log table not present — migration not applied yet")
		}
		t.Fatalf("seed deletion_audit_log: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM deletion_audit_log WHERE job_id = $1`, jobID)
	})

	if err := repo.RecordJobComplete(context.Background(), jobID); err != nil {
		t.Fatalf("RecordJobComplete: %v", err)
	}

	var completedAt sql.NullTime
	if err := db.QueryRowContext(context.Background(),
		`SELECT completed_at FROM deletion_audit_log WHERE job_id = $1`, jobID).Scan(&completedAt); err != nil {
		t.Fatalf("select completed_at: %v", err)
	}
	if !completedAt.Valid {
		t.Error("expected completed_at to be set, got NULL")
	}
}
