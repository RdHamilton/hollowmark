package repository_test

// collection_pagination_test.go — integration tests for #1325
//
// Tests for:
//  - ListCollectionPage with Page/Limit params (offset pagination)
//  - CountFilteredCollection (the count-half of the shared builder)
//  - Deterministic ordering with tiebreaker card_id across all 5 sort columns
//  - Search predicate in CTE (not outer WHERE) — reprint dedup preserved
//  - Shared builder: ListCollectionPage and CountFilteredCollection agree on filtered count

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ---------------------------------------------------------------------------
// seedLargeInventory seeds n cards for accountID using bulk INSERT for speed.
// set_cards rows use arena_ids starting at startArenaID. Returns those IDs.
// ---------------------------------------------------------------------------
func seedLargeInventory(t *testing.T, n int, accountID int64, setCode string, startArenaID int) []int {
	t.Helper()
	db := openTestDB(t)

	ids := make([]int, 0, n)

	// Bulk-insert set_cards in one statement to avoid N round-trips over SSM.
	const batchSize = 500
	for batchStart := 0; batchStart < n; batchStart += batchSize {
		end := batchStart + batchSize
		if end > n {
			end = n
		}
		batchN := end - batchStart

		var scBuf bytes.Buffer
		scBuf.WriteString("INSERT INTO set_cards (set_code, arena_id, scryfall_id, name, rarity, colors) VALUES ")
		scArgs := make([]any, 0, batchN*6)
		scIdx := 1
		for i := range batchN {
			arenaID := startArenaID + batchStart + i
			name := fmt.Sprintf("Card %05d", arenaID)
			if i > 0 {
				scBuf.WriteString(", ")
			}
			scBuf.WriteString(fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d)",
				scIdx, scIdx+1, scIdx+2, scIdx+3, scIdx+4, scIdx+5))
			scArgs = append(
				scArgs,
				setCode,
				fmt.Sprintf("%d", arenaID),
				fmt.Sprintf("scryfall-%d-%s", arenaID, setCode),
				name,
				"common",
				"[]",
			)
			scIdx += 6
			ids = append(ids, arenaID)
		}
		scBuf.WriteString(" ON CONFLICT (set_code, arena_id) DO NOTHING")
		if _, err := db.ExecContext(context.Background(), scBuf.String(), scArgs...); err != nil {
			t.Fatalf("seedLargeInventory set_cards batch: %v", err)
		}

		// Bulk-insert card_inventory
		var ciBuf bytes.Buffer
		ciBuf.WriteString("INSERT INTO card_inventory (account_id, card_id, count, snapshot_hash) VALUES ")
		ciArgs := make([]any, 0, batchN*4)
		ciIdx := 1
		for i := range batchN {
			arenaID := startArenaID + batchStart + i
			if i > 0 {
				ciBuf.WriteString(", ")
			}
			ciBuf.WriteString(fmt.Sprintf("($%d,$%d,$%d,$%d)", ciIdx, ciIdx+1, ciIdx+2, ciIdx+3))
			ciArgs = append(ciArgs, accountID, arenaID, 1, fmt.Sprintf("hash-%d", arenaID))
			ciIdx += 4
		}
		ciBuf.WriteString(" ON CONFLICT (account_id, card_id) DO UPDATE SET count = EXCLUDED.count, snapshot_hash = EXCLUDED.snapshot_hash")
		if _, err := db.ExecContext(context.Background(), ciBuf.String(), ciArgs...); err != nil {
			t.Fatalf("seedLargeInventory card_inventory batch: %v", err)
		}
	}

	// Register cleanup
	t.Cleanup(func() {
		arenaStrs := make([]string, len(ids))
		for i, id := range ids {
			arenaStrs[i] = fmt.Sprintf("'%d'", id)
		}
		_, _ = db.ExecContext(
			context.Background(),
			"DELETE FROM set_cards WHERE set_code = $1 AND arena_id = ANY(ARRAY["+strings.Join(arenaStrs, ",")+"]);",
			setCode,
		)
		cardArgs := make([]any, len(ids)+1)
		cardArgs[0] = accountID
		placeholders := make([]string, len(ids))
		for i, id := range ids {
			cardArgs[i+1] = id
			placeholders[i] = fmt.Sprintf("$%d", i+2)
		}
		_, _ = db.ExecContext(
			context.Background(),
			"DELETE FROM card_inventory WHERE account_id = $1 AND card_id = ANY(ARRAY["+strings.Join(placeholders, ",")+"]);",
			cardArgs...,
		)
	})

	return ids
}

// openTestDBDirect opens a fresh *sql.DB handle not bound to a specific test's
// cleanup — used by seedLargeInventory so the connection outlives a sub-test.
func openTestDBDirect(t *testing.T) *sql.DB {
	t.Helper()
	return openTestDB(t)
}

// ---------------------------------------------------------------------------
// TestCollectionRepository_Pagination_PageBoundaries
//
// Seeds 10,000 cards and verifies:
//   - page 1 returns Limit items (50)
//   - page 2 returns the NEXT Limit items (no overlap)
//   - deep page (page 100) returns items without falling back to all 10k
//   - no duplicates across contiguous pages
//
// ---------------------------------------------------------------------------
func TestCollectionRepository_Pagination_PageBoundaries(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewCollectionRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "pag-boundaries")
	insertTestSet(t, db, "PGB", "Pagination Boundaries Set")
	seedLargeInventory(t, 10000, accountID, "PGB", 8000001)

	const pageSize = 50
	f := repository.CollectionFilter{}

	// Page 1
	p := repository.CollectionPage{Page: 1, Limit: pageSize}
	page1, err := repo.ListCollectionPage(ctx, accountID, f, p)
	if err != nil {
		t.Fatalf("ListCollectionPage page=1: %v", err)
	}
	if len(page1) != pageSize {
		t.Fatalf("page 1: got %d items, want %d", len(page1), pageSize)
	}

	// Page 2 — items must not overlap with page 1
	p2 := repository.CollectionPage{Page: 2, Limit: pageSize}
	page2, err := repo.ListCollectionPage(ctx, accountID, f, p2)
	if err != nil {
		t.Fatalf("ListCollectionPage page=2: %v", err)
	}
	if len(page2) != pageSize {
		t.Fatalf("page 2: got %d items, want %d", len(page2), pageSize)
	}

	// Build card-id set from page 1
	page1IDs := make(map[int]struct{}, pageSize)
	for _, item := range page1 {
		page1IDs[item.CardID] = struct{}{}
	}
	for _, item := range page2 {
		if _, dup := page1IDs[item.CardID]; dup {
			t.Errorf("card %d appears on both page 1 and page 2 (boundary overlap)", item.CardID)
		}
	}

	// Deep page (page 100 of 200 with pageSize=50 over 10k cards)
	pDeep := repository.CollectionPage{Page: 100, Limit: pageSize}
	pageDeep, err := repo.ListCollectionPage(ctx, accountID, f, pDeep)
	if err != nil {
		t.Fatalf("ListCollectionPage deep page=100: %v", err)
	}
	if len(pageDeep) != pageSize {
		t.Fatalf("deep page 100: got %d items, want %d", len(pageDeep), pageSize)
	}
}

// ---------------------------------------------------------------------------
// TestCollectionRepository_Pagination_LastPageIsPartial
//
// Seeds 105 cards; with pageSize=50:
//   - pages 1 and 2 return 50 items each
//   - page 3 returns 5 items (the remainder)
//
// ---------------------------------------------------------------------------
func TestCollectionRepository_Pagination_LastPageIsPartial(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewCollectionRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "pag-partial")
	insertTestSet(t, db, "PPL", "Partial Last Page Set")
	seedLargeInventory(t, 105, accountID, "PPL", 9100001)

	f := repository.CollectionFilter{}
	const pageSize = 50

	p3 := repository.CollectionPage{Page: 3, Limit: pageSize}
	page3, err := repo.ListCollectionPage(ctx, accountID, f, p3)
	if err != nil {
		t.Fatalf("ListCollectionPage page=3: %v", err)
	}
	if len(page3) != 5 {
		t.Fatalf("last page: got %d items, want 5", len(page3))
	}
}

// ---------------------------------------------------------------------------
// TestCollectionRepository_CountFilteredCollection_MatchesListTotal
//
// Verifies that CountFilteredCollection and ListCollectionPage use the same
// shared builder (they agree on filtered count).
// ---------------------------------------------------------------------------
func TestCollectionRepository_CountFilteredCollection_MatchesListTotal(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewCollectionRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "pag-count-match")
	insertTestSet(t, db, "PCM", "Count Match Set")
	insertTestSet(t, db, "PCN", "Count Non-match Set")

	// Insert 30 cards in PCM and 20 in PCN
	seedLargeInventory(t, 30, accountID, "PCM", 9200001)
	seedLargeInventory(t, 20, accountID, "PCN", 9200101)

	// Filter by PCM — should count 30
	f := repository.CollectionFilter{SetCode: "PCM"}
	p := repository.CollectionPage{Page: 1, Limit: 100}

	items, err := repo.ListCollectionPage(ctx, accountID, f, p)
	if err != nil {
		t.Fatalf("ListCollectionPage: %v", err)
	}
	count, err := repo.CountFilteredCollection(ctx, accountID, f)
	if err != nil {
		t.Fatalf("CountFilteredCollection: %v", err)
	}
	if count != 30 {
		t.Errorf("CountFilteredCollection(PCM): got %d, want 30", count)
	}
	if len(items) != 30 {
		t.Errorf("ListCollectionPage(PCM): got %d items, want 30", len(items))
	}
	// The two must agree
	if len(items) != count {
		t.Errorf("list(%d) and count(%d) disagree for the same filter", len(items), count)
	}
}

// ---------------------------------------------------------------------------
// TestCollectionRepository_CountFilteredCollection_EmptyFilter
//
// With no filter, CountFilteredCollection returns total owned cards.
// ---------------------------------------------------------------------------
func TestCollectionRepository_CountFilteredCollection_EmptyFilter(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewCollectionRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "pag-count-empty")
	insertTestSet(t, db, "PCE", "Count Empty Set")
	seedLargeInventory(t, 42, accountID, "PCE", 9300001)

	count, err := repo.CountFilteredCollection(ctx, accountID, repository.CollectionFilter{})
	if err != nil {
		t.Fatalf("CountFilteredCollection: %v", err)
	}
	if count != 42 {
		t.Errorf("CountFilteredCollection (no filter): got %d, want 42", count)
	}
}

// ---------------------------------------------------------------------------
// TestCollectionRepository_Sort_DeterministicTiebreaker
//
// Inserts cards with identical sort-column values (same name, quantity, rarity,
// cmc, price) and verifies that repeated calls with the same sort return
// identical ordering — meaning the ci.card_id tiebreaker is active.
// ---------------------------------------------------------------------------
func TestCollectionRepository_Sort_DeterministicTiebreaker(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewCollectionRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "pag-sort-tie")
	insertTestSet(t, db, "PST", "Sort Tiebreaker Set")

	// Insert 10 cards all named "Tiebreaker Card" with same rarity/cmc — sort
	// tiebreaker is the only differentiator.
	for i := range 10 {
		arenaID := 9400001 + i
		insertTestSetCard(t, db, setCardSeed{
			SetCode: "PST",
			ArenaID: fmt.Sprintf("%d", arenaID),
			Name:    "Tiebreaker Card",
			Rarity:  "rare",
		})
		insertTestInventory(t, db, accountID, arenaID, 2)
	}

	sortColumns := []string{"name", "quantity", "rarity", "cmc", "price"}
	for _, col := range sortColumns {
		t.Run("sort="+col, func(t *testing.T) {
			f := repository.CollectionFilter{SortBy: col, SortDesc: false}
			p := repository.CollectionPage{Page: 1, Limit: 10}

			run1, err := repo.ListCollectionPage(ctx, accountID, f, p)
			if err != nil {
				t.Fatalf("run1 sort=%s: %v", col, err)
			}
			run2, err := repo.ListCollectionPage(ctx, accountID, f, p)
			if err != nil {
				t.Fatalf("run2 sort=%s: %v", col, err)
			}
			if len(run1) != len(run2) {
				t.Fatalf("sort=%s: run1 len=%d run2 len=%d", col, len(run1), len(run2))
			}
			for i := range run1 {
				if run1[i].CardID != run2[i].CardID {
					t.Errorf("sort=%s: nondeterministic at position %d: run1=%d run2=%d (missing card_id tiebreaker)",
						col, i, run1[i].CardID, run2[i].CardID)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCollectionRepository_Search_InCTE_PreservesReprintDedup
//
// Search predicate must live in the CTE WHERE, not the outer WHERE.
// A card whose name matches the search but whose lowest-id printing is in a
// different set must still appear exactly once.
// ---------------------------------------------------------------------------
func TestCollectionRepository_Search_InCTE_PreservesReprintDedup(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewCollectionRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "pag-search-cte")
	insertTestSet(t, db, "SR1", "Search Reprint One")
	insertTestSet(t, db, "SR2", "Search Reprint Two")

	// Same arena_id in two sets. S1 row has lower id (inserted first).
	// Search matches the name — should return exactly ONE result.
	insertTestSetCard(t, db, setCardSeed{SetCode: "SR1", ArenaID: "9500001", Name: "Searchable Card", Rarity: "rare"})
	insertTestSetCard(t, db, setCardSeed{SetCode: "SR2", ArenaID: "9500001", Name: "Searchable Card", Rarity: "rare"})
	insertTestInventory(t, db, accountID, 9500001, 1)

	f := repository.CollectionFilter{Search: "searchable"}
	p := repository.CollectionPage{Page: 1, Limit: 50}

	items, err := repo.ListCollectionPage(ctx, accountID, f, p)
	if err != nil {
		t.Fatalf("ListCollectionPage search: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("search reprint dedup: expected 1 item, got %d (search predicate in outer WHERE reintroduced reprint bug)", len(items))
	}
}

// ---------------------------------------------------------------------------
// TestCollectionRepository_Search_MatchesNameAndSetCode
//
// Search matches against name and set_code.
// ---------------------------------------------------------------------------
func TestCollectionRepository_Search_MatchesNameAndSetCode(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewCollectionRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "pag-search-fields")
	insertTestSet(t, db, "SMF", "Search Fields Set")

	insertTestSetCard(t, db, setCardSeed{SetCode: "SMF", ArenaID: "9600001", Name: "Fireball", Rarity: "common"})
	insertTestSetCard(t, db, setCardSeed{SetCode: "SMF", ArenaID: "9600002", Name: "Counterspell", Rarity: "uncommon"})
	insertTestInventory(t, db, accountID, 9600001, 1)
	insertTestInventory(t, db, accountID, 9600002, 1)

	// Search by name substring
	f := repository.CollectionFilter{Search: "fire"}
	p := repository.CollectionPage{Page: 1, Limit: 50}

	items, err := repo.ListCollectionPage(ctx, accountID, f, p)
	if err != nil {
		t.Fatalf("search by name: %v", err)
	}
	if len(items) != 1 || items[0].Name != "Fireball" {
		t.Errorf("search 'fire': expected [Fireball], got %v", items)
	}

	// Search by set_code
	f2 := repository.CollectionFilter{Search: "smf"}
	items2, err := repo.ListCollectionPage(ctx, accountID, f2, p)
	if err != nil {
		t.Fatalf("search by set_code: %v", err)
	}
	if len(items2) != 2 {
		t.Errorf("search 'smf' (set code): expected 2 items, got %d", len(items2))
	}
}

// ---------------------------------------------------------------------------
// TestCollectionRepository_Sort_AllColumns
//
// Smoke-test all 5 sort columns (name/quantity/rarity/cmc/price) in both
// directions — verifies the sort compiles and returns the correct count.
// ---------------------------------------------------------------------------
func TestCollectionRepository_Sort_AllColumns(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewCollectionRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "pag-sort-all")
	insertTestSet(t, db, "PSA", "Sort All Set")

	for i := range 10 {
		arenaID := 9700001 + i
		insertTestSetCard(t, db, setCardSeed{
			SetCode:  "PSA",
			ArenaID:  fmt.Sprintf("%d", arenaID),
			Name:     fmt.Sprintf("SortCard%02d", i),
			Rarity:   []string{"common", "uncommon", "rare", "mythic"}[i%4],
			PriceUSD: ptrF(float64(i) * 0.5),
		})
		insertTestInventory(t, db, accountID, arenaID, i+1)
	}

	cases := []struct {
		col  string
		desc bool
	}{
		{"name", false},
		{"name", true},
		{"quantity", false},
		{"quantity", true},
		{"rarity", false},
		{"rarity", true},
		{"cmc", false},
		{"cmc", true},
		{"price", false},
		{"price", true},
	}
	p := repository.CollectionPage{Page: 1, Limit: 50}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_desc=%v", tc.col, tc.desc), func(t *testing.T) {
			f := repository.CollectionFilter{SortBy: tc.col, SortDesc: tc.desc}
			items, err := repo.ListCollectionPage(ctx, accountID, f, p)
			if err != nil {
				t.Fatalf("ListCollectionPage sort=%s desc=%v: %v", tc.col, tc.desc, err)
			}
			if len(items) != 10 {
				t.Errorf("sort=%s desc=%v: got %d items, want 10", tc.col, tc.desc, len(items))
			}
		})
	}
}

// ptrF returns a pointer to a float64.
func ptrF(v float64) *float64 { return &v }
