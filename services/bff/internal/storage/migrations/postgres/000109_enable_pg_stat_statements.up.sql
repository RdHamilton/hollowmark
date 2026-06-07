-- Migration 000109: Enable pg_stat_statements extension.
--
-- pg_stat_statements is pre-loaded into shared_preload_libraries on RDS
-- PostgreSQL 15 (the VaultMTG production instance) — no parameter-group
-- change or reboot is required. This migration simply activates the
-- extension in the database so the pg_stat_statements view becomes
-- queryable. Idempotent via IF NOT EXISTS.
--
-- Purpose: unblocks the /db-health-check SOP query for slowest-query
-- analysis (ticket #1042 / Ray verdict comment 4644118255).

CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
