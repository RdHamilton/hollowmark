package daemon

// Tests for #144: auth_status field in heartbeatPayload.
//
// These tests verify that the heartbeat event dispatched to the BFF carries
// auth_status matching the output of computeAuthStatus for each of the four
// daemon-side states. The BFF maps unknown states and absence-of-field to
// "unknown"; the daemon never emits "unknown".
//
// Test pattern: spin up a httptest.Server capturing ingest calls, run the
// daemon Run loop with a very short heartbeatInterval, then decode the first
// daemon.heartbeat event and assert auth_status.  This exercises the same
// goroutine path as production — the heartbeat ticker builds hbPayload and
// calls dispatch.BuildEvent + SendOrBuffer.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/contract"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureFirstHeartbeatAuthStatus spins up an httptest.Server that captures
// the first daemon.heartbeat event's auth_status field and sends it on ch.
// The caller is responsible for closing the server.
func captureFirstHeartbeatAuthStatus(t *testing.T) (srv *httptest.Server, ch <-chan string) {
	t.Helper()
	out := make(chan string, 1)
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ingest/events" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// Events may arrive as a JSON batch or as a single event.
		var batch []contract.DaemonEvent
		var single contract.DaemonEvent
		var evt contract.DaemonEvent
		if json.Unmarshal(body, &batch) == nil && len(batch) > 0 {
			evt = batch[0]
		} else if json.Unmarshal(body, &single) == nil {
			evt = single
		}
		if evt.Type == "daemon.heartbeat" {
			var p struct {
				AuthStatus string `json:"auth_status"`
			}
			if json.Unmarshal(evt.Payload, &p) == nil {
				select {
				case out <- p.AuthStatus:
				default:
				}
			}
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	return srv, out
}

// runUntilHeartbeat runs the daemon with the given config until the first
// daemon.heartbeat event is captured, then cancels. Returns the auth_status
// from the first heartbeat payload, or t.Fatal if none arrives in 500ms.
func runUntilHeartbeat(t *testing.T, cfg *config.Config, mutate func(svc *Service)) string {
	t.Helper()

	srv, ch := captureFirstHeartbeatAuthStatus(t)
	defer srv.Close()

	cfg.CloudAPIURL = srv.URL
	cfg.IngestPath = "/v1/ingest/events"
	if cfg.LogPath == "" {
		cfg.LogPath = "/dev/null"
	}

	svc := New(cfg)

	// Stub the credential store so Keychain=true tests pass on keychain-less CI
	// (Linux, headless). New() calls the real credstore, which fails when
	// org.freedesktop.secrets / the macOS Keychain is absent, setting keychainErr
	// and causing Run() to block in retryKeychain's 2s/4s/8s backoff — the
	// daemon never reaches the heartbeat ticker. Override after New() so the
	// getter returns a valid key and any startup keychainErr is cleared before
	// Run() begins. The mutate callback runs after this stub so individual tests
	// can further adjust svc fields (e.g. authPaused) without fighting the stub.
	svc.keychainGet = func() (string, error) { return "test-api-key", nil }
	svc.keychainErr = nil

	if mutate != nil {
		mutate(svc)
	}

	old := heartbeatInterval
	heartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { heartbeatInterval = old })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = svc.Run(ctx)
	}()

	select {
	case got := <-ch:
		cancel()
		wg.Wait()
		return got
	case <-ctx.Done():
		wg.Wait()
		t.Fatal("no daemon.heartbeat event received within 500ms")
		return ""
	}
}

// TestHeartbeatPayload_AuthStatus_Authenticated verifies that when Keychain=true
// and AccountID is set and no keychain error exists, the heartbeat carries
// auth_status="authenticated".
func TestHeartbeatPayload_AuthStatus_Authenticated(t *testing.T) {
	cfg := &config.Config{
		APIKey:    "test-key",
		AccountID: "acc-authenticated",
		Keychain:  true,
	}
	// No mutation needed — no keychain error, no auth pause.
	got := runUntilHeartbeat(t, cfg, nil)
	assert.Equal(t, "authenticated", got,
		"heartbeat auth_status must be 'authenticated' when keychain=true, accountID set, no errors")
}

// TestHeartbeatPayload_AuthStatus_SetupRequired verifies that when Keychain is
// false (keychain mode not enabled), the heartbeat carries
// auth_status="setup_required".
//
// AccountID must be non-empty for the heartbeat to fire (the ticker skips
// when AccountID == ""); Keychain=false with a non-empty AccountID represents
// the transitional state where the account is linked but keychain mode has not
// yet been enabled by the user.
func TestHeartbeatPayload_AuthStatus_SetupRequired(t *testing.T) {
	cfg := &config.Config{
		APIKey:    "test-key",
		AccountID: "acc-setup-required",
		Keychain:  false, // keychain mode not enabled → setup_required
	}
	got := runUntilHeartbeat(t, cfg, nil)
	assert.Equal(t, "setup_required", got,
		"heartbeat auth_status must be 'setup_required' when Keychain=false")
}

// TestHeartbeatPayload_AuthStatus_KeychainError verifies that when the daemon
// has a keychain error sentinel set mid-run, the next heartbeat carries
// auth_status="keychain_error".
//
// The daemon starts in a healthy state (plaintext APIKey, AccountID set so
// the heartbeat loop runs). After the first heartbeat fires, we inject a
// keychain error via setKeychainErr and wait for the second heartbeat —
// which must reflect keychain_error. This mirrors the production case where
// KeychainReauthRequired sets keychainErr during the event loop.
func TestHeartbeatPayload_AuthStatus_KeychainError(t *testing.T) {
	// Collect ALL heartbeat auth_status values, not just the first.
	var mu sync.Mutex
	var statuses []string
	injectErr := make(chan struct{})
	var injectOnce sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ingest/events" {
			body, _ := io.ReadAll(r.Body)
			var batch []contract.DaemonEvent
			var single contract.DaemonEvent
			var evt contract.DaemonEvent
			if json.Unmarshal(body, &batch) == nil && len(batch) > 0 {
				evt = batch[0]
			} else if json.Unmarshal(body, &single) == nil {
				evt = single
			}
			if evt.Type == "daemon.heartbeat" {
				var p struct {
					AuthStatus string `json:"auth_status"`
				}
				if json.Unmarshal(evt.Payload, &p) == nil {
					mu.Lock()
					statuses = append(statuses, p.AuthStatus)
					mu.Unlock()
					// Signal to inject the error after the first heartbeat.
					injectOnce.Do(func() { close(injectErr) })
				}
			}
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	cfg := &config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-keychain-error",
		LogPath:     "/dev/null",
	}

	old := heartbeatInterval
	heartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { heartbeatInterval = old })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	svc := New(cfg)

	go func() {
		// Wait for first heartbeat, then inject the keychain error so the
		// second heartbeat reflects keychain_error.
		select {
		case <-injectErr:
			svc.setKeychainErr(errors.New("keychain: access denied"))
		case <-ctx.Done():
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = svc.Run(ctx)
	}()

	// Wait until we have at least 2 heartbeats (first healthy, second with error).
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(statuses) >= 2
	}, 2*time.Second, 20*time.Millisecond, "expected at least 2 heartbeats")

	cancel()
	wg.Wait()

	mu.Lock()
	last := statuses[len(statuses)-1]
	mu.Unlock()

	assert.Equal(t, "keychain_error", last,
		"heartbeat auth_status must be 'keychain_error' after setKeychainErr injection")
}

// TestHeartbeatPayload_AuthStatus_AuthPaused verifies that when authPaused is
// true (PKCE attempt cap reached), the heartbeat carries auth_status="auth_paused".
// auth_paused OUTRANKS keychain_error in the computeAuthStatus precedence chain.
func TestHeartbeatPayload_AuthStatus_AuthPaused(t *testing.T) {
	cfg := &config.Config{
		APIKey:    "test-key",
		AccountID: "acc-auth-paused",
		Keychain:  true,
	}
	got := runUntilHeartbeat(t, cfg, func(svc *Service) {
		svc.authPaused.Store(true)
	})
	assert.Equal(t, "auth_paused", got,
		"heartbeat auth_status must be 'auth_paused' when authPaused=true")
}

// TestHeartbeatPayload_AuthStatus_AlwaysPresent verifies that the auth_status
// key is always present in the JSON payload, even when empty — the BFF uses
// absence-vs-empty to distinguish new-daemon "" from old-daemon (no field).
// An authenticated daemon should emit a non-empty value in all cases.
func TestHeartbeatPayload_AuthStatus_AlwaysPresent(t *testing.T) {
	// Capture the raw payload to inspect JSON key presence (not just decoded value).
	var rawPayload []byte
	var rawMu sync.Mutex
	captured := make(chan struct{}, 1)

	capSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/ingest/events" {
			body, _ := io.ReadAll(r.Body)
			var batch []contract.DaemonEvent
			var single contract.DaemonEvent
			var evt contract.DaemonEvent
			if json.Unmarshal(body, &batch) == nil && len(batch) > 0 {
				evt = batch[0]
			} else if json.Unmarshal(body, &single) == nil {
				evt = single
			}
			if evt.Type == "daemon.heartbeat" {
				rawMu.Lock()
				rawPayload = evt.Payload
				rawMu.Unlock()
				select {
				case captured <- struct{}{}:
				default:
				}
			}
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer capSrv.Close()

	cfg := &config.Config{
		CloudAPIURL: capSrv.URL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "acc-presence",
		Keychain:    true,
		LogPath:     "/dev/null",
	}

	old := heartbeatInterval
	heartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { heartbeatInterval = old })

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	svc := New(cfg)

	// Same credstore stub as runUntilHeartbeat: clear any startup keychainErr so
	// Run() does not block in retryKeychain backoff on keychain-less CI.
	svc.keychainGet = func() (string, error) { return "test-api-key", nil }
	svc.keychainErr = nil

	// Use a WaitGroup so we can join Run() before the t.Cleanup fires. Without
	// this, the cleanup writes heartbeatInterval = old while the Run goroutine is
	// still reading heartbeatInterval at service.go startup — data race detected
	// by the race detector (#3309 Defect 2).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = svc.Run(ctx)
	}()

	select {
	case <-captured:
	case <-ctx.Done():
		t.Fatal("no heartbeat captured")
	}
	// Cancel and join before reading rawPayload and before t.Cleanup runs.
	// This ensures the Run goroutine has exited before heartbeatInterval is
	// restored, eliminating the race.
	cancel()
	wg.Wait()

	rawMu.Lock()
	p := rawPayload
	rawMu.Unlock()

	// Unmarshal into a map to check for key presence (not just zero value).
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(p, &m), "payload must be valid JSON")
	_, ok := m["auth_status"]
	assert.True(t, ok, "auth_status key must be present in heartbeat payload JSON (no omitempty)")
}
