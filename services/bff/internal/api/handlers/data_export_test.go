package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	"github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
)

// ---------------------------------------------------------------------------
// Stubs
// ---------------------------------------------------------------------------

// stubExportRateLimiter stubs the DSR rate-limit check.
type stubExportRateLimiter struct {
	limited    bool
	retryAfter int64
	err        error
	recorded   []int64 // userIDs passed to RecordExport
}

func (s *stubExportRateLimiter) CheckRecentExport(_ context.Context, _ int64) (bool, int64, error) {
	return s.limited, s.retryAfter, s.err
}

func (s *stubExportRateLimiter) RecordExport(_ context.Context, userID int64) (string, error) {
	s.recorded = append(s.recorded, userID)
	return "export-test-uuid", nil
}

// stubDataGatherer stubs the data gather operation.
type stubDataGatherer struct {
	result *handlers.ExportPayload
	err    error
}

func (s *stubDataGatherer) GatherForUser(_ context.Context, _, _ int64) (*handlers.ExportPayload, error) {
	return s.result, s.err
}

// stubExportAccountResolver stubs account ID resolution.
type stubExportAccountResolver struct {
	accountID int64
	found     bool
	err       error
}

func (s *stubExportAccountResolver) GetAccountIDByUserID(_ context.Context, _ int64) (int64, bool, error) {
	return s.accountID, s.found, s.err
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newExportRequest(userID int64) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/data-export", nil)
	if userID != 0 {
		req = req.WithContext(middleware.WithUserID(req.Context(), userID))
	}
	return req
}

func validExportPayload() *handlers.ExportPayload {
	return &handlers.ExportPayload{
		ExportID:      "test-export-id",
		ExportedAt:    time.Now(),
		AccountIDHash: "abc123def456abcd",
		Format:        "access",
		Manifest:      []handlers.ManifestEntry{{Source: "accounts", RowCount: 1}},
		Data:          map[string]any{"accounts": []any{}},
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestDataExportHandler_Returns200WithExportBody verifies the happy path:
// authenticated GET /api/v1/account/data-export returns 200 with valid JSON.
func TestDataExportHandler_Returns200WithExportBody(t *testing.T) {
	limiter := &stubExportRateLimiter{limited: false}
	gatherer := &stubDataGatherer{result: validExportPayload()}
	resolver := &stubExportAccountResolver{accountID: 42, found: true}

	h := handlers.NewDataExportHandler(limiter, gatherer, resolver)

	req := newExportRequest(1)
	w := httptest.NewRecorder()
	h.Export(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response body: %v", err)
	}
	if _, ok := body["export_id"]; !ok {
		t.Error("response body missing export_id")
	}
	if _, ok := body["manifest"]; !ok {
		t.Error("response body missing manifest")
	}
	if _, ok := body["data"]; !ok {
		t.Error("response body missing data")
	}
}

// TestDataExportHandler_Returns401WhenNoUserID verifies that a request without
// a user ID on context (i.e. ClerkAuthMiddleware not applied) returns 401.
func TestDataExportHandler_Returns401WhenNoUserID(t *testing.T) {
	limiter := &stubExportRateLimiter{}
	gatherer := &stubDataGatherer{result: validExportPayload()}
	resolver := &stubExportAccountResolver{accountID: 42, found: true}

	h := handlers.NewDataExportHandler(limiter, gatherer, resolver)

	// No userID set on context.
	req := newExportRequest(0)
	w := httptest.NewRecorder()
	h.Export(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want 401", w.Code)
	}
}

// TestDataExportHandler_Returns429WhenRateLimited verifies that a request within
// the 24h window returns 429 Too Many Requests with Retry-After header.
func TestDataExportHandler_Returns429WhenRateLimited(t *testing.T) {
	const expectedRetryAfter = int64(3600)
	limiter := &stubExportRateLimiter{limited: true, retryAfter: expectedRetryAfter}
	gatherer := &stubDataGatherer{result: validExportPayload()}
	resolver := &stubExportAccountResolver{accountID: 42, found: true}

	h := handlers.NewDataExportHandler(limiter, gatherer, resolver)

	req := newExportRequest(1)
	w := httptest.NewRecorder()
	h.Export(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status: got %d, want 429", w.Code)
	}

	retryAfterHeader := w.Header().Get("Retry-After")
	if retryAfterHeader == "" {
		t.Error("expected Retry-After header, got empty string")
	}
}

// TestDataExportHandler_Returns404WhenNoAccount verifies that a user without
// an account row returns 404.
func TestDataExportHandler_Returns404WhenNoAccount(t *testing.T) {
	limiter := &stubExportRateLimiter{limited: false}
	gatherer := &stubDataGatherer{result: validExportPayload()}
	resolver := &stubExportAccountResolver{accountID: 0, found: false}

	h := handlers.NewDataExportHandler(limiter, gatherer, resolver)

	req := newExportRequest(1)
	w := httptest.NewRecorder()
	h.Export(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", w.Code)
	}
}

// TestDataExportHandler_Returns500WhenGatherFails verifies that a database
// error during gather returns 500.
func TestDataExportHandler_Returns500WhenGatherFails(t *testing.T) {
	limiter := &stubExportRateLimiter{limited: false}
	gatherer := &stubDataGatherer{err: errors.New("db error")}
	resolver := &stubExportAccountResolver{accountID: 42, found: true}

	h := handlers.NewDataExportHandler(limiter, gatherer, resolver)

	req := newExportRequest(1)
	w := httptest.NewRecorder()
	h.Export(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", w.Code)
	}
}

// TestDataExportHandler_RecordsExportAfterSuccessfulGather verifies that
// RecordExport is called exactly once on a successful export, so the rate-limit
// window is opened correctly.
func TestDataExportHandler_RecordsExportAfterSuccessfulGather(t *testing.T) {
	limiter := &stubExportRateLimiter{limited: false}
	gatherer := &stubDataGatherer{result: validExportPayload()}
	resolver := &stubExportAccountResolver{accountID: 42, found: true}

	h := handlers.NewDataExportHandler(limiter, gatherer, resolver)

	req := newExportRequest(1)
	w := httptest.NewRecorder()
	h.Export(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	if len(limiter.recorded) != 1 {
		t.Errorf("expected RecordExport called once, got %d calls", len(limiter.recorded))
	}
	if limiter.recorded[0] != 1 {
		t.Errorf("RecordExport called with userID=%d, want 1", limiter.recorded[0])
	}
}

// TestDataExportHandler_ContentDispositionHeader verifies the response includes
// the Content-Disposition header for file download per the approved plan.
func TestDataExportHandler_ContentDispositionHeader(t *testing.T) {
	limiter := &stubExportRateLimiter{limited: false}
	gatherer := &stubDataGatherer{result: validExportPayload()}
	resolver := &stubExportAccountResolver{accountID: 42, found: true}

	h := handlers.NewDataExportHandler(limiter, gatherer, resolver)

	req := newExportRequest(1)
	w := httptest.NewRecorder()
	h.Export(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	cd := w.Header().Get("Content-Disposition")
	if cd == "" {
		t.Error("expected Content-Disposition header, got empty string")
	}
}

// TestDataExportHandler_ClerkProfilePresentInResponse verifies that when the
// gatherer returns a ClerkProfile, it appears in the JSON response body
// (Art.15 Q2 -- raw email included in export, Ray-approved).
func TestDataExportHandler_ClerkProfilePresentInResponse(t *testing.T) {
	limiter := &stubExportRateLimiter{limited: false}
	gatherer := &stubDataGatherer{result: func() *handlers.ExportPayload {
		p := validExportPayload()
		p.ClerkProfile = &handlers.ClerkProfile{
			Email:     "art15test@example.com",
			FirstName: "Art",
			LastName:  "Fifteen",
		}
		return p
	}()}
	resolver := &stubExportAccountResolver{accountID: 42, found: true}

	h := handlers.NewDataExportHandler(limiter, gatherer, resolver)

	req := newExportRequest(1)
	w := httptest.NewRecorder()
	h.Export(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200; body: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse response body: %v", err)
	}
	cp, ok := body["clerk_profile"]
	if !ok {
		t.Fatal("response body missing clerk_profile")
	}
	cpMap, ok := cp.(map[string]any)
	if !ok {
		t.Fatalf("clerk_profile is not an object: %T", cp)
	}
	if email, _ := cpMap["email"].(string); email != "art15test@example.com" {
		t.Errorf("clerk_profile.email: got %q, want %q", email, "art15test@example.com")
	}
}
