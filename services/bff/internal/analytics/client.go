// Package analytics provides the canonical PostHog emission seam for the BFF.
//
// All PostHog capture calls MUST go through Client.Capture — direct use of
// posthog.Client.Enqueue outside this package is forbidden by the
// grep-zero-direct-calls CI lint gate (except in test files and within this
// package itself).
//
// Sequencing: until ticket #890 merges and provides a real HaltChecker
// implementation, callers should wire analytics.NewNoopHaltChecker().  The
// noop always returns (false, nil) — forwarding every event — which is
// behaviour-preserving with the pre-seam code.
package analytics

import (
	"context"

	posthog "github.com/posthog/posthog-go"
)

// PostHogEnqueuer is the minimal posthog.Client surface required by the seam.
// Satisfied by the real *posthog.Client and by test doubles.
type PostHogEnqueuer interface {
	Enqueue(posthog.Message) error
}

// HaltChecker reports whether analytics emission should be halted for a given
// account.  It is keyed on the pre-computed account_id_hash (SHA-256 hex[:16])
// so no reverse lookup is ever needed.
//
// Implementations MUST be:
//   - Safe for concurrent use (called in hot handler paths).
//   - Fail-closed on error — the wrapper never forwards when IsHalted errors.
//
// The noop implementation (NewNoopHaltChecker) always returns (false, nil) and
// is the correct choice until ticket #890 provides the real DB-backed impl.
type HaltChecker interface {
	IsHalted(ctx context.Context, accountIDHash string) (bool, error)
}

// CaptureOptions carries per-call overrides.  Pass as a variadic trailing
// argument to Capture — only the first element is read.
type CaptureOptions struct {
	// Operational marks an event as operational telemetry (e.g.
	// projection.dead_letter, projection.missing_field, waitlist signup).
	// When true, the HaltChecker is bypassed and the event is always forwarded.
	// Use ONLY for system observability events that must fire regardless of a
	// user's Art.18 processing restriction — this is a GDPR §6(1)(f)
	// legitimate-interest carve-out.  Never use it for feature-use or funnel
	// events that should respect the halt flag.
	Operational bool
}

// Client is the analytics emission seam.  Construct via NewClient.
type Client struct {
	postHog     PostHogEnqueuer
	haltChecker HaltChecker
}

// NewClient constructs a Client with the given PostHog enqueuer and halt checker.
// Pass analytics.NewNoopHaltChecker() for haltChecker until ticket #890 lands.
func NewClient(ph PostHogEnqueuer, hc HaltChecker) *Client {
	return &Client{postHog: ph, haltChecker: hc}
}

// Capture emits a PostHog event through the analytics seam.
//
// Flow:
//  1. If CaptureOptions.Operational is true → skip halt check, forward directly.
//  2. Call haltChecker.IsHalted(ctx, accountIDHash).
//     On error → fail closed (return error, do NOT forward).
//  3. If halted → drop silently, return nil.
//  4. Forward to postHog.Enqueue.
//
// accountIDHash must be the pre-computed SHA-256 hex[:16] of the internal
// account ID — never a raw Clerk user_id or email address.
func (c *Client) Capture(
	ctx context.Context,
	accountIDHash string,
	event string,
	props map[string]any,
	opts ...CaptureOptions,
) error {
	// Step 1 — operational carve-out bypasses the halt check.
	if len(opts) > 0 && opts[0].Operational {
		return c.enqueue(accountIDHash, event, props)
	}

	// Step 2 — halt check (fail-closed on error).
	halted, err := c.haltChecker.IsHalted(ctx, accountIDHash)
	if err != nil {
		return err
	}

	// Step 3 — drop silently for halted accounts.
	if halted {
		return nil
	}

	// Step 4 — forward.
	return c.enqueue(accountIDHash, event, props)
}

// enqueue builds the posthog.Capture message and calls Enqueue.
func (c *Client) enqueue(accountIDHash, event string, props map[string]any) error {
	ph := posthog.NewProperties()
	for k, v := range props {
		ph = ph.Set(k, v)
	}
	return c.postHog.Enqueue(posthog.Capture{
		DistinctId: accountIDHash,
		Event:      event,
		Properties: ph,
	})
}
