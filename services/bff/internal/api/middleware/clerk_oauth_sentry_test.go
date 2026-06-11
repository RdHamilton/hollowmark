package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/observability"
	"github.com/getsentry/sentry-go"
)

// TestFetchUserInfo_Non2xx_ReportsSentryEvent is an integration test for the
// non-2xx error path in fetchUserInfo.  It stubs the Clerk /oauth/userinfo
// endpoint to return HTTP 500, initialises Sentry with an in-process
// MockTransport (no real network calls), drives the path via
// RequireClerkOAuthToken, and asserts that exactly one Sentry event is
// captured with the expected outbound tags.
//
// AC1: httptest.NewServer returns HTTP 500.
// AC2: sentry.MockTransport captures the event.
// AC3: event tags component=outbound and target=clerk-oauth.
// AC4: no real Clerk network calls — fully hermetic.
func TestFetchUserInfo_Non2xx_ReportsSentryEvent(t *testing.T) {
	// AC1: stub Clerk userinfo endpoint returning 500.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer stub.Close()

	// AC2: initialise Sentry with an in-process MockTransport.
	transport := &sentry.MockTransport{}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://key@o0.ingest.sentry.io/0",
		Transport: transport,
	}); err != nil {
		t.Fatalf("sentry.Init: %v", err)
	}
	observability.ResetRateLimiter()
	t.Cleanup(func() {
		_ = sentry.Init(sentry.ClientOptions{})
		observability.ResetRateLimiter()
	})

	// Wire RequireClerkOAuthToken to point at our stub server.
	// RequireClerkOAuthToken appends "/oauth/userinfo" to the base URL.
	handler := bffmiddleware.RequireClerkOAuthToken(stub.URL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	// Fire a request with a Bearer token — the stub returns 500 for any path.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ingest", nil)
	req.Header.Set("Authorization", "Bearer test-daemon-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// The middleware must propagate the 500 as a 401 to the caller.
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on non-2xx userinfo, got %d", rr.Code)
	}

	// Flush buffered SDK events to the transport before inspecting.
	sentry.Flush(200 * time.Millisecond)

	// AC3: exactly one event with the outbound tags.
	events := transport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 Sentry event, got %d", len(events))
	}
	ev := events[0]
	if ev.Tags["component"] != "outbound" {
		t.Errorf("tag component: want %q, got %q", "outbound", ev.Tags["component"])
	}
	if ev.Tags["target"] != "clerk-oauth" {
		t.Errorf("tag target: want %q, got %q", "clerk-oauth", ev.Tags["target"])
	}
}

// TestFetchUserInfo_Non2xx_401_ReportsSentryEvent verifies the same Sentry
// reporting path for HTTP 401 (token rejected by Clerk) — a separate status
// code that also exercises the non-2xx branch.
func TestFetchUserInfo_Non2xx_401_ReportsSentryEvent(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer stub.Close()

	transport := &sentry.MockTransport{}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://key@o0.ingest.sentry.io/0",
		Transport: transport,
	}); err != nil {
		t.Fatalf("sentry.Init: %v", err)
	}
	observability.ResetRateLimiter()
	t.Cleanup(func() {
		_ = sentry.Init(sentry.ClientOptions{})
		observability.ResetRateLimiter()
	})

	handler := bffmiddleware.RequireClerkOAuthToken(stub.URL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ingest", nil)
	req.Header.Set("Authorization", "Bearer expired-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}

	sentry.Flush(200 * time.Millisecond)

	events := transport.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 Sentry event, got %d", len(events))
	}
	ev := events[0]
	if ev.Tags["component"] != "outbound" {
		t.Errorf("tag component: want %q, got %q", "outbound", ev.Tags["component"])
	}
	if ev.Tags["target"] != "clerk-oauth" {
		t.Errorf("tag target: want %q, got %q", "clerk-oauth", ev.Tags["target"])
	}
}

// TestFetchUserInfo_2xx_NoSentryEvent verifies that a successful 200 response
// from the Clerk userinfo endpoint does NOT produce a Sentry event.  This is
// the happy path; no error means no capture.
func TestFetchUserInfo_2xx_NoSentryEvent(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"sub":"user_daemon_abc","email":"test@example.com"}`))
	}))
	defer stub.Close()

	transport := &sentry.MockTransport{}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://key@o0.ingest.sentry.io/0",
		Transport: transport,
	}); err != nil {
		t.Fatalf("sentry.Init: %v", err)
	}
	observability.ResetRateLimiter()
	t.Cleanup(func() {
		_ = sentry.Init(sentry.ClientOptions{})
		observability.ResetRateLimiter()
	})

	handler := bffmiddleware.RequireClerkOAuthToken(stub.URL)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ingest", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 on success, got %d", rr.Code)
	}

	sentry.Flush(200 * time.Millisecond)

	events := transport.Events()
	if len(events) != 0 {
		t.Errorf("expected 0 Sentry events on 2xx, got %d", len(events))
	}
}
