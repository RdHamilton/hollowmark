# v0.3.2 Arch Review — MTGA-Companion → VaultMTG Rename

**Date**: 2026-05-10
**Status**: PM-drafted synthesis, pending arch sign-off
**Audit source**: `docs/engineering/mtga-companion-rename-audit.md` (architect, 2026-05-10)
**Ray's decisions**:
1. Brand casing → `VaultMTG` (capital V, capital MTG, no separator). Lowercase `vaultmtg` for hostnames/paths. Kebab `vault-mtg` only for the GitHub repo slug.
2. Repo rename → Yes. `RdHamilton/MTGA-Companion` → `RdHamilton/vault-mtg`.
3. Database rename → Yes. `mtga_companion` → `vaultmtg`.
4. Archived docs → Rewrite (not banner).

---

## 1. Strategy assessment

The audit's 6-wave sequencing is sound. Ray's four decisions tighten the scope in two important ways:

- **Repo rename = yes** locks in Wave 6 as a real wave (not optional). This couples GitHub URLs, `GONOSUMDB`/`GOPRIVATE` env, contributor remotes, README clone snippets, and the Go module path. All of those are already in the plan but Wave 6 is now mandatory, not optional.
- **DB rename = yes** elevates V32-W6-1 from a nice-to-have to a coordinated downtime change. It must run AFTER the SSM cutover (W3-2) but does not gate Wave 6's other tickets — they can run in parallel.

The sequencing graph holds:
```
W0 (ADR) ──> W1 (docs) ─┐
                        ├──> W3 (SSM/EC2) ──> W5 (S3/Vercel) ──┐
W0 ──> W2 (code) ───────┘                                      ├──> W6 (repo+DB+CI)
                       ──> W4 (daemon, gates W2-4) ────────────┘
```

W1 and W2 may run in parallel after W0. W3 and W4 may run in parallel after W2. W5 follows W3. W6 last.

---

## 2. Brand casing convention (Ray's decision #1)

Per Ray: `VaultMTG` is canonical. Operationalized as:

| Context | Canonical form | Notes |
|---|---|---|
| Prose, marketing, brand strings | `VaultMTG` | "Welcome to VaultMTG" |
| Go identifiers, exported types | `VaultMTG` | Matches Go convention for acronyms |
| Go module path | `github.com/RdHamilton/vault-mtg` | GitHub org/repo segments are kebab |
| npm `package.json` `name` | `vaultmtg` | npm names don't allow uppercase |
| PostgreSQL DB name | `vaultmtg` | Lowercase convention |
| AWS SSM path prefix | `/vaultmtg/` | Already partially in use for staging |
| AWS S3 bucket | `vaultmtg-deploy-artifacts-staging` | DNS-safe, lowercase |
| systemd unit | `vaultmtg-bff`, `vaultmtg-bff-staging` | Lowercase |
| EC2 Name tag | `vaultmtg-bff-production` | |
| macOS launchd label | `com.vaultmtg.daemon` | Reverse-DNS form |
| macOS keychain service | `com.vaultmtg.daemon` | Same as launchd |
| Windows scheduled task | `VaultMTG-Daemon` | Pascal acceptable on Windows |
| `~/.config` dir (mac/Linux) | `~/.vaultmtg/` | |
| `%APPDATA%` dir (Windows) | `%APPDATA%\vaultmtg\` | |
| `%ProgramFiles%` dir (Windows) | `%ProgramFiles%\VaultMTG\` | Pascal because Windows convention |
| Daemon binary | `vaultmtg-daemon` | Kebab-lowercase |
| User-Agent string | `VaultMTG/<version>` | Brand visibility |
| Custom URL scheme | `vaultmtg://callback` | Lowercase per RFC 3986 |
| Domain | `vaultmtg.app` | Already canonical |
| GitHub repo slug | `vault-mtg` | Per Ray's decision #2 — kebab convention |

ADR-021 (V32-W0-1) must table this and treat it as the source of truth for every subsequent rename PR. Reviewers should reject any PR that deviates without an explicit ADR amendment.

---

## 3. Risks I want flagged for Ray

### 3.1 Daemon migration is the single highest user-facing risk
Wave 4 cuts across user devices we don't own. A bug in the macOS launchd label migration produces TWO daemons running simultaneously — both poll the same MTGA log file, both POST to the BFF, both consume telemetry. This is silent, hard to detect, and damages the product analytics layer (PostHog event volumes) even more than user trust.

**Mitigation**:
- Wave 4 install scripts MUST detect existing service identifiers and unload/unregister them BEFORE registering the new ones (V32-W4-2 and V32-W4-3 ACs cover this — verify in code review)
- Smoke-test on fresh VMs AND on VMs with the old daemon already installed (V32-W4-2/W4-3 ACs)
- Add server-side defensive logic: BFF rejects duplicate ingest from the same `account_id` if the daemon registers two different machine fingerprints within 60s

### 3.2 SSM cutover order matters
If V32-W3-2 (workflow update) ships before V32-W3-1 (parameter mirror), the next deploy 500s on missing SSM params. The ticket list locks the order with a `Dependencies` field, but PM and PR review must enforce.

### 3.3 GitHub repo rename + Go module rename + CI auth env are coupled
Ray's decision #2 (rename repo) means V32-W2-1 (Go module path) MUST use `github.com/RdHamilton/vault-mtg` as the new path — but the repo doesn't actually have that name until V32-W6-2 ships. GitHub's redirect handles HTTP and `git clone` but NOT the Go module proxy in some configurations.

**Mitigation**:
- Sequence V32-W6-2 (repo rename) BEFORE V32-W2-1 (module path), OR
- Ship V32-W2-1 with the new module path and accept that `go mod tidy` fails for a brief window between W2-1 merging and W6-2 running, OR (recommended)
- Move V32-W6-2 (GitHub rename) earlier — before V32-W2-1 — and treat the repo rename as a Wave 1 prerequisite alongside ADR-021. This restructures Wave 6 but is the safest order.

**Recommendation to Ray**: Do the GitHub repo rename as the FIRST action after ADR-021 lands. Then everything else is internally consistent. I have NOT updated the ticket-list.md to reflect this — it needs a yes/no from Ray. If yes, V32-W6-2 moves to Wave 0.5 (between ADR and Wave 1). V32-W6-3 (GONOSUMDB/GOPRIVATE) moves with it.

### 3.4 DB rename is high-risk and isolated — schedule a maintenance window
V32-W6-1 requires BFF down. Run on staging first, validate, then production during a low-traffic window (Sunday 04:00 ET historical low). Take a backup. Make a rollback runbook (just `ALTER DATABASE "vaultmtg" RENAME TO "mtga_companion"`).

### 3.5 Frontend localStorage migration shim is silent if it breaks
V32-W2-2 ships a one-shot migration. If the shim has a bug, every user loses dev-mode flags, filters, accordion state, and feels like the rename "broke their settings." Test scenarios:
- Old keys present, new keys absent → migrate, then mark migrated
- New keys already present (already migrated, possible if user opens app on multiple devices) → leave alone, mark migrated
- Old keys absent (new user) → no-op, mark migrated
- Old AND new keys present → prefer new keys, log warning to Sentry

PostHog should fire a `frontend.localstorage.migration.{success|noop|error}` event so we can observe the rollout.

### 3.6 17lands export metadata field is a public contract
`ExportedFrom: "MTGA-Companion"` is read by 17lands when a user uploads a draft. Changing it to `"VaultMTG"` may cause 17lands to either reject the upload or attribute it to a different source. Action item: ping 17lands before V32-W1-6 ships.

### 3.7 The audit excluded two companion repos
Per audit §1 — `mtga-companion-infra` and `mtga-companion-web` are not audited. Wave 3 (CloudFormation tag updates) and Wave 5 (S3 bucket if owned by CFN) reference companion infra repo PRs that don't exist yet. PM should file tickets in those repos OR audit them and add to v0.3.2 as carry-forward.

---

## 4. ADRs to write before implementation

Only one ADR is required: **ADR-021 — Rename to VaultMTG** (V32-W0-1). It must include:
- The rename decision and rationale (audit summary)
- Casing convention table (section 2 above)
- 6-wave sequencing (or 5+1 if Ray accepts §3.3 recommendation)
- Decisions on the four open questions (Ray's #1–#4)
- Per-wave acceptance criteria summary
- Rollback strategy per wave

All other rename details fit in the ticket bodies + audit. No additional ADRs needed.

---

## 5. Recommendations to Ray (decisions outstanding)

1. **Reorder V32-W6-2 (GitHub rename) to be Wave 0.5**, before V32-W2-1 ships. This eliminates the Go module path inconsistency window. — **Recommend yes**.
2. **Confirm 17lands integration impact** before V32-W1-6 changes the `ExportedFrom` field. A simple email to 17lands maintainer should suffice. — **Recommend send email this week**.
3. **Confirm companion repos `mtga-companion-infra` and `mtga-companion-web` will be handled in v0.3.2 or deferred.** If deferred, file a v0.3.3 ticket explicitly. — **Recommend defer to v0.3.3 and file a tracking ticket** (rename of the actual app is the priority; sibling repos are lower-traffic and can lag).
4. **Confirm the DB-rename maintenance window** (suggested Sunday 04:00 ET). PM will coordinate the announcement to the closed beta cohort.
5. **Confirm the rhamiltoneng portfolio CloudFormation tag** stays as `vaultmtg` or moves to a portfolio-specific value (V32-W3-5 flagged this — the tag is on a portfolio site, not VaultMTG).

---

## 6. Sign-off

- [ ] Architect (Ray): reviewed, agrees with recommendations 1–5 above
- [x] PM (Najah): drafted 2026-05-10
- [ ] Lead engineer: aware of dependency graph and high-risk tickets

Once architect signs off, PM updates ticket-list.md (potentially reordering W6-2 to Wave 0.5) and project-manager creates the GitHub issues.
