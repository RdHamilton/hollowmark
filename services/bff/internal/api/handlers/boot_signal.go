package handlers

// boot_signal.go — handler for POST /api/v1/boot-signal (ADR-077, ticket #1212).
//
// This endpoint receives config-failure beacons from the SPA at startup.
// It is intentionally public (no Clerk auth — fires before config inits),
// rate limited at 20 req/min per IP, and sinks to a structured CloudWatch
// log line only (no DB writes, no PostHog events).
//
// Status codes:
//   - 400: body > 1 KB OR body not valid JSON OR schema invalid (enum mismatch).
//   - 204: all other paths — valid request accepted, over-limit silent drop.
//         NEVER 429 (AC4 / ADR-077 silent-drop policy).
//
// Ordering (Ray R1 binding ruling):
//   1. Wrap body with MaxBytesReader (bounds any drain even on silent drop).
//   2. realIP(r) → rate check → over-limit: 204 early-return WITHOUT reading body.
//   3. io.ReadAll → error (over-size): bounded rejection log, 400.
//   4. json.Unmarshal → error: bounded rejection log, 400.
//   5. Enum validation → fail: bounded rejection log, 400.
//   6. Emit structured log line, 204.
//
// Interplay note: an over-limit oversized payload returns 204 (AC4 silent-drop
// dominates over AC3's 400) because the rate check precedes the body read.
// Go's http.Server drains and discards unread request bodies after the handler
// returns; MaxBytesReader bounds any such drain to bootSignalBodyLimitBytes.
//
// IP hashing (Ray R2): uses identityhash.HashPII(salt, ip) — salted SHA-256[:16].
// HashAccountID (unsalted) is NOT used here because the ~4B IPv4 address space
// is trivially precomputable without a salt. If the PII salt is empty (local dev
// without ANALYTICS_PII_SALT set), ip_hash=disabled is logged instead.

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/identityhash"
)

const (
	// bootSignalBodyLimitBytes is the maximum accepted request body size (1 KB).
	// Payloads exceeding this are rejected with 400 (AC3).
	bootSignalBodyLimitBytes = 1024

	// bootSignalRateLimitMax is the maximum requests allowed per IP per window (AC4).
	bootSignalRateLimitMax = 20

	// bootSignalRateLimitWindow is the sliding window for per-IP rate limiting.
	bootSignalRateLimitWindow = time.Minute

	// BootSignalRateMapCap is the maximum number of entries in the rateByIP map.
	// Security control (S-07 FINDING-2): without a cap, unique spoofed
	// X-Forwarded-For values grow the map without bound on this public unauth
	// endpoint. At ~200 bytes per entry, 50 000 entries ≈ 10 MB ceiling.
	// When the map hits the cap on insert, expired entries are evicted first;
	// if none are expired the oldest entry (by last-call time) is removed.
	// Exported so the test package can assert the bound.
	BootSignalRateMapCap = 50_000
)

// validBootFailureTypes is the set of accepted failure_type enum values.
// Cross-contract with Frank's #1208: "missing_field" uses underscore, singular.
var validBootFailureTypes = map[string]bool{
	"network":       true,
	"parse":         true,
	"missing_field": true,
}

// validBootEnvironments is the set of accepted environment enum values.
var validBootEnvironments = map[string]bool{
	"production": true,
	"staging":    true,
}

// bootSignalRequest is the JSON schema for POST /api/v1/boot-signal.
// Content-Type is text/plain (sendBeacon CORS-simple constraint — AC2) but the
// body is valid JSON parsed server-side. Unknown fields are silently dropped by
// json.Unmarshal into a typed struct (no reflection of attacker-controlled data).
type bootSignalRequest struct {
	FailureType string `json:"failure_type"`
	Environment string `json:"environment"`
	AppVersion  string `json:"app_version"`
	Timestamp   string `json:"timestamp"`
}

// bootSignalRateEntry tracks request timestamps for one IP address.
// It is owned by BootSignalHandler and follows the same per-handler-owned
// pattern as waitlistRateEntry and rateEntry (daemon_register.go).
type bootSignalRateEntry struct {
	mu        sync.Mutex
	callTimes []time.Time
}

// allow returns true if the request is within the rate limit, false otherwise.
// Implements the sliding-window eviction: call times older than the window are
// pruned on each call, so map entries self-trim without a background goroutine.
func (e *bootSignalRateEntry) allow() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-bootSignalRateLimitWindow)

	filtered := e.callTimes[:0]
	for _, t := range e.callTimes {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	e.callTimes = filtered

	if len(e.callTimes) >= bootSignalRateLimitMax {
		return false
	}
	e.callTimes = append(e.callTimes, now)
	return true
}

// BootSignalHandler handles POST /api/v1/boot-signal.
//
// Public endpoint (no Clerk auth required — AC7). Rate limited at 20 req/min
// per IP. Sink: structured CloudWatch log line only (no DB writes — AC6).
//
// The rateByIP map is capped at BootSignalRateMapCap entries (S-07 FINDING-2).
type BootSignalHandler struct {
	piiSalt  string
	rateMu   sync.Mutex
	rateByIP map[string]*bootSignalRateEntry
}

// NewBootSignalHandler constructs a BootSignalHandler with the given PII salt.
// piiSalt must be cfg.AnalyticsPIISalt (SSM /vaultmtg/{env}/analytics-pii-salt).
// If piiSalt is empty (local dev without ANALYTICS_PII_SALT), ip_hash=disabled
// is logged instead of an unsalted hash (fail-safe per Ray R2).
func NewBootSignalHandler(piiSalt string) *BootSignalHandler {
	return &BootSignalHandler{
		piiSalt:  piiSalt,
		rateByIP: make(map[string]*bootSignalRateEntry),
	}
}

// RateMapSize returns the current number of entries in the rateByIP map.
// Exported for test use only — lets the test package assert the eviction bound
// without accessing unexported fields.
func (h *BootSignalHandler) RateMapSize() int {
	h.rateMu.Lock()
	defer h.rateMu.Unlock()
	return len(h.rateByIP)
}

// Handle implements the POST /api/v1/boot-signal handler.
//
// Ordering per Ray R1: rate check BEFORE body read.
func (h *BootSignalHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// 1. Wrap body with MaxBytesReader. This bounds any subsequent drain even
	//    on the silent-drop (over-limit) path where we return before reading.
	r.Body = http.MaxBytesReader(w, r.Body, bootSignalBodyLimitBytes)

	// 2. Rate check — BEFORE body read (Ray R1).
	//    Over-limit: silent 204 drop. NEVER 429.
	ip := realIP(r)
	if !h.rateAllow(ip) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// 3. Read body. MaxBytesReader returns a *http.MaxBytesError when the limit
	//    is exceeded (Go 1.19+). Any read error (including oversize) → 400.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		reason := "oversize"
		if !isMaxBytesError(err) {
			reason = "read"
		}
		log.Printf("level=warn msg=boot_signal_rejected reason=%s", reason)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 4. Parse JSON. Content-Type is text/plain per sendBeacon CORS-simple
	//    constraint (AC2) — do NOT check the Content-Type header.
	var req bootSignalRequest
	if err := json.Unmarshal(body, &req); err != nil {
		log.Printf("level=warn msg=boot_signal_rejected reason=parse")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 5. Schema validation — enum check (AC3).
	//    Missing or empty required fields fail the enum check (empty string is
	//    not a valid enum value) so we do not need a separate presence check.
	if !validBootFailureTypes[req.FailureType] || !validBootEnvironments[req.Environment] {
		log.Printf("level=warn msg=boot_signal_rejected reason=schema")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 6. PII-safe structured log (AC5 / I-10).
	//    ip_hash: HashPII(salt, ip) — salted SHA-256[:16] (Ray R2).
	//    Raw IP is never logged. If salt is empty, log ip_hash=disabled.
	ipHash := "disabled"
	if h.piiSalt != "" {
		ipHash = identityhash.HashPII(h.piiSalt, ip)
	}

	log.Printf(
		"level=info msg=boot_signal failure_type=%s environment=%s app_version=%s ts=%s ip_hash=%s",
		req.FailureType,
		req.Environment,
		sanitizeBootVersion(req.AppVersion),
		time.Now().UTC().Format(time.RFC3339),
		ipHash,
	)

	w.WriteHeader(http.StatusNoContent)
}

// rateAllow checks and records a rate-limit call for ip.
// On new-IP insert it enforces the BootSignalRateMapCap bound (S-07 FINDING-2):
// if the map is at capacity, expired entries are evicted first; if none are
// expired the entry with the oldest last-call time is removed.
func (h *BootSignalHandler) rateAllow(ip string) bool {
	h.rateMu.Lock()
	entry, ok := h.rateByIP[ip]
	if !ok {
		if len(h.rateByIP) >= BootSignalRateMapCap {
			h.evictOldestLocked()
		}
		entry = &bootSignalRateEntry{}
		h.rateByIP[ip] = entry
	}
	h.rateMu.Unlock()
	return entry.allow()
}

// evictOldestLocked removes stale or oldest entries to make room for a new IP.
// Must be called with h.rateMu held.
// Strategy: scan once for any fully-expired entry (all callTimes outside the
// window); if found, remove it and return. If no fully-expired entry exists,
// remove the entry whose most-recent call time is oldest (least recently active).
// This is O(n) over the map — called only when the map reaches BootSignalRateMapCap,
// which is an exceptional condition under normal traffic.
func (h *BootSignalHandler) evictOldestLocked() {
	cutoff := time.Now().Add(-bootSignalRateLimitWindow)

	// First pass: evict any fully-expired entry (zero live call times).
	for k, e := range h.rateByIP {
		e.mu.Lock()
		live := 0
		for _, t := range e.callTimes {
			if t.After(cutoff) {
				live++
			}
		}
		e.mu.Unlock()
		if live == 0 {
			delete(h.rateByIP, k)
			return
		}
	}

	// No fully-expired entry: remove the least-recently-active entry.
	var oldestKey string
	var oldestTime time.Time
	for k, e := range h.rateByIP {
		e.mu.Lock()
		var last time.Time
		for _, t := range e.callTimes {
			if t.After(last) {
				last = t
			}
		}
		e.mu.Unlock()
		if oldestKey == "" || last.Before(oldestTime) {
			oldestKey = k
			oldestTime = last
		}
	}
	if oldestKey != "" {
		delete(h.rateByIP, oldestKey)
	}
}

// BackdateRateEntry is exported for test use only. It backdates all recorded
// call times for ip by the given duration so the test can simulate a window
// expiry without sleeping. The caller must hold no other lock on the entry.
//
// This is prefixed with a capital letter so the _test package (handlers_test)
// can call it. It must not be used in production code.
func (h *BootSignalHandler) BackdateRateEntry(ip string, by time.Duration) {
	h.rateMu.Lock()
	entry, ok := h.rateByIP[ip]
	h.rateMu.Unlock()
	if !ok {
		return
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	for i := range entry.callTimes {
		entry.callTimes[i] = entry.callTimes[i].Add(-by)
	}
}

// sanitizeBootVersion sanitizes app_version for safe log emission.
// Security (S-07 FINDING-1): replaces any control character (< 0x20 or == 0x7F)
// with '_' BEFORE truncating, preventing log-injection via embedded newlines or
// other control sequences. strings.TrimSpace alone is insufficient — it only
// strips leading/trailing whitespace; internal \n, \r, \t survive and can forge
// a second log line via log.Printf's format string expansion.
// Order: TrimSpace → control-char replacement → truncate to 32 → empty guard.
func sanitizeBootVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7F {
			return '_'
		}
		return r
	}, v)
	if len(v) > 32 {
		v = v[:32]
	}
	if v == "" {
		return "unknown"
	}
	return v
}

// isMaxBytesError returns true when err is an *http.MaxBytesError (oversize body).
// In Go 1.19+ http.MaxBytesReader returns this typed error. We check the error
// string as a fallback for older test environments.
func isMaxBytesError(err error) bool {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return true
	}
	return strings.Contains(err.Error(), "http: request body too large")
}
