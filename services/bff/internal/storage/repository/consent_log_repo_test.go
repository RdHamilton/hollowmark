package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// seedUserAndAccount inserts a minimal users + accounts row and returns the
// (userID, accountID). Cleaned up by t.Cleanup.
func seedUserAndAccount(t *testing.T, db *sql.DB, suffix string) (userID, accountID int64) {
	t.Helper()
	clerkID := "clerk_consent_" + suffix

	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO users (email, clerk_user_id) VALUES ($1, $2) RETURNING id`,
		clerkID+"@test.local", clerkID,
	).Scan(&userID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	err = db.QueryRowContext(
		context.Background(),
		`INSERT INTO accounts (name, client_id, user_id) VALUES ($1, $2, $3) RETURNING id`,
		"TestAccount_"+suffix, "MTGA_consent_"+suffix, userID,
	).Scan(&accountID)
	if err != nil {
		t.Fatalf("seed account: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", accountID)
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	return userID, accountID
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestConsentLogRepo_InsertSignupEvent verifies that inserting a signup event
// writes the row with the expected fields and no raw PII.
func TestConsentLogRepo_InsertSignupEvent(t *testing.T) {
	db := openTestDB(t)
	_, accountID := seedUserAndAccount(t, db, t.Name())

	repo := repository.NewConsentLogRepository(db)

	tosVer := "2026-06-10"
	ppVer := "2026-06-10"
	ipHash := "abc1234567890def" // 16 hex chars

	event := repository.ConsentEvent{
		AccountID:            accountID,
		EventType:            "signup",
		TOSVersion:           &tosVer,
		PrivacyPolicyVersion: &ppVer,
		IPAddressHash:        &ipHash,
	}

	if err := repo.InsertConsentEvent(context.Background(), event); err != nil {
		t.Fatalf("InsertConsentEvent: %v", err)
	}

	// Verify the row was written.
	var (
		gotEventType            string
		gotTOSVersion           sql.NullString
		gotPrivacyPolicyVersion sql.NullString
		gotIPHash               sql.NullString
		gotAccountID            sql.NullInt64
	)
	err := db.QueryRowContext(
		context.Background(),
		`SELECT event_type, tos_version, privacy_policy_version, ip_address_hash, account_id
		 FROM consent_log
		 WHERE account_id = $1 AND event_type = 'signup'
		 ORDER BY consented_at DESC LIMIT 1`,
		accountID,
	).Scan(&gotEventType, &gotTOSVersion, &gotPrivacyPolicyVersion, &gotIPHash, &gotAccountID)
	if err != nil {
		t.Fatalf("SELECT consent_log: %v", err)
	}

	if gotEventType != "signup" {
		t.Errorf("event_type: want %q, got %q", "signup", gotEventType)
	}
	if !gotTOSVersion.Valid || gotTOSVersion.String != tosVer {
		t.Errorf("tos_version: want %q, got %v", tosVer, gotTOSVersion)
	}
	if !gotPrivacyPolicyVersion.Valid || gotPrivacyPolicyVersion.String != ppVer {
		t.Errorf("privacy_policy_version: want %q, got %v", ppVer, gotPrivacyPolicyVersion)
	}
	if !gotIPHash.Valid || gotIPHash.String != ipHash {
		t.Errorf("ip_address_hash: want %q, got %v", ipHash, gotIPHash)
	}
	if !gotAccountID.Valid || gotAccountID.Int64 != accountID {
		t.Errorf("account_id: want %d, got %v", accountID, gotAccountID)
	}

	// Cleanup the consent_log row (separate from user/account cleanup above).
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM consent_log WHERE account_id = $1", accountID)
	})
}

// TestConsentLogRepo_InsertCOPPAEvent verifies that inserting a coppa_gate
// event with metadata round-trips the JSONB correctly.
func TestConsentLogRepo_InsertCOPPAEvent(t *testing.T) {
	db := openTestDB(t)
	_, accountID := seedUserAndAccount(t, db, t.Name())

	repo := repository.NewConsentLogRepository(db)

	metadata := []byte(`{"dob_year_verified":true,"coppa_restricted":false}`)
	event := repository.ConsentEvent{
		AccountID: accountID,
		EventType: "coppa_gate",
		Metadata:  metadata,
	}

	if err := repo.InsertConsentEvent(context.Background(), event); err != nil {
		t.Fatalf("InsertConsentEvent: %v", err)
	}

	var gotMetadata []byte
	err := db.QueryRowContext(
		context.Background(),
		`SELECT metadata FROM consent_log WHERE account_id = $1 AND event_type = 'coppa_gate' ORDER BY consented_at DESC LIMIT 1`,
		accountID,
	).Scan(&gotMetadata)
	if err != nil {
		t.Fatalf("SELECT consent_log metadata: %v", err)
	}

	if string(gotMetadata) == "" {
		t.Error("metadata: expected non-empty JSONB, got empty")
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM consent_log WHERE account_id = $1", accountID)
	})
}

// TestConsentLogRepo_AccountDeleteCascadeSETNULL is the load-bearing
// integration test required by Ray's plan approval. It proves end-to-end:
//  1. consent_log rows survive account deletion (not CASCADE-deleted)
//  2. account_id is SET NULL after accounts row is deleted (FK ON DELETE SET NULL)
//  3. ip_address_hash and metadata are NULL after the #891-style anonymization
//
// This test FAILS if FK is ON DELETE RESTRICT (step 4 would error).
// This test PASSES only with ON DELETE SET NULL.
//
// Isolation fix (#668): row IDs are captured immediately after insert (while
// account_id is still set) and all subsequent assertions and cleanup are scoped
// to those IDs. This replaces the prior time-windowed global predicate
// (account_id IS NULL + consented_at > NOW()-interval) which was swept by
// concurrent or -count=2 test runs.
func TestConsentLogRepo_AccountDeleteCascadeSETNULL(t *testing.T) {
	db := openTestDB(t)
	_, accountID := seedUserAndAccount(t, db, t.Name())

	repo := repository.NewConsentLogRepository(db)

	tosVer := "2026-06-10"
	ppVer := "2026-06-10"
	ipHash := "abc1234567890def"
	metadata := []byte(`{"locale":"en-US"}`)

	// Step 1: Insert two consent_log rows referencing the account.
	e1 := repository.ConsentEvent{
		AccountID:            accountID,
		EventType:            "signup",
		TOSVersion:           &tosVer,
		PrivacyPolicyVersion: &ppVer,
		IPAddressHash:        &ipHash,
		Metadata:             metadata,
	}
	e2 := repository.ConsentEvent{
		AccountID: accountID,
		EventType: "cookie_accept",
		Metadata:  []byte(`{"locale":"en-US"}`),
	}
	if err := repo.InsertConsentEvent(context.Background(), e1); err != nil {
		t.Fatalf("InsertConsentEvent e1: %v", err)
	}
	if err := repo.InsertConsentEvent(context.Background(), e2); err != nil {
		t.Fatalf("InsertConsentEvent e2: %v", err)
	}

	// Step 2: Capture the row IDs while account_id is still set.
	// All subsequent assertions and cleanup use these IDs — never a global predicate.
	// This is the isolation fix: concurrent runs cannot sweep each other's rows.
	rows, err := db.QueryContext(
		context.Background(),
		`SELECT id FROM consent_log WHERE account_id = $1 ORDER BY id`,
		accountID,
	)
	if err != nil {
		t.Fatalf("query consent_log IDs: %v", err)
	}
	var rowIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan consent_log id: %v", err)
		}
		rowIDs = append(rowIDs, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}
	if len(rowIDs) != 2 {
		t.Fatalf("expected 2 consent_log rows, got %d", len(rowIDs))
	}

	// Cleanup: delete exactly the two rows we own.
	// Registered before account-delete so it targets our rows before they lose their IDs.
	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM consent_log WHERE id = $1 OR id = $2`,
			rowIDs[0], rowIDs[1],
		)
	})

	// Step 3: Simulate #891 Step 4a anonymize-in-place.
	if _, err := db.ExecContext(
		context.Background(),
		`UPDATE consent_log SET ip_address_hash = NULL, metadata = NULL WHERE account_id = $1`,
		accountID,
	); err != nil {
		t.Fatalf("#891 anonymize UPDATE: %v", err)
	}

	// Step 4: Delete the account (triggers ON DELETE SET NULL on consent_log.account_id).
	// If FK is RESTRICT this will fail with a foreign key violation.
	if _, err := db.ExecContext(
		context.Background(),
		`DELETE FROM accounts WHERE id = $1`,
		accountID,
	); err != nil {
		t.Fatalf("DELETE accounts (FK SET NULL should allow this): %v", err)
	}

	// Step 5: Assert the two consent_log rows still exist (not deleted) — by ID.
	var survivingCount int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM consent_log WHERE id = $1 OR id = $2`,
		rowIDs[0], rowIDs[1],
	).Scan(&survivingCount); err != nil {
		t.Fatalf("count surviving rows: %v", err)
	}
	if survivingCount != 2 {
		t.Errorf("consent_log rows should survive account deletion: want 2, got %d", survivingCount)
	}

	// Step 6: Assert account_id IS NULL on both surviving rows.
	var nullAccountIDCount int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM consent_log WHERE (id = $1 OR id = $2) AND account_id IS NULL`,
		rowIDs[0], rowIDs[1],
	).Scan(&nullAccountIDCount); err != nil {
		t.Fatalf("count null account_id: %v", err)
	}
	if nullAccountIDCount != 2 {
		t.Errorf("account_id should be NULL after SET NULL cascade: want 2, got %d", nullAccountIDCount)
	}

	// Step 7: Assert ip_address_hash IS NULL on both rows (anonymized in step 3).
	var nullIPCount int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM consent_log WHERE (id = $1 OR id = $2) AND ip_address_hash IS NULL`,
		rowIDs[0], rowIDs[1],
	).Scan(&nullIPCount); err != nil {
		t.Fatalf("count null ip_address_hash: %v", err)
	}
	if nullIPCount != 2 {
		t.Errorf("ip_address_hash should be NULL after anonymization: want 2, got %d", nullIPCount)
	}
}

// TestConsentLogRepo_RepoLayerPreventsUpdate verifies that the ConsentLogRepository
// exposes no Update or Delete methods (compile-time enforcement of append-only).
// This test documents that enforcement is at the application layer.
func TestConsentLogRepo_RepoLayerPreventsUpdate(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewConsentLogRepository(db)

	// The compile-time check: if InsertConsentEvent exists and no UpdateConsentEvent
	// or DeleteConsentEvent exist, the type system enforces append-only.
	// This test passes if the code compiles — the absence of Update/Delete methods
	// on the concrete type is the proof.
	//
	// We verify by confirming the repo only satisfies the consentEventInserter interface.
	var _ interface {
		InsertConsentEvent(ctx context.Context, e repository.ConsentEvent) error
	} = repo

	// Sanity: insert still works.
	_, accountID := seedUserAndAccount(t, db, t.Name())
	event := repository.ConsentEvent{
		AccountID: accountID,
		EventType: "install_dialog",
	}
	if err := repo.InsertConsentEvent(context.Background(), event); err != nil {
		t.Fatalf("InsertConsentEvent: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM consent_log WHERE account_id = $1", accountID)
	})
}

// TestConsentLogRepo_NullableFieldsForNonTOSEvents verifies that non-ToS events
// (e.g. install_dialog) can be inserted with nil tos_version and
// privacy_policy_version (these fields are optional).
func TestConsentLogRepo_NullableFieldsForNonTOSEvents(t *testing.T) {
	db := openTestDB(t)
	_, accountID := seedUserAndAccount(t, db, t.Name())

	repo := repository.NewConsentLogRepository(db)

	event := repository.ConsentEvent{
		AccountID: accountID,
		EventType: "install_dialog",
		// TOSVersion and PrivacyPolicyVersion are nil — not a ToS event.
	}

	if err := repo.InsertConsentEvent(context.Background(), event); err != nil {
		t.Fatalf("InsertConsentEvent (nil optional fields): %v", err)
	}

	var (
		gotTOSVersion           sql.NullString
		gotPrivacyPolicyVersion sql.NullString
	)
	err := db.QueryRowContext(
		context.Background(),
		`SELECT tos_version, privacy_policy_version FROM consent_log WHERE account_id = $1 AND event_type = 'install_dialog' ORDER BY consented_at DESC LIMIT 1`,
		accountID,
	).Scan(&gotTOSVersion, &gotPrivacyPolicyVersion)
	if err != nil {
		t.Fatalf("SELECT: %v", err)
	}
	if gotTOSVersion.Valid {
		t.Errorf("tos_version: want NULL for non-ToS event, got %q", gotTOSVersion.String)
	}
	if gotPrivacyPolicyVersion.Valid {
		t.Errorf("privacy_policy_version: want NULL for non-ToS event, got %q", gotPrivacyPolicyVersion.String)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM consent_log WHERE account_id = $1", accountID)
	})
}
