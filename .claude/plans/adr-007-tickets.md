# ADR-007 Implementation Tickets

This file is the spec the Project Manager uses to create GitHub issues that
implement ADR-007 (Frontend Serving Model). All tickets are Sonnet-ready
(< 2 hours, < 6 files).

Reference: `docs/adr/007-frontend-serving-model.md`

---

### Ticket: Demote EC2 frontend deploy workflow to manual-dispatch only

**Labels**: infrastructure, frontend
**Priority**: P0
**Agent**: infrastructure
**Estimated Effort**: 30 minutes
**Files Expected to Change**: 1 (`.github/workflows/frontend.yml`)

**Description**:
Per ADR-007, the EC2 nginx + S3 + SSM frontend deploy workflow must no longer
trigger on push to `main`. Production frontend traffic is served by Vercel.
The EC2 path is preserved for disaster recovery and future staging only and
must run on `workflow_dispatch` exclusively.

**Acceptance Criteria**:
- [ ] `.github/workflows/frontend.yml` no longer contains the `push:` trigger
- [ ] `workflow_dispatch: {}` remains as the only trigger
- [ ] A comment block at the top of the file states: "EC2 frontend deploy is preview/DR only — see ADR-007. Production frontend is served by Vercel."
- [ ] Manual `workflow_dispatch` invocation still produces a working build and SSM deploy (smoke test once)
- [ ] No path filter changes are needed (since `push` is removed entirely)

**Depends on**: none (ADR-007 PR must be merged first)

---

### Ticket: Delete redundant deploy-frontend.yml from mtga-companion-infra

**Labels**: infrastructure
**Priority**: P0
**Agent**: infrastructure
**Estimated Effort**: 15 minutes
**Files Expected to Change**: 1 (in `mtga-companion-infra` repo)
**Note**: This expands existing issue #1211 — the PM should add this ticket's
acceptance criteria to #1211 rather than creating a new issue.

**Description**:
The infra repo contains `.github/workflows/deploy-frontend.yml`, a duplicate
manual-dispatch frontend deploy workflow that uses static IAM keys and has
been failing. Per ADR-007 and the original #1211 proposal, delete it. The
canonical EC2 frontend deploy (now manual-dispatch only) lives in the app repo.

**Acceptance Criteria**:
- [ ] `mtga-companion-infra/.github/workflows/deploy-frontend.yml` is deleted
- [ ] No other workflow in the infra repo references `DEPLOY_BUCKET` for frontend purposes
- [ ] PR opened in `mtga-companion-infra` references ADR-007 and closes #1211

**Depends on**: ADR-007 PR merged

---

### Ticket: Annotate nginx static-serve block as DR-only

**Labels**: infrastructure, documentation
**Priority**: P1
**Agent**: infrastructure
**Estimated Effort**: 20 minutes
**Files Expected to Change**: 2 (in `mtga-companion-infra`: `mtga-companion.conf`, `mtga-companion-ssl.conf`)

**Description**:
The nginx config files in `mtga-companion-infra` contain a `location /` block
that serves the SPA from `/var/www/mtga-companion/`. Per ADR-007, this block
is preserved for DR/preview only and must be clearly marked so future readers
do not assume it serves production traffic.

**Acceptance Criteria**:
- [ ] Both nginx config files have a comment block above the `location /` block that says: `# DR/preview only — production frontend is served by Vercel. See ADR-007.`
- [ ] No functional config change (the block still works for manual deploys)
- [ ] PR opened in `mtga-companion-infra` references ADR-007

**Depends on**: ADR-007 PR merged

---

### Ticket: Update DEPLOYMENT and root README to document Vercel-canonical frontend

**Labels**: documentation, frontend
**Priority**: P1
**Agent**: backend-engineer (docs author; no Go code)
**Estimated Effort**: 45 minutes
**Files Expected to Change**: 2-3 (`README.md`, `docs/DEPLOYMENT.md` if present, possibly `frontend/README.md`)

**Description**:
Project-level docs must state that the production frontend is served by Vercel
and that the EC2 nginx static-serve path is DR/preview only. This unblocks
#1066 (the sync README rewrite, which depends on a coherent deploy story).

**Acceptance Criteria**:
- [ ] Root `README.md` "Architecture" or "Deployment" section names Vercel as the production frontend host
- [ ] If `docs/DEPLOYMENT.md` exists, it contains a "Frontend serving model" subsection that summarises ADR-007 and links to it
- [ ] If `frontend/README.md` exists, it links to ADR-006 and ADR-007 for production deploy context
- [ ] No mention of EC2 nginx as a production frontend host remains in any doc
- [ ] PR description links #1066 as related (does not close it)

**Depends on**: ADR-007 PR merged

---

### Ticket: Verify production DNS resolves to Vercel for mtgacompanion.com

**Labels**: infrastructure
**Priority**: P0
**Agent**: infrastructure
**Estimated Effort**: 30 minutes (verification only) or 1 hour (if cutover needed)
**Files Expected to Change**: 0 (verification) or 1-2 (Route53 / DNS provider config)

**Description**:
ADR-007 requires that `mtgacompanion.com` and `www.mtgacompanion.com` resolve
to Vercel. Verify current DNS and, if either record currently points at the
EC2 elastic IP, schedule and execute a cutover.

**Acceptance Criteria**:
- [ ] `dig mtgacompanion.com` and `dig www.mtgacompanion.com` are resolved and the resolved IP / CNAME is documented in the ticket
- [ ] If both already point at Vercel: ticket closed with a comment "verified — no cutover needed"
- [ ] If either points at EC2: a cutover plan is posted on the ticket, the cutover is executed, and post-cutover verification is documented
- [ ] `api.mtga-companion.com` (or equivalent BFF subdomain) is verified to point at the EC2 instance

**Depends on**: none — can be done in parallel with the workflow edits

---

### Ticket: Smoke-test Vercel preview deploy to confirm BFF connectivity post-ADR-007

**Labels**: frontend, infrastructure
**Priority**: P2
**Agent**: front-engineer
**Estimated Effort**: 30 minutes
**Files Expected to Change**: 0 (verification only) or 1 (smoke test spec)

**Description**:
After ADR-007 lands and the EC2 push trigger is removed, confirm that the
end-to-end Vercel + BFF + RDS path still works end-to-end on a preview deploy
(per-PR Vercel build → cross-origin request to `api.mtga-companion.com` →
authenticated BFF response → DB read).

**Acceptance Criteria**:
- [ ] A throwaway PR is opened that only edits a comment in `frontend/`
- [ ] The Vercel preview URL is captured and tested manually for: SPA loads, login works, a `/api/v1/decks` GET returns 200
- [ ] CORS errors are absent from the browser console
- [ ] If a Playwright smoke covers this flow, it is run against the preview URL; otherwise note the coverage gap
- [ ] Findings posted on the ticket; throwaway PR closed without merge

**Depends on**: "Demote EC2 frontend deploy workflow" ticket merged

---

## Summary for the PM

Six tickets total:

| # | Title | Priority | Agent | Repo |
|---|---|---|---|---|
| 1 | Demote EC2 frontend deploy workflow to manual-dispatch only | P0 | infrastructure | MTGA-Companion |
| 2 | Delete redundant deploy-frontend.yml (expand #1211) | P0 | infrastructure | mtga-companion-infra |
| 3 | Annotate nginx static-serve block as DR-only | P1 | infrastructure | mtga-companion-infra |
| 4 | Update DEPLOYMENT and root README | P1 | backend-engineer (docs) | MTGA-Companion |
| 5 | Verify production DNS resolves to Vercel | P0 | infrastructure | (DNS provider) |
| 6 | Smoke-test Vercel preview deploy post-cutover | P2 | front-engineer | MTGA-Companion |

All tickets must be on the active milestone (Phase 2: AWS Deployment) and
labelled per the table above. Tickets 1, 2, 5 are P0 and unblock #1066 and
#1211. Ticket 6 is the verification step after the structural work lands.
