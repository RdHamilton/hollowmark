package repository_test

import (
	"context"
	"testing"

	posthog "github.com/posthog/posthog-go"

	"github.com/RdHamilton/hollowmark/services/bff/internal/analytics"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// enqueueRecorder records Enqueue calls for assertion.
type enqueueRecorder struct {
	calls []posthog.Capture
}

func (r *enqueueRecorder) Enqueue(msg posthog.Message) error {
	if c, ok := msg.(posthog.Capture); ok {
		r.calls = append(r.calls, c)
	}
	return nil
}

// ---------------------------------------------------------------------------
// AC4 unit test: duplicate-account WARN path emits analytics event
// ---------------------------------------------------------------------------

// TestAccountRepository_GetOrCreateByClientID_DuplicateWarnEmitsMetric verifies
// that when GetOrCreateByClientID inserts a new accounts row for a user who
// already owns at least one account, an analytics event is emitted with the
// correct event name and no raw PII in the properties.
//
// Two accounts for the same user are seeded via direct SQL (distinct client_ids
// to bypass the UNIQUE constraint). GetOrCreateByClientID is called with a third
// client_id; the INSERT succeeds, the post-insert duplicate count check detects
// count > 1, and the analytics event must fire.
func TestAccountRepository_GetOrCreateByClientID_DuplicateWarnEmitsMetric(t *testing.T) {
	db := openTestDB(t)

	recorder := &enqueueRecorder{}
	ac := analytics.NewClient(recorder, analytics.NewNoopHaltChecker())

	repo := repository.NewAccountRepository(db).WithAnalyticsClient(ac)

	// Seed a user.
	clerkID := "clerk_dupwarn_" + t.Name()
	var userID int64
	if err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO users (email, clerk_user_id) VALUES ($1, $2) RETURNING id`,
		clerkID+"@test.local", clerkID,
	).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	// Seed a pre-existing accounts row for this user (distinct client_id).
	var existingAccountID int64
	if err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO accounts (name, client_id, user_id) VALUES ($1, $2, $3) RETURNING id`,
		"ExistingAccount", "MTGA_existing_"+t.Name(), userID,
	).Scan(&existingAccountID); err != nil {
		t.Fatalf("seed existing account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", existingAccountID)
	})

	// Call GetOrCreateByClientID with a NEW client_id for the same user. The
	// INSERT will succeed, and the post-insert duplicate check should emit a metric.
	newClientID := "MTGA_newdup_" + t.Name()
	newAccountID, err := repo.GetOrCreateByClientID(context.Background(), newClientID, userID)
	if err != nil {
		t.Fatalf("GetOrCreateByClientID: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", newAccountID)
	})

	// The event must have been emitted.
	if len(recorder.calls) == 0 {
		t.Fatal("expected analytics event for duplicate account insert, got none")
	}

	ev := recorder.calls[0]
	if ev.Event != analytics.EventAccountDuplicateDetected {
		t.Errorf("event name: want %q, got %q", analytics.EventAccountDuplicateDetected, ev.Event)
	}

	// PII rule: account_id_hash must be present; raw user_id / account_id must NOT be.
	props := ev.Properties
	if props["account_id_hash"] == nil {
		t.Error("expected account_id_hash in event properties (pseudonymous id required)")
	}
	if props["user_id"] != nil {
		t.Errorf("raw user_id must not appear in analytics properties (PII leak): %v", props["user_id"])
	}
	if props["account_id"] != nil {
		t.Errorf("raw account_id must not appear in analytics properties (PII leak): %v", props["account_id"])
	}

	// duplicate_count must be >= 2.
	cnt, ok := props["duplicate_count"].(int64)
	if !ok {
		t.Errorf("duplicate_count property missing or wrong type: %T(%v)", props["duplicate_count"], props["duplicate_count"])
	} else if cnt < 2 {
		t.Errorf("duplicate_count: want >= 2, got %d", cnt)
	}
}

// TestAccountRepository_GetOrCreateByClientID_EmptyHashNoEvent verifies that
// when accounts.account_id_hash is NULL (user has no clerk_user_id), the
// duplicate-warn path does NOT emit an analytics event even when a duplicate
// accounts row is inserted.  An empty hash would produce misleading PostHog
// events; the daily D7.1 canary is the fallback detection path in this case.
func TestAccountRepository_GetOrCreateByClientID_EmptyHashNoEvent(t *testing.T) {
	db := openTestDB(t)

	recorder := &enqueueRecorder{}
	ac := analytics.NewClient(recorder, analytics.NewNoopHaltChecker())
	repo := repository.NewAccountRepository(db).WithAnalyticsClient(ac)

	// Seed a user with no clerk_user_id — this causes account_id_hash to be NULL
	// at INSERT time (see account_repo.go CASE expression).
	var userID int64
	if err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO users (email) VALUES ($1) RETURNING id`,
		"noclerk_emptyhash_"+t.Name()+"@test.local",
	).Scan(&userID); err != nil {
		t.Fatalf("seed user without clerk_user_id: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	// Seed a pre-existing accounts row for this user.
	var firstID int64
	if err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO accounts (name, client_id, user_id) VALUES ($1, $2, $3) RETURNING id`,
		"NullHashAcct", "MTGA_nohash_first_"+t.Name(), userID,
	).Scan(&firstID); err != nil {
		t.Fatalf("seed first account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", firstID)
	})

	// Insert a duplicate via GetOrCreateByClientID. Because clerk_user_id is NULL,
	// account_id_hash will be NULL, triggering the empty-hash guard.
	secondID, err := repo.GetOrCreateByClientID(context.Background(), "MTGA_nohash_second_"+t.Name(), userID)
	if err != nil {
		t.Fatalf("GetOrCreateByClientID: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", secondID)
	})

	// No analytics event must have been emitted.
	if len(recorder.calls) != 0 {
		t.Errorf("expected no analytics event for empty-hash duplicate, got %d event(s): %v", len(recorder.calls), recorder.calls)
	}
}

// ---------------------------------------------------------------------------
// AC1 integration tests: CheckDuplicateAccounts canary query
// ---------------------------------------------------------------------------

// TestAccountRepository_CheckDuplicateAccounts_DetectsDuplicate verifies that
// CheckDuplicateAccounts returns at least one DuplicateAccountRow for a user
// with more than one accounts row.
func TestAccountRepository_CheckDuplicateAccounts_DetectsDuplicate(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountRepository(db)

	// Seed a user with two accounts.
	clerkID := "clerk_canary_" + t.Name()
	var userID int64
	if err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO users (email, clerk_user_id) VALUES ($1, $2) RETURNING id`,
		clerkID+"@test.local", clerkID,
	).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	for i, clientID := range []string{
		"MTGA_canary_a_" + t.Name(),
		"MTGA_canary_b_" + t.Name(),
	} {
		var id int64
		if err := db.QueryRowContext(
			context.Background(),
			`INSERT INTO accounts (name, client_id, user_id) VALUES ($1, $2, $3) RETURNING id`,
			"CanaryAcct", clientID, userID,
		).Scan(&id); err != nil {
			t.Fatalf("seed account %d: %v", i, err)
		}
		capturedID := id
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", capturedID)
		})
	}

	dups, err := repo.CheckDuplicateAccounts(context.Background())
	if err != nil {
		t.Fatalf("CheckDuplicateAccounts: %v", err)
	}

	found := false
	for _, d := range dups {
		if d.UserID == userID {
			found = true
			if d.Count < 2 {
				t.Errorf("DuplicateAccountRow.Count: want >= 2, got %d", d.Count)
			}
		}
	}
	if !found {
		t.Errorf("CheckDuplicateAccounts: seeded duplicate for user_id %d not detected", userID)
	}
}

// TestAccountRepository_CheckDuplicateAccounts_EmptyWhenClean verifies that
// CheckDuplicateAccounts returns no row for a user with exactly one account.
func TestAccountRepository_CheckDuplicateAccounts_EmptyWhenClean(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountRepository(db)

	clerkID := "clerk_canary_clean_" + t.Name()
	var userID int64
	if err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO users (email, clerk_user_id) VALUES ($1, $2) RETURNING id`,
		clerkID+"@test.local", clerkID,
	).Scan(&userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	var acctID int64
	if err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO accounts (name, client_id, user_id) VALUES ($1, $2, $3) RETURNING id`,
		"CleanAcct", "MTGA_clean_"+t.Name(), userID,
	).Scan(&acctID); err != nil {
		t.Fatalf("seed account: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", acctID)
	})

	dups, err := repo.CheckDuplicateAccounts(context.Background())
	if err != nil {
		t.Fatalf("CheckDuplicateAccounts: %v", err)
	}

	for _, d := range dups {
		if d.UserID == userID {
			t.Errorf("CheckDuplicateAccounts: unexpected row for single-account user %d", userID)
		}
	}
}
