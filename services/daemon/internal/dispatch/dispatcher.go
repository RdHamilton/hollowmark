// Package dispatch handles encoding and posting contract.DaemonEvent payloads to the BFF.
package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RdHamilton/hollowmark/services/contract"
)

// ErrReauthRequired is returned by a Refresher when the token cannot be
// refreshed automatically and user interaction is required (e.g. keychain
// mode where re-authentication must be triggered via the tray icon). The
// dispatcher treats this sentinel as a hard stop: it breaks the retry loop
// immediately after the first attempt and propagates the error to the caller.
var ErrReauthRequired = errors.New("reauth required: user interaction needed")

const (
	maxAttempts = 3
	retryBase   = 500 * time.Millisecond

	// max429Backoff is the default ceiling for the 429-aware hold-in-loop wait.
	// It matches the per-account replenishment window used by the BFF's nginx
	// rate-limit zone (60 s), making the constant self-documenting. Override per
	// test via With429MaxBackoff.
	max429Backoff = 60 * time.Second

	// retryAfterCap is the absolute maximum the dispatcher will wait on a
	// Retry-After header value, regardless of what the server sends (RFC 7231
	// §10.6.30 safety guidance). A header above this value is clamped.
	retryAfterCap = 300 * time.Second

	// retryAfterFloor is the minimum 429 wait even when Retry-After: 0 is
	// returned (floor(max(header, 1s)) per Ray's PLAN_VERDICT ruling).
	retryAfterFloor = 1 * time.Second
)

// Refresher is implemented by any component that can obtain a fresh daemon JWT.
// The dispatcher calls it when the BFF returns 401 before retrying the request.
type Refresher interface {
	Refresh(ctx context.Context) (newToken string, err error)
}

// Dispatcher POSTs DaemonEvents to the BFF ingest endpoint.
// It maintains a per-session monotonic sequence counter that is assigned to
// each event before dispatch (ADR-013).  The counter starts at 1 and resets
// to 0 when the Dispatcher is created (i.e. on daemon restart).
type Dispatcher struct {
	cloudAPIURL string
	ingestPath  string
	// apiKey is the current bearer token. Protected by apiKeyMu so that
	// SetToken (called from the re-auth goroutine in AC-3, #2135) and Token /
	// doSend (called from concurrent Send goroutines) do not race.
	apiKeyMu  sync.RWMutex
	apiKey    string
	client    *http.Client
	refresher Refresher
	// buffer is the optional ring buffer wired via WithBuffer. When non-nil,
	// SendOrBuffer enqueues pre-marshaled bytes after retry exhaustion rather
	// than returning an error to the caller.
	buffer *RingBuffer
	// onBFFFailure is an optional callback invoked once when SendOrBuffer
	// exhausts all retry attempts and buffers the event (terminal failure path
	// only). statusCode is the last HTTP status returned by the BFF, or 0 for
	// transport-level failures. The callback must NOT be invoked on intermediate
	// retry attempts or on context-cancellation buffering — only on the
	// "all attempts failed" branch. Set via WithOnBFFFailure; nil is safe.
	onBFFFailure func(statusCode int)
	// onBFFSuccess is an optional callback invoked when SendOrBuffer successfully
	// delivers an event to the BFF (HTTP 2xx). Called before draining the buffer.
	// Set via WithOnBFFSuccess; nil is safe.
	onBFFSuccess func()
	// seq is the per-session sequence counter.  Incremented atomically so
	// Send is safe for concurrent callers.  Reset to 0 on daemon restart
	// because the Dispatcher itself is recreated on restart.
	seq atomic.Uint64

	// backoff429Max is the ceiling for the hold-in-loop wait on a 429 response.
	// Defaults to max429Backoff (60 s). Override via With429MaxBackoff for tests.
	backoff429Max time.Duration
}

// New creates a Dispatcher.
//
// cloudAPIURL: base URL of the cloud API / BFF, e.g. "https://api.example.com"
// ingestPath: path of the ingest endpoint, e.g. "/v1/ingest/events"
// apiKey: bearer token for Authorization header
func New(cloudAPIURL, ingestPath, apiKey string) *Dispatcher {
	return &Dispatcher{
		cloudAPIURL: cloudAPIURL,
		ingestPath:  ingestPath,
		apiKey:      apiKey,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		backoff429Max: max429Backoff,
	}
}

// With429MaxBackoff overrides the default ceiling for the 429-aware
// hold-in-loop wait. Intended for test configurability (Ray's required option).
// In production, the default max429Backoff constant (60 s) should be used.
func (d *Dispatcher) With429MaxBackoff(dur time.Duration) *Dispatcher {
	d.backoff429Max = dur
	return d
}

// parse429Wait parses the Retry-After header from a 429 response and returns
// the duration the dispatcher should wait before the next attempt.
//
// Rules (Ray's PLAN_VERDICT):
//   - Delta-seconds only (RFC 7231 §7.1.3); HTTP-date format is not supported
//     by our BFF and is treated as absent.
//   - floor: max(parsed, 1s) — a Retry-After: 0 becomes 1 s.
//   - cap:   min(wait, maxBackoff) — header values above maxBackoff are clamped.
//   - absent/unparseable: returns maxBackoff (the configured ceiling).
func parse429Wait(header http.Header, maxBackoff time.Duration) time.Duration {
	ra := header.Get("Retry-After")
	if ra == "" {
		return maxBackoff
	}
	secs, err := strconv.ParseInt(ra, 10, 64)
	if err != nil || secs < 0 {
		// Unparseable or negative value — use the configured ceiling.
		return maxBackoff
	}
	wait := time.Duration(secs) * time.Second
	// Apply floor.
	if wait < retryAfterFloor {
		wait = retryAfterFloor
	}
	// Apply cap (clamped to min(wait, retryAfterCap, maxBackoff)).
	if wait > retryAfterCap {
		wait = retryAfterCap
	}
	if wait > maxBackoff {
		wait = maxBackoff
	}
	return wait
}

// WithRefresher attaches a Refresher that will be called when the BFF returns 401.
// This enables automatic JWT re-registration without restarting the daemon.
func (d *Dispatcher) WithRefresher(r Refresher) *Dispatcher {
	d.refresher = r
	return d
}

// WithBuffer attaches a RingBuffer that SendOrBuffer will use to store
// pre-marshaled event bytes when all retry attempts are exhausted. The buffer
// is per-Dispatcher; concurrent callers share the same RingBuffer instance.
func (d *Dispatcher) WithBuffer(b *RingBuffer) *Dispatcher {
	d.buffer = b
	return d
}

// WithOnBFFFailure registers an optional callback that is invoked exactly once
// when SendOrBuffer exhausts all retry attempts and buffers the event. The
// callback receives the HTTP status code from the last BFF attempt (0 for
// transport-level failures). It is NOT called on intermediate retries or when
// buffering occurs due to context cancellation. Set to nil to disable.
func (d *Dispatcher) WithOnBFFFailure(cb func(statusCode int)) *Dispatcher {
	d.onBFFFailure = cb
	return d
}

// WithOnBFFSuccess registers an optional callback invoked when SendOrBuffer
// successfully delivers an event (HTTP 2xx). Called before buffer drain.
// Used by the service to reset the consecutive-failure counter.
func (d *Dispatcher) WithOnBFFSuccess(cb func()) *Dispatcher {
	d.onBFFSuccess = cb
	return d
}

// SetToken updates the bearer token used for subsequent requests.
// Safe to call concurrently with Send, SendOrBuffer, and Token.
// Called after successful re-registration or in-process re-auth (AC-3, #2135).
func (d *Dispatcher) SetToken(token string) {
	d.apiKeyMu.Lock()
	d.apiKey = token
	d.apiKeyMu.Unlock()
}

// Token returns the current bearer token. Safe to call concurrently.
// Used when building a transient dispatcher that needs the same credentials
// as the primary dispatcher.
func (d *Dispatcher) Token() string {
	d.apiKeyMu.RLock()
	defer d.apiKeyMu.RUnlock()
	return d.apiKey
}

// Send assigns the next per-session sequence number to the event, encodes it
// as JSON, and POSTs it to the BFF with up to 3 attempts.
// Retries on transport errors or non-2xx responses with 500ms * attempt backoff.
// On a 401 response, calls the Refresher (if set) to obtain a new token before
// the next retry. On a 429 response, honors Retry-After (delta-seconds, floored
// to 1 s, capped to min(header, 300 s, backoff429Max)) and holds in the retry
// loop rather than immediately moving to the next attempt.
func (d *Dispatcher) Send(ctx context.Context, event contract.DaemonEvent) error {
	// Assign per-session sequence (ADR-013).  Add(1) returns the new value, so
	// the first call yields 1 — matching the "starts at 1" requirement.
	event.Sequence = d.seq.Add(1)

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var statusCode int
		var respHeader http.Header
		statusCode, respHeader, lastErr = d.doSend(ctx, body)
		if lastErr == nil {
			log.Printf("[dispatch] event %q sent (session=%s)", event.Type, event.SessionID)
			return nil
		}
		// On 429, hold in the retry loop for the Retry-After duration (or the
		// configured max429Backoff ceiling when the header is absent/invalid).
		// This prevents 3x-amplifying request volume against the per-IP limit.
		if statusCode == http.StatusTooManyRequests {
			wait := parse429Wait(respHeader, d.backoff429Max)
			log.Printf("[dispatch] 429 received (attempt %d/%d); backing off %s", attempt, maxAttempts, wait)
			if attempt < maxAttempts {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(wait):
				}
			}
			continue
		}
		// On 401, attempt to refresh the token before retrying.
		if statusCode == http.StatusUnauthorized && d.refresher != nil {
			log.Printf("[dispatch] 401 received; attempting token refresh")
			newToken, refreshErr := d.refresher.Refresh(ctx)
			if errors.Is(refreshErr, ErrReauthRequired) {
				log.Printf("[dispatch] reauth required; aborting retry loop")
				return ErrReauthRequired
			}
			if refreshErr != nil {
				log.Printf("[dispatch] token refresh failed: %v", refreshErr)
			} else {
				d.SetToken(newToken)
				log.Printf("[dispatch] token refreshed; retrying")
			}
		}
		if attempt < maxAttempts {
			backoff := retryBase * time.Duration(attempt)
			log.Printf("[dispatch] attempt %d/%d failed: %v; retrying in %s", attempt, maxAttempts, lastErr, backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}

// SendOrBuffer behaves like Send but, when a buffer has been wired via
// WithBuffer, silently enqueues the pre-marshaled event bytes on retry
// exhaustion instead of returning an error.
//
// This satisfies ADR-013 Option C: the sequence number is stamped into the
// marshaled bytes at emission time (inside Send's seq.Add(1) call), so
// bytes stored in the buffer carry their original sequence and are replayed
// verbatim without re-numbering.
//
// When no buffer is attached, SendOrBuffer is identical to Send.
func (d *Dispatcher) SendOrBuffer(ctx context.Context, event contract.DaemonEvent) error {
	// Stamp sequence and marshal before calling doSend so the bytes are
	// ready to buffer if needed — same as Send's internal flow, but we need
	// the marshaled bytes to hand to the ring buffer on failure.
	event.Sequence = d.seq.Add(1)

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	var lastErr error
	var lastStatusCode int
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var statusCode int
		var respHeader http.Header
		statusCode, respHeader, lastErr = d.doSend(ctx, body)
		if lastErr == nil {
			log.Printf("[dispatch] event %q sent (session=%s)", event.Type, event.SessionID)
			// Notify the service of a confirmed BFF success before draining the
			// buffer. This allows the service to reset failure counters at the
			// earliest possible moment (before replay events potentially fail).
			if d.onBFFSuccess != nil {
				d.onBFFSuccess()
			}
			// AC-3: drain buffered events on first successful send (best-effort;
			// per ADR-013 amendment Q1/OQ-1 a failed drain item is logged and
			// discarded — no re-enqueue to avoid thundering-herd / livelock).
			if d.buffer != nil {
				for _, item := range d.buffer.Drain() {
					if _, _, drainErr := d.doSend(ctx, item); drainErr != nil {
						log.Printf("[dispatch] drain replay failed: %v", drainErr)
					}
				}
			}
			return nil
		}
		lastStatusCode = statusCode
		// On 429, hold in the retry loop for the Retry-After duration (or the
		// configured max429Backoff ceiling when the header is absent/invalid).
		// If maxAttempts are exhausted, falls through to the ring-buffer enqueue
		// below (event not lost). On ctx-cancel during the wait, buffer + return.
		if statusCode == http.StatusTooManyRequests {
			wait := parse429Wait(respHeader, d.backoff429Max)
			log.Printf("[dispatch] 429 received (attempt %d/%d); backing off %s", attempt, maxAttempts, wait)
			if attempt < maxAttempts {
				select {
				case <-ctx.Done():
					if d.buffer != nil {
						d.buffer.Enqueue(body)
						log.Printf("[dispatch] context cancelled during 429 wait; buffered event seq=%d", event.Sequence)
					}
					return ctx.Err()
				case <-time.After(wait):
				}
			}
			continue
		}
		// On 401, attempt to refresh the token before retrying.
		if statusCode == http.StatusUnauthorized && d.refresher != nil {
			log.Printf("[dispatch] 401 received; attempting token refresh")
			newToken, refreshErr := d.refresher.Refresh(ctx)
			if errors.Is(refreshErr, ErrReauthRequired) {
				log.Printf("[dispatch] reauth required; aborting retry loop")
				if d.buffer != nil {
					d.buffer.Enqueue(body)
					log.Printf("[dispatch] reauth required; buffered event seq=%d", event.Sequence)
				}
				return ErrReauthRequired
			}
			if refreshErr != nil {
				log.Printf("[dispatch] token refresh failed: %v", refreshErr)
			} else {
				d.SetToken(newToken)
				log.Printf("[dispatch] token refreshed; retrying")
			}
		}
		if attempt < maxAttempts {
			backoff := retryBase * time.Duration(attempt)
			log.Printf("[dispatch] attempt %d/%d failed: %v; retrying in %s", attempt, maxAttempts, lastErr, backoff)
			select {
			case <-ctx.Done():
				if d.buffer != nil {
					d.buffer.Enqueue(body)
					log.Printf("[dispatch] context cancelled; buffered event seq=%d", event.Sequence)
				}
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	if d.buffer != nil {
		d.buffer.Enqueue(body)
		log.Printf("[dispatch] all %d attempts failed; buffered event seq=%d (dropped_total=%d)",
			maxAttempts, event.Sequence, d.buffer.Dropped())
		// Notify the caller that a terminal BFF failure occurred. The last
		// known HTTP status code is passed (0 for transport-level failures)
		// so the service can record it for the dispatch_degraded counter.
		// This fires ONLY on the "all retries exhausted" path — NOT on
		// intermediate retries and NOT on context-cancellation buffering.
		if d.onBFFFailure != nil {
			d.onBFFFailure(lastStatusCode)
		}
		return nil
	}
	return fmt.Errorf("all %d attempts failed: %w", maxAttempts, lastErr)
}

// doSend performs a single POST of body to the ingest endpoint.
// Returns the HTTP status code (0 on transport failure), the response headers
// (nil on transport failure), and any error. Callers use the headers to read
// Retry-After on 429 responses.
func (d *Dispatcher) doSend(ctx context.Context, body []byte) (int, http.Header, error) {
	url := d.cloudAPIURL + d.ingestPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if tok := d.Token(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("post event: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, resp.Header, fmt.Errorf("BFF returned %d", resp.StatusCode)
	}

	return resp.StatusCode, resp.Header, nil
}

// StampSeq increments the per-session sequence counter and returns the new
// value.  It is intended for use by BatchBuffer's stamp closure so the
// sequence is stamped at Add() time (ADR-013 monotonic guarantee) without
// exposing the seq field directly.
func (d *Dispatcher) StampSeq() uint64 {
	return d.seq.Add(1)
}

// SendBatch marshals events as a JSON array and POSTs it to the ingest
// endpoint using the same doSend (and its 429-aware Retry-After backoff from
// PR #816) as SendOrBuffer.
//
// On 429, doSend returns the response headers so parse429Wait can compute the
// correct hold duration — this is how SendBatch inherits the #816 backoff.
//
// On retry exhaustion the batch is decomposed and each event is individually
// enqueued to the RingBuffer via reEnqueueBatch: the RingBuffer is
// single-event-oriented (one []byte slot per event), and sequence numbers are
// already stamped at BatchBuffer.Add time, so verbatim re-enqueue is safe.
// SendBatch returns an error on retry exhaustion (unlike SendOrBuffer which
// silences it with the ring buffer); the BatchBuffer's FlushFn is already
// error-tolerant and logs the failure.
//
// Sequence numbers must be stamped on each event before calling SendBatch.
func (d *Dispatcher) SendBatch(ctx context.Context, events []contract.DaemonEvent) error {
	if len(events) == 0 {
		return nil
	}

	body, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("marshal batch: %w", err)
	}

	var lastErr error
	var lastStatus int
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		var statusCode int
		var respHeader http.Header
		statusCode, respHeader, lastErr = d.doSend(ctx, body)
		if lastErr == nil {
			log.Printf("[dispatch] batch of %d events sent (attempt %d)", len(events), attempt)
			if d.onBFFSuccess != nil {
				d.onBFFSuccess()
			}
			// Drain the ring buffer on success — same path as SendOrBuffer.
			if d.buffer != nil {
				for _, item := range d.buffer.Drain() {
					if _, _, drainErr := d.doSend(ctx, item); drainErr != nil {
						log.Printf("[dispatch] batch: drain replay failed: %v", drainErr)
					}
				}
			}
			return nil
		}
		lastStatus = statusCode

		// On 429, honor Retry-After (captured via respHeader) and hold in the
		// retry loop.  This mirrors SendOrBuffer's 429 handling exactly — both
		// call parse429Wait(respHeader, d.backoff429Max).
		if statusCode == http.StatusTooManyRequests {
			wait := parse429Wait(respHeader, d.backoff429Max)
			log.Printf("[dispatch] batch: 429 received (attempt %d/%d); backing off %s",
				attempt, maxAttempts, wait)
			if attempt < maxAttempts {
				select {
				case <-ctx.Done():
					d.reEnqueueBatch(events)
					return ctx.Err()
				case <-time.After(wait):
				}
			}
			continue
		}

		// On 401, attempt to refresh the token before retrying — same path as
		// SendOrBuffer so the keychainRefresherAdapter fires correctly.
		if statusCode == http.StatusUnauthorized && d.refresher != nil {
			log.Printf("[dispatch] batch: 401 received; attempting token refresh")
			newToken, refreshErr := d.refresher.Refresh(ctx)
			if errors.Is(refreshErr, ErrReauthRequired) {
				log.Printf("[dispatch] batch: reauth required; aborting retry loop")
				d.reEnqueueBatch(events)
				return ErrReauthRequired
			}
			if refreshErr != nil {
				log.Printf("[dispatch] batch: token refresh failed: %v", refreshErr)
			} else {
				d.SetToken(newToken)
				log.Printf("[dispatch] batch: token refreshed; retrying")
			}
		}

		if attempt < maxAttempts {
			backoff := retryBase * time.Duration(attempt)
			log.Printf("[dispatch] batch: attempt %d/%d failed: %v; retrying in %s",
				attempt, maxAttempts, lastErr, backoff)
			select {
			case <-ctx.Done():
				d.reEnqueueBatch(events)
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	// All attempts failed — re-enqueue per-event to the ring buffer.
	d.reEnqueueBatch(events)
	if d.onBFFFailure != nil {
		d.onBFFFailure(lastStatus)
	}
	return fmt.Errorf("SendBatch: all %d attempts failed: %w", maxAttempts, lastErr)
}

// reEnqueueBatch individually marshals and enqueues each event from a failed
// batch into the RingBuffer.  The RingBuffer is single-event-oriented (one
// []byte slot per event), so batch-granularity re-enqueue is not possible.
// Sequence numbers are already stamped, so bytes are stored verbatim.
func (d *Dispatcher) reEnqueueBatch(events []contract.DaemonEvent) {
	if d.buffer == nil {
		return
	}
	for _, e := range events {
		b, err := json.Marshal(e)
		if err != nil {
			log.Printf("[dispatch] reEnqueueBatch: marshal error seq=%d: %v", e.Sequence, err)
			continue
		}
		d.buffer.Enqueue(b)
		log.Printf("[dispatch] reEnqueueBatch: re-enqueued event seq=%d type=%s", e.Sequence, e.Type)
	}
}

// BuildEvent constructs a contract.DaemonEvent from raw log entry data.
//
// eventType: semantic event type, e.g. "draft.pick"
// accountID: MTGA account ID
// sessionID: current monitoring session ID
// payload: any JSON-serialisable value
func BuildEvent(eventType, accountID, sessionID string, payload interface{}) (contract.DaemonEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return contract.DaemonEvent{}, fmt.Errorf("marshal payload: %w", err)
	}
	return contract.DaemonEvent{
		Type:       eventType,
		AccountID:  accountID,
		SessionID:  sessionID,
		OccurredAt: time.Now().UTC(),
		Payload:    json.RawMessage(raw),
	}, nil
}
