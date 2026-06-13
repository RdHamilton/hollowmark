-- Down migration for 000127: Remove the SOS row seeded by the up migration.
-- If SOS was already present before the up migration ran (e.g. it was inserted
-- manually), this down migration will still DELETE it — acceptable since the
-- down path is only exercised in development/rollback scenarios.
DELETE FROM sets WHERE code = 'sos';
