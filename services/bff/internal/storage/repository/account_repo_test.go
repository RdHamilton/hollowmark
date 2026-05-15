package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ramonehamilton/mtga-bff/internal/storage/repository"
)

// ---------------------------------------------------------------------------
// Unit tests (no DB required)
// ---------------------------------------------------------------------------

// TestAccountRepository_Interface verifies NewAccountRepository compiles
// correctly with any DB implementation.
func TestAccountRepository_Interface(t *testing.T) {
	var db repository.DB = &fakeDB{}
	repo := repository.NewAccountRepository(db)

	if repo == nil {
		t.Fatal("NewAccountRepository returned nil")
	}
}

// ---------------------------------------------------------------------------
// Integration tests (require TEST_DATABASE_URL + migration 000082 applied)
// ---------------------------------------------------------------------------

// seedUser inserts a minimal users row and returns its id.  Cleaned up by
// t.Cleanup.
func seedUser(t *testing.T, db interface {
	ExecContext(context.Context, string, ...any) (interface {
		LastInsertId() (int64, error)
		RowsAffected() (int64, error)
	}, error)
	QueryRowContext(context.Context, string, ...any) interface{ Scan(...any) error }
}, clerkID string,
) int64 {
	t.Helper()
	// Use the real *sql.DB — openTestDB always returns *sql.DB which satisfies
	// repository.DB; we just use ExecContext directly here.
	return 0
}

// TestAccountRepository_GetOrCreateByClientID_CreatesNewAccount verifies that
// calling GetOrCreateByClientID for an unknown client_id inserts a new row and
// returns a non-zero account ID.
func TestAccountRepository_GetOrCreateByClientID_CreatesNewAccount(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountRepository(db)

	// Insert a users row so the FK is satisfied.
	clerkID := "clerk_accttest_create_" + t.Name()
	var userID int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO users (email, clerk_user_id) VALUES ($1, $2) RETURNING id`,
		clerkID+"@test.local", clerkID,
	).Scan(&userID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	clientID := "MTGA_" + t.Name()

	accountID, err := repo.GetOrCreateByClientID(context.Background(), clientID, userID)
	if err != nil {
		t.Fatalf("GetOrCreateByClientID: %v", err)
	}
	if accountID == 0 {
		t.Error("expected non-zero accountID")
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", accountID)
	})
}

// TestAccountRepository_GetOrCreateByClientID_IdempotentSameUser verifies that
// calling GetOrCreateByClientID twice with the same client_id and same userID
// returns the same account ID both times (idempotent upsert).
func TestAccountRepository_GetOrCreateByClientID_IdempotentSameUser(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountRepository(db)

	clerkID := "clerk_accttest_idem_" + t.Name()
	var userID int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO users (email, clerk_user_id) VALUES ($1, $2) RETURNING id`,
		clerkID+"@test.local", clerkID,
	).Scan(&userID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	clientID := "MTGA_idem_" + t.Name()

	first, err := repo.GetOrCreateByClientID(context.Background(), clientID, userID)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	second, err := repo.GetOrCreateByClientID(context.Background(), clientID, userID)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if first != second {
		t.Errorf("idempotent: first=%d second=%d (want equal)", first, second)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", first)
	})
}

// TestAccountRepository_GetOrCreateByClientID_CrossTenantRejected verifies
// that attempting to claim a client_id already owned by a different userID
// returns ErrCrosstenantAccount and does NOT insert a new row.
func TestAccountRepository_GetOrCreateByClientID_CrossTenantRejected(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountRepository(db)

	// Create two distinct users.
	var userA, userB int64
	for _, tc := range []struct {
		dst     *int64
		clerkID string
	}{
		{&userA, "clerk_crosstenant_a_" + t.Name()},
		{&userB, "clerk_crosstenant_b_" + t.Name()},
	} {
		err := db.QueryRowContext(
			context.Background(),
			`INSERT INTO users (email, clerk_user_id) VALUES ($1, $2) RETURNING id`,
			tc.clerkID+"@test.local", tc.clerkID,
		).Scan(tc.dst)
		if err != nil {
			t.Fatalf("seed user %s: %v", tc.clerkID, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE id IN ($1, $2)", userA, userB)
	})

	clientID := "MTGA_cross_" + t.Name()

	// User A legitimately registers the client_id.
	accountID, err := repo.GetOrCreateByClientID(context.Background(), clientID, userA)
	if err != nil {
		t.Fatalf("user A GetOrCreate: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM accounts WHERE id = $1", accountID)
	})

	// User B must be rejected.
	_, err = repo.GetOrCreateByClientID(context.Background(), clientID, userB)
	if err == nil {
		t.Fatal("expected ErrCrosstenantAccount, got nil")
	}

	if !errors.Is(err, repository.ErrCrosstenantAccount) {
		t.Errorf("want ErrCrosstenantAccount, got: %v", err)
	}

	// Verify that only one accounts row exists for this client_id.
	var count int
	_ = db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM accounts WHERE client_id = $1`, clientID,
	).Scan(&count)

	if count != 1 {
		t.Errorf("expected exactly 1 accounts row for client_id, got %d — duplicate insert not prevented", count)
	}
}

// TestAccountRepository_GetAccountIDByUserID_NotFound verifies that a missing
// account returns (0, false, nil).
func TestAccountRepository_GetAccountIDByUserID_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewAccountRepository(db)

	id, found, err := repo.GetAccountIDByUserID(context.Background(), -999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found {
		t.Error("expected found=false for non-existent user")
	}
	if id != 0 {
		t.Errorf("expected id=0, got %d", id)
	}
}
