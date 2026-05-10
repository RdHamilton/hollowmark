# MTGA-Companion → VaultMTG Rename Audit

**Date**: 2026-05-10
**Author**: Architect (Ray)
**Status**: Audit only — no implementation
**Scope**: Catalogue every reference to `mtga-companion` (and case/separator variants) in the `MTGA-Companion` repo so we can plan a coordinated migration to `vault-mtg` / `vaultmtg`.

---

## 1. Methodology

Search performed across the full repo with `rg` for case-insensitive matches of:
- `mtga-companion`
- `mtga_companion`
- `mtgacompanion`
- `MTGACompanion`
- `MTGA_COMPANION`
- `MTGA-COMPANION`

### Headline counts

| Metric | Count |
|---|---|
| Total matches (all variants, all files) | **1,134** matches across **302** files |
| Non-hyphenated variants (`MTGACompanion`, `mtgacompanion`, `mtga_companion`) | **68** matches across **11** files |
| Files containing the canonical `mtga-companion` form | ~291 files |

The vast majority of "match volume" comes from one repeating pattern: the Go module path
`github.com/ramonehamilton/MTGA-Companion` (and `github.com/RdHamilton/MTGA-Companion`)
appearing as an import line in nearly every Go file. That single decision (the module path)
is a *medium-risk one-line change in `go.mod`* but produces hundreds of import-line edits.

### Companion repos NOT audited

This audit covers **only** the `MTGA-Companion` repo. The following sibling repos almost
certainly contain similar references and must be audited separately before any rename:

- `RdHamilton/mtga-companion-infra` (CloudFormation, deploy workflow)
- `RdHamilton/mtga-companion-web` (Next.js marketing site)

---

## 2. High-Risk Findings (live AWS resources or external contracts)

These cannot be renamed by an in-repo PR alone — they require coordinated infra changes,
DNS cutovers, or new resource creation followed by a migration.

### 2.1 SSM Parameter Hierarchy — `/mtga-companion/{production,staging}/*`

**Risk**: **HIGH** — these are live AWS resources read by the BFF on startup; renaming
requires creating new parameters under a new prefix, dual-reading, then deleting the old
prefix.

| Path | File | Line |
|---|---|---|
| `/mtga-companion/production/CLERK_SECRET_KEY` | `.github/workflows/release.yml` | 81, 320, 328 |
| `/mtga-companion/production/db-secret-arn` | `.github/workflows/release.yml` | 82, 274; `infra/scripts/create-staging-db.sh` 45; `infra/scripts/truncate-staging-db.sh` 42; `scripts/deploy/provision-db-url.sh` 20 |
| `/mtga-companion/production/db-endpoint` | `.github/workflows/release.yml` 83, 275; `infra/scripts/create-staging-db.sh` 31; `infra/scripts/truncate-staging-db.sh` 35; `scripts/deploy/provision-db-url.sh` 26 |
| `/mtga-companion/production/db-name` | `.github/workflows/release.yml` 84, 276; `infra/scripts/create-staging-db.sh` 38; `scripts/deploy/provision-db-url.sh` 32 |
| `/mtga-companion/production/ALLOWED_ORIGINS` | `.github/workflows/release.yml` 228, 236; `infrastructure/ssm/parameters.md` 7, 68 |
| `/mtga-companion/production/DATABASE_URL` | `infrastructure/ssm/parameters.md` 8 |
| `/mtga-companion/production/DAEMON_JWT_SECRET` | `infrastructure/ssm/parameters.md` 9 |
| `/mtga-companion/production/JWT_SECRET` | `infrastructure/ssm/parameters.md` 10 |
| `/mtga-companion/staging/db-password` | `infra/scripts/create-staging-db.sh` 70, 74; `infra/db/create-staging-db.sql` 26 |
| `/mtga-companion/staging/database-url` | `infra/scripts/create-staging-db.sh` 86; `infra/scripts/run-staging-migrations.sh` 18, 97, 103 |
| `/mtga-companion/staging/db-secret-arn` | `infra/scripts/run-staging-migrations.sh` 136 |
| `/mtga-companion/staging/db-endpoint` | `infra/scripts/run-staging-migrations.sh` 153 |
| `/mtga-companion/staging/PORT` | `scripts/deploy/provision-staging-env.sh` 53 |
| `/mtga-companion/staging/ALLOWED_ORIGINS` | `scripts/deploy/provision-staging-env.sh` 54 |
| `/mtga-companion/staging/CLERK_PUBLISHABLE_KEY` | `scripts/deploy/provision-staging-env.sh` 55 |
| `/mtga-companion/staging/CLERK_SECRET_KEY` | `scripts/deploy/provision-staging-env.sh` 56 |

**Migration pattern**: SSM parameters can be re-created at a new path. Rollout requires:
1. Copy each value to `/vaultmtg/{env}/<name>` (most staging params already use `/vaultmtg/staging/*` per `provision-staging-env.sh:4`).
2. Update workflows + scripts + BFF code to read from new path.
3. Deploy + verify, then delete old parameters.

### 2.2 S3 Bucket — `mtga-companion-deploy-artifacts-staging`

**Risk**: **HIGH** — S3 bucket names are immutable. Cannot be renamed in place.

| File | Line | String |
|---|---|---|
| `.github/workflows/staging-deploy.yml` | 23 | `STAGING_BUCKET: mtga-companion-deploy-artifacts-staging` |
| `scripts/deploy/stage-binary-staging.sh` | 7 | doc-comment referencing the bucket |

**Migration pattern**: Create `vaultmtg-deploy-artifacts-staging`, update workflows, run
both buckets in parallel for one release, then delete the old bucket. Coordinate with
`mtga-companion-infra` repo for CloudFormation that owns the bucket (if any).

### 2.3 EC2 Tag-Based Discovery — `mtga-companion-bff-production`

**Risk**: **HIGH** — scripts find the EC2 instance by `Name` tag. Renaming the tag also
requires updating any CloudFormation stack/template in the infra repo that sets the tag.

| File | Line | String |
|---|---|---|
| `infra/scripts/create-staging-db.sh` | 101, 106 | `Name=tag:Name,Values=mtga-companion-bff-production` |

### 2.4 CloudFront / S3 Project Tag — `Project: mtga-companion`

**Risk**: **MEDIUM** (tags are mutable but the value is referenced for cost allocation).

| File | Lines |
|---|---|
| `infrastructure/cloudformation/rhamiltoneng-cdn.yaml` | 43, 60, 120 |

This template tags the rhamiltoneng portfolio site bucket, ACM cert, and CloudFront
distribution with `Project=mtga-companion`. Since this stack is for the unrelated
portfolio site, the tag is arguably wrong already. Suggest re-tagging to `vaultmtg`
(or a literal portfolio project name) on next stack update.

### 2.5 Systemd Unit & Filesystem Paths on EC2

**Risk**: **HIGH** — these paths exist on running EC2 instances and BFF reads
`EnvironmentFile=/etc/mtga-companion-staging/env` at startup. Renaming requires a
coordinated user-data + provision-script change AND a manual cleanup on already-running
instances (or instance recycle).

| Path | File(s) |
|---|---|
| `/etc/mtga-companion/env` | `scripts/deploy/provision-env.sh` 24, 47; `scripts/deploy/provision-db-url.sh` 17, 50; `.github/workflows/release.yml` 222, 268, 314; `infrastructure/ssm/parameters.md` 42 |
| `/etc/mtga-companion-staging/env` | `infra/systemd/mtga-bff-staging.service` 14; `scripts/deploy/provision-staging-env.sh` 13, 14; `.github/workflows/staging-deploy.yml` 96 |
| `/var/www/mtga-companion`, `/var/www/mtga-companion-staging` | `scripts/deploy/deploy-frontend.sh` 15, 16 |
| `/opt/mtga-companion` | `infra/scripts/run-staging-migrations.sh` 49 |
| Systemd service names: `mtga-companion-bff`, `mtga-companion` (legacy), `mtga-bff-staging` | `infrastructure/ssm/parameters.md` 37, 82; `infra/systemd/mtga-bff-staging.service` (filename + unit) |

### 2.6 Vercel Hostname `mtga-companion.vercel.app`

**Risk**: **MEDIUM** — appears in `ALLOWED_ORIGINS` allow-list and BFF CORS test data.
Vercel project rename is a separate operation in Vercel UI; CORS allow-list must be
updated in SSM at the same moment.

| File | Line |
|---|---|
| `infrastructure/ssm/parameters.md` | 49, 59 |
| `services/bff/internal/config/config_test.go` | 295, 306, 423 |
| `services/bff/internal/config/config.go` | 50 (doc-comment example) |

### 2.7 Daemon Release Asset Names — `mtga-companion-daemon-<os>-<arch>[.exe]`

**Risk**: **MEDIUM** — already-published GitHub Releases retain the old asset names.
The frontend `DaemonDownload` component links to `releases/latest/download/mtga-companion-daemon-*`.
Renaming requires:
- Updating the daemon `Makefile` `BINARY_PREFIX`
- Updating the release workflow build step
- Updating the frontend download URLs
- Either: leave existing releases alone (old links break for cached pages) or republish artifacts under both names for one transition release

| File | Lines |
|---|---|
| `services/daemon/Makefile` | 3, 8 |
| `.github/workflows/daemon.yml` | 9 (header comment), build step output names |
| `.github/workflows/release.yml` | 141–146, 162–167 (six per-platform binaries) |
| `frontend/src/components/DaemonDownload.tsx` | 6, 84 |
| `frontend/src/components/DaemonDownload.test.tsx` | 6, 40, 50, 60 |
| `frontend/tests/e2e/download.spec.ts` | 4, 38, 47, 56 |
| `services/daemon/install/windows/install.ps1` | 21 (`$AssetName = 'mtga-companion-daemon-windows-amd64.exe'`) |
| `services/daemon/install/README.md` | 123–125 |

### 2.8 Daemon Service / Keychain / Plist Identifiers

**Risk**: **HIGH** — these names live on installed user machines. Renaming a service
identifier requires either:
- A daemon migration step on first-run that registers the new identifier and unregisters the old, **or**
- Forcing all beta users to uninstall and reinstall.

This is the riskiest single category because it cuts across user devices, not infra
we own.

| Identifier | Files |
|---|---|
| `com.mtga-companion.daemon` (macOS launchd label, keychain service name) | `services/daemon/internal/keychain/keychain.go` 3, 23; `services/daemon/internal/keychain/keychain_test.go` 66; `services/daemon/install/macos/install.sh` 24; `services/daemon/install/macos/uninstall.sh` 16; `services/daemon/install/macos/pkg/postinstall` 26; `services/daemon/internal/config/config.go` 5, 38 |
| `MTGA-Companion-Daemon` (Windows scheduled task) | `services/daemon/install/windows/install.ps1` 26; `services/daemon/install/windows/nsis/installer.nsi` 92, 94, 101, 111, 112; `services/daemon/install/windows/uninstall.ps1` 21 |
| `MTGACompanionDaemon` (legacy macOS launch agent + Windows service + Linux unit name from desktop era) | `cmd/mtga-companion/service.go` 41, 50, 90, 92, 94 |
| `~/.mtga-companion/` (config dir on macOS/Linux) | `services/daemon/cmd/daemon/main.go` 268, 269, 277, 284; `services/daemon/install/macos/install.sh` 26; `services/daemon/install/macos/pkg/postinstall` 30; `services/daemon/install/README.md` 38, 41, 49; main repo daemon config: `internal/daemon/config.go` 32, 64; `internal/daemon/service.go` 1191; flight recorder traces named `mtga-companion-trace-*` in `internal/daemon/flight_recorder.go` 143, 189 + tests |
| `%APPDATA%\mtga-companion\daemon.json` (Windows config dir) | `services/daemon/cmd/daemon/main.go` 2, 268, 277; `services/daemon/install/windows/install.ps1` 27, 31; `services/daemon/install/windows/nsis/installer.nsi` 8, 71, 77, 79, 95, 119; `services/daemon/install/README.md` 89, 94 |
| `%ProgramFiles%\MTGA-Companion\` / `%LOCALAPPDATA%\MTGA-Companion\` (Windows install dir) | `services/daemon/install/windows/install.ps1` 25, 79, 80; `services/daemon/install/windows/nsis/installer.nsi` 4, 38, 39 |
| `~/Library/Logs/mtga-companion-daemon.log` | `services/daemon/install/macos/install.sh` 165, 167, 185; `services/daemon/install/macos/uninstall.sh` 49; `services/daemon/install/README.md` 47 |

---

## 3. Medium-Risk Findings (code/config — rename in PR)

### 3.1 Go Module Paths

**Risk**: **MEDIUM** — single-line change in `go.mod`, but every Go file with an import
must be edited. `gopls` rename or `gofmt`-aware tooling can do this safely.

| File | Line | String |
|---|---|---|
| `go.mod` | 1 | `module github.com/ramonehamilton/MTGA-Companion` |
| `go.mod` | 8 | `github.com/RdHamilton/MTGA-Companion/services/contract v0.1.0` |
| `pkg/logparse/go.mod` | 1 | `module github.com/RdHamilton/MTGA-Companion/pkg/logparse` |
| All Go source files | various | `import "github.com/ramonehamilton/MTGA-Companion/internal/..."` (hundreds of import lines) |
| `services/*/go.mod` | — | (need verification — the search saw the contract path; sibling services likely import it too) |

**Note on github org path**: The repo lives at `github.com/RdHamilton/MTGA-Companion`
but the local `go.mod` historically used `github.com/ramonehamilton/...` — this is an
existing inconsistency, not a rename concern. If we rename the GitHub repo, both org
and repo segments change.

### 3.2 GitHub Actions — `GONOSUMDB` / `GOPRIVATE` env values

**Risk**: **MEDIUM** — every Go step in CI sets these to `github.com/RdHamilton/MTGA-Companion`.
If the repo is renamed on GitHub, every workflow file must update the path or `go mod download`
will fail to use git auth.

Files (all `.github/workflows/*`): `ci.yml`, `daemon.yml`, `daemon-release.yml`, `e2e-smoke.yml`, `integration.yml`, `release.yml`, `staging-deploy.yml`, `sync.yml`. Roughly 40 individual occurrences.

### 3.3 Frontend Code — npm package, localStorage keys, log strings

**Risk**: **MEDIUM** — localStorage keys are user-visible state; renaming them
will silently reset user preferences (filters, accordion state, dev-mode flag, etc.)
unless we ship a migration shim that copies old keys → new keys on first load.

| File | Line | String |
|---|---|---|
| `package.json` | 2 | `"name": "mtga-companion"` |
| `frontend/src/services/apiClient.ts` | 45 | `'mtga-companion-api-key'` (localStorage) |
| `frontend/src/components/settings/SettingsAccordion.test.tsx` | 7 | `'mtga-companion-settings-expanded'` |
| `frontend/src/hooks/useSettingsAccordion.ts` | 3 | same |
| `frontend/src/hooks/useSettingsAccordion.test.ts` | 6 | same |
| `frontend/src/hooks/useDeveloperMode.ts` | 3 | `'mtga-companion-developer-mode'` |
| `frontend/src/hooks/useDeveloperMode.test.ts` | 6 | same |
| `frontend/src/pages/Settings.test.tsx` | 653, 680, 693 | `'mtga-companion-developer-mode'` |
| `frontend/src/context/AppContext.tsx` | 107 | `'mtga-companion-filters'` |
| `frontend/src/context/AppContext.test.tsx` | 46 | same |
| `frontend/src/pages/Meta.tsx` | 24 | `'mtga-companion-meta-refresh-timestamps'` |
| `frontend/src/services/api/__tests__/system.test.ts` | 67 | `service: 'mtga-companion'` (mock data) |
| `frontend/src/services/api/drafts.test.ts` | 40, 127, 199 | `'MTGA-Companion'` (export metadata field) |
| `frontend/src/test/msw/handlers.ts` | 473 | same |
| `frontend/src/components/settings/sections/DatabaseSection.tsx` | 20 | `placeholder="/Users/username/.mtga-companion/mtga.db"` |

### 3.4 Backend Go Code — runtime strings

**Risk**: **MEDIUM** — affects API response payloads and outbound HTTP user-agent.
Anyone scraping our `/health` or `/version` endpoints expects `service: "mtga-companion-api"`.

| File | Line | String |
|---|---|---|
| `internal/api/router.go` | 419 | `"service": "mtga-companion-api"` |
| `internal/api/handlers/system.go` | 68 | same |
| `internal/meta/mtgtop8.go` | 242 | `"User-Agent", "MTGA-Companion/1.4.0"` |
| `internal/meta/goldfish.go` | 206 | same |
| `internal/export/draft_17lands.go` | 77 | `ExportedFrom: "MTGA-Companion"` (export metadata) |
| `internal/export/draft_17lands_test.go` | 111, 112 | same |

### 3.5 Database Seed — Dev User Email

**Risk**: **LOW–MEDIUM** — only affects local dev/staging seeded user.

| File | Line |
|---|---|
| `services/bff/internal/storage/migrations/postgres/000056_seed_dev_user.up.sql` | 5 |
| `services/bff/internal/storage/migrations/postgres/000056_seed_dev_user.down.sql` | 1 |

### 3.6 Production DB Name — `mtga_companion`

**Risk**: **HIGH** (data) — but renaming the PostgreSQL database is independently a
disruptive operation and likely deferred. Note the value in `infra/db/...` and SSM is
`mtga_companion` (underscore). The runbook mentions this:

| File | Line |
|---|---|
| `docs/infra/staging-db-runbook.md` | 184, 198 |

Recommend keeping the database name `mtga_companion` for now and only renaming the
SSM-published value if a true DB rename is scheduled. A DB-name rename in PostgreSQL
requires either `ALTER DATABASE ... RENAME` (cheap if no active connections) or
dump/restore.

### 3.7 cmd/ Binary Name and CLI Help Text

**Risk**: **MEDIUM** — the binary is called `mtga-companion` everywhere in user-facing
help output and the install dir defaults to `~/.mtga-companion/data.db`. This is the
desktop-era binary path; if we keep that binary alive at all, it must rename.

| File | Lines |
|---|---|
| `cmd/mtga-companion/main.go` | many — all `Usage:` print strings (lines 154, 164–168, 187, 257, 275, 300, 312, 315–318, 330, 431, 543, 573, 624, 699, 711–736, 739–740, 830, 839, 842, 845, 848) |
| `cmd/mtga-companion/service.go` | 41, 50, 58, 86, 87, 90, 92, 94, 111, 152 |
| `scripts/dev.sh` | 71, 72 (build target `bin/mtga-companion`) |
| `scripts/test.sh` | 2, 31 (script header comments) |

The directory `cmd/mtga-companion/` itself is part of the rename surface.

---

## 4. Low-Risk Findings (docs, comments, archived plans)

### 4.1 Active Documentation

About **48 files in `docs/`** match. The high-traffic ones to update:

- `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md` — root-level
- `docs/engineering/regression.md` — ~20 references
- `docs/engineering/claude-code-guide.md`
- `docs/engineering/reference/daemon-api.md`
- `docs/engineering/reference/go-1.25-features.md`
- `docs/support/triage-runbook.md`
- `docs/support/daemon-troubleshooting.md`
- `docs/support/daemon-uninstall.md`
- `docs/infra/staging-db-runbook.md`
- `docs/architecture/decisions/ADR-020.md`
- `docs/architecture/adr/0001-service-split-approaches.md`
- `docs/architecture/adr/0006-vercel-bff-connectivity.md`
- `docs/architecture/adr/0007-frontend-serving-model.md`
- `docs/architecture/adr/0008-frontend-serving-model-s3.md`
- `docs/architecture/changelog.md`
- `docs/product/milestones/v0.4.0/kickoff.md`, `project-manager-instructions.md`
- `docs/product/milestones/v0.3.1/kickoff.md`, `arch-review-wave0.md`
- `docs/product/milestones/v0.3.0/post-mortem-infra.md`
- `docs/prd/0001-daemon-packaging.md`
- `docs/status/{backend-engineer,infrastructure,dba}.md`

### 4.2 Archived / Desktop-era Documentation

`docs/archive/desktop-era/*` and `docs/archive/completed-plans/*` collectively contain
~150 references. These are historical artifacts; recommendation is to **leave them as
written** and add a banner at the top of each file noting the rename. Editing archived
plans loses the historical record.

### 4.3 Status / Report Documents

- `docs/reports/2026-05-beta-cost-model.md`
- `docs/archive/README-legacy-index.md`

### 4.4 OAuth Callback Custom URL Scheme

`mtgacompanion://callback` is mentioned as a fallback OAuth scheme. If we ever
implement it, the scheme name must match the renamed brand (`vaultmtg://callback`).

| File | Line |
|---|---|
| `docs/product/milestones/v0.3.1/arch-review-wave0.md` | 132 |
| `docs/product/milestones/v0.3.1/kickoff.md` | 419 |
| `docs/architecture/decisions/ADR-020.md` | 259 |

### 4.5 ADR-0007/0008 Domain References (`mtgacompanion.com`)

Old domain that may have never existed publicly — the active product domain is
`vaultmtg.app`. These ADRs still mention `mtgacompanion.com` as the production DNS
target.

| File | Lines |
|---|---|
| `docs/architecture/adr/0007-frontend-serving-model.md` | 19, 56, 92, 188 |
| `docs/architecture/adr/0008-frontend-serving-model-s3.md` | 17, 99 |

---

## 5. Files NOT Requiring Changes

- `go.sum`, `go.work.sum` — auto-regenerated by `go mod tidy`. Do not hand-edit.
- `frontend/.env.{production,local,example}` — verified empty of matches.
- No `Dockerfile`, `docker-compose.*` files in repo.
- `infrastructure/nginx/` — no matches.
- `infrastructure/cloudformation/vaultmtg-app-cdn.yaml` — already on the new name.

---

## 6. Recommended Migration Sequencing

The findings above naturally cluster into **five waves** that should ship as separate
PRs (or, where infra is concerned, separate coordinated releases):

### Wave 0 — Decision + ADR
- Write **ADR-021: Rename project from MTGA-Companion to VaultMTG**.
- Decide: do we rename the GitHub repo? If yes, all `GONOSUMDB`/`GOPRIVATE` and
  `RdHamilton/MTGA-Companion` references across CI become coupled. If no, we can do
  an internal rename without touching CI auth env.
- Decide: do we rename the Go module path? (Recommended yes — a half-renamed module is
  more confusing than the current state.)

### Wave 1 — Pure docs and low-risk strings (no functional risk)
- All non-archived markdown files
- User-Agent strings (`MTGA-Companion/1.4.0` → `VaultMTG/1.x`)
- Health/version `service: ...` field — add an alias period where both names ship
- 17lands export metadata `ExportedFrom: "MTGA-Companion"` → `"VaultMTG"`

### Wave 2 — In-repo code rename (no infra cutover)
- `go.mod` module path + bulk import rewrite (use `gopls rename` or `gofmt -r`)
- `package.json` name field
- Frontend localStorage keys + migration shim (read old key on mount, write new key,
  delete old)
- Frontend release-asset URLs (must coordinate with Wave 4 daemon binary rename)
- `cmd/mtga-companion/` directory rename + binary name + help text

### Wave 3 — SSM + EC2 cutover (infra)
- Create `/vaultmtg/production/*` parameters mirroring `/mtga-companion/production/*`
- Update `release.yml`, `provision-*.sh` to read new paths
- Re-issue EC2 user-data so `/etc/vaultmtg/env` is the canonical env file
- Rename systemd unit `mtga-companion-bff` → `vaultmtg-bff` (this requires either
  instance recycle or a manual `systemctl` rename on each instance)
- Eventually delete the old `/mtga-companion/*` SSM tree

### Wave 4 — Daemon rename (user-installed software)
- Tag a daemon release that:
  - Ships under both `mtga-companion-daemon-*` and `vaultmtg-daemon-*` artifact names
  - On install, registers new launchd label / scheduled-task name AND removes the old
    one
  - Migrates `~/.mtga-companion/daemon.json` → `~/.vaultmtg/daemon.json`
  - Migrates keychain entry from `com.mtga-companion.daemon` → `com.vaultmtg.daemon`
- Frontend `DaemonDownload` cuts over once new release is published.
- Two releases later, drop the legacy `mtga-companion-daemon-*` artifacts.

### Wave 5 — S3 bucket rename (deploy artifacts)
- Create `vaultmtg-deploy-artifacts-staging`
- Switch `staging-deploy.yml` to new bucket
- Run for one staging release
- Empty + delete old bucket

### Wave 6 — Repo rename (optional, last)
- Rename `RdHamilton/MTGA-Companion` → `RdHamilton/VaultMTG` (or similar)
- GitHub provides automatic redirect for old URL but `git remote` must be updated by
  every contributor
- Bulk-update `GONOSUMDB`/`GOPRIVATE` env in workflows
- Update README/CONTRIBUTING git-clone snippets

---

## 7. Open Questions for Ray

1. **Brand string casing**: Is the canonical new name `vault-mtg` (kebab), `vaultmtg` (one word), `VaultMTG` (camel), or all three depending on context? Today the codebase uses `vaultmtg.app` (domain) and `vaultmtg-staging` (some SSM). I have been writing `vaultmtg` / `VaultMTG` in the report — confirm or correct.
2. **Repo rename**: rename the GitHub repo, or keep `MTGA-Companion` as the org path forever?
3. **Database rename**: rename `mtga_companion` PostgreSQL DB at all? (My recommendation: **no**, because rename buys nothing operationally and adds risk. Just rename the SSM key that holds the DB name, not the DB itself.)
4. **Archived docs**: leave or rewrite? (My recommendation: **leave**, with a banner.)

---

## 8. Appendix — Files NOT yet inspected line-by-line

This audit found matches by ripgrep but did not read every match by hand. The following
files contain matches that were enumerated above but may have additional context worth
reading before the rename PRs are written:

- `services/daemon/install/windows/nsis/installer.nsi` (NSIS installer script — the
  Windows uninstall path leaves config on disk; we need to decide if migration deletes
  the old config or leaves it side-by-side)
- `services/daemon/install/macos/install.sh` (full install flow with the keychain +
  launchd label + log path + config path all hard-coded)
- `infrastructure/cloudformation/rhamiltoneng-cdn.yaml` (the `Project: mtga-companion`
  tag is on a portfolio site, not a VaultMTG resource — possibly a leftover; flagging
  for product owner review)
- `internal/daemon/flight_recorder.go` (trace file naming pattern — backwards-compat
  matters for anyone who has captured traces with the old name)

---

**End of audit report**
