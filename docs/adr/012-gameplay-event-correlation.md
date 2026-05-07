# ADR-012: Gameplay Event Correlation

**Status**: Accepted
**Date**: 2026-05-07
**Author**: Architect / Product Manager
**Milestone**: v0.3.0

---

## Context

MTG Arena emits GRE (Game Rules Engine) events as discrete log lines throughout a match — game start, each turn, life total changes, and game end. These arrive as separate log entries with no single "match result" event. The daemon must correlate these into a coherent `GamePlayEvent` that the BFF can project into `game_plays` and `life_change_tracking` rows.

Two sub-problems must be solved:

1. **Session buffering**: events for the same game must be grouped by session ID before any projection can occur.
2. **Mid-session flush**: a game may never produce a clean `game_end` event if Arena crashes, the user alt-tabs, or the daemon restarts mid-match. Without a flush strategy, buffered events are silently dropped.

---

## Decision

### Session buffering

The daemon maintains an in-memory session buffer keyed by GRE session ID. Events are appended to the buffer as they arrive. When a `match.game_ended` event is received, the buffer is flushed as a single `GamePlayEvent` to the BFF ingest endpoint and the buffer is cleared.

### Flush threshold

A configurable flush threshold enforces a ceiling on buffer size. When the buffer for a single session reaches `GRE_SESSION_FLUSH_THRESHOLD` events (default: 500), the daemon emits a partial `GamePlayEvent` with `partial: true` in the payload and resets the buffer. This prevents unbounded memory growth during a very long game or if `game_end` never arrives.

`GRE_SESSION_FLUSH_THRESHOLD` is read from the daemon config at startup. Default: 500. Must be ≥ 50 and ≤ 2000. Out-of-range values revert to the default with a warning log.

### Mid-session flush

On daemon shutdown (SIGTERM or SIGINT), the daemon flushes all non-empty session buffers as partial `GamePlayEvent` payloads before exit. This preserves data from interrupted matches.

Additionally, a periodic stale-buffer sweep runs every 10 minutes. Any session buffer with a last-updated timestamp older than `GRE_SESSION_STALE_MINUTES` (default: 15) is flushed as partial and evicted. This handles Arena crashes where the daemon keeps running but the game process has died.

### BFF side

The BFF ingest handler accepts `GamePlayEvent` payloads with `partial: true`. Partial events are written to `game_plays` with a `partial: true` flag. The GRE projector (ticket #1512) must handle partial events gracefully — they contribute to life total history but may not produce a complete win/loss record until a non-partial event arrives for the same match.

---

## Consequences

**Positive:**
- No silent data loss on daemon crash or Arena hang.
- Memory-bounded regardless of game length.
- Partial events preserve life total history even for incomplete matches.

**Negative:**
- The BFF projection layer must handle partial/incomplete `GamePlayEvent` records. Win/loss stats must exclude partial-only matches from aggregate queries.
- The flush threshold config adds a surface area that must be documented in the daemon README.

**Implementation tickets:**
- #1508: `match.game_started` / `match.game_ended` classifiers — session buffering and flush on `game_end`
- New ticket required: daemon config for `GRE_SESSION_FLUSH_THRESHOLD` and stale-buffer sweep
- New ticket required: BFF `game_plays` schema — add `partial BOOLEAN NOT NULL DEFAULT FALSE` column

---

## Alternatives Considered

**Emit each GRE event individually**: rejected. The BFF would receive hundreds of unorrelated rows per game with no join key. Projection would require a second correlation pass on the BFF side, duplicating logic.

**Store raw events and correlate at query time**: rejected. Query-time correlation of 500+ raw rows per game is not tenable at scale and moves complexity to the read path.
