-- Migration 000125: add arena_id_source to set_cards.
-- Distinguishes rows seeded from Scryfall bulk-data (the default, full-metadata path)
-- from rows seeded from 17lands card ratings (stub rows, name + arena_id only).
-- The NOT NULL DEFAULT 'scryfall' is backward-compatible: all existing rows silently
-- receive 'scryfall' without a data backfill pass.
ALTER TABLE set_cards
    ADD COLUMN IF NOT EXISTS arena_id_source TEXT NOT NULL DEFAULT 'scryfall';
