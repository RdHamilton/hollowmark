package repository

import (
	"context"
	"database/sql"
	"time"
)

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
	defer rows.Close()

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
