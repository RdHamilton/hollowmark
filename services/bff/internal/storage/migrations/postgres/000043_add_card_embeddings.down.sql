DROP INDEX IF EXISTS idx_similarity_cache_score;
DROP INDEX IF EXISTS idx_similarity_cache_card;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS card_similarity_cache CASCADE;
DROP INDEX IF EXISTS idx_card_embeddings_version;
DROP INDEX IF EXISTS idx_card_embeddings_name;
DROP INDEX IF EXISTS idx_card_embeddings_arena_id;
DROP TABLE IF EXISTS card_embeddings CASCADE;