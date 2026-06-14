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

// stubDaemonHealthChecker is a test double for DaemonHealthChecker.
// It satisfies both HasRecentEventByUserID and GetLatestHeartbeatAuthStatus.
type stubDaemonHealthChecker struct {
	connected  bool
	err        error
	authStatus string
	authErr    error
}

func (s *stubDaemonHealthChecker) HasRecentEventByUserID(_ context.Context, _ int64, _ time.Duration) (bool, error) {
	return s.connected, s.err
}

func (s *stubDaemonHealthChecker) GetLatestHeartbeatAuthStatus(_ context.Context, _ int64) (string, error) {
	if s.authErr != nil {
		return "unknown", s.authErr
	}
	if s.authStatus == "" {
		return "unknown", nil
	}
	return s.authStatus, nil
}

// authedHealthHandler injects userID into context and delegates to GetDaemonHealth.
func authedHealthHandler(h *handlers.DaemonHealthHandler, userID int64) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := middleware.WithUserID(r.Context(), userID)
		r = r.WithContext(ctx)
		h.GetDaemonHealth(w, r)
	})
}

func TestGetDaemonHealth_Connected(t *testing.T) {
	checker := &stubDaemonHealthChecker{connected: true}
	h := handlers.NewDaemonHealthHandler(checker)
	handler := authedHealthHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/daemon", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["status"] != "connected" {
		t.Errorf("expected status=connected, got %q", resp["status"])
	}
}

func TestGetDaemonHealth_Disconnected(t *testing.T) {
	checker := &stubDaemonHealthChecker{connected: false}
	h := handlers.NewDaemonHealthHandler(checker)
	handler := authedHealthHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/daemon", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["status"] != "disconnected" {
		t.Errorf("expected status=disconnected, got %q", resp["status"])
	}
}

func TestGetDaemonHealth_DBError_ReturnsDisconnected(t *testing.T) {
	// When the DB errors out we still return 200 with "disconnected" — the
	// frontend should degrade gracefully and not show a hard error.
	checker := &stubDaemonHealthChecker{err: errors.New("db unavailable")}
	h := handlers.NewDaemonHealthHandler(checker)
	handler := authedHealthHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/daemon", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["status"] != "disconnected" {
		t.Errorf("expected status=disconnected on DB error, got %q", resp["status"])
	}
}

func TestGetDaemonHealth_Unauthorized(t *testing.T) {
	checker := &stubDaemonHealthChecker{connected: true}
	h := handlers.NewDaemonHealthHandler(checker)
	// No user ID injected — simulate missing auth.

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/daemon", nil)
	rr := httptest.NewRecorder()
	h.GetDaemonHealth(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestGetDaemonHealth_ContentType(t *testing.T) {
	checker := &stubDaemonHealthChecker{connected: true}
	h := handlers.NewDaemonHealthHandler(checker)
	handler := authedHealthHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/daemon", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("expected Content-Type=application/json, got %q", ct)
	}
}

// ---------------------------------------------------------------------------
// auth_status field tests (#144)
// ---------------------------------------------------------------------------

// daemonHealthFull is a typed response struct for tests that assert auth_status.
type daemonHealthFull struct {
	Status     string `json:"status"`
	AuthStatus string `json:"auth_status"`
}

func parseHealthFull(t *testing.T, rr *httptest.ResponseRecorder) daemonHealthFull {
	t.Helper()
	var resp daemonHealthFull
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal daemonHealthFull: %v", err)
	}
	return resp
}

// TestGetDaemonHealth_AuthStatus_Connected_Authenticated verifies that a
// connected daemon with auth_status="authenticated" returns both fields.
func TestGetDaemonHealth_AuthStatus_Connected_Authenticated(t *testing.T) {
	checker := &stubDaemonHealthChecker{
		connected:  true,
		authStatus: "authenticated",
	}
	h := handlers.NewDaemonHealthHandler(checker)
	handler := authedHealthHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/daemon", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	resp := parseHealthFull(t, rr)
	if resp.Status != "connected" {
		t.Errorf("status: want connected, got %q", resp.Status)
	}
	if resp.AuthStatus != "authenticated" {
		t.Errorf("auth_status: want authenticated, got %q", resp.AuthStatus)
	}
}

// TestGetDaemonHealth_AuthStatus_Disconnected_KeychainError verifies that a
// disconnected daemon with a keychain error returns both fields correctly.
func TestGetDaemonHealth_AuthStatus_Disconnected_KeychainError(t *testing.T) {
	checker := &stubDaemonHealthChecker{
		connected:  false,
		authStatus: "keychain_error",
	}
	h := handlers.NewDaemonHealthHandler(checker)
	handler := authedHealthHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/daemon", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	resp := parseHealthFull(t, rr)
	if resp.Status != "disconnected" {
		t.Errorf("status: want disconnected, got %q", resp.Status)
	}
	if resp.AuthStatus != "keychain_error" {
		t.Errorf("auth_status: want keychain_error, got %q", resp.AuthStatus)
	}
}

// TestGetDaemonHealth_AuthStatus_RepoError_FallsBackToUnknown verifies that
// when GetLatestHeartbeatAuthStatus returns an error, auth_status is "unknown"
// in the response (never a 500 — the error is absorbed, Ray's verdict §2).
func TestGetDaemonHealth_AuthStatus_RepoError_FallsBackToUnknown(t *testing.T) {
	checker := &stubDaemonHealthChecker{
		connected: true,
		authErr:   errors.New("db timeout"),
	}
	h := handlers.NewDaemonHealthHandler(checker)
	handler := authedHealthHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/daemon", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — error must not surface as HTTP 5xx", rr.Code)
	}
	resp := parseHealthFull(t, rr)
	if resp.AuthStatus != "unknown" {
		t.Errorf("auth_status: want unknown on repo error, got %q", resp.AuthStatus)
	}
}

// TestGetDaemonHealth_AuthStatus_NoHeartbeat_ReturnsUnknown verifies that
// when GetLatestHeartbeatAuthStatus returns "" (no heartbeat rows yet),
// the response carries auth_status="unknown".
func TestGetDaemonHealth_AuthStatus_NoHeartbeat_ReturnsUnknown(t *testing.T) {
	// authStatus "" → stub returns "unknown", nil
	checker := &stubDaemonHealthChecker{
		connected:  false,
		authStatus: "",
	}
	h := handlers.NewDaemonHealthHandler(checker)
	handler := authedHealthHandler(h, 1)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health/daemon", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	resp := parseHealthFull(t, rr)
	if resp.AuthStatus != "unknown" {
		t.Errorf("auth_status: want unknown when no heartbeat rows, got %q", resp.AuthStatus)
	}
}
