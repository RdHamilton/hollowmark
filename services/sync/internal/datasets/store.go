package datasets

import (
	"context"

	"github.com/RdHamilton/vault-mtg/services/sync/internal/draftdata"
	"github.com/RdHamilton/vault-mtg/services/sync/internal/scryfall"
	"github.com/RdHamilton/vault-mtg/services/sync/internal/seventeenlands"
)

// Store persists and retrieves draft card ratings.
type Store interface {
	// GetActiveSets returns set codes where is_draft_active = TRUE.
	// This covers all Arena-draftable sets including masters, alchemy, and
	// draft_innovation types that are not necessarily standard-legal.
	GetActiveSets(ctx context.Context) ([]string, error)
	UpsertRatings(ctx context.Context, ratings draftdata.SetRatings) error
	GetRatings(ctx context.Context, setCode, draftFormat string) (*draftdata.SetRatings, error)
	// UpsertSets upserts set metadata and marks each as draft-active.
	UpsertSets(ctx context.Context, sets []scryfall.ScryfallSet) error
	// UpsertColorRatings replaces all color-combination ratings for the given
	// set/format in draft_color_ratings.
	UpsertColorRatings(ctx context.Context, setCode, draftFormat string, ratings []seventeenlands.ColorRating) error
	// GetHash returns the stored hash for the given key (e.g. a set code).
	// Returns ("", nil) when no hash has been stored for that key.
	GetHash(ctx context.Context, key string) (string, error)
	// SetHash stores a hash for the given key, replacing any existing value.
	SetHash(ctx context.Context, key string, hash string) error
}
