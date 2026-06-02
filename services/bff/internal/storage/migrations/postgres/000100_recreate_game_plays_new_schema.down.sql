-- Rollback migration 000100: restore the original game_plays action-log schema.
-- Note: data written by the v0.3.7 projection worker (per-game results) is
-- permanently lost on rollback. Only perform this rollback on a DB with no
-- production data.

DROP TABLE IF EXISTS life_change_tracking;
DROP TABLE IF EXISTS game_plays;

-- Restore original action-log game_plays (from migration 000030 / 000054).
CREATE TABLE game_plays (
    id              BIGSERIAL PRIMARY KEY,
    game_id         BIGINT NOT NULL,
    match_id        TEXT NOT NULL,
    turn_number     INTEGER NOT NULL,
    phase           TEXT,
    step            TEXT,
    player_type     TEXT NOT NULL,
    action_type     TEXT NOT NULL,
    card_id         INTEGER,
    card_name       TEXT,
    zone_from       TEXT,
    zone_to         TEXT,
    timestamp       TIMESTAMPTZ NOT NULL,
    sequence_number INTEGER NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    life_from       INTEGER,
    life_to         INTEGER,
    account_id      TEXT,
    partial         BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_game_plays_game_id    ON game_plays(game_id);
CREATE INDEX idx_game_plays_match_id   ON game_plays(match_id);

-- Restore life_change_tracking (from migration 000073).
CREATE TABLE life_change_tracking (
    id          BIGSERIAL    PRIMARY KEY,
    account_id  BIGINT       NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    game_play_id BIGINT      NOT NULL,
    team_id     INT          NOT NULL,
    life_total  INT          NOT NULL,
    delta       INT          NOT NULL,
    turn_number INT          NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);
