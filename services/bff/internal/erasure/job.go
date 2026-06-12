// Package erasure implements the asynchronous GDPR Art.17 right-to-erasure
// cascade job (ADR-056).
//
// The cascade runs in a background goroutine dispatched from the BFF root
// context (not the request context, which cancels on the 202 response).  The
// caller must increment its sync.WaitGroup before dispatch and defer a
// decrement so the BFF shutdown sequence drains in-flight jobs.
//
// # Step ordering
//
//	Step 0        — Capture email + all MTGA client_id strings into memory (pre-job).
//	Step 1        — Soft-delete gate: UPDATE users SET deleted_at=NOW() (sync, before 202).
//	Step 2        — PostHog bulk-delete: recompute hash → DELETE person.
//	Step 4a       — Explicit DELETE of TEXT-keyed tables using captured client_ids.
//	                Also covers inventory_history when its account_id is TEXT (incremental
//	                migration path via 000068). See F1 note in ExplicitBigintRowDeleter.
//	Step 4b       — Anonymize consent_log in-place (null ip_address_hash + metadata).
//	Step 4c       — DELETE waitlist_entries WHERE email = <captured>.
//	Step 4d       — Hard-delete users row (cascades to api_keys).
//	Step 4e       — Hard-delete accounts row (fires ON DELETE CASCADE + SET NULL on consent_log).
//	Step 4-explicit — Defense-in-depth explicit DELETE of BIGINT-account_id tables that
//	                are cascade-unreachable at certain schema versions (game_plays has no FK
//	                today; matches/player_stats/rank_history/collection_history had no FK
//	                before migration 000119; inventory_history when BIGINT path per 000054).
//	                Runs AFTER Step 4e so any FK cascade already fired; the explicit DELETE
//	                is a no-op if cascade already removed the rows.
//	Step 4-sweep  — Residual sweep: queries information_schema and asserts zero rows remain
//	                for the erased account/clientIDs across all identity-keyed tables.
//	                Runs AFTER Step 4-explicit, BEFORE Step 5.  On failure: halts cascade,
//	                leaves completed_at NULL for AC7 re-trigger.
//	Step 5        — Clerk Admin API DELETE /v1/users/{id} — MUST follow Steps 1–4-sweep.
//	Step 6        — Mailchimp delete-permanent (GDPR + suppression).
//	Step 8        — Mark deletion_audit_log.completed_at = NOW().
//
// The step numbering matches ADR-056 §Decision; Steps 3 and 7 are no-ops in
// the current implementation (ML data is anonymous; no Redis cache to clear).
package erasure

import (
	"context"
	"fmt"
	"log"
	"strconv"

	emailpkg "github.com/RdHamilton/hollowmark/services/bff/internal/email"
	"github.com/RdHamilton/hollowmark/services/bff/internal/identityhash"
)

// PreJobDataSource is satisfied by the deletion repository's CapturePreJobData
// method.  It captures the email and all MTGA client_id strings associated
// with ALL of the user's accounts before any row is deleted (#1333: was
// single accountID — multi-account users had clientIDs from N-1 accounts
// silently omitted).
type PreJobDataSource interface {
	CapturePreJobData(ctx context.Context, userID int64, accountIDs []int64) (email string, clientIDs []string, err error)
}

// UserSoftDeleter marks users.deleted_at synchronously — before the 202 is
// returned — to block new writes from concurrent requests.
type UserSoftDeleter interface {
	SoftDeleteUser(ctx context.Context, userID int64) error
}

// TextKeyedDeleter deletes all rows in TEXT-keyed tables that reference MTGA
// client_id strings.  These tables have no FK to accounts and are unreachable
// by cascade:
//   - daemon_events.account_id TEXT
//   - daemon_api_keys.account_id TEXT
//   - user_play_patterns.account_id TEXT
//   - projection_errors.account_id TEXT
//   - inventory_history.account_id TEXT (incremental migration path via 000068 only;
//     see ExplicitBigintRowDeleter for the BIGINT path)
//
// quest_session_tracking is NOT in this list — migration 000080 converted its
// account_id column to BIGINT FK (ON DELETE CASCADE from accounts.id), so it is
// erased by AccountHardDeleter (Step 4e), not here.
type TextKeyedDeleter interface {
	DeleteTextKeyedRows(ctx context.Context, clientIDs []string) error
}

// ConsentLogAnonymizer anonymizes consent_log rows in-place by setting
// ip_address_hash=NULL and metadata=NULL for ALL of the user's account IDs
// (#1333: was a single accountID — multi-account users had N-1 consent_log
// rows left un-anonymized).
// The ON DELETE SET NULL cascade on consent_log.account_id (migration #885)
// then fires when each accounts row is deleted in Step 4e.
type ConsentLogAnonymizer interface {
	AnonymizeConsentLog(ctx context.Context, accountIDs []int64) error
}

// WaitlistDeleter deletes waitlist_entries rows by email (CITEXT match).
type WaitlistDeleter interface {
	DeleteWaitlistEntry(ctx context.Context, email string) error
}

// UserHardDeleter hard-deletes the users row, which cascades to api_keys.
type UserHardDeleter interface {
	HardDeleteUser(ctx context.Context, userID int64) error
}

// AccountHardDeleter hard-deletes the accounts row.  This fires:
//   - ON DELETE CASCADE on all BIGINT FK user-keyed tables (collection, decks,
//     drafts, quests, user_settings, etc. — per ADR-056 FM-3 enumeration).
//   - ON DELETE SET NULL on consent_log.account_id (migration #885).
//
// Note: game_plays has no FK CASCADE today (000120 deferred it); matches,
// player_stats, rank_history, collection_history gained FKs in 000119 but did
// not have them at the time of the P1 incident (#1237).  These tables are
// covered defense-in-depth by ExplicitBigintRowDeleter (Step 4-explicit).
type AccountHardDeleter interface {
	HardDeleteAccount(ctx context.Context, accountID int64) error
}

// ExplicitBigintRowDeleter performs defense-in-depth explicit DELETEs on
// BIGINT-account_id tables that are cascade-unreachable at some schema versions
// (#1257 — erasure completeness class fix):
//
//   - matches        — FK added 000119; absent on pre-119 DBs (P1 incident class).
//   - player_stats   — FK added 000119; absent on pre-119 DBs.
//   - rank_history   — FK added 000119; absent on pre-119 DBs.
//   - collection_history — FK added 000119; absent on pre-119 DBs.
//   - game_plays     — account_id converted TEXT→BIGINT in 000120; NO FK CASCADE today.
//   - inventory_history  — BIGINT FK CASCADE on fresh-init (000054) path only; see below.
//
// # inventory_history schema fork (F1)
//
// The incremental migration path (000068) added account_id as TEXT; the
// fresh-init (000054) schema has it as BIGINT NOT NULL FK CASCADE.  There is no
// conversion migration between them.  DeleteExplicitBigintRows MUST gate on
// information_schema.data_type before issuing the DELETE:
//   - TEXT path: inventory_history is already covered by DeleteTextKeyedRows (step4a).
//   - BIGINT path: issue DELETE WHERE account_id = $1::bigint here.
//
// The gate mirrors how migration 000120 handles the game_plays type fork.
//
// Step ordering: runs AFTER HardDeleteAccount (step4e) so FK cascades have
// already fired; the explicit DELETEs are no-ops when cascade covered the rows.
// Explicitly deletes are idempotent — re-running is safe.
type ExplicitBigintRowDeleter interface {
	DeleteExplicitBigintRows(ctx context.Context, accountID int64) error
}

// ResidualSweeper queries information_schema.columns for every public base table
// with an account_id or user_id column and asserts zero residual rows for ALL
// of the erased accountIDs and clientIDs (#1333: was a single accountID —
// multi-account users had N-1 accounts unchecked by the sweep).
//
// Coverage:
//   - BIGINT/identity tables: WHERE account_id = ANY($accountIDs)
//   - TEXT-keyed tables:      WHERE account_id = ANY($clientIDs) — skip if clientIDs empty
//
// Excluded (retained by design — Ray ruling #1257):
//   - deletion_audit_log     — compliance evidence, non-identifiable post-erasure
//   - restriction_audit_log  — GDPR Art.18 audit trail
//   - dsr_access_log         — GDPR Art.15 access log
//   - rectification_audit_log — GDPR Art.16 rectification audit
//
// On failure: returns a non-nil error listing ALL offending tables (fail-all,
// not fail-first — C2 condition).  The sweep must be called AFTER
// ExplicitBigintRowDeleter and BEFORE ClerkDeleter so no irreversible external
// delete occurs when residuals exist.
type ResidualSweeper interface {
	AssertZeroResiduals(ctx context.Context, accountIDs []int64, clientIDs []string) error
}

// AuditLogger marks the deletion_audit_log row as completed.
type AuditLogger interface {
	RecordJobComplete(ctx context.Context, jobID string) error
}

// PostHogDeleter calls the PostHog bulk-delete API to erase all events for a
// distinct_id.  The distinct_id is the identityhash of the accountID string,
// recomputed from accountID — NOT from the Clerk user id.
type PostHogDeleter interface {
	DeletePerson(ctx context.Context, distinctID string) error
}

// ClerkDeleter calls the Clerk Admin API to delete the user identity.
// MUST run after Steps 1–4-sweep — deleting Clerk first destroys the hash
// mapping needed for the PostHog delete.
type ClerkDeleter interface {
	DeleteUser(ctx context.Context, clerkUserID string) error
}

// MailchimpPermanentDeleter calls the Mailchimp Marketing API
// POST /3.0/lists/{id}/members/{hash}/actions/delete-permanent.
// This is a GDPR delete that removes the contact record AND adds a suppression
// hash so the address cannot be re-added.  It is distinct from Unsubscribe.
type MailchimpPermanentDeleter interface {
	DeletePermanent(ctx context.Context, email string) error
}

// ErrorReporter is satisfied by observability.Reporter (a zero-value adapter
// wrapping the package-level observability.ReportError function).  It is
// separated into an interface so RunErasureCascade remains independently
// testable without a real Sentry hub.
//
// The signature mirrors observability.ReportError exactly:
//
//	func ReportError(ctx context.Context, err error, tags ...map[string]string)
//
// A nil Reporter field is legal — the alert is silently skipped (e.g. in
// tests that do not need Sentry assertions).  Production wires in
// observability.Reporter{} via the Deps struct.
type ErrorReporter interface {
	ReportError(ctx context.Context, err error, tags ...map[string]string)
}

// DBOps is the combined interface that the deletion repository must satisfy.
// It groups all database-side operations for the cascade.
type DBOps interface {
	PreJobDataSource
	UserSoftDeleter
	TextKeyedDeleter
	ConsentLogAnonymizer
	WaitlistDeleter
	UserHardDeleter
	AccountHardDeleter
	ExplicitBigintRowDeleter
	ResidualSweeper
	AuditLogger
}

// Deps holds all external collaborators for RunErasureCascade.
// Separating DB from the external API clients makes each side independently
// injectable in tests.
type Deps struct {
	DB        DBOps
	PostHog   PostHogDeleter
	Clerk     ClerkDeleter
	Mailchimp MailchimpPermanentDeleter
	// Reporter receives a Sentry alert when a cascade step fails (#887 AC5).
	// Nil is safe — the alert is skipped when not wired (e.g. unit tests that
	// do not assert Sentry behavior).  Production sets this to observability.Reporter{}.
	Reporter ErrorReporter
	// Email is the transactional-email sender (ADR-076 / hollowmark-tickets#1172).
	// Nil is safe — the email send is skipped when not wired (pre-SES-provisioning
	// builds and tests that do not assert email behavior).
	// An error from Email.Send* is logged but does NOT block or re-trigger the
	// cascade — the cascade is already complete when the email is attempted.
	Email emailpkg.Sender
}

// RunErasureCascade executes the full GDPR Art.17 erasure cascade for ALL of
// a user's accounts.  It is designed to be called in a background goroutine
// from the BFF root context.
//
// Parameters:
//   - ctx: BFF root context (NOT the HTTP request context).
//   - jobID: UUID of the deletion_audit_log row for this job.
//   - clerkUserID: Clerk user id (used only for Step 5 Clerk API delete).
//   - userID: internal users.id (BIGINT).
//   - accountIDs: ALL internal accounts.id values owned by userID (#1333 fix:
//     was a single accountID — GDPR Art.17 requires erasing ALL accounts).
//   - deps: injected collaborators.
//
// On any step failure the cascade halts and returns the error; the
// deletion_audit_log row retains a NULL completed_at so the AC7 runbook can
// identify and re-trigger incomplete jobs by job_id.
//
// When deps.Reporter is non-nil, a Sentry alert is fired on the first step
// failure with structured tags: step (short key e.g. "step4a"), job_id, and
// user_id_hash (SHA-256 prefix of the userID string — no raw PII).
func RunErasureCascade(ctx context.Context, jobID, clerkUserID string, userID int64, accountIDs []int64, deps Deps) error {
	// Defensive nil guard: deps.DB must always be wired by the caller
	// (buildAccountDeletionHandler in cmd/main.go).  A nil DB indicates a
	// construction bug — fail loud with a clear error rather than panicking.
	if deps.DB == nil {
		return fmt.Errorf("erasure: RunErasureCascade called with nil Deps.DB — cascade cannot proceed; this is a wiring bug in the caller")
	}

	// userIDHash is the privacy-safe tag value used in Sentry alerts.
	// Keyed on userID (the human identity) rather than any single accountID,
	// since erasure now spans all accounts.
	userIDHash := identityhash.HashAccountID(strconv.FormatInt(userID, 10))

	// capturedEmail holds the Step-0 email address once CapturePreJobData
	// succeeds.  It is written once (Step 0) and read by reportStepErr and the
	// success path.  An empty string means Step 0 failed and no email can be
	// sent.
	var capturedEmail string
	var err error

	// sendEmailNonFatal calls fn(ctx, addr) when the email sender is wired and
	// addr is non-empty.  Any error is logged but does NOT propagate — email
	// sends are fire-and-forget from the cascade's perspective.
	sendEmailNonFatal := func(fn func(context.Context, string) error, addr string) {
		if deps.Email == nil || addr == "" {
			return
		}
		if sendErr := fn(ctx, addr); sendErr != nil {
			log.Printf("[erasure] email send failed job_id=%s: %v", jobID, sendErr)
		}
	}

	// reportStepErr fires a Sentry alert via deps.Reporter (when non-nil), sends
	// a failure notification email (when Email is wired and the address was
	// captured), then wraps and returns the error so the caller can halt the
	// cascade.
	//
	//   stepKey — short canonical identifier written to the "step" Sentry tag
	//             (e.g. "step4a"); keep stable — runbooks key on these values.
	//   desc    — human-readable label included in the wrapped error message.
	reportStepErr := func(stepKey, desc string, err error) error {
		if deps.Reporter != nil {
			deps.Reporter.ReportError(ctx, err, map[string]string{
				"step":          stepKey,
				"job_id":        jobID,
				"user_id_hash":  userIDHash,
				"account_count": strconv.Itoa(len(accountIDs)),
			})
		}
		// Send failure notification using the Step-0-captured address.
		// capturedEmail is empty when Step 0 itself failed — skip in that case.
		if deps.Email != nil {
			sendEmailNonFatal(deps.Email.SendDeletionFailed, capturedEmail)
		}
		return fmt.Errorf("erasure %s (%s): %w", stepKey, desc, err)
	}

	// -----------------------------------------------------------------------
	// Step 0 — Capture email + all client_id strings BEFORE any deletion.
	//
	// The TEXT-keyed tables (daemon_events, daemon_api_keys, etc.) are keyed by
	// the MTGA client_id string, not by the internal accounts.id BIGINT.  Once
	// the accounts rows are deleted in Step 4e, these strings cannot be recovered
	// from the DB.  We capture them here — across ALL accounts — to use in Step 4a.
	//
	// The email address is also held in capturedEmail so the post-cascade email
	// sends (success / failure paths) use the in-memory value — by terminal state
	// the DB row is gone and a lookup would return nothing (ADR-076 §Consequences).
	// -----------------------------------------------------------------------
	var clientIDs []string
	capturedEmail, clientIDs, err = deps.DB.CapturePreJobData(ctx, userID, accountIDs)
	if err != nil {
		return reportStepErr("step0", "capture pre-job data", err)
	}

	// -----------------------------------------------------------------------
	// Step 1 — Soft-delete gate.
	//
	// Sets users.deleted_at = NOW().  This is called synchronously before the
	// 202 response; downstream steps run asynchronously.  The soft-delete
	// blocks new daemon ingest writes from hitting the tenant.
	// -----------------------------------------------------------------------
	if err := deps.DB.SoftDeleteUser(ctx, userID); err != nil {
		return reportStepErr("step1", "soft-delete user", err)
	}

	// -----------------------------------------------------------------------
	// Step 2 — PostHog bulk-delete.
	//
	// The distinct_id is SHA-256(userID string)[:16] — keyed on the user (the
	// human), not a single account, since PostHog tracks at the user level.
	//
	// Note: PostHog step precedes the DB sweep by ADR-056 hash-dependency
	// design (the hash is derived from userID which is available here).
	// PostHog delete is re-runnable on re-trigger; it does not need to follow
	// the sweep.
	// -----------------------------------------------------------------------
	if err := deps.PostHog.DeletePerson(ctx, userIDHash); err != nil {
		return reportStepErr("step2", "posthog bulk-delete", err)
	}

	// Step 3 — ML training data: no-op.  ML data is anonymous (D7a, ADR-058
	// Option A accepted).  No personal data in the ML store; no delete needed.

	// -----------------------------------------------------------------------
	// Step 4 — PostgreSQL explicit deletes (in FK-dependency order).
	// -----------------------------------------------------------------------

	// 4a — TEXT-keyed tables: delete BEFORE accounts rows (ordering mandatory).
	//
	// Covers tables with TEXT account_id (MTGA client_id string, no FK):
	//   daemon_events, daemon_api_keys, user_play_patterns, projection_errors.
	// Also covers inventory_history when its account_id column is TEXT
	// (incremental migration path via 000068). See TextKeyedDeleter doc.
	// clientIDs aggregated from ALL accounts in Step 0.
	if err := deps.DB.DeleteTextKeyedRows(ctx, clientIDs); err != nil {
		return reportStepErr("step4a", "text-keyed delete", err)
	}

	// 4b — Anonymize consent_log in-place for ALL accounts: null ip_address_hash
	// + metadata.  The accounts hard-delete (step4e) then fires the SET NULL
	// cascade on consent_log.account_id (migration #885).
	if err := deps.DB.AnonymizeConsentLog(ctx, accountIDs); err != nil {
		return reportStepErr("step4b", "anonymize consent_log", err)
	}

	// 4c — waitlist_entries PII: DELETE WHERE email = <captured> (CITEXT match).
	if err := deps.DB.DeleteWaitlistEntry(ctx, capturedEmail); err != nil {
		return reportStepErr("step4c", "waitlist delete", err)
	}

	// 4d — Hard-delete users row; cascades to api_keys via users(id) FK.
	// accounts rows are deleted per-account in step 4e below (accounts.user_id
	// references users.id, so the users delete must NOT cascade accounts — it
	// doesn't: accounts has ON DELETE CASCADE from users, so deleting users
	// also deletes all accounts rows.  We still run 4e + 4-explicit per account
	// for the BIGINT-child data before the user row is deleted.
	//
	// Ordering: delete account-scoped BIGINT children first (4e/4-explicit),
	// THEN delete the users row (4d).  This avoids FK violations on tables that
	// reference accounts(id) but cascade from accounts, not users.
	//
	// Account-scoped steps (4e + 4-explicit) loop over ALL accountIDs.
	for _, accountID := range accountIDs {
		// 4e — Hard-delete accounts row; fires ON DELETE CASCADE on BIGINT FK
		// tables and SET NULL on consent_log.  game_plays has no FK CASCADE
		// today (000120 deferred it); matches/player_stats/rank_history/
		// collection_history gained FKs in 000119 but were absent at the P1
		// incident.  Step 4-explicit covers those gaps defense-in-depth.
		if err := deps.DB.HardDeleteAccount(ctx, accountID); err != nil {
			return reportStepErr("step4e", fmt.Sprintf("hard-delete account %d", accountID), err)
		}

		// 4-explicit — Defense-in-depth explicit DELETE of BIGINT-keyed tables
		// that are cascade-unreachable at some schema versions (#1257).
		//
		// Runs AFTER HardDeleteAccount (4e) so FK cascades have already fired;
		// the explicit DELETEs are idempotent no-ops when cascade covered rows.
		// Tables: matches, player_stats, rank_history, collection_history,
		// game_plays (no FK today), inventory_history (BIGINT FK path on
		// fresh-init 000054; data_type-gated in the repo method).
		if err := deps.DB.DeleteExplicitBigintRows(ctx, accountID); err != nil {
			return reportStepErr("step4explicit", fmt.Sprintf("explicit bigint-keyed delete account %d", accountID), err)
		}
	}

	// Hard-delete the users row AFTER all account-scoped deletes (#1333
	// ordering: children before parent).
	if err := deps.DB.HardDeleteUser(ctx, userID); err != nil {
		return reportStepErr("step4d", "hard-delete user", err)
	}

	// -----------------------------------------------------------------------
	// Step 4-sweep — Residual sweep.
	//
	// Queries information_schema for every public base table with an account_id
	// or user_id column and asserts zero rows remain for ALL erased accounts and
	// clientIDs.  Excluded (retained by design, Ray ruling #1257):
	//   deletion_audit_log, restriction_audit_log, dsr_access_log,
	//   rectification_audit_log.
	//
	// On sweep failure: cascade halts with a descriptive error listing ALL
	// offending tables; completed_at stays NULL so the AC7 re-trigger runbook
	// can identify and re-run the job.  Step 5 (Clerk) and Step 8 (complete)
	// are NOT called — no irreversible external action on residual state.
	// -----------------------------------------------------------------------
	if err := deps.DB.AssertZeroResiduals(ctx, accountIDs, clientIDs); err != nil {
		return reportStepErr("step4sweep", "residual sweep", err)
	}

	// -----------------------------------------------------------------------
	// Step 5 — Clerk Admin API delete.
	//
	// MUST run after Steps 1–4-sweep are complete.  Deleting Clerk before Step 2
	// would destroy the hash mapping used by PostHog.
	// -----------------------------------------------------------------------
	if err := deps.Clerk.DeleteUser(ctx, clerkUserID); err != nil {
		return reportStepErr("step5", "clerk delete", err)
	}

	// -----------------------------------------------------------------------
	// Step 6 — Mailchimp delete-permanent.
	//
	// POST /3.0/lists/{id}/members/{md5(lower(email))}/actions/delete-permanent
	// GDPR-deletes the contact and adds a suppression hash.  This is NOT
	// Unsubscribe — Unsubscribe retains the contact record (Q2 ruling).
	// -----------------------------------------------------------------------
	if err := deps.Mailchimp.DeletePermanent(ctx, capturedEmail); err != nil {
		return reportStepErr("step6", "mailchimp delete-permanent", err)
	}

	// Step 7 — Cache invalidation: no-op (no Redis in current architecture).
	// Daemon cache is invalidated via the device.revoked SSE event on the
	// existing daemon revoke path.

	// -----------------------------------------------------------------------
	// Step 8 — Mark deletion_audit_log.completed_at = NOW().
	//
	// Only reached when all DB deletes + sweep succeed.  completed_at = NULL
	// for any aborted cascade is the AC7 re-trigger signal.
	// -----------------------------------------------------------------------
	if err := deps.DB.RecordJobComplete(ctx, jobID); err != nil {
		return reportStepErr("step8", "record job complete", err)
	}

	// -----------------------------------------------------------------------
	// Post-cascade: send deletion-completion email to the user (ADR-076 /
	// hollowmark-tickets#1172).
	//
	// This fires AFTER completed_at is written (Step 8) — the cascade is done.
	// An email-send failure is logged but does NOT return an error: the cascade
	// is complete; a send failure must not leave completed_at NULL or re-trigger
	// the cascade.  The Sentry alert (PR-A) remains the authoritative failure-
	// visibility channel; the email is the user-facing notification.
	// -----------------------------------------------------------------------
	if deps.Email != nil {
		sendEmailNonFatal(deps.Email.SendDeletionComplete, capturedEmail)
	}

	return nil
}
