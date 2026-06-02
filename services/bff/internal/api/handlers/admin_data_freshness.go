package handlers

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"time"
)

// CardRatingsFreshnessChecker is the minimal interface required by
// AdminDataFreshnessHandler. *repository.DraftRatingsRepository satisfies it.
type CardRatingsFreshnessChecker interface {
	GetGlobalMaxCachedAt(ctx context.Context) (time.Time, error)
}

// AdminDataFreshnessHandler serves GET /api/v1/admin/data-freshness.
//
// Returns a JSON object describing the freshness of draft_card_ratings — i.e.
// the most recent timestamp written by the 17Lands sync Lambda. This lets
// operators confirm that ML inputs are current before a feature flag flip,
// and provides an automated signal for future alerting.
//
// Protected by AdminTokenAuth middleware (same static Bearer token used by the
// fleet-health endpoint — SSM /vaultmtg/app/production/bff-admin-token).
// No PII returned.
type AdminDataFreshnessHandler struct {
	checker        CardRatingsFreshnessChecker
	thresholdHours int
}

// NewAdminDataFreshnessHandler returns an AdminDataFreshnessHandler.
//
//   - checker        — data source for MAX(cached_at) across draft_card_ratings
//   - thresholdHours — age in hours beyond which ratings are considered stale
//     (matches DraftRatingsStalenessThresholdHours from config)
func NewAdminDataFreshnessHandler(checker CardRatingsFreshnessChecker, thresholdHours int) *AdminDataFreshnessHandler {
	return &AdminDataFreshnessHandler{
		checker:        checker,
		thresholdHours: thresholdHours,
	}
}

// dataFreshnessResponse is the JSON body returned by GET
// /api/v1/admin/data-freshness.
type dataFreshnessResponse struct {
	// Status is one of: "fresh", "stale", "no_data".
	Status         string     `json:"status"`
	MaxCachedAt    *time.Time `json:"max_cached_at,omitempty"`
	AgeHours       *float64   `json:"age_hours,omitempty"`
	ThresholdHours int        `json:"threshold_hours"`
	AsOf           time.Time  `json:"as_of"`
}

// ServeHTTP handles GET /api/v1/admin/data-freshness.
func (h *AdminDataFreshnessHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	maxCachedAt, err := h.checker.GetGlobalMaxCachedAt(r.Context())
	if err != nil {
		log.Printf("[admin_data_freshness] GetGlobalMaxCachedAt: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)

		return
	}

	now := time.Now().UTC()

	resp := dataFreshnessResponse{
		ThresholdHours: h.thresholdHours,
		AsOf:           now,
	}

	switch {
	case maxCachedAt.IsZero():
		resp.Status = "no_data"

	default:
		ageHours := now.Sub(maxCachedAt).Hours()
		rounded := math.Round(ageHours*10) / 10 // 1 decimal place
		resp.AgeHours = &rounded
		resp.MaxCachedAt = &maxCachedAt

		if ageHours > float64(h.thresholdHours) {
			resp.Status = "stale"
		} else {
			resp.Status = "fresh"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[admin_data_freshness] encode: %v", err)
	}
}
