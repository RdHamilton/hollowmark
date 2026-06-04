-- Migration 000105: Wildcard Advisor Indexes (ADR-045 §2)
--
-- Adds two indexes required by the four-table join in
-- GET /api/v1/recommendations/wildcards.
--
-- Index existence audit performed against staging RDS on 2026-06-04 via SSM
-- RunDocument (command d19f12da / ac6d74fe) before authoring this migration.
-- Results are documented below for each index.
--
-- NOTE: No CONCURRENTLY — golang-migrate runs each migration in a transaction,
-- and CREATE INDEX CONCURRENTLY cannot run inside a transaction block.
-- The table sizes at beta scale make non-concurrent creation acceptable:
--   draft_card_ratings: O(20k) rows  (nightly Lambda sync)
--   set_cards: O(50k) rows           (Scryfall sync)
-- Both indexes will complete in well under the 30-second migration timeout.

-- 1. idx_draft_card_ratings_arena_format
--    Supports the correlated subquery that resolves the most recent
--    set_code for a given (arena_id, draft_format) pair.
--    Audit result: ABSENT on staging (only idx_draft_card_ratings_arena_id
--    on arena_id alone exists; no composite with draft_format).
CREATE INDEX IF NOT EXISTS idx_draft_card_ratings_arena_format
    ON draft_card_ratings(arena_id, draft_format, cached_at DESC);

-- 2. idx_set_cards_name_lower
--    Supports the case-insensitive name-based join between
--    mtgzone_archetype_cards.card_name and set_cards.name.
--    Audit result: ABSENT on staging (idx_set_cards_name on name only
--    exists, but it is a plain btree on the un-lowercased column; the
--    query uses lower(sc.name) = lower(ac.card_name) which requires a
--    functional index to avoid a sequential scan).
CREATE INDEX IF NOT EXISTS idx_set_cards_name_lower
    ON set_cards(lower(name), id DESC);

-- Intentionally omitted indexes (already present on staging, noted for audit):
--
-- idx_mtgzone_archetype_cards_archetype_id (ADR-045 name):
--   ABSENT by that exact name, but idx_mtgzone_archetype_cards_archetype
--   on (archetype_id) already exists and covers all queries on that column.
--   Creating a duplicate index would waste disk and provide no benefit.
--   The query planner uses idx_mtgzone_archetype_cards_archetype.
--
-- idx_card_inventory_account_card (ADR-045 name):
--   EXISTS as a UNIQUE index on (account_id, card_id) under the same name.
--   No action needed.
