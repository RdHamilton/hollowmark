package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/api/sse"
	"github.com/RdHamilton/hollowmark/services/bff/internal/config"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	contract "github.com/RdHamilton/hollowmark/services/contract"
	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/clerktest"
)

// stubUserRepo is a ClerkUserLookup stub that always returns a fixed user (id=1).
type stubUserRepo struct {
	failWith error
}

func (s *stubUserRepo) UpsertByClerkUserID(_ context.Context, _ string) (*repository.User, error) {
	if s.failWith != nil {
		return nil, s.failWith
	}

	clerkID := "user_stub"

	return &repository.User{ID: 1, Email: "stub@clerk.local", ClerkUserID: &clerkID, SubscriptionTier: "free"}, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Test helpers
// ──────────────────────────────────────────────────────────────────────────────

// jwksForKey builds a minimal JWKS document from an RSA public key.
func jwksForKey(kid string, pub crypto.PublicKey) string {
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		panic("jwksForKey: not *rsa.PublicKey")
	}

	n := base64.RawURLEncoding.EncodeToString(rsaPub.N.Bytes())
	eBytes := big.NewInt(int64(rsaPub.E)).Bytes()
	e := base64.RawURLEncoding.EncodeToString(eBytes)

	return fmt.Sprintf(
		`{"keys":[{"use":"sig","kty":"RSA","kid":"%s","alg":"RS256","n":"%s","e":"%s"}]}`,
		kid, n, e,
	)
}

// setupClerkBackend starts a mock JWKS server and points the Clerk SDK at it.
// Returns a valid signed JWT string. The server is shut down via t.Cleanup.
func setupClerkBackend(t *testing.T) string {
	t.Helper()

	// Unique kid per test prevents JWKS cache collisions between tests.
	kid := "router-test-kid-" + t.Name()
	now := time.Now()
	claims := map[string]any{
		"sub": "user_router_test",
		"sid": "sess_router_test",
		"iss": "https://clerk.test",
		"iat": now.Add(-1 * time.Minute).Unix(),
		"nbf": now.Add(-1 * time.Minute).Unix(),
		"exp": now.Add(1 * time.Hour).Unix(),
	}

	jwt, pubKey := clerktest.GenerateJWT(t, claims, kid)
	jwks := jwksForKey(kid, pubKey)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/jwks" && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(jwks))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	clerk.SetBackend(clerk.NewBackend(&clerk.BackendConfig{
		HTTPClient: srv.Client(),
		URL:        &srv.URL,
	}))

	return jwt
}

// minimalConfig returns a non-production Config with no DB or secrets required.
func minimalConfig() *config.Config {
	return &config.Config{
		Env:                                 "development",
		AllowedOrigins:                      []string{"*"},
		DraftRatingsStalenessThresholdHours: 48,
		DaemonLatestVersion:                 "0.1.0",
	}
}

// noopBroadcaster satisfies handlers.EventBroadcaster without doing anything.
type noopBroadcaster struct{}

func (n *noopBroadcaster) BroadcastDaemonEvent(_ int64, _ contract.DaemonEvent) {}

// stubDraftGetter is a DraftRatingsGetter that always returns (nil, nil).
type stubDraftGetter struct{}

func (s *stubDraftGetter) GetRatings(_ context.Context, _, _ string) (*repository.DraftRatingsResult, error) {
	return nil, nil
}

// depsWithClerk builds minimal RouterDeps with ClerkAuthMiddl and
// ClerkUserResolver set (stub repo returns user id=1).
func depsWithClerk(t *testing.T) RouterDeps {
	t.Helper()

	broker := sse.NewWithHeartbeat(0)
	ingest := handlers.NewIngestHandler(&noopBroadcaster{})

	return RouterDeps{
		Broker:            broker,
		IngestHandler:     ingest,
		ClerkAuthMiddl:    bffmiddleware.RequireClerkAuth("test-secret-key"),
		ClerkUserResolver: bffmiddleware.ClerkUserResolver(&stubUserRepo{}),
	}
}

// depsNoAuth builds minimal RouterDeps with no auth middleware configured.
func depsNoAuth(t *testing.T) RouterDeps {
	t.Helper()

	broker := sse.NewWithHeartbeat(0)
	ingest := handlers.NewIngestHandler(&noopBroadcaster{})

	return RouterDeps{
		Broker:        broker,
		IngestHandler: ingest,
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Public routes
// ──────────────────────────────────────────────────────────────────────────────

// TestRouter_Health_IsPublic verifies /health is accessible without any auth.
func TestRouter_Health_IsPublic(t *testing.T) {
	r := BuildRouter(minimalConfig(), depsNoAuth(t))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /health: want 200, got %d", rr.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode /health body: %v", err)
	}

	if body["status"] != "ok" {
		t.Errorf("/health status: want \"ok\", got %q", body["status"])
	}
}

// TestRouter_DaemonVersion_IsPublic verifies daemon version endpoint requires no auth.
func TestRouter_DaemonVersion_IsPublic(t *testing.T) {
	r := BuildRouter(minimalConfig(), depsNoAuth(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemon/version", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/daemon/version: want 200, got %d", rr.Code)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// SSE endpoint (GET /api/v1/events) — Clerk-protected
// ──────────────────────────────────────────────────────────────────────────────

func TestRouter_SSE_Returns401_WithoutToken(t *testing.T) {
	r := BuildRouter(minimalConfig(), depsWithClerk(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/events no token: want 401, got %d", rr.Code)
	}
}

func TestRouter_SSE_Returns401_WithInvalidToken(t *testing.T) {
	r := BuildRouter(minimalConfig(), depsWithClerk(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer not.a.valid.jwt.at.all")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/events bad token: want 401, got %d", rr.Code)
	}
}

func TestRouter_SSE_401Body_IsJSON(t *testing.T) {
	r := BuildRouter(minimalConfig(), depsWithClerk(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: want application/json, got %q", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("401 body not valid JSON: %v", err)
	}

	if body["error"] != "unauthorized" {
		t.Errorf("body[\"error\"]: want \"unauthorized\", got %q", body["error"])
	}
}

// TestRouter_SSE_ValidJWT_PassesClerkMiddleware verifies that a valid Clerk JWT
// is accepted by RequireClerkAuth. Uses httptest.NewServer + a real HTTP client
// so that http.Client.Do returns once response headers arrive — without blocking
// on the SSE stream. httptest.NewRecorder cannot be used here because
// r.ServeHTTP would never return (the SSE handler blocks on ctx.Done()).
func TestRouter_SSE_ValidJWT_PassesClerkMiddleware(t *testing.T) {
	jwt := setupClerkBackend(t)

	r := BuildRouter(minimalConfig(), depsWithClerk(t))

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/events: %v", err)
	}
	defer resp.Body.Close()

	// With a valid JWT the Clerk middleware must NOT emit {"error":"unauthorized"}.
	if resp.StatusCode == http.StatusUnauthorized {
		var body map[string]string
		if decErr := json.NewDecoder(resp.Body).Decode(&body); decErr == nil && body["error"] == "unauthorized" {
			t.Fatal("valid JWT: Clerk middleware rejected the token — it should have passed through")
		}
		t.Fatalf("unexpected 401 from /api/v1/events with valid JWT")
	}
	// Deferred cancel() terminates the SSE connection; ts.Close() then completes cleanly.
}

// ──────────────────────────────────────────────────────────────────────────────
// Draft ratings endpoint — Clerk-protected
// ──────────────────────────────────────────────────────────────────────────────

func TestRouter_DraftRatings_Returns401_WithoutToken(t *testing.T) {
	cfg := minimalConfig()
	deps := depsWithClerk(t)
	deps.DraftRatingsHandler = handlers.NewDraftRatingsHandler(&stubDraftGetter{}, cfg)

	r := BuildRouter(cfg, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/draft-ratings/DSK/PremierDraft", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/draft-ratings no token: want 401, got %d", rr.Code)
	}
}

func TestRouter_DraftRatings_Returns401_WithInvalidToken(t *testing.T) {
	cfg := minimalConfig()
	deps := depsWithClerk(t)
	deps.DraftRatingsHandler = handlers.NewDraftRatingsHandler(&stubDraftGetter{}, cfg)

	r := BuildRouter(cfg, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/draft-ratings/DSK/PremierDraft", nil)
	req.Header.Set("Authorization", "Bearer tampered.token.value")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/draft-ratings bad token: want 401, got %d", rr.Code)
	}
}

func TestRouter_DraftRatings_ValidJWT_PassesClerkMiddleware(t *testing.T) {
	jwt := setupClerkBackend(t)

	cfg := minimalConfig()
	deps := depsWithClerk(t)
	deps.DraftRatingsHandler = handlers.NewDraftRatingsHandler(&stubDraftGetter{}, cfg)

	r := BuildRouter(cfg, deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/draft-ratings/DSK/PremierDraft", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	// Clerk middleware passes → handler returns 404 (stub returns nil result).
	// What must NOT happen: Clerk middleware rejects with {"error":"unauthorized"}.
	if rr.Code == http.StatusUnauthorized {
		var body map[string]string
		if err := json.NewDecoder(rr.Body).Decode(&body); err == nil {
			if body["error"] == "unauthorized" {
				t.Fatal("valid JWT: Clerk middleware rejected the token — it should have passed through")
			}
		}
	}

	// Stub returns nil → handler returns 404.
	if rr.Code != http.StatusNotFound {
		t.Fatalf("valid JWT with nil stub: want 404 from handler, got %d", rr.Code)
	}
}

// TestRouter_DraftRatings_RouteAbsent_WhenHandlerNil verifies that when no
// DraftRatingsHandler is configured (no DB), chi returns 404 for that route —
// no panic and no unexpected error.
func TestRouter_DraftRatings_RouteAbsent_WhenHandlerNil(t *testing.T) {
	deps := depsWithClerk(t)
	// DraftRatingsHandler intentionally left nil.

	r := BuildRouter(minimalConfig(), deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/draft-ratings/DSK/PremierDraft", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("unregistered route: want 404, got %d", rr.Code)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// No-auth degraded mode
// ──────────────────────────────────────────────────────────────────────────────

// TestRouter_SSE_Returns503_WhenNoAuthConfigured verifies the 503 fallback when
// neither Clerk nor APIKey auth is configured.
func TestRouter_SSE_Returns503_WhenNoAuthConfigured(t *testing.T) {
	r := BuildRouter(minimalConfig(), depsNoAuth(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /api/v1/events no auth: want 503, got %d", rr.Code)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// ClerkUserResolver middleware tests
// ──────────────────────────────────────────────────────────────────────────────

// TestRouter_SSE_ValidJWT_WithResolver_ReachesHandler verifies that a valid
// Clerk JWT combined with a working ClerkUserResolver stub resolves the int64
// user ID and reaches the SSE handler (200 SSE response).
//
// Uses httptest.NewServer so http.Client.Do returns once headers arrive —
// avoiding the infinite SSE stream block that httptest.NewRecorder would cause.
func TestRouter_SSE_ValidJWT_WithResolver_ReachesHandler(t *testing.T) {
	jwt := setupClerkBackend(t)

	deps := depsWithClerk(t) // includes stubUserRepo returning id=1
	r := BuildRouter(minimalConfig(), deps)

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/events: %v", err)
	}
	defer resp.Body.Close()

	// A 401 from Clerk middleware indicates a token problem — must not happen.
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/events with resolver: unexpected 401")
	}

	// A JSON 500 from the resolver indicates the stub repo failed — must not happen.
	if resp.StatusCode == http.StatusInternalServerError {
		var errBody map[string]string
		if decErr := json.NewDecoder(resp.Body).Decode(&errBody); decErr == nil && errBody["error"] == "internal server error" {
			t.Fatalf("GET /api/v1/events with resolver: resolver returned 500")
		}
	}
	// Deferred cancel() terminates the SSE connection; ts.Close() then completes cleanly.
}

// TestRouter_SSE_ValidJWT_ResolverDBError_Returns500 verifies that when the
// user repo returns an error (e.g. DB down), the resolver middleware returns 500.
func TestRouter_SSE_ValidJWT_ResolverDBError_Returns500(t *testing.T) {
	jwt := setupClerkBackend(t)

	broker := sse.NewWithHeartbeat(0)
	ingest := handlers.NewIngestHandler(&noopBroadcaster{})

	deps := RouterDeps{
		Broker:            broker,
		IngestHandler:     ingest,
		ClerkAuthMiddl:    bffmiddleware.RequireClerkAuth("test-secret-key"),
		ClerkUserResolver: bffmiddleware.ClerkUserResolver(&stubUserRepo{failWith: context.DeadlineExceeded}),
	}

	r := BuildRouter(minimalConfig(), deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("resolver DB error: want 500, got %d", rr.Code)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// SSE endpoint — ClerkAuthSSEMiddl (?token= query-param path) — issue #778
// ──────────────────────────────────────────────────────────────────────────────
//
// The browser EventSource API cannot set Authorization headers, so the SPA
// appends the Clerk JWT as ?token= on every (re)connect.  These tests exercise
// the ClerkAuthSSEMiddl path (RequireClerkAuthForSSE) through the full router.

// depsWithClerkSSE builds RouterDeps with both ClerkAuthMiddl and
// ClerkAuthSSEMiddl populated (the production configuration).
func depsWithClerkSSE(t *testing.T) RouterDeps {
	t.Helper()
	broker := sse.NewWithHeartbeat(0)
	ingest := handlers.NewIngestHandler(&noopBroadcaster{})
	return RouterDeps{
		Broker:            broker,
		IngestHandler:     ingest,
		ClerkAuthMiddl:    bffmiddleware.RequireClerkAuth("test-secret-key"),
		ClerkAuthSSEMiddl: bffmiddleware.RequireClerkAuthForSSE("test-secret-key"),
		ClerkUserResolver: bffmiddleware.ClerkUserResolver(&stubUserRepo{}),
	}
}

// TestRouter_SSE_ClerkAuthSSEMiddl_Returns401_NoToken verifies that when
// ClerkAuthSSEMiddl is configured (production path), a request with no token
// at all returns 401 — not 200 with stream headers.
func TestRouter_SSE_ClerkAuthSSEMiddl_Returns401_NoToken(t *testing.T) {
	r := BuildRouter(minimalConfig(), depsWithClerkSSE(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/events ClerkAuthSSEMiddl no token: want 401, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct == "text/event-stream" {
		t.Error("headers must NOT include text/event-stream when auth fails before stream opens")
	}
}

// TestRouter_SSE_ClerkAuthSSEMiddl_Returns401_EmptyQueryToken verifies that
// ?token= with an empty value returns 401 and does not open an SSE stream.
// This is the case Frank's frontend fix addresses: getToken() returns null,
// EventSource URL is opened as /api/v1/events?token= (or no ?token at all).
func TestRouter_SSE_ClerkAuthSSEMiddl_Returns401_EmptyQueryToken(t *testing.T) {
	r := BuildRouter(minimalConfig(), depsWithClerkSSE(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?token=", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/events?token= empty: want 401, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct == "text/event-stream" {
		t.Error("headers must NOT include text/event-stream when auth fails before stream opens")
	}
}

// TestRouter_SSE_ClerkAuthSSEMiddl_Returns401_InvalidQueryToken verifies that
// ?token=<garbage> (e.g. the literal string "null" from a JavaScript null
// coerce, or a structurally invalid value) returns 401.
func TestRouter_SSE_ClerkAuthSSEMiddl_Returns401_InvalidQueryToken(t *testing.T) {
	r := BuildRouter(minimalConfig(), depsWithClerkSSE(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?token=null", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/events?token=null: want 401, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
}

// TestRouter_SSE_ClerkAuthSSEMiddl_ValidQueryToken_PassesAuth verifies that a
// valid Clerk JWT passed via ?token= is accepted by ClerkAuthSSEMiddl and the
// request reaches the SSE handler (200 + text/event-stream headers).
//
// Uses httptest.NewServer + a real HTTP client so the streaming SSE handler
// does not block httptest.NewRecorder.
func TestRouter_SSE_ClerkAuthSSEMiddl_ValidQueryToken_PassesAuth(t *testing.T) {
	jwt := setupClerkBackend(t)

	r := BuildRouter(minimalConfig(), depsWithClerkSSE(t))
	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		ts.URL+"/api/v1/events?token="+jwt, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	// No Authorization header — only ?token= to simulate EventSource.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/events?token=<jwt>: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("valid ?token= JWT: ClerkAuthSSEMiddl rejected the token — it should have passed through")
	}
	// Deferred cancel() terminates the SSE connection; ts.Close() then completes cleanly.
}

// ──────────────────────────────────────────────────────────────────────────────
// E2EUnguardedSSE — pipeline E2E bypass
// ──────────────────────────────────────────────────────────────────────────────

// TestRouter_SSE_E2EUnguardedSSE_AllowsUnauthenticated verifies that when
// E2EUnguardedSSE=true, GET /api/v1/events is reachable without any auth token.
// The sentinel middleware injects user ID=1 into context so the SSE broker
// does not return 401. The context is cancelled immediately so the SSE handler
// exits without blocking.
func TestRouter_SSE_E2EUnguardedSSE_AllowsUnauthenticated(t *testing.T) {
	deps := depsNoAuth(t)
	deps.E2EUnguardedSSE = true
	r := BuildRouter(minimalConfig(), deps)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so SSE handler exits after setup

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusServiceUnavailable {
		t.Fatalf("GET /api/v1/events E2EUnguardedSSE=true: got 503 (auth blocking); want SSE handler response")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// /api/v1/daemons (ADR-031 §3 + §4) — list + soft-revoke endpoints
// ──────────────────────────────────────────────────────────────────────────────

// stubDaemonsRepo satisfies both the list and revoke repository contracts
// used by DaemonsListHandler and DaemonsRevokeHandler.
type stubDaemonsRepo struct{}

func (s *stubDaemonsRepo) ListByAccountID(_ context.Context, _ string) ([]repository.DaemonAPIKey, error) {
	return []repository.DaemonAPIKey{}, nil
}

func (s *stubDaemonsRepo) RevokeByAccountIDAndDeviceID(_ context.Context, _, _ string) (bool, error) {
	return false, nil
}

func TestRouter_DaemonsList_Returns401_WithoutToken(t *testing.T) {
	deps := depsWithClerk(t)
	deps.DaemonsListHandler = handlers.NewDaemonsListHandler(&stubDaemonsRepo{})

	r := BuildRouter(minimalConfig(), deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemons", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/daemons no token: want 401, got %d", rr.Code)
	}
}

func TestRouter_DaemonsList_ValidJWT_Returns200(t *testing.T) {
	jwt := setupClerkBackend(t)

	deps := depsWithClerk(t)
	deps.DaemonsListHandler = handlers.NewDaemonsListHandler(&stubDaemonsRepo{})

	r := BuildRouter(minimalConfig(), deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/daemons", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/daemons valid JWT: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRouter_DaemonsRevoke_Returns401_WithoutToken(t *testing.T) {
	deps := depsWithClerk(t)
	deps.DaemonsRevokeHandler = handlers.NewDaemonsRevokeHandler(&stubDaemonsRepo{})

	r := BuildRouter(minimalConfig(), deps)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/daemons/11111111-1111-1111-1111-111111111111", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("DELETE /api/v1/daemons/{device_id} no token: want 401, got %d", rr.Code)
	}
}

func TestRouter_DaemonsRevoke_ValidJWT_Returns404_NonExistent(t *testing.T) {
	jwt := setupClerkBackend(t)

	deps := depsWithClerk(t)
	deps.DaemonsRevokeHandler = handlers.NewDaemonsRevokeHandler(&stubDaemonsRepo{})

	r := BuildRouter(minimalConfig(), deps)

	// Repo stub returns (false, nil) → handler returns 404 (cross-tenant /
	// not-existent / already-revoked all collapse here).
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/daemons/11111111-1111-1111-1111-111111111111", nil)
	req.Header.Set("Authorization", "Bearer "+jwt)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("DELETE /api/v1/daemons/{device_id} valid JWT non-existent: want 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRouter_DaemonsRevoke_RouteAbsent_WhenHandlerNil(t *testing.T) {
	deps := depsWithClerk(t)
	// DaemonsRevokeHandler intentionally left nil.

	r := BuildRouter(minimalConfig(), deps)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/daemons/11111111-1111-1111-1111-111111111111", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound && rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE /api/v1/daemons with nil handler: want 404/405, got %d", rr.Code)
	}
}

// ─── Admin fleet-health route tests (#2559) ───────────────────────────────────

// stubFleetRepo satisfies the fleetHealthSnapshotter interface (unexported —
// accessed via handlers.NewAdminFleetHealthHandler which accepts the interface).
type stubFleetRepo struct{}

func (s *stubFleetRepo) FleetHealthSnapshot(_ context.Context) (repository.FleetHealthSnapshot, error) {
	return repository.FleetHealthSnapshot{
		TotalPaired:  5,
		ActiveLast5m: 1,
		ActiveLast1h: 3,
		Revoked:      2,
	}, nil
}

func TestRouter_AdminFleetHealth_MissingToken_Returns401(t *testing.T) {
	deps := depsWithClerk(t)
	deps.AdminFleetHealthHandler = handlers.NewAdminFleetHealthHandler(&stubFleetRepo{})
	deps.AdminTokenMiddl = bffmiddleware.AdminTokenAuth("router-test-admin-token")

	r := BuildRouter(minimalConfig(), deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/daemons/fleet-health", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/admin/daemons/fleet-health no token: want 401, got %d", rr.Code)
	}
}

func TestRouter_AdminFleetHealth_WrongToken_Returns401(t *testing.T) {
	deps := depsWithClerk(t)
	deps.AdminFleetHealthHandler = handlers.NewAdminFleetHealthHandler(&stubFleetRepo{})
	deps.AdminTokenMiddl = bffmiddleware.AdminTokenAuth("correct-admin-token")

	r := BuildRouter(minimalConfig(), deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/daemons/fleet-health", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/admin/daemons/fleet-health wrong token: want 401, got %d", rr.Code)
	}
}

func TestRouter_AdminFleetHealth_CorrectToken_Returns200(t *testing.T) {
	const adminToken = "correct-admin-token-for-router-test"

	deps := depsWithClerk(t)
	deps.AdminFleetHealthHandler = handlers.NewAdminFleetHealthHandler(&stubFleetRepo{})
	deps.AdminTokenMiddl = bffmiddleware.AdminTokenAuth(adminToken)

	r := BuildRouter(minimalConfig(), deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/daemons/fleet-health", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/admin/daemons/fleet-health correct token: want 200, got %d: %s",
			rr.Code, rr.Body.String())
	}
}

func TestRouter_AdminFleetHealth_RouteAbsent_WhenHandlerNil(t *testing.T) {
	deps := depsWithClerk(t)
	// AdminFleetHealthHandler intentionally left nil — route must not be mounted.

	r := BuildRouter(minimalConfig(), deps)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/daemons/fleet-health", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound && rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/v1/admin/daemons/fleet-health nil handler: want 404/405, got %d", rr.Code)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/v1/waitlist — public endpoint smoke tests (ticket #121)
// ──────────────────────────────────────────────────────────────────────────────

// stubWaitlistRouterRepo satisfies the waitlistRepo interface used by WaitlistHandler.
// InsertIfNew always returns a new row (created=true, position=1) so the handler
// returns 200 OK with {"position":1}.
type stubWaitlistRouterRepo struct{}

func (s *stubWaitlistRouterRepo) InsertIfNew(_ context.Context, _ string, _, _, _ *string, _ *string) (string, int64, bool, error) {
	return "uuid-router-test", 1, true, nil
}

func (s *stubWaitlistRouterRepo) UpdateMailchimpStatus(_ context.Context, _, _ string) error {
	return nil
}

// TestRouter_Waitlist_IsPublic verifies POST /api/v1/waitlist is reachable
// without any authentication token and routes to WaitlistHandler.Join.
func TestRouter_Waitlist_IsPublic(t *testing.T) {
	deps := depsNoAuth(t)
	deps.WaitlistHandler = handlers.NewWaitlistHandler(&stubWaitlistRouterRepo{}, nil)

	r := BuildRouter(minimalConfig(), deps)

	body := bytes.NewBufferString(`{"email":"smoke@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	// 200 OK — handler was reached and InsertIfNew returned created=true, position=1.
	if rr.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/waitlist: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestRouter_Waitlist_RouteAbsent_WhenHandlerNil verifies that when no
// WaitlistHandler is configured, chi returns 404 — no panic.
func TestRouter_Waitlist_RouteAbsent_WhenHandlerNil(t *testing.T) {
	deps := depsNoAuth(t)
	// WaitlistHandler intentionally left nil.

	r := BuildRouter(minimalConfig(), deps)

	body := bytes.NewBufferString(`{"email":"smoke@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound && rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/v1/waitlist nil handler: want 404/405, got %d", rr.Code)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// POST /api/v1/daemon/register — daemon PKCE registration route
//
// These tests guard against the 2026-05-30 prod incident where
// CLERK_FRONTEND_API was not provisioned to the BFF env file, causing
// ClerkOAuthMiddl to be nil and the route to be silently dropped from the
// router.  The daemon (cloud_api_url=https://api.vaultmtg.app/api/v1)
// constructs the URL as bffBaseURL+"/daemon/register" which resolves to
// /api/v1/daemon/register — the correct path — but received 404 because the
// route was not mounted.
// ──────────────────────────────────────────────────────────────────────────────

// stubDaemonRegisterRepo satisfies daemonAPIKeyUpsertRepo for routing tests.
// It is never actually called — the middleware rejects all requests before the
// handler runs.
type stubDaemonRegisterRepo struct{}

func (s *stubDaemonRegisterRepo) UpsertKey(_ context.Context, _, _, _, _, _, _ string) (*repository.DaemonAPIKey, bool, error) {
	return nil, false, nil
}

func (s *stubDaemonRegisterRepo) GetByAccountAndDevice(_ context.Context, _, _ string) (*repository.DaemonAPIKey, error) {
	return nil, repository.ErrDaemonAPIKeyNotFound
}

// noopOAuthMiddl is a stand-in ClerkOAuthMiddl for routing tests.  It always
// returns 401 so we can verify the route IS mounted without a real Clerk backend.
func noopOAuthMiddl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	})
}

// TestRouter_DaemonRegister_Returns401_WhenOAuthMiddlPresent verifies that
// POST /api/v1/daemon/register is mounted and protected by ClerkOAuthMiddl.
// With no token the middleware returns 401 — confirming the route exists and
// auth is enforced.  A 404 here would indicate the route was not mounted
// (the 2026-05-30 prod incident symptom).
func TestRouter_DaemonRegister_Returns401_WhenOAuthMiddlPresent(t *testing.T) {
	deps := depsNoAuth(t)
	deps.ClerkOAuthMiddl = noopOAuthMiddl
	deps.DaemonRegisterHandler = handlers.NewDaemonRegisterHandler(&stubDaemonRegisterRepo{}, nil)

	r := BuildRouter(minimalConfig(), deps)

	body := bytes.NewBufferString(`{"device_id":"","platform":"darwin","daemon_ver":"0.3.4"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/daemon/register", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatal("POST /api/v1/daemon/register: got 404 — route is not mounted (CLERK_FRONTEND_API misconfiguration)")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("POST /api/v1/daemon/register with no token: want 401, got %d", rr.Code)
	}
}

// TestRouter_DaemonRegister_Returns503_WhenOAuthMiddlNil verifies the
// fail-closed behaviour introduced to fix the 2026-05-30 prod incident.
// When CLERK_FRONTEND_API is not configured (ClerkOAuthMiddl==nil), the route
// must still be mounted and return 503 — not silently 404.
func TestRouter_DaemonRegister_Returns503_WhenOAuthMiddlNil(t *testing.T) {
	deps := depsNoAuth(t)
	// ClerkOAuthMiddl intentionally nil — simulates missing CLERK_FRONTEND_API.
	deps.DaemonRegisterHandler = handlers.NewDaemonRegisterHandler(&stubDaemonRegisterRepo{}, nil)

	r := BuildRouter(minimalConfig(), deps)

	body := bytes.NewBufferString(`{"device_id":"","platform":"darwin","daemon_ver":"0.3.4"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/daemon/register", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code == http.StatusNotFound {
		t.Fatal("POST /api/v1/daemon/register with nil ClerkOAuthMiddl: got 404 — route must be mounted fail-closed as 503")
	}
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /api/v1/daemon/register with nil ClerkOAuthMiddl: want 503, got %d", rr.Code)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// CORS credentials — Access-Control-Allow-Credentials (#777)
//
// The browser EventSource API connects with withCredentials:true, which
// requires the server to respond with:
//   - Access-Control-Allow-Credentials: true
//   - Access-Control-Allow-Origin: <specific-origin>  (never "*")
//
// These tests guard the fix for the third sequential SSE bug: ACAO was present
// but ACAC was absent because cors.Options{AllowCredentials: true} was missing.
// ──────────────────────────────────────────────────────────────────────────────

// corsConfig returns a Config with a specific allowed origin suitable for
// credentialed CORS tests.  The wildcard from minimalConfig() cannot be used
// because go-chi/cors will not emit ACAO:"*" when AllowCredentials is true.
func corsConfig() *config.Config {
	return &config.Config{
		Env:                                 "development",
		AllowedOrigins:                      []string{"https://stg-app.vaultmtg.app"},
		DraftRatingsStalenessThresholdHours: 48,
		DaemonLatestVersion:                 "0.1.0",
	}
}

// TestRouter_CORS_SSE_IncludesAllowCredentials verifies that a cross-origin GET
// to /api/v1/events with a valid allowed Origin receives
// Access-Control-Allow-Credentials:true on the 401 response (the token is
// absent so auth fails, but CORS headers must be present before the auth check
// rejects the request — the CORS middleware runs first in the chain).
func TestRouter_CORS_SSE_IncludesAllowCredentials(t *testing.T) {
	r := BuildRouter(corsConfig(), depsWithClerk(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Origin", "https://stg-app.vaultmtg.app")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	// Auth fails (no token), so we expect 401 — but CORS must have run first.
	if got := rr.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials: want \"true\", got %q (was ACAC missing from cors.Options?)", got)
	}

	acao := rr.Header().Get("Access-Control-Allow-Origin")
	if acao != "https://stg-app.vaultmtg.app" {
		t.Errorf("Access-Control-Allow-Origin: want %q, got %q (must be specific origin, not wildcard, when credentials allowed)",
			"https://stg-app.vaultmtg.app", acao)
	}
}

// TestRouter_CORS_SSE_RejectsWildcardOriginWithCredentials verifies that an
// origin NOT in the allowed list does not receive ACAO or ACAC headers — the
// browser must be blocked from connecting with credentials to an unrecognised
// origin.
func TestRouter_CORS_SSE_RejectsWildcardOriginWithCredentials(t *testing.T) {
	r := BuildRouter(corsConfig(), depsWithClerk(t))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	// The disallowed origin must not receive an ACAO header.
	if acao := rr.Header().Get("Access-Control-Allow-Origin"); acao != "" {
		t.Errorf("Access-Control-Allow-Origin: want empty for disallowed origin, got %q", acao)
	}
}

// TestRouter_CORS_SSE_ValidJWT_StreamResponseHasCredentialHeaders verifies that
// when a credentialed EventSource connects with a valid JWT and a recognised
// Origin, the actual streaming 200 response carries ACAO and ACAC headers.
//
// Uses httptest.NewServer + a real HTTP client (same pattern as the existing
// TestRouter_SSE_ValidJWT_WithResolver_ReachesHandler) so the SSE handler does
// not block on httptest.NewRecorder.
func TestRouter_CORS_SSE_ValidJWT_StreamResponseHasCredentialHeaders(t *testing.T) {
	jwt := setupClerkBackend(t)

	deps := depsWithClerkSSE(t)
	r := BuildRouter(corsConfig(), deps)

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		ts.URL+"/api/v1/events?token="+jwt, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Origin", "https://stg-app.vaultmtg.app")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/events: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("valid ?token= JWT with CORS origin: unexpected 401")
	}

	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("streaming 200: Access-Control-Allow-Credentials: want \"true\", got %q", got)
	}

	acao := resp.Header.Get("Access-Control-Allow-Origin")
	if acao != "https://stg-app.vaultmtg.app" {
		t.Errorf("streaming 200: Access-Control-Allow-Origin: want %q, got %q", "https://stg-app.vaultmtg.app", acao)
	}
	// Deferred cancel() terminates the SSE connection; ts.Close() then completes cleanly.
}
