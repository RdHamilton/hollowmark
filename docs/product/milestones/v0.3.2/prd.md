# PRD: v0.3.2 — Rename MTGA-Companion → VaultMTG

**Author**: PM (Najah)
**Date**: 2026-05-10
**Status**: Draft, pending architect sign-off
**Audit source**: `docs/engineering/mtga-companion-rename-audit.md`
**Arch review**: `docs/product/milestones/v0.3.2/arch-review.md`
**Project**: #34 (`PVT_kwHOABsZ684BXSA8`)
**Milestone**: #71 (v0.3.2)

---

## Problem statement

The product is named **VaultMTG** but the codebase is named `MTGA-Companion`. The
gap exists in 1,134 places across 302 files: import paths, AWS SSM parameters,
S3 buckets, EC2 tags, systemd units, daemon launchd labels, Windows scheduled
tasks, frontend localStorage keys, npm package name, PostgreSQL DB name, Go
module path, GitHub repo slug, public docs, ADRs, and ~150 archived planning
documents.

This naming gap creates four concrete costs:
1. **Brand confusion at first contact**. The closed beta launches 2026-08-18.
   New users encountering `MTGA-Companion` in install scripts, daemon paths,
   error messages, or download URLs will not connect them to the product they
   signed up for.
2. **Operational ambiguity**. AWS resources named `mtga-companion-*` in cost
   reports do not match the brand, slowing finance + ops triage.
3. **Cumulative friction**. Every new doc, ticket, ADR, or PR description
   either uses the old name (perpetuating the gap) or invents new convention
   (creating drift). There is no single source of truth.
4. **Repo-rename window closing**. GitHub's automatic redirect for renamed
   repos is reliable in the short term but degrades over years. Doing the
   rename now, before the closed beta brings external traffic to public links,
   minimizes blast radius.

The longer this stays unresolved, the more places it has to be fixed. v0.3.2
exists to close the gap.

---

## Goals

1. **Zero functional changes**. v0.3.2 is a rename. No new features, no API
   contract changes (one transitional alias is acceptable in `/health`),
   no schema changes beyond renaming a DB.
2. **Single source of truth**. ADR-021 documents the canonical brand-casing
   convention. Every subsequent PR cites it.
3. **No silent user-state loss**. Frontend localStorage migration shim ports
   user preferences. Daemon migration ports config dirs and keychain entries.
4. **Sequenced infra cutover**. SSM parameters, EC2 tags, S3 buckets, and the
   PostgreSQL DB rename in a documented order with rollback per step.
5. **GitHub repo renamed** with all CI auth env updated and contributor remotes
   re-pointed.
6. **The PostgreSQL production DB renamed** during a scheduled maintenance
   window with backup + rollback runbook.

## Non-goals

- No marketing site changes (separate repo, separate ticket).
- No `mtga-companion-infra` or `mtga-companion-web` companion-repo work.
  Carry-forward to v0.3.3.
- No new features or product changes. Pure rename.
- No mobile app work (no mobile app exists).
- No cleanup of the rhamiltoneng portfolio CloudFormation stack except its
  `Project` tag (the portfolio site itself is out of scope for VaultMTG).
- No changes to PostHog event names (those are user-facing analytics contracts;
  retain `mtga_companion.*` event names for historical continuity unless we
  decide to migrate in a future milestone).

---

## Target users

This is an internal/operational milestone. User impact is indirect but real:

- **Closed-beta users (post-2026-08-18)**: see the brand consistently in install
  flows, download links, support docs, and error messages.
- **Existing dev/test daemon installations** (Ray + a small handful of internal
  testers): silently migrated by Wave 4 install scripts. Old config preserved
  during transition.
- **Contributors / agents**: must update their `git remote` after Wave 6 repo
  rename. GitHub redirect handles a transition period.

---

## Success metrics

**Primary**:
- Zero `mtga-companion`, `MTGA-Companion`, `MTGACompanion`, `mtga_companion`
  references remain after Wave 6 (verified by `rg` sweep, except the audit file
  itself which stays as a historical record).

**Secondary**:
- Zero rollback events during the SSM cutover (W3) or DB rename (W6-1).
- Zero user-reported daemon issues in the 7 days after Wave 4 ships.
- Frontend localStorage migration shim emits `success` for every user's first
  post-W2-2 session (PostHog: `frontend.localstorage.migration.success`).
- All daemon assets visible under both names for one release window, then only
  `vaultmtg-daemon-*` from W4-5 onward.
- CI green continuously through all 6 waves (no rename-induced pipeline breaks).

**Tertiary**:
- Internal team friction: every contributor's `git remote` updated within 7 days
  of repo rename (Ray collects via Slack/standup).

---

## The 6-wave delivery plan

### Wave 0 — Decision and ADR (1 ticket)
- Write **ADR-021 — Rename to VaultMTG**: capture decisions, casing convention
  table, 6-wave sequencing, rollback strategies.
- Gate: every subsequent ticket cites ADR-021. PRs that deviate are rejected.

### Wave 1 — Pure docs and low-risk strings (8 tickets)
- All non-archived markdown files
- Archived markdown files (Ray's decision #4: rewrite, not banner)
- ADR archive (preserve dates and decisions; only swap brand strings)
- Backend runtime strings (User-Agent, `service` field with one-release alias,
  17lands export metadata)
- DB seed dev-user email (new migration, not editing existing)
- Status + report docs

Risk: low. No functional changes. Parallel-safe across the 8 tickets.

### Wave 2 — In-repo code rename (5 tickets)
- Go module path rewrite + bulk import update + `go mod tidy`
- `package.json` name + frontend localStorage migration shim
- `cmd/mtga-companion/` directory rename + binary + help text
- Frontend daemon download URLs (gated by Wave 4 W4-1)
- In-repo daemon config + flight recorder paths (with backwards-compat shim)

Risk: medium. Mechanical for module path, careful for frontend localStorage
(silent-loss class). Can run in parallel with Wave 1.

### Wave 3 — SSM + EC2 cutover (5 tickets)
- Mirror SSM parameters under `/vaultmtg/{env}/`
- Update workflows + scripts to read new path
- EC2 systemd unit + filesystem path cutover (with symlink fallback)
- EC2 Name tag + tag-based discovery scripts
- Delete old `/mtga-companion/*` SSM tree + retag rhamiltoneng CFN tag

Risk: high. Operational, irreversible deletes at the end. Sequenced
strictly: 3-1 → 3-2 → 3-3 → 3-4 → 3-5.

### Wave 4 — Daemon rename (5 tickets) — single highest user risk
- Daemon release ships under both old and new asset names (one window)
- macOS migration (launchd label + keychain + paths + plist)
- Windows migration (scheduled task + paths + NSIS)
- Linux migration (systemd unit + paths)
- Drop legacy artifact names two releases after W4-1

Risk: high. Cuts across user devices. Install scripts MUST detect existing
identifiers and unload before registering new ones (otherwise two daemons run).
Smoke-tested on clean VMs AND VMs with old daemon installed.

### Wave 5 — S3 bucket rename + remaining infra (3 tickets)
- Create `vaultmtg-deploy-artifacts-staging` and dual-write
- Empty + delete old bucket (Ray confirms before merge)
- Vercel project rename + ALLOWED_ORIGINS cutover

Risk: medium. S3 deletion is irreversible — Ray confirms.

### Wave 6 — Repo rename, DB rename, and CI auth env (4 tickets)
- Rename PostgreSQL DB during maintenance window (staging first, then prod;
  backup taken; rollback runbook in place)
- Rename GitHub repo `MTGA-Companion` → `vault-mtg`
- Update GONOSUMDB/GOPRIVATE in all workflows
- Final sweep + zero remaining references verified

Risk: high. Coordinates downtime, contributor friction, and CI auth env.

**Open arch recommendation**: Move repo rename (W6-2) to Wave 0.5 — between
ADR-021 and Wave 1 — to eliminate the Go-module-path inconsistency window.
Pending Ray's call.

---

## Risks and mitigations (incorporating arch review §3)

| # | Risk | Severity | Mitigation |
|---|---|---|---|
| 1 | Two daemons running simultaneously after macOS migration bug | High (silent telemetry damage) | Install scripts unload old launchd label first; smoke-test on existing-install VMs; BFF defensive logic rejects duplicate ingest |
| 2 | SSM cutover order wrong → BFF 500s on next deploy | High (production outage) | Strict ticket dependency: 3-1 → 3-2 → 3-3 → 3-4 → 3-5; PR review enforces |
| 3 | Go module path mismatch with not-yet-renamed repo | Medium (`go mod tidy` failures) | Move repo rename to Wave 0.5 (pending Ray decision); OR sequence W6-2 before W2-1 |
| 4 | DB rename causes prolonged downtime | High (data + downtime) | Staging first; backup; maintenance window; rollback runbook (`ALTER DATABASE` reverse) |
| 5 | Frontend localStorage migration shim silently corrupts user state | Medium (user trust) | Test all 4 scenarios (old-only, new-only, both, neither); PostHog event on every migration outcome; Sentry on errors |
| 6 | 17lands export `ExportedFrom` field rejected after rename | Medium (broken integration) | Email 17lands maintainer before W1-6 |
| 7 | Companion repos (`mtga-companion-infra`, `mtga-companion-web`) not audited | Low (out of scope but causes drift) | File v0.3.3 carry-forward ticket explicitly |
| 8 | S3 bucket deletion is irreversible | Medium (operational) | Run new bucket for one full release first; Ray confirms in PR before deletion |
| 9 | Repo rename breaks contributor workflows | Medium (friction) | GitHub redirect handles ~1 yr; Ray broadcasts to all agents + contributors when rename ships; CONTRIBUTING.md updated in W1-1 |
| 10 | CI breaks mid-wave because `GONOSUMDB`/`GOPRIVATE` references old repo | High (merges blocked) | Sequence W6-3 immediately after W6-2; verify on test PR before merging |

---

## User stories

### S1: Internal contributor cloning the repo for the first time after rename
> **As a** contributor (engineer or agent),
> **I want** the README clone snippet to use the canonical new repo URL,
> **so that** I clone from `RdHamilton/vault-mtg` directly without relying on the GitHub redirect.

**ACs** (covered by V32-W1-1, V32-W6-2):
- [ ] README's `git clone` snippet uses `git@github.com:RdHamilton/vault-mtg.git`
- [ ] CONTRIBUTING.md uses the same URL
- [ ] After Wave 6 ships, `git clone git@github.com:RdHamilton/vault-mtg.git` succeeds

### S2: Closed-beta user installing the daemon for the first time after rename
> **As a** closed-beta user (post-2026-08-18),
> **I want** the daemon to install with VaultMTG branding throughout — install dialog, file paths, log file, scheduled task name —
> **so that** the product I'm using matches the product I signed up for.

**ACs** (covered by V32-W4-1 through V32-W4-4):
- [ ] Daemon install package signed under VaultMTG identity
- [ ] macOS: `launchctl list | grep vaultmtg` returns `com.vaultmtg.daemon`
- [ ] Windows: scheduled task named `VaultMTG-Daemon` exists
- [ ] Linux: config dir at `~/.vaultmtg/`
- [ ] Frontend download links serve `vaultmtg-daemon-*` artifacts
- [ ] Support docs at `docs/support/daemon-troubleshooting.md` reference VaultMTG paths

### S3: Existing dev daemon user (Ray, internal testers) upgrading after Wave 4
> **As an** existing daemon user with `~/.mtga-companion/daemon.json` already on disk,
> **I want** the new daemon installer to migrate my config and unregister the old daemon automatically,
> **so that** I don't have two daemons running and don't lose my settings.

**ACs** (covered by V32-W4-2, V32-W4-3, V32-W4-4):
- [ ] Old launchd label / scheduled task / systemd unit detected and unregistered before new one is installed
- [ ] Config dir migration: contents copied from old to new path
- [ ] Keychain entries migrated (macOS)
- [ ] No duplicate ingest events to BFF in 60s after install
- [ ] Smoke-tested on a VM with the old daemon already installed

### S4: Frontend user opening the app after Wave 2 ships
> **As an** existing frontend user with `mtga-companion-developer-mode=true` in localStorage,
> **I want** my dev-mode flag preserved after the rename,
> **so that** I don't have to reconfigure my settings.

**ACs** (covered by V32-W2-2):
- [ ] On first mount post-W2-2, migration shim copies all old-key values to new keys
- [ ] Old keys deleted after migration
- [ ] Re-runs of the shim are no-ops (gated on `vaultmtg-migration-v1` flag)
- [ ] PostHog event `frontend.localstorage.migration.success` fires
- [ ] E2E test verifies migration on app mount

### S5: Operator running a deploy after Wave 3 ships
> **As an** operator (infrastructure agent or Ray),
> **I want** GitHub Actions deploy workflows to read SSM from `/vaultmtg/{env}/*` paths,
> **so that** new SSM keys are the canonical source and old keys can be deleted safely.

**ACs** (covered by V32-W3-1, V32-W3-2, V32-W3-5):
- [ ] All `/mtga-companion/{env}/*` parameters mirrored under `/vaultmtg/{env}/*`
- [ ] All workflows + scripts read from new path
- [ ] One staging deploy runs successfully end-to-end with new path
- [ ] Old `/mtga-companion/*` parameters deleted after one production release
- [ ] `aws ssm get-parameters-by-path --path /mtga-companion --recursive` returns empty

### S6: Architect (Ray) reviewing the rename strategy
> **As the** architect,
> **I want** ADR-021 to capture every casing decision and sequencing choice,
> **so that** every PR review can cite a single source of truth and reject deviations.

**ACs** (covered by V32-W0-1):
- [ ] ADR-021 committed with casing convention table (~16 contexts)
- [ ] Sequencing rationale documented for all 6 waves
- [ ] Open questions answered with Ray's decisions
- [ ] Linked from `docs/architecture/changelog.md`

---

## Open questions (after arch review)

1. **Repo-rename ordering** — should V32-W6-2 (GitHub repo rename) move to
   Wave 0.5, before V32-W2-1 ships? Arch review §3.3 recommends yes.
   Awaiting Ray.
2. **17lands integration** — confirm impact of changing `ExportedFrom` field
   before V32-W1-6 ships. Email needed.
3. **Companion repos** — defer `mtga-companion-infra` and `mtga-companion-web`
   audits to v0.3.3? Recommend yes.
4. **DB rename window** — confirm Sunday 04:00 ET maintenance window for
   V32-W6-1 production DB rename.
5. **rhamiltoneng portfolio CFN tag** — should the `Project` tag move to
   `vaultmtg` or to a portfolio-specific value? Audit flagged as ambiguous.
6. **PostHog event names** — keep historical `mtga_companion.*` event names
   or migrate? Recommend keep (analytics continuity).

---

## Dependencies

- Architect must sign off on `arch-review.md` before project-manager creates tickets
- Ray must answer the 6 open questions above (decisions 1, 2, 4, 5 are blocking; 3 and 6 are recommendations)
- 17lands maintainer ping must happen before V32-W1-6 lands

---

## Out of scope / explicitly cut

- Companion repos `mtga-companion-infra` and `mtga-companion-web` (carry to v0.3.3)
- PostHog historical event names (analytics continuity wins)
- New features or product changes
- Marketing site copy beyond the canonical brand swap
- Mobile app (does not exist)
- The audit file itself (`docs/engineering/mtga-companion-rename-audit.md`) is preserved as a historical record — not modified

---

## Timeline estimate

This is rough — engineering owns the actual sequencing once tickets are in flight.

| Wave | Tickets | Estimated effort | Wall-clock |
|---|---|---|---|
| W0 | 1 | 2 days (architect) | 2 days |
| W1 | 8 | parallel; 1–2 days each | 3 days |
| W2 | 5 | parallel; 2–4 days each | 5 days |
| W3 | 5 | strict-sequenced; 1–2 days each | 7 days |
| W4 | 5 | parallel; 3–5 days each (smoke-test heavy) | 7 days |
| W5 | 3 | sequenced; 1–2 days each | 4 days |
| W6 | 4 | sequenced; 1–3 days each | 5 days |

**Estimated total wall-clock**: ~3 weeks if Wave 1 + Wave 2 run in parallel
and Wave 3 + Wave 4 run in parallel. Closed beta launches 2026-08-18, so this
fits cleanly before that gate.

---

## Sign-off

- [ ] Architect (Ray): `docs/product/milestones/v0.3.2/arch-review.md` signed off
- [ ] PM (Najah): drafted 2026-05-10
- [ ] Lead engineer: aware of ticket list and dependency graph
- [ ] project-manager: tickets created on project #34
