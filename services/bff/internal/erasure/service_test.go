package erasure_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/erasure"
)

// ---------------------------------------------------------------------------
// stubAuditDB — implements JobAuditLogger for service tests
// ---------------------------------------------------------------------------

// stubAuditDB satisfies the full JobAuditLogger (DBOps + CreateAuditLogEntry)
// interface and can simulate the "already active" idempotency-guard path.
type stubAuditDB struct {
	// Embed stubDB so we satisfy the DBOps interface.
	*stubDB

	mu           sync.Mutex
	createCalls  int
	returnActive bool // when true, simulate an in-flight conflict
}

func newStubAuditDB() *stubAuditDB {
	return &stubAuditDB{stubDB: newStubDB()}
}

func (s *stubAuditDB) CreateAuditLogEntry(_ context.Context, _ string, _, _ int64) (jobID string, alreadyActive bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls++
	if s.returnActive {
		return "job-existing-abc", true, nil
	}
	return "job-new-abc", false, nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestService_StartErasureJob_DispatchesGoroutineOnNewJob verifies that when
// CreateAuditLogEntry returns alreadyActive=false, a cascade goroutine IS
// dispatched (wg drains after the call).
func TestService_StartErasureJob_DispatchesGoroutineOnNewJob(t *testing.T) {
	db := newStubAuditDB()
	var wg sync.WaitGroup
	svc := erasure.NewService(context.Background(), db, erasure.Deps{
		DB:        db.stubDB,
		PostHog:   &stubPostHog{},
		Clerk:     &stubClerk{},
		Mailchimp: &stubMailchimp{},
	}, &wg)

	erasure.SetClerkUserIDFromContextFn(func(_ context.Context) (string, bool) {
		return "user_clerk_test", true
	})

	jobID, err := svc.StartErasureJob(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("StartErasureJob: %v", err)
	}
	if jobID != "job-new-abc" {
		t.Errorf("jobID: got %q, want %q", jobID, "job-new-abc")
	}

	// The dispatched goroutine should complete; wg.Wait() must not block.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("wg.Wait timed out — cascade goroutine did not complete")
	}

	db.mu.Lock()
	calls := db.createCalls
	db.mu.Unlock()
	if calls != 1 {
		t.Errorf("CreateAuditLogEntry calls: got %d, want 1", calls)
	}
}

// TestService_StartErasureJob_ReturnsExistingJobOnConcurrentCall is the
// idempotency regression test.  When CreateAuditLogEntry returns
// alreadyActive=true, StartErasureJob MUST return the existing job_id and
// MUST NOT dispatch a second goroutine (wg stays at zero).
func TestService_StartErasureJob_ReturnsExistingJobOnConcurrentCall(t *testing.T) {
	db := newStubAuditDB()
	db.returnActive = true // simulate an in-flight conflict

	var wg sync.WaitGroup
	svc := erasure.NewService(context.Background(), db, erasure.Deps{
		DB:        db.stubDB,
		PostHog:   &stubPostHog{},
		Clerk:     &stubClerk{},
		Mailchimp: &stubMailchimp{},
	}, &wg)

	erasure.SetClerkUserIDFromContextFn(func(_ context.Context) (string, bool) {
		return "user_clerk_test", true
	})

	jobID, err := svc.StartErasureJob(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("StartErasureJob: %v", err)
	}
	if jobID != "job-existing-abc" {
		t.Errorf("jobID: got %q, want %q — expected existing job returned on concurrent call", jobID, "job-existing-abc")
	}

	// The WaitGroup must drain immediately — no goroutine should have been
	// dispatched for an already-active job.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
		// Pass — wg was already zero; no goroutine was dispatched.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("wg.Wait timed out — service dispatched a goroutine on an already-active job (idempotency violation)")
	}
}
