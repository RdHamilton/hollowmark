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
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ─── stubs ──────────────────────────────────────────────────────────────────

type consentAccountLookup struct {
	accountID int64
	found     bool
	err       error
}

func (c *consentAccountLookup) GetAccountIDByUserID(_ context.Context, _ int64) (int64, bool, error) {
	return c.accountID, c.found, c.err
}

type stubConsentWriter struct {
	lastEvent *repository.ConsentEvent
	err       error
}

func (s *stubConsentWriter) InsertConsentEvent(_ context.Context, e repository.ConsentEvent) error {
	s.lastEvent = &e
	return s.err
}

// ─── helpers ────────────────────────────────────────────────────────────────

func newConsentHandler(w *stubConsentWriter, a *consentAccountLookup) *handlers.ConsentHandler {
	cfg := handlers.ConsentConfig{
		TOSVersion:           "2026-06-10",
		PrivacyPolicyVersion: "2026-06-10",
	}
	return handlers.NewConsentHandler(w, a, cfg)
}

func authedConsentRequest(t *testing.T, body any, userID int64) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/consent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.1") // test IP
	return req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
}

// ─── handler tests ───────────────────────────────────────────────────────────

// TestConsentHandler_Signup_OK verifies a valid signup consent body returns 201
// and that the repo receives the correct ConsentEvent with server-canonical
// version strings (not whatever the client sent).
func TestConsentHandler_Signup_OK(t *testing.T) {
	writer := stubConsentWriter{}
	h := newConsentHandler(&writer, &consentAccountLookup{accountID: 42, found: true})

	body := map[string]string{
		"event_type":             "signup",
		"tos_version":            "client-supplied-should-be-ignored",
		"privacy_policy_version": "client-supplied-should-be-ignored",
	}
	req := authedConsentRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.RecordConsent(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status: want 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	if writer.lastEvent == nil {
		t.Fatal("InsertConsentEvent was not called")
	}

	if writer.lastEvent.EventType != "signup" {
		t.Errorf("EventType: want %q, got %q", "signup", writer.lastEvent.EventType)
	}
	if writer.lastEvent.AccountID != 42 {
		t.Errorf("AccountID: want 42, got %d", writer.lastEvent.AccountID)
	}

	// Server-canonical version — client value must be overridden.
	if writer.lastEvent.TOSVersion == nil || *writer.lastEvent.TOSVersion != "2026-06-10" {
		t.Errorf("TOSVersion: want %q (server-canonical), got %v", "2026-06-10", writer.lastEvent.TOSVersion)
	}
	if writer.lastEvent.PrivacyPolicyVersion == nil || *writer.lastEvent.PrivacyPolicyVersion != "2026-06-10" {
		t.Errorf("PrivacyPolicyVersion: want %q (server-canonical), got %v", "2026-06-10", writer.lastEvent.PrivacyPolicyVersion)
	}

	// IP must be hashed — never raw.
	if writer.lastEvent.IPAddressHash == nil {
		t.Error("IPAddressHash should be set for signup events")
	}
	rawIP := "203.0.113.1"
	if writer.lastEvent.IPAddressHash != nil && *writer.lastEvent.IPAddressHash == rawIP {
		t.Error("IPAddressHash must not be the raw IP address")
	}
	// Hash must be 16 hex chars (SHA-256 hex[:16]).
	if writer.lastEvent.IPAddressHash != nil && len(*writer.lastEvent.IPAddressHash) != 16 {
		t.Errorf("IPAddressHash: want 16 hex chars, got %d (%q)", len(*writer.lastEvent.IPAddressHash), *writer.lastEvent.IPAddressHash)
	}
}

// TestConsentHandler_CookieAccept_OK verifies cookie_accept events succeed
// without requiring tos_version.
func TestConsentHandler_CookieAccept_OK(t *testing.T) {
	writer := stubConsentWriter{}
	h := newConsentHandler(&writer, &consentAccountLookup{accountID: 7, found: true})

	body := map[string]string{"event_type": "cookie_accept"}
	req := authedConsentRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.RecordConsent(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("status: want 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}
}

// TestConsentHandler_Signup_AcceptsWithoutClientVersion verifies that a signup
// event body with no client-supplied version fields is accepted — the server
// fills in canonical versions from config.
func TestConsentHandler_Signup_AcceptsWithoutClientVersion(t *testing.T) {
	writer := stubConsentWriter{}
	h := newConsentHandler(&writer, &consentAccountLookup{accountID: 42, found: true})

	// Client sends no version fields — server should fill them in.
	body := map[string]string{"event_type": "signup"}
	req := authedConsentRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.RecordConsent(rec, req)

	// Should succeed — server has canonical versions from config.
	if rec.Code != http.StatusCreated {
		t.Errorf("status: want 201, got %d (body: %s)", rec.Code, rec.Body.String())
	}
	if writer.lastEvent == nil {
		t.Fatal("InsertConsentEvent was not called")
	}
	// Server fills in versions from config even when client omits them.
	if writer.lastEvent.TOSVersion == nil || *writer.lastEvent.TOSVersion != "2026-06-10" {
		t.Errorf("TOSVersion: want server-canonical %q, got %v", "2026-06-10", writer.lastEvent.TOSVersion)
	}
}

// TestConsentHandler_UnknownEventType verifies that an unknown event_type
// value is rejected with 400 Bad Request.
func TestConsentHandler_UnknownEventType(t *testing.T) {
	writer := stubConsentWriter{}
	h := newConsentHandler(&writer, &consentAccountLookup{accountID: 42, found: true})

	body := map[string]string{"event_type": "definitely_not_valid"}
	req := authedConsentRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.RecordConsent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", rec.Code)
	}
}

// TestConsentHandler_Unauthorized verifies that a request without a Clerk
// user ID in context returns 401.
func TestConsentHandler_Unauthorized(t *testing.T) {
	writer := stubConsentWriter{}
	h := newConsentHandler(&writer, &consentAccountLookup{accountID: 42, found: true})

	body := map[string]string{"event_type": "signup"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/consent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	// No context user ID — simulate unauthenticated request.
	rec := httptest.NewRecorder()

	h.RecordConsent(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status: want 401, got %d", rec.Code)
	}
}

// TestConsentHandler_AccountNotFound verifies that a Clerk user with no
// accounts row returns 404.
func TestConsentHandler_AccountNotFound(t *testing.T) {
	writer := stubConsentWriter{}
	h := newConsentHandler(&writer, &consentAccountLookup{found: false})

	body := map[string]string{"event_type": "signup"}
	req := authedConsentRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.RecordConsent(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: want 404, got %d", rec.Code)
	}
}

// TestConsentHandler_RepoError verifies that a repo INSERT error returns 500.
func TestConsentHandler_RepoError(t *testing.T) {
	writer := stubConsentWriter{err: errors.New("db down")}
	h := newConsentHandler(&writer, &consentAccountLookup{accountID: 42, found: true})

	body := map[string]string{"event_type": "signup"}
	req := authedConsentRequest(t, body, 1)
	rec := httptest.NewRecorder()

	h.RecordConsent(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: want 500, got %d", rec.Code)
	}
}

// TestConsentHandler_InvalidJSON verifies that a malformed JSON body returns 400.
func TestConsentHandler_InvalidJSON(t *testing.T) {
	writer := stubConsentWriter{}
	h := newConsentHandler(&writer, &consentAccountLookup{accountID: 42, found: true})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/consent", bytes.NewBufferString(`{not valid json`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(bffmiddleware.WithUserID(req.Context(), 1))
	rec := httptest.NewRecorder()

	h.RecordConsent(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: want 400, got %d", rec.Code)
	}
}

// TestConsentHandler_AllAllowedEventTypes verifies every event type in the
// allowlist is accepted.
func TestConsentHandler_AllAllowedEventTypes(t *testing.T) {
	allowedTypes := []string{
		"signup",
		"coppa_gate",
		"cookie_accept",
		"cookie_decline",
		"install_dialog",
	}

	for _, et := range allowedTypes {
		et := et
		t.Run(et, func(t *testing.T) {
			writer := stubConsentWriter{}
			h := newConsentHandler(&writer, &consentAccountLookup{accountID: 42, found: true})

			body := map[string]string{"event_type": et}
			req := authedConsentRequest(t, body, 1)
			rec := httptest.NewRecorder()

			h.RecordConsent(rec, req)

			if rec.Code != http.StatusCreated {
				t.Errorf("[%s] status: want 201, got %d (body: %s)", et, rec.Code, rec.Body.String())
			}
		})
	}
}
