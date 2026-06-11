package erasure_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/erasure"
)

// ---------------------------------------------------------------------------
// stubReporter — captures ReportError calls for assertion
// ---------------------------------------------------------------------------

type reportCall struct {
	err  error
	tags map[string]string
}

type stubReporter struct {
	mu    sync.Mutex
	calls []reportCall
}

func (r *stubReporter) ReportError(_ context.Context, err error, tags ...map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	merged := make(map[string]string)
	for _, m := range tags {
		for k, v := range m {
			merged[k] = v
		}
	}
	r.calls = append(r.calls, reportCall{err: err, tags: merged})
}

func (r *stubReporter) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func (r *stubReporter) firstCall() (reportCall, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return reportCall{}, false
	}
	return r.calls[0], true
}

// ---------------------------------------------------------------------------
// Tests — RED phase: written before implementation
// ---------------------------------------------------------------------------

// TestRunErasureCascade_SentryAlertOnStepFailure verifies that when a cascade
// step fails, ReportError is called exactly once with:
//   - a non-nil error
//   - a "step" tag matching the failing step name
//   - a "job_id" tag matching the job UUID
//   - an "account_id_hash" tag that is NOT the raw account ID string
//   - no "clerk_user_id" tag (no raw PII)
func TestRunErasureCascade_SentryAlertOnStepFailure(t *testing.T) {
	const (
		jobID     = "job-sentry-test-001"
		clerkUID  = "user_clerk_pii_raw"
		rawUserID = int64(999)
		rawAcctID = int64(42)
	)

	db := newStubDB()
	// Inject a failure at step4a so we have a predictable failing step.
	db.injectError("step4a", errors.New("db connection reset"))

	reporter := &stubReporter{}

	deps := erasure.Deps{
		DB:        db,
		PostHog:   &stubPostHog{},
		Clerk:     &stubClerk{},
		Mailchimp: &stubMailchimp{},
		Reporter:  reporter,
	}

	err := erasure.RunErasureCascade(context.Background(), jobID, clerkUID, rawUserID, rawAcctID, deps)
	if err == nil {
		t.Fatal("expected RunErasureCascade to return an error when step4a fails")
	}

	// ReportError must have been called exactly once.
	if n := reporter.callCount(); n != 1 {
		t.Fatalf("ReportError call count: got %d, want 1", n)
	}

	call, _ := reporter.firstCall()

	// Error must be non-nil and match what was injected.
	if call.err == nil {
		t.Error("ReportError called with nil error")
	}

	// "step" tag must identify the failing step.
	step, ok := call.tags["step"]
	if !ok {
		t.Error("ReportError tags missing \"step\" key")
	} else if step != "step4a" {
		t.Errorf("ReportError step tag: got %q, want %q", step, "step4a")
	}

	// "job_id" tag must be present and correct.
	if call.tags["job_id"] != jobID {
		t.Errorf("ReportError job_id tag: got %q, want %q", call.tags["job_id"], jobID)
	}

	// "account_id_hash" must be present and must NOT equal the raw account ID.
	hash, ok := call.tags["account_id_hash"]
	if !ok {
		t.Error("ReportError tags missing \"account_id_hash\" key")
	} else {
		rawAcctStr := fmt.Sprintf("%d", rawAcctID)
		if hash == rawAcctStr {
			t.Errorf("account_id_hash must not be the raw account ID %q — PII leak", rawAcctStr)
		}
		// Hash must be non-empty.
		if hash == "" {
			t.Error("account_id_hash must not be empty")
		}
	}

	// "clerk_user_id" must NOT appear — it is raw PII.
	if v, present := call.tags["clerk_user_id"]; present {
		t.Errorf("ReportError must not include raw clerk_user_id tag (got %q)", v)
	}

	// The raw Clerk UID must not appear in any tag value.
	for k, v := range call.tags {
		if strings.Contains(v, clerkUID) {
			t.Errorf("tag %q contains raw clerk_user_id %q — PII leak", k, clerkUID)
		}
	}
}

// TestRunErasureCascade_NoSentryAlertOnSuccess verifies that ReportError is
// NOT called when the cascade completes without error.
func TestRunErasureCascade_NoSentryAlertOnSuccess(t *testing.T) {
	db := newStubDB()
	reporter := &stubReporter{}

	deps := erasure.Deps{
		DB:        db,
		PostHog:   &stubPostHog{},
		Clerk:     &stubClerk{},
		Mailchimp: &stubMailchimp{},
		Reporter:  reporter,
	}

	err := erasure.RunErasureCascade(context.Background(), "job-ok-001", "clerk_uid_ok", int64(1), int64(1), deps)
	if err != nil {
		t.Fatalf("unexpected error on clean cascade: %v", err)
	}

	if n := reporter.callCount(); n != 0 {
		t.Errorf("ReportError must not be called on success; got %d calls", n)
	}
}

// TestRunErasureCascade_SentryStepTagMatchesEachFailingStep is a table-driven
// test verifying that the step tag correctly identifies each possible failing
// step — not just step4a.
func TestRunErasureCascade_SentryStepTagMatchesEachFailingStep(t *testing.T) {
	cases := []struct {
		injectStep string
		wantStep   string
	}{
		{"step0", "step0"},
		{"step1", "step1"},
		{"step4a", "step4a"},
		{"step4b", "step4b"},
		{"step4c", "step4c"},
		{"step4d", "step4d"},
		{"step4e", "step4e"},
		{"step4explicit", "step4explicit"},
		{"step4sweep", "step4sweep"},
		{"step8", "step8"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.injectStep, func(t *testing.T) {
			db := newStubDB()
			db.injectError(tc.injectStep, fmt.Errorf("injected failure at %s", tc.injectStep))

			reporter := &stubReporter{}
			deps := erasure.Deps{
				DB:        db,
				PostHog:   &stubPostHog{},
				Clerk:     &stubClerk{},
				Mailchimp: &stubMailchimp{},
				Reporter:  reporter,
			}

			err := erasure.RunErasureCascade(context.Background(), "job-tt-001", "clerk_uid_tt", int64(5), int64(7), deps)
			if err == nil {
				t.Fatalf("step=%s: expected error, got nil", tc.injectStep)
			}

			if n := reporter.callCount(); n != 1 {
				t.Fatalf("step=%s: ReportError call count: got %d, want 1", tc.injectStep, n)
			}

			call, _ := reporter.firstCall()
			if call.tags["step"] != tc.wantStep {
				t.Errorf("step=%s: step tag: got %q, want %q", tc.injectStep, call.tags["step"], tc.wantStep)
			}
		})
	}
}

// TestRunErasureCascade_SentryStepTagMatchesExternalFailures covers the
// external-client steps (step2=PostHog, step5=Clerk, step6=Mailchimp) which
// are injected via their respective stubs, not via stubDB.
func TestRunErasureCascade_SentryStepTagMatchesExternalFailures(t *testing.T) {
	cases := []struct {
		name     string
		makeDeps func(*stubReporter) erasure.Deps
		wantStep string
	}{
		{
			name: "posthog_step2",
			makeDeps: func(rep *stubReporter) erasure.Deps {
				db := newStubDB()
				ph := &stubPostHog{err: errors.New("posthog 500")}
				return erasure.Deps{
					DB:        db,
					PostHog:   ph,
					Clerk:     &stubClerk{},
					Mailchimp: &stubMailchimp{},
					Reporter:  rep,
				}
			},
			wantStep: "step2",
		},
		{
			name: "clerk_step5",
			makeDeps: func(rep *stubReporter) erasure.Deps {
				db := newStubDB()
				cl := &stubClerk{err: errors.New("clerk 503")}
				return erasure.Deps{
					DB:        db,
					PostHog:   &stubPostHog{},
					Clerk:     cl,
					Mailchimp: &stubMailchimp{},
					Reporter:  rep,
				}
			},
			wantStep: "step5",
		},
		{
			name: "mailchimp_step6",
			makeDeps: func(rep *stubReporter) erasure.Deps {
				db := newStubDB()
				mc := &stubMailchimp{err: errors.New("mailchimp 429")}
				return erasure.Deps{
					DB:        db,
					PostHog:   &stubPostHog{},
					Clerk:     &stubClerk{},
					Mailchimp: mc,
					Reporter:  rep,
				}
			},
			wantStep: "step6",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			reporter := &stubReporter{}
			deps := tc.makeDeps(reporter)

			err := erasure.RunErasureCascade(context.Background(), "job-ext-001", "clerk_uid_ext", int64(3), int64(8), deps)
			if err == nil {
				t.Fatalf("%s: expected error, got nil", tc.name)
			}

			if n := reporter.callCount(); n != 1 {
				t.Fatalf("%s: ReportError call count: got %d, want 1", tc.name, n)
			}

			call, _ := reporter.firstCall()
			if call.tags["step"] != tc.wantStep {
				t.Errorf("%s: step tag: got %q, want %q", tc.name, call.tags["step"], tc.wantStep)
			}
		})
	}
}

// TestService_StartErasureJob_SentryAlertViaGoroutine verifies that the
// Service's background goroutine propagates a cascade failure to Sentry via
// the Reporter injected in Deps.  This is the integration path through
// service.go → RunErasureCascade → Reporter.
func TestService_StartErasureJob_SentryAlertViaGoroutine(t *testing.T) {
	db := newStubAuditDB()
	db.injectError("step0", errors.New("db timeout"))

	reporter := &stubReporter{}

	var wg sync.WaitGroup
	svc := erasure.NewService(context.Background(), db, erasure.Deps{
		DB:        db.stubDB,
		PostHog:   &stubPostHog{},
		Clerk:     &stubClerk{},
		Mailchimp: &stubMailchimp{},
		Reporter:  reporter,
	}, &wg)

	erasure.SetClerkUserIDFromContextFn(func(_ context.Context) (string, bool) {
		return "user_clerk_svc_test", true
	})

	_, err := svc.StartErasureJob(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("StartErasureJob returned unexpected synchronous error: %v", err)
	}

	// Wait for the goroutine to complete.
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("wg.Wait timed out — cascade goroutine did not complete")
	}

	if n := reporter.callCount(); n != 1 {
		t.Errorf("ReportError call count via goroutine: got %d, want 1", n)
	}
}
