target: backend-engineer
---
## 2026-05-06 — Issue #1400: feat(observability): Sentry Go BFF integration
**PR**: #1408
**Files changed**:
- `services/bff/go.mod` — add github.com/getsentry/sentry-go v0.46.2
- `services/bff/internal/config/config.go` — add SentryDSN field sourced from SENTRY_DSN env var (SSM /vaultmtg/prod/sentry-bff-dsn at deploy time)
- `services/bff/internal/config/config_test.go` — tests for SentryDSN empty/set cases
- `services/bff/internal/api/middleware/sentry.go` — NewSentryMiddleware using sentryhttp.Handler with Repanic=true and user ID scope attachment
- `services/bff/internal/api/middleware/sentry_test.go` — 3 tests: panic capture, no-op uninitialised, user ID attachment (MockTransport, no network)
- `services/bff/cmd/main.go` — Sentry Init at startup, Flush on shutdown, SentryMiddl wired into RouterDeps/BuildRouter before chi Recoverer
**Summary**: Wired sentry-go SDK into the BFF so panics and errors are captured automatically; DSN sourced from SENTRY_DSN env var (never logged), middleware installed as outermost handler with Repanic=true so chi Recoverer still writes 500 responses, and user context attached without PII.
