package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// newCardSetResolverFromDB constructs a CardSetResolver against the given DB.
// The helper is used by all CardSetResolver integration tests in this file.
func newCardSetResolverFromDB(db *sql.DB) *repository.CardSetResolver {
	return repository.NewCardSetResolver(db)
}

// TestCardSetResolver_Resolve exercises the real ResolveArenaID query against a
// live database.  The test inserts a synthetic set_cards row and verifies the
// resolver returns the correct arena_id.
//
// This is an integration test: it is skipped when DATABASE_URL is not set.
func TestCardSetResolver_Resolve(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Insert a synthetic set_cards row for testing.  Use a high numeric arena_id
	// string that is unlikely to collide with real data.
	const testSetCode = "TST"
	const testArenaID = "999901"
	const testName = "Test Card Alpha"

	_, err := db.ExecContext(
		ctx, `
		INSERT INTO set_cards (set_code, arena_id, scryfall_id, name)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (set_code, arena_id) DO NOTHING`,
		testSetCode, testArenaID, "test-scryfall-id-999901", testName,
	)
	if err != nil {
		t.Fatalf("insert test set_cards row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM set_cards WHERE set_code = $1 AND arena_id = $2`, testSetCode, testArenaID)
	})

	resolver := newCardSetResolverFromDB(db)

	t.Run("found by set_code and name", func(t *testing.T) {
		arenaID, found, err := resolver.ResolveArenaID(ctx, testSetCode, testName)
		if err != nil {
			t.Fatalf("ResolveArenaID: %v", err)
		}
		if !found {
			t.Fatal("expected found=true, got false")
		}
		if arenaID != 999901 {
			t.Errorf("expected arenaID=999901, got %d", arenaID)
		}
	})

	t.Run("not found returns found=false, no error", func(t *testing.T) {
		_, found, err := resolver.ResolveArenaID(ctx, "ZZZ", "Nonexistent Card")
		if err != nil {
			t.Fatalf("ResolveArenaID: %v", err)
		}
		if found {
			t.Error("expected found=false for unknown card, got true")
		}
	})

	t.Run("case-insensitive set_code match", func(t *testing.T) {
		arenaID, found, err := resolver.ResolveArenaID(ctx, "tst", testName)
		if err != nil {
			t.Fatalf("ResolveArenaID: %v", err)
		}
		if !found {
			t.Fatal("expected found=true for lowercase set_code, got false")
		}
		if arenaID != 999901 {
			t.Errorf("expected arenaID=999901, got %d", arenaID)
		}
	})
}

// TestCardSetResolver_ResolveMultipleRows ensures that when the same card
// appears in two sets the resolver returns a result (doesn't error or panic).
func TestCardSetResolver_ResolveMultipleRows(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	const setA = "SA9"
	const setB = "SB9"
	const sharedName = "Shared Reprint Card"

	for _, row := range []struct{ set, id, scryfall string }{
		{setA, "999911", "scryfall-sa9"},
		{setB, "999912", "scryfall-sb9"},
	} {
		_, err := db.ExecContext(
			ctx, `
			INSERT INTO set_cards (set_code, arena_id, scryfall_id, name)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (set_code, arena_id) DO NOTHING`,
			row.set, row.id, row.scryfall, sharedName,
		)
		if err != nil {
			t.Fatalf("insert set_cards row set=%q: %v", row.set, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM set_cards WHERE set_code IN ($1, $2) AND name = $3`, setA, setB, sharedName)
	})

	resolver := newCardSetResolverFromDB(db)

	// Resolve for setA — must return setA's arena_id.
	arenaID, found, err := resolver.ResolveArenaID(ctx, setA, sharedName)
	if err != nil {
		t.Fatalf("ResolveArenaID setA: %v", err)
	}
	if !found {
		t.Fatal("expected found=true for setA")
	}
	if arenaID != 999911 {
		t.Errorf("expected 999911 for setA, got %d", arenaID)
	}

	// Resolve for setB — must return setB's arena_id.
	arenaID, found, err = resolver.ResolveArenaID(ctx, setB, sharedName)
	if err != nil {
		t.Fatalf("ResolveArenaID setB: %v", err)
	}
	if !found {
		t.Fatal("expected found=true for setB")
	}
	if arenaID != 999912 {
		t.Errorf("expected 999912 for setB, got %d", arenaID)
	}
}
