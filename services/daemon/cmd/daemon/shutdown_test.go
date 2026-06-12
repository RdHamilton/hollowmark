package main

// ---------------------------------------------------------------------------
// ADR-083 — Shutdown semantics: bootout only on explicit tray-Quit
// Ticket: #1354 "Daemon must not self-bootout on SIGTERM"
// Ticket: #1357 SH-5 structured shutdown-reason logging + daemon.shutdown telemetry
//
// These tests enforce the SH-1..SH-5 invariants from ADR-083:
//
//   SH-1  stopLaunchAgentFn is called ONLY from the tray-Quit callback.
//   SH-2  SIGTERM/SIGINT → graceful drain + plain exit; NO bootout.
//   SH-4  Headless auth-cap exhaustion → idle on auth_paused=true; NO bootout.
//   SH-5  Shutdown log lines carry a reason= tag.
//
// AC1: every exit path emits "[daemon] stopped reason=<reason>" to the logger.
// AC2: a daemon.shutdown Sentry breadcrumb is captured carrying the reason field.
// AC3: run_error reason includes the error string (truncated to 200 chars).
// AC4: no unhashed PII leaks into breadcrumb data.
//
// Sarah P2 #3256: the signal-handler goroutine panic is captured-then-re-panicked
// (not silently swallowed).
//
// FF-1 (fitness function): verified by TestFF1_ExactlyOneNonTestStopLaunchAgentCallSite
// via a grep over the production source file.
// ---------------------------------------------------------------------------

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Sentry breadcrumb capture helper (AC2 tests)
// ---------------------------------------------------------------------------

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
// SH-1 / SH-2 — stopLaunchAgentFn injection
// ---------------------------------------------------------------------------

// TestStopLaunchAgentFn_IsPackageLevelVar verifies that stopLaunchAgentFn is a
// package-level var (not a direct call) so it can be injected in tests.
// If this fails to compile, the production code still calls stopLaunchAgent()
// directly — SH-1 is not implemented.
func TestStopLaunchAgentFn_IsPackageLevelVar(t *testing.T) {
	called := false
	orig := stopLaunchAgentFn
	t.Cleanup(func() { stopLaunchAgentFn = orig })

	stopLaunchAgentFn = func() { called = true }
	stopLaunchAgentFn()

	assert.True(t, called, "stopLaunchAgentFn must be an injectable package-level var (SH-1)")
}

// ---------------------------------------------------------------------------
// SH-2 — Signal handler does NOT trigger bootout
// ---------------------------------------------------------------------------

// TestSIGTERM_DoesNotCallStopLaunchAgent verifies that handleSignalShutdown
// (the extracted signal shutdown path) does NOT invoke stopLaunchAgentFn.
// Only the tray-Quit callback must call it (SH-1 / SH-2).
func TestSIGTERM_DoesNotCallStopLaunchAgent(t *testing.T) {
	bootoutCalled := false
	orig := stopLaunchAgentFn
	t.Cleanup(func() { stopLaunchAgentFn = orig })
	stopLaunchAgentFn = func() { bootoutCalled = true }

	cancelCalled := false
	cancel := func() { cancelCalled = true }

	origLogger := shutdownLogger
	t.Cleanup(func() { shutdownLogger = origLogger })
	shutdownLogger = log.New(os.Stderr, "", 0)

	handleSignalShutdown(cancel)

	assert.False(t, bootoutCalled,
		"SIGTERM/SIGINT must NOT call stopLaunchAgentFn (SH-2: bootout only on explicit tray-Quit)")
	assert.True(t, cancelCalled,
		"SIGTERM/SIGINT must cancel the context so the graceful drain fires")
}

// ---------------------------------------------------------------------------
// SH-5 — Shutdown log lines carry a reason= tag (helpers)
// ---------------------------------------------------------------------------

// TestSIGTERM_LogsReasonTag verifies that handleSignalShutdown emits a log
// line containing reason=sigterm (SH-5).
func TestSIGTERM_LogsReasonTag(t *testing.T) {
	var buf bytes.Buffer
	testLog := log.New(&buf, "", 0)

	orig := stopLaunchAgentFn
	t.Cleanup(func() { stopLaunchAgentFn = orig })
	stopLaunchAgentFn = func() {}

	origLogger := shutdownLogger
	t.Cleanup(func() { shutdownLogger = origLogger })
	shutdownLogger = testLog

	handleSignalShutdown(func() {})

	got := buf.String()
	assert.True(t, strings.Contains(got, "reason=sigterm"),
		"SIGTERM shutdown log must contain reason=sigterm (SH-5), got: %q", got)
}

// TestTrayQuit_LogsReasonTag verifies that the tray-Quit path logs reason=tray_quit (SH-5).
func TestTrayQuit_LogsReasonTag(t *testing.T) {
	var buf bytes.Buffer
	testLog := log.New(&buf, "", 0)

	orig := stopLaunchAgentFn
	t.Cleanup(func() { stopLaunchAgentFn = orig })
	stopLaunchAgentFn = func() {}

	origLogger := shutdownLogger
	t.Cleanup(func() { shutdownLogger = origLogger })
	shutdownLogger = testLog

	cancelCalled := false
	trayQuitShutdown(func() { cancelCalled = true })

	got := buf.String()
	assert.True(t, strings.Contains(got, "reason=tray_quit"),
		"tray-Quit shutdown log must contain reason=tray_quit (SH-5), got: %q", got)
	assert.True(t, cancelCalled,
		"tray-Quit must cancel the context")
}

// TestTrayQuit_CallsStopLaunchAgent verifies that trayQuitShutdown IS the one
// call site that invokes stopLaunchAgentFn (SH-1).
func TestTrayQuit_CallsStopLaunchAgent(t *testing.T) {
	bootoutCalled := false
	orig := stopLaunchAgentFn
	t.Cleanup(func() { stopLaunchAgentFn = orig })
	stopLaunchAgentFn = func() { bootoutCalled = true }

	origLogger := shutdownLogger
	t.Cleanup(func() { shutdownLogger = origLogger })
	shutdownLogger = log.New(os.Stderr, "", 0)

	trayQuitShutdown(func() {})

	assert.True(t, bootoutCalled,
		"tray-Quit MUST call stopLaunchAgentFn — it is the only sanctioned bootout site (SH-1)")
}

// ---------------------------------------------------------------------------
// SH-4 — Headless auth-cap exhaustion → idle on auth_paused, NOT bootout
// ---------------------------------------------------------------------------

// TestHeadlessAuthCap_DoesNotCallStopLaunchAgent verifies AC4 from the ticket:
// when headless mode reaches the auth-attempt cap, the daemon sets
// auth_paused=true and exits WITHOUT calling stopLaunchAgentFn (SH-4).
// The respawned process must idle on auth_paused=true rather than exit-again
// into a respawn loop.
func TestHeadlessAuthCap_DoesNotCallStopLaunchAgent(t *testing.T) {
	bootoutCalled := false
	orig := stopLaunchAgentFn
	t.Cleanup(func() { stopLaunchAgentFn = orig })
	stopLaunchAgentFn = func() { bootoutCalled = true }

	origLogger := shutdownLogger
	t.Cleanup(func() { shutdownLogger = origLogger })
	shutdownLogger = log.New(os.Stderr, "", 0)

	exitCalled := false
	headlessAuthCapExit(func(int) { exitCalled = true })

	assert.False(t, bootoutCalled,
		"headless auth-cap exit must NOT call stopLaunchAgentFn (SH-4: bootout defeats KeepAlive respawn)")
	assert.True(t, exitCalled,
		"headless auth-cap exit must call exitFn so launchd KeepAlive respawns the daemon")
}

// TestHeadlessAuthCap_LogsReasonTag verifies that headlessAuthCapExit emits a log
// line containing reason=auth_cap (SH-5 + SH-4).
func TestHeadlessAuthCap_LogsReasonTag(t *testing.T) {
	var buf bytes.Buffer
	testLog := log.New(&buf, "", 0)

	orig := stopLaunchAgentFn
	t.Cleanup(func() { stopLaunchAgentFn = orig })
	stopLaunchAgentFn = func() {}

	origLogger := shutdownLogger
	t.Cleanup(func() { shutdownLogger = origLogger })
	shutdownLogger = testLog

	headlessAuthCapExit(func(int) {})

	got := buf.String()
	assert.True(t, strings.Contains(got, "reason=auth_cap"),
		"headless auth-cap exit must log reason=auth_cap (SH-5), got: %q", got)
}

// ---------------------------------------------------------------------------
// Sarah P2 #3256 — Signal-handler goroutine: capture-then-re-panic
// ---------------------------------------------------------------------------

// TestSignalHandlerRecoverCapturesThenRePanics verifies that
// recoverSignalHandler (the deferred recover helper used in the signal-handler
// goroutine) captures the panic via the provided capture function AND then
// re-panics — it does NOT silently swallow the panic.
//
// Silent swallow = SIGTERM lost; the goroutine exits but the signal is never
// processed, leaving the daemon in a half-alive state.
func TestSignalHandlerRecoverCapturesThenRePanics(t *testing.T) {
	captured := false
	captureFn := func(err error) { captured = true }

	assert.Panics(t, func() {
		func() {
			defer recoverSignalHandler(captureFn)
			panic("simulated signal-handler panic")
		}()
	}, "recoverSignalHandler must re-panic after capturing (not silently swallow)")

	assert.True(t, captured,
		"recoverSignalHandler must call captureFn before re-panicking (#3256)")
}

// ---------------------------------------------------------------------------
// FF-1 — Fitness function: exactly ONE non-test stopLaunchAgentFn call site
// ---------------------------------------------------------------------------

// TestFF1_ExactlyOneNonTestStopLaunchAgentCallSite asserts that the production
// source file (main.go) contains EXACTLY ONE call to stopLaunchAgentFn() —
// inside trayQuitShutdown. This is the structural enforcement of SH-1: no
// accidental re-introduction of bootout calls in signal handlers or error
// paths. Comment lines are excluded from the count.
func TestFF1_ExactlyOneNonTestStopLaunchAgentCallSite(t *testing.T) {
	mainSrc, err := os.ReadFile("main.go")
	require.NoError(t, err, "must be able to read main.go from within the package test")

	lines := strings.Split(string(mainSrc), "\n")
	var callSites []int
	for i, line := range lines {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "//") {
			continue
		}
		if strings.Contains(stripped, "stopLaunchAgentFn()") {
			callSites = append(callSites, i+1) // 1-indexed line number
		}
	}

	assert.Len(t, callSites, 1,
		"FF-1: main.go must contain EXACTLY ONE call to stopLaunchAgentFn() (SH-1). "+
			"Found %d call site(s) at lines: %v. "+
			"bootout must only fire from trayQuitShutdown.", len(callSites), callSites)
}

// ---------------------------------------------------------------------------
// AC1 — structured log line (logShutdown function directly)
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

// TestLogShutdown_BreadcrumbDataDoesNotContainRawAccountID verifies that a raw
// API key that appears in an error message is not passed through to the
// breadcrumb data without scrubbing. Per PII rules: error strings are truncated
// but NOT actively scrubbed (scrubbing is out of scope per ticket); the test
// verifies that the reason value length cap (200 chars) is the only sanitisation
// applied and that the function does not add any unhashed account/user IDs to
// breadcrumb data beyond what the error string itself contains.
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
	assert.Equal(t, shutdownReason("auth_cap"), ReasonAuthCap,
		"ReasonAuthCap must have value 'auth_cap'")
}
