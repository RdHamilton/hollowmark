// wildcard_gap_repo.go — ADR-045 §2 Phase 1 gap-analysis query
//
// WildcardGapRepository executes the four-table join that forms the core of
// the wildcard advisor feature. It is intentionally separate from the existing
// per-table repositories (inventory_repo, card_inventory_repo, meta_repo,
// draft_ratings_repo) because the query crosses four tables and is specific to
// the advisor use case — adding it to any single table's repo would obscure
// ownership.

package repository

import (
	"context"
)

// WildcardGapRow is one output row from the ADR-045 §2 Phase 1 gap-analysis
// query. It represents a single (archetype, card) pair annotated with the
// player's ownership data and the 17Lands GIHWR rating.
type WildcardGapRow struct {
	ArchetypeID    int64
	ArchetypeName  string
	Format         string
	Tier           *string
	SourceURL      *string
	CardName       string
	CopiesRequired int
	CopiesOwned    int
	CopiesMissing  int
	Rarity         string
	ArenaID        int
	GIHWR          *float64
}

// WildcardGapRepository runs the ADR-045 §2 gap-analysis join.
type WildcardGapRepository struct {
	db DB
}

// NewWildcardGapRepository returns a WildcardGapRepository backed by db.
func NewWildcardGapRepository(db DB) *WildcardGapRepository {
	return &WildcardGapRepository{db: db}
}

// GetWildcardGapRows executes the ADR-045 §2 Phase 1 four-table join for the
// given account and format. It returns one row per (archetype, card) pair —
// including cards the player already owns all copies of (CopiesMissing == 0).
// The caller aggregates rows by ArchetypeID to produce per-archetype summaries.
//
// Query design:
//   - Filters archetypes by lower(format) = lower($format) and
//     last_updated > NOW() - 7 days (ADR-045 §5 staleness guard is handled
//     in the handler; this query returns all fresh rows and lets the handler
//     decide on the 503 path).
//   - Resolves card_name → arena_id + rarity via set_cards using a
//     DISTINCT ON dedup (the same card may appear in multiple sets).
//   - Joins card_inventory for the player's current copy count.
//   - Joins draft_card_ratings for the most-recently-synced GIHWR for each
//     (arena_id, PremierDraft) pair using a correlated subquery that picks
//     the row with MAX(cached_at).
//
// Index dependencies (verified via SSM on 2026-06-04):
//   - idx_mtgzone_archetype_cards_archetype (archetype_id) — EXISTS
//   - idx_set_cards_name_lower lower(name,id DESC) — added by migration 000105
//   - idx_card_inventory_account_card UNIQUE (account_id, card_id) — EXISTS
//   - idx_draft_card_ratings_arena_format (arena_id, draft_format, cached_at DESC)
//     — added by migration 000105
//
// $1 = accountID (int64), $2 = format (string)
const wildcardGapQuery = `
SELECT
    a.id            AS archetype_id,
    a.name          AS archetype_name,
    a.format,
    a.tier,
    a.source_url,
    ac.card_name,
    ac.copies       AS copies_required,
    COALESCE(ci.count, 0) AS copies_owned,
    GREATEST(ac.copies - COALESCE(ci.count, 0), 0) AS copies_missing,
    COALESCE(sc.rarity, '') AS rarity,
    COALESCE(sc.arena_id::INTEGER, 0) AS arena_id,
    dcr.gihwr
FROM mtgzone_archetypes a
JOIN mtgzone_archetype_cards ac ON ac.archetype_id = a.id
-- Resolve card_name -> arena_id + rarity via set_cards (name-based join).
-- DISTINCT ON (lower(name)) picks the row with the highest id (most recent
-- set) when the same card name appears in multiple sets.
-- idx_set_cards_name_lower (migration 000105) supports this join.
LEFT JOIN (
    SELECT DISTINCT ON (lower(name)) arena_id, rarity, name
    FROM set_cards
    ORDER BY lower(name), id DESC
) sc ON lower(sc.name) = lower(ac.card_name)
-- Player collection: how many copies owned?
-- idx_card_inventory_account_card UNIQUE (account_id, card_id) supports this.
LEFT JOIN card_inventory ci
    ON ci.account_id = $1
   AND ci.card_id = sc.arena_id::INTEGER
-- 17Lands ratings: most-recently-synced GIHWR for PremierDraft.
-- idx_draft_card_ratings_arena_format (migration 000105) supports this.
LEFT JOIN LATERAL (
    SELECT gihwr
    FROM draft_card_ratings
    WHERE arena_id = sc.arena_id::INTEGER
      AND draft_format = 'PremierDraft'
    ORDER BY cached_at DESC
    LIMIT 1
) dcr ON true
WHERE lower(a.format) = lower($2)
  AND a.last_updated > NOW() - INTERVAL '7 days'`

// GetWildcardGapRows executes the gap-analysis query for the given account
// and format.
func (r *WildcardGapRepository) GetWildcardGapRows(ctx context.Context, accountID int64, format string) ([]WildcardGapRow, error) {
	rows, err := r.db.QueryContext(ctx, wildcardGapQuery, accountID, format)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []WildcardGapRow
	for rows.Next() {
		var row WildcardGapRow
		if err := rows.Scan(
			&row.ArchetypeID,
			&row.ArchetypeName,
			&row.Format,
			&row.Tier,
			&row.SourceURL,
			&row.CardName,
			&row.CopiesRequired,
			&row.CopiesOwned,
			&row.CopiesMissing,
			&row.Rarity,
			&row.ArenaID,
			&row.GIHWR,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = make([]WildcardGapRow, 0)
	}
	return out, nil
}

// CountCardInventory returns the count of distinct cards in the player's
// collection. Used for the sparse-collection data-quality warning (ADR-045 §4):
// fewer than 50 distinct cards triggers "collection_may_be_incomplete".
func (r *WildcardGapRepository) CountCardInventory(ctx context.Context, accountID int64) (int, error) {
	const q = `SELECT COUNT(*) FROM card_inventory WHERE account_id = $1`
	var n int
	if err := r.db.QueryRowContext(ctx, q, accountID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}
