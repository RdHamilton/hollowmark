# ADR-013: Daemon Event Ordering

**Status**: Accepted
**Date**: 2026-05-07
**Author**: Architect / Product Manager
**Milestone**: v0.3.0

---

## Context

The daemon dispatches events to the BFF ingest endpoint over HTTPS. Because the ingest endpoint is stateless HTTP POST (not a streaming connection), events can arrive out of order due to:

- Retry logic on transient failures (a failed event is retried after a later event succeeds)
- Concurrent dispatch goroutines
- Network jitter

The GRE projector (ADR-012) requires events within a session to be processed in emission order. Without ordering metadata, the BFF cannot detect gaps or reorder events before projection.

---

## Decision

### Sequence field

Every `DaemonEvent` payload gains a `sequence` field of type `uint64`. The daemon increments a monotonic counter starting at `1` when the daemon process starts. The counter is per-process (not persisted); it resets to `1` on daemon restart.

The `sequence` field is set on every event before dispatch, regardless of event type.

### BFF storage

The BFF ingest handler stores the `sequence` value in the `daemon_events` table. A new `sequence` column (`BIGINT NOT NULL DEFAULT 0`) is added to `daemon_events` via migration. This is a separate migration from the contract change in #1509 — the schema change is a DBA ticket; the contract change is an application ticket.

### Ordering in the GRE projector

The GRE projector (ticket #1512) orders events within a session by `(occurred_at, sequence)` before batched insert. `occurred_at` is the primary sort key (wall clock time); `sequence` is the tiebreaker for events with identical `occurred_at` timestamps.

### Gap detection

The BFF logs a warning (and emits a PostHog event `daemon_event_gap_detected`) when it receives an event for a known session where `sequence > last_sequence + 1`. This is a signal-only mechanism — the BFF does not block or discard out-of-order events. Gaps are observable but not fatal.

### Sequence reset detection

The BFF detects a sequence reset (new `sequence` value lower than the last seen value for the same `account_id`) and resets its per-session tracking. A reset is treated as a new daemon session start.

---

## Consequences

**Positive:**
- Ordering is deterministic within a session even when events arrive out of order.
- Gap detection surfaces silent data loss to the operator without blocking the ingest path.
- No breaking change to the existing ingest schema — the column has a safe default.

**Negative:**
- The `daemon_events` table gains a column that must be backfilled for existing rows (set to 0, acceptable — pre-ADR-013 rows have no ordering guarantee).
- The sequence counter resets on daemon restart; the BFF must handle this case without treating it as a gap.
- Gap alerting produces noise if the daemon is restarted frequently (e.g., during development).

**Implementation tickets:**
- #1509: Add `sequence uint64` to `DaemonEvent` contract (daemon + BFF application layer)
- New ticket required: `daemon_events` schema migration — add `sequence BIGINT NOT NULL DEFAULT 0` column
- New ticket required: BFF gap detection logging and PostHog event instrumentation

---

## Alternatives Considered

**Persist sequence counter to disk**: rejected for v0.3.0. Adds complexity; the BFF's per-session tracking handles restarts adequately. Revisit at v0.4.0 if gap rates are unacceptable.

**Use `occurred_at` only for ordering**: rejected. Two GRE events in the same millisecond are possible (e.g., simultaneous life total changes). Sequence provides a deterministic tiebreaker.

**Reject out-of-order events at the BFF**: rejected. This would cause data loss on retry scenarios and punish transient network failures.
