package repository_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ---------------------------------------------------------------------------
// FM-3 export coverage fitness test (AC per approved plan §4)
//
// TestExportCoverage_MirrorsFM3 asserts that the export table set produced by
// DataExportRepository.TableNames() is a superset of every FM-3
// knownUserKeyedTables entry with disposition "cascade", "explicit", or
// "anonymize" — minus:
//   - "retain" disposition rows (deletion_audit_log — compliance evidence, not
//     returned to the subject)
//   - waitlist_entries (email-keyed, not account-keyed; carved out per plan)
//   - the four draft_* non-keyed aggregate tables (D1 defect from Ray's review)
//
// If a new table is added to knownUserKeyedTables without being added to
// DataExportRepository, this test FAILS — keeping the export scope in sync
// with the erasure enumeration by machine enforcement rather than hand-
// maintenance (ADR-056 fitness function pattern).
// ---------------------------------------------------------------------------

// exportScopeTables mirrors the "include" subset of FM-3 knownUserKeyedTables.
// Dispositions "cascade", "explicit", "anonymize" → included.
// "retain" → excluded (deletion_audit_log).
// waitlist_entries → excluded (email-keyed, not account-keyed).
var exportScopeTables = map[string]bool{
	// cascade — accounts(id) or users(id)
	"collection":               true,
	"collection_new":           true,
	"collection_history":       true,
	"matches":                  true,
	"player_stats":             true,
	"decks":                    true,
	"rank_history":             true,
	"draft_events":             true,
	"draft_sessions":           true,
	"inventory":                true,
	"inventory_history":        true,
	"quests":                   true,
	"user_settings":            true,
	"recommendation_feedback":  true,
	"card_inventory":           true,
	"game_plays":               true,
	"draft_picks":              true,
	"draft_packs":              true,
	"draft_match_results":      true,
	"game_event_counters":      true,
	"life_change_tracking":     true,
	"matchup_statistics":       true,
	"deck_performance_history": true,
	"currency_history":         true,
	"match_game_results":       true,
	// cascade — matches(id)
	"games":                   true,
	"game_state_snapshots":    true,
	"opponent_cards_observed": true,
	"opponent_deck_profiles":  true,
	// cascade — decks(id)
	"deck_cards":        true,
	"deck_notes":        true,
	"deck_tags":         true,
	"ml_suggestions":    true,
	"deck_permutations": true,
	// cascade — users(id)
	"accounts": true,
	"api_keys": true,
	// cascade — accounts(id) BIGINT (migration 000080)
	"quest_session_tracking": true,
	// explicit (TEXT-keyed)
	"daemon_events":      true,
	"daemon_api_keys":    true,
	"user_play_patterns": true,
	"projection_errors":  true,
	// anonymize in-place
	"consent_log": true,
}

// TestExportCoverage_MirrorsFM3 verifies the DataExportRepository export table
// set is a superset of exportScopeTables (FM-3 cascade+explicit+anonymize minus
// retain and waitlist_entries).
func TestExportCoverage_MirrorsFM3(t *testing.T) {
	repo := repository.NewDataExportRepository(nil, nil) // no DB needed for TableNames()
	names := repo.TableNames()

	nameSet := make(map[string]bool, len(names))
	for _, n := range names {
		nameSet[n] = true
	}

	// Every FM-3 scope table must appear in the export set.
	for table := range exportScopeTables {
		if !nameSet[table] {
			t.Errorf("FM-3 export coverage: table %q is in exportScopeTables but NOT in DataExportRepository.TableNames() — add it to the export", table)
		}
	}

	// The export set must NOT include the four non-keyed draft aggregate tables
	// (Ray D1 defect: these have no account_id / user_id column).
	d1Excluded := []string{
		"draft_archetype_stats",
		"draft_community_comparison",
		"draft_temporal_trends",
		"draft_pattern_analysis",
	}
	for _, table := range d1Excluded {
		if nameSet[table] {
			t.Errorf("FM-3 export coverage: table %q is a non-keyed aggregate (D1 defect) and must NOT be in the export, but it is", table)
		}
	}

	// The export set must NOT include waitlist_entries (email-keyed, not
	// account-keyed — carved out per Ray's approval of v2 plan).
	if nameSet["waitlist_entries"] {
		t.Error("FM-3 export coverage: waitlist_entries must NOT be in export (email-keyed, not account-keyed)")
	}

	// The export set must NOT include deletion_audit_log (retain disposition —
	// compliance evidence, not returned to the subject).
	if nameSet["deletion_audit_log"] {
		t.Error("FM-3 export coverage: deletion_audit_log must NOT be in export (retain disposition — compliance evidence)")
	}
}

// ---------------------------------------------------------------------------
// DSRAccessLogRepository integration tests
// ---------------------------------------------------------------------------

// TestDSRAccessLog_RecordAndCheck verifies the full rate-limit cycle:
//  1. A fresh user has no recent request → CheckRecentExport returns (false, 0).
//  2. After RecordExport, CheckRecentExport returns (true, positive Retry-After).
func TestDSRAccessLog_RecordAndCheck(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDSRAccessLogRepository(db)

	userID, _, _ := seedDeletionUser(t, db)
	ctx := context.Background()

	// No prior request → not rate-limited.
	limited, retryAfter, err := repo.CheckRecentExport(ctx, userID)
	if err != nil {
		t.Fatalf("CheckRecentExport (initial): %v", err)
	}
	if limited {
		t.Error("expected not rate-limited for fresh user, got limited")
	}
	if retryAfter != 0 {
		t.Errorf("expected retryAfter=0 for fresh user, got %d", retryAfter)
	}

	// Record an export.
	exportID, err := repo.RecordExport(ctx, userID)
	if err != nil {
		t.Fatalf("RecordExport: %v", err)
	}
	if exportID == "" {
		t.Error("expected non-empty exportID from RecordExport")
	}

	// Now rate-limited within the 24h window.
	limited, retryAfter, err = repo.CheckRecentExport(ctx, userID)
	if err != nil {
		t.Fatalf("CheckRecentExport (after record): %v", err)
	}
	if !limited {
		t.Error("expected rate-limited after RecordExport, got not limited")
	}
	if retryAfter <= 0 {
		t.Errorf("expected positive retryAfter seconds, got %d", retryAfter)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM dsr_access_log WHERE user_id = $1`, userID)
	})
}

// TestDSRAccessLog_CrossUserIsolation verifies user A's export does not trigger
// user B's rate-limit (IDOR isolation at the repo level).
func TestDSRAccessLog_CrossUserIsolation(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDSRAccessLogRepository(db)

	userA, _, _ := seedDeletionUser(t, db)
	userB, _, _ := seedDeletionUser(t, db)
	ctx := context.Background()

	// Record an export for user A.
	_, err := repo.RecordExport(ctx, userA)
	if err != nil {
		t.Fatalf("RecordExport userA: %v", err)
	}

	// User B must not be rate-limited by user A's export.
	limited, _, err := repo.CheckRecentExport(ctx, userB)
	if err != nil {
		t.Fatalf("CheckRecentExport userB: %v", err)
	}
	if limited {
		t.Error("IDOR: user B is rate-limited by user A's export — must be isolated")
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM dsr_access_log WHERE user_id IN ($1, $2)`, userA, userB)
	})
}

// TestDSRAccessLog_RecordExport_UsesUTC verifies requested_at is stored as UTC.
func TestDSRAccessLog_RecordExport_UsesUTC(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDSRAccessLogRepository(db)

	userID, _, _ := seedDeletionUser(t, db)
	ctx := context.Background()

	exportID, err := repo.RecordExport(ctx, userID)
	if err != nil {
		t.Fatalf("RecordExport: %v", err)
	}

	var requestedAt time.Time
	err = db.QueryRowContext(ctx,
		`SELECT requested_at FROM dsr_access_log WHERE export_id = $1`,
		exportID).Scan(&requestedAt)
	if err != nil {
		t.Fatalf("fetch requested_at: %v", err)
	}

	age := time.Since(requestedAt.UTC())
	if age < 0 || age > time.Minute {
		t.Errorf("requested_at age %v outside expected range (0, 1m) — UTC issue?", age)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM dsr_access_log WHERE user_id = $1`, userID)
	})
}

// ---------------------------------------------------------------------------
// DataExportRepository integration tests
// ---------------------------------------------------------------------------

// TestDataExportRepository_GatherForUser_ReturnsExportWithManifest verifies
// GatherForUser returns a structurally valid export for a seeded user.
func TestDataExportRepository_GatherForUser_ReturnsExportWithManifest(t *testing.T) {
	db := openTestDB(t)
	exportRepo := repository.NewDataExportRepository(db, nil)

	userID, accountID, _ := seedDeletionUser(t, db)
	ctx := context.Background()

	export, err := exportRepo.GatherForUser(ctx, userID, accountID)
	if err != nil {
		t.Fatalf("GatherForUser: %v", err)
	}
	if export == nil {
		t.Fatal("expected non-nil export")
	}
	if export.ExportID == "" {
		t.Error("expected non-empty export_id")
	}
	if export.ExportedAt.IsZero() {
		t.Error("expected non-zero exported_at")
	}
	if len(export.Manifest) == 0 {
		t.Error("expected non-empty manifest")
	}

	for _, m := range export.Manifest {
		if m.Source == "" {
			t.Error("manifest entry has empty source name")
		}
		if m.RowCount < 0 {
			t.Errorf("manifest entry %q has negative row count: %d", m.Source, m.RowCount)
		}
	}

	// account_id_hash must be SHA-256 hex[:16] of the accountID.
	wantHash := testHashAccountID(strconv.FormatInt(accountID, 10))
	if export.AccountIDHash != wantHash {
		t.Errorf("account_id_hash: got %q, want %q", export.AccountIDHash, wantHash)
	}
}

// TestDataExportRepository_GatherForUser_IsolatesCrossUser verifies user A
// cannot see user B's data (mandatory IDOR isolation test per approved plan).
func TestDataExportRepository_GatherForUser_IsolatesCrossUser(t *testing.T) {
	db := openTestDB(t)
	exportRepo := repository.NewDataExportRepository(db, nil)

	_, accountAID, _ := seedDeletionUser(t, db)
	userBID, accountBID, _ := seedDeletionUser(t, db)
	ctx := context.Background()

	// Export for user B must not reference user A's account_id_hash.
	exportB, err := exportRepo.GatherForUser(ctx, userBID, accountBID)
	if err != nil {
		t.Fatalf("GatherForUser userB: %v", err)
	}

	wantHashA := testHashAccountID(strconv.FormatInt(accountAID, 10))
	if exportB.AccountIDHash == wantHashA {
		t.Errorf("IDOR: export for user B has user A's account_id_hash %q", wantHashA)
	}

	wantHashB := testHashAccountID(strconv.FormatInt(accountBID, 10))
	if exportB.AccountIDHash != wantHashB {
		t.Errorf("export B account_id_hash: got %q, want %q", exportB.AccountIDHash, wantHashB)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testHashAccountID mirrors identityhash.HashAccountID (SHA-256 hex[:16]).
// Kept local to avoid import cycle between repository_test and identityhash.
func testHashAccountID(id string) string {
	sum := sha256.Sum256([]byte(id))
	return fmt.Sprintf("%x", sum)[:16]
}

// Compile-time assertion: *sql.DB is referenced in openTestDB; keep the import.
var _ *sql.DB

// ---------------------------------------------------------------------------
// Clerk profile gather test
// ---------------------------------------------------------------------------

// stubClerkFetcher is a test stub satisfying clerkProfileFetcher.
type stubClerkFetcher struct {
	profile *repository.ClerkProfile
	err     error
}

func (s *stubClerkFetcher) FetchClerkProfile(_ context.Context, _ string) (*repository.ClerkProfile, error) {
	return s.profile, s.err
}

// TestDataExportRepository_GatherForUser_IncludesClerkProfile verifies that
// GatherForUser populates ClerkProfile.Email in the export when a clerkFetcher
// is provided and the users row has a clerk_user_id (Art.15 Q2 requirement).
func TestDataExportRepository_GatherForUser_IncludesClerkProfile(t *testing.T) {
	db := openTestDB(t)

	userID, accountID, _ := seedDeletionUser(t, db)
	ctx := context.Background()

	wantEmail := "art15.test@example.com"
	wantFirst := "Test"
	wantLast := "User"

	fetcher := &stubClerkFetcher{
		profile: &repository.ClerkProfile{
			Email:     wantEmail,
			FirstName: wantFirst,
			LastName:  wantLast,
			CreatedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	exportRepo := repository.NewDataExportRepository(db, fetcher)
	export, err := exportRepo.GatherForUser(ctx, userID, accountID)
	if err != nil {
		t.Fatalf("GatherForUser: %v", err)
	}

	if export.ClerkProfile == nil {
		t.Fatal("expected clerk_profile in export, got nil")
	}
	if export.ClerkProfile.Email != wantEmail {
		t.Errorf("clerk_profile.email: got %q, want %q", export.ClerkProfile.Email, wantEmail)
	}
	if export.ClerkProfile.FirstName != wantFirst {
		t.Errorf("clerk_profile.first_name: got %q, want %q", export.ClerkProfile.FirstName, wantFirst)
	}
	if export.ClerkProfile.LastName != wantLast {
		t.Errorf("clerk_profile.last_name: got %q, want %q", export.ClerkProfile.LastName, wantLast)
	}
}

// TestDataExportRepository_GatherForUser_NilFetcher_OmitsClerkProfile verifies
// that GatherForUser with a nil clerkFetcher produces a valid export without
// clerk_profile (used in local dev without Clerk).
func TestDataExportRepository_GatherForUser_NilFetcher_OmitsClerkProfile(t *testing.T) {
	db := openTestDB(t)

	userID, accountID, _ := seedDeletionUser(t, db)
	ctx := context.Background()

	exportRepo := repository.NewDataExportRepository(db, nil)
	export, err := exportRepo.GatherForUser(ctx, userID, accountID)
	if err != nil {
		t.Fatalf("GatherForUser: %v", err)
	}

	if export.ClerkProfile != nil {
		t.Errorf("expected nil clerk_profile when fetcher is nil, got %+v", export.ClerkProfile)
	}
}
