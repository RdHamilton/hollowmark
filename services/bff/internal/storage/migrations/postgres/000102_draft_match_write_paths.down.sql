-- Migration 000102 down: revert draft session write-path schema changes (ADR-051)

-- Drop draft_sessions columns added in step 0 (Prof additions).
ALTER TABLE draft_sessions
    DROP COLUMN IF EXISTS is_trophy;

ALTER TABLE draft_sessions
    DROP COLUMN IF EXISTS format_type;

ALTER TABLE decks
    DROP CONSTRAINT IF EXISTS fk_decks_draft_session;

ALTER TABLE decks
    RENAME COLUMN draft_session_id TO draft_event_id;

DROP INDEX IF EXISTS idx_decks_draft_session_id;

CREATE INDEX IF NOT EXISTS idx_decks_draft_event_id
    ON decks(draft_event_id);

ALTER TABLE matches
    DROP COLUMN IF EXISTS draft_session_id;
