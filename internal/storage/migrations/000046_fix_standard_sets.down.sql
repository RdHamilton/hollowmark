-- Revert Standard set fixes
-- Note: This doesn't fully restore original state, just undoes the TLA->TDM fix

UPDATE sets SET is_standard_legal = TRUE, rotation_date = '2028-01-01' WHERE code = 'TLA';
UPDATE sets SET is_standard_legal = FALSE WHERE code = 'TDM';
UPDATE sets SET is_standard_legal = FALSE WHERE code = 'BIG';
