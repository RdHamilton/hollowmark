package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// deletionJobReader reads the status of an erasure job.
type deletionJobReader interface {
	GetJobStatus(ctx context.Context, jobID string) (*repository.DeletionJobStatus, error)
}

// AccountDeletionStatusHandler handles GET /api/v1/account/deletion-status/{job_id}.
type AccountDeletionStatusHandler struct {
	reader deletionJobReader
}

// NewAccountDeletionStatusHandler returns a handler backed by reader.
func NewAccountDeletionStatusHandler(reader deletionJobReader) *AccountDeletionStatusHandler {
	return &AccountDeletionStatusHandler{reader: reader}
}

// deletionStatusResponse is the JSON body for the deletion status endpoint.
type deletionStatusResponse struct {
	JobID       string     `json:"job_id"`
	Status      string     `json:"status"` // "pending" | "completed"
	RequestedAt time.Time  `json:"requested_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Status handles GET /api/v1/account/deletion-status/{job_id}.
//
// Returns the current state of an erasure job:
//   - 200 with status="pending" if completed_at is NULL.
//   - 200 with status="completed" if completed_at is set.
//   - 404 if the job_id is not found.
//   - 401 if the request is not authenticated.
func (h *AccountDeletionStatusHandler) Status(w http.ResponseWriter, r *http.Request) {
	_, ok := bffmiddleware.ClerkUserIDFromContext(r)
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	jobID := strings.TrimSpace(chi.URLParam(r, "job_id"))
	if jobID == "" {
		writeJSONError(w, "job_id is required", http.StatusBadRequest)
		return
	}

	job, err := h.reader.GetJobStatus(r.Context(), jobID)
	if err != nil {
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if job == nil {
		writeJSONError(w, "job not found", http.StatusNotFound)
		return
	}

	status := "pending"
	if job.CompletedAt != nil {
		status = "completed"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(deletionStatusResponse{
		JobID:       job.JobID,
		Status:      status,
		RequestedAt: job.RequestedAt,
		CompletedAt: job.CompletedAt,
	})
}
