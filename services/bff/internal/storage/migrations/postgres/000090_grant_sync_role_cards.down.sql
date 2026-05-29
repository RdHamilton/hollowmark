-- Revoke the grants added in the up migration.
REVOKE INSERT, UPDATE ON cards FROM mtga_sync;
REVOKE INSERT, UPDATE ON set_cards FROM mtga_sync;
REVOKE USAGE, SELECT ON SEQUENCE set_cards_id_seq FROM mtga_sync;
