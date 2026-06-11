package reconciler_test

// mailchimp_reconciler_test.go — unit tests for the Mailchimp waitlist reconciler.
// Ticket: hollowmark-tickets#126
//
// Tests use injected stubs for the mailchimp client and waitlist store — no real
// DB or Mailchimp API is required. Integration tests in storage/repository cover
// the DB layer.
//
// Test coverage:
//   1. RunOnce on empty batch → no calls, no error
//   2. RunOnce with one 'failed' entry → calls AddMember, then MarkWaitlistSubscribed
//   3. RunOnce with Mailchimp failure → calls IncrementAttemptsAndMaybeTerminate
//   4. RunOnce with multiple entries → processes all independently
//   5. RunOnce: AddMember success, MarkWaitlistSubscribed DB error → logs only, no panic
//   6. RunOnce: ListFailedWaitlistEntries error → returns early, no AddMember call
//   7. Run loop: Run exits when context is cancelled
//   8. Metrics: batch stats (subscribed/failed/terminated) are aggregated correctly

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/reconciler"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// --- stubs ---

type stubWaitlistStore struct {
	listFailed   func(ctx context.Context, limit int) ([]repository.FailedWaitlistEntry, error)
	markSubbed   func(ctx context.Context, id string) error
	incrAttempts func(ctx context.Context, id string, maxAttempts int) error
}

func (s *stubWaitlistStore) ListFailedWaitlistEntries(ctx context.Context, limit int) ([]repository.FailedWaitlistEntry, error) {
	return s.listFailed(ctx, limit)
}

func (s *stubWaitlistStore) MarkWaitlistSubscribed(ctx context.Context, id string) error {
	return s.markSubbed(ctx, id)
}

func (s *stubWaitlistStore) IncrementAttemptsAndMaybeTerminate(ctx context.Context, id string, maxAttempts int) error {
	return s.incrAttempts(ctx, id, maxAttempts)
}

type stubMailchimp struct {
	addMember func(ctx context.Context, email string) error
}

func (s *stubMailchimp) AddMember(ctx context.Context, email string) error {
	return s.addMember(ctx, email)
}

// --- tests ---

// TestReconciler_RunOnce_EmptyBatch verifies RunOnce is a no-op when there are
// no failed entries.
func TestReconciler_RunOnce_EmptyBatch(t *testing.T) {
	var addCalls int64

	store := &stubWaitlistStore{
		listFailed: func(_ context.Context, _ int) ([]repository.FailedWaitlistEntry, error) {
			return nil, nil
		},
		markSubbed:   func(_ context.Context, _ string) error { return nil },
		incrAttempts: func(_ context.Context, _ string, _ int) error { return nil },
	}
	mc := &stubMailchimp{
		addMember: func(_ context.Context, _ string) error {
			atomic.AddInt64(&addCalls, 1)
			return nil
		},
	}

	r := reconciler.NewMailchimpReconciler(store, mc)
	r.RunOnce(context.Background())

	if addCalls != 0 {
		t.Errorf("AddMember called %d times on empty batch, want 0", addCalls)
	}
}

// TestReconciler_RunOnce_SuccessPath verifies that a 'failed' entry that
// succeeds at AddMember has MarkWaitlistSubscribed called.
func TestReconciler_RunOnce_SuccessPath(t *testing.T) {
	const testID = "row-id-success"
	const testEmail = "success@example.com"

	var addCalled, markCalled int64

	store := &stubWaitlistStore{
		listFailed: func(_ context.Context, _ int) ([]repository.FailedWaitlistEntry, error) {
			return []repository.FailedWaitlistEntry{{ID: testID, Email: testEmail}}, nil
		},
		markSubbed: func(_ context.Context, id string) error {
			if id != testID {
				t.Errorf("MarkWaitlistSubscribed: got id %q, want %q", id, testID)
			}
			atomic.AddInt64(&markCalled, 1)
			return nil
		},
		incrAttempts: func(_ context.Context, _ string, _ int) error {
			t.Error("IncrementAttemptsAndMaybeTerminate must not be called on success")
			return nil
		},
	}
	mc := &stubMailchimp{
		addMember: func(_ context.Context, email string) error {
			if email != testEmail {
				t.Errorf("AddMember: got email %q, want %q", email, testEmail)
			}
			atomic.AddInt64(&addCalled, 1)
			return nil
		},
	}

	r := reconciler.NewMailchimpReconciler(store, mc)
	r.RunOnce(context.Background())

	if addCalled != 1 {
		t.Errorf("AddMember: want 1 call, got %d", addCalled)
	}
	if markCalled != 1 {
		t.Errorf("MarkWaitlistSubscribed: want 1 call, got %d", markCalled)
	}
}

// TestReconciler_RunOnce_MailchimpFailure verifies that when AddMember fails,
// IncrementAttemptsAndMaybeTerminate is called instead of MarkWaitlistSubscribed.
func TestReconciler_RunOnce_MailchimpFailure(t *testing.T) {
	const testID = "row-id-fail"
	const testEmail = "fail@example.com"

	var incrCalled int64

	store := &stubWaitlistStore{
		listFailed: func(_ context.Context, _ int) ([]repository.FailedWaitlistEntry, error) {
			return []repository.FailedWaitlistEntry{{ID: testID, Email: testEmail}}, nil
		},
		markSubbed: func(_ context.Context, _ string) error {
			t.Error("MarkWaitlistSubscribed must not be called on Mailchimp failure")
			return nil
		},
		incrAttempts: func(_ context.Context, id string, _ int) error {
			if id != testID {
				t.Errorf("IncrementAttemptsAndMaybeTerminate: got id %q, want %q", id, testID)
			}
			atomic.AddInt64(&incrCalled, 1)
			return nil
		},
	}
	mc := &stubMailchimp{
		addMember: func(_ context.Context, _ string) error {
			return errors.New("mailchimp: 500 internal server error")
		},
	}

	r := reconciler.NewMailchimpReconciler(store, mc)
	r.RunOnce(context.Background())

	if incrCalled != 1 {
		t.Errorf("IncrementAttemptsAndMaybeTerminate: want 1 call, got %d", incrCalled)
	}
}

// TestReconciler_RunOnce_MultipleEntries verifies that all entries in a batch
// are processed independently.
func TestReconciler_RunOnce_MultipleEntries(t *testing.T) {
	entries := []repository.FailedWaitlistEntry{
		{ID: "id-1", Email: "e1@example.com"},
		{ID: "id-2", Email: "e2@example.com"},
		{ID: "id-3", Email: "e3@example.com"},
	}

	var addCalls, markCalls int64

	store := &stubWaitlistStore{
		listFailed: func(_ context.Context, _ int) ([]repository.FailedWaitlistEntry, error) {
			return entries, nil
		},
		markSubbed: func(_ context.Context, _ string) error {
			atomic.AddInt64(&markCalls, 1)
			return nil
		},
		incrAttempts: func(_ context.Context, _ string, _ int) error {
			t.Error("IncrementAttemptsAndMaybeTerminate must not be called — all succeed")
			return nil
		},
	}
	mc := &stubMailchimp{
		addMember: func(_ context.Context, _ string) error {
			atomic.AddInt64(&addCalls, 1)
			return nil
		},
	}

	r := reconciler.NewMailchimpReconciler(store, mc)
	r.RunOnce(context.Background())

	if addCalls != int64(len(entries)) {
		t.Errorf("AddMember: want %d calls, got %d", len(entries), addCalls)
	}
	if markCalls != int64(len(entries)) {
		t.Errorf("MarkWaitlistSubscribed: want %d calls, got %d", len(entries), markCalls)
	}
}

// TestReconciler_RunOnce_MarkSubscribedDBError verifies that a DB error from
// MarkWaitlistSubscribed after a successful AddMember logs but does not panic
// or propagate the error (best-effort post-success write).
func TestReconciler_RunOnce_MarkSubscribedDBError(t *testing.T) {
	const testID = "row-id-db-err"
	const testEmail = "db-err@example.com"

	store := &stubWaitlistStore{
		listFailed: func(_ context.Context, _ int) ([]repository.FailedWaitlistEntry, error) {
			return []repository.FailedWaitlistEntry{{ID: testID, Email: testEmail}}, nil
		},
		markSubbed: func(_ context.Context, _ string) error {
			return errors.New("db: connection reset")
		},
		incrAttempts: func(_ context.Context, _ string, _ int) error { return nil },
	}
	mc := &stubMailchimp{
		addMember: func(_ context.Context, _ string) error { return nil },
	}

	r := reconciler.NewMailchimpReconciler(store, mc)
	// Must not panic.
	r.RunOnce(context.Background())
}

// TestReconciler_RunOnce_ListError verifies that a DB error from
// ListFailedWaitlistEntries causes RunOnce to return early without calling
// AddMember.
func TestReconciler_RunOnce_ListError(t *testing.T) {
	var addCalls int64

	store := &stubWaitlistStore{
		listFailed: func(_ context.Context, _ int) ([]repository.FailedWaitlistEntry, error) {
			return nil, errors.New("db: timeout")
		},
		markSubbed:   func(_ context.Context, _ string) error { return nil },
		incrAttempts: func(_ context.Context, _ string, _ int) error { return nil },
	}
	mc := &stubMailchimp{
		addMember: func(_ context.Context, _ string) error {
			atomic.AddInt64(&addCalls, 1)
			return nil
		},
	}

	r := reconciler.NewMailchimpReconciler(store, mc)
	r.RunOnce(context.Background())

	if addCalls != 0 {
		t.Errorf("AddMember must not be called when list fails; got %d calls", addCalls)
	}
}

// TestReconciler_Run_StopsOnContextCancel verifies that the Run loop exits
// cleanly when the context is cancelled.
func TestReconciler_Run_StopsOnContextCancel(t *testing.T) {
	store := &stubWaitlistStore{
		listFailed:   func(_ context.Context, _ int) ([]repository.FailedWaitlistEntry, error) { return nil, nil },
		markSubbed:   func(_ context.Context, _ string) error { return nil },
		incrAttempts: func(_ context.Context, _ string, _ int) error { return nil },
	}
	mc := &stubMailchimp{
		addMember: func(_ context.Context, _ string) error { return nil },
	}

	r := reconciler.NewMailchimpReconciler(store, mc)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// Pass: Run exited after cancel.
	case <-time.After(3 * time.Second):
		t.Error("Run did not exit within 3s of context cancellation")
	}
}

// TestReconciler_RunOnce_BatchStats verifies that RunOnce returns correct
// batch statistics: subscribed, failed, and total counts.
func TestReconciler_RunOnce_BatchStats(t *testing.T) {
	// 2 succeed, 1 fails Mailchimp.
	entries := []repository.FailedWaitlistEntry{
		{ID: "stat-1", Email: "s1@example.com"},
		{ID: "stat-2", Email: "s2@example.com"},
		{ID: "stat-3", Email: "s3@example.com"},
	}

	store := &stubWaitlistStore{
		listFailed: func(_ context.Context, _ int) ([]repository.FailedWaitlistEntry, error) {
			return entries, nil
		},
		markSubbed:   func(_ context.Context, _ string) error { return nil },
		incrAttempts: func(_ context.Context, _ string, _ int) error { return nil },
	}

	callCount := int64(0)
	mc := &stubMailchimp{
		addMember: func(_ context.Context, _ string) error {
			n := atomic.AddInt64(&callCount, 1)
			// Third call fails.
			if n == 3 {
				return errors.New("mailchimp: rate limited")
			}
			return nil
		},
	}

	r := reconciler.NewMailchimpReconciler(store, mc)
	stats := r.RunOnce(context.Background())

	if stats.Total != 3 {
		t.Errorf("stats.Total: want 3, got %d", stats.Total)
	}
	if stats.Subscribed != 2 {
		t.Errorf("stats.Subscribed: want 2, got %d", stats.Subscribed)
	}
	if stats.Failed != 1 {
		t.Errorf("stats.Failed: want 1, got %d", stats.Failed)
	}
}
