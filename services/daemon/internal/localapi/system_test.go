package localapi_test

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/localapi"
)

// startTestServer spins up a Server on an ephemeral port with a baseline
// State suitable for happy-path assertions. Callers can override fields via
// the optional mutator.
func startTestServer(t *testing.T, mut func(*localapi.State)) *localapi.Server {
	t.Helper()
	started := time.Date(2026, 5, 11, 21, 0, 0, 0, time.UTC)
	state := localapi.State{
		Version:      "0.3.1-rc18",
		SessionID:    "live-test-session",
		StartedAt:    started,
		AccountID:    "user_abc",
		CloudAPIURL:  "https://staging-api.vaultmtg.app/api/v1",
		BFFReachable: true,
	}
	if mut != nil {
		mut(&state)
	}
	srv := localapi.New(0, state)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })
	return srv
}

func getJSON(t *testing.T, srv *localapi.Server, path string, out any) *http.Response {
	t.Helper()
	resp, err := http.Get("http://" + srv.Addr() + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status: got %d, want 200", path, resp.StatusCode)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp
}

func TestSystemStatusConnected(t *testing.T) {
	srv := startTestServer(t, nil)
	var body struct {
		Status    string `json:"status"`
		Connected bool   `json:"connected"`
		Mode      string `json:"mode"`
		URL       string `json:"url"`
		Port      int    `json:"port"`
	}
	getJSON(t, srv, "/api/v1/system/status", &body)
	if body.Status != "connected" || !body.Connected {
		t.Errorf("expected connected, got %+v", body)
	}
	if body.URL != "https://staging-api.vaultmtg.app/api/v1" {
		t.Errorf("url: got %q", body.URL)
	}
	// #667: the status endpoint must report the port the server actually
	// bound — not a hardcoded DefaultPort. startTestServer binds an ephemeral
	// port (New(0, ...)), so the reported port must equal the port parsed
	// from the live listener address rather than the prod default 9001.
	_, addrPort, err := net.SplitHostPort(srv.Addr())
	if err != nil {
		t.Fatalf("split listener addr %q: %v", srv.Addr(), err)
	}
	wantPort, err := strconv.Atoi(addrPort)
	if err != nil {
		t.Fatalf("parse listener port %q: %v", addrPort, err)
	}
	if body.Port != wantPort {
		t.Errorf("port: got %d, want bound port %d", body.Port, wantPort)
	}
}

// TestSystemStatusReportsChannelDerivedPort verifies that when the server is
// constructed with an explicit channel-derived port (as service.go now does
// via install.Identity(install.Channel).LocalAPIPort), the status endpoint
// reports that exact port — the cosmetic half of the #667 fix.
func TestSystemStatusReportsChannelDerivedPort(t *testing.T) {
	const stagingPort = 9011
	ln, err := net.Listen("tcp", "127.0.0.1:9011")
	if err != nil {
		t.Skipf("port %d already in use on this host; skipping", stagingPort)
	}
	_ = ln.Close()

	srv := localapi.New(stagingPort, localapi.State{
		Version:      "0.3.7-staging",
		CloudAPIURL:  "https://stg-api.vaultmtg.app/api/v1",
		BFFReachable: true,
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("Start on %d: %v", stagingPort, err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	var body struct {
		Port int `json:"port"`
	}
	getJSON(t, srv, "/api/v1/system/status", &body)
	if body.Port != stagingPort {
		t.Errorf("staging status port: got %d, want %d", body.Port, stagingPort)
	}
}

func TestSystemStatusDegradedWhenBFFUnreachable(t *testing.T) {
	srv := startTestServer(t, func(s *localapi.State) { s.BFFReachable = false })
	var body struct {
		Status    string `json:"status"`
		Connected bool   `json:"connected"`
	}
	getJSON(t, srv, "/api/v1/system/status", &body)
	if body.Status != "degraded" {
		t.Errorf("status: got %q, want degraded", body.Status)
	}
}

func TestSystemDaemonStatus(t *testing.T) {
	srv := startTestServer(t, nil)
	var body struct {
		Status    string `json:"status"`
		Connected bool   `json:"connected"`
	}
	getJSON(t, srv, "/api/v1/system/daemon/status", &body)
	if !body.Connected || body.Status != "connected" {
		t.Errorf("daemon status: %+v", body)
	}
}

func TestSystemVersion(t *testing.T) {
	srv := startTestServer(t, nil)
	var body struct {
		Version string `json:"version"`
		Service string `json:"service"`
	}
	getJSON(t, srv, "/api/v1/system/version", &body)
	if body.Version != "0.3.1-rc18" {
		t.Errorf("version: %q", body.Version)
	}
	if body.Service != "vaultmtg-daemon" {
		t.Errorf("service: %q", body.Service)
	}
}

func TestSystemHealthIncludesLastDispatch(t *testing.T) {
	dispatch := time.Date(2026, 5, 11, 21, 5, 0, 0, time.UTC)
	srv := startTestServer(t, func(s *localapi.State) { s.LastDispatchAt = &dispatch })

	var body struct {
		Status     string `json:"status"`
		Version    string `json:"version"`
		Uptime     int64  `json:"uptime"`
		LogMonitor struct {
			Status   string `json:"status"`
			LastRead string `json:"lastRead"`
		} `json:"logMonitor"`
	}
	getJSON(t, srv, "/api/v1/system/health", &body)
	if body.Status != "ok" {
		t.Errorf("status: %q", body.Status)
	}
	if body.LogMonitor.LastRead != "2026-05-11T21:05:00Z" {
		t.Errorf("lastRead: %q", body.LogMonitor.LastRead)
	}
}

func TestSystemAccountStubShape(t *testing.T) {
	srv := startTestServer(t, nil)
	var body struct {
		ID        int    `json:"ID"`
		Name      string `json:"Name"`
		IsDefault bool   `json:"IsDefault"`
	}
	getJSON(t, srv, "/api/v1/system/account", &body)
	if !body.IsDefault {
		t.Errorf("expected IsDefault=true on stub, got %+v", body)
	}
}

func TestSystemDatabasePathEmpty(t *testing.T) {
	srv := startTestServer(t, nil)
	var body struct {
		Path string `json:"path"`
	}
	getJSON(t, srv, "/api/v1/system/database/path", &body)
	if body.Path != "" {
		t.Errorf("expected empty path, got %q", body.Path)
	}
}

func TestSystemDaemonConnectAndDisconnect(t *testing.T) {
	srv := startTestServer(t, nil)
	for _, path := range []string{"/api/v1/system/daemon/connect", "/api/v1/system/daemon/disconnect"} {
		resp, err := http.Post("http://"+srv.Addr()+path, "application/json", nil)
		if err != nil {
			t.Fatalf("POST %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("POST %s status: %d", path, resp.StatusCode)
		}
		var body struct {
			Status string `json:"status"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		_ = resp.Body.Close()
		if body.Status != "ok" {
			t.Errorf("POST %s body: %+v", path, body)
		}
	}
}

// TestSystemHealth_DispatchDroppedZero verifies that /api/v1/system/health
// includes dispatch_dropped:0 when no events have been lost.
func TestSystemHealth_DispatchDroppedZero(t *testing.T) {
	srv := startTestServer(t, nil)

	var body struct {
		Metrics struct {
			TotalProcessed  int64 `json:"totalProcessed"`
			TotalErrors     int64 `json:"totalErrors"`
			DispatchDropped int64 `json:"dispatchDropped"`
		} `json:"metrics"`
	}
	getJSON(t, srv, "/api/v1/system/health", &body)
	if body.Metrics.DispatchDropped != 0 {
		t.Errorf("dispatchDropped: got %d, want 0", body.Metrics.DispatchDropped)
	}
}

// TestSystemHealth_DispatchDroppedNonZero verifies that when the state
// carries a non-zero DispatchDropped count, it is surfaced in the health
// response metrics.
func TestSystemHealth_DispatchDroppedNonZero(t *testing.T) {
	srv := startTestServer(t, func(s *localapi.State) {
		s.DispatchDropped = 7
	})

	var body struct {
		Metrics struct {
			DispatchDropped int64 `json:"dispatchDropped"`
		} `json:"metrics"`
	}
	getJSON(t, srv, "/api/v1/system/health", &body)
	if body.Metrics.DispatchDropped != 7 {
		t.Errorf("dispatchDropped: got %d, want 7", body.Metrics.DispatchDropped)
	}
}

// TestSetState_DispatchDroppedWired verifies that SetState propagates the
// DispatchDropped counter and it is reflected in subsequent /health polls.
func TestSetState_DispatchDroppedWired(t *testing.T) {
	srv := startTestServer(t, nil)

	started := time.Date(2026, 5, 11, 21, 0, 0, 0, time.UTC)
	srv.SetState(localapi.State{
		Version:         "0.3.3",
		SessionID:       "sess",
		StartedAt:       started,
		CloudAPIURL:     "https://api.vaultmtg.app/api/v1",
		BFFReachable:    true,
		DispatchDropped: 42,
	})

	var body struct {
		Metrics struct {
			DispatchDropped int64 `json:"dispatchDropped"`
		} `json:"metrics"`
	}
	getJSON(t, srv, "/api/v1/system/health", &body)
	if body.Metrics.DispatchDropped != 42 {
		t.Errorf("dispatchDropped after SetState: got %d, want 42", body.Metrics.DispatchDropped)
	}
}

func TestSetStateUpdatesEndpoints(t *testing.T) {
	srv := startTestServer(t, nil)

	// Mutate published state and re-fetch /version.
	srv.SetState(localapi.State{
		Version:     "0.3.1-rc19",
		SessionID:   "live-new-session",
		StartedAt:   time.Now().UTC(),
		CloudAPIURL: "https://api.vaultmtg.app/api/v1",
	})

	var body struct {
		Version string `json:"version"`
	}
	getJSON(t, srv, "/api/v1/system/version", &body)
	if body.Version != "0.3.1-rc19" {
		t.Errorf("version after SetState: %q", body.Version)
	}
}

// helperInfoBody is the shape of the helper_info sub-object in /system/status.
type helperInfoBody struct {
	HelperInstalled bool    `json:"helper_installed"`
	KeychainError   *string `json:"keychain_error"`
	LastSync        *string `json:"last_sync"`
}

// systemStatusWithHelper is the full /system/status response shape including
// the new helper_info field added by #1439.
type systemStatusWithHelper struct {
	Status    string         `json:"status"`
	Connected bool           `json:"connected"`
	Mode      string         `json:"mode"`
	URL       string         `json:"url"`
	Port      int            `json:"port"`
	HelperInfo helperInfoBody `json:"helper_info"`
}

// TestSystemStatus_HelperInfo_DefaultsWhenNotSet verifies that when the State
// carries no helper fields the response still includes helper_info with safe
// zero-value defaults (helper_installed=false, null keychain_error, null last_sync).
// This proves backward-compat: existing callers that don't set the new fields
// get a well-formed response, not a missing key.
func TestSystemStatus_HelperInfo_DefaultsWhenNotSet(t *testing.T) {
	srv := startTestServer(t, nil) // no helper fields set
	var body systemStatusWithHelper
	getJSON(t, srv, "/api/v1/system/status", &body)

	if body.HelperInfo.HelperInstalled {
		t.Errorf("helper_installed: want false when not set, got true")
	}
	if body.HelperInfo.KeychainError != nil {
		t.Errorf("keychain_error: want null when not set, got %q", *body.HelperInfo.KeychainError)
	}
	if body.HelperInfo.LastSync != nil {
		t.Errorf("last_sync: want null when not set, got %q", *body.HelperInfo.LastSync)
	}
}

// TestSystemStatus_HelperInfo_InstalledAndHealthy verifies that when
// State.HelperInstalled is true and no error / sync time are set, the response
// reflects that.
func TestSystemStatus_HelperInfo_InstalledAndHealthy(t *testing.T) {
	srv := startTestServer(t, func(s *localapi.State) {
		s.HelperInstalled = true
	})
	var body systemStatusWithHelper
	getJSON(t, srv, "/api/v1/system/status", &body)

	if !body.HelperInfo.HelperInstalled {
		t.Errorf("helper_installed: want true, got false")
	}
	if body.HelperInfo.KeychainError != nil {
		t.Errorf("keychain_error: want null for healthy helper, got %q", *body.HelperInfo.KeychainError)
	}
	if body.HelperInfo.LastSync != nil {
		t.Errorf("last_sync: want null when no sync recorded, got %q", *body.HelperInfo.LastSync)
	}
}

// TestSystemStatus_HelperInfo_KeychainError verifies that a non-empty
// State.HelperKeychainError is surfaced as a non-null keychain_error string.
func TestSystemStatus_HelperInfo_KeychainError(t *testing.T) {
	const wantErr = "keychain item not found"
	srv := startTestServer(t, func(s *localapi.State) {
		s.HelperInstalled = true
		s.HelperKeychainError = wantErr
	})
	var body systemStatusWithHelper
	getJSON(t, srv, "/api/v1/system/status", &body)

	if body.HelperInfo.KeychainError == nil {
		t.Fatal("keychain_error: want non-null, got null")
	}
	if *body.HelperInfo.KeychainError != wantErr {
		t.Errorf("keychain_error: got %q, want %q", *body.HelperInfo.KeychainError, wantErr)
	}
}

// TestSystemStatus_HelperInfo_LastSync verifies that State.HelperLastSync is
// serialised as an RFC3339 timestamp in last_sync.
func TestSystemStatus_HelperInfo_LastSync(t *testing.T) {
	syncAt := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	srv := startTestServer(t, func(s *localapi.State) {
		s.HelperInstalled = true
		s.HelperLastSync = &syncAt
	})
	var body systemStatusWithHelper
	getJSON(t, srv, "/api/v1/system/status", &body)

	if body.HelperInfo.LastSync == nil {
		t.Fatal("last_sync: want non-null, got null")
	}
	if *body.HelperInfo.LastSync != "2026-06-01T12:00:00Z" {
		t.Errorf("last_sync: got %q, want 2026-06-01T12:00:00Z", *body.HelperInfo.LastSync)
	}
}
