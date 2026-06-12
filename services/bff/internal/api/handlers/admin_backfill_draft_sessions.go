package handlers

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// draftSessionsBackfiller is the subset of DraftSessionsRepository used by
// the backfill handler.
type draftSessionsBackfiller interface {
	BackfillStaleDraftSessions(ctx context.Context, opts repository.BackfillOptions) (repository.BackfillResult, error)
}

// AdminBackfillDraftSessionsHandler handles
//
//	POST /api/v1/admin/ops/backfill-draft-sessions
//
// One-time ops endpoint (ticket #1350).  Closes stale in_progress
// draft_sessions rows left behind by the bug in #1344 PR-B: the projection
// worker never received draft.completed events from old daemon builds, so
// sessions were never promoted out of in_progress.
//
// Optional query parameters (safe defaults shown):
//
//	staleness_hours=4   sessions inactive for < N hours are left alone
//	min_picks=42        orphaned sessions with < N picks are left alone
//	account_id=0        0 = all accounts; positive int scopes to one account
//
// Protected by AdminTokenMiddl. Idempotent: safe to re-run.
type AdminBackfillDraftSessionsHandler struct {
	repo draftSessionsBackfiller
}

// NewAdminBackfillDraftSessionsHandler returns an AdminBackfillDraftSessionsHandler.
func NewAdminBackfillDraftSessionsHandler(repo draftSessionsBackfiller) *AdminBackfillDraftSessionsHandler {
	return &AdminBackfillDraftSessionsHandler{repo: repo}
}

// backfillDraftSessionsResponse is the JSON body returned on success.
type backfillDraftSessionsResponse struct {
	ClosedCompleted []string `json:"closed_completed"`
	ClosedAbandoned []string `json:"closed_abandoned"`
	TotalClosed     int      `json:"total_closed"`
}

// ServeHTTP handles POST /api/v1/admin/ops/backfill-draft-sessions.
func (h *AdminBackfillDraftSessionsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	opts := repository.BackfillOptions{
		StalenessThreshold: backfillHoursParam(r, "staleness_hours", 4),
		MinPicksForStale:   backfillIntParam(r, "min_picks", 42),
		AccountID:          backfillInt64Param(r, "account_id", 0),
	}

	log.Printf(
		"[AdminBackfillDraftSessionsHandler] starting staleness_hours=%.0f min_picks=%d account_id=%d",
		opts.StalenessThreshold.Hours(), opts.MinPicksForStale, opts.AccountID,
	)

	result, err := h.repo.BackfillStaleDraftSessions(r.Context(), opts)
	if err != nil {
		log.Printf("[AdminBackfillDraftSessionsHandler] BackfillStaleDraftSessions: %v", err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	total := len(result.ClosedCompleted) + len(result.ClosedAbandoned)

	log.Printf(
		"[AdminBackfillDraftSessionsHandler] done closed_completed=%d closed_abandoned=%d total=%d",
		len(result.ClosedCompleted), len(result.ClosedAbandoned), total,
	)

	// Return empty slices rather than null in JSON.
	if result.ClosedCompleted == nil {
		result.ClosedCompleted = []string{}
	}

	if result.ClosedAbandoned == nil {
		result.ClosedAbandoned = []string{}
	}

	writeJSON(w, backfillDraftSessionsResponse{
		ClosedCompleted: result.ClosedCompleted,
		ClosedAbandoned: result.ClosedAbandoned,
		TotalClosed:     total,
	}, http.StatusOK)
}

// backfillHoursParam parses a positive integer hours query param as a duration.
// Falls back to defaultHours when the param is absent or invalid.
func backfillHoursParam(r *http.Request, key string, defaultHours int) time.Duration {
	if raw := r.URL.Query().Get(key); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return time.Duration(n) * time.Hour
		}
	}

	return time.Duration(defaultHours) * time.Hour
}

// backfillIntParam parses a positive integer query param, falling back to defaultVal.
func backfillIntParam(r *http.Request, key string, defaultVal int) int {
	if raw := r.URL.Query().Get(key); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}

	return defaultVal
}

// backfillInt64Param parses a non-negative int64 query param, falling back to defaultVal.
func backfillInt64Param(r *http.Request, key string, defaultVal int64) int64 {
	if raw := r.URL.Query().Get(key); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n >= 0 {
			return n
		}
	}

	return defaultVal
}
