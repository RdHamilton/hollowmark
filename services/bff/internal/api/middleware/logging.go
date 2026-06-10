// Package middleware provides HTTP middleware for the BFF service.
package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/identityhash"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// hashAccountIDForLog returns a privacy-safe representation of accountID for
// log output: SHA-256 hex, first 16 characters.  No raw PII is ever emitted.
// The input must already be the string form of the account ID.
//
// Delegates to identityhash.HashAccountID per the FM-2 one-implementation rule.
func hashAccountIDForLog(accountID string) string {
	return identityhash.HashAccountID(accountID)
}

// responseCapture wraps ResponseWriter to capture the status code written by
// the downstream handler so the logging middleware can include it in the
// structured log record.
type responseCapture struct {
	http.ResponseWriter
	status int
}

func (rc *responseCapture) WriteHeader(code int) {
	if rc.status == 0 {
		rc.status = code
	}

	rc.ResponseWriter.WriteHeader(code)
}

func (rc *responseCapture) Write(b []byte) (int, error) {
	if rc.status == 0 {
		rc.status = http.StatusOK
	}

	return rc.ResponseWriter.Write(b)
}

// Flush delegates to the underlying ResponseWriter so SSE handlers can flush
// without type asserting through the wrapper.
func (rc *responseCapture) Flush() {
	if f, ok := rc.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// statusCode returns the captured status or 200 when WriteHeader was never called.
func (rc *responseCapture) statusCode() int {
	if rc.status == 0 {
		return http.StatusOK
	}

	return rc.status
}

// NewStructuredLogger returns a chi-compatible middleware that emits one JSON
// log line per request with the following fields:
//
//   - time        — RFC3339Nano timestamp of request completion
//   - level       — slog level string (INFO / WARN / ERROR)
//   - request_id  — value set by chi RequestID middleware (or empty string)
//   - method      — HTTP method
//   - route       — chi route pattern (e.g. /api/v1/drafts/{sessionId})
//   - path        — raw request URL path
//   - status      — HTTP response status code
//   - latency_ms  — request duration in milliseconds (float64)
//   - account_id_hash — SHA-256[:16] of the authenticated int64 user ID, or
//     empty string when no user is on the context (public routes)
//
// Level selection: status >= 500 → ERROR; status >= 400 → WARN; else INFO.
//
// Secrets are never emitted: Authorization header values are not logged.
// The raw user/account ID is never logged — only its hash.
//
// The logger writes to the provided slog.Logger.  Pass NewDefaultLogger() to
// use the process-level logger configured via LOG_LEVEL.
func NewStructuredLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap to capture the status code.
			rc := &responseCapture{ResponseWriter: w}

			next.ServeHTTP(rc, r)

			latencyMs := float64(time.Since(start).Nanoseconds()) / 1e6
			status := rc.statusCode()

			// Determine log level by status code.
			level := slog.LevelInfo
			if status >= 500 {
				level = slog.LevelError
			} else if status >= 400 {
				level = slog.LevelWarn
			}

			// request_id from chi's RequestID middleware.
			requestID := chimiddleware.GetReqID(r.Context())

			// chi route pattern (e.g. /api/v1/drafts/{sessionId}).
			// RouteContext is nil on routes that chi does not know about (404).
			route := r.URL.Path
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if rp := rctx.RoutePattern(); rp != "" {
					route = rp
				}
			}

			// account_id_hash: hash the int64 user ID stored by auth middleware.
			// Raw user ID is NEVER logged.
			var accountIDHash string
			if userID, ok := UserIDFromContext(r.Context()); ok && userID != 0 {
				accountIDHash = hashAccountIDForLog(fmt.Sprintf("%d", userID))
			}

			logger.LogAttrs(
				r.Context(),
				level,
				"request",
				slog.String("request_id", requestID),
				slog.String("method", r.Method),
				slog.String("route", route),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Float64("latency_ms", latencyMs),
				slog.String("account_id_hash", accountIDHash),
			)
		})
	}
}

// NewDefaultLogger returns a JSON slog.Logger whose level is set from the
// LOG_LEVEL environment variable (DEBUG / INFO / WARN / ERROR).  Defaults to
// INFO when the variable is absent or unrecognised.  Output goes to os.Stdout
// so the CloudWatch Agent (R-06) captures it from the process stdout stream.
func NewDefaultLogger() *slog.Logger {
	level := slog.LevelInfo

	switch strings.ToUpper(strings.TrimSpace(os.Getenv("LOG_LEVEL"))) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
}
