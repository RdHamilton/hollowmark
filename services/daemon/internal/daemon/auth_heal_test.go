package daemon

// auth_heal_test.go — TDD tests for the daemon self-heal auth cohort.
//
// Covers:
//   Fix A (#1326): startup token-liveness probe — stale/revoked key → PKCE re-auth
//   Fix B (#1327): computeAuthStatus correct in keychain mode (no spurious warning path)
//   Fix C (#1328): reactive 401 at ingest → SetupRequired + authPaused (tray always reachable)
//   Fix D (#1329): identity change on re-auth → events use new account_id
//
// All tests were written (and verified to FAIL) before implementation — per TDD protocol.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/config"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/localapi"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/logreader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ────────────────────────────────────────────────────────────────────────────
// Fix A (#1326) — startup token-liveness probe
// ────────────────────────────────────────────────────────────────────────────

// TestProbeTokenLiveness_401ReturnsFalse verifies that ProbeTokenLiveness
// returns (false, nil) when the BFF responds with 401, signalling that the
// stored token is stale and PKCE re-auth is required.
func TestProbeTokenLiveness_401ReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/health/daemon", r.URL.Path)
		assert.Equal(t, "Bearer stale-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	live, err := ProbeTokenLiveness(context.Background(), srv.URL, "stale-key")
	require.NoError(t, err)
	assert.False(t, live, "401 response must return live=false")
}

// TestProbeTokenLiveness_200ReturnsTrue verifies that ProbeTokenLiveness
// returns (true, nil) when the BFF responds with 200, signalling that the
// stored token is valid and normal startup can proceed.
func TestProbeTokenLiveness_200ReturnsTrue(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/health/daemon", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"connected":true}`))
	}))
	defer srv.Close()

	live, err := ProbeTokenLiveness(context.Background(), srv.URL, "valid-key")
	require.NoError(t, err)
	assert.True(t, live, "200 response must return live=true")
}

// TestProbeTokenLiveness_503ReturnsTrueTransient verifies that ProbeTokenLiveness
// returns (true, nil) for 5xx responses — a server-side error does not mean the
// token is invalid; we assume the token is valid to avoid false-positive PKCE
// flows during BFF downtime.
func TestProbeTokenLiveness_503ReturnsTrueTransient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	live, err := ProbeTokenLiveness(context.Background(), srv.URL, "some-key")
	require.NoError(t, err)
	assert.True(t, live, "5xx (transient BFF error) must return live=true to avoid spurious PKCE")
}

// TestProbeTokenLiveness_403ReturnsFalse verifies that ProbeTokenLiveness
// returns (false, nil) for a 403 Forbidden, treating it the same as a revoked
// token — re-auth is required.
func TestProbeTokenLiveness_403ReturnsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	live, err := ProbeTokenLiveness(context.Background(), srv.URL, "bad-key")
	require.NoError(t, err)
	assert.False(t, live, "403 response must return live=false")
}

// ────────────────────────────────────────────────────────────────────────────
// Fix B (#1327) — computeAuthStatus correct for keychain mode (AC5)
// ────────────────────────────────────────────────────────────────────────────

// TestComputeAuthStatus_KeychainWithNoAPIKeyIsAuthenticated verifies that
// computeAuthStatus returns AuthStatusAuthenticated when keychain=true and
// account_id is set — even though api_key and daemon_jwt are both empty.
// This is the AC5 case: empty api_key/daemon_jwt is EXPECTED in keychain mode.
func TestComputeAuthStatus_KeychainWithNoAPIKeyIsAuthenticated(t *testing.T) {
	cfg := &config.Config{
		CloudAPIURL: "http://bff",
		Keychain:    true,
		AccountID:   "acc-kc-auth",
		// APIKey and DaemonJWT are both empty — expected in keychain mode.
	}
	status := computeAuthStatus(cfg, nil /* keychainErr */, false /* authPaused */)
	assert.Equal(t, localapi.AuthStatusAuthenticated, status,
		"keychain mode with non-empty AccountID must be AuthStatusAuthenticated even without api_key/daemon_jwt")
}

// TestComputeAuthStatus_KeychainWithNoAccountIDIsSetupRequired verifies that
// computeAuthStatus returns AuthStatusSetupRequired when keychain=true but
// account_id is empty (daemon not yet registered after fresh install).
func TestComputeAuthStatus_KeychainWithNoAccountIDIsSetupRequired(t *testing.T) {
	cfg := &config.Config{
		CloudAPIURL: "http://bff",
		Keychain:    true,
		AccountID:   "", // not yet registered
	}
	status := computeAuthStatus(cfg, nil, false)
	assert.Equal(t, localapi.AuthStatusSetupRequired, status,
		"keychain mode with empty AccountID must be AuthStatusSetupRequired")
}

// ────────────────────────────────────────────────────────────────────────────
// Fix C (#1328) — reactive 401 at ingest → SetupRequired + authPaused
// ────────────────────────────────────────────────────────────────────────────

// TestReactive401_SetsAuthPausedAndSetupRequired verifies that when a reactive
// 401 arrives during ingest (keychainRefresherAdapter fires reauthFunc which
// fails), the daemon sets authPaused=true AND fires SetSetupRequired(true) on
// the tray so the "Retry Setup" item is always reachable — never keychain_error.
//
// This is the Fix C contract: reactive 401 + failed PKCE → authPaused=true.
func TestReactive401_SetsAuthPausedAndSetupRequired(t *testing.T) {
	// BFF: ingest always returns 401 to trigger reactive reauth.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/ingest/events",
		AccountID:   "acc-reactive-401",
		Keychain:    true,
	}
	svc := New(cfg)

	// Wire tray hooks to capture SetSetupRequired calls.
	var setupRequiredCalls atomic.Int32
	svc.trayHooks = TrayHooks{
		SetReauthRequired: func(string) {},
		SetSetupRequired: func(v bool) {
			if v {
				setupRequiredCalls.Add(1)
			}
		},
		SetKeychainError: func(bool) {},
	}

	// Wire a reauthFunc that always fails — simulates PKCE timeout after reactive 401.
	svc.WithReauthFunc(func(_ context.Context) error {
		return context.DeadlineExceeded
	})

	// Dispatch an entry to trigger a 401 → reactive reauth path.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"request": `{"PickInfo":{"PackNumber":0,"PickNumber":0,"CardId":102704}}`},
	}
	require.NoError(t, svc.handleEntry(context.Background(), entry))
	flushBatch(svc)

	// Allow the async reauth goroutine to complete.
	time.Sleep(300 * time.Millisecond)

	// After failed reactive reauth, authPaused must be true so the main.go retry
	// loop can surface "Retry Setup" in the tray (Fix C contract).
	assert.True(t, svc.authPaused.Load(),
		"authPaused must be true after reactive 401 + failed PKCE so tray Retry Setup is reachable")

	// SetSetupRequired must have fired.
	assert.GreaterOrEqual(t, setupRequiredCalls.Load(), int32(1),
		"SetSetupRequired must be called after reactive reauth failure")

	// computeAuthStatus with authPaused=true must return auth_paused.
	authStatus := computeAuthStatus(cfg, nil, svc.authPaused.Load())
	assert.Equal(t, localapi.AuthStatusAuthPaused, authStatus,
		"computeAuthStatus must return auth_paused after reactive reauth failure")
}

// TestReactive401_SetupRequiredSupersedesKeychainError verifies that even
// when keychainErr is set (which would normally give keychain_error status),
// if authPaused is also true the status is auth_paused — the auth_paused
// state outranks keychainErr in the computeAuthStatus precedence chain (RC5).
func TestReactive401_SetupRequiredSupersedesKeychainError(t *testing.T) {
	cfg := &config.Config{
		CloudAPIURL: "http://bff",
		Keychain:    true,
		AccountID:   "acc-precedence",
	}
	// Both keychainErr set AND authPaused=true.
	status := computeAuthStatus(cfg, ErrReauthFailed, true /* authPaused */)
	assert.Equal(t, localapi.AuthStatusAuthPaused, status,
		"auth_paused must outrank keychain_error in computeAuthStatus precedence (RC5)")
}

// ────────────────────────────────────────────────────────────────────────────
// Fix D (#1329) — identity change on re-auth: events carry new account_id
// ────────────────────────────────────────────────────────────────────────────

// TestHandleEntry_UsesLiveAccountIDNotStale verifies that after the cfg.AccountID
// is updated to a new live identity (as happens post-successful PKCE), events
// dispatched carry the new account_id — not the stale cached one.
//
// Fix D contract: dispatch AccountID from the LIVE authenticated identity.
func TestHandleEntry_UsesLiveAccountIDNotStale(t *testing.T) {
	var capturedAccountID atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var batch []struct {
			AccountID string `json:"account_id"`
		}
		if err := json.Unmarshal(body, &batch); err == nil && len(batch) > 0 {
			capturedAccountID.Store(batch[0].AccountID)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	// Start with stale account_id "old-account".
	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/ingest/events",
		AccountID:   "old-account",
		Keychain:    true,
	}
	svc := New(cfg)

	// Simulate identity change: cfg.AccountID updated to new live identity.
	cfg.AccountID = "new-account"

	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"rankClass": "Gold",
			"rankTier":  float64(2),
		},
	}
	require.NoError(t, svc.handleEntry(context.Background(), entry))
	flushBatch(svc)

	stored := capturedAccountID.Load()
	require.NotNil(t, stored, "an event must have been dispatched to the BFF")
	assert.Equal(t, "new-account", stored.(string),
		"events dispatched after identity change must use the new account_id, not the stale one")
}

// TestIdentityChange_DispatcherUsesNewTokenAfterReauth verifies that after
// PropagateKeychainToken (called post-successful-PKCE), the dispatcher uses
// the fresh token stored in the keychain — not the empty/stale token.
// Covers Fix D's "no stale identity dispatch" requirement.
func TestIdentityChange_DispatcherUsesNewTokenAfterReauth(t *testing.T) {
	var receivedAuth atomic.Value

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/ingest/events",
		AccountID:   "acc-token-propagate",
		Keychain:    true,
	}
	svc := New(cfg)

	// Override keychainGet to return a fresh token simulating successful PKCE.
	const freshToken = "fresh-api-key-after-reauth"
	svc.keychainGet = func() (string, error) { return freshToken, nil }

	// PropagateKeychainToken wires the fresh token into the dispatcher.
	err := svc.PropagateKeychainToken()
	require.NoError(t, err, "PropagateKeychainToken must succeed when keychain returns a valid token")

	// Dispatch an event — must use the fresh token.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"rankClass": "Gold",
			"rankTier":  float64(2),
		},
	}
	require.NoError(t, svc.handleEntry(context.Background(), entry))
	flushBatch(svc)

	stored := receivedAuth.Load()
	require.NotNil(t, stored)
	assert.Equal(t, "Bearer "+freshToken, stored.(string),
		"dispatcher must use the fresh token after PropagateKeychainToken")
}
