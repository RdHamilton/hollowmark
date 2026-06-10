package handlers_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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

// stubClerkEmailFetcher returns a preset email (the verified primary address).
type stubClerkEmailFetcher struct {
	email string
	err   error
}

func (s *stubClerkEmailFetcher) FetchPrimaryEmail(_ context.Context, _ string) (string, error) {
	return s.email, s.err
}

// ─── helpers ────────────────────────────────────────────────────────────────

const testPIISalt = "test-salt-value"

// unsaltedHash computes SHA-256(value)[:16] — the same formula as
// identityhash.HashAccountID.  Used in tests to confirm the handler is NOT
// using the unsalted function for PII fields.
func unsaltedHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum)[:16]
}

func newProfileHandler(
	auditWriter *stubRectificationWriter,
	emailUpdater *stubEmailUpdater,
	accounts *profileAccountLookup,
) *handlers.AccountProfileHandler {
	return handlers.NewAccountProfileHandler(auditWriter, emailUpdater, accounts, testPIISalt, nil)
}

// newProfileHandlerWithClerk builds a handler with a real Clerk email fetcher stub.
func newProfileHandlerWithClerk(
	auditWriter *stubRectificationWriter,
	emailUpdater *stubEmailUpdater,
	accounts *profileAccountLookup,
	clerkFetcher *stubClerkEmailFetcher,
) *handlers.AccountProfileHandler {
	return handlers.NewAccountProfileHandler(auditWriter, emailUpdater, accounts, testPIISalt, clerkFetcher)
}

func authedProfileRequest(t *testing.T, body any, userID int64) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/account/profile", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
	// Inject a synthetic Clerk user ID (needed for the Clerk re-fetch path — Fix 4).
	req = bffmiddleware.WithClerkUserID(req, "user_test_clerk_id")
	return req
}

// ─── Fix 2: Salted PII hash tests ────────────────────────────────────────────

// TestAccountProfileHandler_HashUsesSalt verifies that the new_value_hash for a
// changed email is computed using HashPII(salt, value) and NOT HashAccountID(value)
// (i.e. NOT an unsalted SHA-256 of the raw email alone).
func TestAccountProfileHandler_HashUsesSalt(t *testing.T) {
	const rawEmail = "salted@example.com"

	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 7, found: true}
	// Provide a Clerk fetcher stub that returns the same email so we isolate
	// the hash test from any Clerk-fetch side-effects.
	clerkFetcher := &stubClerkEmailFetcher{email: rawEmail}

	h := newProfileHandlerWithClerk(auditWriter, emailUpdater, accounts, clerkFetcher)

	body := map[string]string{"email": rawEmail}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// The hash produced by HashPII(salt, rawEmail) must differ from
	// HashAccountID(rawEmail) (which is the unsalted SHA-256 of the email).
	// If they are equal, the salt is not being applied.
	want := unsaltedHash(rawEmail)
	if auditWriter.lastNewHash == want {
		t.Errorf(
			"new_value_hash appears to be the unsalted digest of the email "+
				"(HashAccountID behavior) — want HashPII(salt, email) which differs; "+
				"got unsalted hash %q",
			want,
		)
	}

	// Must not be raw PII.
	if auditWriter.lastNewHash == rawEmail {
		t.Error("new_value_hash must not be the raw email address")
	}
	// Must be exactly 16 hex chars.
	if len(auditWriter.lastNewHash) != 16 {
		t.Errorf("new_value_hash length: want 16, got %d", len(auditWriter.lastNewHash))
	}
}

// TestAccountProfileHandler_DisplayName_HashUsesSalt mirrors the email test
// for display_name.
func TestAccountProfileHandler_DisplayName_HashUsesSalt(t *testing.T) {
	const displayName = "SaltedDisplayName"

	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 7, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	body := map[string]string{"display_name": displayName}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// Must differ from the unsalted hash.
	want := unsaltedHash(displayName)
	if auditWriter.lastNewHash == want {
		t.Errorf(
			"new_value_hash for display_name appears to be unsalted; "+
				"want HashPII(salt, displayName), got unsalted hash %q",
			want,
		)
	}

	// Must not be raw PII.
	if auditWriter.lastNewHash == displayName {
		t.Error("new_value_hash must not be the raw display_name")
	}
	if len(auditWriter.lastNewHash) != 16 {
		t.Errorf("new_value_hash length: want 16, got %d", len(auditWriter.lastNewHash))
	}
}

// ─── Fix 4: Trusted email source (Clerk re-fetch) ────────────────────────────

// TestAccountProfileHandler_UsesTrustedEmailFromClerk verifies that when the
// client sends email="client@example.com" but Clerk's verified primary email is
// "verified@example.com", the handler writes "verified@example.com" to users.email
// (not the client-supplied value).
func TestAccountProfileHandler_UsesTrustedEmailFromClerk(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}
	clerkFetcher := &stubClerkEmailFetcher{email: "verified@example.com"}

	h := newProfileHandlerWithClerk(auditWriter, emailUpdater, accounts, clerkFetcher)

	// Client sends a different email than what Clerk has.
	body := map[string]string{"email": "client@example.com"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status: want 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// The email written to users.email must be the Clerk-verified value.
	if emailUpdater.lastEmail != "verified@example.com" {
		t.Errorf("UpdateEmail: want Clerk-verified %q, got %q",
			"verified@example.com", emailUpdater.lastEmail)
	}
}

// TestAccountProfileHandler_ClerkFetchError_Returns500 verifies that a failure
// to contact the Clerk Backend API returns 500 (fail closed — do not fall back
// to an untrusted client-supplied email).
func TestAccountProfileHandler_ClerkFetchError_Returns500(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}
	clerkFetcher := &stubClerkEmailFetcher{err: errors.New("clerk API unreachable")}

	h := newProfileHandlerWithClerk(auditWriter, emailUpdater, accounts, clerkFetcher)

	body := map[string]string{"email": "client@example.com"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500 on Clerk fetch error, got %d", rec.Code)
	}
	// Must not have written to the DB.
	if emailUpdater.callCount != 0 {
		t.Error("UpdateEmail must not be called when Clerk fetch fails")
	}
}

// TestAccountProfileHandler_ClerkFetchEmptyEmail_Returns500 verifies that an
// empty email returned from Clerk (no primary email configured) returns 500.
func TestAccountProfileHandler_ClerkFetchEmptyEmail_Returns500(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}
	clerkFetcher := &stubClerkEmailFetcher{email: ""} // empty = no primary address

	h := newProfileHandlerWithClerk(auditWriter, emailUpdater, accounts, clerkFetcher)

	body := map[string]string{"email": "client@example.com"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500 when Clerk returns empty email, got %d", rec.Code)
	}
}

// TestAccountProfileHandler_EmailFormatValidation_Rejected verifies that a
// malformed email in the request body is rejected 400 (defense-in-depth
// validation before the Clerk re-fetch path).
func TestAccountProfileHandler_EmailFormatValidation_Rejected(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}

	// No Clerk fetcher — tests the defense-in-depth validation path.
	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	body := map[string]string{"email": "notanemail"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400 for malformed email, got %d", rec.Code)
	}
}

// TestAccountProfileHandler_EmailLengthCap_Rejected verifies that an email
// longer than 254 chars is rejected with 400.
func TestAccountProfileHandler_EmailLengthCap_Rejected(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}

	h := newProfileHandler(auditWriter, emailUpdater, accounts)

	long := make([]byte, 250)
	for i := range long {
		long[i] = 'a'
	}
	// 250 'a' chars + "@x.com" = 256 chars > 254 cap
	body := map[string]string{"email": string(long) + "@x.com"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400 for email > 254 chars, got %d", rec.Code)
	}
}

// ─── Fix 1: Atomicity — partial-failure rollback ─────────────────────────────

// TestAccountProfileHandler_AtomicRollback_AuditFails verifies that when the
// audit INSERT fails the email UPDATE is NOT executed (transaction rolled back).
func TestAccountProfileHandler_AtomicRollback_AuditFails(t *testing.T) {
	auditWriter := &stubRectificationWriter{err: errors.New("db down")}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}
	clerkFetcher := &stubClerkEmailFetcher{email: "good@example.com"}

	h := newProfileHandlerWithClerk(auditWriter, emailUpdater, accounts, clerkFetcher)

	body := map[string]string{"email": "good@example.com"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500 on audit failure, got %d", rec.Code)
	}
	// Email must NOT have been updated — the transaction rolls back.
	if emailUpdater.callCount != 0 {
		t.Errorf(
			"UpdateEmail must not be called when audit INSERT fails (atomicity), "+
				"got %d calls",
			emailUpdater.callCount,
		)
	}
}

// TestAccountProfileHandler_AtomicRollback_EmailUpdateFails verifies that when
// UpdateEmail fails, the handler returns 500.
func TestAccountProfileHandler_AtomicRollback_EmailUpdateFails(t *testing.T) {
	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{err: errors.New("unique constraint")}
	accounts := &profileAccountLookup{accountID: 42, found: true}
	clerkFetcher := &stubClerkEmailFetcher{email: "taken@example.com"}

	h := newProfileHandlerWithClerk(auditWriter, emailUpdater, accounts, clerkFetcher)

	body := map[string]string{"email": "taken@example.com"}
	req := authedProfileRequest(t, body, 1)
	rec := httptest.NewRecorder()
	h.Patch(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500 on UpdateEmail failure, got %d", rec.Code)
	}
}

// ─── Existing handler tests (retained, updated for new constructor signature) ─

// TestAccountProfileHandler_EmailOnly_OK verifies a valid email-change body:
//   - returns 200 with updated_at
//   - writes one rectification audit row for the "email" field
//   - calls UpdateEmail with the Clerk-verified email
//   - new_value_hash is not raw PII
func TestAccountProfileHandler_EmailOnly_OK(t *testing.T) {
	const clerkEmail = "new@example.com"

	auditWriter := &stubRectificationWriter{}
	emailUpdater := &stubEmailUpdater{}
	accounts := &profileAccountLookup{accountID: 42, found: true}
	clerkFetcher := &stubClerkEmailFetcher{email: clerkEmail}

	h := newProfileHandlerWithClerk(auditWriter, emailUpdater, accounts, clerkFetcher)

	body := map[string]string{"email": clerkEmail}
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
	if auditWriter.lastNewHash == clerkEmail {
		t.Error("new_value_hash must not be the raw new email address")
	}
	// new_value_hash must be exactly 16 hex chars.
	if len(auditWriter.lastNewHash) != 16 {
		t.Errorf("new_value_hash length: want 16, got %d", len(auditWriter.lastNewHash))
	}

	// email sync called with Clerk-verified address.
	if emailUpdater.callCount != 1 {
		t.Errorf("UpdateEmail call count: want 1, got %d", emailUpdater.callCount)
	}
	if emailUpdater.lastEmail != clerkEmail {
		t.Errorf("UpdateEmail email: want %q, got %q", clerkEmail, emailUpdater.lastEmail)
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
	clerkFetcher := &stubClerkEmailFetcher{email: "both@example.com"}

	h := newProfileHandlerWithClerk(auditWriter, emailUpdater, accounts, clerkFetcher)

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
	clerkFetcher := &stubClerkEmailFetcher{email: "x@example.com"}

	h := newProfileHandlerWithClerk(auditWriter, emailUpdater, accounts, clerkFetcher)

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
	clerkFetcher := &stubClerkEmailFetcher{email: "x@example.com"}

	h := newProfileHandlerWithClerk(auditWriter, emailUpdater, accounts, clerkFetcher)

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
	clerkFetcher := &stubClerkEmailFetcher{email: rawEmail}

	h := newProfileHandlerWithClerk(auditWriter, emailUpdater, accounts, clerkFetcher)

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
