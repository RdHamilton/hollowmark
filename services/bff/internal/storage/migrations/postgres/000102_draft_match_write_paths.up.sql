-- Migration 000102: draft session write-path schema changes (ADR-051)
--
-- Pre-deploy check: verify SELECT COUNT(*) FROM decks WHERE draft_event_id IS NOT NULL = 0
-- before running. If non-zero, the FK add in step 2 will fail on rows pointing
-- to non-existent draft sessions — investigate before proceeding.
--
-- 0. Add format_type and is_trophy to draft_sessions (Prof review — must ship
--    in this migration so the columns are available for the backfill step and
--    all new projections from day one).
--
--    format_type: derived from CourseName (event_name) at projection time;
--    avoids parsing event_name at query time on every list call.
--    Values: quick_draft | premier_draft | traditional_draft | contender_draft
--
--    is_trophy: true when the session completes with wins >= 7;
--    avoids re-joining draft_match_results to compute this on every list call.
ALTER TABLE draft_sessions
    ADD COLUMN IF NOT EXISTS format_type TEXT NOT NULL DEFAULT 'quick_draft';

ALTER TABLE draft_sessions
    ADD COLUMN IF NOT EXISTS is_trophy BOOLEAN NOT NULL DEFAULT FALSE;

-- 1. Add draft_session_id to matches (nullable; REFERENCES draft_sessions so the
--    FK is enforced when set, but NOT NULL is not required — non-draft matches
--    always have NULL here).
ALTER TABLE matches
    ADD COLUMN IF NOT EXISTS draft_session_id TEXT
        REFERENCES draft_sessions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_matches_draft_session_id
    ON matches(draft_session_id) WHERE draft_session_id IS NOT NULL;

-- 2. Rename decks.draft_event_id → decks.draft_session_id and add FK.
--    The column is currently always NULL (the write path never set it), so no
--    data migration is needed. The rename is safe.
ALTER TABLE decks
    RENAME COLUMN draft_event_id TO draft_session_id;

ALTER TABLE decks
    ADD CONSTRAINT fk_decks_draft_session
        FOREIGN KEY (draft_session_id) REFERENCES draft_sessions(id)
        ON DELETE SET NULL;

-- Rename the existing index to match the new column name.
DROP INDEX IF EXISTS idx_decks_draft_event_id;

CREATE INDEX IF NOT EXISTS idx_decks_draft_session_id
    ON decks(draft_session_id) WHERE draft_session_id IS NOT NULL;
