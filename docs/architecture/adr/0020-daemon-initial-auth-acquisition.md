# ADR-020: Daemon Initial Auth Acquisition: PKCE Browser Redirect

**Date**: 2026-05-09
**Status**: Accepted
**Deciders**: Ray Hamilton
**Supersedes**: ADR-011 § "First-run config" (the SPA `/setup` mints the first API key path)
**Related**: ADR-009 (Clerk auth), ADR-011 (daemon distribution strategy)

---

## Context

ADR-011 specified that the daemon's first-run pairing flow works as follows:
on missing `daemon.json`, the daemon directs the user to `vaultmtg.app/setup`,
and the SPA `/setup` page mints a Clerk API key and posts it to the daemon's
local health endpoint.

This requires the daemon to be running and reachable on localhost before the
user can authenticate. It also requires the SPA to hold Clerk session state
and make an outbound call to the daemon's local port — a cross-origin call
from a web page to `localhost` that is increasingly restricted by browsers
and blocked by enterprise firewalls.

Ray's decision is to replace this SPA-mint-key path with a **PKCE browser
redirect** flow where the daemon itself drives OAuth login directly. This ADR
documents that decision and supersedes the first-run pairing section of ADR-011.

---

## Decision

**The daemon acquires its initial Clerk API key through a PKCE OAuth
browser-redirect flow. The daemon opens the system browser, the user logs
in via Clerk, the daemon captures the auth code on a localhost callback,
exchanges it for a Clerk session token, calls `POST /v1/daemon/register`
on the BFF, and the BFF mints and returns the first API key. The daemon
stores the key in the OS keychain.**

### Flow (step by step)

1. **First-run detection** — daemon starts, finds no `daemon.json` (or
   finds a stub with no `api_key`).

2. **PKCE setup** — daemon generates a cryptographically random
   `code_verifier` (32 bytes, base64url) and derives `code_challenge`
   (SHA-256 of verifier, base64url).

3. **Localhost callback server** — daemon binds a one-shot HTTP server on
   **fixed port `51423`** (one retry on `51424` if busy). The redirect URI is
   `http://localhost:51423/oauth/callback` (or `51424` on retry).
   Clerk OAuth redirect URIs **must be registered for both**:
   - `http://localhost:51423/oauth/callback`
   - `http://localhost:51424/oauth/callback`

4. **Browser open** — daemon constructs the Clerk OAuth authorization URL
   with `response_type=code`, `code_challenge`, `code_challenge_method=S256`,
   and `redirect_uri`. Daemon calls the OS "open URL" command
   (`open` on macOS, `start` on Windows) to launch the system browser.

5. **User authenticates** — user logs in to their Clerk account in the
   browser. Clerk redirects to `localhost:PORT/callback?code=AUTH_CODE`.

6. **Code capture** — daemon's callback server receives the request,
   extracts `code`, shuts down the listener.

7. **Token exchange** — daemon POSTs `code` + `code_verifier` to Clerk's
   token endpoint. Clerk returns a session token (JWT).

8. **Daemon registration** — daemon calls `POST /v1/daemon/register` on
   the BFF with the Clerk JWT in `Authorization: Bearer`. BFF verifies the
   JWT via `clerk-sdk-go v2`, creates (or retrieves) a per-machine API key
   scoped to the authenticated user's `account_id`, and returns the key in
   the response body. See § "POST /v1/daemon/register Wire Format" below for
   the complete request/response contract.

9. **Keychain storage** — daemon stores the API key in the OS keychain
   using `go-keyring` (service name: `com.mtga-companion.daemon`, key: `api-key`).
   The key is NOT written to `daemon.json` in plaintext.
   `com.mtga-companion.daemon` is the canonical service name — this resolves
   the conflict between this ADR (which previously said `mtga-companion`) and
   ticket #1651 (`com.mtga-companion.daemon`). All implementations must use
   `com.mtga-companion.daemon`.

10. **Config write** — daemon writes `daemon.json` with `cloud_api_url`
    and `keychain: true` (flag indicating the API key lives in the keychain,
    not the config file). All subsequent restarts read from the keychain.

### What this supersedes in ADR-011

ADR-011 § "First-run config: zero installer prompts" stated:

> On first launch the daemon detects a missing `daemon.json`, writes a stub
> config, and immediately directs the user to `https://vaultmtg.app/setup`.
> The setup flow on the SPA mints a Clerk API key (per ADR-009) and writes
> the config to disk via the daemon's local health endpoint.

That path is replaced by the PKCE flow above. The SPA `/setup` page:

- **Retains**: daemon status polling (health endpoint checks)
- **Retains**: "First-time install warnings" section (Gatekeeper / SmartScreen)
- **Removes**: the API key minting and localhost-write flow
- **Removes**: TBD-G from ADR-011 ("daemon pairing flow that mints a Clerk
  API key and posts it to the daemon's local health endpoint")

TBD-G is replaced by the three ADR-020 implementation tickets below.

---

## Consequences

### Positive

- **No localhost cross-origin calls from SPA.** The daemon drives its own
  auth; the browser is a dumb redirect target, not a client making API calls
  to localhost.
- **Clerk session token never leaves the daemon process.** The JWT is
  exchanged and discarded; only the API key is persisted, and only in the
  OS keychain.
- **Works headlessly.** If the user is on a server or a machine without a
  browser, the daemon can print the authorization URL and accept a
  `?code=` paste — same PKCE flow, no browser dependency.
- **Simpler SPA.** The SPA no longer needs to hold daemon-pairing state,
  make localhost calls, or handle cross-origin errors.

### Negative / Trade-offs

- **Requires BFF endpoint.** `POST /v1/daemon/register` is a new endpoint.
  It must be protected by Clerk JWT verification and rate-limited to prevent
  key-minting abuse.
- **Requires OS keychain dependency.** `go-keyring` adds a CGo dependency
  on Linux (not a target platform today) and has platform-specific behavior.
  macOS Keychain and Windows Credential Manager are well-supported.
- **PKCE callback port conflicts.** Ephemeral port selection must handle the
  (rare) case that the chosen port is in use. Implementation must retry with
  a different port.

### Neutral

- The daemon binary size increases negligibly (one HTTP listener + PKCE
  crypto, both in stdlib).
- The user experience is unchanged from the user's perspective: they
  double-click the installer, the browser opens, they log in, the browser
  closes, the daemon is paired. Identical to Spotify/Slack desktop login.

---

## Implementation Tickets

| Ticket | Scope | Owner |
|---|---|---|
| **ADR020-1** | `feat(daemon): implement PKCE OAuth browser-redirect login flow` — generate verifier/challenge, bind localhost callback, open system browser, capture auth code, exchange for Clerk session token | backend-engineer |
| **ADR020-2** | `feat(daemon): store Clerk API key in OS keychain (go-keyring)` — write/read/delete from macOS Keychain and Windows Credential Manager; fallback error handling if keychain unavailable | backend-engineer |
| **ADR020-3** | `feat(bff): add POST /v1/daemon/register endpoint` — accept Clerk JWT in Authorization header, verify via `clerk-sdk-go v2`, mint per-machine API key scoped to account_id, return key in response body; rate-limit to 5 req/min per user | backend-engineer |

---

## Confirmed Decisions (Wave 0 Review — 2026-05-09)

The following decisions were confirmed by Ray Hamilton during the Wave 0 architecture review
and are now binding. They resolve open questions and conflicts present in the original draft.

---

### API Key Scoping (Beta)

One API key per user for the beta period. The key is scoped to the authenticated
`account_id` and stored in the OS keychain under service name `com.mtga-companion.daemon`.
This resolves the conflict between the original draft of this ADR (which used `mtga-companion`)
and ticket #1651 — **`com.mtga-companion.daemon` is canonical**.

---

### API Key Revocation and Re-Pair Behavior

**Silent re-use on re-pair.** If a valid API key already exists in the keychain when the
daemon runs `POST /v1/daemon/register`, the BFF returns the existing key (HTTP 200) without
creating a new one. A new key is only issued when:

1. No key exists in the keychain for this user/device combination, **or**
2. The existing key has been revoked server-side (BFF returns 401 on the existing key).

When the daemon receives a 401 using its cached API key, it must re-run the full PKCE flow
to obtain a new Clerk session token and then call `POST /v1/daemon/register` again.

---

### POST /v1/daemon/register Wire Format

#### Request (daemon → BFF)

```
POST /v1/daemon/register
Authorization: Bearer <Clerk session JWT>
Content-Type: application/json
```

```json
{
  "clerk_user_id": "user_xxxxx",
  "device_id": "<uuid-v4 generated on first install, stored in keychain>",
  "platform": "darwin" | "windows",
  "daemon_version": "0.3.1"
}
```

- `device_id`: UUID v4 generated once on first install and persisted in the keychain alongside
  the API key. Never regenerated unless the keychain entry is deleted.
- `platform`: `"darwin"` or `"windows"` — verbatim GOOS values.
- `daemon_version`: the running daemon's semver string (from build-time `ldflags`).

#### Response — 201 Created (new key issued)

```json
{
  "api_key": "vlt_live_xxxxx",
  "account_id": "<uuid>"
}
```

#### Response — 200 OK (existing valid key returned; re-use)

```json
{
  "api_key": "vlt_live_xxxxx",
  "account_id": "<uuid>"
}
```

#### Rate Limiting

5 requests per minute per Clerk user ID. Excess requests receive:

```
HTTP 429 Too Many Requests
Retry-After: <seconds>
```

---

### macOS Quarantine Clearing

The `.pkg` postinstall script **must** clear the macOS quarantine attribute from the
daemon binary immediately after installation:

```bash
xattr -dr com.apple.quarantine /usr/local/bin/vaultmtg-daemon
```

Failure to clear the quarantine attribute causes macOS Gatekeeper to block the daemon
on first run with an "unidentified developer" error that requires manual user intervention.
This step must be included in the postinstall script for every `.pkg` release.

**This is an acceptance criterion for ticket #1640.** See that ticket for full ACs.

---

## Alternatives Considered

### A. SPA mints API key and POSTs to daemon localhost (ADR-011 original)

**Rejected (superseded by this ADR).** Browser-to-localhost cross-origin
calls are restricted by modern browsers (mixed-content and CORS policies).
Enterprise firewalls block localhost ports. The SPA holding Clerk session
state and making outbound calls to daemon ports is an architectural anti-pattern.

### B. User copies API key from SPA and pastes into daemon CLI

**Rejected.** Requires the user to understand what an API key is, find it
in the SPA, copy it without leaking it, and paste it into a terminal or
config file. Unacceptable UX for the target audience (MTG Arena players,
not engineers).

### C. Device Authorization Grant (OAuth Device Flow)

**Considered.** The device flow is designed for devices without browsers.
The daemon does have access to a browser (it can `open` URLs), so PKCE
is the more standard choice. PKCE also avoids the polling loop that device
flow requires. Device flow can be used as a fallback for headless
environments in a future iteration.

---

## References

- ADR-009 — Clerk user auth provider decision
- ADR-011 — Daemon distribution strategy (superseded first-run section)
- [Clerk PKCE documentation](https://clerk.com/docs/backend-requests/resources/session-tokens)
- [go-keyring](https://github.com/zalando/go-keyring) — cross-platform OS keychain library
- [RFC 7636](https://www.rfc-editor.org/rfc/rfc7636) — Proof Key for Code Exchange
