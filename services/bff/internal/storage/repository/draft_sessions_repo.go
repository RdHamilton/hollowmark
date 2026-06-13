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
	ID         string
	SetCode    string
	DraftType  string
	StartTime  time.Time
	EndTime    *time.Time
	Wins       int
	Losses     int
	FormatType string
	IsTrophy   bool
}

// DraftSessionsRepository provides read access to the draft_sessions table.
type DraftSessionsRepository struct {
	db DB
}

// NewDraftSessionsRepository returns a DraftSessionsRepository backed by db.
func NewDraftSessionsRepository(db DB) *DraftSessionsRepository {
	return &DraftSessionsRepository{db: db}
}

// ListByAccountID returns a page of completed draft sessions for the given
// account, ordered by start_time DESC.  Only sessions with status='completed'
// are returned — in-progress sessions are excluded so the Draft History view
// never surfaces a draft that is still underway (#1419).  setCode may be
// empty to return all sets.  wins/losses are computed via JOIN against
// draft_match_results in a single query.
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
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses,
			       ds.format_type, ds.is_trophy
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1 AND ds.set_code = $2 AND ds.status = 'completed'
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time, ds.format_type, ds.is_trophy
			ORDER BY ds.start_time DESC
			LIMIT $3 OFFSET $4`

		rows, err = r.db.QueryContext(ctx, q, accountID, setCode, limit, offset)
	} else {
		const q = `
			SELECT ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time,
			       COALESCE(SUM(CASE WHEN dmr.result = 'win' THEN 1 ELSE 0 END), 0)  AS wins,
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses,
			       ds.format_type, ds.is_trophy
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1 AND ds.status = 'completed'
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time, ds.format_type, ds.is_trophy
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
			&s.Wins, &s.Losses, &s.FormatType, &s.IsTrophy,
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
		const q = `SELECT COUNT(*) FROM draft_sessions WHERE account_id = $1 AND set_code = $2 AND status = 'completed'`
		row := r.db.QueryRowContext(ctx, q, accountID, setCode)

		if err := row.Scan(&total); err != nil {
			return 0, err
		}
	} else {
		const q = `SELECT COUNT(*) FROM draft_sessions WHERE account_id = $1 AND status = 'completed'`
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
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses,
			       ds.format_type, ds.is_trophy
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1
			  AND ds.set_code = $2
			  AND ds.status = 'completed'
			  AND (ds.start_time < $3 OR (ds.start_time = $3 AND ds.id < $4))
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time, ds.format_type, ds.is_trophy
			ORDER BY ds.start_time DESC, ds.id DESC
			LIMIT $5`

		rows, err = r.db.QueryContext(ctx, q, accountID, setCode, *cursorTS, cursorID, fetch)

	case setCode != "" && cursorTS == nil:
		const q = `
			SELECT ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time,
			       COALESCE(SUM(CASE WHEN dmr.result = 'win' THEN 1 ELSE 0 END), 0)  AS wins,
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses,
			       ds.format_type, ds.is_trophy
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1 AND ds.set_code = $2 AND ds.status = 'completed'
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time, ds.format_type, ds.is_trophy
			ORDER BY ds.start_time DESC, ds.id DESC
			LIMIT $3`

		rows, err = r.db.QueryContext(ctx, q, accountID, setCode, fetch)

	case setCode == "" && cursorTS != nil:
		const q = `
			SELECT ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time,
			       COALESCE(SUM(CASE WHEN dmr.result = 'win' THEN 1 ELSE 0 END), 0)  AS wins,
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses,
			       ds.format_type, ds.is_trophy
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1
			  AND ds.status = 'completed'
			  AND (ds.start_time < $2 OR (ds.start_time = $2 AND ds.id < $3))
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time, ds.format_type, ds.is_trophy
			ORDER BY ds.start_time DESC, ds.id DESC
			LIMIT $4`

		rows, err = r.db.QueryContext(ctx, q, accountID, *cursorTS, cursorID, fetch)

	default:
		const q = `
			SELECT ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time,
			       COALESCE(SUM(CASE WHEN dmr.result = 'win' THEN 1 ELSE 0 END), 0)  AS wins,
			       COALESCE(SUM(CASE WHEN dmr.result = 'loss' THEN 1 ELSE 0 END), 0) AS losses,
			       ds.format_type, ds.is_trophy
			FROM draft_sessions ds
			LEFT JOIN draft_match_results dmr ON dmr.session_id = ds.id
			WHERE ds.account_id = $1 AND ds.status = 'completed'
			GROUP BY ds.id, ds.set_code, ds.draft_type, ds.start_time, ds.end_time, ds.format_type, ds.is_trophy
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
			&s.Wins, &s.Losses, &s.FormatType, &s.IsTrophy,
		); err != nil {
			return nil, err
		}

		sessions = append(sessions, s)
	}

	return sessions, rows.Err()
}

// UpsertDraftSession inserts or updates a draft_sessions row.
// Used by the projection worker.
//
// format_type: DraftSessionUpsert.FormatType may be empty for partial upserts
// (e.g. draft.pick events that only carry session_id + total_picks). When
// empty, the existing column value is preserved via COALESCE on conflict.
// On INSERT, the column default ('quick_draft') applies when FormatType is
// empty.
//
// is_trophy: DraftSessionUpsert.IsTrophy may be nil for partial upserts.
// When nil, the existing column value is preserved. A session that achieves
// trophy (is_trophy = TRUE) is never retroactively cleared by a subsequent
// partial upsert.
func (r *DraftSessionsRepository) UpsertDraftSession(ctx context.Context, s DraftSessionUpsert) error {
	// Pass FormatType as NULL when empty so the ON CONFLICT COALESCE guard
	// retains the existing value. On INSERT, COALESCE(NULL, 'quick_draft')
	// applies the column default.
	var formatTypeParam *string
	if s.FormatType != "" {
		ft := s.FormatType
		formatTypeParam = &ft
	}

	// is_trophy: nil → NULL so COALESCE retains existing. TRUE is sticky — once
	// set it is never cleared by a partial upsert.
	var isTrophyParam *bool
	if s.IsTrophy != nil && *s.IsTrophy {
		t := true
		isTrophyParam = &t
	}

	const q = `
		INSERT INTO draft_sessions (
			id, account_id, event_name, set_code, draft_type, start_time, end_time,
			status, total_picks, format_type, is_trophy, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,COALESCE($10,'quick_draft'),COALESCE($11,FALSE),NOW())
		ON CONFLICT (id) DO UPDATE
			SET end_time    = COALESCE(EXCLUDED.end_time, draft_sessions.end_time),
			    total_picks = draft_sessions.total_picks + EXCLUDED.total_picks,
			    status      = EXCLUDED.status,
			    format_type = COALESCE($10, draft_sessions.format_type),
			    is_trophy   = CASE WHEN $11 IS TRUE THEN TRUE ELSE draft_sessions.is_trophy END,
			    updated_at  = NOW()`

	_, err := r.db.ExecContext(
		ctx, q,
		s.ID, s.AccountID, s.EventName, s.SetCode, s.DraftType,
		s.StartTime, s.EndTime, s.Status, s.TotalPicks,
		formatTypeParam, isTrophyParam,
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
	// FormatType is the normalised draft format derived from CourseName/EventName
	// at projection time. Values: quick_draft | premier_draft |
	// traditional_draft | contender_draft. Empty string means "do not update"
	// (the column retains its existing value via COALESCE).
	FormatType string
	// IsTrophy, when non-nil, sets draft_sessions.is_trophy. Nil means "do not
	// update" (the column retains its existing value via COALESCE).
	IsTrophy *bool
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
		  AND start_time >= $3::timestamptz - INTERVAL '48 hours'
		  AND start_time <= $3::timestamptz
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

// GetWinsForSession returns the number of 'win' rows in draft_match_results
// for the given sessionID. Used by the projection worker to compute is_trophy
// when projecting a draft.completed event.
func (r *DraftSessionsRepository) GetWinsForSession(ctx context.Context, sessionID string) (int, error) {
	const q = `SELECT COUNT(*) FROM draft_match_results WHERE session_id = $1 AND result = 'win'`
	var n int
	if err := r.db.QueryRowContext(ctx, q, sessionID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
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

// PickCardIDsForSession returns every card_id stored in draft_picks for the
// given session, ordered by (pack_number, pick_number). card_id is TEXT in
// the schema (arena ID string); callers that need int values must parse.
// Returns an empty slice (not an error) when no picks exist yet.
func (r *DraftSessionsRepository) PickCardIDsForSession(ctx context.Context, sessionID string) ([]string, error) {
	const q = `
		SELECT card_id
		FROM draft_picks
		WHERE session_id = $1
		ORDER BY pack_number, pick_number`

	rows, err := r.db.QueryContext(ctx, q, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var cardID string
		if err := rows.Scan(&cardID); err != nil {
			return nil, err
		}
		out = append(out, cardID)
	}
	return out, rows.Err()
}
