-- sync_hashes stores the last-seen content hash for each sync key
-- (e.g. a set code) so the Lambda handler can skip unchanged payloads.
-- Owned by the mtga_sync role (same scope as card ratings tables).
CREATE TABLE IF NOT EXISTS sync_hashes (
    key        TEXT PRIMARY KEY,
    hash       TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE sync_hashes TO mtga_sync;
