package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/RdHamilton/vault-mtg/services/bff/internal/api/handlers"
)

// ─── stub repo ────────────────────────────────────────────────────────────────

type waitlistUpdateCall struct {
	ID     string
	Status string
}

type stubWaitlistRepo struct {
	insertID      string
	insertCreated bool
	insertErr     error
	updateErr     error

	mu          sync.Mutex
	updateCalls []waitlistUpdateCall
	updateDone  chan struct{}
}

func newStubWaitlistRepo(id string, created bool) *stubWaitlistRepo {
	return &stubWaitlistRepo{
		insertID:      id,
		insertCreated: created,
		updateDone:    make(chan struct{}, 1),
	}
}

func newStubWaitlistRepoErr(err error) *stubWaitlistRepo {
	return &stubWaitlistRepo{
		insertErr:  err,
		updateDone: make(chan struct{}, 1),
	}
}

func (s *stubWaitlistRepo) InsertIfNew(_ context.Context, _ string, _ *string) (string, bool, error) {
	return s.insertID, s.insertCreated, s.insertErr
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

// ─── request helper ───────────────────────────────────────────────────────────

func newWaitlistRequest(email, referrer string) *http.Request {
	body := map[string]string{"email": email, "referrer": referrer}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "1.2.3.4:12345"
	return req
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestWaitlist_NewEmail_Returns201 verifies that a brand-new email returns 201.
func TestWaitlist_NewEmail_Returns201(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-1", true)
	h := handlers.NewWaitlistHandler(repo, nil)

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("alice@example.com", ""))

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	assertWaitlistOKBody(t, rr)
}

// TestWaitlist_ExistingEmail_Returns200 verifies idempotency: duplicate email → 200.
func TestWaitlist_ExistingEmail_Returns200(t *testing.T) {
	repo := newStubWaitlistRepo("", false)
	h := handlers.NewWaitlistHandler(repo, nil)

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("alice@example.com", ""))

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for duplicate email, got %d: %s", rr.Code, rr.Body.String())
	}
	assertWaitlistOKBody(t, rr)
}

// TestWaitlist_MissingEmail_Returns400 verifies empty email is rejected.
func TestWaitlist_MissingEmail_Returns400(t *testing.T) {
	repo := newStubWaitlistRepo("", false)
	h := handlers.NewWaitlistHandler(repo, nil)

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("", ""))

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing email, got %d", rr.Code)
	}
}

// TestWaitlist_DBError_Returns500 verifies repository errors surface as 500.
func TestWaitlist_DBError_Returns500(t *testing.T) {
	repo := newStubWaitlistRepoErr(context.DeadlineExceeded)
	h := handlers.NewWaitlistHandler(repo, nil)

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("bob@example.com", ""))

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// TestWaitlist_MailchimpError_NonFatal verifies that a Mailchimp 5xx leaves the
// handler still returning 201 and does NOT call UpdateMailchimpStatus.
func TestWaitlist_MailchimpError_NonFatal(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-fail", true)
	mc := newStubMailchimpClient(fmt.Errorf("mailchimp: unexpected status 500"))

	h := handlers.NewWaitlistHandler(repo, mc)

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("charlie@example.com", ""))

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 even on Mailchimp error, got %d: %s", rr.Code, rr.Body.String())
	}
	assertWaitlistOKBody(t, rr)

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
	repo := newStubWaitlistRepo("uuid-ok", true)
	mc := newStubMailchimpClient(nil) // success

	h := handlers.NewWaitlistHandler(repo, mc)

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("dana@example.com", ""))

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

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
	repo := newStubWaitlistRepo("uuid-rl", true)
	h := handlers.NewWaitlistHandler(repo, nil)

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
	repo := newStubWaitlistRepo("uuid-ip", true)
	h := handlers.NewWaitlistHandler(repo, nil)

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

// TestWaitlist_ResponseBody_OK asserts the response body shape is {"ok":true}.
func TestWaitlist_ResponseBody_OK(t *testing.T) {
	repo := newStubWaitlistRepo("uuid-body", true)
	h := handlers.NewWaitlistHandler(repo, nil)

	rr := httptest.NewRecorder()
	h.Join(rr, newWaitlistRequest("eve@example.com", ""))

	assertWaitlistOKBody(t, rr)
}

// ─── assertion helpers ────────────────────────────────────────────────────────

func assertWaitlistOKBody(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	ok, _ := resp["ok"].(bool)
	if !ok {
		t.Errorf(`expected body {"ok":true}, got %v`, resp)
	}
}
