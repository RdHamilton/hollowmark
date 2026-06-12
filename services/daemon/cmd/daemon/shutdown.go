package main

// shutdown.go — SH-5: structured shutdown-reason logging + daemon.shutdown
// Sentry breadcrumb telemetry (ADR-083, tickets#1357).
//
// Every daemon exit path calls logShutdown before the process ends so that
// any future "daemon stopped unexpectedly" investigation starts with a single
// grep rather than a multi-hour source-trace. The Sentry breadcrumb is
// non-blocking and flushed explicitly at each call site that uses os.Exit
// (os.Exit bypasses defers, so the flush cannot be deferred).

import (
	"log"
	"unicode/utf8"

	"github.com/getsentry/sentry-go"
)

// shutdownReason is the tagged reason value for a daemon termination.
// Values ∈ {sigterm, tray_quit, run_error} per SH-5.
// The run_error variant is extended at the log-line level with ":<err_summary>".
type shutdownReason string

const (
	// ReasonSIGTERM is emitted when the daemon receives SIGTERM or SIGINT
	// from the OS (installer, logout/shutdown, pkill). Under the corrected
	// SH-2 semantics (ADR-083) the daemon drains and exits plainly; launchd
	// respawns it via KeepAlive=true.
	ReasonSIGTERM shutdownReason = "sigterm"

	// ReasonTrayQuit is emitted when the user explicitly clicks Quit in the
	// system-tray menu. This is the one legitimate bootout path (SH-1).
	ReasonTrayQuit shutdownReason = "tray_quit"

	// ReasonRunError is emitted when svc.Run returns a non-nil error. The log
	// line includes the error summary; the Sentry breadcrumb is LevelError.
	ReasonRunError shutdownReason = "run_error"
)

// maxErrSummaryBytes is the maximum byte length of the error string appended to
// a run_error reason field (AC3 — cap at 200 chars to bound PII surface area).
const maxErrSummaryBytes = 200

// logShutdown writes the SH-5 structured log line and captures a Sentry
// breadcrumb. It does NOT call sentry.Flush — the caller is responsible for
// flushing before any os.Exit call (defers fire on normal return but not on
// os.Exit; the Sentry flush is already deferred in main() for the normal-exit
// path, and each os.Exit call site must flush explicitly — see call sites in
// main.go).
//
// l must not be nil; in production pass log.Default().
//
// For ReasonRunError, err may be nil (produces "run_error" without a colon
// suffix). For all other reasons, err is ignored.
func logShutdown(l *log.Logger, reason shutdownReason, err error) {
	reasonStr := buildReasonString(reason, err)
	l.Printf("[daemon] stopped reason=%s", reasonStr)

	level := sentry.LevelInfo
	if reason == ReasonRunError {
		level = sentry.LevelError
	}

	sentry.AddBreadcrumb(&sentry.Breadcrumb{
		Category: "daemon.shutdown",
		Message:  "daemon.shutdown reason=" + reasonStr,
		Level:    level,
	})
}

// buildReasonString returns the full reason string for the log line.
// For run_error with a non-nil error it appends ":<err_summary>" (truncated).
func buildReasonString(reason shutdownReason, err error) string {
	if reason != ReasonRunError || err == nil {
		return string(reason)
	}
	summary := truncateUTF8(err.Error(), maxErrSummaryBytes)
	return string(reason) + ":" + summary
}

// truncateUTF8 returns s truncated to at most maxBytes bytes, respecting
// UTF-8 rune boundaries so the result is always valid UTF-8.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	// Walk backwards from maxBytes to find a valid rune boundary.
	b := maxBytes
	for b > 0 && !utf8.RuneStart(s[b]) {
		b--
	}
	return s[:b]
}
