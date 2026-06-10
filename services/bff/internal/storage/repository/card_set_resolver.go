package repository

import (
	"context"
	"database/sql"
	"strconv"
)

// CardSetResolver resolves a (set_code, name) pair from the MTGA Arena export
// format to the integer arena_id used in card_inventory.card_id.
//
// set_cards has UNIQUE(set_code, arena_id) and an index on name, so the lookup
// is a single indexed scan.  Resolution is by (set_code, name) because the
// Arena export carries no arena_id field — only name, set_code, and a
// collector number that is not stored in set_cards.
type CardSetResolver struct {
	db DB
}

// NewCardSetResolver returns a CardSetResolver backed by db.
func NewCardSetResolver(db DB) *CardSetResolver {
	return &CardSetResolver{db: db}
}

// ResolveArenaID returns the integer arena_id for the card identified by
// (setCode, name).  The set_code match is case-insensitive (MTGA exports may
// differ in case from the sync-populated values).
//
// Returns (id, true, nil) when found, (0, false, nil) when no matching row
// exists, and (0, false, err) on a database error.
func (r *CardSetResolver) ResolveArenaID(ctx context.Context, setCode, name string) (int, bool, error) {
	const q = `
		SELECT arena_id
		FROM set_cards
		WHERE lower(set_code) = lower($1) AND name = $2
		LIMIT 1`

	var raw string
	err := r.db.QueryRowContext(ctx, q, setCode, name).Scan(&raw)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}

	id, err := strconv.Atoi(raw)
	if err != nil {
		// arena_id stored as TEXT but must be numeric; treat non-numeric as
		// not-found rather than surfacing an internal schema inconsistency.
		return 0, false, nil
	}
	return id, true, nil
}
