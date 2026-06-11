package handlers

import (
	"context"
	"crypto/md5" //nolint:gosec // Mailchimp subscriber hash is MD5 by Mailchimp API spec — not a security choice.
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/analytics"
	"github.com/RdHamilton/hollowmark/services/bff/internal/identityhash"
)

// RC4 note: hashAccountID (posthog.go) uses SHA-256 for PostHog PII hashing.
// Mailchimp's subscriber hash is MD5(lowercase(email)) per the Mailchimp API
// spec (https://mailchimp.com/developer/marketing/api/list-members/). These
// are different algorithms for different purposes — MD5 is required here by
// the external API contract, not as a security primitive.

const (
	// waitlistRateLimitWindow is the sliding window for per-IP rate limiting.
	// Mirrors the daemon_register per-account window (1 hour).
	waitlistRateLimitWindow = time.Hour

	// waitlistRateLimitMax is the maximum POST /api/v1/waitlist calls allowed
	// per IP per waitlistRateLimitWindow (RC5).
	waitlistRateLimitMax = 5

	// waitlistBodyLimitBytes is the maximum accepted request body size (4 KB).
	// Requests with a body exceeding this limit are rejected with 413 (#132).
	waitlistBodyLimitBytes = 4096

	// WaitlistRateMapCap is the maximum number of entries in the rateByIP map.
	// Mirrors BootSignalRateMapCap: without a bound, unique spoofed X-Real-IP
	// values can grow the map without limit on this public unauth endpoint.
	// At ~200 bytes per entry, 50 000 entries ≈ 10 MB ceiling.
	// Exported so tests can assert the eviction bound.
	WaitlistRateMapCap = 50_000
)

// waitlistRateEntry tracks request timestamps for one IP address.
// It is separate from rateEntry (daemon_register.go) so it can apply the
// waitlist-specific window and max without touching the daemon rate-limit path.
type waitlistRateEntry struct {
	mu        sync.Mutex
	callTimes []time.Time
}

// allow returns true if the request is within the rate limit, false otherwise.
// Uses waitlistRateLimitWindow and waitlistRateLimitMax.
func (e *waitlistRateEntry) allow() bool {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-waitlistRateLimitWindow)

	filtered := e.callTimes[:0]
	for _, t := range e.callTimes {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	e.callTimes = filtered

	if len(e.callTimes) >= waitlistRateLimitMax {
		return false
	}
	e.callTimes = append(e.callTimes, now)
	return true
}

// waitlistRepo is the subset of WaitlistRepository used by WaitlistHandler.
type waitlistRepo interface {
	InsertIfNew(ctx context.Context, email string, utmSource, utmMedium, utmCampaign *string, utmContent, utmTerm *string, referrer *string) (id string, position int64, created bool, err error)
	UpdateMailchimpStatus(ctx context.Context, id, status string) error
}

// MailchimpClient is a mockable interface for the Mailchimp Marketing API.
type MailchimpClient interface {
	AddMember(ctx context.Context, email string) error
}

// WaitlistHandler handles POST /api/v1/waitlist.
//
// Public endpoint (no Clerk auth required). Rate limited at 5 req/hour per IP.
//
// Guard order (hardened per tickets #132/#133/#134/#135):
//  1. Content-Type: application/json prefix check → 415
//  2. http.MaxBytesReader wraps body (4096 bytes) → 413 if exceeded
//  3. Per-IP rate check (realIP) → 429
//  4. json.Decode → 400 on parse/oversize error
//  5. Email empty check → 400
//  6. net/mail.ParseAddress email format check → 400
//  7. InsertIfNew → 500 on DB error; 409 on duplicate; 200 on new insert
//
// New email: 200 OK with {"position": N} (1-based row count at insert time).
// Duplicate email: 409 Conflict with {"error": "This email is already registered."}.
// Mailchimp is NOT called again on duplicate — the member is already subscribed.
//
// Mailchimp signup is best-effort and non-fatal: a Mailchimp 5xx results in the
// DB row retaining mailchimp_status='failed' and the handler still returning
// 200. A future reconciler (separate ticket) picks up failed rows.
//
// Analytics: fires funnel_waitlist_signup_completed on the new-email path only.
// Goroutine-dispatched so analytics latency does not block the HTTP response.
//
// PII logging (#135): log lines omit the raw email entirely. Identity adds
// nothing to diagnosis on the DB error path; the Mailchimp reconciler retries
// off the row, not the log. Add email correlation to logs only if a concrete
// operational need arises.
type WaitlistHandler struct {
	repo      waitlistRepo
	mailchimp MailchimpClient
	analytics *analytics.Client
	rateMu    sync.Mutex
	rateByIP  map[string]*waitlistRateEntry
}

// NewWaitlistHandler returns a handler backed by repo.
// mc may be nil in tests or when MAILCHIMP_API_KEY is not configured.
// piiSalt is accepted for forward compatibility — per Ray Q4 ruling, email is
// omitted from log lines entirely (not hashed). The parameter is intentionally
// blank so callers pass cfg.AnalyticsPIISalt and the signature stays stable if
// salted-hash logging is added later.
func NewWaitlistHandler(repo waitlistRepo, mc MailchimpClient, _ string) *WaitlistHandler {
	return &WaitlistHandler{
		repo:      repo,
		mailchimp: mc,
		analytics: analytics.NewClient(analytics.NoopEnqueuer{}, analytics.NewNoopHaltChecker()),
		rateByIP:  make(map[string]*waitlistRateEntry),
	}
}

// WithPostHogClient is deprecated. Use WithAnalyticsClient instead.
func (h *WaitlistHandler) WithPostHogClient(ph analytics.PostHogEnqueuer) *WaitlistHandler {
	return h.WithAnalyticsClient(analytics.NewClient(ph, analytics.NewNoopHaltChecker()))
}

// WithAnalyticsClient wires an analytics.Client into the handler.
func (h *WaitlistHandler) WithAnalyticsClient(c *analytics.Client) *WaitlistHandler {
	h.analytics = c
	return h
}

// WaitlistRateMapSize returns the current number of entries in the rateByIP map.
// Exported for test use only — lets the test package assert the eviction bound.
func (h *WaitlistHandler) WaitlistRateMapSize() int {
	h.rateMu.Lock()
	defer h.rateMu.Unlock()
	return len(h.rateByIP)
}

// waitlistRequest is the JSON body for POST /api/v1/waitlist.
type waitlistRequest struct {
	Email       string `json:"email"`
	UTMSource   string `json:"utm_source"`
	UTMMedium   string `json:"utm_medium"`
	UTMCampaign string `json:"utm_campaign"`
	UTMContent  string `json:"utm_content"`
	UTMTerm     string `json:"utm_term"`
	Referrer    string `json:"referrer"`
}

// waitlistResponse is the JSON body returned by POST /api/v1/waitlist on a
// successful new signup: {"position": N} where N is the 1-based row count.
type waitlistResponse struct {
	Position int64 `json:"position"`
}

// Join handles POST /api/v1/waitlist.
func (h *WaitlistHandler) Join(w http.ResponseWriter, r *http.Request) {
	// 1. Content-Type guard: must be application/json (prefix match allows
	//    "application/json; charset=utf-8" etc.). Waitlist is a user-facing
	//    JSON POST — unlike boot-signal which accepts text/plain (sendBeacon
	//    CORS-simple constraint). 415 Unsupported Media Type.
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		writeJSONError(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}

	// 2. Body size cap: 4096 bytes. Wrap before any read so that an oversized
	//    body is bounded and the subsequent json.Decode returns a MaxBytesError.
	r.Body = http.MaxBytesReader(w, r.Body, waitlistBodyLimitBytes)

	// 3. Per-IP rate limit: 5 req/hour.
	ip := realIP(r)
	if !h.rateAllow(ip) {
		writeJSONError(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	// 4. Decode. MaxBytesReader returns a *http.MaxBytesError when the body
	//    exceeds the limit — map that to 413; all other decode errors to 400.
	var req waitlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		if isMaxBytesError(err) {
			writeJSONError(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		log.Printf("[waitlist] decode body: %v", err)
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// 5. Empty-email check.
	rawEmail := strings.TrimSpace(req.Email)
	if rawEmail == "" {
		writeJSONError(w, "email is required", http.StatusBadRequest)
		return
	}

	// 6. Email format validation (net/mail.ParseAddress — RFC 5322, stdlib, no deps).
	//    Normalise to strings.ToLower(strings.TrimSpace(addr.Address)) before storing:
	//    - addr.Address strips the display-name wrapper ("Name <user@x.com>" → "user@x.com")
	//    - lowercase + trim ensures dedup stability ("User@X.com" ≡ "user@x.com")
	//    Ray Q2 ruling: store addr.Address, not raw req.Email.
	addr, err := mail.ParseAddress(rawEmail)
	if err != nil {
		writeJSONError(w, "invalid email address", http.StatusBadRequest)
		return
	}
	email := strings.ToLower(strings.TrimSpace(addr.Address))

	nullableStr := func(s string) *string {
		v := strings.TrimSpace(s)
		if v == "" {
			return nil
		}
		return &v
	}

	utmSource := nullableStr(req.UTMSource)
	utmMedium := nullableStr(req.UTMMedium)
	utmCampaign := nullableStr(req.UTMCampaign)
	utmContent := nullableStr(req.UTMContent)
	utmTerm := nullableStr(req.UTMTerm)
	referrer := nullableStr(req.Referrer)

	// 7. Insert or no-op. ON CONFLICT DO NOTHING: no row returned → email already existed.
	// Returns position (1-based COUNT(*)) on new insert so the SPA can show the
	// queue position immediately without a second round-trip.
	// PII log (#135): omit email from the error log — identity adds nothing to
	// diagnosis on this path; the reconciler retries off the DB row, not the log.
	id, position, created, err := h.repo.InsertIfNew(r.Context(), email, utmSource, utmMedium, utmCampaign, utmContent, utmTerm, referrer)
	if err != nil {
		log.Printf("[waitlist] InsertIfNew: %v", err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if !created {
		// Duplicate email: the email is already on the waitlist. Do NOT call
		// Mailchimp again — the member is already subscribed. Return 409 Conflict
		// per the shipped SPA contract (Frank's PR #32).
		writeJSONError(w, "This email is already registered.", http.StatusConflict)
		return
	}

	// Best-effort Mailchimp signup. Non-fatal: on any error the row keeps
	// mailchimp_status='failed' and a future reconciler will retry.
	// PII log (#135): omit email from the error log — the reconciler retries
	// off the DB row (row id), not the log line.
	if h.mailchimp != nil {
		go func(rowID string) {
			mcCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			mcErr := h.mailchimp.AddMember(mcCtx, email)
			if mcErr != nil {
				log.Printf("[waitlist] mailchimp AddMember id=%s: %v (non-fatal; reconciler will retry)", rowID, mcErr)
				return
			}

			if dbErr := h.repo.UpdateMailchimpStatus(mcCtx, rowID, "subscribed"); dbErr != nil {
				log.Printf("[waitlist] UpdateMailchimpStatus id=%s: %v", rowID, dbErr)
			}
		}(id)
	}

	// Fire funnel_waitlist_signup_completed on the new-email path only.
	// Goroutine-dispatched: analytics latency must not block the HTTP response.
	// distinct_id: HashAccountID(email) — unsalted, per I-10 / Ray Q1 ruling.
	// (PostHog dedup requires stability across sessions — unsalted is intentional.)
	// Operational: true — waitlist signup is pre-auth; GDPR §6(1)(f) carve-out.
	ac := h.analytics
	go func(src, medium, campaign, content, term, ref *string) {
		hashedAddr := identityhash.HashAccountID(email)
		if err := ac.Capture(context.Background(), hashedAddr, "funnel_waitlist_signup_completed", map[string]any{
			"utm_source":   strOrEmpty(src),
			"utm_medium":   strOrEmpty(medium),
			"utm_campaign": strOrEmpty(campaign),
			"utm_content":  strOrEmpty(content),
			"utm_term":     strOrEmpty(term),
			"referrer":     strOrEmpty(ref),
		}, analytics.CaptureOptions{Operational: true}); err != nil {
			log.Printf("[waitlist] analytics capture: %v", err)
		}
	}(utmSource, utmMedium, utmCampaign, utmContent, utmTerm, referrer)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(waitlistResponse{Position: position}); err != nil {
		log.Printf("[waitlist] encode: %v", err)
	}
}

// strOrEmpty returns the dereferenced string or "" when p is nil.
func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// rateAllow checks and records a rate-limit call for ip.
// On new-IP insert it enforces the WaitlistRateMapCap bound (mirrors
// BootSignalHandler.rateAllow): if the map is at capacity, expired entries
// are evicted first; if none are expired the oldest entry is removed.
// Parallel implementation: types differ from bootSignalRateEntry so no shared
// helper; see BootSignalHandler.rateAllow for the parallel. Re-evaluate
// extraction at a third consumer.
func (h *WaitlistHandler) rateAllow(ip string) bool {
	h.rateMu.Lock()
	entry, ok := h.rateByIP[ip]
	if !ok {
		if len(h.rateByIP) >= WaitlistRateMapCap {
			h.evictOldestLocked()
		}
		entry = &waitlistRateEntry{}
		h.rateByIP[ip] = entry
	}
	h.rateMu.Unlock()
	return entry.allow()
}

// evictOldestLocked removes stale or oldest entries to make room for a new IP.
// Must be called with h.rateMu held. Mirrors BootSignalHandler.evictOldestLocked.
// Strategy: scan for any fully-expired entry first; if none, remove least-recent.
func (h *WaitlistHandler) evictOldestLocked() {
	cutoff := time.Now().Add(-waitlistRateLimitWindow)

	// First pass: evict any fully-expired entry.
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

// realIP extracts the client IP for rate-limiting.
//
// Trust policy: nginx sets X-Real-IP from $remote_addr (kernel-level TCP peer
// — not client-controllable) on every proxy_pass location. X-Forwarded-For is
// NOT used: nginx appends $remote_addr to any existing XFF header, so a client
// can prepend spoofed IPs to the list. X-Real-IP is single-valued and cannot
// be influenced by the client when nginx is in the path.
// See hollowmark-infra/nginx/mtga-companion-ssl.conf proxy_set_header directives
// (verified by Ray on all 4 nginx confs: prod SSL, prod plain, staging-api
// hollowmark.app, staging-api vaultmtg.app — ticket #1222).
// If X-Real-IP is absent (local dev, tests without a proxy), fall back to
// RemoteAddr with port stripped.
func realIP(r *http.Request) string {
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// Strip port from RemoteAddr (host:port format, including IPv6 [::1]:port).
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

// RealIPForTest exposes realIP for package-external test use only.
// Must not be called from production code.
func RealIPForTest(r *http.Request) string { return realIP(r) }

// mailchimpSubscriberHash returns the MD5 hash of the lower-cased email address
// as required by the Mailchimp Marketing API for subscriber lookups and adds.
// MD5 is mandated by Mailchimp's API spec — this is not a security primitive.
func mailchimpSubscriberHash(email string) string {
	h := md5.Sum([]byte(strings.ToLower(email))) //nolint:gosec
	return fmt.Sprintf("%x", h)
}
