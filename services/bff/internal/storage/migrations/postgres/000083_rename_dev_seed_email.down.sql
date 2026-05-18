-- Revert the dev seed user email back to the original MTGA-Companion domain.
UPDATE users SET email = 'dev@mtga-companion.local' WHERE email = 'dev@vaultmtg.local';
