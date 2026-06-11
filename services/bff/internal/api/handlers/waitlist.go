package handlers

import (
	"context"
	"crypto/md5" //nolint:gosec // Mailchimp subscriber hash is MD5 by Mailchimp API spec — not a security choice.
	"encoding/json"
	"fmt"
	"log"
	"net/http"
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
	InsertIfNew(ctx context.Context, email string, utmSource, utmMedium, utmCampaign *string, referrer *string) (id string, position int64, created bool, err error)
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
type WaitlistHandler struct {
	repo      waitlistRepo
	mailchimp MailchimpClient
	analytics *analytics.Client
	rateMu    sync.Mutex
	rateByIP  map[string]*waitlistRateEntry
}

// NewWaitlistHandler returns a handler backed by repo. mc may be nil in tests
// or when MAILCHIMP_API_KEY is not configured; Mailchimp signup is skipped.
func NewWaitlistHandler(repo waitlistRepo, mc MailchimpClient) *WaitlistHandler {
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

// waitlistRequest is the JSON body for POST /api/v1/waitlist.
type waitlistRequest struct {
	Email       string `json:"email"`
	UTMSource   string `json:"utm_source"`
	UTMMedium   string `json:"utm_medium"`
	UTMCampaign string `json:"utm_campaign"`
	Referrer    string `json:"referrer"`
}

// waitlistResponse is the JSON body returned by POST /api/v1/waitlist on a
// successful new signup: {"position": N} where N is the 1-based row count.
type waitlistResponse struct {
	Position int64 `json:"position"`
}

// Join handles POST /api/v1/waitlist.
func (h *WaitlistHandler) Join(w http.ResponseWriter, r *http.Request) {
	// Per-IP rate limit: 5 req/hour. Uses the same rateEntry type as daemon_register.
	ip := realIP(r)
	if !h.rateAllow(ip) {
		writeJSONError(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	var req waitlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[waitlist] decode body: %v", err)
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		writeJSONError(w, "email is required", http.StatusBadRequest)
		return
	}

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
	referrer := nullableStr(req.Referrer)

	// Insert or no-op. ON CONFLICT DO NOTHING: no row returned → email already existed.
	// Returns position (1-based COUNT(*)) on new insert so the SPA can show the
	// queue position immediately without a second round-trip.
	id, position, created, err := h.repo.InsertIfNew(r.Context(), email, utmSource, utmMedium, utmCampaign, referrer)
	if err != nil {
		log.Printf("[waitlist] InsertIfNew email=%s: %v", email, err)
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
	if h.mailchimp != nil {
		go func(rowID, addr string) {
			mcCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			mcErr := h.mailchimp.AddMember(mcCtx, addr)
			if mcErr != nil {
				log.Printf("[waitlist] mailchimp AddMember email=%s: %v (non-fatal; reconciler will retry)", addr, mcErr)
				return
			}

			if dbErr := h.repo.UpdateMailchimpStatus(mcCtx, rowID, "subscribed"); dbErr != nil {
				log.Printf("[waitlist] UpdateMailchimpStatus id=%s: %v", rowID, dbErr)
			}
		}(id, email)
	}

	// Fire funnel_waitlist_signup_completed on the new-email path only.
	// Goroutine-dispatched: analytics latency must not block the HTTP response.
	// distinct_id: SHA-256 hash of email — uses identityhash for PII safety.
	// Operational: true — waitlist signup is pre-auth; GDPR §6(1)(f) carve-out.
	ac := h.analytics
	go func(addr string, src, medium, campaign, ref *string) {
		hashedAddr := identityhash.HashAccountID(addr)
		if err := ac.Capture(context.Background(), hashedAddr, "funnel_waitlist_signup_completed", map[string]any{
			"utm_source":   strOrEmpty(src),
			"utm_medium":   strOrEmpty(medium),
			"utm_campaign": strOrEmpty(campaign),
			"referrer":     strOrEmpty(ref),
		}, analytics.CaptureOptions{Operational: true}); err != nil {
			log.Printf("[waitlist] analytics capture: %v", err)
		}
	}(email, utmSource, utmMedium, utmCampaign, referrer)

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
func (h *WaitlistHandler) rateAllow(ip string) bool {
	h.rateMu.Lock()
	entry, ok := h.rateByIP[ip]
	if !ok {
		entry = &waitlistRateEntry{}
		h.rateByIP[ip] = entry
	}
	h.rateMu.Unlock()
	return entry.allow()
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
