-- 000125_delete_precon_decks.up.sql
--
-- Removes precon / system / event decks that were incorrectly synced from the
-- daemon before the "?=?Loc/Decks/Precon/" filter was added (#1341).  These
-- rows are identified exclusively by the MTGA localization key name prefix —
-- player deck names never start with "?=?Loc/Decks/Precon/".
--
-- deck_cards rows cascade via the existing ON DELETE CASCADE FK on
-- deck_cards.deck_id → decks.id, so no explicit deck_cards DELETE is needed.
-- On prod this removes 93 rows (all on account_id=7) with 0 associated
-- deck_cards rows (precon decks arrived via the DeckSummaries header-only path).

DELETE FROM decks WHERE name LIKE '?=?Loc/Decks/Precon/%';
