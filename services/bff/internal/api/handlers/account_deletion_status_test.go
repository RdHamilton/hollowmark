package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	"github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// stubDeletionReader is a test double for the deletionJobReader interface.
// It maps (jobID, clerkUserID) pairs to results — simulating scoped lookups.
type stubDeletionReader struct {
	// jobs maps jobID → ownerClerkUserID.  GetJobStatus returns non-nil only
	// when the provided clerkUserID matches the owner.
	jobs map[string]string
	err  error
}

func (s *stubDeletionReader) GetJobStatus(_ context.Context, jobID, clerkUserID string) (*repository.DeletionJobStatus, error) {
	if s.err != nil {
		return nil, s.err
	}
	owner, exists := s.jobs[jobID]
	if !exists || owner != clerkUserID {
		// Intentionally identical response for "not found" and "wrong owner"
		// to prevent enumeration.
		return nil, nil
	}
	now := time.Now()
	return &repository.DeletionJobStatus{
		JobID:       jobID,
		RequestedAt: now,
	}, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newStatusRequest builds a GET request for the deletion-status route with the
// job_id embedded in the chi URL params and an optional Clerk user ID in context.
func newStatusRequest(clerkUserID, jobID string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/deletion-status/"+jobID, nil)
	// Inject chi URL param.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("job_id", jobID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	if clerkUserID != "" {
		req = middleware.WithClerkUserID(req, clerkUserID)
	}
	return req
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestAccountDeletionStatusHandler_Returns200ForOwner verifies the happy path:
// the owner of a job gets 200 with the correct status field.
func TestAccountDeletionStatusHandler_Returns200ForOwner(t *testing.T) {
	reader := &stubDeletionReader{
		jobs: map[string]string{"job-abc": "user_clerk_owner"},
	}
	h := handlers.NewAccountDeletionStatusHandler(reader)

	req := newStatusRequest("user_clerk_owner", "job-abc")
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var body struct {
		JobID  string `json:"job_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response body: %v", err)
	}
	if body.JobID != "job-abc" {
		t.Errorf("job_id: got %q, want %q", body.JobID, "job-abc")
	}
	if body.Status != "pending" {
		t.Errorf("status: got %q, want %q", body.Status, "pending")
	}
}

// TestAccountDeletionStatusHandler_Returns401WhenNoClerkID verifies that
// unauthenticated requests are rejected with 401.
func TestAccountDeletionStatusHandler_Returns401WhenNoClerkID(t *testing.T) {
	reader := &stubDeletionReader{jobs: map[string]string{"job-abc": "user_clerk_owner"}}
	h := handlers.NewAccountDeletionStatusHandler(reader)

	req := newStatusRequest("", "job-abc") // No Clerk session.
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestAccountDeletionStatusHandler_Returns404ForUnknownJob verifies that a
// job_id that does not exist returns 404.
func TestAccountDeletionStatusHandler_Returns404ForUnknownJob(t *testing.T) {
	reader := &stubDeletionReader{jobs: map[string]string{}}
	h := handlers.NewAccountDeletionStatusHandler(reader)

	req := newStatusRequest("user_clerk_owner", "job-nonexistent")
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusNotFound)
	}
}

// TestAccountDeletionStatusHandler_Returns404ForWrongUser is the IDOR
// regression test: user B polls user A's job_id and MUST receive 404, not
// 200.  The DB query scopes by clerk_user_id so the row is simply not
// returned — the response is indistinguishable from "not found".
func TestAccountDeletionStatusHandler_Returns404ForWrongUser(t *testing.T) {
	reader := &stubDeletionReader{
		jobs: map[string]string{"job-abc": "user_clerk_owner"}, // job belongs to owner
	}
	h := handlers.NewAccountDeletionStatusHandler(reader)

	// attacker polls with their own Clerk ID but job A's job_id.
	req := newStatusRequest("user_clerk_attacker", "job-abc")
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("IDOR: cross-user job read returned %d, want 404", w.Code)
	}
}

// TestAccountDeletionStatusHandler_Returns500OnReaderError verifies that a
// repository error is mapped to 500 Internal Server Error.
func TestAccountDeletionStatusHandler_Returns500OnReaderError(t *testing.T) {
	reader := &stubDeletionReader{err: errors.New("db unavailable")}
	h := handlers.NewAccountDeletionStatusHandler(reader)

	req := newStatusRequest("user_clerk_owner", "job-abc")
	w := httptest.NewRecorder()
	h.Status(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
