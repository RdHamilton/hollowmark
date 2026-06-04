// wildcard_recommendations.go — ADR-045 §6 (v0.3.8 full implementation)
//
// GET /api/v1/recommendations/wildcards
//
// Full implementation replacing the v0.3.7 501 scaffold (#416).
// Joins inventory + card_inventory + set_cards + draft_card_ratings +
// mtgzone_archetypes + mtgzone_archetype_cards per ADR-045 §2 and ranks
// results with the composite scoring formula in ADR-045 §3.
//
// GIHWR fractional-units contract (ADR-045 / #787):
//   draft_card_ratings.gihwr is stored as a FRACTIONAL value (0.0–1.0).
//   A 62.3% win rate is stored as 0.623. The gihwr_percentile computation
//   normalises fractional values within the response set — do NOT multiply
//   by 100 before normalising.

package handlers

import (
	"context"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	bffmiddleware "github.com/RdHamilton/vault-mtg/services/bff/internal/api/middleware"
	"github.com/RdHamilton/vault-mtg/services/bff/internal/storage/repository"
)

// ─── ranking constants ────────────────────────────────────────────────────────

const (
	rankWeightCompletion        = 0.50
	rankWeightGIHWRPercentile   = 0.25
	rankWeightTier              = 0.20
	rankWeightRotationProximity = 0.05

	// maxRecommendations is the maximum number of ranked results returned.
	maxRecommendations = 10

	// cardRatingsStalenessThreshold is the age at which card ratings are
	// considered stale (ADR-045 §5).
	cardRatingsStalenessThreshold = 48 * time.Hour

	// metaStalenessThreshold is the age at which meta data triggers a 503
	// (ADR-045 §5).
	metaStalenessThreshold = 7 * 24 * time.Hour

	// sparseCollectionThreshold is the card count below which a
	// data_quality_warning is emitted (ADR-045 §4).
	sparseCollectionThreshold = 50
)

// validFormats is the allowlist for the ?format= query parameter (ADR-045 §1,
// Sarah P3 — validate before any SQL execution).
var validFormats = map[string]bool{
	"Standard": true,
	"Historic": true,
	"Alchemy":  true,
	"Explorer": true,
}

// tierScoreMap maps mtgzone_archetypes.tier TEXT values to the float64 scoring
// values defined in ADR-045 §3. All tier strings are upper-cased before lookup.
var tierScoreMap = map[string]float64{
	"S": 1.0,
	"1": 0.9,
	"2": 0.7,
	"3": 0.5,
	"4": 0.3,
}

// tierScoreDefault is used when the tier value is NULL or unrecognised.
const tierScoreDefault = 0.2

// ─── narrow repository interfaces ────────────────────────────────────────────

// InventoryReader reads wildcard counts for an account from the inventory table.
// Returns repository.WildcardCounts (defined in the repository package to avoid
// circular imports).
type InventoryReader interface {
	GetWildcardCounts(ctx context.Context, accountID int64) (repository.WildcardCounts, error)
}

// CardInventoryChecker checks whether an account has any card_inventory rows.
// Used for the zero-collection guard (ADR-045 §4 — return 409, not 200 with
// all copies_owned=0).
type CardInventoryChecker interface {
	HasCardInventory(ctx context.Context, accountID int64) (bool, error)
}

// DraftRatingsMaxCacheChecker reads MAX(cached_at) from draft_card_ratings for
// the requested format (across all set codes). Used to populate
// data_freshness.card_ratings_cached_at and to set data_freshness.stale when
// the value is older than 48h (ADR-045 §5).
//
// Returns (*time.Time, error): nil pointer when no rows exist for the format
// (Lambda has never synced for this format). The handler maps nil to an empty
// string in the JSON response and sets stale=false (no data is not the same as
// stale data).
type DraftRatingsMaxCacheChecker interface {
	GetMaxCachedAtByFormat(ctx context.Context, draftFormat string) (*time.Time, error)
}

// MetaFreshnessChecker reads MAX(last_updated) from mtgzone_archetypes for
// the requested format. Used for data_freshness.meta_last_updated and the
// 7-day staleness guard that produces 503 (ADR-045 §5).
//
// Returns (*time.Time, error): nil pointer when no archetypes exist for the
// format (bool=false from LatestArchetypeUpdate is translated to nil — do NOT
// pass the bool through). The TestMetaRepo_GetMetaLastUpdated_NoRows_ReturnsNil
// test locks this contract.
type MetaFreshnessChecker interface {
	GetMetaLastUpdated(ctx context.Context, format string) (*time.Time, error)
}

// WildcardQueryRunner executes the ADR-045 §2 Phase 1 gap-analysis query and
// returns one row per archetype-card that is missing or undercounted.
type WildcardQueryRunner interface {
	GetWildcardGapRows(ctx context.Context, accountID int64, format string) ([]repository.WildcardGapRow, error)
}

// CardCountChecker returns the total number of distinct cards in the account's
// collection. Used for the sparse-collection data-quality warning (ADR-045 §4).
type CardCountChecker interface {
	CountCardInventory(ctx context.Context, accountID int64) (int, error)
}

// ─── response shapes (complete ADR-045 JSON contract) ────────────────────────

type wildcardBudgetResponse struct {
	Common   int `json:"common"`
	Uncommon int `json:"uncommon"`
	Rare     int `json:"rare"`
	Mythic   int `json:"mythic"`
}

type wildcardDataFreshnessResponse struct {
	// CardRatingsCachedAt is the ISO-8601 timestamp of the most recent
	// draft_card_ratings row for the requested format, or empty string when
	// no ratings exist.
	CardRatingsCachedAt string `json:"card_ratings_cached_at"`
	// MetaLastUpdated is the ISO-8601 timestamp of the most recent
	// mtgzone_archetypes.last_updated for the requested format, or empty
	// string when no archetypes exist.
	MetaLastUpdated string `json:"meta_last_updated"`
	// Stale is true when card_ratings_cached_at is older than 48h (ADR-045 §5).
	Stale bool `json:"stale"`
	// StaleReason is set when Stale=true to describe why the data is stale.
	// Omitted from JSON when empty (ADR-045 §5).
	// Value: "card_ratings_older_than_48h".
	StaleReason string `json:"stale_reason,omitempty"`
}

// wildcardMissingCard represents a single card that the player needs more
// copies of to complete the archetype deck path.
type wildcardMissingCard struct {
	ArenaID      int     `json:"arena_id"`
	Name         string  `json:"name"`
	Rarity       string  `json:"rarity"`
	CopiesNeeded int     `json:"copies_needed"`
	CopiesOwned  int     `json:"copies_owned"`
	GIHWR        float64 `json:"gihwr"`
}

// wildcardRecommendationItem represents a single ranked archetype deck-path
// recommendation in the ADR-045 response.
//
// Tier is a string (e.g. "S", "1", "2", "3", "4") per Ray's ruling —
// the SPA displays the tier label verbatim. TierScore is the float64 sort key
// used by the ranking formula (ADR-045 §3); it is included so the SPA can
// perform client-side re-sorting without re-encoding the tier mapping.
//
// Valid Tier values: "S" (best), "1", "2", "3", "4", "" (unknown/null).
type wildcardRecommendationItem struct {
	ArchetypeName      string                 `json:"archetype_name"`
	Format             string                 `json:"format"`
	Tier               string                 `json:"tier"`
	TierScore          float64                `json:"tier_score"`
	CompletionScore    float64                `json:"completion_score"`
	CardsNeeded        int                    `json:"cards_needed"`
	WildcardsRequired  wildcardBudgetResponse `json:"wildcards_required"`
	Affordable         bool                   `json:"affordable"`
	ArchetypeSourceURL string                 `json:"archetype_source_url"`
	MissingCards       []wildcardMissingCard  `json:"missing_cards"`
	// rankScore is not serialised — used for sort ordering only.
	rankScore float64 `json:"-"`
}

// wildcardRecommendationsResponse is the complete ADR-045 HTTP 200 shape.
type wildcardRecommendationsResponse struct {
	WildcardBudget wildcardBudgetResponse        `json:"wildcard_budget"`
	DataFreshness  wildcardDataFreshnessResponse `json:"data_freshness"`
	// DataQualityWarning is set when collection data may be incomplete.
	// Omitted from JSON when empty (Ray's ruling: single string, omitempty).
	// Value "collection_may_be_incomplete" per ADR-045 §4.
	DataQualityWarning string `json:"data_quality_warning,omitempty"`
	// Recommendations holds the ranked list of deck-path craft targets.
	// Always a JSON array (never null) — initialised with make([], 0).
	Recommendations []wildcardRecommendationItem `json:"recommendations"`
}

// ─── handler ─────────────────────────────────────────────────────────────────

// WildcardRecommendationsHandler serves GET /api/v1/recommendations/wildcards.
type WildcardRecommendationsHandler struct {
	accounts     AccountLookup
	inventory    InventoryReader
	cardInv      CardInventoryChecker
	draftRatings DraftRatingsMaxCacheChecker
	meta         MetaFreshnessChecker
	gapQuery     WildcardQueryRunner
	cardCount    CardCountChecker
}

// NewWildcardRecommendationsHandler constructs the handler with its injected
// dependencies. gapQuery and cardCount are optional — when nil the handler
// returns empty recommendations (useful for tests that only exercise the
// freshness/budget surface without a full DB).
func NewWildcardRecommendationsHandler(
	accounts AccountLookup,
	inventory InventoryReader,
	cardInv CardInventoryChecker,
	draftRatings DraftRatingsMaxCacheChecker,
	meta MetaFreshnessChecker,
	gapQuery WildcardQueryRunner,
	cardCount CardCountChecker,
) *WildcardRecommendationsHandler {
	return &WildcardRecommendationsHandler{
		accounts:     accounts,
		inventory:    inventory,
		cardInv:      cardInv,
		draftRatings: draftRatings,
		meta:         meta,
		gapQuery:     gapQuery,
		cardCount:    cardCount,
	}
}

// GetWildcardRecommendations handles GET /api/v1/recommendations/wildcards.
//
// Query params:
//
//	?format= — narrows archetypes to a specific format (default: "Standard").
//	            Valid values: Standard, Historic, Alchemy, Explorer.
//
// Error responses:
//
//	401 — missing/invalid Clerk JWT
//	404 — no account row for this user
//	409 — card_inventory is empty (collection not synced)
//	503 — meta data is stale beyond 7 days
//	500 — database error
func (h *WildcardRecommendationsHandler) GetWildcardRecommendations(w http.ResponseWriter, r *http.Request) {
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	accountID, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[WildcardRecommendationsHandler] GetAccountIDByUserID: %v", err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSONError(w, "account not found", http.StatusNotFound)
		return
	}

	// ── format param: allowlist validation (Sarah P3) ─────────────────────────
	// Validate before any SQL execution. Invalid values default to "Standard"
	// rather than 422 — the SPA may omit the param or send a stale value.
	format := strings.TrimSpace(r.URL.Query().Get("format"))
	if format == "" || !validFormats[format] {
		format = "Standard"
	}

	// ── data freshness checks ─────────────────────────────────────────────────

	// Meta staleness: if all archetypes for the format are older than 7 days,
	// return 503 (ADR-045 §5). A nil pointer means no archetypes exist — treat
	// as a degraded 503 (no data = functionally stale for this endpoint).
	metaLastUpdated, err := h.meta.GetMetaLastUpdated(r.Context(), format)
	if err != nil {
		log.Printf("[WildcardRecommendationsHandler] GetMetaLastUpdated: %v", err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if metaLastUpdated == nil || time.Since(*metaLastUpdated) > metaStalenessThreshold {
		writeJSONError(w, "meta data unavailable or stale", http.StatusServiceUnavailable)
		return
	}

	// Card ratings freshness: populate data_freshness.stale but do NOT block.
	// Stale GIHWR data is still useful for deck-path recommendations.
	//
	// M1 — PremierDraft hardcode rationale:
	//   draft_card_ratings stores 17Lands GIHWR data keyed by draft_format.
	//   The only format the 17Lands sync Lambda currently populates is
	//   "PremierDraft". GIHWR is used here as a card-quality signal that is
	//   independent of the constructed-format being queried (?format=Standard,
	//   Historic, etc.): a card with high PremierDraft GIHWR is generally a
	//   strong card, which is relevant regardless of whether the player is
	//   crafting for Standard or Explorer.
	//
	//   Therefore: freshness tracks PremierDraft data unconditionally — NOT the
	//   ?format= param. This is intentionally different from the ADR-045 §5
	//   wording "the requested format", which is imprecise for this signal.
	//   Ray has been flagged for an ADR-045 §5 clarification (vmt-t#O1-follow-on).
	//
	//   If a Historic/Explorer query is made, card_ratings_cached_at reflects
	//   PremierDraft freshness (the GIHWR source), not Historic draft-ratings
	//   freshness (which does not exist). This is the correct behaviour and must
	//   NOT 503 a Historic/Explorer query just because no Historic draft-ratings
	//   row exists.
	ratingsCachedAt, err := h.draftRatings.GetMaxCachedAtByFormat(r.Context(), "PremierDraft")
	if err != nil {
		log.Printf("[WildcardRecommendationsHandler] GetMaxCachedAtByFormat: %v", err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	ratingsStale := ratingsCachedAt != nil && time.Since(*ratingsCachedAt) > cardRatingsStalenessThreshold
	staleReason := ""
	if ratingsStale {
		staleReason = "card_ratings_older_than_48h"
	}
	cardRatingsCachedAtStr := ""
	if ratingsCachedAt != nil {
		cardRatingsCachedAtStr = ratingsCachedAt.UTC().Format(time.RFC3339)
	}
	metaLastUpdatedStr := metaLastUpdated.UTC().Format(time.RFC3339)

	// ── zero-collection guard (ADR-045 §4) ───────────────────────────────────
	hasInventory, err := h.cardInv.HasCardInventory(r.Context(), accountID)
	if err != nil {
		log.Printf("[WildcardRecommendationsHandler] HasCardInventory: %v", err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !hasInventory {
		writeJSONError(w, "collection_not_synced", http.StatusConflict)
		return
	}

	// ── wildcard budget ───────────────────────────────────────────────────────
	wc, err := h.inventory.GetWildcardCounts(r.Context(), accountID)
	if err != nil {
		log.Printf("[WildcardRecommendationsHandler] GetWildcardCounts: %v", err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// ── sparse-collection data quality warning (ADR-045 §4) ──────────────────
	dataQualityWarning := ""
	if h.cardCount != nil {
		cardDistinctCount, cntErr := h.cardCount.CountCardInventory(r.Context(), accountID)
		if cntErr != nil {
			log.Printf("[WildcardRecommendationsHandler] CountCardInventory: %v", cntErr)
			// Non-fatal: omit the warning rather than failing the response.
		} else if cardDistinctCount < sparseCollectionThreshold {
			dataQualityWarning = "collection_may_be_incomplete"
		}
	}

	// ── gap analysis + aggregation ────────────────────────────────────────────
	recommendations := make([]wildcardRecommendationItem, 0)

	if h.gapQuery != nil {
		gapRows, gapErr := h.gapQuery.GetWildcardGapRows(r.Context(), accountID, format)
		if gapErr != nil {
			log.Printf("[WildcardRecommendationsHandler] GetWildcardGapRows: %v", gapErr)
			writeJSONError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		recommendations = buildRecommendations(gapRows, wc)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeMatchesJSON(w, wildcardRecommendationsResponse{
		WildcardBudget: wildcardBudgetResponse{
			Common:   wc.Common,
			Uncommon: wc.Uncommon,
			Rare:     wc.Rare,
			Mythic:   wc.Mythic,
		},
		DataFreshness: wildcardDataFreshnessResponse{
			CardRatingsCachedAt: cardRatingsCachedAtStr,
			MetaLastUpdated:     metaLastUpdatedStr,
			Stale:               ratingsStale,
			StaleReason:         staleReason,
		},
		DataQualityWarning: dataQualityWarning,
		Recommendations:    recommendations,
	})
}

// ─── aggregation + ranking ────────────────────────────────────────────────────

// buildRecommendations aggregates raw gap rows by archetype, computes the
// ADR-045 §3 ranking score for each, and returns the top maxRecommendations
// results sorted descending by score (ties broken by archetype_name ascending
// for determinism).
func buildRecommendations(rows []repository.WildcardGapRow, budget repository.WildcardCounts) []wildcardRecommendationItem {
	if len(rows) == 0 {
		return make([]wildcardRecommendationItem, 0)
	}

	// ── Phase 2a: aggregate per archetype ─────────────────────────────────────

	type archetypeAcc struct {
		name         string
		format       string
		tier         string
		sourceURL    string
		totalCards   int
		missingCards []wildcardMissingCard
		gihwrSum     float64
		gihwrCount   int
		wcRequired   wildcardBudgetResponse
	}

	byID := make(map[int64]*archetypeAcc)
	order := make([]int64, 0) // preserve first-seen insertion order for stable ties

	for _, row := range rows {
		acc, exists := byID[row.ArchetypeID]
		if !exists {
			acc = &archetypeAcc{
				name:      row.ArchetypeName,
				format:    row.Format,
				tier:      stringOrEmpty(row.Tier),
				sourceURL: stringOrEmpty(row.SourceURL),
			}
			byID[row.ArchetypeID] = acc
			order = append(order, row.ArchetypeID)
		}
		acc.totalCards++

		if row.CopiesMissing > 0 {
			acc.missingCards = append(acc.missingCards, wildcardMissingCard{
				ArenaID:      row.ArenaID,
				Name:         row.CardName,
				Rarity:       row.Rarity,
				CopiesNeeded: row.CopiesRequired,
				CopiesOwned:  row.CopiesOwned,
				GIHWR:        float64OrZero(row.GIHWR),
			})
			// Accumulate wildcard cost by rarity.
			switch strings.ToLower(row.Rarity) {
			case "common":
				acc.wcRequired.Common += row.CopiesMissing
			case "uncommon":
				acc.wcRequired.Uncommon += row.CopiesMissing
			case "rare":
				acc.wcRequired.Rare += row.CopiesMissing
			case "mythic":
				acc.wcRequired.Mythic += row.CopiesMissing
			}
		}

		// Accumulate GIHWR for the archetype's mean — used for gihwr_percentile.
		// Use all archetype cards (not just missing) so the signal reflects the
		// full deck quality, not just the gap cards.
		if row.GIHWR != nil {
			acc.gihwrSum += *row.GIHWR
			acc.gihwrCount++
		}
	}

	// ── Phase 2b: compute per-archetype mean GIHWR ─────────────────────────
	type archetypeSummary struct {
		id              int64
		acc             *archetypeAcc
		meanGIHWR       float64
		completionScore float64
	}

	summaries := make([]archetypeSummary, 0, len(order))
	for _, id := range order {
		acc := byID[id]
		var meanGIHWR float64
		if acc.gihwrCount > 0 {
			// gihwr is stored fractionally (0.0–1.0); do NOT multiply by 100.
			meanGIHWR = acc.gihwrSum / float64(acc.gihwrCount)
		}
		cardsNeeded := len(acc.missingCards)
		var completionScore float64
		if acc.totalCards > 0 {
			completionScore = float64(acc.totalCards-cardsNeeded) / float64(acc.totalCards)
		}
		summaries = append(summaries, archetypeSummary{
			id:              id,
			acc:             acc,
			meanGIHWR:       meanGIHWR,
			completionScore: completionScore,
		})
	}

	// ── Phase 2c: normalise GIHWR to [0.0, 1.0] within the response set ──────
	// (gihwr_percentile per ADR-045 §3)
	minGIHWR, maxGIHWR := summaries[0].meanGIHWR, summaries[0].meanGIHWR
	for _, s := range summaries {
		if s.meanGIHWR < minGIHWR {
			minGIHWR = s.meanGIHWR
		}
		if s.meanGIHWR > maxGIHWR {
			maxGIHWR = s.meanGIHWR
		}
	}

	gihwrRange := maxGIHWR - minGIHWR

	// ── Phase 2d: build ranked recommendation items ────────────────────────────
	items := make([]wildcardRecommendationItem, 0, len(summaries))
	for _, s := range summaries {
		acc := s.acc

		tierStr := strings.ToUpper(acc.tier)
		tierScore, ok := tierScoreMap[tierStr]
		if !ok {
			tierScore = tierScoreDefault
		}

		// gihwr_percentile: within-response-set normalisation of fractional values.
		var gihwrPercentile float64
		if gihwrRange > 0 {
			gihwrPercentile = (s.meanGIHWR - minGIHWR) / gihwrRange
		} else if maxGIHWR > 0 {
			// All archetypes have the same GIHWR — assign 0.5 (neutral).
			gihwrPercentile = 0.5
		}

		// rotation_proximity_score: 0.5 (neutral) for all non-Standard formats.
		// Standard: hardcoded 0.5 (no rotation-schedule SSM param in v0.3.8;
		// a future ADR will supply a real proximity score).
		rotationProximityScore := 0.5

		rankScore := (s.completionScore * rankWeightCompletion) +
			(gihwrPercentile * rankWeightGIHWRPercentile) +
			(tierScore * rankWeightTier) +
			(rotationProximityScore * rankWeightRotationProximity)

		// Affordable: wildcards_required fits within the player's budget.
		affordable := acc.wcRequired.Common <= budget.Common &&
			acc.wcRequired.Uncommon <= budget.Uncommon &&
			acc.wcRequired.Rare <= budget.Rare &&
			acc.wcRequired.Mythic <= budget.Mythic

		// Ensure MissingCards is never nil in JSON (always array, may be empty).
		missingCards := acc.missingCards
		if missingCards == nil {
			missingCards = make([]wildcardMissingCard, 0)
		}

		items = append(items, wildcardRecommendationItem{
			ArchetypeName:      acc.name,
			Format:             acc.format,
			Tier:               acc.tier,
			TierScore:          tierScore,
			CompletionScore:    roundFloat(s.completionScore, 4),
			CardsNeeded:        len(acc.missingCards),
			WildcardsRequired:  acc.wcRequired,
			Affordable:         affordable,
			ArchetypeSourceURL: acc.sourceURL,
			MissingCards:       missingCards,
			rankScore:          rankScore,
		})
	}

	// ── Phase 2e: sort descending by rankScore; ties by archetype_name asc ────
	sort.Slice(items, func(i, j int) bool {
		if items[i].rankScore != items[j].rankScore {
			return items[i].rankScore > items[j].rankScore
		}
		return items[i].ArchetypeName < items[j].ArchetypeName
	})

	// Cap at maxRecommendations.
	if len(items) > maxRecommendations {
		items = items[:maxRecommendations]
	}

	return items
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func stringOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func float64OrZero(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func roundFloat(f float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(f*pow) / pow
}
