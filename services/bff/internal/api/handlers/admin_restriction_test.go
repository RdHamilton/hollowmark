package handlers_test

// ─── AdminRestrictionHandler unit tests ──────────────────────────────────────
// Ticket: #890 GDPR Art.18 Right to Restriction (ADR-055)
//
// These are unit tests — dependencies are test doubles.  The admin token gate
// is tested using middleware.AdminTokenAuth directly, matching production wiring.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
)

const testAdminToken = "test-admin-secret-token-xyz"

// buildAdminRestrictionHandler wires an AdminRestrictionHandler behind
// AdminTokenAuth middleware and a chi router (for path param extraction).
func buildAdminRestrictionHandler(t *testing.T, repo *stubRestrictionRepo, resolver *stubAccountResolver) http.Handler {
	t.Helper()

	h := handlers.NewAdminRestrictionHandler(repo, resolver)

	r := chi.NewRouter()
	authMiddl := bffmiddleware.AdminTokenAuth(testAdminToken)

	r.With(authMiddl).Post("/admin/account/{userID}/restrict-processing", h.AdminSetRestriction)
	r.With(authMiddl).Delete("/admin/account/{userID}/restrict-processing", h.AdminClearRestriction)

	return r
}

// TestAdminRestrictionHandler_Set_Returns200 verifies that a valid admin-token
// request returns 200 with {"status":"restricted"}.
func TestAdminRestrictionHandler_Set_Returns200(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 20, found: true}
	handler := buildAdminRestrictionHandler(t, repo, resolver)

	req := httptest.NewRequest(http.MethodPost, "/admin/account/5/restrict-processing", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "restricted" {
		t.Errorf("status: want %q, got %q", "restricted", body["status"])
	}
}

// TestAdminRestrictionHandler_Clear_Returns200 verifies that a valid admin-token
// DELETE request returns 200 with {"status":"unrestricted"}.
func TestAdminRestrictionHandler_Clear_Returns200(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 20, found: true}
	handler := buildAdminRestrictionHandler(t, repo, resolver)

	req := httptest.NewRequest(http.MethodDelete, "/admin/account/5/restrict-processing", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "unrestricted" {
		t.Errorf("status: want %q, got %q", "unrestricted", body["status"])
	}
}

// TestAdminRestrictionHandler_MissingToken_Returns401 verifies that requests
// without a Bearer token are rejected with 401.
func TestAdminRestrictionHandler_MissingToken_Returns401(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 20, found: true}
	handler := buildAdminRestrictionHandler(t, repo, resolver)

	req := httptest.NewRequest(http.MethodPost, "/admin/account/5/restrict-processing", nil)
	// No Authorization header.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// TestAdminRestrictionHandler_WrongToken_Returns401 verifies that requests with
// an incorrect Bearer token are rejected with 401.
func TestAdminRestrictionHandler_WrongToken_Returns401(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 20, found: true}
	handler := buildAdminRestrictionHandler(t, repo, resolver)

	req := httptest.NewRequest(http.MethodPost, "/admin/account/5/restrict-processing", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// TestAdminRestrictionHandler_Set_AuditRowHasAdminActor verifies that
// AdminSetRestriction writes an audit log row with actor='admin'.
func TestAdminRestrictionHandler_Set_AuditRowHasAdminActor(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 25, found: true}
	handler := buildAdminRestrictionHandler(t, repo, resolver)

	req := httptest.NewRequest(http.MethodPost, "/admin/account/6/restrict-processing", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(repo.auditRows) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(repo.auditRows))
	}
	if repo.auditRows[0].actor != "admin" {
		t.Errorf("actor: want %q, got %q", "admin", repo.auditRows[0].actor)
	}
	if repo.auditRows[0].action != "restricted" {
		t.Errorf("action: want %q, got %q", "restricted", repo.auditRows[0].action)
	}
}

// TestAdminRestrictionHandler_InvalidUserIDParam_Returns400 verifies that a
// non-integer userID path param returns 400.
func TestAdminRestrictionHandler_InvalidUserIDParam_Returns400(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 20, found: true}
	handler := buildAdminRestrictionHandler(t, repo, resolver)

	req := httptest.NewRequest(http.MethodPost, "/admin/account/not-a-number/restrict-processing", nil)
	req.Header.Set("Authorization", "Bearer "+testAdminToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}
