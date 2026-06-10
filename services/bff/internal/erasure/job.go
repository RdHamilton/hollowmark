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
//	Step 0  — Capture email + all MTGA client_id strings into memory (pre-job).
//	Step 1  — Soft-delete gate: UPDATE users SET deleted_at=NOW() (sync, before 202).
//	Step 2  — PostHog bulk-delete: recompute hash → DELETE person.
//	Step 4a — Explicit DELETE of TEXT-keyed tables using captured client_ids.
//	Step 4b — Anonymize consent_log in-place (null ip_address_hash + metadata).
//	Step 4c — DELETE waitlist_entries WHERE email = <captured>.
//	Step 4d — Hard-delete users row (cascades to api_keys).
//	Step 4e — Hard-delete accounts row (fires ON DELETE CASCADE + SET NULL on consent_log).
//	Step 5  — Clerk Admin API DELETE /v1/users/{id} — MUST follow Steps 1–4e.
//	Step 6  — Mailchimp delete-permanent (GDPR + suppression).
//	Step 8  — Mark deletion_audit_log.completed_at = NOW().
//
// The step numbering matches ADR-056 §Decision; Steps 3 and 7 are no-ops in
// the current implementation (ML data is anonymous; no Redis cache to clear).
package erasure

import (
	"context"
	"fmt"
	"strconv"

	"github.com/RdHamilton/hollowmark/services/bff/internal/identityhash"
)

// PreJobDataSource is satisfied by the deletion repository's CapturePreJobData
// method.  It captures the email and all MTGA client_id strings associated
// with the account before any row is deleted.
type PreJobDataSource interface {
	CapturePreJobData(ctx context.Context, userID, accountID int64) (email string, clientIDs []string, err error)
}

// UserSoftDeleter marks users.deleted_at synchronously — before the 202 is
// returned — to block new writes from concurrent requests.
type UserSoftDeleter interface {
	SoftDeleteUser(ctx context.Context, userID int64) error
}

// TextKeyedDeleter deletes all rows in the five no-FK TEXT-keyed tables that
// reference MTGA client_id strings:
//   - daemon_events.account_id TEXT
//   - daemon_api_keys.account_id TEXT
//   - quest_session_tracking.account_id TEXT
//   - user_play_patterns.account_id TEXT
//   - projection_errors.account_id TEXT
type TextKeyedDeleter interface {
	DeleteTextKeyedRows(ctx context.Context, clientIDs []string) error
}

// ConsentLogAnonymizer anonymizes consent_log rows in-place by setting
// ip_address_hash=NULL and metadata=NULL for the given account_id.
// The ON DELETE SET NULL cascade on consent_log.account_id (migration #885)
// then fires when the accounts row is deleted in Step 4e.
type ConsentLogAnonymizer interface {
	AnonymizeConsentLog(ctx context.Context, accountID int64) error
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
//   - ON DELETE CASCADE on all BIGINT FK user-keyed tables (collection, matches,
//     decks, drafts, etc. — 25+ tables per ADR-056 FM-3 enumeration).
//   - ON DELETE SET NULL on consent_log.account_id (migration #885).
type AccountHardDeleter interface {
	HardDeleteAccount(ctx context.Context, accountID int64) error
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
// MUST run after Steps 1–4e — deleting Clerk first destroys the hash mapping
// needed for the PostHog delete.
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
}

// RunErasureCascade executes the full 9-step GDPR Art.17 erasure cascade for a
// single account.  It is designed to be called in a background goroutine from
// the BFF root context.
//
// Parameters:
//   - ctx: BFF root context (NOT the HTTP request context).
//   - jobID: UUID of the deletion_audit_log row for this job.
//   - clerkUserID: Clerk user id (used only for Step 5 Clerk API delete).
//   - userID: internal users.id (BIGINT).
//   - accountID: internal accounts.id (BIGINT).
//   - deps: injected collaborators.
//
// On any step failure the cascade halts and returns the error; the
// deletion_audit_log row retains a NULL completed_at so the AC7 runbook can
// identify and re-trigger incomplete jobs by job_id.
func RunErasureCascade(ctx context.Context, jobID, clerkUserID string, userID, accountID int64, deps Deps) error {
	// -----------------------------------------------------------------------
	// Step 0 — Capture email + all client_id strings BEFORE any deletion.
	//
	// The TEXT-keyed tables (daemon_events, daemon_api_keys, etc.) are keyed by
	// the MTGA client_id string, not by the internal accounts.id BIGINT.  Once
	// the accounts row is deleted in Step 4e, these strings cannot be recovered
	// from the DB.  We capture them here to use in Step 4a.
	// -----------------------------------------------------------------------
	email, clientIDs, err := deps.DB.CapturePreJobData(ctx, userID, accountID)
	if err != nil {
		return fmt.Errorf("erasure step0 (capture pre-job data): %w", err)
	}

	// -----------------------------------------------------------------------
	// Step 1 — Soft-delete gate.
	//
	// Sets users.deleted_at = NOW().  This is called synchronously before the
	// 202 response; downstream steps run asynchronously.  The soft-delete
	// blocks new daemon ingest writes from hitting the tenant.
	// -----------------------------------------------------------------------
	if err := deps.DB.SoftDeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("erasure step1 (soft-delete user): %w", err)
	}

	// -----------------------------------------------------------------------
	// Step 2 — PostHog bulk-delete.
	//
	// The distinct_id is SHA-256(accountID string)[:16] — the same hash used
	// by all PostHog emits in the BFF (identityhash.HashAccountID).  We
	// recompute it here from the accountID, which must still be live in Clerk
	// at this point (Clerk delete is Step 5, AFTER this step).
	// -----------------------------------------------------------------------
	distinctID := identityhash.HashAccountID(strconv.FormatInt(accountID, 10))
	if err := deps.PostHog.DeletePerson(ctx, distinctID); err != nil {
		return fmt.Errorf("erasure step2 (posthog bulk-delete): %w", err)
	}

	// Step 3 — ML training data: no-op.  ML data is anonymous (D7a, ADR-058
	// Option A accepted).  No personal data in the ML store; no delete needed.

	// -----------------------------------------------------------------------
	// Step 4 — PostgreSQL explicit deletes (in FK-dependency order).
	// -----------------------------------------------------------------------

	// 4a — TEXT-keyed tables: delete BEFORE accounts row (ordering mandatory).
	//
	// These five tables use MTGA client_id strings as account_id (no FK to
	// accounts), so they are unreachable by cascade.  They must be explicitly
	// deleted using the client_ids captured in Step 0.
	if err := deps.DB.DeleteTextKeyedRows(ctx, clientIDs); err != nil {
		return fmt.Errorf("erasure step4a (text-keyed delete): %w", err)
	}

	// 4b — Anonymize consent_log in-place: null ip_address_hash + metadata.
	// The accounts hard-delete (step4e) then fires the SET NULL cascade on
	// consent_log.account_id (migration #885, coupled with #891).
	if err := deps.DB.AnonymizeConsentLog(ctx, accountID); err != nil {
		return fmt.Errorf("erasure step4b (anonymize consent_log): %w", err)
	}

	// 4c — waitlist_entries PII: DELETE WHERE email = <captured> (CITEXT match).
	if err := deps.DB.DeleteWaitlistEntry(ctx, email); err != nil {
		return fmt.Errorf("erasure step4c (waitlist delete): %w", err)
	}

	// 4d — Hard-delete users row; cascades to api_keys via users(id) FK.
	if err := deps.DB.HardDeleteUser(ctx, userID); err != nil {
		return fmt.Errorf("erasure step4d (hard-delete user): %w", err)
	}

	// 4e — Hard-delete accounts row; fires ON DELETE CASCADE on all BIGINT FK
	// user-keyed tables (25+ tables, ADR-056 FM-3) and SET NULL on consent_log.
	if err := deps.DB.HardDeleteAccount(ctx, accountID); err != nil {
		return fmt.Errorf("erasure step4e (hard-delete account): %w", err)
	}

	// -----------------------------------------------------------------------
	// Step 5 — Clerk Admin API delete.
	//
	// MUST run after Steps 1–4e are complete.  Deleting Clerk before Step 2
	// would destroy the hash mapping used by PostHog.
	// -----------------------------------------------------------------------
	if err := deps.Clerk.DeleteUser(ctx, clerkUserID); err != nil {
		return fmt.Errorf("erasure step5 (clerk delete): %w", err)
	}

	// -----------------------------------------------------------------------
	// Step 6 — Mailchimp delete-permanent.
	//
	// POST /3.0/lists/{id}/members/{md5(lower(email))}/actions/delete-permanent
	// GDPR-deletes the contact and adds a suppression hash.  This is NOT
	// Unsubscribe — Unsubscribe retains the contact record (Q2 ruling).
	// -----------------------------------------------------------------------
	if err := deps.Mailchimp.DeletePermanent(ctx, email); err != nil {
		return fmt.Errorf("erasure step6 (mailchimp delete-permanent): %w", err)
	}

	// Step 7 — Cache invalidation: no-op (no Redis in current architecture).
	// Daemon cache is invalidated via the device.revoked SSE event on the
	// existing daemon revoke path.

	// -----------------------------------------------------------------------
	// Step 8 — Mark deletion_audit_log.completed_at = NOW().
	// -----------------------------------------------------------------------
	if err := deps.DB.RecordJobComplete(ctx, jobID); err != nil {
		return fmt.Errorf("erasure step8 (record job complete): %w", err)
	}

	return nil
}
