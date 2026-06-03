-- Migrate accounts.is_default from INTEGER to BOOLEAN.
-- Incremental-path DBs (000001→onwards) have is_default INTEGER NOT NULL DEFAULT 0.
-- Fresh-init DBs (000054 baseline) already have is_default BOOLEAN.
-- This migration normalizes both paths to BOOLEAN so index predicates and Go scan types
-- are unambiguous. Only runs on incremental-path DBs (guard below is belt-and-suspenders).

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'accounts'
          AND column_name = 'is_default'
          AND data_type = 'integer'
    ) THEN
        -- Drop the integer-predicate index before altering the column.
        -- It will be recreated with = TRUE predicate after the ALTER.
        DROP INDEX IF EXISTS idx_accounts_default;
        DROP INDEX IF EXISTS idx_accounts_is_default;

        -- Must drop the integer DEFAULT before the type ALTER; PostgreSQL
        -- cannot auto-cast DEFAULT 0 to boolean in the same ALTER statement.
        ALTER TABLE accounts
            ALTER COLUMN is_default DROP DEFAULT;

        ALTER TABLE accounts
            ALTER COLUMN is_default TYPE BOOLEAN
            USING (is_default <> 0);

        ALTER TABLE accounts
            ALTER COLUMN is_default SET DEFAULT FALSE;
    END IF;
END $$;

-- Recreate indexes unconditionally (idempotent; correct on both paths post-ALTER).
CREATE INDEX IF NOT EXISTS idx_accounts_is_default ON accounts(is_default);
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_default ON accounts(is_default)
    WHERE is_default = TRUE;
