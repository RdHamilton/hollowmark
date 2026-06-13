-- 000125_delete_precon_decks.down.sql
--
-- The deleted precon rows are non-recoverable and should not be restored
-- (they were noise introduced by a daemon bug). Down migration is intentionally
-- a no-op.
--
-- If you need to roll back to 000124 for a different reason, the deck data
-- will remain absent — that is correct behavior.

SELECT 1; -- no-op
