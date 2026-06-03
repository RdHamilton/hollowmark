-- Revert accounts.is_default from BOOLEAN back to INTEGER.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_name = 'accounts'
          AND column_name = 'is_default'
          AND data_type = 'boolean'
    ) THEN
        DROP INDEX IF EXISTS idx_accounts_default;
        DROP INDEX IF EXISTS idx_accounts_is_default;

        -- Must drop the boolean DEFAULT before the type ALTER; PostgreSQL
        -- cannot auto-cast DEFAULT FALSE to integer in the same ALTER statement.
        ALTER TABLE accounts
            ALTER COLUMN is_default DROP DEFAULT;

        ALTER TABLE accounts
            ALTER COLUMN is_default TYPE INTEGER
            USING (is_default::int);

        ALTER TABLE accounts
            ALTER COLUMN is_default SET DEFAULT 0;

        -- Restore the CHECK(is_default IN (0, 1)) constraint from migration 000002.
        ALTER TABLE accounts
            ADD CONSTRAINT accounts_is_default_check CHECK (is_default = ANY (ARRAY[0, 1]));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_accounts_is_default ON accounts(is_default);
CREATE UNIQUE INDEX IF NOT EXISTS idx_accounts_default ON accounts(is_default)
    WHERE is_default = 1;
