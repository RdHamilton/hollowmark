package main

// boot_signal_router_test.go — router-level auth-isolation tests for
// POST /api/v1/boot-signal (ticket #1212, ADR-077).
//
// Two invariants:
//   1. The boot-signal route is reachable WITHOUT any auth token (AC7).
//   2. Neighboring auth-protected routes are NOT affected by the exemption (AC7
//      surgical — adding a public route must not accidentally bypass auth
//      on adjacent protected routes).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
)

// TestRouter_BootSignal_ReachableWithoutAuth verifies that
// POST /api/v1/boot-signal is reachable without any Authorization header
// and returns 204 (not 401, 403, 404, or 405).
func TestRouter_BootSignal_ReachableWithoutAuth(t *testing.T) {
	cfg := minimalConfig()
	cfg.AnalyticsPIISalt = "router-test-pii-salt"
	deps := depsNoAuth(t)

	r := BuildRouter(cfg, deps)

	payload := map[string]string{
		"failure_type": "network",
		"environment":  "production",
		"app_version":  "v0.4.3",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/boot-signal", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatal("POST /api/v1/boot-signal: got 404 — route is not mounted")
	}
	if rr.Code == http.StatusMethodNotAllowed {
		t.Fatal("POST /api/v1/boot-signal: got 405 — route is registered as wrong method")
	}
	if rr.Code == http.StatusUnauthorized {
		t.Fatal("POST /api/v1/boot-signal: got 401 — route must be public (no Clerk auth required)")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("POST /api/v1/boot-signal without auth: want 204, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
}

// TestRouter_BootSignal_AuthRouteNeighbor_StillRequiresAuth verifies that
// adding POST /api/v1/boot-signal as a public route did not accidentally
// remove auth enforcement from neighboring Clerk-protected routes.
//
// Uses GET /api/v1/daemons as the neighboring route — it requires a valid
// Clerk JWT and must still return 401 without one.
func TestRouter_BootSignal_AuthRouteNeighbor_StillRequiresAuth(t *testing.T) {
	deps := depsWithClerk(t)
	deps.DaemonsListHandler = handlers.NewDaemonsListHandler(&stubDaemonsRepo{})

	r := BuildRouter(minimalConfig(), deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemons", nil)
	// No Authorization header.
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/daemons without auth token (neighbor of boot-signal): want 401, got %d", rr.Code)
	}
}
