package draftdata

import (
	"time"

	"github.com/RdHamilton/hollowmark/services/sync/internal/seventeenlands"
)

// SetRatings holds the fetched card ratings for a single MTG set.
type SetRatings struct {
	SetCode     string
	DraftFormat string
	FetchedAt   time.Time
	Cards       []seventeenlands.CardRating
}
