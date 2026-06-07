-- Migration 000108 DOWN: remove vaultmtg_ro role and all associated grants.
--
-- ORDER MATTERS: role cannot be dropped while any grants are outstanding.
-- Revoke in reverse order: default privileges → table SELECT → schema USAGE
-- → database CONNECT → then DROP ROLE.
--
-- NOTE: AC7 (REVOKE CREATE ON SCHEMA public FROM vaultmtg_app) is intentionally
-- one-way if applied. There is no inverse in this down migration — re-granting
-- CREATE to vaultmtg_app would re-introduce the DDL privilege we are removing.
-- AC7 is gated on has_schema_privilege returning true; if it ran, leave that
-- REVOKE in place even when rolling back this migration.

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'vaultmtg_ro') THEN
        -- Revoke DEFAULT PRIVILEGES first (matches FOR ROLE used in up.sql).
        EXECUTE 'ALTER DEFAULT PRIVILEGES FOR ROLE ' || quote_ident(current_user) ||
                ' IN SCHEMA public REVOKE SELECT ON TABLES FROM vaultmtg_ro';

        -- Revoke SELECT on all existing tables.
        EXECUTE 'REVOKE SELECT ON ALL TABLES IN SCHEMA public FROM vaultmtg_ro';

        -- Revoke USAGE on schema.
        EXECUTE 'REVOKE USAGE ON SCHEMA public FROM vaultmtg_ro';

        -- Revoke CONNECT on the live database.
        EXECUTE 'REVOKE CONNECT ON DATABASE ' || quote_ident(current_database()) || ' FROM vaultmtg_ro';

        -- Drop the role. Wrapped in the EXISTS guard so this is idempotent.
        DROP ROLE vaultmtg_ro;
    END IF;
END
$$;
