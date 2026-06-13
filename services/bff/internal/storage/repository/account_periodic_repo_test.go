package repository_test

// account_periodic_repo_test.go — TDD integration tests for #1344: UpsertPeriodicWins
// must write daily_wins and weekly_wins to the accounts table scoped by account_id.
// These columns already exist (migration 000012) but nothing in the BFF has ever
// written them — this is the fix for the Quests page showing stale/zero win counts.

import (
	"context"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// TestUpsertPeriodicWins_WritesColumns verifies that UpsertPeriodicWins sets
// daily_wins and weekly_wins on an existing accounts row.
func TestUpsertPeriodicWins_WritesColumns(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountPeriodicRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "periodic-write")

	err := repo.UpsertPeriodicWins(ctx, repository.PeriodicWinsUpsert{
		AccountID:  accountID,
		DailyWins:  4,
		WeeklyWins: 7,
		UpdatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertPeriodicWins: %v", err)
	}

	var daily, weekly int
	err = db.QueryRowContext(ctx,
		`SELECT daily_wins, weekly_wins FROM accounts WHERE id = $1`,
		accountID).Scan(&daily, &weekly)
	if err != nil {
		t.Fatalf("SELECT after UpsertPeriodicWins: %v", err)
	}
	if daily != 4 {
		t.Errorf("daily_wins: got %d, want 4", daily)
	}
	if weekly != 7 {
		t.Errorf("weekly_wins: got %d, want 7", weekly)
	}
}

// TestUpsertPeriodicWins_ZeroValues verifies that daily_wins=0 / weekly_wins=0
// are preserved (a period reset should write 0, not be treated as absent).
func TestUpsertPeriodicWins_ZeroValues(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountPeriodicRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "periodic-zero")

	// Pre-populate with non-zero values.
	err := repo.UpsertPeriodicWins(ctx, repository.PeriodicWinsUpsert{
		AccountID:  accountID,
		DailyWins:  5,
		WeeklyWins: 10,
		UpdatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("first UpsertPeriodicWins: %v", err)
	}

	// Reset to zero (new daily/weekly period starts).
	err = repo.UpsertPeriodicWins(ctx, repository.PeriodicWinsUpsert{
		AccountID:  accountID,
		DailyWins:  0,
		WeeklyWins: 0,
		UpdatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("second UpsertPeriodicWins (zero reset): %v", err)
	}

	var daily, weekly int
	err = db.QueryRowContext(ctx,
		`SELECT daily_wins, weekly_wins FROM accounts WHERE id = $1`,
		accountID).Scan(&daily, &weekly)
	if err != nil {
		t.Fatalf("SELECT after zero reset: %v", err)
	}
	if daily != 0 {
		t.Errorf("daily_wins after reset: got %d, want 0", daily)
	}
	if weekly != 0 {
		t.Errorf("weekly_wins after reset: got %d, want 0", weekly)
	}
}

// TestUpsertPeriodicWins_Idempotent verifies that calling UpsertPeriodicWins
// twice with the same account_id overwrites rather than errors.
func TestUpsertPeriodicWins_Idempotent(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountPeriodicRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "periodic-idempotent")
	now := time.Now().UTC()

	err := repo.UpsertPeriodicWins(ctx, repository.PeriodicWinsUpsert{
		AccountID:  accountID,
		DailyWins:  1,
		WeeklyWins: 3,
		UpdatedAt:  now,
	})
	if err != nil {
		t.Fatalf("first UpsertPeriodicWins: %v", err)
	}

	err = repo.UpsertPeriodicWins(ctx, repository.PeriodicWinsUpsert{
		AccountID:  accountID,
		DailyWins:  4,
		WeeklyWins: 7,
		UpdatedAt:  now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("second UpsertPeriodicWins: %v", err)
	}

	var daily, weekly int
	err = db.QueryRowContext(ctx,
		`SELECT daily_wins, weekly_wins FROM accounts WHERE id = $1`,
		accountID).Scan(&daily, &weekly)
	if err != nil {
		t.Fatalf("SELECT after second upsert: %v", err)
	}
	if daily != 4 {
		t.Errorf("daily_wins after second upsert: got %d, want 4", daily)
	}
	if weekly != 7 {
		t.Errorf("weekly_wins after second upsert: got %d, want 7", weekly)
	}
}

// TestGetPeriodicWins_ReturnsWrittenValues verifies that GetPeriodicWins reads
// back the exact values written by UpsertPeriodicWins.
func TestGetPeriodicWins_ReturnsWrittenValues(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountPeriodicRepository(db)
	accountRepo := repository.NewAccountRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "periodic-read")

	if err := repo.UpsertPeriodicWins(ctx, repository.PeriodicWinsUpsert{
		AccountID:  accountID,
		DailyWins:  4,
		WeeklyWins: 7,
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("UpsertPeriodicWins: %v", err)
	}

	daily, weekly, err := accountRepo.GetPeriodicWins(ctx, accountID)
	if err != nil {
		t.Fatalf("GetPeriodicWins: %v", err)
	}
	if daily != 4 {
		t.Errorf("GetPeriodicWins daily: got %d, want 4", daily)
	}
	if weekly != 7 {
		t.Errorf("GetPeriodicWins weekly: got %d, want 7", weekly)
	}
}

// TestGetPeriodicWins_DefaultsToZero verifies that a fresh account row returns
// daily_wins=0 / weekly_wins=0 before any periodic.updated event is projected.
func TestGetPeriodicWins_DefaultsToZero(t *testing.T) {
	db := openTestDB(t)
	accountRepo := repository.NewAccountRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "periodic-read-zero")

	daily, weekly, err := accountRepo.GetPeriodicWins(ctx, accountID)
	if err != nil {
		t.Fatalf("GetPeriodicWins on fresh account: %v", err)
	}
	if daily != 0 || weekly != 0 {
		t.Errorf("fresh account: got daily=%d weekly=%d, want 0/0", daily, weekly)
	}
}

// TestUpsertPeriodicWins_ScopedToAccount verifies that UpsertPeriodicWins only
// modifies the row for the specified account_id and leaves other accounts unchanged.
func TestUpsertPeriodicWins_ScopedToAccount(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountPeriodicRepository(db)
	ctx := context.Background()

	accountA := insertTestAccount(t, db, "periodic-scoped-a")
	accountB := insertTestAccount(t, db, "periodic-scoped-b")

	err := repo.UpsertPeriodicWins(ctx, repository.PeriodicWinsUpsert{
		AccountID:  accountA,
		DailyWins:  4,
		WeeklyWins: 7,
		UpdatedAt:  time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertPeriodicWins for accountA: %v", err)
	}

	// accountB must still have default zero values.
	var dailyB, weeklyB int
	err = db.QueryRowContext(ctx,
		`SELECT daily_wins, weekly_wins FROM accounts WHERE id = $1`,
		accountB).Scan(&dailyB, &weeklyB)
	if err != nil {
		t.Fatalf("SELECT accountB: %v", err)
	}
	if dailyB != 0 {
		t.Errorf("accountB daily_wins: got %d, want 0 (must not be touched)", dailyB)
	}
	if weeklyB != 0 {
		t.Errorf("accountB weekly_wins: got %d, want 0 (must not be touched)", weeklyB)
	}
}
