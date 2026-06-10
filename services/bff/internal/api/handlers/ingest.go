// Package handlers provides HTTP request handlers for the BFF service.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/analytics"
	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/identityhash"
	contract "github.com/RdHamilton/hollowmark/services/contract"
)

// dispatchDegradedThreshold is the minimum number of consecutive BFF dispatch
// failures (counted by the daemon) that triggers a daemon.dispatch_degraded
// analytics event. Defined BFF-side so the threshold can be tuned without a
// daemon redeploy (per Ray's PLAN_VERDICT §architectural notes point 6).
const dispatchDegradedThreshold = uint32(3)

// maxBatchSize is the maximum number of events accepted in a single batched
// POST body. Requests exceeding this cap are rejected with 413 as a defence
// against malformed or hostile daemon payloads (ADR-053 §5). The honest
// daemon caps at N=25, so this limit is not expected to fire in production.
const maxBatchSize = 100

// maxIngestBodyBytes is the maximum number of bytes read from any ingest
// request body (single or batch). Bodies truncated by io.LimitReader cause a
// JSON decode error which is surfaced as 413 Request Entity Too Large.
// 1 MiB is generous for a 25-event honest batch and blocks absurdly large
// payloads before JSON decode allocates memory for them (ADR-053 §5, Ray's
// required named constant).
const maxIngestBodyBytes = 1 * 1024 * 1024 // 1 MiB

// EventBroadcaster is implemented by any type that can broadcast a daemon event
// to connected clients (e.g. an SSE broker).  userID scopes delivery to the
// authenticated user's SSE subscribers only — preventing cross-tenant leakage.
type EventBroadcaster interface {
	BroadcastDaemonEvent(userID int64, event contract.DaemonEvent)
}

// DaemonEventInserter is implemented by any type that can persist a daemon event
// to durable storage.  It is satisfied by *repository.DaemonEventsRepository.
type DaemonEventInserter interface {
	Insert(ctx context.Context, userID int64, accountID string, eventType string, payload json.RawMessage, occurredAt time.Time, eventID string, sequence uint64) error
}

// IngestHandler accepts daemon events posted by the daemon service and
// broadcasts them to connected frontend clients via the broadcaster.
// When a DaemonEventInserter is wired, each event is also persisted to the
// database before broadcasting.
type IngestHandler struct {
	broadcaster EventBroadcaster
	repo        DaemonEventInserter
	gapDetector *GapDetector
	analytics   *analytics.Client
}

// NewIngestHandler creates an IngestHandler that broadcasts received events
// through the provided broadcaster.  Pass nil for repo to run in
// broadcast-only mode (no persistence).
//
// A GapDetector is always initialised.  The analytics client defaults to a
// no-op until WithAnalyticsClient is called.
func NewIngestHandler(broadcaster EventBroadcaster) *IngestHandler {
	return &IngestHandler{
		broadcaster: broadcaster,
		gapDetector: &GapDetector{},
		analytics:   analytics.NewClient(analytics.NoopEnqueuer{}, analytics.NewNoopHaltChecker()),
	}
}

// WithRepository returns a copy of h with repo wired for persistence.
// This enables optional dependency injection without changing the existing
// NewIngestHandler call-sites.
func (h *IngestHandler) WithRepository(repo DaemonEventInserter) *IngestHandler {
	return &IngestHandler{
		broadcaster: h.broadcaster,
		repo:        repo,
		gapDetector: h.gapDetector,
		analytics:   h.analytics,
	}
}

// WithPostHogClient is deprecated. Use WithAnalyticsClient instead.
func (h *IngestHandler) WithPostHogClient(client analytics.PostHogEnqueuer) *IngestHandler {
	return h.WithAnalyticsClient(analytics.NewClient(client, analytics.NewNoopHaltChecker()))
}

// WithAnalyticsClient wires an analytics.Client into the handler.
func (h *IngestHandler) WithAnalyticsClient(c *analytics.Client) *IngestHandler {
	return &IngestHandler{
		broadcaster: h.broadcaster,
		repo:        h.repo,
		gapDetector: h.gapDetector,
		analytics:   c,
	}
}

// IngestEvent handles POST /v1/ingest/events.
// Authentication is enforced by APIKeyAuth middleware upstream.
// By the time this handler runs, UserIDFromContext is set on the request context.
//
// Dual-shape body detection (ADR-053 §5, backward-compatible — no new MIME type):
//   - Body starting with '{' → single DaemonEvent (existing wire format; old daemons).
//   - Body starting with '[' → JSON array of DaemonEvents (batch, new in v0.3.8+).
//
// Both shapes are accepted on the same endpoint so old daemons (pre-batch)
// remain fully functional without a flag day. Per-body size is limited to
// maxIngestBodyBytes; batch size is capped at maxBatchSize events.
func (h *IngestHandler) IngestEvent(w http.ResponseWriter, r *http.Request) {
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Read the full body up to maxIngestBodyBytes + 1 so we can detect an
	// over-size body and return 413 rather than silently truncating.
	limited := io.LimitReader(r.Body, maxIngestBodyBytes+1)
	rawBody, err := io.ReadAll(limited)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	if int64(len(rawBody)) > maxIngestBodyBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Detect body shape by the first non-whitespace byte.
	trimmed := bytes.TrimSpace(rawBody)
	if len(trimmed) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	if trimmed[0] == '[' {
		// Batch path (ADR-053 §5).
		var events []contract.DaemonEvent
		if err := json.Unmarshal(rawBody, &events); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if len(events) == 0 {
			http.Error(w, "batch must contain at least one event", http.StatusBadRequest)
			return
		}
		if len(events) > maxBatchSize {
			http.Error(w, "batch exceeds maximum allowed size", http.StatusRequestEntityTooLarge)
			return
		}
		for _, event := range events {
			// Accept-and-skip malformed events per ADR-039 projection resilience.
			// Logged at DEBUG so the skip is observable without polluting INFO.
			if event.Type == "" {
				slog.Debug("[IngestHandler] skipping batch event: missing type",
					"account_id_hash", hashAccountID(event.AccountID))
				continue
			}
			h.processEvent(r.Context(), userID, event)
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}

	// Single-event path — body starts with '{' (or any other non-'[' token).
	var event contract.DaemonEvent
	if err := json.Unmarshal(rawBody, &event); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if event.Type == "" {
		http.Error(w, "event type is required", http.StatusBadRequest)
		return
	}

	h.processEvent(r.Context(), userID, event)
	w.WriteHeader(http.StatusAccepted)
}

// processEvent persists, gap-checks, emits analytics observability signals, and
// broadcasts a single DaemonEvent. It is called by both the single-event and
// batch paths of IngestEvent so behavior is identical regardless of the wire
// shape.
func (h *IngestHandler) processEvent(ctx context.Context, userID int64, event contract.DaemonEvent) {
	// Persist the event before broadcasting. A persistence failure is logged
	// but does not drop the live event — the broadcast still proceeds so the
	// frontend receives the event even when the database is degraded.
	if h.repo != nil {
		if err := h.repo.Insert(ctx, userID, event.AccountID, event.Type, event.Payload, event.OccurredAt, event.EventID, event.Sequence); err != nil {
			slog.Error(
				"[IngestHandler] ERROR persisting event",
				"type", event.Type,
				"userID", userID,
				"account_id_hash", hashAccountID(event.AccountID),
				"err", err,
			)
		}
	}

	// Gap detection: check for sequence discontinuities.
	// This never blocks or discards events — it is observability only.
	if event.Sequence > 0 {
		if isGap, expected := h.gapDetector.Check(event.AccountID, event.SessionID, event.Sequence); isGap {
			slog.Warn(
				"[IngestHandler] sequence gap detected",
				"account_id_hash", hashAccountID(event.AccountID),
				"session_id", event.SessionID,
				"expected_sequence", expected,
				"received_sequence", event.Sequence,
			)

			hashedAccountID := identityhash.HashAccountID(event.AccountID)
			_ = h.analytics.Capture(ctx, hashedAccountID, analytics.EventDaemonEventGapDetected, map[string]any{
				"account_id_hash":   hashedAccountID,
				"session_id":        event.SessionID,
				"expected_sequence": expected,
				"received_sequence": event.Sequence,
			})
		}
	}

	// Heartbeat: inspect payload for observability signals and emit analytics
	// events when thresholds are exceeded. Analytics emission is BFF-only per
	// ADR-027 §OQ-5. The daemon does not import posthog-go (ADR-027 FF#3).
	if event.Type == "daemon.heartbeat" {
		// heartbeatPayload mirrors the daemon-local struct (JSON wire contract
		// agreed in Ray's PLAN_VERDICT for #2569 and #2139). Both sides use
		// omitempty on the counter fields; zero values skip emission.
		var hb struct {
			ParseFailureCount      uint32   `json:"parse_failure_count"`
			SampleLineHash         string   `json:"sample_line_hash,omitempty"`
			FailedEventTypes       []string `json:"failed_event_types,omitempty"`
			ConsecutiveBFFFailures uint32   `json:"consecutive_bff_failures,omitempty"`
			LastBFFStatusCode      int      `json:"last_bff_status_code,omitempty"`
		}
		if err := json.Unmarshal(event.Payload, &hb); err == nil {
			hashedAccountID := identityhash.HashAccountID(event.AccountID)
			// log_format_drift (#2569): emit when parse failures occurred.
			if hb.ParseFailureCount > 0 {
				_ = h.analytics.Capture(ctx, hashedAccountID, analytics.EventDaemonDispatchDegraded, map[string]any{
					"account_id_hash":     hashedAccountID,
					"degraded_reason":     "log_format_drift",
					"parse_failure_count": hb.ParseFailureCount,
					"sample_line_hash":    hb.SampleLineHash,
					"failed_event_types":  hb.FailedEventTypes,
				})
			}
			// daemon.dispatch_degraded (#2139): emit when BFF failure count
			// meets or exceeds the threshold. Threshold is BFF-side so it can
			// be tuned without a daemon redeploy.
			if hb.ConsecutiveBFFFailures >= dispatchDegradedThreshold {
				_ = h.analytics.Capture(ctx, hashedAccountID, analytics.EventDaemonDispatchDegraded, map[string]any{
					"account_id_hash":      hashedAccountID,
					"degraded_reason":      "dispatch_error",
					"consecutive_failures": hb.ConsecutiveBFFFailures,
					"status_code":          hb.LastBFFStatusCode,
				})
			}
		}
	}

	// daemon.auth_failed (#2139): dedicated dispatch event sent immediately
	// when ErrReauthRequired fires (BFF 401/403) or PKCE flow fails. The
	// daemon dispatches this event directly so latency is minimal (no
	// heartbeat-window delay). distinct_id is always hashAccountID(AccountID)
	// — the AccountID is the live Clerk session ID (post-auth path only).
	if event.Type == "daemon.auth_failed" {
		var p struct {
			Reason        string `json:"reason"`
			BFFStatusCode int    `json:"bff_status_code,omitempty"`
			Platform      string `json:"platform"`
			DaemonVersion string `json:"daemon_version"`
		}
		if err := json.Unmarshal(event.Payload, &p); err == nil {
			hashedAccountID := identityhash.HashAccountID(event.AccountID)
			props := map[string]any{
				"account_id_hash": hashedAccountID,
				"reason":          p.Reason,
				"platform":        p.Platform,
				"daemon_version":  p.DaemonVersion,
			}
			if p.BFFStatusCode != 0 {
				props["bff_status_code"] = p.BFFStatusCode
			}
			_ = h.analytics.Capture(ctx, hashedAccountID, analytics.EventDaemonAuthFailed, props)
		}
	}

	// daemon.keychain_error (#2139): dedicated dispatch event sent after all
	// retryKeychain retries are exhausted and AccountID is non-empty (post-auth
	// case B per Ray's OQ-1). Pre-auth keychain failures are unobservable via
	// the BFF emission boundary and are not emitted.
	if event.Type == "daemon.keychain_error" {
		var p struct {
			ErrorType     string `json:"error_type"`
			Platform      string `json:"platform"`
			DaemonVersion string `json:"daemon_version"`
		}
		if err := json.Unmarshal(event.Payload, &p); err == nil {
			hashedAccountID := identityhash.HashAccountID(event.AccountID)
			_ = h.analytics.Capture(ctx, hashedAccountID, analytics.EventDaemonKeychainError, map[string]any{
				"account_id_hash": hashedAccountID,
				"error_type":      p.ErrorType,
				"platform":        p.Platform,
				"daemon_version":  p.DaemonVersion,
			})
		}
	}

	if h.broadcaster != nil {
		h.broadcaster.BroadcastDaemonEvent(userID, event)
	}

	slog.Info(
		"[IngestHandler] Received event",
		"type", event.Type,
		"seq", event.Sequence,
		"account_id_hash", hashAccountID(event.AccountID),
		"userID", userID,
	)
}
