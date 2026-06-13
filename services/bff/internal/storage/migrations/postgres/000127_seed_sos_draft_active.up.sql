-- Migration 000127: Seed Secrets of Strixhaven (SOS) into the sets table
-- and mark it draft-active so the Card-Sync Lambda includes it in its rotation.
--
-- Root cause (hollowmark-tickets#1420, Defect B1): SOS was absent from the sets
-- table entirely. GetActiveSets queries WHERE is_draft_active = TRUE — with no
-- row for 'sos', the Lambda never tried to fetch its 17Lands ratings, causing
-- draft_card_ratings to have 0 rows for SOS across ~1013 consecutive runs.
--
-- Verification: After applying this migration and running a sync cycle,
--   SELECT count(*) FROM draft_card_ratings WHERE set_code = 'sos';
-- must return a non-zero value (17Lands confirms ~341 cards for SOS).
--
-- The seventeenlands_code column is left NULL — 17Lands uses the same code
-- "SOS" as Scryfall, so COALESCE(seventeenlands_code, code) falls back to 'sos'
-- correctly for API requests.

INSERT INTO sets (code, name, released_at, set_type, card_count, is_draft_active, last_updated)
VALUES (
    'sos',
    'Secrets of Strixhaven',
    '2021-04-23',
    'masters',
    63,
    TRUE,
    NOW()
)
ON CONFLICT (code) DO UPDATE SET
    is_draft_active = TRUE,
    last_updated    = NOW();
