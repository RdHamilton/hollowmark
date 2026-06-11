// Package reconciler provides a background worker that retries failed Mailchimp
// waitlist subscriptions. It is the counterpart to the non-fatal handler pattern
// introduced in vault-mtg-tickets#121: the handler stores 'failed' rows durably
// and this reconciler retries them on a 15-minute cadence.
package reconciler

import (
	"context"
	"log"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

const (
	// reconcilerBatchSize is the maximum number of failed entries processed per run.
	reconcilerBatchSize = 100
	// reconcilerTickInterval is how often the reconciler runs.
	reconcilerTickInterval = 15 * time.Minute
	// reconcilerMaxAttempts is the maximum number of reconciler retries before
	// a row is moved to 'terminal'. Manual recovery requires resetting BOTH
	// mailchimp_status='failed' AND mailchimp_attempts=0.
	reconcilerMaxAttempts = 10
)

// waitlistStore is the subset of WaitlistRepository the reconciler needs.
// Using an interface allows unit tests to inject stubs without a real DB.
type waitlistStore interface {
	ListFailedWaitlistEntries(ctx context.Context, limit int) ([]repository.FailedWaitlistEntry, error)
	MarkWaitlistSubscribed(ctx context.Context, id string) error
	IncrementAttemptsAndMaybeTerminate(ctx context.Context, id string, maxAttempts int) error
}

// mailchimpClient is the subset of MailchimpHTTPClient the reconciler needs.
type mailchimpClient interface {
	AddMember(ctx context.Context, email string) error
}

// RunStats holds per-run metrics returned by RunOnce.
type RunStats struct {
	Total      int
	Subscribed int
	Failed     int
}

// MailchimpReconciler retries failed Mailchimp waitlist subscriptions.
type MailchimpReconciler struct {
	store waitlistStore
	mc    mailchimpClient
}

// NewMailchimpReconciler constructs a MailchimpReconciler wired with the
// provided store and Mailchimp client.
func NewMailchimpReconciler(store waitlistStore, mc mailchimpClient) *MailchimpReconciler {
	return &MailchimpReconciler{store: store, mc: mc}
}

// Run starts the reconciler loop. It performs an immediate pass on startup,
// then ticks every reconcilerTickInterval. The loop exits when ctx is cancelled.
// Mirrors the projection worker pattern (internal/projection/worker.go Run).
func (r *MailchimpReconciler) Run(ctx context.Context) {
	log.Println("[reconciler] mailchimp reconciler started")

	r.RunOnce(ctx)

	ticker := time.NewTicker(reconcilerTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[reconciler] mailchimp reconciler stopped")
			return
		case <-ticker.C:
			r.RunOnce(ctx)
		}
	}
}

// RunOnce processes one batch of failed waitlist entries. It is exported to
// allow targeted invocation in integration tests.
// Returns RunStats summarising this run's outcomes.
func (r *MailchimpReconciler) RunOnce(ctx context.Context) RunStats {
	start := time.Now()

	entries, err := r.store.ListFailedWaitlistEntries(ctx, reconcilerBatchSize)
	if err != nil {
		log.Printf("[reconciler] ListFailedWaitlistEntries: %v", err)
		return RunStats{}
	}

	var stats RunStats
	stats.Total = len(entries)

	for _, e := range entries {
		if mcErr := r.mc.AddMember(ctx, e.Email); mcErr != nil {
			log.Printf("[reconciler] AddMember id=%s: %v", e.ID, mcErr)
			stats.Failed++
			if dbErr := r.store.IncrementAttemptsAndMaybeTerminate(ctx, e.ID, reconcilerMaxAttempts); dbErr != nil {
				log.Printf("[reconciler] IncrementAttemptsAndMaybeTerminate id=%s: %v", e.ID, dbErr)
			}
			continue
		}

		// AddMember succeeded — flip the row to 'subscribed'.
		if dbErr := r.store.MarkWaitlistSubscribed(ctx, e.ID); dbErr != nil {
			// Post-success DB write is best-effort; the Mailchimp side is already
			// 'subscribed'. Log and continue — the status mismatch will be visible
			// via Sentry. The next reconciler run will retry via ListFailedWaitlistEntries
			// (row is still 'failed') and hit the Mailchimp PUT upsert (idempotent).
			log.Printf("[reconciler] MarkWaitlistSubscribed id=%s: %v", e.ID, dbErr)
		}
		stats.Subscribed++
	}

	if stats.Total > 0 {
		log.Printf(
			"[reconciler] runOnce completed total=%d subscribed=%d failed=%d duration_ms=%d",
			stats.Total, stats.Subscribed, stats.Failed,
			time.Since(start).Milliseconds(),
		)
	}

	return stats
}
