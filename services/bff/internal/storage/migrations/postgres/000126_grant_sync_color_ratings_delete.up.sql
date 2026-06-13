-- 000126_grant_sync_color_ratings_delete.up.sql
--
-- Grants DELETE on draft_color_ratings and USAGE/SELECT on its sequence to
-- the mtga_sync role.
--
-- Root cause (hollowmark-tickets#1395 / Defect B2):
--   Migration 000057 granted only SELECT, INSERT, UPDATE on draft_color_ratings to
--   mtga_sync. The sync Lambda's UpsertColorRatings wraps a DELETE + batch INSERT in
--   a single transaction (to replace all rows for a set/format atomically). Without
--   DELETE, every invocation failed with insufficient_privilege (SQLSTATE 42501), the
--   transaction rolled back, and the table was permanently empty — causing the draft
--   advisor to have no color-combination win-rate data.
--
-- Compare: draft_card_ratings was correctly granted SELECT, INSERT, UPDATE, DELETE in
-- migration 000057 and has always populated correctly.
--
-- The sequence grant is also included here because INSERT on a BIGSERIAL table
-- requires USAGE/SELECT on the underlying sequence, and no prior migration granted
-- these on draft_color_ratings_id_seq (unlike draft_card_ratings_id_seq whose
-- sequence access was established through an earlier operational step).
--
-- This migration makes no schema change — only privilege corrections.
-- The sync Lambda will populate draft_color_ratings on its next scheduled run
-- without any Lambda code change.

GRANT DELETE ON draft_color_ratings TO mtga_sync;
GRANT USAGE, SELECT ON SEQUENCE draft_color_ratings_id_seq TO mtga_sync;
