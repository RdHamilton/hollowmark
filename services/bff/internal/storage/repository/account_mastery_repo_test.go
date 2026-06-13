package repository_test

// account_mastery_repo_test.go — TDD integration tests for #1338: UpsertMastery
// must write mastery_level, mastery_pass, mastery_max to the accounts table
// scoped by account_id; ON CONFLICT is idempotent.

import (
	"context"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// TestUpsertMastery_WritesColumns verifies that UpsertMastery sets
// mastery_level, mastery_pass, and mastery_max on an existing accounts row.
func TestUpsertMastery_WritesColumns(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountMasteryRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "mastery-write")

	err := repo.UpsertMastery(ctx, repository.MasteryUpsert{
		AccountID:    accountID,
		MasteryLevel: 42,
		MasteryPass:  "Standard",
		MasteryMax:   80,
		UpdatedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertMastery: %v", err)
	}

	var level int
	var pass string
	var max int
	err = db.QueryRowContext(ctx,
		`SELECT mastery_level, mastery_pass, mastery_max FROM accounts WHERE id = $1`,
		accountID).Scan(&level, &pass, &max)
	if err != nil {
		t.Fatalf("SELECT after UpsertMastery: %v", err)
	}
	if level != 42 {
		t.Errorf("mastery_level: got %d, want 42", level)
	}
	if pass != "Standard" {
		t.Errorf("mastery_pass: got %q, want %q", pass, "Standard")
	}
	if max != 80 {
		t.Errorf("mastery_max: got %d, want 80", max)
	}
}

// TestUpsertMastery_Idempotent verifies that calling UpsertMastery twice with
// the same account_id updates the row rather than erroring (ON CONFLICT).
func TestUpsertMastery_Idempotent(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountMasteryRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "mastery-idempotent")
	now := time.Now().UTC()

	// First upsert — level 10.
	err := repo.UpsertMastery(ctx, repository.MasteryUpsert{
		AccountID:    accountID,
		MasteryLevel: 10,
		MasteryPass:  "Basic",
		MasteryMax:   80,
		UpdatedAt:    now,
	})
	if err != nil {
		t.Fatalf("first UpsertMastery: %v", err)
	}

	// Second upsert — level 20, should overwrite.
	err = repo.UpsertMastery(ctx, repository.MasteryUpsert{
		AccountID:    accountID,
		MasteryLevel: 20,
		MasteryPass:  "Standard",
		MasteryMax:   80,
		UpdatedAt:    now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("second UpsertMastery: %v", err)
	}

	var level int
	var pass string
	err = db.QueryRowContext(ctx,
		`SELECT mastery_level, mastery_pass FROM accounts WHERE id = $1`,
		accountID).Scan(&level, &pass)
	if err != nil {
		t.Fatalf("SELECT after second UpsertMastery: %v", err)
	}
	if level != 20 {
		t.Errorf("mastery_level after second upsert: got %d, want 20", level)
	}
	if pass != "Standard" {
		t.Errorf("mastery_pass after second upsert: got %q, want Standard", pass)
	}
}

// TestUpsertMastery_ScopedToAccount verifies that UpsertMastery only modifies
// the row for the specified account_id and leaves other accounts unchanged.
func TestUpsertMastery_ScopedToAccount(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountMasteryRepository(db)
	ctx := context.Background()

	accountA := insertTestAccount(t, db, "mastery-scoped-a")
	accountB := insertTestAccount(t, db, "mastery-scoped-b")

	// Upsert only for accountA.
	err := repo.UpsertMastery(ctx, repository.MasteryUpsert{
		AccountID:    accountA,
		MasteryLevel: 55,
		MasteryPass:  "Premium",
		MasteryMax:   80,
		UpdatedAt:    time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertMastery for accountA: %v", err)
	}

	// accountB must still have default values (0 / "Basic" per migration 000013).
	var levelB int
	var passB string
	err = db.QueryRowContext(ctx,
		`SELECT mastery_level, mastery_pass FROM accounts WHERE id = $1`,
		accountB).Scan(&levelB, &passB)
	if err != nil {
		t.Fatalf("SELECT accountB: %v", err)
	}
	if levelB != 0 {
		t.Errorf("accountB mastery_level: got %d, want 0 (must not be touched)", levelB)
	}
	if passB != "Basic" {
		t.Errorf("accountB mastery_pass: got %q, want Basic (default — must not be touched)", passB)
	}
}
