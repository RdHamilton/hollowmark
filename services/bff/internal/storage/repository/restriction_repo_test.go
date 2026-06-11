package repository_test

// ─── RestrictionRepository + DBHaltChecker integration tests ─────────────────
// These tests require DATABASE_URL + migration 000116 applied.
// They are skipped (not failed) when DATABASE_URL is not set (same pattern as
// all other repository integration tests in this package).
//
// Ticket: #890 GDPR Art.18 Right to Restriction (ADR-055)

import (
	"context"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/identityhash"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// TestSetProcessingRestriction verifies that SetProcessingRestriction sets
// processing_restricted_at to a non-null timestamp on the users row.
func TestSetProcessingRestriction(t *testing.T) {
	db := openTestDB(t)
	userRepo := repository.NewUserRepository(db)
	repo := repository.NewRestrictionRepository(db)

	clerkID := "restriction_test_set_" + t.Name()
	u, err := userRepo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("UpsertByClerkUserID: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM restriction_audit_log WHERE user_id = $1`, u.ID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM users WHERE clerk_user_id = $1`, clerkID)
	})

	if err := repo.SetProcessingRestriction(context.Background(), u.ID); err != nil {
		t.Fatalf("SetProcessingRestriction: %v", err)
	}

	var isRestricted bool
	err = db.QueryRowContext(
		context.Background(),
		`SELECT processing_restricted_at IS NOT NULL FROM users WHERE id = $1`, u.ID,
	).Scan(&isRestricted)
	if err != nil {
		t.Fatalf("SELECT users.processing_restricted_at: %v", err)
	}
	if !isRestricted {
		t.Error("processing_restricted_at should be non-null after SetProcessingRestriction")
	}
}

// TestClearProcessingRestriction verifies that ClearProcessingRestriction sets
// processing_restricted_at back to NULL on the users row.
func TestClearProcessingRestriction(t *testing.T) {
	db := openTestDB(t)
	userRepo := repository.NewUserRepository(db)
	repo := repository.NewRestrictionRepository(db)

	clerkID := "restriction_test_clear_" + t.Name()
	u, err := userRepo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("UpsertByClerkUserID: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM restriction_audit_log WHERE user_id = $1`, u.ID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM users WHERE clerk_user_id = $1`, clerkID)
	})

	// Set first.
	if err := repo.SetProcessingRestriction(context.Background(), u.ID); err != nil {
		t.Fatalf("SetProcessingRestriction: %v", err)
	}

	// Then clear.
	if err := repo.ClearProcessingRestriction(context.Background(), u.ID); err != nil {
		t.Fatalf("ClearProcessingRestriction: %v", err)
	}

	var isRestricted bool
	err = db.QueryRowContext(
		context.Background(),
		`SELECT processing_restricted_at IS NOT NULL FROM users WHERE id = $1`, u.ID,
	).Scan(&isRestricted)
	if err != nil {
		t.Fatalf("SELECT users.processing_restricted_at: %v", err)
	}
	if isRestricted {
		t.Error("processing_restricted_at should be NULL after ClearProcessingRestriction")
	}
}

// TestDBHaltChecker_IsHalted_WhenRestricted verifies that DBHaltChecker.IsHalted
// returns (true, nil) for an account whose user has processing_restricted_at set.
func TestDBHaltChecker_IsHalted_WhenRestricted(t *testing.T) {
	db := openTestDB(t)
	userRepo := repository.NewUserRepository(db)
	repo := repository.NewRestrictionRepository(db)
	checker := repository.NewDBHaltChecker(db)

	clerkID := "restriction_test_halt_set_" + t.Name()
	u, err := userRepo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("UpsertByClerkUserID: %v", err)
	}

	// Seed an account row with a known account_id_hash.
	// is_default is omitted (defaults to false): idx_accounts_default only allows
	// one is_default=true row globally, and this test only exercises IsHalted which
	// joins on account_id_hash — the default flag is irrelevant here.
	const testHash = "abcdef1234567890"
	var accountID int64
	err = db.QueryRowContext(context.Background(), `
		INSERT INTO accounts (user_id, name, account_id_hash)
		VALUES ($1, 'test halt set', $2)
		RETURNING id
	`, u.ID, testHash).Scan(&accountID)
	if err != nil {
		t.Fatalf("insert accounts: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM restriction_audit_log WHERE user_id = $1`, u.ID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM accounts WHERE id = $1`, accountID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM users WHERE clerk_user_id = $1`, clerkID)
	})

	if err := repo.SetProcessingRestriction(context.Background(), u.ID); err != nil {
		t.Fatalf("SetProcessingRestriction: %v", err)
	}

	halted, err := checker.IsHalted(context.Background(), testHash)
	if err != nil {
		t.Fatalf("IsHalted: %v", err)
	}
	if !halted {
		t.Error("IsHalted: want true for restricted user, got false")
	}
}

// TestDBHaltChecker_IsHalted_WhenNotRestricted verifies that DBHaltChecker.IsHalted
// returns (false, nil) for an account whose user has processing_restricted_at NULL.
func TestDBHaltChecker_IsHalted_WhenNotRestricted(t *testing.T) {
	db := openTestDB(t)
	userRepo := repository.NewUserRepository(db)
	checker := repository.NewDBHaltChecker(db)

	clerkID := "restriction_test_halt_clear_" + t.Name()
	u, err := userRepo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("UpsertByClerkUserID: %v", err)
	}

	// is_default omitted (defaults false) — idx_accounts_default only allows one
	// is_default=true row globally; this test does not exercise default logic.
	const testHash = "fedcba9876543210"
	var accountID int64
	err = db.QueryRowContext(context.Background(), `
		INSERT INTO accounts (user_id, name, account_id_hash)
		VALUES ($1, 'test', $2)
		RETURNING id
	`, u.ID, testHash).Scan(&accountID)
	if err != nil {
		t.Fatalf("insert accounts: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM accounts WHERE id = $1`, accountID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM users WHERE clerk_user_id = $1`, clerkID)
	})

	// No SetProcessingRestriction — column is NULL by default.
	halted, err := checker.IsHalted(context.Background(), testHash)
	if err != nil {
		t.Fatalf("IsHalted: %v", err)
	}
	if halted {
		t.Error("IsHalted: want false for unrestricted user, got true")
	}
}

// TestRestrictionAuditLog_RowOnSet verifies that SetProcessingRestriction paired
// with InsertAuditLogEntry writes a row with action='restricted' and actor='user'.
func TestRestrictionAuditLog_RowOnSet(t *testing.T) {
	db := openTestDB(t)
	userRepo := repository.NewUserRepository(db)
	repo := repository.NewRestrictionRepository(db)

	clerkID := "restriction_audit_set_" + t.Name()
	u, err := userRepo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("UpsertByClerkUserID: %v", err)
	}

	// is_default omitted (defaults false) — idx_accounts_default only allows one
	// is_default=true row globally; this test does not exercise default logic.
	var accountID int64
	err = db.QueryRowContext(context.Background(), `
		INSERT INTO accounts (user_id, name, account_id_hash)
		VALUES ($1, 'test', 'aaaa1111bbbb2222')
		RETURNING id
	`, u.ID).Scan(&accountID)
	if err != nil {
		t.Fatalf("insert accounts: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM restriction_audit_log WHERE user_id = $1`, u.ID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM accounts WHERE id = $1`, accountID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM users WHERE clerk_user_id = $1`, clerkID)
	})

	if err := repo.SetProcessingRestriction(context.Background(), u.ID); err != nil {
		t.Fatalf("SetProcessingRestriction: %v", err)
	}
	if err := repo.InsertAuditLogEntry(context.Background(), u.ID, accountID, "restricted", "user"); err != nil {
		t.Fatalf("InsertAuditLogEntry: %v", err)
	}

	var gotAction, gotActor string
	err = db.QueryRowContext(
		context.Background(),
		`SELECT action, actor FROM restriction_audit_log WHERE user_id = $1 ORDER BY restricted_at DESC LIMIT 1`,
		u.ID,
	).Scan(&gotAction, &gotActor)
	if err != nil {
		t.Fatalf("SELECT restriction_audit_log: %v", err)
	}
	if gotAction != "restricted" {
		t.Errorf("action: want %q, got %q", "restricted", gotAction)
	}
	if gotActor != "user" {
		t.Errorf("actor: want %q, got %q", "user", gotActor)
	}
}

// TestRestrictionAuditLog_RowOnClear verifies that ClearProcessingRestriction
// paired with InsertAuditLogEntry writes a row with action='unrestricted'.
func TestRestrictionAuditLog_RowOnClear(t *testing.T) {
	db := openTestDB(t)
	userRepo := repository.NewUserRepository(db)
	repo := repository.NewRestrictionRepository(db)

	clerkID := "restriction_audit_clear_" + t.Name()
	u, err := userRepo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("UpsertByClerkUserID: %v", err)
	}

	// is_default omitted (defaults false) — idx_accounts_default only allows one
	// is_default=true row globally; this test does not exercise default logic.
	var accountID int64
	err = db.QueryRowContext(context.Background(), `
		INSERT INTO accounts (user_id, name, account_id_hash)
		VALUES ($1, 'test', 'cccc3333dddd4444')
		RETURNING id
	`, u.ID).Scan(&accountID)
	if err != nil {
		t.Fatalf("insert accounts: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM restriction_audit_log WHERE user_id = $1`, u.ID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM accounts WHERE id = $1`, accountID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM users WHERE clerk_user_id = $1`, clerkID)
	})

	if err := repo.SetProcessingRestriction(context.Background(), u.ID); err != nil {
		t.Fatalf("SetProcessingRestriction: %v", err)
	}
	if err := repo.ClearProcessingRestriction(context.Background(), u.ID); err != nil {
		t.Fatalf("ClearProcessingRestriction: %v", err)
	}
	if err := repo.InsertAuditLogEntry(context.Background(), u.ID, accountID, "unrestricted", "user"); err != nil {
		t.Fatalf("InsertAuditLogEntry: %v", err)
	}

	var gotAction string
	err = db.QueryRowContext(
		context.Background(),
		`SELECT action FROM restriction_audit_log WHERE user_id = $1 ORDER BY restricted_at DESC LIMIT 1`,
		u.ID,
	).Scan(&gotAction)
	if err != nil {
		t.Fatalf("SELECT restriction_audit_log: %v", err)
	}
	if gotAction != "unrestricted" {
		t.Errorf("action: want %q, got %q", "unrestricted", gotAction)
	}
}

// TestRestrictionAuditLog_AdminActor verifies that InsertAuditLogEntry accepts
// actor='admin' and writes the value correctly.
func TestRestrictionAuditLog_AdminActor(t *testing.T) {
	db := openTestDB(t)
	userRepo := repository.NewUserRepository(db)
	repo := repository.NewRestrictionRepository(db)

	clerkID := "restriction_audit_admin_" + t.Name()
	u, err := userRepo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("UpsertByClerkUserID: %v", err)
	}

	// is_default omitted (defaults false) — idx_accounts_default only allows one
	// is_default=true row globally; this test does not exercise default logic.
	var accountID int64
	err = db.QueryRowContext(context.Background(), `
		INSERT INTO accounts (user_id, name, account_id_hash)
		VALUES ($1, 'test', 'eeee5555ffff6666')
		RETURNING id
	`, u.ID).Scan(&accountID)
	if err != nil {
		t.Fatalf("insert accounts: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM restriction_audit_log WHERE user_id = $1`, u.ID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM accounts WHERE id = $1`, accountID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM users WHERE clerk_user_id = $1`, clerkID)
	})

	if err := repo.SetProcessingRestriction(context.Background(), u.ID); err != nil {
		t.Fatalf("SetProcessingRestriction: %v", err)
	}
	if err := repo.InsertAuditLogEntry(context.Background(), u.ID, accountID, "restricted", "admin"); err != nil {
		t.Fatalf("InsertAuditLogEntry (admin): %v", err)
	}

	var gotActor string
	err = db.QueryRowContext(
		context.Background(),
		`SELECT actor FROM restriction_audit_log WHERE user_id = $1 ORDER BY restricted_at DESC LIMIT 1`,
		u.ID,
	).Scan(&gotActor)
	if err != nil {
		t.Fatalf("SELECT restriction_audit_log: %v", err)
	}
	if gotActor != "admin" {
		t.Errorf("actor: want %q, got %q", "admin", gotActor)
	}
}

// TestDBHaltChecker_IsHalted_HashMatchesClerkID is the critical regression test
// for the hash-mismatch defect (Ben's Blocker 1 on PR #3102).
//
// It proves that DBHaltChecker.IsHalted returns (true, nil) when queried with
// identityhash.HashAccountID(clerkUserID) — the EXACT value the analytics seam
// passes at runtime.  It relies on migration 000116 backfilling account_id_hash
// as SHA-256(users.clerk_user_id)[:16] via the users join, NOT SHA-256(accounts.id::text)[:16].
//
// If the backfill formula or the account INSERT path ever reverts to hashing the
// numeric BIGSERIAL, this test will catch it: the account_id_hash stored in the
// DB will differ from what the runtime passes, and IsHalted will return false.
func TestDBHaltChecker_IsHalted_HashMatchesClerkID(t *testing.T) {
	db := openTestDB(t)
	userRepo := repository.NewUserRepository(db)
	repo := repository.NewRestrictionRepository(db)
	checker := repository.NewDBHaltChecker(db)

	// Use a realistic Clerk-format user ID.
	clerkID := "user_test_halt_hash_" + t.Name()

	u, err := userRepo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("UpsertByClerkUserID: %v", err)
	}

	// Insert an account row WITHOUT manually specifying account_id_hash —
	// we rely on the INSERT path (GetOrCreateByClientID / account repo) to
	// populate it from users.clerk_user_id.  Use the repo method so we exercise
	// the production INSERT code path, not a hand-crafted SQL fixture.
	accountRepo := repository.NewAccountRepository(db)
	clientID := "MTGA_hash_test_" + t.Name()
	accountID, err := accountRepo.GetOrCreateByClientID(context.Background(), clientID, u.ID)
	if err != nil {
		t.Fatalf("GetOrCreateByClientID: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM restriction_audit_log WHERE user_id = $1`, u.ID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM accounts WHERE id = $1`, accountID)
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM users WHERE clerk_user_id = $1`, clerkID)
	})

	// Restrict the user.
	if err := repo.SetProcessingRestriction(context.Background(), u.ID); err != nil {
		t.Fatalf("SetProcessingRestriction: %v", err)
	}

	// The key assertion: query using the SAME hash the analytics seam produces
	// at runtime — identityhash.HashAccountID(clerkUserID).  If the DB column
	// holds a hash of the numeric account ID instead, this will return false.
	runtimeHash := identityhash.HashAccountID(clerkID)
	halted, err := checker.IsHalted(context.Background(), runtimeHash)
	if err != nil {
		t.Fatalf("IsHalted: %v", err)
	}
	if !halted {
		t.Errorf("IsHalted(%q): want true (account restricted + hash matches), got false — hash mismatch between DB column and runtime value", runtimeHash)
	}
}
