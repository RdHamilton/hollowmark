-- 000126_grant_sync_color_ratings_delete.down.sql
--
-- Reverts the DELETE grant and sequence grants on draft_color_ratings
-- from the mtga_sync role. After this runs, UpsertColorRatings will fail
-- again and draft_color_ratings will not be populated.

REVOKE DELETE ON draft_color_ratings FROM mtga_sync;
REVOKE USAGE, SELECT ON SEQUENCE draft_color_ratings_id_seq FROM mtga_sync;
