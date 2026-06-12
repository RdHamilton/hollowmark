package erasure

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
)

// JobAuditLogger extends DBOps with the CreateAuditLogEntry method needed by
// the service to register a new job before dispatching the goroutine.
//
// CreateAuditLogEntry returns (jobID, alreadyActive, err):
//   - alreadyActive=false → new job created; caller MUST dispatch the goroutine.
//   - alreadyActive=true  → a concurrent job exists; caller MUST NOT dispatch again.
type JobAuditLogger interface {
	DBOps
	CreateAuditLogEntry(ctx context.Context, clerkUserID string, userID int64, accountIDs []int64) (jobID string, alreadyActive bool, err error)
}

// Service orchestrates the erasure cascade: creates the audit log entry,
// dispatches the goroutine from the root context, and exposes the
// StartErasureJob method that satisfies the handlers.erasureJobStarter interface.
type Service struct {
	rootCtx context.Context
	db      JobAuditLogger
	deps    Deps
	wg      *sync.WaitGroup
}

// NewService constructs a Service.
//
// rootCtx must be the BFF root context — NOT the HTTP request context.  The
// goroutines dispatched by StartErasureJob outlive the request lifecycle.
//
// wg is incremented for each dispatched goroutine and decremented on
// completion; the BFF shutdown sequence waits for wg to reach zero.
func NewService(rootCtx context.Context, db JobAuditLogger, deps Deps, wg *sync.WaitGroup) *Service {
	return &Service{
		rootCtx: rootCtx,
		db:      db,
		deps:    deps,
		wg:      wg,
	}
}

// StartErasureJob creates the deletion_audit_log entry, then dispatches a
// background goroutine (from the root context) to run the full cascade across
// ALL of the user's accounts (#1333 fix: was single accountID).
//
// The method returns the job_id immediately — the cascade runs asynchronously.
// The 202 response is sent before the goroutine completes.
//
// Implements the handlers.erasureJobStarter interface.
func (s *Service) StartErasureJob(ctx context.Context, userID int64, accountIDs []int64) (jobID string, err error) {
	// Resolve the Clerk user ID from context.
	// The handler already validated the Clerk session; we read the ID here.
	// NOTE: clerkUserID is stored in the HTTP request context by the Clerk middleware.
	// We need it to write to deletion_audit_log.  The handler resolved userID/accountIDs
	// via the resolver, but also needs to pass clerkUserID to us.  We get it from the
	// request context via a separate lookup — but in Go, context values are opaque.
	//
	// Design choice: the handler passes clerkUserID via the request context using the
	// middleware key.  We read it here using the same key.
	clerkUserID, ok := clerkUserIDFromContext(ctx)
	if !ok || clerkUserID == "" {
		return "", fmt.Errorf("erasure: SetClerkUserIDFromContextFn not configured or returned empty — cannot write deletion_audit_log without a Clerk user ID")
	}

	// Create the audit log entry synchronously before dispatching the goroutine.
	// This ensures the job_id exists in the DB before the 202 is returned, so the
	// polling endpoint can answer immediately.
	//
	// If alreadyActive=true a concurrent cascade is already running for this user.
	// We return the existing job_id (202) without spawning a second goroutine —
	// the idempotency guard (unique per-user WHERE completed_at IS NULL) prevents
	// duplicate cascades.
	var alreadyActive bool
	jobID, alreadyActive, err = s.db.CreateAuditLogEntry(ctx, clerkUserID, userID, accountIDs)
	if err != nil {
		return "", err
	}
	if alreadyActive {
		// Concurrent request: return the in-flight job_id without dispatching again.
		return jobID, nil
	}

	// Capture values for the goroutine closure — do NOT close over request-scoped
	// variables (they may be freed after the 202 response).
	capturedJobID := jobID
	capturedClerkUserID := clerkUserID
	capturedUserID := userID
	capturedAccountIDs := accountIDs
	capturedDeps := s.deps
	capturedDB := s.db

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// Use the root context — not the request context — so the goroutine
		// is not cancelled when the HTTP request completes.
		err := RunErasureCascade(
			s.rootCtx,
			capturedJobID,
			capturedClerkUserID,
			capturedUserID,
			capturedAccountIDs,
			capturedDeps,
		)
		if err != nil {
			log.Printf("[erasure] cascade failed job_id=%s clerk_user_id=%s accounts=%d: %v",
				capturedJobID, capturedClerkUserID, len(capturedAccountIDs), err)
			// RecordJobComplete is NOT called on failure — completed_at stays NULL,
			// which is the AC7 signal for the recovery runbook.
			// The runbook identifies failed jobs by:
			//   SELECT job_id, requested_at FROM deletion_audit_log WHERE completed_at IS NULL;
			// and triggers a re-run by passing job_id + clerk_user_id to the admin
			// re-trigger endpoint (to be implemented in a follow-up ticket).
			_ = capturedDB // suppress unused warning; db retained for future re-trigger
		}
	}()

	return jobID, nil
}

// clerkUserIDFromContextFn holds the wired context-key extractor.
// Stored via sync/atomic.Pointer so concurrent reads and a one-time write
// from cmd/main.go are data-race free.
//
// The type alias avoids a direct instantiation of atomic.Pointer with a
// function type, which requires an addressable value.
type contextExtractorFn func(ctx context.Context) (string, bool)

var clerkUserIDFromContextFn atomic.Pointer[contextExtractorFn]

func clerkUserIDFromContext(ctx context.Context) (string, bool) {
	if fn := clerkUserIDFromContextFn.Load(); fn != nil {
		return (*fn)(ctx)
	}
	return "", false
}

// SetClerkUserIDFromContextFn wires the context-key extractor.  Called from
// cmd/main.go after both packages are imported.  Safe to call concurrently
// (atomic store).
func SetClerkUserIDFromContextFn(fn func(ctx context.Context) (string, bool)) {
	casted := contextExtractorFn(fn)
	clerkUserIDFromContextFn.Store(&casted)
}
