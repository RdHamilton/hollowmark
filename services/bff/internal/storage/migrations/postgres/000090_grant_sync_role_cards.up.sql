-- Grant mtga_sync role INSERT and UPDATE on cards and set_cards so the
-- Lambda can run UpsertCards and UpsertSetCards. Only set_cards uses a
-- BIGSERIAL PK (id) so the SEQUENCE grant is scoped to that table only.
-- cards.id is a TEXT primary key (Scryfall UUID) — no sequence needed.
GRANT INSERT, UPDATE ON cards TO mtga_sync;
GRANT INSERT, UPDATE ON set_cards TO mtga_sync;
GRANT USAGE, SELECT ON SEQUENCE set_cards_id_seq TO mtga_sync;
