package erasure_test

// ---------------------------------------------------------------------------
// Email-notification tests (AC5/AC6/AC7 from hollowmark-tickets#1172)
//
// These tests verify the wiring between RunErasureCascade and the EmailSender:
//   - On CASCADE SUCCESS: SendDeletionComplete is called with the Step-0
//     captured email address (not looked up post-cascade).
//   - On CASCADE FAILURE: SendDeletionFailed is called with the Step-0
//     captured email address.
//   - An email-send failure does NOT block the cascade (fail-closed is not
//     broken — email send errors are logged/reported but do not re-trigger
//     or halt the erasure).  The email send happens AFTER the cascade
//     terminal state, so it can never leave completed_at NULL.
//
// "Fail-closed" in this context means: if the email cannot be sent, the
// erasure is still complete.  The Sentry alert (PR-A) remains the
// authoritative failure-visibility channel.
// ---------------------------------------------------------------------------

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/email"
	"github.com/RdHamilton/hollowmark/services/bff/internal/erasure"
)

// ---------------------------------------------------------------------------
// TestRunErasureCascade_SendsDeletionCompleteEmailOnSuccess
//
// Verifies AC7: on a clean cascade (all steps succeed), the EmailSender's
// SendDeletionComplete is called exactly once, with the Step-0-captured email
// address ("test@example.com" per stubDB.CapturePreJobData).
// ---------------------------------------------------------------------------
func TestRunErasureCascade_SendsDeletionCompleteEmailOnSuccess(t *testing.T) {
	db := newStubDB()
	sender := &email.MockSender{}

	deps := erasure.Deps{
		DB:        db,
		PostHog:   &stubPostHog{},
		Clerk:     &stubClerk{},
		Mailchimp: &stubMailchimp{},
		Email:     sender,
	}

	err := erasure.RunErasureCascade(context.Background(), "job-email-ok", "clerk_uid_email", int64(1), int64(1), deps)
	if err != nil {
		t.Fatalf("RunErasureCascade: unexpected error: %v", err)
	}

	if n := sender.DeletionCompleteCallCount(); n != 1 {
		t.Errorf("SendDeletionComplete call count: got %d, want 1", n)
	}
	if got := sender.LastDeletionCompleteAddr(); got != "test@example.com" {
		t.Errorf("SendDeletionComplete addr: got %q, want %q", got, "test@example.com")
	}

	// SendDeletionFailed must NOT be called on a clean cascade.
	if n := sender.DeletionFailedCallCount(); n != 0 {
		t.Errorf("SendDeletionFailed must not be called on success; got %d calls", n)
	}
}

// ---------------------------------------------------------------------------
// TestRunErasureCascade_SendsDeletionFailedEmailOnCascadeFailure
//
// Verifies AC6/AC7: when the cascade fails at any step, SendDeletionFailed is
// called with the Step-0-captured email.  Here we inject a failure at step4a
// (post-step0, so the email address has been captured).
// ---------------------------------------------------------------------------
func TestRunErasureCascade_SendsDeletionFailedEmailOnCascadeFailure(t *testing.T) {
	db := newStubDB()
	db.injectError("step4a", errors.New("db timeout"))
	sender := &email.MockSender{}

	deps := erasure.Deps{
		DB:        db,
		PostHog:   &stubPostHog{},
		Clerk:     &stubClerk{},
		Mailchimp: &stubMailchimp{},
		Email:     sender,
	}

	err := erasure.RunErasureCascade(context.Background(), "job-email-fail", "clerk_uid_ef", int64(2), int64(2), deps)
	if err == nil {
		t.Fatal("expected cascade to return an error on step4a failure")
	}

	if n := sender.DeletionFailedCallCount(); n != 1 {
		t.Errorf("SendDeletionFailed call count: got %d, want 1", n)
	}
	if got := sender.LastDeletionFailedAddr(); got != "test@example.com" {
		t.Errorf("SendDeletionFailed addr: got %q, want %q", got, "test@example.com")
	}

	// SendDeletionComplete must NOT be called on failure.
	if n := sender.DeletionCompleteCallCount(); n != 0 {
		t.Errorf("SendDeletionComplete must not be called on failure; got %d calls", n)
	}
}

// ---------------------------------------------------------------------------
// TestRunErasureCascade_Step0FailureSkipsEmailSend
//
// Verifies that when Step 0 (CapturePreJobData) fails — meaning the email
// address is never captured — neither email method is called.  We cannot send
// to an unknown address; the Sentry alert handles visibility.
// ---------------------------------------------------------------------------
func TestRunErasureCascade_Step0FailureSkipsEmailSend(t *testing.T) {
	db := newStubDB()
	db.injectError("step0", errors.New("db connection lost"))
	sender := &email.MockSender{}

	deps := erasure.Deps{
		DB:        db,
		PostHog:   &stubPostHog{},
		Clerk:     &stubClerk{},
		Mailchimp: &stubMailchimp{},
		Email:     sender,
	}

	err := erasure.RunErasureCascade(context.Background(), "job-email-step0", "clerk_uid_s0", int64(3), int64(3), deps)
	if err == nil {
		t.Fatal("expected cascade to return an error on step0 failure")
	}

	if n := sender.DeletionCompleteCallCount() + sender.DeletionFailedCallCount(); n != 0 {
		t.Errorf("no email must be sent when step0 fails (no address captured); got %d call(s)", n)
	}
}

// ---------------------------------------------------------------------------
// TestRunErasureCascade_EmailSendFailureDoesNotBlockCascade
//
// Verifies fail-closed contract: an error from SendDeletionComplete must NOT
// cause RunErasureCascade to return a non-nil error.  The cascade is already
// complete at the point the email is sent (step 8 / completed_at is written);
// an email failure must not re-trigger the cascade or leave completed_at NULL.
// ---------------------------------------------------------------------------
func TestRunErasureCascade_EmailSendFailureDoesNotBlockCascade(t *testing.T) {
	db := newStubDB()
	sender := &email.MockSender{}
	sender.InjectSendError(errors.New("ses: unavailable"))

	deps := erasure.Deps{
		DB:        db,
		PostHog:   &stubPostHog{},
		Clerk:     &stubClerk{},
		Mailchimp: &stubMailchimp{},
		Email:     sender,
	}

	err := erasure.RunErasureCascade(context.Background(), "job-email-err", "clerk_uid_ee", int64(4), int64(4), deps)
	if err != nil {
		t.Errorf("email-send failure must not cause cascade error; got: %v", err)
	}

	// Step 8 (RecordJobComplete) must still have been called — the cascade
	// completed successfully before the email send was attempted.
	steps := orderedSteps(db)
	found := false
	for _, s := range steps {
		if s == "step8" {
			found = true
			break
		}
	}
	if !found {
		t.Error("step8 (RecordJobComplete) must be called even when email send fails")
	}
}

// ---------------------------------------------------------------------------
// TestRunErasureCascade_NilEmailSenderIsAccepted
//
// Verifies that a nil Deps.Email (email sender not wired) does not cause a
// panic or error — for backward compatibility with existing tests and
// production builds that have not yet wired the SES client.
// ---------------------------------------------------------------------------
func TestRunErasureCascade_NilEmailSenderIsAccepted(t *testing.T) {
	db := newStubDB()

	deps := erasure.Deps{
		DB:        db,
		PostHog:   &stubPostHog{},
		Clerk:     &stubClerk{},
		Mailchimp: &stubMailchimp{},
		Email:     nil, // explicitly nil
	}

	err := erasure.RunErasureCascade(context.Background(), "job-nil-email", "clerk_uid_nil_email", int64(5), int64(5), deps)
	if err != nil {
		t.Errorf("nil Email sender must not cause error; got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestService_StartErasureJob_EmailSentViaGoroutine
//
// Integration path through service.go → RunErasureCascade → EmailSender.
// Verifies that the completion email fires from the background goroutine.
// ---------------------------------------------------------------------------
func TestService_StartErasureJob_EmailSentViaGoroutine(t *testing.T) {
	db := newStubAuditDB()
	sender := &email.MockSender{}

	var wg sync.WaitGroup
	svc := erasure.NewService(context.Background(), db, erasure.Deps{
		DB:        db.stubDB,
		PostHog:   &stubPostHog{},
		Clerk:     &stubClerk{},
		Mailchimp: &stubMailchimp{},
		Email:     sender,
	}, &wg)

	erasure.SetClerkUserIDFromContextFn(func(_ context.Context) (string, bool) {
		return "user_clerk_email_svc_test", true
	})

	_, err := svc.StartErasureJob(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("StartErasureJob returned unexpected synchronous error: %v", err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("wg.Wait timed out — cascade goroutine did not complete")
	}

	if n := sender.DeletionCompleteCallCount(); n != 1 {
		t.Errorf("SendDeletionComplete call count via goroutine: got %d, want 1", n)
	}
}
