package handlers_test

// boot_signal_test.go — unit tests for POST /api/v1/boot-signal handler.
// Written test-first per TDD protocol; implementation lives in boot_signal.go.
//
// Contract pins (cross-validated against Frank's #1208 plan v2):
//   - failure_type ∈ {"network", "parse", "missing_field"} — underscore, singular
//   - environment  ∈ {"production", "staging"}
//   - Body size limit: exactly 1024 bytes (1 KB hard cap)
//   - Over-limit returns 204 (NOT 429 — AC4 / Ray R1)
//   - Content-Type NOT checked — sendBeacon sends text/plain
//   - IP hash uses HashPII (salted) not HashAccountID (unsalted) — Ray R2
//   - Rate check happens BEFORE body read — Ray R1 binding ruling

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
)

// bootSignalBody is a helper that JSON-encodes a beacon payload.
func bootSignalBody(failureType, env, appVersion, ts string) *bytes.Buffer {
	m := map[string]string{
		"failure_type": failureType,
		"environment":  env,
		"app_version":  appVersion,
		"timestamp":    ts,
	}
	b, _ := json.Marshal(m)
	return bytes.NewBuffer(b)
}

func bootSignalReq(body *bytes.Buffer) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/boot-signal", body)
	// sendBeacon sends text/plain — handler must NOT reject this Content-Type.
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	return req
}

// ── Valid request paths ───────────────────────────────────────────────────────

func TestBootSignal_ValidNetwork_Returns204(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	body := bootSignalBody("network", "production", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
	req := bootSignalReq(body)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("want 204, got %d — body: %s", rr.Code, rr.Body.String())
	}
}

func TestBootSignal_ValidParse_Returns204(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	body := bootSignalBody("parse", "staging", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
	req := bootSignalReq(body)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("want 204, got %d", rr.Code)
	}
}

// TestBootSignal_ValidMissingField is the CROSS-CONTRACT ENUM PIN.
// Frank's #1208 emitter sends failure_type="missing_field" (underscore, singular).
// Any drift (e.g. "missing-fields", "missing_fields", "missingField") would cause
// the BFF to reject valid beacons with 400. This test pins the exact wire value.
func TestBootSignal_ValidMissingField_Returns204(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	body := bootSignalBody("missing_field", "production", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
	req := bootSignalReq(body)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("cross-contract enum pin: want 204 for failure_type=missing_field, got %d", rr.Code)
	}
}

// ── Invalid schema → 400 ─────────────────────────────────────────────────────

func TestBootSignal_InvalidFailureType_Returns400(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	body := bootSignalBody("boom", "production", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
	req := bootSignalReq(body)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("invalid failure_type: want 400, got %d", rr.Code)
	}
}

func TestBootSignal_InvalidEnvironment_Returns400(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	body := bootSignalBody("network", "development", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
	req := bootSignalReq(body)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("invalid environment: want 400, got %d", rr.Code)
	}
}

func TestBootSignal_NotJSON_Returns400(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	req := bootSignalReq(bytes.NewBufferString("not json at all"))
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("non-JSON body: want 400, got %d", rr.Code)
	}
}

func TestBootSignal_EmptyBody_Returns400(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	req := bootSignalReq(bytes.NewBufferString(""))
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("empty body: want 400, got %d", rr.Code)
	}
}

func TestBootSignal_MissingFailureType_Returns400(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	body := bytes.NewBufferString(`{"environment":"production","app_version":"v0.4.3","timestamp":"2026-06-10T00:00:00Z"}`)
	req := bootSignalReq(body)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("missing failure_type: want 400, got %d", rr.Code)
	}
}

func TestBootSignal_MissingEnvironment_Returns400(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	body := bytes.NewBufferString(`{"failure_type":"network","app_version":"v0.4.3","timestamp":"2026-06-10T00:00:00Z"}`)
	req := bootSignalReq(body)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("missing environment: want 400, got %d", rr.Code)
	}
}

// ── Body size boundary: exactly 1 KB ─────────────────────────────────────────

// TestBootSignal_BodyAtLimit_Returns204 — a payload at exactly 1024 bytes must
// be accepted (boundary is inclusive on the "allow" side).
func TestBootSignal_BodyAtLimit_Returns204(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	// Build a JSON payload that is exactly 1024 bytes.
	// Start with the required fields and pad "app_version" to hit the limit.
	prefix := `{"failure_type":"network","environment":"production","app_version":"`
	suffix := `","timestamp":"2026-06-10T00:00:00Z"}`
	// We need len(prefix) + len(padding) + len(suffix) == 1024
	padLen := 1024 - len(prefix) - len(suffix)
	if padLen < 1 {
		t.Fatalf("test setup: cannot construct exactly-1024-byte body (prefix+suffix=%d)", len(prefix)+len(suffix))
	}
	padding := strings.Repeat("x", padLen)
	body := prefix + padding + suffix
	if len(body) != 1024 {
		t.Fatalf("test setup: body length is %d, want 1024", len(body))
	}

	req := bootSignalReq(bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("body at 1024 bytes: want 204, got %d", rr.Code)
	}
}

// TestBootSignal_BodyOverLimit_Returns400 — a payload of 1025 bytes must be
// rejected with 400. This is the primary body-cap test (AC3).
func TestBootSignal_BodyOverLimit_Returns400(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	// 1025-byte body (one byte over the 1 KB cap).
	body := strings.Repeat("x", 1025)

	req := bootSignalReq(bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("body at 1025 bytes: want 400, got %d", rr.Code)
	}
}

// ── Rate limiting: over-limit → 204, NOT 429 ─────────────────────────────────

// TestBootSignal_OverRateLimit_Returns204 is the LOAD-BEARING rate-limit test.
// AC4 mandates 204 (not 429) on over-limit. The waitlist handler returns 429 on
// over-limit — this test pins that this handler diverges from that pattern.
// Also validates Ray's R1: rate check is BEFORE body read.
func TestBootSignal_OverRateLimit_Returns204(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	// Send 21 valid requests from the same IP — 20 allowed, 21st is over-limit.
	// All 21 must return 204 (the 21st because over-limit is a silent drop, not a 429).
	for i := range 21 {
		body := bootSignalBody("network", "production", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
		req := bootSignalReq(body)
		req.Header.Set("X-Forwarded-For", "203.0.113.1") // same IP for all calls
		rr := httptest.NewRecorder()

		h.Handle(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("call %d: want 204, got %d (MUST NOT return 429 on over-limit)", i+1, rr.Code)
		}
	}
}

// TestBootSignal_OverRateLimit_OverSizedBody_Returns204 verifies Ray R1:
// the rate check is BEFORE the body read. An over-limit request with a body
// larger than 1 KB must return 204 (rate limit wins), not 400 (body size check).
func TestBootSignal_OverRateLimit_OverSizedBody_Returns204(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	// Exhaust the rate limit for this IP.
	for range 20 {
		body := bootSignalBody("network", "production", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
		req := bootSignalReq(body)
		req.Header.Set("X-Forwarded-For", "203.0.113.2")
		rr := httptest.NewRecorder()
		h.Handle(rr, req)
	}

	// Now send an oversized body — rate-limit silent drop must win.
	req := bootSignalReq(bytes.NewBufferString(strings.Repeat("x", 2048)))
	req.Header.Set("X-Forwarded-For", "203.0.113.2")
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("over-limit oversized body (R1): want 204, got %d", rr.Code)
	}
}

// TestBootSignal_RateLimitWindow_Resets verifies the sliding-window eviction:
// after the window elapses, a previously-exhausted IP is allowed again.
// Direct callTimes manipulation (approach (a) from Ben's plan) — avoids sleeping.
func TestBootSignal_RateLimitWindow_Resets(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	ip := "203.0.113.3"
	// Exhaust the 20-req/min limit for this IP.
	for range 20 {
		body := bootSignalBody("network", "production", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
		req := bootSignalReq(body)
		req.Header.Set("X-Forwarded-For", ip)
		rr := httptest.NewRecorder()
		h.Handle(rr, req)
	}

	// Backdate the rate-limit entry's call times to more than 1 minute ago
	// so the next call falls in a fresh window.
	h.BackdateRateEntry(ip, 2*time.Minute)

	// Now a fresh request should be allowed (204).
	body := bootSignalBody("network", "production", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
	req := bootSignalReq(body)
	req.Header.Set("X-Forwarded-For", ip)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("after window reset: want 204, got %d", rr.Code)
	}
}

// ── Unknown fields dropped — no reflection ───────────────────────────────────

func TestBootSignal_UnknownFields_Dropped_Returns204(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt-value")

	body := bytes.NewBufferString(`{"failure_type":"network","environment":"production","app_version":"v0.4.3","timestamp":"2026-06-10T00:00:00Z","evil":"<script>alert(1)</script>"}`)
	req := bootSignalReq(body)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("unknown fields: want 204, got %d", rr.Code)
	}
}

// ── Log output verification ───────────────────────────────────────────────────

// TestBootSignal_LogsStructuredLine verifies the structured log format and
// that the raw IP is never emitted.
func TestBootSignal_LogsStructuredLine(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-pii-salt-for-log-test")

	// Redirect log output to a buffer for inspection.
	var logBuf strings.Builder
	orig := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(orig) })

	rawIP := "198.51.100.42"

	body := bootSignalBody("network", "production", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
	req := bootSignalReq(body)
	req.Header.Set("X-Forwarded-For", rawIP)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("valid request: want 204, got %d", rr.Code)
	}

	logged := logBuf.String()

	// Required fields must appear in the log line.
	for _, want := range []string{"msg=boot_signal", "failure_type=network", "environment=production", "ip_hash="} {
		if !strings.Contains(logged, want) {
			t.Errorf("log line missing %q — got: %s", want, logged)
		}
	}

	// Raw IP must NOT appear in the log line (I-10 compliance).
	if strings.Contains(logged, rawIP) {
		t.Errorf("log line contains raw IP %q — PII violation; must only log ip_hash", rawIP)
	}
}

// TestBootSignal_LogsRejection_OnSchemaInvalid verifies Ray C2: a bounded
// rejection log line is emitted on schema-invalid paths (after the rate check
// passes) so drift is detectable in CloudWatch.
func TestBootSignal_LogsRejection_OnSchemaInvalid(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-pii-salt")

	var logBuf strings.Builder
	orig := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(orig) })

	body := bootSignalBody("bad_type", "production", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
	req := bootSignalReq(body)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "boot_signal_rejected") {
		t.Errorf("rejection log missing boot_signal_rejected — got: %s", logged)
	}
	if !strings.Contains(logged, "reason=schema") {
		t.Errorf("rejection log missing reason=schema — got: %s", logged)
	}
}

func TestBootSignal_LogsRejection_OnOversize(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-pii-salt")

	var logBuf strings.Builder
	orig := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(orig) })

	req := bootSignalReq(bytes.NewBufferString(strings.Repeat("x", 1025)))
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "boot_signal_rejected") {
		t.Errorf("rejection log missing boot_signal_rejected — got: %s", logged)
	}
	if !strings.Contains(logged, "reason=oversize") {
		t.Errorf("rejection log missing reason=oversize — got: %s", logged)
	}
}

// TestBootSignal_IPHash_UsesSaltedPII verifies Ray R2: the ip_hash field in the
// log line is produced by HashPII (salted), not HashAccountID (unsalted).
// An empty salt causes the handler to log ip_hash=disabled (fail-safe per R2).
func TestBootSignal_IPHash_Disabled_WhenSaltEmpty(t *testing.T) {
	// Empty salt — handler must log ip_hash=disabled, not an unsalted hash.
	h := handlers.NewBootSignalHandler("")

	var logBuf strings.Builder
	orig := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(orig) })

	body := bootSignalBody("network", "production", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
	req := bootSignalReq(body)
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("empty-salt valid request: want 204, got %d", rr.Code)
	}

	logged := logBuf.String()
	if !strings.Contains(logged, "ip_hash=disabled") {
		t.Errorf("empty salt: want ip_hash=disabled in log, got: %s", logged)
	}
}

// TestBootSignal_ContentType_TextPlain_Accepted verifies AC2: the handler must
// not reject requests with Content-Type: text/plain (sendBeacon CORS-simple).
func TestBootSignal_ContentType_TextPlain_Accepted(t *testing.T) {
	h := handlers.NewBootSignalHandler("test-salt")

	body := bootSignalBody("parse", "staging", "v0.4.3", time.Now().UTC().Format(time.RFC3339))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/boot-signal", body)
	req.Header.Set("Content-Type", "text/plain")
	rr := httptest.NewRecorder()

	h.Handle(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("Content-Type: text/plain: want 204, got %d", rr.Code)
	}
}
