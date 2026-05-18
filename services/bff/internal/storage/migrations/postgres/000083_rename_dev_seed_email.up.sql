-- Rename the dev seed user email to reflect the VaultMTG brand.
-- This is a no-op on any database where the old email does not exist.
UPDATE users SET email = 'dev@vaultmtg.local' WHERE email = 'dev@mtga-companion.local';
