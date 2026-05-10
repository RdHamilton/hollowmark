# v0.3.2 — MTGA-Companion → VaultMTG Rename Ticket List

**Source audit**: `docs/engineering/mtga-companion-rename-audit.md`
**Project**: #34 (`PVT_kwHOABsZ684BXSA8`)
**Milestone**: #71 — v0.3.2
**Ray's decisions** (final, all 6 resolved 2026-05-10):
1. Brand casing → **VaultMTG** (capital V, capital MTG, no separator). Lowercase form `vaultmtg` for paths, hostnames, kebab unsafe contexts.
2. Repo rename → **Yes**. `RdHamilton/MTGA-Companion` → `RdHamilton/vault-mtg` (kebab, lowercase — GitHub convention). **Sequenced into Wave 0.5** (between ADR and Wave 1) so all downstream waves reference the new repo URL from the start.
3. Database rename → **Yes**. `mtga_companion` → `vaultmtg`. **No maintenance window required** — no users in staging/prod yet.
4. Archived docs → **Rewrite** (not banner). Replace `mtga-companion`/`MTGA-Companion` with the canonical new form in archive content.
5. Companion repos (`mtga-companion-infra`, `mtga-companion-web`) → **In scope for v0.3.2** (not deferred). Tickets added to relevant waves.
6. 17lands `ExportedFrom` field → **Just rename to `"VaultMTG"`**, no outreach to 17lands.
7. rhamiltoneng CloudFormation `Project` tag → **Rename to `vaultmtg`**.
8. PostHog event names → **Rename away from `mtga_companion.*`** (not enough data to preserve continuity).

---

## Wave 0 — Decision and ADR (1 ticket, blocking everything else)

### V32-W0-1: ADR-021 — Rename project to VaultMTG
- **Description**: Write `docs/architecture/decisions/ADR-021-rename-to-vaultmtg.md` capturing the rename rationale, brand casing convention, repo rename decision, DB rename decision, archived-doc rewrite policy, and the 6-wave sequencing strategy. Document casing rules: `VaultMTG` for prose, code identifiers, brand strings; `vaultmtg` for hostnames, paths, AWS resource names, package names; `vault-mtg` for the GitHub repo slug only.
- **Acceptance criteria**:
  - [ ] ADR-021 file committed under `docs/architecture/decisions/`
  - [ ] ADR linked from `docs/architecture/changelog.md`
  - [ ] Casing convention table covers: prose, Go module path, npm package, Postgres DB, SSM, S3 bucket, systemd unit, daemon launchd label, Windows scheduled task name, GitHub repo slug
  - [ ] Migration sequencing matches the 6-wave plan (waves 1–6 below)
  - [ ] All four open questions from the audit are answered with Ray's decisions
- **Wave**: 0
- **Risk**: Low (docs only, but blocks all downstream work)
- **Labels**: `rename`, `architecture`, `agent:architect`, `priority: critical-path`
- **Owner**: architect

---

## Wave 0.5 — GitHub repo rename (1 ticket, sequenced before all downstream waves)

### V32-W05-1: Rename GitHub repo MTGA-Companion → vault-mtg
- **Description**: Per Ray's decision #2, rename the GitHub repo `RdHamilton/MTGA-Companion` → `RdHamilton/vault-mtg` BEFORE Wave 1 runs. GitHub's auto-redirect handles HTTP and git protocol traffic for ~1 year. Sequencing this before Wave 1 means all downstream waves' docs/code/configs reference the canonical new repo URL from the start (not "old URL with redirect"). Every contributor and agent must update their local `git remote set-url origin git@github.com:RdHamilton/vault-mtg.git`. Branch protection rules and webhooks transfer automatically. Coordinate the moment of rename with all active agents (PM broadcasts in `.claude/agents/BROADCAST.md` and pauses merges for ~30 min).
- **Acceptance criteria**:
  - [ ] GitHub repo renamed via Settings → Rename repository
  - [ ] Branch protection rules verified intact
  - [ ] CI workflows still trigger on PRs against new repo
  - [ ] Agents notified via BROADCAST and confirm `git remote` updated
  - [ ] Ray's local working tree updated: `git remote set-url origin git@github.com:RdHamilton/vault-mtg.git`
  - [ ] One smoke PR opened + merged on the new repo to confirm CI plumbing
- **Wave**: 0.5
- **Risk**: Medium (GitHub redirect mitigates breakage but contributor friction is real if not coordinated)
- **Labels**: `rename`, `infrastructure`, `priority: critical-path`
- **Owner**: Ray (manual GitHub UI action) + infrastructure (smoke verification)
- **Dependencies**: V32-W0-1 (ADR-021)

---

## Wave 1 — Pure docs and low-risk strings (8 tickets, no functional risk)

### V32-W1-1: Update root-level docs (README, CONTRIBUTING, SECURITY, CHANGELOG)
- **Description**: Rewrite all references to `MTGA-Companion` / `mtga-companion` / `MTGACompanion` in repo root markdown files to the canonical VaultMTG form. Update git-clone snippets to use the new repo URL (Wave 6 dependency — write the new URL even though the rename ships in Wave 6, since Wave 6 is the same milestone).
- **Acceptance criteria**:
  - [ ] `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md` contain zero `mtga-companion` or `MTGA-Companion` references
  - [ ] Repo URL references use `RdHamilton/vault-mtg`
  - [ ] Brand strings use `VaultMTG`
  - [ ] `rg -i mtga.companion README.md CONTRIBUTING.md SECURITY.md CHANGELOG.md` returns zero matches
- **Wave**: 1
- **Risk**: Low
- **Labels**: `rename`, `documentation`
- **Owner**: any engineer

### V32-W1-2: Update active engineering and support docs
- **Description**: Replace all matches in `docs/engineering/`, `docs/support/`, `docs/infra/` (excluding the audit file itself, which is the source of truth and stays as written). Specifically: `regression.md`, `claude-code-guide.md`, `reference/daemon-api.md`, `reference/go-1.25-features.md`, `triage-runbook.md`, `daemon-troubleshooting.md`, `daemon-uninstall.md`, `staging-db-runbook.md`.
- **Acceptance criteria**:
  - [ ] All listed files contain zero `mtga-companion` references
  - [ ] Daemon paths in support docs reference `~/.vaultmtg/` (macOS/Linux) and `%APPDATA%\vaultmtg\` (Windows)
  - [ ] Support runbooks reference `vaultmtg-daemon-*` artifact names
  - [ ] `docs/engineering/mtga-companion-rename-audit.md` is NOT modified (kept as historical record)
- **Wave**: 1
- **Risk**: Low
- **Labels**: `rename`, `documentation`
- **Owner**: any engineer

### V32-W1-3: Update active product/milestone docs
- **Description**: Replace matches in `docs/product/milestones/v0.4.0/`, `docs/product/milestones/v0.3.1/`, `docs/prd/0001-daemon-packaging.md`, and any other live PRD/milestone docs. Includes `mtgacompanion://callback` → `vaultmtg://callback` references in v0.3.1 arch-review and kickoff.
- **Acceptance criteria**:
  - [ ] No `mtga-companion` references in active milestone docs
  - [ ] OAuth callback scheme references updated to `vaultmtg://callback`
  - [ ] PRD `0001-daemon-packaging.md` updated to reference new daemon paths
- **Wave**: 1
- **Risk**: Low
- **Labels**: `rename`, `documentation`
- **Owner**: any engineer

### V32-W1-4: Rewrite archived docs (per Ray's decision #4)
- **Description**: Per Ray's decision, REWRITE (not banner) all archived doc references in `docs/archive/desktop-era/*` and `docs/archive/completed-plans/*` (~150 references across multiple files). Replace `mtga-companion` with `vaultmtg`, `MTGA-Companion` with `VaultMTG`, `mtga_companion` with `vaultmtg`. Preserve historical accuracy: do NOT change historical dates, decisions, or tickets — only swap the brand string.
- **Acceptance criteria**:
  - [ ] All archived doc files updated
  - [ ] `rg -i 'mtga.companion|mtga_companion' docs/archive/` returns zero matches
  - [ ] Git diff review confirms only brand strings changed (no semantic changes to historical content)
- **Wave**: 1
- **Risk**: Low
- **Labels**: `rename`, `documentation`
- **Owner**: any engineer

### V32-W1-5: Update ADR archive (ADR-0007/0008/0001/0006/0020)
- **Description**: Update domain references in `docs/architecture/adr/0007-frontend-serving-model.md` and `0008-frontend-serving-model-s3.md` (replace `mtgacompanion.com` with `vaultmtg.app`), plus brand references in `0001-service-split-approaches.md`, `0006-vercel-bff-connectivity.md`, `decisions/ADR-020.md`, and `architecture/changelog.md`. ADRs are historical decisions — preserve dates and decisions intact, only swap brand strings.
- **Acceptance criteria**:
  - [ ] ADRs updated with new brand strings
  - [ ] Domain `mtgacompanion.com` references replaced with `vaultmtg.app`
  - [ ] No semantic changes to ADR decisions or dates
- **Wave**: 1
- **Risk**: Low
- **Labels**: `rename`, `architecture`, `documentation`
- **Owner**: any engineer

### V32-W1-6: Backend runtime strings — User-Agent, service field, export metadata
- **Description**: Update Go runtime strings: `User-Agent: MTGA-Companion/1.4.0` → `VaultMTG/1.x` in `internal/meta/mtgtop8.go` and `internal/meta/goldfish.go`. Update 17lands export `ExportedFrom: "MTGA-Companion"` → `"VaultMTG"` in `internal/export/draft_17lands.go` plus its test (per Ray's decision #4: just rename — no outreach to 17lands; their importer reads the field as a string and does not gate on a known-publisher list). Update `service: "mtga-companion-api"` in `internal/api/router.go` and `internal/api/handlers/system.go` — but ALSO add an alias period where both the new and old names ship in `/health` and `/version` responses (3-release deprecation window) per audit Wave 1 recommendation.
- **Acceptance criteria**:
  - [ ] User-Agent uses `VaultMTG/<version>`
  - [ ] Export metadata `ExportedFrom: "VaultMTG"` (and updated test fixtures)
  - [ ] `/health` and `/version` return `service: "vaultmtg-api"` AND include a transitional `aliases: ["mtga-companion-api"]` field for one release window
  - [ ] Frontend tests in `frontend/src/services/api/__tests__/system.test.ts` and `drafts.test.ts` updated for new mock data
- **Wave**: 1
- **Risk**: Medium (API contract — anyone scraping `/health` for service name will see the new value; aliases give one-release transition)
- **Labels**: `rename`, `backend`, `agent:backend`
- **Owner**: backend-engineer

### V32-W1-7: Database seed dev user email
- **Description**: Update seed migration `services/bff/internal/storage/migrations/postgres/000056_seed_dev_user.up.sql` and `.down.sql` to use a `vaultmtg`-branded email. New migration (do NOT edit the existing one — migrations are immutable once shipped). Add a new migration `000xxx_rename_dev_user_email.up.sql` that updates the seeded row.
- **Acceptance criteria**:
  - [ ] New migration file created (do not edit `000056_seed_dev_user.up.sql`)
  - [ ] Migration updates dev user email from `*@mtga-companion.*` to `*@vaultmtg.*`
  - [ ] Down migration restores the old email
  - [ ] Migration runs cleanly against staging DB
- **Wave**: 1
- **Risk**: Low
- **Labels**: `rename`, `database`, `agent:dba`
- **Owner**: dba

### V32-W1-8: Status and report docs
- **Description**: Update remaining doc references in `docs/reports/2026-05-beta-cost-model.md`, `docs/archive/README-legacy-index.md`, `docs/status/{backend-engineer,infrastructure,dba}.md`, `docs/product/milestones/v0.3.0/post-mortem-infra.md`. Sweep + commit.
- **Acceptance criteria**:
  - [ ] All listed files updated
  - [ ] Status files reference VaultMTG paths and identifiers
  - [ ] `rg -i 'mtga.companion' docs/ | grep -v 'mtga-companion-rename-audit.md' | grep -v archive` returns zero matches (or only matches from files explicitly excluded by other Wave 1 tickets)
- **Wave**: 1
- **Risk**: Low
- **Labels**: `rename`, `documentation`
- **Owner**: any engineer

---

## Wave 2 — In-repo code rename (5 tickets, no infra cutover)

### V32-W2-1: Rename Go module path (go.mod + bulk import rewrite)
- **Description**: Rename module path in `go.mod` from `github.com/ramonehamilton/MTGA-Companion` to `github.com/RdHamilton/vault-mtg` (matches new repo slug from Ray's decision #2). Update `pkg/logparse/go.mod` and any sibling `services/*/go.mod` that imports the contract. Bulk-rewrite every `import "github.com/ramonehamilton/MTGA-Companion/..."` and `import "github.com/RdHamilton/MTGA-Companion/..."` statement using `gofmt -r` or `gopls rename`. Run `go mod tidy` and `go build ./...` to confirm.
- **Acceptance criteria**:
  - [ ] `go.mod` module line updated
  - [ ] All Go imports use the new path
  - [ ] `go.sum` regenerated via `go mod tidy`
  - [ ] `go build ./...` succeeds across all modules
  - [ ] `go test ./...` passes
  - [ ] `gofumpt` clean on every modified file
  - [ ] `rg 'ramonehamilton/MTGA-Companion|RdHamilton/MTGA-Companion' --type go` returns zero matches
- **Wave**: 2
- **Risk**: Medium (touches every Go file but mechanical)
- **Labels**: `rename`, `backend`, `agent:backend`, `priority: critical-path`
- **Owner**: backend-engineer
- **Dependencies**: ADR-021 (V32-W0-1)

### V32-W2-2: Frontend package.json + localStorage migration shim
- **Description**: Update `frontend/package.json` `"name": "mtga-companion"` → `"vaultmtg"`. Rename localStorage keys: `mtga-companion-api-key`, `mtga-companion-settings-expanded`, `mtga-companion-developer-mode`, `mtga-companion-filters`, `mtga-companion-meta-refresh-timestamps`. CRITICAL: ship a migration shim that runs once on mount and copies any old key values to the new keys, then deletes the old keys. Without this, every existing user loses their settings, dev-mode flag, and filter state silently.
- **Acceptance criteria**:
  - [ ] `package.json` name field updated
  - [ ] All localStorage keys renamed to `vaultmtg-*`
  - [ ] Migration shim file (`frontend/src/utils/localStorageMigration.ts`) reads old keys, writes new keys, deletes old keys
  - [ ] Migration shim runs once on app mount (gated by a `vaultmtg-migration-v1` flag so it doesn't re-run)
  - [ ] Component tests updated for renamed keys
  - [ ] E2E test verifies migration: set old keys, mount app, assert new keys exist with same values
  - [ ] `DatabaseSection.tsx` placeholder updated to `~/.vaultmtg/vaultmtg.db`
- **Wave**: 2
- **Risk**: Medium (user-visible state — bug here loses user preferences silently)
- **Labels**: `rename`, `frontend`, `agent:frontend`, `priority: critical-path`
- **Owner**: front-engineer

### V32-W2-3: Rename cmd/mtga-companion/ directory + binary + help text
- **Description**: Rename `cmd/mtga-companion/` directory to `cmd/vaultmtg/`. Update `service.go` and `main.go` to reference `vaultmtg` as the binary name in all `Usage:` print strings (~30 lines per audit). Update build scripts `scripts/dev.sh` and `scripts/test.sh` to build `bin/vaultmtg`. The legacy macOS launchd label `MTGACompanionDaemon` in `cmd/mtga-companion/service.go:41,50,90,92,94` is the desktop-era binary's service label — rename to `VaultMTGDaemon` (this is desktop-era code; if no longer shipped, document that in the PR).
- **Acceptance criteria**:
  - [ ] Directory renamed to `cmd/vaultmtg/`
  - [ ] All `Usage:` strings reference `vaultmtg` as the binary name
  - [ ] Build scripts produce `bin/vaultmtg`
  - [ ] Legacy `MTGACompanionDaemon` label renamed to `VaultMTGDaemon` in `service.go`
  - [ ] `go build ./cmd/vaultmtg/...` succeeds
  - [ ] PR description clarifies whether desktop-era binary is still shipped or is dead code
- **Wave**: 2
- **Risk**: Medium (binary path change affects anyone building from source)
- **Labels**: `rename`, `backend`, `agent:backend`
- **Owner**: backend-engineer

### V32-W2-4: Frontend daemon download asset URLs
- **Description**: Update `frontend/src/components/DaemonDownload.tsx` and its test to point at `vaultmtg-daemon-*` asset names. **MUST coordinate with Wave 4 (V32-W4-1)**: the new asset names must be published BEFORE the frontend cuts over, otherwise download links 404. Plan: ship a daemon release with both names (Wave 4 ticket V32-W4-1), then merge this frontend ticket. Update `frontend/tests/e2e/download.spec.ts` to verify the new URLs.
- **Acceptance criteria**:
  - [ ] `DaemonDownload.tsx` references `vaultmtg-daemon-*` URLs
  - [ ] Component test updated
  - [ ] E2E test updated and passes against the new release
  - [ ] PR is gated behind V32-W4-1 (do NOT merge until dual-named daemon release is published)
- **Wave**: 2
- **Risk**: Medium (broken download links if shipped before Wave 4)
- **Labels**: `rename`, `frontend`, `agent:frontend`
- **Owner**: front-engineer
- **Dependencies**: V32-W4-1 (must merge after)

### V32-W2-5: Internal daemon config + flight recorder paths (in-repo daemon code)
- **Description**: Update in-repo daemon code that references `~/.mtga-companion/`: `internal/daemon/config.go:32,64`, `internal/daemon/service.go:1191`, `internal/daemon/flight_recorder.go:143,189`. Trace files named `mtga-companion-trace-*` → `vaultmtg-trace-*`. Add backwards-compat: if old config dir exists and new doesn't, migrate it on first run. Update tests.
- **Acceptance criteria**:
  - [ ] All in-repo daemon code references `~/.vaultmtg/`
  - [ ] Trace file names use `vaultmtg-trace-*` prefix
  - [ ] Migration logic copies `~/.mtga-companion/*` → `~/.vaultmtg/*` if old dir exists and new doesn't
  - [ ] Tests updated
  - [ ] Existing flight recorder traces with old name still readable (backwards-compat)
- **Wave**: 2
- **Risk**: Medium (touches user-installed config)
- **Labels**: `rename`, `daemon`, `backend`, `agent:backend`
- **Owner**: backend-engineer

---

## Wave 3 — SSM + EC2 cutover + companion infra repo (6 tickets, infra)

### V32-W3-1: Mirror SSM parameters under /vaultmtg/{env}/
- **Description**: Create `/vaultmtg/production/*` and `/vaultmtg/staging/*` parameters that mirror every value in `/mtga-companion/{env}/*`. Includes: `CLERK_SECRET_KEY`, `db-secret-arn`, `db-endpoint`, `db-name`, `ALLOWED_ORIGINS`, `DATABASE_URL`, `DAEMON_JWT_SECRET`, `JWT_SECRET`, `db-password`, `database-url`, `PORT`, `CLERK_PUBLISHABLE_KEY`. Note: some staging params already exist under `/vaultmtg/staging/*` per `provision-staging-env.sh:4` — verify and reconcile. **DO NOT delete the old `/mtga-companion/*` tree yet** — that's V32-W3-5.
- **Acceptance criteria**:
  - [ ] Every `/mtga-companion/production/*` parameter has a `/vaultmtg/production/*` mirror with identical value
  - [ ] Same for staging
  - [ ] `infrastructure/ssm/parameters.md` updated to document new tree (with old tree still listed as deprecated)
  - [ ] `aws ssm get-parameters-by-path --path /vaultmtg/production --recursive` returns the full set
  - [ ] Old `/mtga-companion/*` parameters NOT deleted (stays as fallback during cutover)
- **Wave**: 3
- **Risk**: Medium (manual SSM ops, but reversible)
- **Labels**: `rename`, `infrastructure`, `agent:infrastructure`
- **Owner**: infrastructure
- **Dependencies**: ADR-021

### V32-W3-2: Update workflows + scripts to read /vaultmtg/* SSM paths
- **Description**: Update `.github/workflows/release.yml`, `.github/workflows/staging-deploy.yml`, `infra/scripts/create-staging-db.sh`, `infra/scripts/truncate-staging-db.sh`, `infra/scripts/run-staging-migrations.sh`, `scripts/deploy/provision-db-url.sh`, `scripts/deploy/provision-staging-env.sh` to read from `/vaultmtg/{env}/*` instead of `/mtga-companion/{env}/*`. Run a staging deploy + production deploy to verify.
- **Acceptance criteria**:
  - [ ] All listed files reference `/vaultmtg/{env}/*` SSM paths
  - [ ] Staging deploy runs end-to-end and BFF reads new SSM tree
  - [ ] Production deploy runs end-to-end (or rehearsed in staging if production is held)
  - [ ] `rg '/mtga-companion/' .github infra scripts` returns zero matches
- **Wave**: 3
- **Risk**: High (broken workflow = broken deploy)
- **Labels**: `rename`, `infrastructure`, `agent:infrastructure`, `priority: critical-path`
- **Owner**: infrastructure
- **Dependencies**: V32-W3-1 (mirror must exist first)

### V32-W3-3: EC2 systemd unit + filesystem path cutover
- **Description**: Cut over `/etc/mtga-companion/env` → `/etc/vaultmtg/env`, `/etc/mtga-companion-staging/env` → `/etc/vaultmtg-staging/env`, `/var/www/mtga-companion[-staging]` → `/var/www/vaultmtg[-staging]`, `/opt/mtga-companion` → `/opt/vaultmtg`. Rename systemd units `mtga-companion-bff` → `vaultmtg-bff` and `mtga-bff-staging` → `vaultmtg-bff-staging`. Update `provision-env.sh`, `provision-db-url.sh`, `release.yml`, `infrastructure/ssm/parameters.md`, `infra/systemd/mtga-bff-staging.service` (rename file too). Strategy: ship symlinks for one release (`/etc/mtga-companion/env` → `/etc/vaultmtg/env`) so a partial deploy doesn't 500.
- **Acceptance criteria**:
  - [ ] Systemd unit files renamed
  - [ ] EC2 user-data + provision scripts write to new paths
  - [ ] Symlinks from old paths to new paths exist on staging instance for one release
  - [ ] BFF starts cleanly with `EnvironmentFile=/etc/vaultmtg/env`
  - [ ] `systemctl status vaultmtg-bff-staging` returns active
  - [ ] Frontend deploy script updated to write to `/var/www/vaultmtg-staging`
  - [ ] EC2 instance recycled (or manual `systemctl daemon-reload` + service restart) so new units take effect
- **Wave**: 3
- **Risk**: High (running EC2 instance reconfiguration)
- **Labels**: `rename`, `infrastructure`, `agent:infrastructure`, `priority: critical-path`
- **Owner**: infrastructure
- **Dependencies**: V32-W3-2

### V32-W3-4: EC2 Name tag + tag-based discovery
- **Description**: Update EC2 instance `Name` tag from `mtga-companion-bff-production` → `vaultmtg-bff-production`. Update `infra/scripts/create-staging-db.sh:101,106` to filter on the new tag value. Coordinate with `mtga-companion-infra` repo if any CloudFormation stack sets the tag (audit calls this out — likely yes).
- **Acceptance criteria**:
  - [ ] EC2 instance retagged
  - [ ] Scripts use new tag value
  - [ ] Companion infra repo PR opened (or ticket filed) for the CloudFormation tag update
  - [ ] `aws ec2 describe-instances --filter Name=tag:Name,Values=vaultmtg-bff-production` returns the instance
- **Wave**: 3
- **Risk**: Medium (tag is mutable, but scripts depend on the literal value)
- **Labels**: `rename`, `infrastructure`, `agent:infrastructure`
- **Owner**: infrastructure
- **Dependencies**: V32-W3-3

### V32-W3-5: Delete /mtga-companion/* SSM tree + retag rhamiltoneng CFN
- **Description**: Once V32-W3-2 has shipped to production AND a release has run successfully against the new SSM tree, delete the old `/mtga-companion/{production,staging}/*` parameters. Per Ray's decision #5: retag the rhamiltoneng portfolio CloudFront stack `Project: mtga-companion` → `Project: vaultmtg` (decision is final — no portfolio-specific alternative).
- **Acceptance criteria**:
  - [ ] `aws ssm get-parameters-by-path --path /mtga-companion --recursive` returns empty
  - [ ] `infrastructure/ssm/parameters.md` no longer lists the old tree
  - [ ] `infrastructure/cloudformation/rhamiltoneng-cdn.yaml` `Project` tag updated to `vaultmtg`
  - [ ] CloudFormation stack updated in deployed AWS account (us-east-1)
  - [ ] `aws cloudformation describe-stacks` shows `Project=vaultmtg` tag on the rhamiltoneng stack
- **Wave**: 3
- **Risk**: Medium (deletion is irreversible without re-creation)
- **Labels**: `rename`, `infrastructure`, `agent:infrastructure`
- **Owner**: infrastructure
- **Dependencies**: V32-W3-2 (production deploy must have run on new tree)

### V32-W3-6: Companion repo `mtga-companion-infra` — rename CloudFormation tags + EC2/SSM resources
- **Description**: Per Ray's decision #5, the companion infra repo (`RdHamilton/mtga-companion-infra`, local path `/Users/ramonehamilton/Documents/Personal Projects/mtga-companion-infra`) is in scope for v0.3.2. Audit + update all CloudFormation templates, Terraform (if any), and scripts that set `Project: mtga-companion` tags or reference `mtga-companion-*` resource names. Coordinate with the EC2 retag in V32-W3-4 (this ticket may own the actual CFN stack that defines the EC2 instance — verify ownership). Open a PR in the companion repo. Rename the companion repo itself to `vaultmtg-infra` as part of this ticket (Wave 0.5 only renames the main app repo).
- **Acceptance criteria**:
  - [ ] All CloudFormation `Project` tags in `mtga-companion-infra` updated to `vaultmtg`
  - [ ] Resource Name tags referencing `mtga-companion-*` updated to `vaultmtg-*`
  - [ ] CloudFormation stacks updated in deployed AWS account
  - [ ] Companion repo renamed `mtga-companion-infra` → `vaultmtg-infra` via GitHub Settings
  - [ ] Companion repo README updated with new brand strings
  - [ ] No `mtga-companion` strings remain in companion repo (`rg -i mtga.companion` returns zero)
- **Wave**: 3
- **Risk**: Medium (touches deployed CFN stacks)
- **Labels**: `rename`, `infrastructure`, `agent:infrastructure`
- **Owner**: infrastructure
- **Dependencies**: V32-W3-1 (SSM mirror exists)

---

## Wave 4 — Daemon rename (5 tickets, user-installed software — highest user risk)

### V32-W4-1: Daemon release ships under both old and new artifact names
- **Description**: Update `services/daemon/Makefile` `BINARY_PREFIX` to `vaultmtg-daemon`. Update `.github/workflows/daemon.yml` and `.github/workflows/release.yml` (lines 141–146, 162–167) to publish artifacts under BOTH `mtga-companion-daemon-*` AND `vaultmtg-daemon-*` names for one full release window. This is the gate that unblocks V32-W2-4 (frontend download URL cutover).
- **Acceptance criteria**:
  - [ ] Makefile builds binary as `vaultmtg-daemon-*`
  - [ ] CI publishes both old and new artifact names per platform (12 artifacts total: 6 platforms × 2 names)
  - [ ] One daemon release tagged with both name sets visible in GitHub Releases
  - [ ] V32-W2-4 (frontend cutover) can now merge
- **Wave**: 4
- **Risk**: Medium (CI changes; if names are wrong, downloads break)
- **Labels**: `rename`, `daemon`, `agent:backend`, `priority: critical-path`
- **Owner**: backend-engineer

### V32-W4-2: macOS daemon migration (launchd label + keychain + paths + plist)
- **Description**: Update `services/daemon/install/macos/install.sh`, `uninstall.sh`, `pkg/postinstall`. Rename launchd label `com.mtga-companion.daemon` → `com.vaultmtg.daemon`. Migrate keychain entry from old service name to new. Migrate config dir `~/.mtga-companion/` → `~/.vaultmtg/`. Migrate log file `~/Library/Logs/mtga-companion-daemon.log` → `~/Library/Logs/vaultmtg-daemon.log`. CRITICAL: install script MUST detect existing `com.mtga-companion.daemon` launchd label and unload it before registering the new one — otherwise both daemons run simultaneously.
- **Acceptance criteria**:
  - [ ] Install script registers `com.vaultmtg.daemon` launchd label
  - [ ] Install script detects + unregisters old `com.mtga-companion.daemon` label if present
  - [ ] Keychain migration: old service entry copied to new, old deleted
  - [ ] Config dir migration: `~/.mtga-companion/` contents copied to `~/.vaultmtg/`, old dir deleted on first successful boot of new daemon
  - [ ] Log file rotation: new logs go to `vaultmtg-daemon.log`; old log left in place (do not delete user data)
  - [ ] Code in `services/daemon/internal/keychain/keychain.go` and `keychain_test.go` updated
  - [ ] Code in `services/daemon/internal/config/config.go` updated
  - [ ] Smoke test on a clean macOS VM AND on a VM with the old daemon already installed
- **Wave**: 4
- **Risk**: High (cuts across user devices)
- **Labels**: `rename`, `daemon`, `installer`, `agent:backend`, `priority: critical-path`
- **Owner**: backend-engineer
- **Dependencies**: V32-W4-1

### V32-W4-3: Windows daemon migration (scheduled task + paths + NSIS)
- **Description**: Update `services/daemon/install/windows/install.ps1`, `uninstall.ps1`, `nsis/installer.nsi`. Rename scheduled task `MTGA-Companion-Daemon` → `VaultMTG-Daemon`. Migrate config dir `%APPDATA%\mtga-companion\` → `%APPDATA%\vaultmtg\`. Migrate install dir `%ProgramFiles%\MTGA-Companion\` and `%LOCALAPPDATA%\MTGA-Companion\` → `vaultmtg`. Update NSIS installer to detect previous version's scheduled task + install dir and offer migration. Update PS1 `$AssetName = 'vaultmtg-daemon-windows-amd64.exe'`.
- **Acceptance criteria**:
  - [ ] Install script creates `VaultMTG-Daemon` scheduled task
  - [ ] Install script detects + removes old `MTGA-Companion-Daemon` scheduled task if present
  - [ ] Config dir migration: `%APPDATA%\mtga-companion\daemon.json` → `%APPDATA%\vaultmtg\daemon.json`
  - [ ] Install dir migration: detect old `%ProgramFiles%\MTGA-Companion\` and uninstall before installing to new path
  - [ ] NSIS installer signs the new path
  - [ ] Smoke test on a clean Windows VM AND on a VM with the old daemon already installed
- **Wave**: 4
- **Risk**: High (cuts across user devices, NSIS is fragile)
- **Labels**: `rename`, `daemon`, `installer`, `agent:backend`, `priority: critical-path`
- **Owner**: backend-engineer
- **Dependencies**: V32-W4-1

### V32-W4-4: Linux daemon migration (systemd unit + paths)
- **Description**: Linux daemon paths: `~/.mtga-companion/` → `~/.vaultmtg/`. Update install scripts (if any exist for Linux — audit references README only at `services/daemon/install/README.md:38,41,49`). Document migration in install README.
- **Acceptance criteria**:
  - [ ] Linux install/upgrade docs reference `~/.vaultmtg/`
  - [ ] If a Linux installer exists, it migrates config dir
  - [ ] Smoke test on Linux VM (clean + upgrade scenarios)
- **Wave**: 4
- **Risk**: Medium (smaller user base than Windows/macOS)
- **Labels**: `rename`, `daemon`, `installer`, `agent:backend`
- **Owner**: backend-engineer
- **Dependencies**: V32-W4-1

### V32-W4-5: Drop legacy mtga-companion-daemon-* artifacts after one release
- **Description**: Two daemon releases after V32-W4-1 ships, drop the dual-named artifacts and only publish `vaultmtg-daemon-*`. Update `release.yml` and `daemon.yml` to remove the old asset names. Document the cutover date in the v0.3.2 changelog.
- **Acceptance criteria**:
  - [ ] Release workflow only publishes `vaultmtg-daemon-*` artifacts
  - [ ] Documentation updated noting the cutover date
  - [ ] Final daemon release with both names is tagged in changelog
- **Wave**: 4
- **Risk**: Low (after cutover validated)
- **Labels**: `rename`, `daemon`, `ci`, `agent:backend`
- **Owner**: backend-engineer
- **Dependencies**: V32-W4-1, V32-W4-2, V32-W4-3, V32-W4-4

---

## Wave 5 — S3, Vercel, companion web repo, PostHog (5 tickets)

### V32-W5-1: Create vaultmtg-deploy-artifacts-staging bucket and dual-write
- **Description**: Create new S3 bucket `vaultmtg-deploy-artifacts-staging` with the same policy + encryption + versioning as the existing one. Update `staging-deploy.yml:23` `STAGING_BUCKET` to the new name. Update `scripts/deploy/stage-binary-staging.sh:7` doc-comment. Run one staging deploy through the new bucket. Coordinate with `mtga-companion-infra` repo if CloudFormation owns the bucket.
- **Acceptance criteria**:
  - [ ] New bucket exists with same configuration as old
  - [ ] `staging-deploy.yml` references new bucket
  - [ ] One staging release runs successfully with the new bucket
  - [ ] Companion infra repo PR opened (or ticket filed) for CloudFormation update
- **Wave**: 5
- **Risk**: Medium (broken deploy if config wrong)
- **Labels**: `rename`, `infrastructure`, `agent:infrastructure`
- **Owner**: infrastructure
- **Dependencies**: V32-W3-3 (EC2 cutover should be stable first)

### V32-W5-2: Empty + delete old mtga-companion-deploy-artifacts-staging bucket
- **Description**: After one successful staging release through the new bucket, empty + delete the old `mtga-companion-deploy-artifacts-staging` bucket. CONFIRM with Ray before deletion (S3 deletion is irreversible).
- **Acceptance criteria**:
  - [ ] Bucket emptied
  - [ ] Bucket deleted
  - [ ] Ray confirms deletion in PR comment before merge
- **Wave**: 5
- **Risk**: High (irreversible S3 deletion)
- **Labels**: `rename`, `infrastructure`, `agent:infrastructure`
- **Owner**: infrastructure
- **Dependencies**: V32-W5-1

### V32-W5-3: Vercel project rename + ALLOWED_ORIGINS cutover
- **Description**: Rename Vercel project `mtga-companion` → `vaultmtg` (or whatever Vercel allows — likely produces a new hostname). Update `ALLOWED_ORIGINS` in SSM (production + staging) to include the new Vercel hostname. Update BFF CORS test data: `services/bff/internal/config/config_test.go:295,306,423` and `config.go:50` doc-comment example.
- **Acceptance criteria**:
  - [ ] Vercel project renamed (or new project created if rename not supported)
  - [ ] SSM `ALLOWED_ORIGINS` updated with new hostname (keep old hostname for one release window)
  - [ ] BFF CORS tests updated
  - [ ] Preview deploys still work
- **Wave**: 5
- **Risk**: Medium (Vercel rename may break PR preview links temporarily)
- **Labels**: `rename`, `infrastructure`, `agent:infrastructure`
- **Owner**: infrastructure

### V32-W5-4: Companion repo `mtga-companion-web` — rename + brand string sweep
- **Description**: Per Ray's decision #5, the marketing/portfolio site repo (`RdHamilton/mtga-companion-web`, local path `/Users/ramonehamilton/Documents/Personal Projects/mtga-companion-web`) is in scope for v0.3.2. Sweep all `mtga-companion` / `MTGA-Companion` strings in source, configs, and content. Update domain references: `mtgacompanion.com` → `vaultmtg.app`. Rename the companion repo itself `mtga-companion-web` → `vaultmtg-web` via GitHub Settings. Update CI/CD that deploys this repo to reference new bucket/CFN names if relevant.
- **Acceptance criteria**:
  - [ ] No `mtga-companion` strings remain in companion repo (`rg -i mtga.companion` returns zero)
  - [ ] Domain refs updated to `vaultmtg.app`
  - [ ] Companion repo renamed `mtga-companion-web` → `vaultmtg-web` via GitHub Settings
  - [ ] Companion repo README updated with new brand strings
  - [ ] Deploy still works against the new naming (smoke test deploy)
- **Wave**: 5
- **Risk**: Medium (separate repo with its own CI/CD)
- **Labels**: `rename`, `frontend`, `agent:frontend`
- **Owner**: front-engineer
- **Dependencies**: V32-W5-1 (S3 bucket rename must be stable first if web repo deploys via that bucket — verify in PR description)

### V32-W5-5: PostHog event name rename — `mtga_companion.*` → `vaultmtg.*`
- **Description**: Per Ray's decision #6, rename all PostHog event names from `mtga_companion.*` to `vaultmtg.*`. Not enough event volume to preserve continuity dashboards — clean rename is fine. Audit all `posthog.capture()` call sites in `frontend/src/` and any backend event emission. Update PostHog dashboards/funnels/insights that reference the old event names. Update `docs/engineering/posthog-event-schema.md` (if exists) with the new naming convention.
- **Acceptance criteria**:
  - [ ] All `posthog.capture("mtga_companion.*")` call sites renamed in code
  - [ ] PostHog instance dashboards/funnels updated to reference new event names
  - [ ] Event schema doc updated with new naming
  - [ ] `rg "mtga_companion\." frontend/src services/` returns zero matches in event-emitting code
  - [ ] Smoke test: trigger a known event, verify it lands in PostHog under new name
- **Wave**: 5
- **Risk**: Low (event continuity sacrificed by decision; pre-launch volume too low to matter)
- **Labels**: `rename`, `frontend`, `analytics`, `agent:frontend`
- **Owner**: front-engineer

---

## Wave 6 — DB rename, and CI auth env (3 tickets, last)

### V32-W6-1: Rename PostgreSQL database mtga_companion → vaultmtg
- **Description**: Per Ray's decision #3, rename the database. Strategy: use `ALTER DATABASE "mtga_companion" RENAME TO "vaultmtg"` if no active connections (cheapest), else dump/restore. Update SSM `/vaultmtg/{production,staging}/db-name` from `mtga_companion` → `vaultmtg`. Update `docs/infra/staging-db-runbook.md:184,198`. Per Ray's decision #3: **NO maintenance window required** — there are no users in staging or production yet. Just stop the BFF, run the rename, restart the BFF.
- **Acceptance criteria**:
  - [ ] Staging DB renamed first, validated, then production
  - [ ] BFF stopped → DB renamed → SSM updated → BFF restarted (no announcement needed — pre-user)
  - [ ] BFF reconnects cleanly to renamed DB
  - [ ] Runbook updated
  - [ ] Backup taken before rename (still good practice even pre-user)
- **Wave**: 6
- **Risk**: Medium (data integrity, but downtime risk is removed by decision #3)
- **Labels**: `rename`, `database`, `agent:dba`, `priority: critical-path`
- **Owner**: dba
- **Dependencies**: V32-W3-2 (BFF must be reading new SSM tree first)

### V32-W6-2: Update GONOSUMDB / GOPRIVATE in all workflows
- **Description**: Update every `.github/workflows/*.yml` env that sets `GONOSUMDB` or `GOPRIVATE` from `github.com/RdHamilton/MTGA-Companion` to `github.com/RdHamilton/vault-mtg`. ~40 occurrences across `ci.yml`, `daemon.yml`, `daemon-release.yml`, `e2e-smoke.yml`, `integration.yml`, `release.yml`, `staging-deploy.yml`, `sync.yml`. Verify CI is green after merge. Note: repo rename ships in Wave 0.5; GitHub redirect makes the old URL keep working, but every workflow should reference the canonical new URL by end of v0.3.2.
- **Acceptance criteria**:
  - [ ] All workflow files updated
  - [ ] `rg 'RdHamilton/MTGA-Companion' .github/workflows/` returns zero matches
  - [ ] CI is green on a test PR
- **Wave**: 6
- **Risk**: Medium (broken CI = broken merges)
- **Labels**: `rename`, `ci`, `infrastructure`, `agent:infrastructure`, `priority: critical-path`
- **Owner**: infrastructure
- **Dependencies**: V32-W05-1 (repo already renamed; GONOSUMDB/GOPRIVATE updates are now canonical)

### V32-W6-3: Final sweep + verify zero remaining references
- **Description**: Run a full-repo `rg -i 'mtga.companion|mtga_companion|mtgacompanion'` after all waves complete. Ignore the audit file itself (intentional historical record). Any other remaining matches get triaged: either fixed in this PR, or filed as carry-forward tickets.
- **Acceptance criteria**:
  - [ ] Final `rg` sweep run and output committed to `docs/product/milestones/v0.3.2/final-sweep.md`
  - [ ] All remaining matches either fixed or have a carry-forward ticket
  - [ ] Wave-close report written
- **Wave**: 6
- **Risk**: Low (verification step)
- **Labels**: `rename`, `documentation`
- **Owner**: any engineer
- **Dependencies**: All other Wave 6 tickets

---

## Summary

- **Total tickets**: 34 (1 W0 + 1 W0.5 + 8 W1 + 5 W2 + 6 W3 + 5 W4 + 5 W5 + 3 W6)
- **Critical-path tickets**: 10 (marked `priority: critical-path`, including W0.5 repo rename)
- **High-risk tickets**: 6 (W3-2, W3-3, W4-2, W4-3, W5-2, ADR-021 blocking) — W6-1 risk reduced by decision #3 (no maintenance window required)
- **Owners**: backend-engineer (10), infrastructure (10), front-engineer (4), dba (2), architect (1), Ray+infra (1), any (6)

### Wave dependency graph
```
W0 (ADR) ──> W0.5 (repo rename) ──> W1 (docs) ─┐
                                                ├──> W3 (SSM/EC2/CFN) ──> W5 (S3/Vercel/web/PostHog) ──┐
                              W2 (code) ───────┘                                                      ├──> W6 (DB+CI)
                                              ──> W4 (daemon, gates W2-4) ───────────────────────────┘
```

Wave 0.5 (repo rename) sits between ADR and Wave 1 so all downstream work references the canonical new URL.
W1 and W2 may run in parallel after W0.5. W3 and W4 may run in parallel after W2. W5 follows W3. W6 is last.

### Decision-driven changes from initial plan
| Decision | Effect on tickets |
|---|---|
| #1 Repo rename to Wave 0.5 | New ticket `V32-W05-1`. Old `V32-W6-2` (repo rename in W6) removed; W6 renumbered to 3 tickets. |
| #2 Companion repos in v0.3.2 | New tickets `V32-W3-6` (mtga-companion-infra) and `V32-W5-4` (mtga-companion-web). |
| #3 No DB maintenance window | `V32-W6-1` ACs trimmed (no announcement/window scheduling). |
| #4 17lands rename, no outreach | `V32-W1-6` description clarifies — no outreach step. |
| #5 rhamiltoneng CFN tag → `vaultmtg` | `V32-W3-5` ACs explicit. |
| #6 PostHog event rename | New ticket `V32-W5-5`. |
