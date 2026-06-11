-- Revert Standard set fixes (partial rollback)
-- Inserted sets (ECL, BIG, AED) are NOT deleted to avoid data loss.
-- Guard: sets table may already be absent if 000054.down ran before this migration
-- (descending order: 000054.down runs at position 54, before position 46).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'sets') THEN
        UPDATE sets SET is_standard_legal = TRUE, rotation_date = '2028-01-01' WHERE code = 'TLA';
        UPDATE sets SET is_standard_legal = FALSE WHERE code = 'TDM';
        UPDATE sets SET is_standard_legal = FALSE WHERE code = 'BIG';
    END IF;
END
$$;
