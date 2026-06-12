package main

// Tests for SH-5: structured shutdown-reason logging + daemon.shutdown telemetry.
// AC1: every exit path emits "[daemon] stopped reason=<reason>" to the logger.
// AC2: a daemon.shutdown Sentry breadcrumb is captured carrying the reason field.
// AC3: run_error reason includes the error string (truncated to 200 chars).
// AC4: no unhashed PII leaks into breadcrumb data.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureShutdownBreadcrumbs initialises a Sentry test hub (no DSN — in-process
// transport) and returns the breadcrumbs captured during the test. It sets up a
// local hub so test breadcrumbs don't bleed into other tests.
//
// We can't easily intercept sentry.AddBreadcrumb without a real client, so we
// prime a before-breadcrumb hook via a custom transport that captures events.
// For breadcrumb testing we verify the breadcrumb was added to the scope by
// capturing a synthetic event with CaptureEvent and inspecting its breadcrumbs.
func captureShutdownBreadcrumbs(t *testing.T, fn func()) []*sentry.Breadcrumb {
	t.Helper()

	var captured []*sentry.Breadcrumb

	transport := &testTransport{
		onSend: func(e *sentry.Event) {
			captured = append(captured, e.Breadcrumbs...)
		},
	}

	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "", // no-DSN: suppress real network
		Transport: transport,
		BeforeBreadcrumb: func(b *sentry.Breadcrumb, hint *sentry.BreadcrumbHint) *sentry.Breadcrumb {
			// Capture every breadcrumb as it is added, so we can inspect them
			// independently of whether a Sentry event is flushed.
			captured = append(captured, b)
			return b
		},
	})
	require.NoError(t, err)

	hub := sentry.NewHub(client, sentry.NewScope())
	sentry.CurrentHub().BindClient(client) // bind to global hub for the test

	t.Cleanup(func() {
		// Restore a blank client after the test so other tests are unaffected.
		blank, _ := sentry.NewClient(sentry.ClientOptions{Dsn: ""})
		sentry.CurrentHub().BindClient(blank)
		_ = hub
	})

	fn()
	return captured
}

// testTransport implements sentry.Transport for tests — captures events sent
// to Sentry without actually transmitting them.
type testTransport struct {
	onSend func(*sentry.Event)
}

func (tt *testTransport) Configure(_ sentry.ClientOptions)        {}
func (tt *testTransport) Close()                                  {}
func (tt *testTransport) Flush(_ time.Duration) bool              { return true }
func (tt *testTransport) FlushWithContext(_ context.Context) bool { return true }
func (tt *testTransport) SendEvent(event *sentry.Event) {
	if tt.onSend != nil {
		tt.onSend(event)
	}
}

// ---------------------------------------------------------------------------
// AC1 — structured log line
// ---------------------------------------------------------------------------

// TestLogShutdown_SIGTERMEmitsStructuredLogLine verifies that logShutdown with
// ReasonSIGTERM writes "[daemon] stopped reason=sigterm" to the logger.
func TestLogShutdown_SIGTERMEmitsStructuredLogLine(t *testing.T) {
	var buf bytes.Buffer
	l := log.New(&buf, "", 0)

	logShutdown(l, ReasonSIGTERM, nil)

	got := buf.String()
	assert.Contains(t, got, "[daemon] stopped")
	assert.Contains(t, got, "reason=sigterm")
}

// TestLogShutdown_TrayQuitEmitsStructuredLogLine verifies that logShutdown with
// ReasonTrayQuit writes "[daemon] stopped reason=tray_quit" to the logger.
func TestLogShutdown_TrayQuitEmitsStructuredLogLine(t *testing.T) {
	var buf bytes.Buffer
	l := log.New(&buf, "", 0)

	logShutdown(l, ReasonTrayQuit, nil)

	got := buf.String()
	assert.Contains(t, got, "[daemon] stopped")
	assert.Contains(t, got, "reason=tray_quit")
}

// TestLogShutdown_RunErrorEmitsStructuredLogLine verifies that logShutdown with
// a non-nil error writes "reason=run_error:<err_summary>" to the logger (AC3).
func TestLogShutdown_RunErrorEmitsStructuredLogLine(t *testing.T) {
	var buf bytes.Buffer
	l := log.New(&buf, "", 0)

	logShutdown(l, ReasonRunError, errors.New("context deadline exceeded"))

	got := buf.String()
	assert.Contains(t, got, "[daemon] stopped")
	assert.Contains(t, got, "reason=run_error:")
	assert.Contains(t, got, "context deadline exceeded")
}

// TestLogShutdown_RunErrorTruncatesLongErrors verifies that error strings longer
// than 200 characters are truncated in the reason field (AC3).
func TestLogShutdown_RunErrorTruncatesLongErrors(t *testing.T) {
	var buf bytes.Buffer
	l := log.New(&buf, "", 0)

	longErr := errors.New(strings.Repeat("x", 300))
	logShutdown(l, ReasonRunError, longErr)

	got := buf.String()
	require.Contains(t, got, "reason=run_error:")

	// Extract the reason field value and verify it's within the 200-char limit.
	// The log line format is "[daemon] stopped reason=run_error:<text>"
	reasonIdx := strings.Index(got, "reason=run_error:")
	require.NotEqual(t, -1, reasonIdx, "reason= field must be present")
	reasonVal := strings.TrimPrefix(got[reasonIdx:], "reason=run_error:")
	// Strip trailing newline/whitespace.
	reasonVal = strings.TrimRight(reasonVal, "\n\r ")
	assert.LessOrEqual(t, len(reasonVal), 200,
		"run_error reason value must be truncated to 200 chars")
}

// TestLogShutdown_RunErrorWithNilErrUsesRunErrorReason verifies that passing
// ReasonRunError with a nil error produces a safe log line without panicking.
func TestLogShutdown_RunErrorWithNilErrUsesRunErrorReason(t *testing.T) {
	var buf bytes.Buffer
	l := log.New(&buf, "", 0)

	// Should not panic even when err is nil.
	assert.NotPanics(t, func() {
		logShutdown(l, ReasonRunError, nil)
	})

	got := buf.String()
	assert.Contains(t, got, "[daemon] stopped")
	assert.Contains(t, got, "reason=run_error")
}

// ---------------------------------------------------------------------------
// AC2 — Sentry breadcrumb
// ---------------------------------------------------------------------------

// TestLogShutdown_SIGTERMCapturesBreadcrumbWithReason verifies that logShutdown
// with ReasonSIGTERM adds a Sentry breadcrumb whose message contains "sigterm".
func TestLogShutdown_SIGTERMCapturesBreadcrumbWithReason(t *testing.T) {
	var buf bytes.Buffer
	l := log.New(&buf, "", 0)

	breadcrumbs := captureShutdownBreadcrumbs(t, func() {
		logShutdown(l, ReasonSIGTERM, nil)
	})

	require.NotEmpty(t, breadcrumbs, "at least one breadcrumb must be captured on shutdown")
	var found bool
	for _, b := range breadcrumbs {
		if strings.Contains(b.Message, "sigterm") || strings.Contains(b.Message, "daemon.shutdown") {
			found = true
			break
		}
	}
	assert.True(t, found, "a breadcrumb with 'sigterm' or 'daemon.shutdown' must be present; got: %v", breadcrumbs)
}

// TestLogShutdown_TrayQuitBreadcrumbLevelIsInfo verifies that a graceful
// tray_quit shutdown emits a breadcrumb with LevelInfo (not LevelError).
func TestLogShutdown_TrayQuitBreadcrumbLevelIsInfo(t *testing.T) {
	var buf bytes.Buffer
	l := log.New(&buf, "", 0)

	breadcrumbs := captureShutdownBreadcrumbs(t, func() {
		logShutdown(l, ReasonTrayQuit, nil)
	})

	require.NotEmpty(t, breadcrumbs)
	var found bool
	for _, b := range breadcrumbs {
		if strings.Contains(b.Message, "tray_quit") || strings.Contains(b.Message, "daemon.shutdown") {
			assert.Equal(t, sentry.LevelInfo, b.Level,
				"tray_quit shutdown breadcrumb must have LevelInfo")
			found = true
			break
		}
	}
	assert.True(t, found, "a tray_quit or daemon.shutdown breadcrumb must be present")
}

// TestLogShutdown_RunErrorBreadcrumbLevelIsError verifies that a run_error
// shutdown emits a breadcrumb with LevelError (AC2: level=error for run_error).
func TestLogShutdown_RunErrorBreadcrumbLevelIsError(t *testing.T) {
	var buf bytes.Buffer
	l := log.New(&buf, "", 0)

	breadcrumbs := captureShutdownBreadcrumbs(t, func() {
		logShutdown(l, ReasonRunError, errors.New("service loop crashed"))
	})

	require.NotEmpty(t, breadcrumbs)
	var found bool
	for _, b := range breadcrumbs {
		if strings.Contains(b.Message, "run_error") || strings.Contains(b.Message, "daemon.shutdown") {
			assert.Equal(t, sentry.LevelError, b.Level,
				"run_error shutdown breadcrumb must have LevelError")
			found = true
			break
		}
	}
	assert.True(t, found, "a run_error or daemon.shutdown breadcrumb must be present")
}

// ---------------------------------------------------------------------------
// AC4 — PII safety
// ---------------------------------------------------------------------------

// TestLogShutdown_RunErrorDoesNotLeakAPIKey verifies that a raw API key that
// appears in an error message is not passed through to the log or breadcrumb
// data without scrubbing. Per PII rules: error strings are truncated but NOT
// actively scrubbed (scrubbing is out of scope per ticket); the test verifies
// that the reason value length cap (200 chars) is the only sanitisation applied
// and that the function does not add any unhashed account/user IDs to breadcrumb
// data beyond what the error string itself contains.
//
// What we specifically prohibit: attaching raw cfg.AccountID to breadcrumb Data
// without hashing. The breadcrumb Message may contain the truncated error string.
func TestLogShutdown_BreadcrumbDataDoesNotContainRawAccountID(t *testing.T) {
	var buf bytes.Buffer
	l := log.New(&buf, "", 0)

	const rawAccountID = "user_2abc123def456"
	// Simulate an error that somehow includes an account-like token.
	err := fmt.Errorf("service failed for account %s: connection refused", rawAccountID)

	breadcrumbs := captureShutdownBreadcrumbs(t, func() {
		logShutdown(l, ReasonRunError, err)
	})

	// The breadcrumb Data map must not contain a key "account_id" with the raw value.
	for _, b := range breadcrumbs {
		if rawID, ok := b.Data["account_id"]; ok {
			assert.NotEqual(t, rawAccountID, rawID,
				"raw account_id must not appear in breadcrumb Data — hash it or omit it")
		}
	}
}

// ---------------------------------------------------------------------------
// Reason string constants
// ---------------------------------------------------------------------------

// TestShutdownReasonConstants verifies the string representation of each
// shutdownReason value matches the ADR-083 SH-5 specification.
func TestShutdownReasonConstants(t *testing.T) {
	assert.Equal(t, shutdownReason("sigterm"), ReasonSIGTERM,
		"ReasonSIGTERM must have value 'sigterm'")
	assert.Equal(t, shutdownReason("tray_quit"), ReasonTrayQuit,
		"ReasonTrayQuit must have value 'tray_quit'")
	assert.Equal(t, shutdownReason("run_error"), ReasonRunError,
		"ReasonRunError must have value 'run_error'")
}
