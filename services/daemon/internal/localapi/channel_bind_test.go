package localapi_test

// Real-bind reachability tests for the channel-derived local-API port
// (#667 / ADR-049 Ticket 5).
//
// These tests are deliberately distinct from the value-assertion tests in
// internal/install/runtime_wiring_test.go (TestFF7_LocalAPIPortsDistinct).
// That test proves the derived *int* flows; it does NOT prove the daemon
// actually BINDS that port — which is exactly why #667 shipped past green.
// The tests here start a real localapi.Server on the channel-derived port and
// confirm a live listener answers GET /health on that port for BOTH channels,
// plus that the two channels bind simultaneously without collision.

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/daemon/internal/install"
	"github.com/RdHamilton/vault-mtg/services/daemon/internal/localapi"
)

// portFree reports whether the given loopback port can be bound right now.
// If something external already holds it (e.g. a real daemon running on the
// dev machine, or a sibling test), we skip rather than fail — the bind
// behaviour under test is the daemon's, not the environment's.
func portFree(t *testing.T, port int) bool {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// startOnChannelPort starts a localapi.Server on the exact port the given
// channel derives via install.Identity. Returns the server and the derived
// port. The caller is responsible for Stop.
func startOnChannelPort(t *testing.T, channel string) (*localapi.Server, int) {
	t.Helper()
	port := install.Identity(channel).LocalAPIPort
	srv := localapi.New(port, localapi.State{Version: "bind-test", AuthStatus: localapi.AuthStatusSetupRequired})
	if err := srv.Start(); err != nil {
		t.Fatalf("channel %q: Start on port %d: %v", channel, port, err)
	}
	return srv, port
}

// probeHealth issues GET /health against 127.0.0.1:port and asserts 200.
func probeHealth(t *testing.T, port int) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		t.Fatalf("GET /health on port %d: %v", port, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health on port %d: status %d, want 200", port, resp.StatusCode)
	}
}

// TestChannelBind_Stable_RealBind9001 asserts the stable channel's daemon
// binds and is reachable on 9001 — the actual listener, not the computed int.
func TestChannelBind_Stable_RealBind9001(t *testing.T) {
	const wantPort = 9001
	if got := install.Identity(install.ChannelStable).LocalAPIPort; got != wantPort {
		t.Fatalf("stable channel derived port %d, want %d", got, wantPort)
	}
	if !portFree(t, wantPort) {
		t.Skipf("port %d already in use on this host; skipping real-bind probe", wantPort)
	}

	srv, port := startOnChannelPort(t, install.ChannelStable)
	defer func() { _ = srv.Stop() }()

	if port != wantPort {
		t.Fatalf("stable bind port %d, want %d", port, wantPort)
	}
	if got := srv.Addr(); got != fmt.Sprintf("127.0.0.1:%d", wantPort) {
		t.Fatalf("stable listener bound %q, want 127.0.0.1:%d", got, wantPort)
	}
	probeHealth(t, wantPort)
}

// TestChannelBind_Staging_RealBind9011 asserts the staging channel's daemon
// binds and is reachable on 9011 (the prod port collision #667 was about).
// This is the test that would have caught #667: the value-assertion test in
// runtime_wiring_test.go passed while the daemon still bound 9001.
func TestChannelBind_Staging_RealBind9011(t *testing.T) {
	const wantPort = 9011
	if got := install.Identity(install.ChannelStaging).LocalAPIPort; got != wantPort {
		t.Fatalf("staging channel derived port %d, want %d", got, wantPort)
	}
	if !portFree(t, wantPort) {
		t.Skipf("port %d already in use on this host; skipping real-bind probe", wantPort)
	}

	srv, port := startOnChannelPort(t, install.ChannelStaging)
	defer func() { _ = srv.Stop() }()

	if port != wantPort {
		t.Fatalf("staging bind port %d, want %d", port, wantPort)
	}
	if got := srv.Addr(); got != fmt.Sprintf("127.0.0.1:%d", wantPort) {
		t.Fatalf("staging listener bound %q, want 127.0.0.1:%d", got, wantPort)
	}
	probeHealth(t, wantPort)
}

// TestChannelBind_DualRunNoCollision asserts that a stable daemon (9001) and a
// staging daemon (9011) bind simultaneously and both answer /health — the
// concurrent dual-run invariant (FF-7 / ADR-049 §5) at the real-bind level.
func TestChannelBind_DualRunNoCollision(t *testing.T) {
	stablePort := install.Identity(install.ChannelStable).LocalAPIPort
	stagingPort := install.Identity(install.ChannelStaging).LocalAPIPort
	if stablePort == stagingPort {
		t.Fatalf("stable and staging derive the same port %d — cannot dual-run", stablePort)
	}
	if !portFree(t, stablePort) || !portFree(t, stagingPort) {
		t.Skipf("ports %d/%d already in use on this host; skipping dual-run probe", stablePort, stagingPort)
	}

	stableSrv, sp := startOnChannelPort(t, install.ChannelStable)
	defer func() { _ = stableSrv.Stop() }()
	stagingSrv, gp := startOnChannelPort(t, install.ChannelStaging)
	defer func() { _ = stagingSrv.Stop() }()

	// Both listeners are live at the same time.
	probeHealth(t, sp)
	probeHealth(t, gp)
}
