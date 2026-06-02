package repository

import (
	"context"
	"database/sql"
	"time"
)

// DraftMatchResultInsert holds the fields needed to write one row to
// draft_match_results. Used by the projection worker after a match.completed
// event is linked to a draft session.
type DraftMatchResultInsert struct {
	SessionID      string
	MatchID        string
	Result         string // "win" or "loss"
	OpponentColors *string
	GameWins       int
	GameLosses     int
	MatchTimestamp time.Time
}

// DraftPickInsert holds the fields needed to write one row to draft_picks.
// Used by the projection worker for each draft.pick event.
type DraftPickInsert struct {
	SessionID  string
	PackNumber int
	PickNumber int
	CardID     string
	Timestamp  time.Time
}

// DraftSessionRow is a row returned from draft_sessions for history reads.
type DraftSessionRow struct {
	ID        string
	SetCode   string
	DraftType string
	StartTime time.Time
	EndTime   *time.Time
	Wins      int
	Losses    int
}

// DraftSessionsRepository provides read access to the draft_sessions table.
type DraftSessionsRepository struct {
	db DB
}

// NewDraftSessionsRepository returns a DraftSessionsRepository backed by db.
func NewDraftSessionsRepository(db DB) *DraftSessionsRepository {
	return &DraftSessionsRepository{db: db}
}

// ListByAccountID returns a page of draft sessions for the given account,
// ordered by start_time DESC.  setCode may be empty to return all sets.
// wins/losses are computed via JOIN against draft_match_results in a single query.
func (r *DraftSessionsRepository) ListByAccountID(
	ctx context.Context,
	accountID int64,
	setCode string,
	page int,
	limit int,
) ([]DraftSessionRow, int, error) {
	offset := (page - 1) * limit

	var (
		rows *sql.Rows
		err  error
	)

	if setCode != "" {
		const q = `
			SELECT ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time,
			       COALESCE(SUM(CASE WHEN dmr.result = 'win' THEN 1 ELSE 0 END), 0)  AS wins,
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1 AND ds.set_code = $2
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time
			ORDER BY ds.start_time DESC
			LIMIT $3 OFFSET $4`

		rows, err = r.db.QueryContext(ctx, q, accountID, setCode, limit, offset)
	} else {
		const q = `
			SELECT ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time,
			       COALESCE(SUM(CASE WHEN dmr.result = 'win' THEN 1 ELSE 0 END), 0)  AS wins,
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time
			ORDER BY ds.start_time DESC
			LIMIT $2 OFFSET $3`

		rows, err = r.db.QueryContext(ctx, q, accountID, limit, offset)
	}

	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()

	var sessions []DraftSessionRow

	for rows.Next() {
		var s DraftSessionRow
		if err := rows.Scan(
			&s.ID, &s.SetCode, &s.DraftType, &s.StartTime, &s.EndTime,
			&s.Wins, &s.Losses,
		); err != nil {
			return nil, 0, err
		}

		sessions = append(sessions, s)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	total, err := r.countByAccountID(ctx, accountID, setCode)
	if err != nil {
		return nil, 0, err
	}

	return sessions, total, nil
}

func (r *DraftSessionsRepository) countByAccountID(ctx context.Context, accountID int64, setCode string) (int, error) {
	var total int

	if setCode != "" {
		const q = `SELECT COUNT(*) FROM draft_sessions WHERE account_id = $1 AND set_code = $2`
		row := r.db.QueryRowContext(ctx, q, accountID, setCode)

		if err := row.Scan(&total); err != nil {
			return 0, err
		}
	} else {
		const q = `SELECT COUNT(*) FROM draft_sessions WHERE account_id = $1`
		row := r.db.QueryRowContext(ctx, q, accountID)

		if err := row.Scan(&total); err != nil {
			return 0, err
		}
	}

	return total, nil
}

// ListByAccountIDCursorP returns up to limit+1 draft sessions using keyset
// (cursor) pagination ordered by start_time DESC, id DESC.
//
// When cursorTS is non-nil the keyset predicate
// (start_time < cursorTS OR (start_time = cursorTS AND id < cursorID)) is
// applied. setCode may be empty to return all sets.
func (r *DraftSessionsRepository) ListByAccountIDCursorP(
	ctx context.Context,
	accountID int64,
	setCode string,
	cursorTS *time.Time,
	cursorID string,
	limit int,
) ([]DraftSessionRow, error) {
	fetch := limit + 1

	var (
		rows *sql.Rows
		err  error
	)

	switch {
	case setCode != "" && cursorTS != nil:
		const q = `
			SELECT ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time,
			       COALESCE(SUM(CASE WHEN dmr.result = 'win' THEN 1 ELSE 0 END), 0)  AS wins,
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1
			  AND ds.set_code = $2
			  AND (ds.start_time < $3 OR (ds.start_time = $3 AND ds.id < $4))
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time
			ORDER BY ds.start_time DESC, ds.id DESC
			LIMIT $5`

		rows, err = r.db.QueryContext(ctx, q, accountID, setCode, *cursorTS, cursorID, fetch)

	case setCode != "" && cursorTS == nil:
		const q = `
			SELECT ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time,
			       COALESCE(SUM(CASE WHEN dmr.result = 'win' THEN 1 ELSE 0 END), 0)  AS wins,
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1 AND ds.set_code = $2
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time
			ORDER BY ds.start_time DESC, ds.id DESC
			LIMIT $3`

		rows, err = r.db.QueryContext(ctx, q, accountID, setCode, fetch)

	case setCode == "" && cursorTS != nil:
		const q = `
			SELECT ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time,
			       COALESCE(SUM(CASE WHEN dmr.result = 'win' THEN 1 ELSE 0 END), 0)  AS wins,
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1
			  AND (ds.start_time < $2 OR (ds.start_time = $2 AND ds.id < $3))
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time
			ORDER BY ds.start_time DESC, ds.id DESC
			LIMIT $4`

		rows, err = r.db.QueryContext(ctx, q, accountID, *cursorTS, cursorID, fetch)

	default:
		const q = `
			SELECT ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time,
			       COALESCE(SUM(CASE WHEN dmr.result = 'win' THEN 1 ELSE 0 END), 0)  AS wins,
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time
			ORDER BY ds.start_time DESC, ds.id DESC
			LIMIT $2`

		rows, err = r.db.QueryContext(ctx, q, accountID, fetch)
	}

	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var sessions []DraftSessionRow

	for rows.Next() {
		var s DraftSessionRow
		if err := rows.Scan(
			&s.ID, &s.SetCode, &s.DraftType, &s.StartTime, &s.EndTime,
			&s.Wins, &s.Losses,
		); err != nil {
			return nil, err
		}

		sessions = append(sessions, s)
	}

	return sessions, rows.Err()
}

// UpsertDraftSession inserts or updates a draft_sessions row.
// Used by the projection worker.
func (r *DraftSessionsRepository) UpsertDraftSession(ctx context.Context, s DraftSessionUpsert) error {
	const q = `
		INSERT INTO draft_sessions (
			id, account_id, event_name, set_code, draft_type, start_time, end_time,
			status, total_picks, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
		ON CONFLICT (id) DO UPDATE
			SET end_time    = COALESCE(EXCLUDED.end_time, draft_sessions.end_time),
			    total_picks = GREATEST(EXCLUDED.total_picks, draft_sessions.total_picks),
			    status      = EXCLUDED.status,
			    updated_at  = NOW()`

	_, err := r.db.ExecContext(
		ctx, q,
		s.ID, s.AccountID, s.EventName, s.SetCode, s.DraftType,
		s.StartTime, s.EndTime, s.Status, s.TotalPicks,
	)
	return err
}

// DraftSessionUpsert holds fields needed to write a draft_sessions row.
type DraftSessionUpsert struct {
	ID         string
	AccountID  int64
	EventName  string
	SetCode    string
	DraftType  string
	StartTime  time.Time
	EndTime    *time.Time
	Status     string
	TotalPicks int
}

// SessionExists returns true if a draft_sessions row with the given sessionID
// exists and is owned by accountID. Used by the projection worker to validate
// a daemon-supplied DraftSessionID before writing to draft_match_results.
func (r *DraftSessionsRepository) SessionExists(ctx context.Context, accountID int64, sessionID string) (bool, error) {
	const q = `SELECT EXISTS(SELECT 1 FROM draft_sessions WHERE id = $1 AND account_id = $2)`
	var exists bool
	if err := r.db.QueryRowContext(ctx, q, sessionID, accountID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// InferSessionForMatch returns the draft_sessions.id for the completed session
// that matches eventName within 48 hours before matchTime for the given
// accountID. Returns ("", nil) when zero or more than one session matches —
// ambiguous results are not guessed, the match is left without a session link.
func (r *DraftSessionsRepository) InferSessionForMatch(ctx context.Context, accountID int64, eventName string, matchTime time.Time) (string, error) {
	const q = `
		SELECT id FROM draft_sessions
		WHERE account_id = $1
		  AND event_name = $2
		  AND start_time >= $3 - INTERVAL '48 hours'
		  AND start_time <= $3
		  AND status = 'completed'
		LIMIT 2`

	rows, err := r.db.QueryContext(ctx, q, accountID, eventName, matchTime)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	if len(ids) != 1 {
		// Zero or multiple candidates — leave NULL.
		return "", nil
	}
	return ids[0], nil
}

// InsertDraftMatchResult writes one row to draft_match_results.
// Uses ON CONFLICT (session_id, match_id) DO NOTHING so re-projection is
// idempotent. A soft failure here must not abort the match projection.
func (r *DraftSessionsRepository) InsertDraftMatchResult(ctx context.Context, ins DraftMatchResultInsert) error {
	const q = `
		INSERT INTO draft_match_results
			(session_id, match_id, result, opponent_colors, game_wins, game_losses, match_timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (session_id, match_id) DO NOTHING`

	_, err := r.db.ExecContext(
		ctx, q,
		ins.SessionID, ins.MatchID, ins.Result, ins.OpponentColors,
		ins.GameWins, ins.GameLosses, ins.MatchTimestamp,
	)
	return err
}

// InsertDraftPick writes one row to draft_picks.
// Uses ON CONFLICT (session_id, pack_number, pick_number) DO NOTHING so
// replay is idempotent.
func (r *DraftSessionsRepository) InsertDraftPick(ctx context.Context, ins DraftPickInsert) error {
	const q = `
		INSERT INTO draft_picks
			(session_id, pack_number, pick_number, card_id, timestamp)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (session_id, pack_number, pick_number) DO NOTHING`

	_, err := r.db.ExecContext(
		ctx, q,
		ins.SessionID, ins.PackNumber, ins.PickNumber, ins.CardID, ins.Timestamp,
	)
	return err
}
