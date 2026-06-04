// wildcard_recommendations.go — ADR-045 §6 (v0.3.7 scaffold)
//
// GET /api/v1/recommendations/wildcards
//
// This file is the v0.3.7 scaffold stub:
//   - Route registered under composeClerkAuth (see cmd/main.go).
//   - Returns HTTP 501 with the complete ADR-045 JSON shape and an empty
//     recommendations slice.
//   - Four narrow repo interfaces are injected so the #420 implementer can
//     swap stubs for real repositories without touching the handler signature.
//
// NOTE for #420 implementer — GIHWR fractional-units guard:
//   draft_card_ratings.gihwr is stored as a FRACTIONAL value (0.0–1.0), NOT
//   as a percentage (0–100). For example, a 62.3% win rate is stored as 0.623.
//   The ranking formula in ADR-045 §3 expects gihwr_percentile in [0.0, 1.0].
//   Do NOT multiply gihwr by 100 before normalising — #787 fixed this exact
//   bug in pkg/draftalgo. The percentile computation is within-response-set
//   normalisation of the raw fractional values, not a conversion step.

package handlers

import (
	"context"
	"log"
	"net/http"
	"strings"
	"time"

	bffmiddleware "github.com/RdHamilton/vault-mtg/services/bff/internal/api/middleware"
)

// ─── narrow repository interfaces ────────────────────────────────────────────
// These four interfaces define the minimal DB surface the full #420
// implementation will need. Keeping them narrow here lets tests inject stubs
// without depending on the concrete repository types.

// WildcardCounts holds the four wildcard rarity buckets from the inventory
// table (wc_common, wc_uncommon, wc_rare, wc_mythic).
type WildcardCounts struct {
	Common   int `json:"common"`
	Uncommon int `json:"uncommon"`
	Rare     int `json:"rare"`
	Mythic   int `json:"mythic"`
}

// InventoryReader reads wildcard counts for an account from the inventory table.
type InventoryReader interface {
	GetWildcardCounts(ctx context.Context, accountID int64) (WildcardCounts, error)
}

// CardInventoryChecker checks whether an account has any card_inventory rows.
// Used for the zero-collection guard (ADR-045 §4 — return 409, not 200 with
// all copies_owned=0).
type CardInventoryChecker interface {
	HasCardInventory(ctx context.Context, accountID int64) (bool, error)
}

// DraftRatingsMaxCacheChecker reads MAX(cached_at) from draft_card_ratings for
// the requested format. Used to populate data_freshness.card_ratings_cached_at
// and to set data_freshness.stale when the value is older than 48h (ADR-045 §5).
type DraftRatingsMaxCacheChecker interface {
	GetMaxCachedAt(ctx context.Context, format string) (*time.Time, error)
}

// MetaFreshnessChecker reads the most recent last_updated timestamp from
// mtgzone_archetypes for the requested format. Used for data_freshness.meta_last_updated
// and the 7-day staleness guard that produces 503 (ADR-045 §5).
type MetaFreshnessChecker interface {
	GetMetaLastUpdated(ctx context.Context, format string) (*time.Time, error)
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
	// mtgzone_archetypes.last_updated for the requested format, or empty string.
	MetaLastUpdated string `json:"meta_last_updated"`
	// Stale is true when card_ratings_cached_at is older than 48h (ADR-045 §5).
	Stale bool `json:"stale"`
}

// wildcardRecommendationsResponse is the complete ADR-045 HTTP 200 shape.
// The stub returns this with an empty Recommendations slice and HTTP 501.
type wildcardRecommendationsResponse struct {
	WildcardBudget wildcardBudgetResponse        `json:"wildcard_budget"`
	DataFreshness  wildcardDataFreshnessResponse `json:"data_freshness"`
	// Recommendations holds the ranked list of deck-path craft targets.
	// The stub always returns an empty slice; #420 populates it from the
	// four-table join + Go-layer aggregation described in ADR-045 §2.
	Recommendations []any `json:"recommendations"`
}

// ─── handler ─────────────────────────────────────────────────────────────────

// WildcardRecommendationsHandler serves GET /api/v1/recommendations/wildcards.
// v0.3.7: stub returning 501 with the complete ADR-045 response shape.
// v0.3.8 (#420): full implementation with four-table join + ranking.
type WildcardRecommendationsHandler struct {
	accounts     AccountLookup
	inventory    InventoryReader
	cardInv      CardInventoryChecker
	draftRatings DraftRatingsMaxCacheChecker
	meta         MetaFreshnessChecker
}

// NewWildcardRecommendationsHandler constructs the handler with its injected
// dependencies. All four repository interfaces are required; pass stubs in tests.
func NewWildcardRecommendationsHandler(
	accounts AccountLookup,
	inventory InventoryReader,
	cardInv CardInventoryChecker,
	draftRatings DraftRatingsMaxCacheChecker,
	meta MetaFreshnessChecker,
) *WildcardRecommendationsHandler {
	return &WildcardRecommendationsHandler{
		accounts:     accounts,
		inventory:    inventory,
		cardInv:      cardInv,
		draftRatings: draftRatings,
		meta:         meta,
	}
}

// GetWildcardRecommendations handles GET /api/v1/recommendations/wildcards.
//
// v0.3.7 stub: returns 501 with the complete ADR-045 JSON shape and an empty
// recommendations slice. Auth guard and 404 (account not provisioned) are live.
//
// Query params (accepted but not yet acted upon — see #420):
//
//	?format= — narrows archetypes to a specific format (default: "Standard").
//	            Valid values per ADR-045: Standard, Historic, Alchemy, Explorer.
func (h *WildcardRecommendationsHandler) GetWildcardRecommendations(w http.ResponseWriter, r *http.Request) {
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	accountID, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[WildcardRecommendationsHandler] GetAccountIDByUserID userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSONError(w, "account not found", http.StatusNotFound)
		return
	}

	// format param: read and normalise now so the routing is wired correctly
	// for #420; it is not used by the stub computation below.
	format := strings.TrimSpace(r.URL.Query().Get("format"))
	if format == "" {
		format = "Standard"
	}

	// Stub: return the complete ADR-045 shape with empty recommendations.
	// The #420 implementer replaces this block with the four-table join,
	// Go-layer aggregation, and ranking formula (ADR-045 §2–§3).
	//
	// Suppress the "accountID and format unused" linter noise by referencing
	// them in the log. They will be load-bearing in #420.
	log.Printf("[WildcardRecommendationsHandler] stub accountID=%d format=%s", accountID, format)

	resp := wildcardRecommendationsResponse{
		WildcardBudget: wildcardBudgetResponse{},
		DataFreshness: wildcardDataFreshnessResponse{
			CardRatingsCachedAt: "",
			MetaLastUpdated:     "",
			Stale:               false,
		},
		Recommendations: []any{},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotImplemented)
	writeMatchesJSON(w, resp)
}
