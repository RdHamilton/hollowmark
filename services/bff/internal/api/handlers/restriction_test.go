package handlers_test

// ─── RestrictionHandler unit tests ───────────────────────────────────────────
// Ticket: #890 GDPR Art.18 Right to Restriction (ADR-055)
//
// These are unit tests — dependencies are test doubles.  Integration tests for
// the underlying repository live in restriction_repo_test.go.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	"github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
)

// ─── test doubles ─────────────────────────────────────────────────────────────

// stubRestrictionRepo implements the restrictionWriter interface used by
// RestrictionHandler.
type stubRestrictionRepo struct {
	setErr    error
	clearErr  error
	auditErr  error
	setCalls  int
	clearCall int
	auditRows []auditRow
}

type auditRow struct {
	userID    int64
	accountID int64
	action    string
	actor     string
}

func (s *stubRestrictionRepo) SetProcessingRestriction(_ context.Context, _ int64) error {
	s.setCalls++
	return s.setErr
}

func (s *stubRestrictionRepo) ClearProcessingRestriction(_ context.Context, _ int64) error {
	s.clearCall++
	return s.clearErr
}

func (s *stubRestrictionRepo) InsertAuditLogEntry(_ context.Context, userID, accountID int64, action, actor string) error {
	s.auditRows = append(s.auditRows, auditRow{userID: userID, accountID: accountID, action: action, actor: actor})
	return s.auditErr
}

// stubAccountResolver implements the accountIDResolver interface used by
// RestrictionHandler.
type stubAccountResolver struct {
	accountID int64
	found     bool
	err       error
}

func (s *stubAccountResolver) GetAccountIDByUserID(_ context.Context, _ int64) (int64, bool, error) {
	return s.accountID, s.found, s.err
}

// buildRestrictionHandler constructs a RestrictionHandler with Clerk auth
// context set (simulating the ClerkAuthMiddleware) for use in unit tests.
func buildRestrictionHandler(t *testing.T, clerkUserID string, userID int64, repo *stubRestrictionRepo, resolver *stubAccountResolver) (http.Handler, http.Handler) {
	t.Helper()

	setH := handlers.NewRestrictionHandler(repo, resolver)
	clearH := handlers.NewRestrictionHandler(repo, resolver)

	// Compose Clerk auth + user resolver middleware, simulating what BuildRouter does.
	setHandler := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = middleware.WithClerkUserID(r, clerkUserID)
			r = r.WithContext(middleware.WithUserID(r.Context(), userID))
			next.ServeHTTP(w, r)
		})
	}(http.HandlerFunc(setH.SetRestriction))

	clearHandler := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = middleware.WithClerkUserID(r, clerkUserID)
			r = r.WithContext(middleware.WithUserID(r.Context(), userID))
			next.ServeHTTP(w, r)
		})
	}(http.HandlerFunc(clearH.ClearRestriction))

	return setHandler, clearHandler
}

// ─── tests ────────────────────────────────────────────────────────────────────

// TestRestrictionHandler_Set_Returns200 verifies that POST /api/v1/account/restrict-processing
// returns 200 with {"status":"restricted"} on success.
func TestRestrictionHandler_Set_Returns200(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 10, found: true}
	setH, _ := buildRestrictionHandler(t, "clerk_user_1", 1, repo, resolver)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/restrict-processing", nil)
	rr := httptest.NewRecorder()
	setH.ServeHTTP(rr, req)

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

// TestRestrictionHandler_Clear_Returns200 verifies that DELETE /api/v1/account/restrict-processing
// returns 200 with {"status":"unrestricted"} on success.
func TestRestrictionHandler_Clear_Returns200(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 10, found: true}
	_, clearH := buildRestrictionHandler(t, "clerk_user_1", 1, repo, resolver)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/account/restrict-processing", nil)
	rr := httptest.NewRecorder()
	clearH.ServeHTTP(rr, req)

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

// TestRestrictionHandler_Set_WritesAuditRow verifies that SetRestriction writes
// an audit log row with action='restricted' and actor='user'.
func TestRestrictionHandler_Set_WritesAuditRow(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 42, found: true}
	setH, _ := buildRestrictionHandler(t, "clerk_user_2", 7, repo, resolver)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/restrict-processing", nil)
	rr := httptest.NewRecorder()
	setH.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(repo.auditRows) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(repo.auditRows))
	}
	row := repo.auditRows[0]
	if row.action != "restricted" {
		t.Errorf("action: want %q, got %q", "restricted", row.action)
	}
	if row.actor != "user" {
		t.Errorf("actor: want %q, got %q", "user", row.actor)
	}
	if row.accountID != 42 {
		t.Errorf("accountID: want 42, got %d", row.accountID)
	}
}

// TestRestrictionHandler_Clear_WritesAuditRow verifies that ClearRestriction
// writes an audit log row with action='unrestricted' and actor='user'.
func TestRestrictionHandler_Clear_WritesAuditRow(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 43, found: true}
	_, clearH := buildRestrictionHandler(t, "clerk_user_3", 8, repo, resolver)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/account/restrict-processing", nil)
	rr := httptest.NewRecorder()
	clearH.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if len(repo.auditRows) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(repo.auditRows))
	}
	row := repo.auditRows[0]
	if row.action != "unrestricted" {
		t.Errorf("action: want %q, got %q", "unrestricted", row.action)
	}
	if row.actor != "user" {
		t.Errorf("actor: want %q, got %q", "user", row.actor)
	}
}

// TestRestrictionHandler_MissingAuth_Returns401 verifies that requests without
// a valid Clerk user ID receive 401.
func TestRestrictionHandler_MissingAuth_Returns401(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 10, found: true}

	// Build handler WITHOUT injecting Clerk user ID context — simulates
	// missing/bypassed ClerkAuthMiddleware.
	h := handlers.NewRestrictionHandler(repo, resolver)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/restrict-processing", nil)
	rr := httptest.NewRecorder()
	h.SetRestriction(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// TestRestrictionHandler_AccountNotFound_Returns404 verifies that 404 is
// returned when the user has no account row yet (first-run state).
func TestRestrictionHandler_AccountNotFound_Returns404(t *testing.T) {
	repo := &stubRestrictionRepo{}
	resolver := &stubAccountResolver{accountID: 0, found: false}
	setH, _ := buildRestrictionHandler(t, "clerk_user_4", 9, repo, resolver)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/restrict-processing", nil)
	rr := httptest.NewRecorder()
	setH.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// TestRestrictionHandler_RepoError_Returns500 verifies that a repository error
// surfaces as 500 rather than silently succeeding.
func TestRestrictionHandler_RepoError_Returns500(t *testing.T) {
	repo := &stubRestrictionRepo{setErr: errors.New("db error")}
	resolver := &stubAccountResolver{accountID: 10, found: true}
	setH, _ := buildRestrictionHandler(t, "clerk_user_5", 11, repo, resolver)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/restrict-processing", nil)
	rr := httptest.NewRecorder()
	setH.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}
