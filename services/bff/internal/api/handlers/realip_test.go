package handlers_test

// realip_test.go — unit tests for the realIP() helper (PR B, ticket #1222).
//
// Trust policy: nginx sets X-Real-IP from $remote_addr (kernel-level TCP peer —
// not client-controllable) on every proxy_pass location. X-Forwarded-For is NOT
// used for rate-limiting: nginx appends $remote_addr to any existing XFF header,
// so a client can prepend spoofed IPs to the list.
//
// TDD: these tests were written RED first; implementation follows in waitlist.go.
//
// Cross-reference: hollowmark-infra/nginx/mtga-companion-ssl.conf
// proxy_set_header X-Real-IP $remote_addr — Ray verified all 4 nginx confs.

import (
	"net/http/httptest"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
)

// TestRealIP_XRealIP_Used verifies that X-Real-IP is the primary IP source.
func TestRealIP_XRealIP_Used(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.RemoteAddr = "10.0.0.1:9999"

	got := handlers.RealIPForTest(req)
	if got != "1.2.3.4" {
		t.Errorf("X-Real-IP: want 1.2.3.4, got %q", got)
	}
}

// TestRealIP_XFF_Ignored_WhenXRealIPSet verifies that a spoofed X-Forwarded-For
// header cannot influence the IP when X-Real-IP is present.
// This is the core security property of PR B.
func TestRealIP_XFF_Ignored_WhenXRealIPSet(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-Real-IP", "203.0.113.9")
	req.Header.Set("X-Forwarded-For", "evil.spoofed.ip, 10.0.0.2")
	req.RemoteAddr = "10.0.0.2:1234"

	got := handlers.RealIPForTest(req)
	if got != "203.0.113.9" {
		t.Errorf("X-Real-IP must win over XFF; want 203.0.113.9, got %q", got)
	}
}

// TestRealIP_XFF_Spoofed_Chain_Cannot_Win verifies that a multi-hop spoofed
// XFF chain (the classic rate-limit bypass) cannot override X-Real-IP.
func TestRealIP_XFF_Spoofed_Chain_Cannot_Win(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	// Client prepended spoofed IPs to XFF; nginx appended real remote.
	req.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2, 3.3.3.3")
	req.Header.Set("X-Real-IP", "198.51.100.5") // nginx-set from $remote_addr
	req.RemoteAddr = "198.51.100.5:8080"

	got := handlers.RealIPForTest(req)
	if got != "198.51.100.5" {
		t.Errorf("spoofed XFF chain: want X-Real-IP 198.51.100.5, got %q", got)
	}
}

// TestRealIP_NoProxy_RemoteAddr verifies the fallback for local dev / test
// environments where no nginx proxy sets X-Real-IP.
func TestRealIP_NoProxy_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	// No X-Real-IP header (local dev without nginx).
	req.RemoteAddr = "1.2.3.4:5678"

	got := handlers.RealIPForTest(req)
	if got != "1.2.3.4" {
		t.Errorf("RemoteAddr fallback: want 1.2.3.4, got %q", got)
	}
}

// TestRealIP_RemoteAddr_IPv6 verifies port stripping on an IPv6 RemoteAddr.
func TestRealIP_RemoteAddr_IPv6(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.RemoteAddr = "[::1]:8080"

	got := handlers.RealIPForTest(req)
	// strings.LastIndex strips the ":port" suffix — result is "[::1]".
	if got != "[::1]" {
		t.Errorf("IPv6 RemoteAddr fallback: want [::1], got %q", got)
	}
}

// TestRealIP_XRealIP_Whitespace_Trimmed verifies leading/trailing whitespace
// in the X-Real-IP header value is trimmed.
func TestRealIP_XRealIP_Whitespace_Trimmed(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-Real-IP", "  203.0.113.1  ")
	req.RemoteAddr = "10.0.0.1:1234"

	got := handlers.RealIPForTest(req)
	if got != "203.0.113.1" {
		t.Errorf("whitespace trim: want 203.0.113.1, got %q", got)
	}
}
