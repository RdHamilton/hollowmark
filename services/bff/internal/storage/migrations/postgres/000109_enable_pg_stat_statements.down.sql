-- Down: remove pg_stat_statements extension.
-- Cascades any dependent views (none expected on standard RDS).

DROP EXTENSION IF EXISTS pg_stat_statements;
