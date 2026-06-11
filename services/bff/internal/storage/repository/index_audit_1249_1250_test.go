package repository_test

// index_audit_1249_1250_test.go — index coverage audit for tickets #1249 and #1250.
//
// Background
// ----------
// Ben's v0.4.3 wave-start DB health check (2026-06-10) observed high seq_scan
// ratios on daemon_api_keys, users, and accounts.  Tickets #1249 and #1250 were
// filed to investigate and add indexes where genuinely missing.
//
// Audit methodology
// -----------------
// 1. Enumerated every query predicate in the repository layer (grep FROM table
//    + WHERE clauses across all *.go files that are not _test.go).
// 2. Traced each predicate to an existing index across all migrations up to 000122.
// 3. Confirmed no index is genuinely missing.
//
// Findings — daemon_api_keys (#1249)
// ------------------------------------
// The 116k seq_scan count reflected historical accumulation from before
// migration 000112 (app/v0.4.2, shipped 2026-06-10) added
// idx_daemon_api_keys_account_id.  All query predicates are now covered:
//
//   Predicate                                          Index (migration)
//   ─────────────────────────────────────────────────  ───────────────────────────────────────────────
//   key_prefix = $1 AND revoked_at IS NULL             daemon_api_keys_key_prefix_active_idx  (000085)
//   account_id = $1 AND revoked_at IS NULL             daemon_api_keys_account_active_idx     (000085)
//   account_id = $1 AND device_id = $2                 daemon_api_keys_account_device_unique  (000085)
//   account_id = ANY($1)  [GDPR erasure DELETE]        idx_daemon_api_keys_account_id         (000112)
//   id = $1                                            PRIMARY KEY
//
// No new migration is warranted.  Outcome: won't-fix (already resolved).
//
// Findings — users / accounts (#1250)
// -------------------------------------
// At beta scale, users and accounts are small tables.  The PostgreSQL planner
// correctly prefers seq-scans when a table fits in a small number of 8 KB blocks
// (micro-table false-signal, same class as tickets #1044 and #1161).  Every
// non-PK predicate that appears in the repository layer is already covered:
//
//   users
//   ─────
//   id = $1               PRIMARY KEY (BIGSERIAL)
//   clerk_user_id = $1    idx_users_clerk_user_id (UNIQUE partial, 000066)
//
//   accounts
//   ────────
//   id = $1               PRIMARY KEY (BIGSERIAL)
//   user_id = $1          idx_accounts_user_id (000051 / 000054)
//   client_id = $1        accounts_client_id_unique UNIQUE constraint (000082; implicit btree)
//   account_id_hash = $1  accounts_account_id_hash_idx (000116)
//
// No new migration is warranted.  Outcome: won't-fix (micro-table false-signal).
//
// These tests are fitness functions — they assert the required index state so
// that a future migration cannot silently drop a load-bearing index without
// test coverage catching it.
//
// All tests skip when DATABASE_URL is not set.

import (
	"context"
	"testing"
)

// ─── daemon_api_keys index assertions (#1249) ─────────────────────────────────

// TestIndexAudit1249_DaemonAPIKeys_AllIndexesPresent verifies that every index
// serving a daemon_api_keys query predicate exists in pg_indexes.  If any of
// these indexes is dropped, the corresponding query will fall back to a
// full table scan on a hot table (every daemon ingest auth check uses this table).
func TestIndexAudit1249_DaemonAPIKeys_AllIndexesPresent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	type indexCheck struct {
		name string
		desc string
	}

	required := []indexCheck{
		{
			"daemon_api_keys_key_prefix_active_idx",
			"WHERE key_prefix = $1 AND revoked_at IS NULL — DaemonAPIKeyAuth middleware hot path (GetByPrefix); migration 000085",
		},
		{
			"daemon_api_keys_account_active_idx",
			"WHERE account_id = $1 AND revoked_at IS NULL — GetActive, ListByAccountID; migration 000085",
		},
		{
			"daemon_api_keys_account_device_unique",
			"(account_id, device_id) UNIQUE constraint — covers GetByAccountAndDevice + enforces multi-device uniqueness; migration 000085",
		},
		{
			"idx_daemon_api_keys_account_id",
			"WHERE account_id = ANY($1) — GDPR Art.17 erasure DELETE in deletion_repo.go; migration 000112",
		},
	}

	for _, r := range required {
		r := r
		t.Run(r.name, func(t *testing.T) {
			var exists bool
			if err := db.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM pg_indexes
					WHERE tablename = 'daemon_api_keys'
					  AND indexname  = $1
				)`, r.name).Scan(&exists); err != nil {
				t.Fatalf("pg_indexes query for %q: %v", r.name, err)
			}
			if !exists {
				t.Errorf("index %q is MISSING from daemon_api_keys — queries will seq-scan: %s", r.name, r.desc)
			}
		})
	}
}

// TestIndexAudit1249_DaemonAPIKeys_AuthHotPath verifies that the prefix-based
// lookup query used by DaemonAPIKeyAuth (GetByPrefix) executes correctly.
// An unknown prefix returns zero rows, which is the expected hot-path miss path.
func TestIndexAudit1249_DaemonAPIKeys_AuthHotPath(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var count int
	if err := db.QueryRowContext(
		ctx, `
		SELECT COUNT(*) FROM daemon_api_keys
		WHERE key_prefix = $1 AND revoked_at IS NULL`,
		"audit1249-probe-pfx-nonexistent",
	).Scan(&count); err != nil {
		t.Fatalf("GetByPrefix pattern query: %v", err)
	}
}

// TestIndexAudit1249_DaemonAPIKeys_ErasureDeletePath verifies that the GDPR
// Art.17 erasure DELETE in deletion_repo.go executes correctly against
// idx_daemon_api_keys_account_id (migration 000112).
func TestIndexAudit1249_DaemonAPIKeys_ErasureDeletePath(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	if _, err := db.ExecContext(
		ctx, `
		DELETE FROM daemon_api_keys
		WHERE account_id = ANY($1)`,
		[]string{"audit1249-erasure-probe-nonexistent"},
	); err != nil {
		t.Fatalf("GDPR erasure DELETE WHERE account_id = ANY($1): %v", err)
	}
}

// ─── users / accounts index assertions (#1250) ───────────────────────────────

// TestIndexAudit1250_Users_AllIndexesPresent verifies the indexes on users that
// cover non-PK repository query predicates.  Audit finding: no new index needed
// (micro-table false-signal; all predicates covered).
func TestIndexAudit1250_Users_AllIndexesPresent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	type indexCheck struct {
		name string
		desc string
	}

	required := []indexCheck{
		{
			"idx_users_clerk_user_id",
			"WHERE clerk_user_id = $1 — GetByClerkUserID, UpsertByClerkUserID ON CONFLICT, deletion_repo.go; migration 000066 (partial UNIQUE)",
		},
	}

	for _, r := range required {
		r := r
		t.Run(r.name, func(t *testing.T) {
			var exists bool
			if err := db.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM pg_indexes
					WHERE tablename = 'users'
					  AND indexname  = $1
				)`, r.name).Scan(&exists); err != nil {
				t.Fatalf("pg_indexes query for %q: %v", r.name, err)
			}
			if !exists {
				t.Errorf("index %q is MISSING from users: %s", r.name, r.desc)
			}
		})
	}
}

// TestIndexAudit1250_Accounts_AllIndexesPresent verifies the indexes on accounts
// that cover non-PK repository query predicates.  Audit finding: no new index
// needed (micro-table false-signal; all predicates covered).
func TestIndexAudit1250_Accounts_AllIndexesPresent(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	type indexCheck struct {
		name string
		desc string
	}

	required := []indexCheck{
		{
			"idx_accounts_user_id",
			"WHERE user_id = $1 — GetByUserID, GetAccountIDByUserID, data_export_repo, deletion_repo; migration 000051/000054",
		},
		{
			"accounts_client_id_unique",
			"WHERE client_id = $1 — GetOrCreateByClientID; UNIQUE constraint from migration 000082 (implicit btree)",
		},
		{
			"accounts_account_id_hash_idx",
			"WHERE account_id_hash = $1 — DBHaltChecker.IsHalted (restriction_repo.go); migration 000116",
		},
	}

	for _, r := range required {
		r := r
		t.Run(r.name, func(t *testing.T) {
			var exists bool
			if err := db.QueryRowContext(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM pg_indexes
					WHERE tablename = 'accounts'
					  AND indexname  = $1
				)`, r.name).Scan(&exists); err != nil {
				t.Fatalf("pg_indexes query for %q: %v", r.name, err)
			}
			if !exists {
				t.Errorf("index %q is MISSING from accounts: %s", r.name, r.desc)
			}
		})
	}
}

// TestIndexAudit1250_Users_ClerkUserIDLookupWorks verifies that the
// clerk_user_id lookup executes without error (structural guard against
// column type or constraint regression).
func TestIndexAudit1250_Users_ClerkUserIDLookupWorks(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var count int
	if err := db.QueryRowContext(
		ctx, `
		SELECT COUNT(*) FROM users WHERE clerk_user_id = $1`,
		"audit1250-probe-clerk-nonexistent",
	).Scan(&count); err != nil {
		t.Fatalf("clerk_user_id lookup: %v", err)
	}
}

// TestIndexAudit1250_Accounts_UserIDLookupWorks verifies that the user_id
// lookup (GetByUserID, GetAccountIDByUserID) executes without error.
func TestIndexAudit1250_Accounts_UserIDLookupWorks(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var count int
	if err := db.QueryRowContext(
		ctx, `
		SELECT COUNT(*) FROM accounts WHERE user_id = $1`,
		int64(9999999),
	).Scan(&count); err != nil {
		t.Fatalf("user_id lookup: %v", err)
	}
}

// TestIndexAudit1250_Accounts_ClientIDLookupWorks verifies that the client_id
// lookup (GetOrCreateByClientID) executes without error via the UNIQUE
// constraint index from migration 000082.
func TestIndexAudit1250_Accounts_ClientIDLookupWorks(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var count int
	if err := db.QueryRowContext(
		ctx, `
		SELECT COUNT(*) FROM accounts WHERE client_id = $1`,
		"audit1250-probe-client-nonexistent",
	).Scan(&count); err != nil {
		t.Fatalf("client_id lookup: %v", err)
	}
}
