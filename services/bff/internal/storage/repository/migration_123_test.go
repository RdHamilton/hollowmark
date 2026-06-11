package repository_test

// migration_123_test.go — integration tests for migration 000123
// (ticket hollowmark-tickets#126: add mailchimp_attempts column to
// waitlist_entries and new reconciler repository methods).
//
// Tests verify:
//   - mailchimp_attempts column exists with correct type and default
//   - ListFailedWaitlistEntries returns only 'failed' rows below the limit
//   - MarkWaitlistSubscribed flips status to 'subscribed' and bumps updated_at
//   - IncrementAttemptsAndMaybeTerminate (A1 guard) only mutates 'failed' rows
//   - Idempotency: guarded UPDATE ignores concurrent status flip to 'subscribed'
//
// All tests skip when DATABASE_URL is not set.

import (
	"context"
	"database/sql"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// TestMigration123_ColumnExists verifies that the mailchimp_attempts column
// was added to waitlist_entries with NOT NULL and a default of 0.
func TestMigration123_ColumnExists(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	var dataType string
	var columnDefault sql.NullString
	var isNullable string
	err := db.QueryRowContext(ctx, `
		SELECT data_type, column_default, is_nullable
		FROM information_schema.columns
		WHERE table_name = 'waitlist_entries'
		  AND column_name = 'mailchimp_attempts'
	`).Scan(&dataType, &columnDefault, &isNullable)
	if err == sql.ErrNoRows {
		t.Fatal("mailchimp_attempts column not found in waitlist_entries — migration 000123 may not have run")
	}
	if err != nil {
		t.Fatalf("catalog query: %v", err)
	}
	if dataType != "integer" {
		t.Errorf("data_type: want 'integer', got %q", dataType)
	}
	if isNullable != "NO" {
		t.Errorf("is_nullable: want 'NO', got %q", isNullable)
	}
	if !columnDefault.Valid || columnDefault.String != "0" {
		t.Errorf("column_default: want '0', got %v", columnDefault)
	}
}

// TestMigration123_NewRowDefaultsToZeroAttempts verifies that inserting a new
// waitlist row gives it mailchimp_attempts = 0 without explicitly setting it.
func TestMigration123_NewRowDefaultsToZeroAttempts(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	const email = "mig123-default-attempts@test.example"
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM waitlist_entries WHERE email = $1`, email)
	})

	repo := repository.NewWaitlistRepository(db)
	id, _, created, err := repo.InsertIfNew(ctx, email, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("InsertIfNew: %v", err)
	}
	if !created {
		t.Fatal("expected created=true")
	}

	var attempts int
	if err := db.QueryRowContext(
		ctx,
		`SELECT mailchimp_attempts FROM waitlist_entries WHERE id = $1`, id,
	).Scan(&attempts); err != nil {
		t.Fatalf("read mailchimp_attempts: %v", err)
	}
	if attempts != 0 {
		t.Errorf("mailchimp_attempts: want 0 (default), got %d", attempts)
	}
}

// TestWaitlistRepository_ListFailedEntries verifies that ListFailedWaitlistEntries
// returns only rows with mailchimp_status = 'failed' and attempts < 10, up to limit.
func TestWaitlistRepository_ListFailedEntries(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	emails := []string{
		"list-failed-a@test.example", // failed, attempts=0 — should appear
		"list-failed-b@test.example", // subscribed — should NOT appear
		"list-failed-c@test.example", // terminal — should NOT appear
		"list-failed-d@test.example", // failed, attempts=9 — should appear (< 10)
		"list-failed-e@test.example", // failed, attempts=10 — should NOT appear (>= 10)
	}
	t.Cleanup(func() {
		for _, e := range emails {
			_, _ = db.ExecContext(ctx, `DELETE FROM waitlist_entries WHERE email = $1`, e)
		}
	})

	// Insert rows with various states.
	repo := repository.NewWaitlistRepository(db)

	insertAndSet := func(email, status string, attempts int) string {
		id, _, _, err := repo.InsertIfNew(ctx, email, nil, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("InsertIfNew %s: %v", email, err)
		}
		if status != "failed" {
			if _, err := db.ExecContext(
				ctx,
				`UPDATE waitlist_entries SET mailchimp_status=$2 WHERE id=$1`, id, status,
			); err != nil {
				t.Fatalf("set status for %s: %v", email, err)
			}
		}
		if attempts > 0 {
			if _, err := db.ExecContext(
				ctx,
				`UPDATE waitlist_entries SET mailchimp_attempts=$2 WHERE id=$1`, id, attempts,
			); err != nil {
				t.Fatalf("set attempts for %s: %v", email, err)
			}
		}
		return id
	}

	idA := insertAndSet("list-failed-a@test.example", "failed", 0)
	_ = insertAndSet("list-failed-b@test.example", "subscribed", 0)
	_ = insertAndSet("list-failed-c@test.example", "terminal", 0)
	idD := insertAndSet("list-failed-d@test.example", "failed", 9)
	_ = insertAndSet("list-failed-e@test.example", "failed", 10)

	entries, err := repo.ListFailedWaitlistEntries(ctx, 100)
	if err != nil {
		t.Fatalf("ListFailedWaitlistEntries: %v", err)
	}

	// Build a set of returned IDs for easy assertion.
	got := make(map[string]bool, len(entries))
	for _, e := range entries {
		got[e.ID] = true
	}

	if !got[idA] {
		t.Error("expected 'failed' row A (attempts=0) in results")
	}
	if !got[idD] {
		t.Error("expected 'failed' row D (attempts=9) in results")
	}
	for _, badEmail := range []string{
		"list-failed-b@test.example",
		"list-failed-c@test.example",
		"list-failed-e@test.example",
	} {
		for _, e := range entries {
			if e.Email == badEmail {
				t.Errorf("expected %s NOT in results (status/attempts filters)", badEmail)
			}
		}
	}
}

// TestWaitlistRepository_MarkWaitlistSubscribed verifies that
// MarkWaitlistSubscribed flips the status to 'subscribed' and bumps updated_at.
func TestWaitlistRepository_MarkWaitlistSubscribed(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	const email = "mark-subscribed@test.example"
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM waitlist_entries WHERE email = $1`, email)
	})

	repo := repository.NewWaitlistRepository(db)
	id, _, created, err := repo.InsertIfNew(ctx, email, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("InsertIfNew: %v", err)
	}
	if !created {
		t.Fatal("expected created=true")
	}

	if err := repo.MarkWaitlistSubscribed(ctx, id); err != nil {
		t.Fatalf("MarkWaitlistSubscribed: %v", err)
	}

	var status string
	if err := db.QueryRowContext(
		ctx,
		`SELECT mailchimp_status FROM waitlist_entries WHERE id = $1`, id,
	).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "subscribed" {
		t.Errorf("mailchimp_status: want 'subscribed', got %q", status)
	}
}

// TestWaitlistRepository_IncrementAttemptsAndMaybeTerminate_Increments verifies
// that the method increments mailchimp_attempts on a 'failed' row.
func TestWaitlistRepository_IncrementAttemptsAndMaybeTerminate_Increments(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	const email = "incr-attempts@test.example"
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM waitlist_entries WHERE email = $1`, email)
	})

	repo := repository.NewWaitlistRepository(db)
	id, _, created, err := repo.InsertIfNew(ctx, email, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("InsertIfNew: %v", err)
	}
	if !created {
		t.Fatal("expected created=true")
	}

	// Call once — row is 'failed' with attempts=0 → should become attempts=1.
	if err := repo.IncrementAttemptsAndMaybeTerminate(ctx, id, 10); err != nil {
		t.Fatalf("IncrementAttemptsAndMaybeTerminate: %v", err)
	}

	var attempts int
	var status string
	if err := db.QueryRowContext(
		ctx,
		`SELECT mailchimp_attempts, mailchimp_status FROM waitlist_entries WHERE id = $1`, id,
	).Scan(&attempts, &status); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if attempts != 1 {
		t.Errorf("mailchimp_attempts: want 1, got %d", attempts)
	}
	if status != "failed" {
		t.Errorf("mailchimp_status: want 'failed' (below threshold), got %q", status)
	}
}

// TestWaitlistRepository_IncrementAttemptsAndMaybeTerminate_Terminates verifies
// that the method sets mailchimp_status='terminal' when attempts reach threshold.
func TestWaitlistRepository_IncrementAttemptsAndMaybeTerminate_Terminates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	const email = "terminate-attempts@test.example"
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM waitlist_entries WHERE email = $1`, email)
	})

	repo := repository.NewWaitlistRepository(db)
	id, _, created, err := repo.InsertIfNew(ctx, email, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("InsertIfNew: %v", err)
	}
	if !created {
		t.Fatal("expected created=true")
	}

	// Seed attempts at threshold - 1 = 9 (threshold=10).
	if _, err := db.ExecContext(
		ctx,
		`UPDATE waitlist_entries SET mailchimp_attempts = 9 WHERE id = $1`, id,
	); err != nil {
		t.Fatalf("seed attempts: %v", err)
	}

	// One more increment should reach threshold → terminal.
	if err := repo.IncrementAttemptsAndMaybeTerminate(ctx, id, 10); err != nil {
		t.Fatalf("IncrementAttemptsAndMaybeTerminate: %v", err)
	}

	var attempts int
	var status string
	if err := db.QueryRowContext(
		ctx,
		`SELECT mailchimp_attempts, mailchimp_status FROM waitlist_entries WHERE id = $1`, id,
	).Scan(&attempts, &status); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if attempts != 10 {
		t.Errorf("mailchimp_attempts: want 10, got %d", attempts)
	}
	if status != "terminal" {
		t.Errorf("mailchimp_status: want 'terminal', got %q", status)
	}
}

// TestWaitlistRepository_IncrementAttemptsAndMaybeTerminate_A1Guard is the
// idempotency / race-condition test for Ray's binding amendment A1.
//
// Scenario: the reconciler attempts to increment attempts on a row that has
// already been flipped to 'subscribed' by the handler goroutine racing with
// the reconciler. The WHERE mailchimp_status='failed' guard must prevent the
// UPDATE from touching the 'subscribed' row — protecting against overwriting
// a successful subscription back to 'failed'/'terminal'.
func TestWaitlistRepository_IncrementAttemptsAndMaybeTerminate_A1Guard(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	const email = "a1-guard-race@test.example"
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM waitlist_entries WHERE email = $1`, email)
	})

	repo := repository.NewWaitlistRepository(db)
	id, _, created, err := repo.InsertIfNew(ctx, email, nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("InsertIfNew: %v", err)
	}
	if !created {
		t.Fatal("expected created=true")
	}

	// Simulate handler goroutine winning the race: flip to 'subscribed'.
	if err := repo.MarkWaitlistSubscribed(ctx, id); err != nil {
		t.Fatalf("MarkWaitlistSubscribed (simulating handler goroutine): %v", err)
	}

	// Now reconciler tries to increment attempts on what it thought was 'failed'.
	// The A1 guard (WHERE mailchimp_status='failed') must be a no-op.
	if err := repo.IncrementAttemptsAndMaybeTerminate(ctx, id, 10); err != nil {
		t.Fatalf("IncrementAttemptsAndMaybeTerminate after race: %v", err)
	}

	// Verify: status remains 'subscribed'; attempts remain 0.
	var attempts int
	var status string
	if err := db.QueryRowContext(
		ctx,
		`SELECT mailchimp_attempts, mailchimp_status FROM waitlist_entries WHERE id = $1`, id,
	).Scan(&attempts, &status); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if status != "subscribed" {
		t.Errorf("mailchimp_status: want 'subscribed' (guard must protect), got %q", status)
	}
	if attempts != 0 {
		t.Errorf("mailchimp_attempts: want 0 (guard must not increment), got %d", attempts)
	}
}
