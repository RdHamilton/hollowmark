//go:build integration

package datasets_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/sync/internal/datasets"
	"github.com/RdHamilton/hollowmark/services/sync/internal/draftdata"
	"github.com/RdHamilton/hollowmark/services/sync/internal/scryfall"
	"github.com/RdHamilton/hollowmark/services/sync/internal/seventeenlands"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPostgresStore_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	store := datasets.NewPostgresStore(pool)

	ratings := draftdata.SetRatings{
		SetCode:     "INT",
		DraftFormat: "PremierDraft",
		FetchedAt:   time.Now().UTC().Truncate(time.Second),
		Cards: []seventeenlands.CardRating{
			{MtgaID: 99901, Name: "Test Card A", ALSA: 1.5, GIHWR: 0.60, SeenCount: 500},
			{MtgaID: 99902, Name: "Test Card B", ALSA: 3.0, GIHWR: 0.45, SeenCount: 300},
		},
	}

	require.NoError(t, store.UpsertRatings(ctx, ratings))

	got, err := store.GetRatings(ctx, "INT", "PremierDraft")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Len(t, got.Cards, 2)

	names := make([]string, len(got.Cards))
	for i, c := range got.Cards {
		names[i] = c.Name
	}
	assert.ElementsMatch(t, []string{"Test Card A", "Test Card B"}, names)

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM draft_card_ratings WHERE set_code = 'INT'")
}

func TestPostgresStore_UpsertSets_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	store := datasets.NewPostgresStore(pool)

	sets := []scryfall.ScryfallSet{
		{Code: "tst", Name: "Test Set Alpha", SetType: "expansion", Digital: true, CardCount: 100, ReleasedAt: "2024-01-01"},
		{Code: "ts2", Name: "Test Set Beta", SetType: "core", Digital: true, CardCount: 200, ReleasedAt: "2024-06-01"},
	}

	require.NoError(t, store.UpsertSets(ctx, sets))

	// Verify rows were inserted with is_draft_active = TRUE.
	// Note: UpsertSets sets is_draft_active (not is_standard_legal). Standard
	// legality is managed separately by BFF migrations and is not written by
	// the sync service.
	for _, s := range sets {
		var name string
		var isDraftActive bool
		var cardCount int
		err := pool.QueryRow(
			ctx,
			`SELECT name, is_draft_active, card_count FROM sets WHERE code = $1`,
			s.Code,
		).Scan(&name, &isDraftActive, &cardCount)
		require.NoError(t, err, "set %q not found", s.Code)
		assert.Equal(t, s.Name, name)
		assert.True(t, isDraftActive, "is_draft_active must be TRUE for %q", s.Code)
		assert.Equal(t, s.CardCount, cardCount)
	}

	// Verify upsert updates an existing row.
	updated := []scryfall.ScryfallSet{
		{Code: "tst", Name: "Test Set Alpha Updated", SetType: "expansion", Digital: true, CardCount: 150, ReleasedAt: "2024-01-01"},
	}
	require.NoError(t, store.UpsertSets(ctx, updated))

	var name string
	err = pool.QueryRow(ctx, `SELECT name FROM sets WHERE code = 'tst'`).Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "Test Set Alpha Updated", name)

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM sets WHERE code IN ('tst', 'ts2')")
}

// TestGetActiveSets_ReturnsSeventeenlandsCode_Integration verifies that when a set has
// seventeenlands_code populated, GetActiveSets returns a SyncSet with
// Code = Scryfall code and ExpansionCode = seventeenlands_code value.
func TestGetActiveSets_ReturnsSeventeenlandsCode_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Seed a test set with a distinct seventeenlands_code.
	_, err = pool.Exec(ctx, `
		INSERT INTO sets (code, name, released_at, set_type, card_count, is_draft_active, seventeenlands_code, last_updated)
		VALUES ('_t1', 'Integration Test Set 1', '2024-01-01', 'expansion', 250, TRUE, 'T1X', NOW())
		ON CONFLICT (code) DO UPDATE SET
			is_draft_active      = TRUE,
			seventeenlands_code  = 'T1X',
			last_updated         = NOW()
	`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM sets WHERE code = '_t1'") })

	store := datasets.NewPostgresStore(pool)
	sets, err := store.GetActiveSets(ctx)
	require.NoError(t, err)

	var found *datasets.SyncSet
	for i := range sets {
		if sets[i].Code == "_t1" {
			found = &sets[i]
			break
		}
	}
	require.NotNil(t, found, "seeded set _t1 must appear in GetActiveSets result")
	assert.Equal(t, "_t1", found.Code, "Code must be the Scryfall code")
	assert.Equal(t, "T1X", found.ExpansionCode, "ExpansionCode must be the seventeenlands_code value")
}

// TestGetActiveSets_FallsBackToCodeWhenNull_Integration verifies that when
// seventeenlands_code IS NULL, GetActiveSets returns ExpansionCode == Code
// (COALESCE fallback).
func TestGetActiveSets_FallsBackToCodeWhenNull_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	// Seed a test set with NULL seventeenlands_code.
	_, err = pool.Exec(ctx, `
		INSERT INTO sets (code, name, released_at, set_type, card_count, is_draft_active, seventeenlands_code, last_updated)
		VALUES ('_t2', 'Integration Test Set 2', '2024-01-01', 'expansion', 100, TRUE, NULL, NOW())
		ON CONFLICT (code) DO UPDATE SET
			is_draft_active     = TRUE,
			seventeenlands_code = NULL,
			last_updated        = NOW()
	`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM sets WHERE code = '_t2'") })

	store := datasets.NewPostgresStore(pool)
	sets, err := store.GetActiveSets(ctx)
	require.NoError(t, err)

	var found *datasets.SyncSet
	for i := range sets {
		if sets[i].Code == "_t2" {
			found = &sets[i]
			break
		}
	}
	require.NotNil(t, found, "seeded set _t2 must appear in GetActiveSets result")
	assert.Equal(t, "_t2", found.Code, "Code must be the Scryfall code")
	assert.Equal(t, "_t2", found.ExpansionCode,
		"ExpansionCode must fall back to Code when seventeenlands_code IS NULL")
}

// TestPostgresStore_UpsertRatings_ZeroFetchedAt_Integration verifies the defensive fallback:
// when FetchedAt is zero, UpsertRatings must substitute time.Now() so that cached_at in
// Postgres is never 0001-01-01 (which would make the BFF staleness check always fire
// X-Cache-Degraded: true).
func TestPostgresStore_UpsertRatings_ZeroFetchedAt_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	store := datasets.NewPostgresStore(pool)

	before := time.Now().UTC().Add(-time.Second)

	// Intentionally omit FetchedAt (zero value) — store must substitute time.Now().
	ratings := draftdata.SetRatings{
		SetCode:     "ZFT",
		DraftFormat: "PremierDraft",
		// FetchedAt intentionally zero
		Cards: []seventeenlands.CardRating{
			{MtgaID: 88801, Name: "Zero Fetch Card", ALSA: 5.0, GIHWR: 0.50, SeenCount: 100},
		},
	}

	require.NoError(t, store.UpsertRatings(ctx, ratings))

	var cachedAt time.Time
	err = pool.QueryRow(
		ctx,
		`SELECT cached_at FROM draft_card_ratings WHERE set_code = 'ZFT' AND draft_format = 'PremierDraft' AND arena_id = 88801`,
	).Scan(&cachedAt)
	require.NoError(t, err, "row must exist after upsert")

	// cached_at must be a real timestamp — not the zero value 0001-01-01.
	assert.False(t, cachedAt.IsZero(), "cached_at must not be zero — defensive fallback must have fired")
	assert.True(t, cachedAt.After(before),
		"cached_at (%v) must be after the time before the upsert (%v)", cachedAt, before)

	// Cleanup
	_, _ = pool.Exec(ctx, "DELETE FROM draft_card_ratings WHERE set_code = 'ZFT'")
}

// TestPostgresStore_UpsertColorRatings_Integration verifies that UpsertColorRatings
// writes rows into draft_color_ratings, replaces them on a second call (DELETE+INSERT
// semantics), and skips entries with an empty short_name.
//
// Root-cause context: migration 000057 granted only SELECT, INSERT, UPDATE on
// draft_color_ratings to mtga_sync — missing DELETE. UpsertColorRatings wraps a
// DELETE + batch INSERT in a single transaction, so every invocation failed with an
// insufficient_privilege error and the table was permanently empty. Migration 000125
// adds the missing GRANT DELETE ON draft_color_ratings TO mtga_sync.
func TestPostgresStore_UpsertColorRatings_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	const setCode = "CRI"
	const format = "PremierDraft"

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM draft_color_ratings WHERE set_code = $1", setCode)
	})

	store := datasets.NewPostgresStore(pool)

	// --- First upsert: two valid rows + one empty-short_name row (must be skipped). ---
	first := []seventeenlands.ColorRating{
		{ShortName: "WU", ColorName: "Azorius", Wins: 2900, Games: 5000, IsSummary: false},
		{ShortName: "BG", ColorName: "Golgari", Wins: 1600, Games: 3200, IsSummary: false},
		{ShortName: "", ColorName: "Ignored", Wins: 100, Games: 200, IsSummary: false}, // must be skipped
	}
	require.NoError(t, store.UpsertColorRatings(ctx, setCode, format, first),
		"UpsertColorRatings must succeed on first call")

	var countAfterFirst int
	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM draft_color_ratings WHERE set_code = $1 AND draft_format = $2`,
		setCode, format,
	).Scan(&countAfterFirst)
	require.NoError(t, err)
	assert.Equal(t, 2, countAfterFirst,
		"two non-empty-short_name rows must be written; empty short_name row must be skipped")

	// Verify win_rate is stored as the computed ratio, not raw wins.
	var winRate float64
	err = pool.QueryRow(
		ctx,
		`SELECT win_rate FROM draft_color_ratings WHERE set_code = $1 AND draft_format = $2 AND color_combination = 'WU'`,
		setCode, format,
	).Scan(&winRate)
	require.NoError(t, err)
	assert.InDelta(t, 0.58, winRate, 0.001,
		"win_rate must be stored as Wins/Games (2900/5000 = 0.58)")

	// --- Second upsert: DELETE+INSERT semantics — only the new row should remain. ---
	second := []seventeenlands.ColorRating{
		{ShortName: "RG", ColorName: "Gruul", Wins: 3000, Games: 5000, IsSummary: false},
	}
	require.NoError(t, store.UpsertColorRatings(ctx, setCode, format, second),
		"UpsertColorRatings must succeed on second call (requires DELETE privilege on draft_color_ratings)")

	var countAfterSecond int
	err = pool.QueryRow(
		ctx,
		`SELECT COUNT(*) FROM draft_color_ratings WHERE set_code = $1 AND draft_format = $2`,
		setCode, format,
	).Scan(&countAfterSecond)
	require.NoError(t, err)
	assert.Equal(t, 1, countAfterSecond,
		"second UpsertColorRatings must replace all rows from first call (DELETE+INSERT semantics)")

	var exists bool
	err = pool.QueryRow(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM draft_color_ratings WHERE set_code = $1 AND draft_format = $2 AND color_combination = 'RG')`,
		setCode, format,
	).Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "only the row from the second call (RG) must remain")
}

// TestPostgresStore_UpsertColorRatings_GamesPlayed_Integration verifies that the
// games_played column is persisted alongside win_rate.
func TestPostgresStore_UpsertColorRatings_GamesPlayed_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	const setCode = "GPI"
	const format = "QuickDraft"

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM draft_color_ratings WHERE set_code = $1", setCode)
	})

	store := datasets.NewPostgresStore(pool)

	ratings := []seventeenlands.ColorRating{
		{ShortName: "WB", ColorName: "Orzhov", Wins: 480, Games: 800, IsSummary: false},
	}
	require.NoError(t, store.UpsertColorRatings(ctx, setCode, format, ratings))

	var gamesPlayed int
	err = pool.QueryRow(
		ctx,
		`SELECT games_played FROM draft_color_ratings WHERE set_code = $1 AND draft_format = $2 AND color_combination = 'WB'`,
		setCode, format,
	).Scan(&gamesPlayed)
	require.NoError(t, err)
	assert.Equal(t, 800, gamesPlayed, "games_played must be persisted from ColorRating.Games")
}

func intPtrTest(v int) *int { return &v }

// TestPostgresStore_UpsertSetCardStubs_WritesRows_Integration verifies that
// UpsertSetCardStubs inserts (arena_id, name, set_code, arena_id_source) rows
// into set_cards and that the source column is set to "17lands".
func TestPostgresStore_UpsertSetCardStubs_WritesRows_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	store := datasets.NewPostgresStore(pool)

	stubs := []datasets.SetCardStub{
		{ArenaID: 9900001, Name: "Stub Card Alpha", SetCode: "_st", Source: "17lands"},
		{ArenaID: 9900002, Name: "Stub Card Beta", SetCode: "_st", Source: "17lands"},
	}

	require.NoError(t, store.UpsertSetCardStubs(ctx, stubs))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM set_cards WHERE set_code = '_st' AND arena_id IN ('9900001', '9900002')`)
	})

	for _, stub := range stubs {
		var name, source string
		err := pool.QueryRow(
			ctx,
			`SELECT name, arena_id_source FROM set_cards WHERE set_code = $1 AND arena_id = $2`,
			stub.SetCode,
			fmt.Sprintf("%d", stub.ArenaID),
		).Scan(&name, &source)
		require.NoError(t, err, "stub row must exist for arena_id=%d", stub.ArenaID)
		assert.Equal(t, stub.Name, name)
		assert.Equal(t, "17lands", source, "arena_id_source must be '17lands' for stub rows")
	}
}

// TestPostgresStore_UpsertSetCardStubs_DoesNotOverwriteScryfall_Integration verifies
// the DO NOTHING conflict behaviour: a 17lands stub must not overwrite an existing
// Scryfall-sourced row (which carries full metadata including image URLs, rarity, etc.).
func TestPostgresStore_UpsertSetCardStubs_DoesNotOverwriteScryfall_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	store := datasets.NewPostgresStore(pool)

	// Write a Scryfall-sourced row first (via UpsertSetCards, which uses DO UPDATE).
	scryfallCards := []scryfall.ScryfallCard{
		{
			ScryfallID: "sc-stub-test-001",
			ArenaID:    intPtrTest(9900101),
			Name:       "Scryfall Card",
			SetCode:    "_st",
			Rarity:     "rare",
		},
	}
	require.NoError(t, store.UpsertSetCards(ctx, scryfallCards))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM set_cards WHERE set_code = '_st' AND arena_id = '9900101'`)
	})

	// Now try to write a stub for the same (set_code, arena_id) — must be silently ignored.
	stubs := []datasets.SetCardStub{
		{ArenaID: 9900101, Name: "17lands Overwrite Attempt", SetCode: "_st", Source: "17lands"},
	}
	require.NoError(t, store.UpsertSetCardStubs(ctx, stubs))

	// The Scryfall row must be unchanged.
	var name, rarity string
	err = pool.QueryRow(
		ctx,
		`SELECT name, COALESCE(rarity, '') FROM set_cards WHERE set_code = '_st' AND arena_id = '9900101'`,
	).Scan(&name, &rarity)
	require.NoError(t, err)
	assert.Equal(t, "Scryfall Card", name,
		"name must remain 'Scryfall Card' — 17lands stub must not overwrite Scryfall row")
	assert.Equal(t, "rare", rarity,
		"rarity must remain 'rare' — Scryfall metadata must be preserved")
}

// TestPostgresStore_UpsertSetCardStubs_Idempotent_Integration verifies that calling
// UpsertSetCardStubs twice with the same stubs is idempotent — no error, no duplicate rows.
func TestPostgresStore_UpsertSetCardStubs_Idempotent_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	store := datasets.NewPostgresStore(pool)

	stubs := []datasets.SetCardStub{
		{ArenaID: 9900201, Name: "Idempotent Card", SetCode: "_st", Source: "17lands"},
	}

	require.NoError(t, store.UpsertSetCardStubs(ctx, stubs), "first call must succeed")
	require.NoError(t, store.UpsertSetCardStubs(ctx, stubs), "second call must succeed (idempotent)")
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM set_cards WHERE set_code = '_st' AND arena_id = '9900201'`)
	})

	var count int
	err = pool.QueryRow(
		ctx,
		`SELECT count(*) FROM set_cards WHERE set_code = '_st' AND arena_id = '9900201'`,
	).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "idempotent call must produce exactly one row, not two")
}

// TestPostgresStore_UpsertSetCards_Integration verifies that UpsertSetCards writes
// per-set card entries to set_cards with arena_id stored as TEXT, that a second
// call upserts (not appends) the rows, and that image_url_small and image_url_art
// are written correctly from the ImageURIs map.
func TestPostgresStore_UpsertSetCards_Integration(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	require.NoError(t, err)
	defer pool.Close()

	store := datasets.NewPostgresStore(pool)

	cards := []scryfall.ScryfallCard{
		{
			ScryfallID: "sc-001",
			ArenaID:    intPtrTest(888001),
			Name:       "Set Card Alpha",
			SetCode:    "tst",
			Rarity:     "uncommon",
			Colors:     []string{"R"},
			ImageURIs: map[string]any{
				"normal":   "https://cards.scryfall.io/normal/front/sc-001.jpg",
				"small":    "https://cards.scryfall.io/small/front/sc-001.jpg",
				"art_crop": "https://cards.scryfall.io/art_crop/front/sc-001.jpg",
			},
		},
		{
			ScryfallID: "sc-002",
			ArenaID:    intPtrTest(888002),
			Name:       "Set Card Beta",
			SetCode:    "tst",
			Rarity:     "mythic",
			Colors:     []string{"G"},
			// No ImageURIs — image columns must be empty string.
		},
	}

	require.NoError(t, store.UpsertSetCards(ctx, cards))

	// Verify both rows were written with arena_id as TEXT.
	for _, c := range cards {
		var name, arenaIDText string
		err := pool.QueryRow(
			ctx,
			`SELECT arena_id, name FROM set_cards WHERE set_code = $1 AND arena_id = $2`,
			c.SetCode,
			fmt.Sprintf("%d", *c.ArenaID),
		).Scan(&arenaIDText, &name)
		require.NoError(t, err, "set_card set_code=%s arena_id=%d must exist", c.SetCode, *c.ArenaID)
		assert.Equal(t, fmt.Sprintf("%d", *c.ArenaID), arenaIDText,
			"set_cards.arena_id must be stored as TEXT")
		assert.Equal(t, c.Name, name)
	}

	// Verify image_url_small and image_url_art were written for the first card.
	var imageURLSmall, imageURLArt string
	err = pool.QueryRow(
		ctx,
		`SELECT COALESCE(image_url_small, ''), COALESCE(image_url_art, '') FROM set_cards WHERE set_code = 'tst' AND arena_id = '888001'`,
	).Scan(&imageURLSmall, &imageURLArt)
	require.NoError(t, err)
	assert.Equal(t, "https://cards.scryfall.io/small/front/sc-001.jpg", imageURLSmall,
		"image_url_small must be written from ImageURIs[\"small\"]")
	assert.Equal(t, "https://cards.scryfall.io/art_crop/front/sc-001.jpg", imageURLArt,
		"image_url_art must be written from ImageURIs[\"art_crop\"]")

	// Verify that the second card (no ImageURIs) stored empty/null image cols.
	var imageURLSmall2, imageURLArt2 string
	err = pool.QueryRow(
		ctx,
		`SELECT COALESCE(image_url_small, ''), COALESCE(image_url_art, '') FROM set_cards WHERE set_code = 'tst' AND arena_id = '888002'`,
	).Scan(&imageURLSmall2, &imageURLArt2)
	require.NoError(t, err)
	assert.Empty(t, imageURLSmall2, "image_url_small must be empty when ImageURIs is nil")
	assert.Empty(t, imageURLArt2, "image_url_art must be empty when ImageURIs is nil")

	// Verify ON CONFLICT upsert: update name and re-upsert.
	updated := []scryfall.ScryfallCard{
		{
			ScryfallID: "sc-001",
			ArenaID:    intPtrTest(888001),
			Name:       "Set Card Alpha Updated",
			SetCode:    "tst",
			Rarity:     "uncommon",
			Colors:     []string{"R"},
			ImageURIs: map[string]any{
				"normal":   "https://cards.scryfall.io/normal/front/sc-001-v2.jpg",
				"small":    "https://cards.scryfall.io/small/front/sc-001-v2.jpg",
				"art_crop": "https://cards.scryfall.io/art_crop/front/sc-001-v2.jpg",
			},
		},
	}
	require.NoError(t, store.UpsertSetCards(ctx, updated))

	var updatedName, updatedSmall string
	err = pool.QueryRow(
		ctx,
		`SELECT name, COALESCE(image_url_small, '') FROM set_cards WHERE set_code = 'tst' AND arena_id = '888001'`,
	).Scan(&updatedName, &updatedSmall)
	require.NoError(t, err)
	assert.Equal(t, "Set Card Alpha Updated", updatedName,
		"second UpsertSetCards call must update existing row via ON CONFLICT DO UPDATE")
	assert.Equal(t, "https://cards.scryfall.io/small/front/sc-001-v2.jpg", updatedSmall,
		"image_url_small must be updated on second upsert")

	// Cleanup
	_, _ = pool.Exec(ctx, `DELETE FROM set_cards WHERE set_code = 'tst' AND arena_id IN ('888001', '888002')`)
}
