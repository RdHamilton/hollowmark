# project-manager handoff — v0.3.2 ticket creation

**Date**: 2026-05-10 (updated after Ray's 6 final decisions resolved)
**From**: PM (Najah)
**To**: project-manager agent
**Action**: Create 34 GitHub issues from the v0.3.2 ticket list and add them to project #34.

---

## Project + milestone context

- **GitHub Project**: #34 — "v0.3.2 — mtga-companion rename"
  - Project ID: `PVT_kwHOABsZ684BXSA8`
  - Status field ID: `PVTSSF_lAHOABsZ684BXSA8zhSf5p4`
  - Status options: Todo `f75ad846`, In Progress `47fc9ee4`, Done `98236657`
- **Milestone**: #71 — `v0.3.2`
- **Repo**: `RdHamilton/MTGA-Companion` (will be renamed to `RdHamilton/vault-mtg` in Wave 0.5; create issues against current name — GitHub redirect handles continuity)

---

## Ticket source

Read in full: `docs/product/milestones/v0.3.2/ticket-list.md`

That doc contains **34 tickets across 8 waves** (Wave 0, 0.5, 1, 2, 3, 4, 5, 6). For each ticket:
- Title (use exactly as written, dropping the `V32-Wn-N:` prefix if you prefer — keep prefix in the body for traceability)
- Description (turn into the issue body — preserve acceptance criteria as a checklist)
- Wave number, risk level, labels, owner, dependencies — preserve all of these in the issue body

---

## Required actions

1. **Create the `rename` label** if it doesn't exist. Color: `#5319e7` (purple).
2. **Create one GitHub issue per ticket** in the `RdHamilton/MTGA-Companion` repo.
3. **Apply labels** as listed per ticket. If a label doesn't exist, create it. Required labels: `rename` on every issue, plus `infra`/`backend`/`frontend`/`daemon`/`docs`/`ci`/`database`/`architecture`/`analytics`/`installer` per ticket. Plus `priority: critical-path` where flagged.
4. **Set milestone** to `v0.3.2` (#71) on every issue.
5. **Add every issue to project #34** with status `Todo`.
6. **Set the Wave field** in the issue body as a bold line at the top (e.g. `**Wave**: 0.5` or `**Wave**: 1`).
7. **Set dependencies** in the issue body using the `Dependencies:` section (cross-reference issue numbers once known — do this in a second pass if needed).

---

## Issue body template

```markdown
**Wave**: <0 | 0.5 | 1 | 2 | 3 | 4 | 5 | 6>
**Risk**: <Low | Medium | High>
**Owner**: <agent name>
**Source**: `docs/engineering/mtga-companion-rename-audit.md` + `docs/product/milestones/v0.3.2/ticket-list.md` (V32-Wn-N)

## Description
<from ticket-list.md>

## Acceptance criteria
- [ ] <each criterion as checkbox>

## Dependencies
- <issue numbers — fill in second pass>

## References
- ADR-021 (when written): `docs/architecture/decisions/ADR-021-rename-to-vaultmtg.md`
- Audit: `docs/engineering/mtga-companion-rename-audit.md`
```

---

## Order of creation (so dependency cross-refs resolve cleanly)

1. V32-W0-1 (ADR-021) — first, all other tickets depend on it
2. V32-W05-1 (repo rename) — Wave 0.5, must precede Wave 1+ doc/code/CI work
3. V32-W1-1 through V32-W1-8 — Wave 1 (8 tickets)
4. V32-W2-1 through V32-W2-5 — Wave 2 (5 tickets)
5. V32-W3-1 through V32-W3-6 — Wave 3 (6 tickets, includes new `mtga-companion-infra` companion repo ticket V32-W3-6)
6. V32-W4-1 through V32-W4-5 — Wave 4 (5 tickets)
7. V32-W5-1 through V32-W5-5 — Wave 5 (5 tickets, includes new `mtga-companion-web` ticket V32-W5-4 and PostHog rename V32-W5-5)
8. V32-W6-1 through V32-W6-3 — Wave 6 (3 tickets — repo rename was moved to Wave 0.5, so Wave 6 is now DB rename + GONOSUMDB workflow update + final sweep)

Total: **34 tickets**.

---

## Decisions reflected in the ticket list (do not re-litigate)

1. Brand casing: `VaultMTG` / `vaultmtg` / `vault-mtg` (per ADR-021)
2. Repo rename: scheduled as Wave 0.5 (single ticket V32-W05-1), not Wave 6
3. Database rename: NO maintenance window required (no users in staging/prod)
4. 17lands `ExportedFrom`: rename only, no outreach
5. rhamiltoneng CFN `Project` tag: rename to `vaultmtg`
6. PostHog event names: rename `mtga_companion.*` → `vaultmtg.*` (continuity not preserved — V32-W5-5)
7. Companion repos `mtga-companion-infra` and `mtga-companion-web`: in scope (V32-W3-6 and V32-W5-4)

---

## Verification

After creating, post a single comment on the milestone (or reply to PM) with:
- Total issues created (expect 34)
- Mapping of `V32-Wn-N` ticket id → GitHub issue number
- Confirmation all are on project #34 with Todo status
- Confirmation milestone v0.3.2 set on all
- Any tickets that failed to create (with reason)

---

## Do NOT do

- Do not move any ticket to In Progress — engineering hasn't started yet, and Wave 0 is gated on ADR-021 being written.
- Do not assign anyone — owners are documented in issue body, but assignment happens at wave kickoff.
- Do not close or modify any existing issues.
- Do not create issues outside the 34 listed. If you spot a gap, comment back to PM rather than improvising.
- Do not modify the ticket list itself — PM owns that file.
- Do not move tickets between waves — the wave assignment in the ticket list is final.
