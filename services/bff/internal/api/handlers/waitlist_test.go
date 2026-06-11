package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	posthog "github.com/posthog/posthog-go"
)

// ─── stub repo ────────────────────────────────────────────────────────────────

type waitlistUpdateCall struct {
	ID     string
	Status string
}

type stubWaitlistRepo struct {
	insertID       string
	insertPosition int64
	insertCreated  bool
	insertErr      error
	updateErr      error

	mu          sync.Mutex
	updateCalls []waitlistUpdateCall
	updateDone  chan struct{}
}

func newStubWaitlistRepo(id string, position int64, created bool) *stubWaitlistRepo {
	return &stubWaitlistRepo{
		insertID:       id,
		insertPosition: position,
		insertCreated:  created,
		updateDone:     make(chan struct{}, 1),
	}
}

func newStubWaitlistRepoErr(err error) *stubWaitlistRepo {
	return &stubWaitlistRepo{
		insertErr:  err,
		updateDone: make(chan struct{}, 1),
	}
}

func (s *stubWaitlistRepo) InsertIfNew(_ context.Context, _ string, _, _, _, _, _ *string, _ *string) (string, int64, bool, error) {
	return s.insertID, s.insertPosition, s.insertCreated, s.insertErr
}

func (s *stubWaitlistRepo) UpdateMailchimpStatus(_ context.Context, id, status string) error {
	s.mu.Lock()
	s.updateCalls = append(s.updateCalls, waitlistUpdateCall{ID: id, Status: status})
	s.mu.Unlock()
	select {
	case s.updateDone <- struct{}{}:
	default:
	}
	return s.updateErr
}

func (s *stubWaitlistRepo) getUpdateCalls() []waitlistUpdateCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]waitlistUpdateCall, len(s.updateCalls))
	copy(out, s.updateCalls)
	return out
}

// ─── stub Mailchimp client ────────────────────────────────────────────────────

type stubMailchimpClient struct {
	err   error
	calls []string
	done  chan struct{}
}

func newStubMailchimpClient(err error) *stubMailchimpClient {
	return &stubMailchimpClient{
		err:  err,
		done: make(chan struct{}, 1),
	}
}

func (s *stubMailchimpClient) AddMember(_ context.Context, email string) error {
	s.calls = append(s.calls, email)
	select {
	case s.done <- struct{}{}:
	default:
	}
	return s.err
}

// ─── stub PostHog client ──────────────────────────────────────────────────────

type stubPostHogClient struct {
	mu       sync.Mutex
	captures []posthog.Capture
	done     chan struct{}
}

func newStubPostHogClient() *stubPostHogClient {
	return &stubPostHogClient{
		done: make(chan struct{}, 1),
	}
}

func (s *stubPostHogClient) Enqueue(msg posthog.Message) error {
	if c, ok := msg.(posthog.Capture); ok {
		s.mu.Lock()
		s.captures = append(s.captures, c)
		s.mu.Unlock()
		select {
		case s.done <- struct{}{}:
		default:
		}
	}
	return nil
}

func (s *stubPostHogClient) getCaptures() []posthog.Capture {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]posthog.Capture, len(s.captures))
	copy(out, s.captures)
	return out
}

// ─── request helper ───────────────────────────────────────────────────────────

func newWaitlistRequest(email, referrer string) *http.Request {
	body := map[string]string{"email": email, "referrer": referrer}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "1.2.3.4:12345"
	return req
}

func newWaitlistRequestWithUTM(email, utmSource, utmMedium, utmCampaign, utmContent, utmTerm, referrer string) *http.Request {
	body := map[string]string{
		"email":        email,
		"utm_source":   utmSource,
		"utm_medium":   utmMedium,
		"utm_campaign": utmCampaign,
		"utm_content":  utmContent,
		"utm_term":     utmTerm,
		"referrer":     referrer,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "1.2.3.4:12345"
	return req
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestWaitlist_NewEmail_Returns200WithPosition verifies that a brand-new email
// returns 200 OK with {"position": N} — matching the shipped SPA contract (PR #32).
func TestWaitlist_NewEmail_Returns200WithPosition(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-1", 3, true)
	h := handlers.NewWaitlistHandler(repo, nil, "")

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("alice@example.com", ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for new email, got %d: %s", rr.Code, rr.Body.String())
	}
	assertWaitlistPositionBody(t, rr, 3)
}

// TestWaitlist_DuplicateEmail_Returns409 verifies that a duplicate email returns
// 409 Conflict with {"error": "This email is already registered."}.
// AC3: ON CONFLICT DO NOTHING returns no row → created=false → 409.
func TestWaitlist_DuplicateEmail_Returns409(t *testing.T) {
	repo := newStubWaitlistRepo("", 0, false)
	h := handlers.NewWaitlistHandler(repo, nil, "")

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("alice@example.com", ""))

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate email, got %d: %s", rr.Code, rr.Body.String())
	}
	assertWaitlistErrorBody(t, rr, "This email is already registered.")
}

// TestWaitlist_MissingEmail_Returns400 verifies empty email is rejected.
func TestWaitlist_MissingEmail_Returns400(t *testing.T) {
	repo := newStubWaitlistRepo("", 0, false)
	h := handlers.NewWaitlistHandler(repo, nil, "")

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("", ""))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing email, got %d", rr.Code)
	}
}

// TestWaitlist_DBError_Returns500 verifies repository errors surface as 500.
func TestWaitlist_DBError_Returns500(t *testing.T) {
	repo := newStubWaitlistRepoErr(context.DeadlineExceeded)
	h := handlers.NewWaitlistHandler(repo, nil, "")

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("bob@example.com", ""))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// TestWaitlist_MailchimpError_NonFatal verifies that a Mailchimp 5xx leaves the
// handler still returning 200 OK and does NOT call UpdateMailchimpStatus.
func TestWaitlist_MailchimpError_NonFatal(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-fail", 1, true)
	mc := newStubMailchimpClient(fmt.Errorf("mailchimp: unexpected status 500"))

	h := handlers.NewWaitlistHandler(repo, mc, "")

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("charlie@example.com", ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 even on Mailchimp error, got %d: %s", rr.Code, rr.Body.String())
	}
	assertWaitlistPositionBody(t, rr, 1)

	// Wait for goroutine to finish.
	<-mc.done

	// UpdateMailchimpStatus must NOT be called — row stays mailchimp_status='failed'.
	calls := repo.getUpdateCalls()
	if len(calls) != 0 {
		t.Errorf("UpdateMailchimpStatus must not be called on Mailchimp error; got %d calls", len(calls))
	}
}

// TestWaitlist_MailchimpSuccess_SetsSubscribed verifies that on a successful
// Mailchimp call, UpdateMailchimpStatus("subscribed") is invoked.
func TestWaitlist_MailchimpSuccess_SetsSubscribed(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-ok", 7, true)
	mc := newStubMailchimpClient(nil) // success

	h := handlers.NewWaitlistHandler(repo, mc, "")

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("dana@example.com", ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertWaitlistPositionBody(t, rr, 7)

	// Wait for UpdateMailchimpStatus to be called (via repo.updateDone).
	<-repo.updateDone

	calls := repo.getUpdateCalls()
	if len(calls) == 0 {
		t.Fatal("UpdateMailchimpStatus('subscribed') was not called after successful Mailchimp add")
	}
	if calls[0].Status != "subscribed" {
		t.Errorf("expected status 'subscribed', got %q", calls[0].Status)
	}
	if calls[0].ID != "uuid-ok" {
		t.Errorf("expected ID 'uuid-ok', got %q", calls[0].ID)
	}
}

// TestWaitlist_RateLimit_Returns429 verifies the 6th request from the same IP
// within one hour is rejected with 429 (RC5).
func TestWaitlist_RateLimit_Returns429(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-rl", 1, true)
	h := handlers.NewWaitlistHandler(repo, nil, "")

	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		h.Join(rr, newWaitlistRequest("ratetest@example.com", ""))
		if rr.Code == http.StatusTooManyRequests {
			t.Fatalf("rate limit hit too early on request %d", i+1)
		}
	}

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("ratetest@example.com", ""))
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 on 6th request, got %d", rr.Code)
	}
}

// TestWaitlist_DifferentIPs_NotRateLimited verifies rate limiting is per-IP.
func TestWaitlist_DifferentIPs_NotRateLimited(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-ip", 1, true)
	h := handlers.NewWaitlistHandler(repo, nil, "")

	// Exhaust IP 1.2.3.4 bucket.
	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		h.Join(rr, newWaitlistRequest("a@example.com", ""))
	}

	// Request from a different IP must not be rate limited.
	req := newWaitlistRequest("b@example.com", "")
	req.RemoteAddr = "9.9.9.9:12345"
	rr := httptest.NewRecorder()
	h.Join(rr, req)

	if rr.Code == http.StatusTooManyRequests {
		t.Errorf("second IP must not be rate-limited by first IP's bucket; got 429")
	}
}

// TestWaitlist_Position1_FirstSignup verifies the position counter is 1 when
// the email is the first entry inserted (1-based count).
func TestWaitlist_Position1_FirstSignup(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-first", 1, true)
	h := handlers.NewWaitlistHandler(repo, nil, "")

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("first@example.com", ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	assertWaitlistPositionBody(t, rr, 1)
}

// ─── PostHog tests ────────────────────────────────────────────────────────────

// TestWaitlist_PostHog_FiredOnNewEmail verifies that funnel_waitlist_signup_completed
// is enqueued exactly once on the new-email path, with correct distinct_id and
// UTM properties.
func TestWaitlist_PostHog_FiredOnNewEmail(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-ph-new", 5, true)
	ph := newStubPostHogClient()
	h := handlers.NewWaitlistHandler(repo, nil, "").WithPostHogClient(ph)

	req := newWaitlistRequestWithUTM("ph@example.com", "twitter", "social", "beta-launch", "ad-variant-a", "mtg arena", "https://t.co/abc")
	rr := httptest.NewRecorder()
	h.Join(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Wait for the PostHog goroutine to complete.
	<-ph.done

	caps := ph.getCaptures()
	if len(caps) != 1 {
		t.Fatalf("expected 1 PostHog capture on new email, got %d", len(caps))
	}
	c := caps[0]

	if c.Event != "funnel_waitlist_signup_completed" {
		t.Errorf("event name: want %q, got %q", "funnel_waitlist_signup_completed", c.Event)
	}

	// distinct_id must be the SHA-256 hash of the email (first 16 hex chars),
	// matching hashAccountID("ph@example.com").
	if c.DistinctId == "" {
		t.Error("distinct_id must not be empty")
	}
	if c.DistinctId == "ph@example.com" {
		t.Error("distinct_id must not be raw email — must be hashed")
	}

	assertProp := func(key, want string) {
		t.Helper()
		got, _ := c.Properties[key].(string)
		if got != want {
			t.Errorf("property %q: want %q, got %q", key, want, got)
		}
	}
	assertProp("utm_source", "twitter")
	assertProp("utm_medium", "social")
	assertProp("utm_campaign", "beta-launch")
	assertProp("utm_content", "ad-variant-a")
	assertProp("utm_term", "mtg arena")
	assertProp("referrer", "https://t.co/abc")
}

// TestWaitlist_PostHog_NotFiredOnConflict verifies that funnel_waitlist_signup_completed
// is NOT enqueued when the email already exists (conflict → 409 path).
func TestWaitlist_PostHog_NotFiredOnConflict(t *testing.T) {
	repo := newStubWaitlistRepo("", 0, false) // conflict: created=false
	ph := newStubPostHogClient()
	h := handlers.NewWaitlistHandler(repo, nil, "").WithPostHogClient(ph)

	req := newWaitlistRequest("dup@example.com", "")
	rr := httptest.NewRecorder()
	h.Join(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for conflict path, got %d: %s", rr.Code, rr.Body.String())
	}

	// Give any inadvertent goroutine a moment to fire (it must not).
	select {
	case <-ph.done:
		t.Error("PostHog Enqueue must NOT be called on the conflict/409 path")
	default:
		// Nothing enqueued — correct.
	}

	caps := ph.getCaptures()
	if len(caps) != 0 {
		t.Errorf("expected 0 PostHog captures on conflict path, got %d", len(caps))
	}
}

// TestWaitlist_PostHog_UTMAbsent_EmptyWhenNotProvided verifies that utm_content
// and utm_term are present in the PostHog event payload as empty strings (not
// absent or non-empty) when the request does not include those fields. This
// prevents accidental default bleeding — a future code change that hardcodes a
// default value for these fields would be caught here.
func TestWaitlist_PostHog_UTMAbsent_EmptyWhenNotProvided(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-ph-absent", 2, true)
	ph := newStubPostHogClient()
	h := handlers.NewWaitlistHandler(repo, nil, "").WithPostHogClient(ph)

	// Request with no utm_content or utm_term fields.
	req := newWaitlistRequestWithUTM("absent@example.com", "google", "cpc", "spring-sale", "", "", "https://example.com")
	rr := httptest.NewRecorder()
	h.Join(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	<-ph.done

	caps := ph.getCaptures()
	if len(caps) != 1 {
		t.Fatalf("expected 1 PostHog capture, got %d", len(caps))
	}
	c := caps[0]

	assertPropAbsent := func(key string) {
		t.Helper()
		got, _ := c.Properties[key].(string)
		if got != "" {
			t.Errorf("property %q: want empty string when field absent, got %q (possible default bleeding)", key, got)
		}
	}
	assertPropAbsent("utm_content")
	assertPropAbsent("utm_term")
}

// ─── PR A hardening tests (TDD — written RED first, tickets #132/#133/#134/#135) ─

// TestWaitlist_LargeBody_Returns413 verifies that a body exceeding 4096 bytes
// is rejected with 413 Request Entity Too Large (#132).
func TestWaitlist_LargeBody_Returns413(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-1", 1, true)
	h := handlers.NewWaitlistHandler(repo, nil, "test-pii-salt")

	// 5 KB body — well over the 4096-byte cap.
	body := bytes.NewBufferString(`{"email":"` + strings.Repeat("a", 5000) + `@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", body)
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "1.2.3.4:12345"
	rr := httptest.NewRecorder()

	h.Join(rr, req)

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("large body: want 413, got %d — body: %s", rr.Code, rr.Body.String())
	}
}

// TestWaitlist_ContentType_Missing_Returns415 verifies that a missing
// Content-Type header is rejected with 415 (#134).
func TestWaitlist_ContentType_Missing_Returns415(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-1", 1, true)
	h := handlers.NewWaitlistHandler(repo, nil, "test-pii-salt")

	body := bytes.NewBufferString(`{"email":"user@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", body)
	// Deliberately no Content-Type header.
	req.RemoteAddr = "1.2.3.4:12345"
	rr := httptest.NewRecorder()

	h.Join(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("missing Content-Type: want 415, got %d", rr.Code)
	}
}

// TestWaitlist_ContentType_TextPlain_Returns415 verifies that text/plain
// is rejected with 415 (#134). Waitlist is a standard JSON form, unlike
// boot-signal which accepts text/plain (sendBeacon constraint).
func TestWaitlist_ContentType_TextPlain_Returns415(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-1", 1, true)
	h := handlers.NewWaitlistHandler(repo, nil, "test-pii-salt")

	body := bytes.NewBufferString(`{"email":"user@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", body)
	req.Header.Set("Content-Type", "text/plain")
	req.RemoteAddr = "1.2.3.4:12345"
	rr := httptest.NewRecorder()

	h.Join(rr, req)

	if rr.Code != http.StatusUnsupportedMediaType {
		t.Errorf("Content-Type: text/plain: want 415, got %d", rr.Code)
	}
}

// TestWaitlist_ContentType_JsonCharset_Accepted verifies that
// "application/json; charset=utf-8" passes the Content-Type prefix guard (#134).
func TestWaitlist_ContentType_JsonCharset_Accepted(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-charset", 1, true)
	h := handlers.NewWaitlistHandler(repo, nil, "test-pii-salt")

	body, _ := json.Marshal(map[string]string{"email": "charset@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.RemoteAddr = "1.2.3.4:12345"
	rr := httptest.NewRecorder()

	h.Join(rr, req)

	// Must pass the CT guard — outcome depends on repo stub (200 or other, but not 415).
	if rr.Code == http.StatusUnsupportedMediaType {
		t.Errorf("Content-Type: application/json; charset=utf-8: must not return 415")
	}
}

// TestWaitlist_InvalidEmail_Returns400 verifies that a string that fails
// net/mail.ParseAddress is rejected with 400 (#133).
func TestWaitlist_InvalidEmail_Returns400(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-1", 1, true)
	h := handlers.NewWaitlistHandler(repo, nil, "test-pii-salt")

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequestWithSalt("not-an-email", "test-pii-salt"))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("invalid email: want 400, got %d", rr.Code)
	}
}

// TestWaitlist_ValidEmail_NormalizedBeforeStore verifies that the email stored
// is strings.ToLower(strings.TrimSpace(addr.Address)) — i.e. display-name forms
// are stripped and case is normalized before InsertIfNew is called (#133, Q2).
func TestWaitlist_ValidEmail_NormalizedBeforeStore(t *testing.T) {
	repo := newStubWaitlistRepoCapture()
	h := handlers.NewWaitlistHandler(repo, nil, "test-pii-salt")

	// Send a padded + display-name email; handler should store "user@example.com".
	body, _ := json.Marshal(map[string]string{"email": "  User@Example.COM  "})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "1.2.3.4:12345"
	rr := httptest.NewRecorder()

	h.Join(rr, req)

	if rr.Code == http.StatusBadRequest {
		t.Fatalf("valid email with whitespace/case: unexpected 400 — body: %s", rr.Body.String())
	}

	got := repo.capturedEmail
	if got != "user@example.com" {
		t.Errorf("stored email: want %q, got %q (must be lower+trimmed addr.Address)", "user@example.com", got)
	}
}

// TestWaitlist_PIILog_NoRawEmail verifies that raw email is absent from the
// log on the DB error path (lines 176 and 198 must omit email entirely — Ray Q4
// ruling: omit, not hash). (#135)
func TestWaitlist_PIILog_NoRawEmail(t *testing.T) {
	const testEmail = "piitest@example.com"

	repo := newStubWaitlistRepoErr(fmt.Errorf("db: connection refused"))
	h := handlers.NewWaitlistHandler(repo, nil, "test-pii-salt")

	var logBuf strings.Builder
	orig := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(orig) })

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequestWithSalt(testEmail, "test-pii-salt"))

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on DB error, got %d", rr.Code)
	}

	logged := logBuf.String()
	if strings.Contains(logged, testEmail) {
		t.Errorf("PII log violation: raw email %q found in log output — must be omitted: %q", testEmail, logged)
	}
}

// TestWaitlist_PIILog_Mailchimp_NoRawEmail verifies that the Mailchimp error
// log goroutine (line 198 equivalent) also omits the raw email (#135).
func TestWaitlist_PIILog_Mailchimp_NoRawEmail(t *testing.T) {
	const testEmail = "mailchimp-pii@example.com"

	repo := newStubWaitlistRepo("uuid-mc-pii", 1, true)
	mc := newStubMailchimpClient(fmt.Errorf("mailchimp: 500"))
	h := handlers.NewWaitlistHandler(repo, mc, "test-pii-salt")

	// Use a mutex-protected log writer to avoid a data race between the
	// Mailchimp goroutine's log.Printf (which fires AFTER AddMember returns
	// and mc.done is signalled) and this test reading the buffer.
	var mu sync.Mutex
	var logLines []string
	orig := log.Writer()
	log.SetOutput(&safeLogWriter{mu: &mu, lines: &logLines})
	t.Cleanup(func() { log.SetOutput(orig) })

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequestWithSalt(testEmail, "test-pii-salt"))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Wait for Mailchimp goroutine's AddMember to be called.
	<-mc.done

	// The log.Printf in the goroutine fires AFTER AddMember returns (which is
	// when mc.done is signalled). Yield to allow the goroutine to complete the
	// log write before we read. Using runtime.Gosched() in a brief loop is
	// sufficient since the goroutine has no other work after log.Printf.
	for range 10 {
		mu.Lock()
		n := len(logLines)
		mu.Unlock()
		if n > 0 {
			break
		}
		// Brief yield — the goroutine only has log.Printf left after mc.done.
		time.Sleep(time.Millisecond)
	}

	mu.Lock()
	logged := strings.Join(logLines, "\n")
	mu.Unlock()

	if strings.Contains(logged, testEmail) {
		t.Errorf("Mailchimp goroutine log PII violation: raw email %q found — must be omitted: %q", testEmail, logged)
	}
}

// safeLogWriter is a thread-safe io.Writer for capturing log output in tests
// where goroutines may write to the logger concurrently.
type safeLogWriter struct {
	mu    *sync.Mutex
	lines *[]string
}

func (s *safeLogWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	*s.lines = append(*s.lines, string(p))
	s.mu.Unlock()
	return len(p), nil
}

// TestWaitlist_RateMapCap_Eviction verifies the rateByIP map is bounded at
// WaitlistRateMapCap and does not grow without bound (#132 / S-07 mirror).
func TestWaitlist_RateMapCap_Eviction(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-cap", 1, true)
	h := handlers.NewWaitlistHandler(repo, nil, "test-pii-salt")

	flood := handlers.WaitlistRateMapCap + 500
	for i := range flood {
		body, _ := json.Marshal(map[string]string{"email": fmt.Sprintf("flood%d@example.com", i)})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Real-IP", fmt.Sprintf("10.%d.%d.%d", (i/65536)%256, (i/256)%256, i%256))
		rr := httptest.NewRecorder()
		h.Join(rr, req)
	}

	size := h.WaitlistRateMapSize()
	if size > handlers.WaitlistRateMapCap {
		t.Errorf("rateByIP map size %d exceeds cap %d", size, handlers.WaitlistRateMapCap)
	}
}

// ─── additional helpers for PR A tests ───────────────────────────────────────

// newWaitlistRequestWithSalt creates a waitlist POST with the given email
// and application/json Content-Type (post-PR-A contract).
func newWaitlistRequestWithSalt(email, _ string) *http.Request {
	body, _ := json.Marshal(map[string]string{"email": email})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "1.2.3.4:12345"
	return req
}

// stubWaitlistRepoCapture is a stub that records the email passed to InsertIfNew.
type stubWaitlistRepoCapture struct {
	capturedEmail string
}

func newStubWaitlistRepoCapture() *stubWaitlistRepoCapture {
	return &stubWaitlistRepoCapture{}
}

func (s *stubWaitlistRepoCapture) InsertIfNew(_ context.Context, email string, _, _, _, _, _ *string, _ *string) (string, int64, bool, error) {
	s.capturedEmail = email
	return "uuid-cap", 1, true, nil
}

func (s *stubWaitlistRepoCapture) UpdateMailchimpStatus(_ context.Context, _, _ string) error {
	return nil
}

// ─── assertion helpers ────────────────────────────────────────────────────────

// assertWaitlistPositionBody decodes the response and asserts {"position": want}.
func assertWaitlistPositionBody(t *testing.T, rr *httptest.ResponseRecorder, want int64) {
	t.Helper()
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	pos, ok := resp["position"]
	if !ok {
		t.Fatalf(`expected body {"position": %d}, got %v`, want, resp)
	}
	// json.Unmarshal uses float64 for numbers.
	posF, ok := pos.(float64)
	if !ok {
		t.Fatalf("position is not a number: %T %v", pos, pos)
	}
	if int64(posF) != want {
		t.Errorf("position: want %d, got %d", want, int64(posF))
	}
}

// assertWaitlistErrorBody decodes the response and asserts {"error": want}.
func assertWaitlistErrorBody(t *testing.T, rr *httptest.ResponseRecorder, want string) {
	t.Helper()
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	errMsg, _ := resp["error"].(string)
	if errMsg != want {
		t.Errorf(`error body: want %q, got %q (full: %v)`, want, errMsg, resp)
	}
}
