package repository_test

import (
	"context"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// TestUserRepository_Interface verifies NewUserRepository compiles correctly
// with any DB implementation.
func TestUserRepository_Interface(t *testing.T) {
	// fakeDB is defined in api_key_repo_test.go in the same package.
	var db repository.DB = &fakeDB{}
	repo := repository.NewUserRepository(db)

	if repo == nil {
		t.Fatal("NewUserRepository returned nil")
	}
}

// TestUserRepository_UpsertByClerkUserID_CreatesRow verifies that upserting a
// brand-new Clerk user ID inserts a row and returns it.
// Requires DATABASE_URL — skipped otherwise.
func TestUserRepository_UpsertByClerkUserID_CreatesRow(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewUserRepository(db)

	clerkID := "user_test_create_" + t.Name()

	u, err := repo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("UpsertByClerkUserID: %v", err)
	}

	if u == nil {
		t.Fatal("UpsertByClerkUserID returned nil user")
	}

	if u.ID == 0 {
		t.Error("expected non-zero user ID")
	}

	if u.ClerkUserID == nil || *u.ClerkUserID != clerkID {
		t.Errorf("ClerkUserID: want %q, got %v", clerkID, u.ClerkUserID)
	}

	wantEmail := clerkID + "@clerk.local"
	if u.Email != wantEmail {
		t.Errorf("Email placeholder: want %q, got %q", wantEmail, u.Email)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE clerk_user_id = $1", clerkID)
	})
}

// TestUserRepository_UpsertByClerkUserID_IdempotentOnConflict verifies that
// upserting the same Clerk user ID twice returns the same user row both times.
func TestUserRepository_UpsertByClerkUserID_IdempotentOnConflict(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewUserRepository(db)

	clerkID := "user_test_idempotent_" + t.Name()

	first, err := repo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	second, err := repo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("idempotent upsert IDs differ: first=%d second=%d", first.ID, second.ID)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE clerk_user_id = $1", clerkID)
	})
}

// TestUserRepository_GetByClerkUserID_NotFound verifies that a lookup for an
// unknown Clerk user ID returns (nil, nil) — no error.
func TestUserRepository_GetByClerkUserID_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewUserRepository(db)

	u, err := repo.GetByClerkUserID(context.Background(), "user_definitely_does_not_exist_xyz_999")
	if err != nil {
		t.Fatalf("GetByClerkUserID: %v", err)
	}

	if u != nil {
		t.Errorf("expected nil for unknown clerk ID, got %+v", u)
	}
}

// TestUserRepository_UpdateCOPPAColumns_SetRestricted verifies that
// UpdateCOPPAColumns stores date_of_birth_year and coppa_restricted correctly.
// Requires DATABASE_URL + migration for COPPA columns applied.
func TestUserRepository_UpdateCOPPAColumns_SetRestricted(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewUserRepository(db)

	clerkID := "user_coppa_test_" + t.Name()
	u, err := repo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("UpsertByClerkUserID: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE clerk_user_id = $1", clerkID)
	})

	dobYear := int16(2014)
	if err := repo.UpdateCOPPAColumns(context.Background(), u.ID, &dobYear, true); err != nil {
		t.Fatalf("UpdateCOPPAColumns: %v", err)
	}

	// Read back and assert.
	var gotDOBYear *int16
	var gotCOPPARestricted bool
	err = db.QueryRowContext(
		context.Background(),
		`SELECT date_of_birth_year, coppa_restricted FROM users WHERE id = $1`,
		u.ID,
	).Scan(&gotDOBYear, &gotCOPPARestricted)
	if err != nil {
		t.Fatalf("SELECT users COPPA columns: %v", err)
	}

	if gotDOBYear == nil || *gotDOBYear != dobYear {
		t.Errorf("date_of_birth_year: want %d, got %v", dobYear, gotDOBYear)
	}
	if !gotCOPPARestricted {
		t.Errorf("coppa_restricted: want true, got false")
	}
}

// TestUserRepository_UpdateCOPPAColumns_Idempotent verifies that calling
// UpdateCOPPAColumns twice with the same values is a no-op (no error).
func TestUserRepository_UpdateCOPPAColumns_Idempotent(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewUserRepository(db)

	clerkID := "user_coppa_idempotent_" + t.Name()
	u, err := repo.UpsertByClerkUserID(context.Background(), clerkID)
	if err != nil {
		t.Fatalf("UpsertByClerkUserID: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DELETE FROM users WHERE clerk_user_id = $1", clerkID)
	})

	dobYear := int16(2015)
	if err := repo.UpdateCOPPAColumns(context.Background(), u.ID, &dobYear, false); err != nil {
		t.Fatalf("first UpdateCOPPAColumns: %v", err)
	}
	// Second call with same values — must not error.
	if err := repo.UpdateCOPPAColumns(context.Background(), u.ID, &dobYear, false); err != nil {
		t.Fatalf("second UpdateCOPPAColumns (idempotent): %v", err)
	}
}
