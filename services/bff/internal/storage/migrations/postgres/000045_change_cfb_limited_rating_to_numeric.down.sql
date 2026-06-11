-- Revert CFB limited_rating from REAL back to TEXT (PostgreSQL)
-- Guard: cfb_ratings is dropped by 000054.down, which runs before this migration
-- in descending order. Skip if the table is absent.
ALTER TABLE IF EXISTS cfb_ratings
    ALTER COLUMN limited_rating TYPE TEXT
    USING CASE
        WHEN limited_rating IS NULL   THEN NULL
        WHEN limited_rating >= 4.75   THEN 'A+'
        WHEN limited_rating >= 4.25   THEN 'A'
        WHEN limited_rating >= 3.75   THEN 'A-'
        WHEN limited_rating >= 3.25   THEN 'B+'
        WHEN limited_rating >= 2.75   THEN 'B'
        WHEN limited_rating >= 2.25   THEN 'B-'
        WHEN limited_rating >= 1.75   THEN 'C+'
        WHEN limited_rating >= 1.25   THEN 'C'
        WHEN limited_rating >= 0.75   THEN 'C-'
        WHEN limited_rating >= 0.25   THEN 'D'
        ELSE 'F'
    END;
