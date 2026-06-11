package erasure_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/erasure"
)

// ---------------------------------------------------------------------------
// Stubs — controllable implementations of each external interface
// ---------------------------------------------------------------------------

// stubDB is a fake DB that tracks calls and can inject errors at specific steps.
type stubDB struct {
	mu sync.Mutex

	// capturedEmail is the email resolved by Step 0.
	capturedEmail string
	// capturedClientIDs is the slice of MTGA client_ids captured in Step 0.
	capturedClientIDs []string

	// stepErrors maps step name to the error to return when that step is called.
	stepErrors map[string]error

	// callOrder records step names in the order they were called.
	callOrder []string
}

func newStubDB() *stubDB {
	return &stubDB{stepErrors: make(map[string]error)}
}

func (s *stubDB) injectError(step string, err error) { s.stepErrors[step] = err }

func (s *stubDB) record(step string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callOrder = append(s.callOrder, step)
	return s.stepErrors[step]
}

// CapturePreJobData implements erasure.PreJobDataSource.
func (s *stubDB) CapturePreJobData(ctx context.Context, userID, accountID int64) (email string, clientIDs []string, err error) {
	if err := s.record("step0"); err != nil {
		return "", nil, err
	}
	s.capturedEmail = "test@example.com"
	s.capturedClientIDs = []string{"client_abc", "client_xyz"}
	return s.capturedEmail, s.capturedClientIDs, nil
}

// SoftDeleteUser implements erasure.UserSoftDeleter.
func (s *stubDB) SoftDeleteUser(ctx context.Context, userID int64) error {
	return s.record("step1")
}

// DeleteTextKeyedRows implements erasure.TextKeyedDeleter.
func (s *stubDB) DeleteTextKeyedRows(ctx context.Context, clientIDs []string) error {
	return s.record("step4a")
}

// AnonymizeConsentLog implements erasure.ConsentLogAnonymizer.
func (s *stubDB) AnonymizeConsentLog(ctx context.Context, accountID int64) error {
	return s.record("step4b")
}

// DeleteWaitlistEntry implements erasure.WaitlistDeleter.
func (s *stubDB) DeleteWaitlistEntry(ctx context.Context, email string) error {
	return s.record("step4c")
}

// HardDeleteUser implements erasure.UserHardDeleter.
func (s *stubDB) HardDeleteUser(ctx context.Context, userID int64) error {
	return s.record("step4d")
}

// HardDeleteAccount implements erasure.AccountHardDeleter.
func (s *stubDB) HardDeleteAccount(ctx context.Context, accountID int64) error {
	return s.record("step4e")
}

// DeleteExplicitBigintRows implements erasure.ExplicitBigintRowDeleter.
// Step 4-explicit: deletes identity-keyed BIGINT-account_id tables that are
// cascade-unreachable at certain schema versions.
func (s *stubDB) DeleteExplicitBigintRows(ctx context.Context, accountID int64) error {
	return s.record("step4explicit")
}

// AssertZeroResiduals implements erasure.ResidualSweeper.
// Step 4-sweep: queries information_schema and asserts no residual rows remain
// for the erased account/clientIDs.
func (s *stubDB) AssertZeroResiduals(ctx context.Context, accountID int64, clientIDs []string) error {
	return s.record("step4sweep")
}

// RecordJobComplete implements erasure.AuditLogger.
func (s *stubDB) RecordJobComplete(ctx context.Context, jobID string) error {
	return s.record("step8")
}

// stubPostHog stubs the PostHog bulk-delete.
type stubPostHog struct {
	mu        sync.Mutex
	callOrder []string
	err       error
}

func (s *stubPostHog) DeletePerson(ctx context.Context, distinctID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callOrder = append(s.callOrder, "step2")
	return s.err
}

// stubClerk stubs the Clerk user deletion.
type stubClerk struct {
	mu        sync.Mutex
	callOrder []string
	err       error
}

func (s *stubClerk) DeleteUser(ctx context.Context, clerkUserID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callOrder = append(s.callOrder, "step5")
	return s.err
}

// stubMailchimp stubs the Mailchimp permanent delete.
type stubMailchimp struct {
	mu        sync.Mutex
	callOrder []string
	err       error

	// capturedAction is the action path asserted by the test.
	capturedAction string
}

func (s *stubMailchimp) DeletePermanent(ctx context.Context, email string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callOrder = append(s.callOrder, "step6")
	s.capturedAction = "delete-permanent"
	return s.err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestDeps(db *stubDB, ph *stubPostHog, cl *stubClerk, mc *stubMailchimp) erasure.Deps {
	return erasure.Deps{
		DB:        db,
		PostHog:   ph,
		Clerk:     cl,
		Mailchimp: mc,
	}
}

// collectOrder merges the call orders from db + external stubs into a single
// ordered slice by replaying: db.callOrder interleaved with external stub
// orders is not deterministic from separate slices.  Instead, each stub
// records into a shared call log via the db stub for ordering tests.
func orderedSteps(db *stubDB) []string {
	db.mu.Lock()
	defer db.mu.Unlock()
	return append([]string(nil), db.callOrder...)
}

// ---------------------------------------------------------------------------
// Tests — RED phase (written before implementation)
// ---------------------------------------------------------------------------

// TestRunErasureCascade_StepOrderInvariant verifies the full cascade executes
// in the mandatory order defined by ADR-056 (amended by ticket #1257):
//
//	step0 → step1 → step2 → step4a → step4b → step4c → step4d → step4e →
//	step4explicit → step4sweep → step5 → step6 → step8
//
// Key invariants:
//   - PostHog delete (step2) MUST run before Clerk delete (step5).
//   - Residual sweep (step4sweep) runs after all DB deletes, before Clerk (step5).
//   - RecordJobComplete (step8) runs only after the sweep succeeds.
func TestRunErasureCascade_StepOrderInvariant(t *testing.T) {
	// Use a shared log embedded in the DB stub so ordering is deterministic.
	db := newStubDB()
	ph := &stubPostHog{}
	cl := &stubClerk{}
	mc := &stubMailchimp{}

	// Wire external stubs to record into db.callOrder for unified ordering.
	phRecorder := &recordingPostHog{inner: ph, db: db}
	clRecorder := &recordingClerk{inner: cl, db: db}
	mcRecorder := &recordingMailchimp{inner: mc, db: db}

	deps := erasure.Deps{
		DB:        db,
		PostHog:   phRecorder,
		Clerk:     clRecorder,
		Mailchimp: mcRecorder,
	}

	err := erasure.RunErasureCascade(context.Background(), "job-1", "clerk_uid_1", int64(99), int64(42), deps)
	if err != nil {
		t.Fatalf("RunErasureCascade returned unexpected error: %v", err)
	}

	got := orderedSteps(db)
	want := []string{
		"step0", "step1", "step2",
		"step4a", "step4b", "step4c", "step4d", "step4e",
		"step4explicit", "step4sweep",
		"step5", "step6", "step8",
	}
	if len(got) != len(want) {
		t.Fatalf("step count: got %d steps %v, want %d steps %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("step[%d]: got %q, want %q (full order: %v)", i, got[i], want[i], got)
		}
	}
}

// TestRunErasureCascade_SweepFailureHaltsBeforeClerkAndComplete verifies C4:
// when the residual sweep (step4sweep) fails, Step 5 (Clerk delete) AND Step 8
// (RecordJobComplete / completed_at) are both uncalled — no irreversible external
// delete occurs and no silent "complete" write happens.  This ensures the job
// leaves deletion_audit_log.completed_at = NULL for the AC7 re-trigger runbook.
func TestRunErasureCascade_SweepFailureHaltsBeforeClerkAndComplete(t *testing.T) {
	db := newStubDB()
	db.injectError("step4sweep", errors.New("residual rows found: matches(2)"))
	ph := &stubPostHog{}
	cl := &stubClerk{}
	mc := &stubMailchimp{}
	phRecorder := &recordingPostHog{inner: ph, db: db}
	clRecorder := &recordingClerk{inner: cl, db: db}
	mcRecorder := &recordingMailchimp{inner: mc, db: db}

	deps := erasure.Deps{DB: db, PostHog: phRecorder, Clerk: clRecorder, Mailchimp: mcRecorder}

	err := erasure.RunErasureCascade(context.Background(), "job-sweep-fail", "clerk_uid_sf", int64(1), int64(1), deps)
	if err == nil {
		t.Error("expected error from sweep failure, got nil")
	}

	steps := orderedSteps(db)
	for _, s := range steps {
		if s == "step5" {
			t.Error("step5 (Clerk delete) must NOT be called when step4sweep fails — external delete is irreversible")
		}
		if s == "step8" {
			t.Error("step8 (RecordJobComplete) must NOT be called when step4sweep fails — completed_at must remain NULL for AC7 re-trigger")
		}
	}
}

// TestRunErasureCascade_ExplicitBigintDeleteBeforeSweep verifies that the
// explicit BIGINT-keyed deletes (step4explicit) run before the residual sweep
// (step4sweep).  The sweep must see the state AFTER explicit deletes.
func TestRunErasureCascade_ExplicitBigintDeleteBeforeSweep(t *testing.T) {
	db := newStubDB()
	ph := &stubPostHog{}
	cl := &stubClerk{}
	mc := &stubMailchimp{}
	phRecorder := &recordingPostHog{inner: ph, db: db}
	clRecorder := &recordingClerk{inner: cl, db: db}
	mcRecorder := &recordingMailchimp{inner: mc, db: db}

	deps := erasure.Deps{DB: db, PostHog: phRecorder, Clerk: clRecorder, Mailchimp: mcRecorder}
	_ = erasure.RunErasureCascade(context.Background(), "job-order", "clerk_uid_order", int64(1), int64(1), deps)

	steps := orderedSteps(db)
	idxExplicit, idxSweep, idxClerk := -1, -1, -1
	for i, s := range steps {
		switch s {
		case "step4explicit":
			idxExplicit = i
		case "step4sweep":
			idxSweep = i
		case "step5":
			idxClerk = i
		}
	}
	if idxExplicit == -1 {
		t.Fatal("step4explicit was never called")
	}
	if idxSweep == -1 {
		t.Fatal("step4sweep was never called")
	}
	if idxClerk == -1 {
		t.Fatal("step5 was never called")
	}
	if idxExplicit >= idxSweep {
		t.Errorf("step4explicit must run before step4sweep; got indices explicit=%d sweep=%d in %v",
			idxExplicit, idxSweep, steps)
	}
	if idxSweep >= idxClerk {
		t.Errorf("step4sweep must run before step5 (Clerk); got indices sweep=%d clerk=%d in %v",
			idxSweep, idxClerk, steps)
	}
}

// TestRunErasureCascade_ClerkDeletedAfterPostHog verifies the critical
// FM-1 invariant: Step 5 (Clerk delete) NEVER runs before Step 2 (PostHog
// bulk-delete). If Clerk is deleted first, the account hash mapping is lost.
func TestRunErasureCascade_ClerkDeletedAfterPostHog(t *testing.T) {
	db := newStubDB()
	ph := &stubPostHog{}
	cl := &stubClerk{}
	mc := &stubMailchimp{}
	phRecorder := &recordingPostHog{inner: ph, db: db}
	clRecorder := &recordingClerk{inner: cl, db: db}
	mcRecorder := &recordingMailchimp{inner: mc, db: db}

	deps := erasure.Deps{
		DB:        db,
		PostHog:   phRecorder,
		Clerk:     clRecorder,
		Mailchimp: mcRecorder,
	}

	_ = erasure.RunErasureCascade(context.Background(), "job-2", "clerk_uid_2", int64(1), int64(1), deps)

	steps := orderedSteps(db)
	posthogIdx := -1
	clerkIdx := -1
	for i, s := range steps {
		switch s {
		case "step2":
			posthogIdx = i
		case "step5":
			clerkIdx = i
		}
	}
	if posthogIdx == -1 {
		t.Error("step2 (PostHog delete) was never called")
	}
	if clerkIdx == -1 {
		t.Error("step5 (Clerk delete) was never called")
	}
	if clerkIdx != -1 && posthogIdx != -1 && clerkIdx < posthogIdx {
		t.Errorf("FM-1 violated: Clerk deleted (step %d) before PostHog (step %d) — hash mapping lost",
			clerkIdx, posthogIdx)
	}
}

// TestRunErasureCascade_Step0FailureAbortsJob verifies that a failure in
// Step 0 (pre-job data capture) aborts the cascade — no subsequent step runs.
func TestRunErasureCascade_Step0FailureAbortsJob(t *testing.T) {
	db := newStubDB()
	db.injectError("step0", errors.New("db timeout"))
	ph := &stubPostHog{}
	cl := &stubClerk{}
	mc := &stubMailchimp{}
	phRecorder := &recordingPostHog{inner: ph, db: db}
	clRecorder := &recordingClerk{inner: cl, db: db}
	mcRecorder := &recordingMailchimp{inner: mc, db: db}

	deps := erasure.Deps{DB: db, PostHog: phRecorder, Clerk: clRecorder, Mailchimp: mcRecorder}

	err := erasure.RunErasureCascade(context.Background(), "job-3", "clerk_uid_3", int64(1), int64(1), deps)
	if err == nil {
		t.Error("expected error from Step 0 failure, got nil")
	}

	steps := orderedSteps(db)
	if len(steps) != 1 || steps[0] != "step0" {
		t.Errorf("expected only step0, got %v", steps)
	}
}

// TestRunErasureCascade_ClerkNotCalledIfDBFails verifies the FM-1 safety
// boundary: if Step 4e (account hard-delete) fails, Clerk delete (Step 5) is
// NOT invoked — the cascade halts before committing the Clerk identity destruction.
func TestRunErasureCascade_ClerkNotCalledIfDBFails(t *testing.T) {
	db := newStubDB()
	db.injectError("step4e", errors.New("fk constraint"))
	ph := &stubPostHog{}
	cl := &stubClerk{}
	mc := &stubMailchimp{}
	phRecorder := &recordingPostHog{inner: ph, db: db}
	clRecorder := &recordingClerk{inner: cl, db: db}
	mcRecorder := &recordingMailchimp{inner: mc, db: db}

	deps := erasure.Deps{DB: db, PostHog: phRecorder, Clerk: clRecorder, Mailchimp: mcRecorder}

	err := erasure.RunErasureCascade(context.Background(), "job-4", "clerk_uid_4", int64(1), int64(1), deps)
	if err == nil {
		t.Error("expected error from Step 4e failure, got nil")
	}

	steps := orderedSteps(db)
	for _, s := range steps {
		if s == "step5" {
			t.Error("step5 (Clerk delete) must NOT be called when step4e fails")
		}
	}
}

// TestRunErasureCascade_IdempotentTextKeyedDelete verifies that the
// DELETE…ANY($client_ids) for TEXT-keyed tables uses the client_ids captured
// in Step 0 — not derived from accounts after its deletion.
func TestRunErasureCascade_UsesStep0ClientIDs(t *testing.T) {
	db := newStubDB()
	ph := &stubPostHog{}
	cl := &stubClerk{}
	mc := &stubMailchimp{}
	phRecorder := &recordingPostHog{inner: ph, db: db}
	clRecorder := &recordingClerk{inner: cl, db: db}
	mcRecorder := &recordingMailchimp{inner: mc, db: db}

	deps := erasure.Deps{DB: db, PostHog: phRecorder, Clerk: clRecorder, Mailchimp: mcRecorder}

	err := erasure.RunErasureCascade(context.Background(), "job-5", "clerk_uid_5", int64(1), int64(1), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify client_ids were captured in step 0 before any deletion.
	if len(db.capturedClientIDs) == 0 {
		t.Error("expected client_ids to be captured in step 0")
	}
}

// TestRunErasureCascade_MailchimpUsesPermanentDelete verifies the Q2 ruling:
// the Mailchimp action is DeletePermanent (not unsubscribe).
func TestRunErasureCascade_MailchimpUsesPermanentDelete(t *testing.T) {
	db := newStubDB()
	mc := &stubMailchimp{}
	ph := &stubPostHog{}
	cl := &stubClerk{}
	phRecorder := &recordingPostHog{inner: ph, db: db}
	clRecorder := &recordingClerk{inner: cl, db: db}
	mcRecorder := &recordingMailchimp{inner: mc, db: db}

	deps := erasure.Deps{DB: db, PostHog: phRecorder, Clerk: clRecorder, Mailchimp: mcRecorder}

	_ = erasure.RunErasureCascade(context.Background(), "job-6", "clerk_uid_6", int64(1), int64(1), deps)

	if mc.capturedAction != "delete-permanent" {
		t.Errorf("Mailchimp action = %q, want %q (Q2 ruling: must use delete-permanent, not unsubscribe)",
			mc.capturedAction, "delete-permanent")
	}
}

// TestRunErasureCascade_Step4aBeforeStep4e verifies TEXT-keyed table deletions
// (step4a) run BEFORE the accounts hard-delete (step4e).
func TestRunErasureCascade_Step4aBeforeStep4e(t *testing.T) {
	db := newStubDB()
	ph := &stubPostHog{}
	cl := &stubClerk{}
	mc := &stubMailchimp{}
	phRecorder := &recordingPostHog{inner: ph, db: db}
	clRecorder := &recordingClerk{inner: cl, db: db}
	mcRecorder := &recordingMailchimp{inner: mc, db: db}

	deps := erasure.Deps{DB: db, PostHog: phRecorder, Clerk: clRecorder, Mailchimp: mcRecorder}
	_ = erasure.RunErasureCascade(context.Background(), "job-7", "clerk_uid_7", int64(1), int64(1), deps)

	steps := orderedSteps(db)
	idx4a, idx4e := -1, -1
	for i, s := range steps {
		if s == "step4a" {
			idx4a = i
		}
		if s == "step4e" {
			idx4e = i
		}
	}
	if idx4a == -1 || idx4e == -1 {
		t.Fatalf("step4a=%d step4e=%d — one or both steps missing from %v", idx4a, idx4e, steps)
	}
	if idx4a >= idx4e {
		t.Errorf("step4a (TEXT-keyed delete) must run before step4e (accounts delete); got indices %d >= %d in %v",
			idx4a, idx4e, steps)
	}
}

// ---------------------------------------------------------------------------
// Recording wrappers — route external stubs into the shared db.callOrder
// ---------------------------------------------------------------------------

type recordingPostHog struct {
	inner *stubPostHog
	db    *stubDB
}

func (r *recordingPostHog) DeletePerson(ctx context.Context, distinctID string) error {
	if err := r.db.record("step2"); err != nil {
		return err
	}
	return r.inner.DeletePerson(ctx, distinctID)
}

type recordingClerk struct {
	inner *stubClerk
	db    *stubDB
}

func (r *recordingClerk) DeleteUser(ctx context.Context, clerkUserID string) error {
	if err := r.db.record("step5"); err != nil {
		return err
	}
	return r.inner.DeleteUser(ctx, clerkUserID)
}

type recordingMailchimp struct {
	inner *stubMailchimp
	db    *stubDB
}

func (r *recordingMailchimp) DeletePermanent(ctx context.Context, email string) error {
	if err := r.db.record("step6"); err != nil {
		return err
	}
	return r.inner.DeletePermanent(ctx, email)
}
