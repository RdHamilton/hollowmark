package analytics_test

import (
	"context"
	"errors"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/analytics"
	posthog "github.com/posthog/posthog-go"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

// captureRecorder records Enqueue calls so tests can assert on them.
type captureRecorder struct {
	calls []posthog.Capture
	err   error // when non-nil, Enqueue returns this error
}

func (r *captureRecorder) Enqueue(msg posthog.Message) error {
	if r.err != nil {
		return r.err
	}
	if c, ok := msg.(posthog.Capture); ok {
		r.calls = append(r.calls, c)
	}
	return nil
}

// haltCheckerStub always returns the configured (halted, err) pair.
type haltCheckerStub struct {
	halted bool
	err    error
}

func (h *haltCheckerStub) IsHalted(_ context.Context, _ string) (bool, error) {
	return h.halted, h.err
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestCapture_NoopHaltChecker verifies that the default noopHaltChecker
// always returns false (not halted), causing every event to be forwarded.
func TestCapture_NoopHaltChecker(t *testing.T) {
	rec := &captureRecorder{}
	client := analytics.NewClient(rec, analytics.NewNoopHaltChecker())

	if err := client.Capture(context.Background(), "abc123", "page_viewed", map[string]any{"page": "home"}); err != nil {
		t.Fatalf("Capture returned unexpected error: %v", err)
	}

	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 Enqueue call, got %d", len(rec.calls))
	}
	if rec.calls[0].Event != "page_viewed" {
		t.Errorf("expected event %q, got %q", "page_viewed", rec.calls[0].Event)
	}
	if rec.calls[0].DistinctId != "abc123" {
		t.Errorf("expected DistinctId %q, got %q", "abc123", rec.calls[0].DistinctId)
	}
}

// TestCapture_HaltedAccount verifies that when HaltChecker returns true the
// event is dropped silently (no Enqueue call, nil error returned).
func TestCapture_HaltedAccount(t *testing.T) {
	rec := &captureRecorder{}
	halted := &haltCheckerStub{halted: true}
	client := analytics.NewClient(rec, halted)

	if err := client.Capture(context.Background(), "abc123", "page_viewed", nil); err != nil {
		t.Fatalf("Capture returned unexpected error: %v", err)
	}

	if len(rec.calls) != 0 {
		t.Errorf("expected 0 Enqueue calls for halted account, got %d", len(rec.calls))
	}
}

// TestCapture_HaltCheckerError verifies that when HaltChecker returns an error
// the wrapper fails closed: the error is returned and no Enqueue call is made.
func TestCapture_HaltCheckerError(t *testing.T) {
	rec := &captureRecorder{}
	sentinel := errors.New("db timeout")
	errChecker := &haltCheckerStub{err: sentinel}
	client := analytics.NewClient(rec, errChecker)

	err := client.Capture(context.Background(), "abc123", "page_viewed", nil)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	if len(rec.calls) != 0 {
		t.Errorf("expected 0 Enqueue calls when halt-check errors, got %d", len(rec.calls))
	}
}

// TestCapture_OperationalFlag_BypassesHaltCheck verifies that CaptureOptions
// with Operational:true causes the wrapper to skip the halt check entirely —
// even when the checker would return true — and forward the event.
func TestCapture_OperationalFlag_BypassesHaltCheck(t *testing.T) {
	rec := &captureRecorder{}
	halted := &haltCheckerStub{halted: true} // would normally drop the event
	client := analytics.NewClient(rec, halted)

	if err := client.Capture(
		context.Background(),
		"abc123",
		"daemon_dispatch_degraded",
		map[string]any{"degraded_reason": "dispatch_error"},
		analytics.CaptureOptions{Operational: true},
	); err != nil {
		t.Fatalf("Capture returned unexpected error: %v", err)
	}

	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 Enqueue call for operational event (bypass halt), got %d", len(rec.calls))
	}
	if rec.calls[0].Event != "daemon_dispatch_degraded" {
		t.Errorf("expected event %q, got %q", "daemon_dispatch_degraded", rec.calls[0].Event)
	}
}

// TestCapture_OperationalFlag_WithHaltCheckerError verifies that CaptureOptions
// with Operational:true forwards the event even when the HaltChecker would
// error — the error is never consulted for operational events.
func TestCapture_OperationalFlag_WithHaltCheckerError(t *testing.T) {
	rec := &captureRecorder{}
	errChecker := &haltCheckerStub{err: errors.New("db timeout")}
	client := analytics.NewClient(rec, errChecker)

	if err := client.Capture(
		context.Background(),
		"abc123",
		"projection.dead_letter",
		nil,
		analytics.CaptureOptions{Operational: true},
	); err != nil {
		t.Fatalf("Capture returned unexpected error: %v", err)
	}

	if len(rec.calls) != 1 {
		t.Errorf("expected 1 Enqueue call, got %d", len(rec.calls))
	}
}

// TestCapture_PropertiesPassedThrough verifies that the properties map is
// forwarded verbatim to the PostHog Capture struct.
func TestCapture_PropertiesPassedThrough(t *testing.T) {
	rec := &captureRecorder{}
	client := analytics.NewClient(rec, analytics.NewNoopHaltChecker())

	props := map[string]any{
		"degraded_reason": "log_format_drift",
		"count":           uint32(5),
	}
	if err := client.Capture(context.Background(), "hash1", "daemon_dispatch_degraded", props); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rec.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(rec.calls))
	}
	got := rec.calls[0].Properties
	if got["degraded_reason"] != "log_format_drift" {
		t.Errorf("degraded_reason property mismatch: %v", got)
	}
}
