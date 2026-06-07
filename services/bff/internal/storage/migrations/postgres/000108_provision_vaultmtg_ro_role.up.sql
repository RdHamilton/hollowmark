-- Migration 000108: provision SELECT-only vaultmtg_ro Postgres role.
--
-- PURPOSE: provide a least-privilege login role for run-prod-sql. The read-only
-- guarantee currently rests on a session-level SET SESSION CHARACTERISTICS guard;
-- this role makes writes impossible at the role layer, independent of session state.
--
-- MASTER DDL ROLE: all VaultMTG application tables in the public schema are owned
-- by the role that runs migrations (current_user at migration time):
--   PROD:    mtga_admin          (confirmed via pg_tables 2026-06-07)
--   STAGING: mtga_admin_staging  (confirmed via pg_tables 2026-06-07)
-- ALTER DEFAULT PRIVILEGES uses EXECUTE with current_user so future-table SELECT
-- coverage is durable and tied to whichever role creates application tables.
--
-- DB NAME: live prod database is 'vaultmtg' (renamed via ALTER DATABASE in #1996 §5e;
-- RDS DBName metadata field 'mtga_companion' is frozen and must NOT be used).
-- GRANT CONNECT uses current_database() via EXECUTE to avoid hardcoding the DB name,
-- making this migration portable across staging (vaultmtg_staging) and prod (vaultmtg).

DO $$
BEGIN
    -- CREATE ROLE IF NOT EXISTS is not supported in PG 15; guard with EXISTS check.
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'vaultmtg_ro') THEN
        CREATE ROLE vaultmtg_ro WITH LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION;
    END IF;

    -- GRANT CONNECT on the live database. Uses current_database() so this works
    -- on both staging (vaultmtg_staging) and prod (vaultmtg) without hardcoding.
    EXECUTE 'GRANT CONNECT ON DATABASE ' || quote_ident(current_database()) || ' TO vaultmtg_ro';

    -- DEFAULT PRIVILEGES: future tables created by the master DDL role (the role
    -- running this migration) will also grant SELECT to vaultmtg_ro.
    -- Uses current_user so the FOR ROLE anchor matches whichever role owns tables
    -- on the target database (mtga_admin on prod, mtga_admin_staging on staging).
    EXECUTE 'ALTER DEFAULT PRIVILEGES FOR ROLE ' || quote_ident(current_user) ||
            ' IN SCHEMA public GRANT SELECT ON TABLES TO vaultmtg_ro';
END
$$;

-- USAGE on the public schema (cannot use EXECUTE outside PL/pgSQL; USAGE on SCHEMA
-- is environment-independent so this is safe to state directly).
GRANT USAGE ON SCHEMA public TO vaultmtg_ro;

-- SELECT on all existing tables in the public schema.
GRANT SELECT ON ALL TABLES IN SCHEMA public TO vaultmtg_ro;
