package analytics

import (
	"context"

	posthog "github.com/posthog/posthog-go"
)

// NoopEnqueuer is a PostHogEnqueuer that silently discards all messages.
// Use this when PostHog is not configured (e.g. POSTHOG_API_KEY is empty or
// in contexts where injection of the real posthog.Client is not practical).
type NoopEnqueuer struct{}

// Enqueue discards the message and returns nil.
func (NoopEnqueuer) Enqueue(posthog.Message) error { return nil }

// noopHaltChecker is the default HaltChecker used until ticket #890 provides
// a real DB-backed implementation.  It always returns (false, nil), making
// every Capture call behaviour-preserving with the pre-seam direct Enqueue calls.
//
// Use NewNoopHaltChecker() — the type is unexported by design so callers cannot
// depend on its concrete type; they must program to the HaltChecker interface.
type noopHaltChecker struct{}

// NewNoopHaltChecker returns a HaltChecker that always reports not-halted.
// Wire this until ticket #890 swaps in the real implementation.
func NewNoopHaltChecker() HaltChecker {
	return noopHaltChecker{}
}

func (noopHaltChecker) IsHalted(_ context.Context, _ string) (bool, error) {
	return false, nil
}
