package repository

import (
	"context"
	"database/sql"
	"time"
)

// WildcardCounts holds the four wildcard rarity buckets read from the inventory
// table for a single account. These are the crafting currencies the wildcard
// advisor uses to determine which archetype deck-paths are affordable.
type WildcardCounts struct {
	Common   int `json:"common"`
	Uncommon int `json:"uncommon"`
	Rare     int `json:"rare"`
	Mythic   int `json:"mythic"`
}

// InventoryUpsert holds the fields written to the inventory table from an
// inventory.updated daemon event.  AccountID is the resolved accounts.id
// BIGINT FK (migration 000080 converts the column from TEXT client_id to
// BIGINT FK so every write is properly tenant-scoped).
type InventoryUpsert struct {
	AccountID          int64
	Gems               int
	Gold               int
	TotalVaultProgress int
	WildCardCommons    int
	WildCardUncommons  int
	WildCardRares      int
	WildCardMythics    int
	UpdatedAt          time.Time
}

// InventoryRepository writes player inventory snapshots to the inventory table.
type InventoryRepository struct {
	db DB
}

// NewInventoryRepository returns an InventoryRepository backed by db.
func NewInventoryRepository(db DB) *InventoryRepository {
	return &InventoryRepository{db: db}
}

// UpsertInventory writes an inventory snapshot for the given account.
// The inventory table has one row per account_id; subsequent writes update all
// tracked fields in-place.  The inventory table was originally designed as a
// singleton row (migration 000023) and later had account_id added (migration
// 000068).  The ON CONFLICT clause keys on account_id so that each account
// maintains its own current snapshot.
func (r *InventoryRepository) UpsertInventory(ctx context.Context, u InventoryUpsert) error {
	const q = `
		INSERT INTO inventory (
			account_id,
			gold,
			gems,
			wc_common,
			wc_uncommon,
			wc_rare,
			wc_mythic,
			vault_progress,
			draft_tokens,
			sealed_tokens,
			updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (account_id) DO UPDATE
			SET gold          = EXCLUDED.gold,
			    gems          = EXCLUDED.gems,
			    wc_common     = EXCLUDED.wc_common,
			    wc_uncommon   = EXCLUDED.wc_uncommon,
			    wc_rare       = EXCLUDED.wc_rare,
			    wc_mythic     = EXCLUDED.wc_mythic,
			    vault_progress = EXCLUDED.vault_progress,
			    updated_at   = EXCLUDED.updated_at`

	_, err := r.db.ExecContext(
		ctx, q,
		u.AccountID,
		u.Gold,
		u.Gems,
		u.WildCardCommons,
		u.WildCardUncommons,
		u.WildCardRares,
		u.WildCardMythics,
		u.TotalVaultProgress,
		0, // draft_tokens — not in InventoryUpdatedPayload; preserve existing via ON CONFLICT
		0, // sealed_tokens — same
		u.UpdatedAt,
	)

	return err
}

// GetWildcardCounts returns the four wildcard rarity buckets for the given
// account from the inventory table. Returns an empty WildcardCounts (all zeros)
// when no inventory row exists for the account (i.e. the daemon has never sent
// an inventory.updated event for this account yet).
func (r *InventoryRepository) GetWildcardCounts(ctx context.Context, accountID int64) (WildcardCounts, error) {
	const q = `
		SELECT
			COALESCE(wc_common, 0),
			COALESCE(wc_uncommon, 0),
			COALESCE(wc_rare, 0),
			COALESCE(wc_mythic, 0)
		FROM inventory
		WHERE account_id = $1`

	var wc WildcardCounts
	err := r.db.QueryRowContext(ctx, q, accountID).Scan(
		&wc.Common, &wc.Uncommon, &wc.Rare, &wc.Mythic,
	)
	if err == sql.ErrNoRows {
		return WildcardCounts{}, nil
	}
	if err != nil {
		return WildcardCounts{}, err
	}
	return wc, nil
}
