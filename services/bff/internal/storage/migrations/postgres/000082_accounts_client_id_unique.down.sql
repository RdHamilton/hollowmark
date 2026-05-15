-- Revert: drop UNIQUE constraint on accounts.client_id

ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_client_id_unique;
