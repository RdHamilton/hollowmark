package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
)

// ─── stubs ──────────────────────────────────────────────────────────────────

type profileAccountLookup struct {
	accountID int64
	found     bool
	err       error
}

func (s *profileAccountLookup) GetAccountIDByUserID(_ context.Context, _ int64) (int64, bool, error) {
	return s.accountID, s.found, s.err
}

type stubRectificationWriter struct {
	lastUserID  int64
	lastField   string
	lastOldHash *string
	lastNewHash string
	err         error
	callCount   int
}

func (s *stubRectificationWriter) InsertRectificationEvent(
	_ context.Context,
	userID int64,
	fieldName string,
	oldValueHash *string,
	newValueHash string,
) error {
	s.lastUserID = userID
	s.lastField = fieldName
	s.lastOldHash = oldValueHash
	s.lastNewHash = newValueHash
	s.callCount++
	return s.err
}

type stubEmailUpdater struct {
	lastUserID int64
	lastEmail  string
	err        error
	callCount  int
}

func (s *stubEmailUpdater) UpdateEmail(_ context.Context, userID int64, email string) error {
	s.lastUserID = userID
	s.lastEmail = email
	s.callCount++
	return s.err
}

// ─── helpers ────────────────────────────────────────────────────────────────

func newProfileHandler(
	auditWriter *stubRectificationWriter,
	emailUpdater *stubEmailUpdater,
	accounts *profileAccountLookup,
) *handlers.AccountProfileHandler {
	return handlers.NewAccountProfileHandler(auditWriter, emailUpdater, accounts)
}

func authedProfileRequest(t *testing.T, body any, userID int64) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/account/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
}

// ─── handler tests ───────────────────────────────────────────────────────────

// TestAccountProfileHandler_EmailOnly_OK verifies a valid email-change body:
//   - returns 200 with updated_at
//   - writes one rectification audit row for the "email" field
//   - calls UpdateEmail with the correct arguments
//   - old_value_hash is set (we have a current email from the account lookup)
func TestAccountProfileHandler_EmailOnly_OK(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	body := map[string]string{"email": "new@example.com"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.Patch(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: want 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// Response body must include updated_at.
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := resp["updated_at"]; !ok {
		t.Error("response must include updated_at field")
	}

	// Audit row written.
	if auditWriter.callCount != 1 {
		t.Errorf("InsertRectificationEvent call count: want 1, got %d", auditWriter.callCount)
	}
	if auditWriter.lastField != "email" {
		t.Errorf("audit field: want %q, got %q", "email", auditWriter.lastField)
	}
	// new_value_hash must not be raw PII.
	if auditWriter.lastNewHash == "new@example.com" {
		t.Error("new_value_hash must not be the raw new email address")
	}
	// new_value_hash must be exactly 16 hex chars.
	if len(auditWriter.lastNewHash) != 16 {
		t.Errorf("new_value_hash length: want 16, got %d", len(auditWriter.lastNewHash))
	}

	// email sync called.
	if emailUpdater.callCount != 1 {
		t.Errorf("UpdateEmail call count: want 1, got %d", emailUpdater.callCount)
	}
	if emailUpdater.lastEmail != "new@example.com" {
		t.Errorf("UpdateEmail email: want %q, got %q", "new@example.com", emailUpdater.lastEmail)
	}
	if emailUpdater.lastUserID != 1 {
		t.Errorf("UpdateEmail userID: want 1, got %d", emailUpdater.lastUserID)
	}
}

// TestAccountProfileHandler_DisplayNameOnly_OK verifies display_name-only body:
//   - returns 200
//   - writes one rectification audit row for "display_name"
//   - does NOT call UpdateEmail (display_name is audit-only, Clerk-owned)
func TestAccountProfileHandler_DisplayNameOnly_OK(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	body := map[string]string{"display_name": "Alice"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.Patch(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: want 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	if auditWriter.callCount != 1 {
		t.Errorf("InsertRectificationEvent call count: want 1, got %d", auditWriter.callCount)
	}
	if auditWriter.lastField != "display_name" {
		t.Errorf("audit field: want %q, got %q", "display_name", auditWriter.lastField)
	}

	// UpdateEmail must NOT be called for display_name (Clerk-owned, not persisted).
	if emailUpdater.callCount != 0 {
		t.Errorf("UpdateEmail must not be called for display_name changes, got %d calls", emailUpdater.callCount)
	}
}

// TestAccountProfileHandler_BothFields_OK verifies that both email and
// display_name in one request writes two audit rows and calls UpdateEmail once.
func TestAccountProfileHandler_BothFields_OK(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	body := map[string]string{
		"email":        "both@example.com",
		"display_name": "Bob",
	}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.Patch(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: want 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// Two audit rows — one per changed field.
	if auditWriter.callCount != 2 {
		t.Errorf("InsertRectificationEvent call count: want 2, got %d", auditWriter.callCount)
	}

	// email sync called exactly once.
	if emailUpdater.callCount != 1 {
		t.Errorf("UpdateEmail call count: want 1, got %d", emailUpdater.callCount)
	}
}

// TestAccountProfileHandler_NoFields_OK verifies that an empty (but valid JSON)
// body with no recognized fields returns 200 and writes no audit rows.
func TestAccountProfileHandler_NoFields_OK(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	body := map[string]string{}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.Patch(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: want 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if auditWriter.callCount != 0 {
		t.Errorf("InsertRectificationEvent call count: want 0, got %d", auditWriter.callCount)
	}
}

// TestAccountProfileHandler_Unauthorized verifies that a request without a
// user ID on the context returns 401.
func TestAccountProfileHandler_Unauthorized(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	b, _ := json.Marshal(map[string]string{"email": "x@example.com"})
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/account/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	// No context userID — simulates unauthenticated request.
	rec := httptest.NewRecorder()

	h.Patch(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: want 401, got %d", rec.Code)
	}
}

// TestAccountProfileHandler_AccountNotFound verifies 404 when the user has no account row.
func TestAccountProfileHandler_AccountNotFound(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{found: false}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	body := map[string]string{"email": "x@example.com"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.Patch(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", rec.Code)
	}
}

// TestAccountProfileHandler_InvalidJSON verifies that a malformed JSON body returns 400.
func TestAccountProfileHandler_InvalidJSON(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/account/profile", bytes.NewBufferString(`{not valid`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(bffmiddleware.WithUserID(req.Context(), 1))
	rec := httptest.NewRecorder()

	h.Patch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", rec.Code)
	}
}

// TestAccountProfileHandler_AuditWriteError returns 500 when the audit log insert fails.
func TestAccountProfileHandler_AuditWriteError(t *testing.T) {
	auditWriter := &stubRectificationWriter{err: errors.New("db down")}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	body := map[string]string{"email": "x@example.com"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.Patch(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500, got %d", rec.Code)
	}
}

// TestAccountProfileHandler_EmailSyncError returns 500 when UpdateEmail fails.
func TestAccountProfileHandler_EmailSyncError(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{err: errors.New("db constraint")}
	accounts := &profileAccountLookup{accountID: 42, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	body := map[string]string{"email": "x@example.com"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.Patch(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500, got %d", rec.Code)
	}
}

// TestAccountProfileHandler_DateOfBirthYear_Rejected verifies that a request
// carrying date_of_birth_year is rejected with 400 (COPPA-gated, not
// self-service per Ray ruling on #888).
func TestAccountProfileHandler_DateOfBirthYear_Rejected(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	body := map[string]any{"date_of_birth_year": 1990}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.Patch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400 (dob_year out-of-scope), got %d", rec.Code)
	}
}

// TestAccountProfileHandler_HashIsNotRawPII verifies that the new_value_hash
// for a changed email is never the raw email string, ensuring PII is not
// persisted in the audit log.
func TestAccountProfileHandler_HashIsNotRawPII(t *testing.T) {
	const rawEmail = "pii-check@example.com"

	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 7, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	body := map[string]string{"email": rawEmail}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.Patch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d", rec.Code)
	}
	if auditWriter.lastNewHash == rawEmail {
		t.Errorf("new_value_hash must not be the raw email; got raw value %q", rawEmail)
	}
}
