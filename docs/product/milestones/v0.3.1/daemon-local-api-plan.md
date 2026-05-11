# Daemon Local HTTP API — Implementation Plan

**Status:** Phase 0 in progress (2026-05-11)
**Owner:** backend-engineer / front-engineer
**Related:** ADR-009, ADR-020, #1315

## Problem

The VaultMTG SPA (`frontend/src/services/api/*.ts`) imports `daemonClient` for
**60+ distinct API paths** across 14 modules (cards, collection, decks, drafts,
matches, meta, ml-suggestions, notes, opponents, quests, settings, standard,
system, gameplays). `daemonClient` is configured to talk to a local HTTP server
at `VITE_DAEMON_URL` (defaulting to `http://localhost:8080/api/v1`).

Separately, `frontend/src/pages/Setup.tsx` polls a hardcoded
`http://localhost:9001/health` endpoint to drive its "daemon connected"
indicator.

**The v0.3.x daemon currently serves no HTTP at all.** It is a one-way
log-reader that watches `Player.log` and POSTs events to the cloud BFF. The
result, observed in the closed-beta SPA console:

- Constant `ERR_CONNECTION_REFUSED` for every daemonClient call
- "Waiting for auth" spinner that never goes green (Setup.tsx)
- Falls back to BFF endpoints that don't exist, producing CORS errors

## Goal

Restore the SPA ↔ daemon contract so the closed beta (2026-08-18) ships a
working experience. Three phases, sequenced by user-visible impact per hour
of engineering effort.

---

## Phase 0 — Health endpoint (2026-05-11, ~1h)

**Goal:** Make the "daemon connected" indicator go green so the SPA recognizes
a running daemon. No data features yet, just liveness.

**Scope:**

- New package `services/daemon/internal/localapi/`
- HTTP server bound to `127.0.0.1:9001` (loopback only, never an external interface)
- One endpoint:
  - `GET /health` → `{ status: "ok", version, session_id, started_at, account_id }`
- CORS configured for the production + staging SPA origins and `localhost:*` for dev
- Started inside `daemon.Service.Run`, shut down cleanly on context cancel

**Exit criteria:**

- Setup.tsx's connection check passes against a running daemon
- `curl http://localhost:9001/health` returns 200 with the expected JSON
- Unit tests cover happy-path, CORS preflight, method rejection, lifecycle

**Out of scope:**

- Match/draft/deck data endpoints (Phase 2)
- `system/status`, `system/version`, etc. (Phase 1)

---

## Phase 1 — System status endpoints (tomorrow, ~3h)

**Goal:** Quiet the localhost connection-refused spam in the console by serving
the remaining `system/*` paths that the SPA polls continuously, regardless of
which page the user is on.

**Scope (on the same loopback server, port 9001):**

- `GET  /api/v1/system/status` → `{ daemon_online, last_dispatch_at, bff_reachable, session_id }`
- `GET  /api/v1/system/health` (alias of `/health` under the API prefix)
- `GET  /api/v1/system/version` → daemon version + build SHA
- `GET  /api/v1/system/account` → `{ account_id, daemon_id }` from `daemon.json`
- `GET  /api/v1/system/database/path` → empty/null (no local DB exists in v0.3.x)
- `POST /api/v1/system/daemon/connect`, `disconnect` → no-ops returning `{ status: "ok" }` (daemon manages its own lifecycle)
- `GET  /api/v1/system/daemon/status` → same payload as `/system/status`

**Unify ports:** kill the dual-port design. The daemon serves on `9001` only.
Frontend's `VITE_DAEMON_URL` defaults to `http://localhost:9001/api/v1`. The
hardcoded `http://localhost:9001/health` in `Setup.tsx` stays. All `daemonClient`
calls target `9001` after this change.

**Frontend changes:**

- `frontend/src/services/daemonClient.ts:24` — change fallback to `http://localhost:9001/api/v1`
- Audit any other hardcoded `localhost:8080` references and update
- Build-time env: drop unused `VITE_DAEMON_URL` if no longer overridden anywhere

**Exit criteria:**

- Open the SPA against a running daemon — no `ERR_CONNECTION_REFUSED` in the console for `system/*` paths
- Tests for each endpoint
- Daemon Sentry coverage on the local-API panic recovery (covered by Wave 9 #1832)

---

## Phase 2 — Data feature migration (week of 2026-05-12, scoped per feature)

**Goal:** SPA data features (match history, deck builder, draft analytics,
collection, etc.) actually return real data.

**Approach:** Audit the 60 paths into three buckets:

### Bucket A — Already exists on BFF (~30 paths, frontend-only change)

The BFF already implements equivalents under `/api/v1/history/*`, `/api/v1/stats/*`,
`/api/v2/{history,decks,collection}/*`. For these, the fix is purely frontend:

1. Switch the module's import from `daemonClient` → `apiClient`
2. Update the path to the BFF version (`/matches` → `/v1/history/matches`, etc.)
3. Handle any response-shape deltas

**Estimated effort:** 30 min/module × ~10 modules = 5h
**Owner:** front-engineer

### Bucket B — Doesn't exist on BFF yet (~20 paths, daemon-proxy for beta)

For paths the BFF doesn't yet support (deck builder operations, ML suggestions,
build-around, etc.), the fastest path to closed beta is a daemon proxy:

1. Daemon's local API adds the endpoint
2. Implementation forwards the request to the BFF as-is, using the daemon's
   api_key as Bearer
3. BFF implements the endpoint when it can

This gives the SPA an immediate working endpoint while the BFF catches up.
The daemon becomes a thin auth-proxy for these paths — no business logic.

**Estimated effort:** 20 min/path × 20 paths = 7h daemon proxy + parallel BFF work
**Owner:** backend-engineer (daemon), backend-engineer (BFF)

### Bucket C — Genuinely local/live state (~10 paths)

In-flight match/draft data that only the running daemon can answer:

- `/api/v1/drafts/grade-pick` — needs the current draft pool, live during draft
- `/api/v1/decks/build-around/suggest-next` — needs current pack state
- `/api/v1/matches/current` (if it exists)

For these, the daemon must hold state and answer locally. Schema TBD per
endpoint.

**Estimated effort:** scoped per endpoint; probably 1h each
**Owner:** backend-engineer (daemon)

---

## Tickets

To be filed under Wave 8.5 / pre-beta hardening:

- `daemon: Phase 0 — local /health endpoint` — *in progress, this branch*
- `daemon: Phase 1 — system/* endpoints + port unification`
- `frontend: Phase 1 — switch VITE_DAEMON_URL default to localhost:9001`
- `daemon+bff: Phase 2 — audit 60 daemonClient paths into Buckets A/B/C`
- `frontend: Phase 2 Bucket A — migrate ~30 paths from daemonClient to apiClient`
- `daemon: Phase 2 Bucket B — implement proxy endpoints for ~20 paths`
- `daemon: Phase 2 Bucket C — implement live-state endpoints for ~10 paths`

## Non-goals

- Offline mode for the SPA — out of scope for v0.3.1; revisit after closed beta
- Local SQLite/cache in the daemon — would require an ingest/sync rewrite
- Daemon ↔ BFF binary protocol — the daemon stays an HTTP client
