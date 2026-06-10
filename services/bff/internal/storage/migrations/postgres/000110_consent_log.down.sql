-- Migration 000110 rollback: drop consent_log table.
-- Indexes are dropped automatically when the table is dropped.
DROP TABLE IF EXISTS consent_log;
