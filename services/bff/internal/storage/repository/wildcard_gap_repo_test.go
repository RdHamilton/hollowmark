package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ─── shared test helpers ──────────────────────────────────────────────────────

// insertTestAccountForWildcard inserts a minimal accounts row and returns its id.
func insertTestAccountForWildcard(t *testing.T, db *sql.DB, suffix string) int64 {
	t.Helper()
	name := fmt.Sprintf("wc-test-account-%s", suffix)
	var id int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO accounts (name) VALUES ($1) RETURNING id`,
		name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestAccountForWildcard %q: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = $1`, id)
	})
	return id
}

// insertTestInventory inserts an inventory row with the given wildcard counts.
func insertTestWildcardInventory(t *testing.T, db *sql.DB, accountID int64, common, uncommon, rare, mythic int) {
	t.Helper()
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO inventory
			(account_id, gold, gems, wc_common, wc_uncommon, wc_rare, wc_mythic,
			 vault_progress, draft_tokens, sealed_tokens, updated_at)
		 VALUES ($1, 0, 0, $2, $3, $4, $5, 0, 0, 0, NOW())
		 ON CONFLICT (account_id) DO UPDATE
		 SET wc_common=$2, wc_uncommon=$3, wc_rare=$4, wc_mythic=$5`,
		accountID, common, uncommon, rare, mythic,
	)
	if err != nil {
		t.Fatalf("insertTestInventory account=%d: %v", accountID, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM inventory WHERE account_id = $1`, accountID)
	})
}

// insertTestCardInventory inserts a card_inventory row.
func insertTestCardInventory(t *testing.T, db *sql.DB, accountID int64, cardID, count int) {
	t.Helper()
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO card_inventory (account_id, card_id, count, snapshot_hash, updated_at)
		 VALUES ($1, $2, $3, 'testhash', NOW())
		 ON CONFLICT (account_id, card_id) DO UPDATE SET count = EXCLUDED.count`,
		accountID, cardID, count,
	)
	if err != nil {
		t.Fatalf("insertTestCardInventory account=%d card=%d: %v", accountID, cardID, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM card_inventory WHERE account_id = $1 AND card_id = $2`,
			accountID, cardID)
	})
}

// ─── InventoryRepository.GetWildcardCounts ───────────────────────────────────

func TestInventoryRepository_GetWildcardCounts_HappyPath(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewInventoryRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForWildcard(t, db, "wc-counts-happy")
	insertTestWildcardInventory(t, db, accountID, 5, 3, 2, 1)

	wc, err := repo.GetWildcardCounts(ctx, accountID)
	if err != nil {
		t.Fatalf("GetWildcardCounts: %v", err)
	}
	if wc.Common != 5 {
		t.Errorf("Common: got %d, want 5", wc.Common)
	}
	if wc.Uncommon != 3 {
		t.Errorf("Uncommon: got %d, want 3", wc.Uncommon)
	}
	if wc.Rare != 2 {
		t.Errorf("Rare: got %d, want 2", wc.Rare)
	}
	if wc.Mythic != 1 {
		t.Errorf("Mythic: got %d, want 1", wc.Mythic)
	}
}

func TestInventoryRepository_GetWildcardCounts_NoRow_ReturnsZeros(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewInventoryRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForWildcard(t, db, "wc-counts-norow")
	// Intentionally do NOT insert an inventory row.

	wc, err := repo.GetWildcardCounts(ctx, accountID)
	if err != nil {
		t.Fatalf("GetWildcardCounts (no row): %v", err)
	}
	if wc.Common != 0 || wc.Uncommon != 0 || wc.Rare != 0 || wc.Mythic != 0 {
		t.Errorf("expected all-zero WildcardCounts for missing inventory row, got %+v", wc)
	}
}

// ─── CardInventoryRepository.HasCardInventory ────────────────────────────────

func TestCardInventoryRepository_HasCardInventory_True(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewCardInventoryRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForWildcard(t, db, "has-inv-true")
	insertTestCardInventory(t, db, accountID, 88001, 4)

	has, err := repo.HasCardInventory(ctx, accountID)
	if err != nil {
		t.Fatalf("HasCardInventory: %v", err)
	}
	if !has {
		t.Error("expected HasCardInventory=true, got false")
	}
}

func TestCardInventoryRepository_HasCardInventory_False(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewCardInventoryRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForWildcard(t, db, "has-inv-false")
	// No card_inventory rows for this account.

	has, err := repo.HasCardInventory(ctx, accountID)
	if err != nil {
		t.Fatalf("HasCardInventory (empty): %v", err)
	}
	if has {
		t.Error("expected HasCardInventory=false for account with no rows, got true")
	}
}

// ─── DraftRatingsRepository.GetMaxCachedAtByFormat ───────────────────────────

func TestDraftRatingsRepository_GetMaxCachedAtByFormat_ReturnsNilForMissing(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftRatingsRepository(db)

	result, err := repo.GetMaxCachedAtByFormat(context.Background(), "FormatThatDoesNotExist999")
	if err != nil {
		t.Fatalf("GetMaxCachedAtByFormat: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for missing format, got %v", result)
	}
}

func TestDraftRatingsRepository_GetMaxCachedAtByFormat_ReturnsPointerWhenRowsExist(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftRatingsRepository(db)

	const format = "PremierDraft"
	now := time.Now().UTC().Truncate(time.Second)
	seedCardRating(t, db, "WCT1", format, "WildcardTestCard", now)

	result, err := repo.GetMaxCachedAtByFormat(context.Background(), format)
	if err != nil {
		t.Fatalf("GetMaxCachedAtByFormat: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil *time.Time, got nil")
	}
	// The returned time should be >= our seeded now (other rows may be newer).
	if result.Before(now.Add(-time.Second)) {
		t.Errorf("GetMaxCachedAtByFormat: %v is before seed time %v", result, now)
	}
}

// ─── MetaRepository.GetMetaLastUpdated ───────────────────────────────────────

// TestMetaRepo_GetMetaLastUpdated_NoRows_ReturnsNil is the canonical test that
// locks the contract: LatestArchetypeUpdate bool=false → nil, nil from
// GetMetaLastUpdated. Ray requires this test by name.
func TestMetaRepo_GetMetaLastUpdated_NoRows_ReturnsNil(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMetaRepository(db)
	ctx := context.Background()

	result, err := repo.GetMetaLastUpdated(ctx, "format-no-meta-rows-xyz")
	if err != nil {
		t.Fatalf("GetMetaLastUpdated (no rows): %v", err)
	}
	if result != nil {
		t.Errorf("expected nil *time.Time for missing format, got %v", result)
	}
}

func TestMetaRepo_GetMetaLastUpdated_HappyPath(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMetaRepository(db)
	ctx := context.Background()

	before := time.Now().UTC().Truncate(time.Second)
	insertTestArchetype(t, db, "wc-meta-freshness-deck", "Standard", nil)

	result, err := repo.GetMetaLastUpdated(ctx, "Standard")
	if err != nil {
		t.Fatalf("GetMetaLastUpdated: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil *time.Time, got nil")
	}
	if result.Before(before) {
		t.Errorf("GetMetaLastUpdated: %v is before seed time %v", result, before)
	}
}

// ─── WildcardGapRepository.CountCardInventory ────────────────────────────────

func TestWildcardGapRepository_CountCardInventory_Zero(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewWildcardGapRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForWildcard(t, db, "count-inv-zero")

	n, err := repo.CountCardInventory(ctx, accountID)
	if err != nil {
		t.Fatalf("CountCardInventory: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 for account with no rows, got %d", n)
	}
}

func TestWildcardGapRepository_CountCardInventory_NonZero(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewWildcardGapRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForWildcard(t, db, "count-inv-nonzero")
	insertTestCardInventory(t, db, accountID, 77001, 4)
	insertTestCardInventory(t, db, accountID, 77002, 2)
	insertTestCardInventory(t, db, accountID, 77003, 1)

	n, err := repo.CountCardInventory(ctx, accountID)
	if err != nil {
		t.Fatalf("CountCardInventory: %v", err)
	}
	if n != 3 {
		t.Errorf("expected 3 distinct cards, got %d", n)
	}
}

// ─── WildcardGapRepository.GetWildcardGapRows ────────────────────────────────

// TestWildcardGapRepository_GetWildcardGapRows_EmptyWhenNoArchetypes verifies
// the query returns an empty slice (not an error) when no archetypes exist for
// the format.
func TestWildcardGapRepository_GetWildcardGapRows_EmptyWhenNoArchetypes(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewWildcardGapRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForWildcard(t, db, "gap-no-archetypes")

	rows, err := repo.GetWildcardGapRows(ctx, accountID, "FormatThatDoesNotExistZZZ")
	if err != nil {
		t.Fatalf("GetWildcardGapRows: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

// TestWildcardGapRepository_GetWildcardGapRows_CrossTenantIsolation verifies
// that account_id scoping is enforced: Account B's card ownership must not
// appear in Account A's results.
//
// This test exercises the COALESCE(ci.count, 0) path only — no set_cards row
// is seeded, so the card_inventory LEFT JOIN never fires and both accounts see
// CopiesOwned=0. It confirms that two independent queries return the same
// archetype rows and neither panics. The stronger ownership-value assertion is
// in TestWildcardGapRepository_GetWildcardGapRows_CrossTenantIsolation_RealOwnership.
func TestWildcardGapRepository_GetWildcardGapRows_CrossTenantIsolation(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewWildcardGapRepository(db)
	ctx := context.Background()

	accountA := insertTestAccountForWildcard(t, db, "gap-isolation-a")
	accountB := insertTestAccountForWildcard(t, db, "gap-isolation-b")

	// Insert a fresh archetype with one card.
	tierS := "S"
	archetypeID := insertRecentArchetype(t, db, "wc-gap-isolation-arch", "Standard", &tierS)
	insertTestArchetypeCard(t, db, archetypeID, "wc-gap-isolation-card", "Creature", 4)

	rowsA, err := repo.GetWildcardGapRows(ctx, accountA, "Standard")
	if err != nil {
		t.Fatalf("GetWildcardGapRows (account A): %v", err)
	}
	rowsB, err := repo.GetWildcardGapRows(ctx, accountB, "Standard")
	if err != nil {
		t.Fatalf("GetWildcardGapRows (account B): %v", err)
	}

	foundA := false
	for _, r := range rowsA {
		if r.ArchetypeID == archetypeID {
			foundA = true
		}
	}
	foundB := false
	for _, r := range rowsB {
		if r.ArchetypeID == archetypeID {
			foundB = true
		}
	}
	if !foundA {
		t.Error("Account A result missing wc-gap-isolation-arch archetype")
	}
	if !foundB {
		t.Error("Account B result missing wc-gap-isolation-arch archetype")
	}
}

// TestWildcardGapRepository_GetWildcardGapRows_CrossTenantIsolation_RealOwnership
// is the strengthened form of the cross-tenant isolation test (ticket #843).
//
// It seeds a real set_cards row so the card_inventory LEFT JOIN fires via the
// arena_id path. Account A is seeded with 2 copies of the test card; Account B
// with 4 copies. The test then asserts:
//   - rowsA contains CopiesOwned=2 for the test card (not 0, not 4).
//   - rowsB contains CopiesOwned=4 for the test card (not 0, not 2).
//   - Neither account's rows contain the other account's CopiesOwned value for
//     the same card, proving the account_id=$1 scoping in the join is enforced.
func TestWildcardGapRepository_GetWildcardGapRows_CrossTenantIsolation_RealOwnership(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewWildcardGapRepository(db)
	ctx := context.Background()

	// Use a high arena_id unlikely to collide with real data. The card name
	// must match the archetype_cards row exactly (case-insensitive join).
	const testArenaID = 88999
	const testCardName = "wc-gap-isolation-owned-card"
	const testSetCode = "WCT"

	// Seed the set_cards row so the name→arena_id join fires.
	insertTestSetCard(t, db, setCardSeed{
		SetCode: testSetCode,
		ArenaID: "88999",
		Name:    testCardName,
		Rarity:  "rare",
	})

	accountA := insertTestAccountForWildcard(t, db, "gap-isolation-owned-a")
	accountB := insertTestAccountForWildcard(t, db, "gap-isolation-owned-b")

	// Archetype requires 4 copies of the test card.
	tierS := "S"
	archetypeID := insertRecentArchetype(t, db, "wc-gap-isolation-owned-arch", "Standard", &tierS)
	insertTestArchetypeCard(t, db, archetypeID, testCardName, "Creature", 4)

	// Account A owns 2 copies; Account B owns 4.
	insertTestCardInventory(t, db, accountA, testArenaID, 2)
	insertTestCardInventory(t, db, accountB, testArenaID, 4)

	rowsA, err := repo.GetWildcardGapRows(ctx, accountA, "Standard")
	if err != nil {
		t.Fatalf("GetWildcardGapRows (account A): %v", err)
	}
	rowsB, err := repo.GetWildcardGapRows(ctx, accountB, "Standard")
	if err != nil {
		t.Fatalf("GetWildcardGapRows (account B): %v", err)
	}

	// Find the test card row in Account A's results.
	var ownedA, ownedB int
	var foundInA, foundInB bool
	for _, r := range rowsA {
		if r.ArchetypeID == archetypeID && r.CardName == testCardName {
			ownedA = r.CopiesOwned
			foundInA = true
		}
	}
	for _, r := range rowsB {
		if r.ArchetypeID == archetypeID && r.CardName == testCardName {
			ownedB = r.CopiesOwned
			foundInB = true
		}
	}

	if !foundInA {
		t.Fatal("Account A result missing test card row for the seeded archetype")
	}
	if !foundInB {
		t.Fatal("Account B result missing test card row for the seeded archetype")
	}

	// Core isolation assertion: each account sees only its own CopiesOwned.
	if ownedA != 2 {
		t.Errorf("Account A CopiesOwned: got %d, want 2 (Account B's value must not bleed through)", ownedA)
	}
	if ownedB != 4 {
		t.Errorf("Account B CopiesOwned: got %d, want 4 (Account A's value must not bleed through)", ownedB)
	}

	// Sanity-check cross-bleed in the opposite direction.
	if ownedA == 4 {
		t.Error("Account A received Account B's CopiesOwned=4 — cross-tenant data bleed detected")
	}
	if ownedB == 2 {
		t.Error("Account B received Account A's CopiesOwned=2 — cross-tenant data bleed detected")
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// insertRecentArchetype inserts an archetype with last_updated = NOW() so the
// 7-day window filter in the gap query includes it.
func insertRecentArchetype(t *testing.T, db *sql.DB, name, format string, tier *string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO mtgzone_archetypes (name, format, tier, last_updated)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (name, format) DO UPDATE
		 SET tier = EXCLUDED.tier, last_updated = NOW()
		 RETURNING id`,
		name, format, tier,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertRecentArchetype %q/%q: %v", name, format, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM mtgzone_archetypes WHERE id = $1`, id)
	})
	return id
}
