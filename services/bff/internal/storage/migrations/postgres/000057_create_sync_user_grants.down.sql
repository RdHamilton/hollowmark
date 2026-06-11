-- Reverse: revoke all grants from mtga_sync, then drop the role.
--
-- DROP ROLE fails if the role still holds any privileges or owns any objects.
-- DROP OWNED BY revokes all in-database privileges and drops owned objects
-- for the named role within the current database.  The role itself is
-- cluster-wide, but in the throwaway-container / round-trip scope this is
-- the correct idiom (matches the up migration's per-database GRANT scope).
--
-- The existence guard prevents "role does not exist" errors on re-run after
-- a partial failure or on a DB where the up never ran.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'mtga_sync') THEN
        EXECUTE 'DROP OWNED BY mtga_sync';
    END IF;
END
$$;
DROP ROLE IF EXISTS mtga_sync;
