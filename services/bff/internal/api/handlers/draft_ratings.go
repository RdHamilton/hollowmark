package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/config"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	"github.com/go-chi/chi/v5"
)

// DraftRatingsGetter is the minimal read interface required by DraftRatingsHandler.
type DraftRatingsGetter interface {
	GetRatings(ctx context.Context, setCode, draftFormat string) (*repository.DraftRatingsResult, error)
}

// DraftRatingsHandler serves GET /api/v1/draft-ratings/{setCode}/{format}.
type DraftRatingsHandler struct {
	repo DraftRatingsGetter
	cfg  *config.Config
}

// NewDraftRatingsHandler constructs a DraftRatingsHandler.
func NewDraftRatingsHandler(repo DraftRatingsGetter, cfg *config.Config) *DraftRatingsHandler {
	return &DraftRatingsHandler{repo: repo, cfg: cfg}
}

// draftRatingsDataQuality is the typed degradation signal added to the response
// body when set_cards metadata is absent or incomplete for the requested set.
//
// AC4: The field is present (non-nil) only when degraded — omitted entirely from
// JSON when all card metadata resolved (omitempty pointer).  The Reason value
// "degraded" mirrors the X-Cache-Degraded: "true" header vocabulary so callers
// share one degradation dictionary across this endpoint.  SetCardsEmpty=true
// indicates the ADR-085 defect-4 post-wipe condition (set_cards has zero rows
// for this set_code).  UnresolvedCardCount is the number of draft_card_ratings
// rows whose arena_id had no matching set_cards entry (color/rarity unavailable).
type draftRatingsDataQuality struct {
	Reason              string `json:"reason"`
	SetCardsEmpty       bool   `json:"set_cards_empty,omitempty"`
	UnresolvedCardCount int    `json:"unresolved_card_count,omitempty"`
}

// draftRatingsResponse is the JSON envelope returned to callers.
type draftRatingsResponse struct {
	SetCode      string                   `json:"set_code"`
	DraftFormat  string                   `json:"draft_format"`
	CachedAt     time.Time                `json:"cached_at"`
	CardRatings  []cardRatingJSON         `json:"card_ratings"`
	ColorRatings []colorRatingJSON        `json:"color_ratings"`
	DataQuality  *draftRatingsDataQuality `json:"data_quality,omitempty"`
}

type cardRatingJSON struct {
	ArenaID  int      `json:"arena_id"`
	Name     string   `json:"name"`
	Color    string   `json:"color,omitempty"`
	Rarity   string   `json:"rarity,omitempty"`
	GIHWR    *float64 `json:"gihwr,omitempty"`
	OHWR     *float64 `json:"ohwr,omitempty"`
	ALSA     *float64 `json:"alsa,omitempty"`
	ATA      *float64 `json:"ata,omitempty"`
	GIHCount *int     `json:"gih_count,omitempty"`
}

type colorRatingJSON struct {
	ColorCombination string   `json:"color_combination"`
	WinRate          *float64 `json:"win_rate,omitempty"`
	GamesPlayed      *int     `json:"games_played,omitempty"`
}

// GetDraftRatings handles GET /api/v1/draft-ratings/{setCode}/{format}.
//
// Response contract (per ADR-004):
//   - 200 with body when rows exist (fresh or stale).
//   - X-Cache-Degraded: true and X-Cache-Age-Hours: <N> when stale and bypass
//     is not enabled.
//   - 404 when no rows exist for the requested set/format.
//   - Never returns 5xx due to stale data alone.
//
// Data-quality signal (AC4):
//   - data_quality field is omitted when all card metadata resolved (healthy path).
//   - data_quality.reason="degraded" when set_cards is empty for this set_code
//     (ADR-085 defect-4 post-wipe condition) or when ≥1 card_rating row has no
//     matching set_cards entry (partial-sync / arena_id drift).
//   - data_quality.set_cards_empty=true flags the systemic empty-table case.
//   - data_quality.unresolved_card_count reports per-row NULL color/rarity count.
//   - Vocabulary mirrors X-Cache-Degraded: "true" — one degradation vocabulary.
func (h *DraftRatingsHandler) GetDraftRatings(w http.ResponseWriter, r *http.Request) {
	setCode := chi.URLParam(r, "setCode")
	format := chi.URLParam(r, "format")

	if setCode == "" || format == "" {
		http.Error(w, "setCode and format are required", http.StatusBadRequest)
		return
	}

	result, err := h.repo.GetRatings(r.Context(), setCode, format)
	if err != nil {
		log.Printf("[DraftRatingsHandler] GetRatings error set=%s format=%s: %v", setCode, format, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if result == nil {
		http.Error(w, "no ratings found for the requested set/format", http.StatusNotFound)
		return
	}

	// Staleness check — bypassed when the escape-hatch flag is set.
	if !h.cfg.DraftRatingsBypassFreshnessCheck {
		ageHours := time.Since(result.CachedAt).Hours()
		if ageHours > float64(h.cfg.DraftRatingsStalenessThresholdHours) {
			rounded := int(math.Round(ageHours))
			w.Header().Set("X-Cache-Degraded", "true")
			w.Header().Set("X-Cache-Age-Hours", fmt.Sprintf("%d", rounded))
			log.Printf("[DraftRatingsHandler] degraded mode set=%s format=%s age_hours=%d threshold=%d",
				setCode, format, rounded, h.cfg.DraftRatingsStalenessThresholdHours)
		}
	}

	// Build response envelope.
	resp := draftRatingsResponse{
		SetCode:     result.SetCode,
		DraftFormat: result.DraftFormat,
		CachedAt:    result.CachedAt,
	}

	// Populate data_quality signal when set_cards metadata is absent or partial.
	// Field is omitted (nil pointer, omitempty) on the fully-healthy path.
	if result.SetCardsEmpty || result.UnresolvedCardCount > 0 {
		resp.DataQuality = &draftRatingsDataQuality{
			Reason:              "degraded",
			SetCardsEmpty:       result.SetCardsEmpty,
			UnresolvedCardCount: result.UnresolvedCardCount,
		}
	}

	for _, c := range result.CardRatings {
		resp.CardRatings = append(resp.CardRatings, cardRatingJSON{
			ArenaID:  c.ArenaID,
			Name:     c.Name,
			Color:    c.Color,
			Rarity:   c.Rarity,
			GIHWR:    c.GIHWR,
			OHWR:     c.OHWR,
			ALSA:     c.ALSA,
			ATA:      c.ATA,
			GIHCount: c.GIHCount,
		})
	}

	for _, cr := range result.ColorRatings {
		resp.ColorRatings = append(resp.ColorRatings, colorRatingJSON{
			ColorCombination: cr.ColorCombination,
			WinRate:          cr.WinRate,
			GamesPlayed:      cr.GamesPlayed,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[DraftRatingsHandler] encode response: %v", err)
	}
}
