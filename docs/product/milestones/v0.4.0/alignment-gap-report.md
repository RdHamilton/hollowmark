# v0.4.0 Alignment Gap Report

**Date**: 2026-05-09
**Author**: Product Manager
**Sources**:
- `docs/product/beta-roadmap.md` (last updated 2026-05-08)
- `docs/product/milestones/v0.4.0/kickoff.md` (2026-05-09)
- Project Board #30, 100 items queried (2026-05-09)

---

## Executive Summary

100 tickets on Board #30. Cross-reference exposes five categories of gaps:
1. Four Wave 0 brittleness tickets exist in kickoff but have no board issues yet.
2. Eight Wave 1 engineering items — including three named tickets — are absent from the board.
3. All five Storybook/Chromatic Wave 1 tickets are TBD and not on the board.
4. Six Wave 1 business-track tickets exist on the board but have NO STATUS (not triaged to Todo).
5. ~55 NO STATUS tickets on the board are either out-of-scope per kickoff §8, stale pre-v0.3.0 era, or need milestone assignment.

---

## Gap 1 — Wave 0: Four TBD Tickets Have No Board Issues

The kickoff names five T1–T5 brittleness tickets. Only T2 (#1041) has a GitHub issue — and #1041 is **not on Board #30** (no milestone assignment to #68).

| Finding | Kickoff label | Board status |
|---------|---------------|-------------|
| Extract `knownFormats` to `handlers/formats.go` | T1 | **NO TICKET** |
| Align daemon `models.go` JSON keys | T2 = #1041 | **NOT ON BOARD #30** |
| Remove projection worker contract struct redeclarations | T3 | **NO TICKET** |
| Add `projection_errors` dead-letter table | T4 | **NO TICKET** |
| Filter partial GRE events from aggregate queries | T5 | **NO TICKET** |

**Note**: #1520 (`add partial column to game_plays`) and #1519 (`GRE session flush threshold`) exist on Board #30 with NO STATUS — they are partial implementations of T5 but are not formally mapped to Wave 0.

**Corrective actions (HAND TO PROJECT-MANAGER):**
- [ ] Create ticket for T1 (XS effort, backend-engineer, `architecture` + `v0.4.0` labels, Milestone #68)
- [ ] Create ticket for T3 (S effort, backend-engineer)
- [ ] Create ticket for T4 (M effort, backend-engineer + dba) — includes migration script
- [ ] Create ticket for T5 (S effort, backend-engineer) — ACs in kickoff §5
- [ ] Add #1041 to Board #30 / Milestone #68
- [ ] Add #1519 and #1520 to Wave 0 scope (they are partial T5 work) and set status to Todo

---

## Gap 2 — Wave 1 Engineering: Three Named Tickets Absent from Board

| Ticket | Title | Board status |
|--------|-------|-------------|
| #1398 | Daemon onboarding flow | **NOT ON BOARD #30** |
| #1541 | Free constructed win rate dashboard | **NOT ON BOARD #30** |
| #1540 | Brawl commander analytics | **NOT ON BOARD #30** |

**Note on #1398**: Gated on OQ-1 (Clerk Pro decision). Kickoff says it stays parked until Ray decides. Still must be on Board #30 with a blocking dependency noted.

**Note on #1598**: `Beta invite flow architecture` is on the board as Todo, but kickoff calls for a full implementation ticket (frontend + backend, with ACs). #1598 appears to be architecture planning only, not the implementation ticket. Kickoff explicitly says "project-manager creates ticket."

**Corrective actions (HAND TO PROJECT-MANAGER):**
- [ ] Add #1398 to Board #30 / Milestone #68 (label it blocked on OQ-1)
- [ ] Add #1541 to Board #30 / Milestone #68
- [ ] Add #1540 to Board #30 / Milestone #68
- [ ] Create full beta invite flow implementation ticket (not just architecture #1598) — backend + frontend, Wave 1, full ACs from kickoff §5 user story 3

---

## Gap 3 — Wave 1 Engineering: BFF Hardening Tickets Missing

Kickoff §5 Wave 1 explicitly calls for two medium-priority BFF hardening tickets:

| Finding | Kickoff reference | Board status |
|---------|------------------|-------------|
| Account lookup cache (T8) | Wave 1 BFF hardening | **NO TICKET** |
| Request timeout middleware (T9) | Wave 1 BFF hardening | **NO TICKET** |

**Corrective actions (HAND TO PROJECT-MANAGER):**
- [ ] Create T8 ticket (account lookup cache, S effort, backend-engineer, Wave 1)
- [ ] Create T9 ticket (request timeout middleware, S effort, backend-engineer, Wave 1)

---

## Gap 4 — Wave 1: All Five Storybook/Chromatic Tickets Missing

Kickoff §5 contains a full Visual Testing track (5 tickets) with detailed ACs. None exist on Board #30.

| Ticket | Title | Effort |
|--------|-------|--------|
| TBD | Discovery spike: Storybook + Chromatic on React 19 + Vite | XS (2 days) |
| TBD | Install and configure Storybook 8 with Vite builder | S |
| TBD | Write stories for all existing UI components | M |
| TBD | Integrate Chromatic into CI pipeline | S |
| TBD | Capture Chromatic baseline snapshots + approve | XS |

Wave 1 close checklist explicitly requires "Chromatic baseline captured and all stories accepted by ux-designer." This is a **hard Wave 1 gate** with no tickets.

**Corrective actions (HAND TO PROJECT-MANAGER):**
- [ ] Create all 5 Storybook/Chromatic tickets with ACs from kickoff §5 Visual Testing section, assign to front-engineer (stories) and infrastructure (Chromatic CI), Wave 1, Milestone #68

---

## Gap 5 — Wave 1 Business Track: Six Tickets Stranded at NO STATUS

All six Wave 1 business-track tickets exist on Board #30 but have NO STATUS — they have not been triaged to Todo.

| Ticket | Title | Current status |
|--------|-------|---------------|
| #1576 | Waitlist launch coordination — June 2 open date | NO STATUS |
| #1577 | Beta invite email copy and Clerk invite flow | NO STATUS |
| #1578 | NPS survey | NO STATUS |
| #1579 | Beta FAQ / onboarding doc | NO STATUS |
| #1580 | PostHog activation funnel definition | NO STATUS |
| #1581 | PostHog feature flag setup for closed-beta cohort | NO STATUS |

**Note**: #1576 (waitlist launch June 2) is **time-critical** — waitlist opens 2026-06-02, 24 days from now. If this is still in NO STATUS it has no owner driving it.

**Corrective actions (PROJECT-MANAGER board moves):**
- [ ] Move #1576–#1581 to Todo on Board #30
- [ ] Confirm owner assigned on #1576 immediately (growth-marketing); escalate to Ray if no owner

---

## Gap 6 — Board Tickets Outside Kickoff Scope (No Roadmap Alignment)

### 6a. Stripe/Tier Enforcement — Deferred to GA, Still on Board

Kickoff §8 explicitly defers all Stripe and tier work. These 10 tickets are on Board #30 with NO STATUS and no milestone — they create noise and risk accidental assignment.

| Tickets | Category |
|---------|----------|
| #980, #982, #1259, #1306 | Stripe billing / pricing strategy |
| #1381, #1382, #1383, #1384, #1385, #1386 | BFF tier enforcement |

**Corrective action (KICKOFF DOC / BOARD):**
- [ ] Remove #980, #982, #1259, #1306, #1381–#1386 from Board #30 or explicitly mark them as deferred (add label `post-ga`, remove from Milestone #68 if assigned). Do NOT close them — they belong on the Post-Beta board.

### 6b. ML / 17Lands — Sidelined, Still on Board

| Tickets | Category |
|---------|----------|
| #1589 | ADR-015 Lambda batch pattern (sidelined §8) |
| #1591 | Deck-archetype classifier |
| #1592 | 17Lands bulk CSV ingestion (sidelined §8) |
| #1593 | craft_recommendations nightly batch Lambda |
| #1594 | GET /v1/user/craft-next BFF endpoint |
| #1595 | Smart Craft Next UI panel |

These tickets map to the "Draft ML win-rate pick advisor" and "17Lands bulk CSV ingestion" explicitly sidelined in kickoff §8. They should not appear on the v0.4.0 board.

**Corrective action (BOARD):**
- [ ] Remove #1589, #1591–#1595 from Board #30 or move to a post-beta board. Update kickoff §8 to note ticket numbers explicitly.

### 6c. Stale Pre-v0.3.0 Era Tickets — 38 NO STATUS Items

~38 tickets numbered below #1050 have NO STATUS, no milestone, and no wave mapping. They predate the v0.3.0 architecture decisions. Examples: #966 (GitHub MCP agent setup), #969–#978 (old EC2 deploy plumbing), #999–#1012 (original monorepo scaffold), #1036–#1038 (tiered test strategy), #1045–#1049 (sync service EC2).

Most of these are superseded by ADR decisions (Lambda over EC2, Clerk over custom auth, etc.).

**Corrective action (KICKOFF DOC / BOARD):**
- [ ] PM + LE triage session: close or reassign each. Do not leave on Board #30 — they pollute the backlog and inflate the 100-ticket count. Target: ≤60 active tickets on Board #30 after triage.

---

## Gap 7 — Wave 2 Tickets: No Issues Created

Wave 2 has 5 items in kickoff, only #1542 (shareable player stats) exists as a GitHub issue — and it's not on Board #30 either.

| Kickoff item | Status |
|-------------|--------|
| T6: Daemon local SQLite event queue | NO TICKET |
| T7: NOTIFY/LISTEN projection worker | NO TICKET |
| ML deck building — degraded mode | NO TICKET |
| Collection log parsing spike (#1543) | NOT ON BOARD #30 |
| #1542: Shareable player stats | NOT ON BOARD #30 |

Wave 2 doesn't start until Wave 1 closes (post-August 18), so these are not blocking. But they need to exist on the board before Wave 1 closes per the wave kickoff checklist.

**Corrective action (HAND TO PROJECT-MANAGER — lower priority):**
- [ ] Create T6, T7, ML degraded mode tickets with placeholder ACs (architect fills ACs before Wave 2 starts)
- [ ] Add #1542 and #1543 to Board #30 as Wave 2 Todo
- [ ] Add Wave 2 items to Board #30 before Wave 1 close report

---

## Gap 8 — Roadmap vs. Kickoff Theme Tension

The beta roadmap states v0.4.0 accomplishes: "Shareable stats pages enable organic viral loops." The kickoff places #1542 (shareable stats) in **Wave 2**, which starts post-August 18 — after the beta is already open. The roadmap's Growth Ready exit gate requires "Shareable stats URL works and renders OG preview on Discord/Reddit."

This is a **contradiction**: the roadmap exit gate requires shareable stats; the kickoff defers it to Wave 2 (post-beta). One of these must change.

**Options:**
- A. Move #1542 to Wave 1 (tighter scope — public profile page only, no season card). Satisfies roadmap exit gate.
- B. Update roadmap exit gate to remove shareable stats requirement, acknowledging Wave 1 data must stabilize first.

**Corrective action (REQUIRES RAY DECISION):**
- [ ] Ray decides: shareable stats in Wave 1 (Option A) or roadmap exit gate updated (Option B). PM updates whichever document is wrong after the decision. Flag as **Ray Action Item**.

---

## Gap 9 — PostHog Engineering Ownership Gap

Roadmap deliverable #2: "PostHog in SPA — funnels, retention, activation tracking wired." The board has:
- #1614, #1615, #1616, #1617 — marketing-owned PostHog/UTM tracking (Todo)
- #1580, #1581 — analytics-owned funnel definition (NO STATUS)
- No engineering-owned ticket for installing and wiring PostHog into the SPA itself

The activation funnel (#1580) cannot emit events if PostHog is not installed in the SPA. This is a dependency gap — #1580 depends on a PostHog SPA install ticket that does not exist on the board.

**Corrective action (HAND TO PROJECT-MANAGER):**
- [ ] Create eng ticket: "feat(frontend): install PostHog JS SDK in SPA and instrument activation events (`signed_up`, `daemon_connected`, `first_draft_started`, `first_draft_complete`)" — Wave 1, front-engineer, prerequisite to #1580

---

## Prioritized Corrective Action List

| Priority | Action | Handler | Wave impact |
|----------|--------|---------|-------------|
| P0 | Create T1, T3, T4, T5 tickets + add #1041/#1519/#1520 to Board #30 | project-manager | BLOCKS Wave 0 start |
| P0 | Move business track #1576–#1581 to Todo; assign #1576 owner NOW | project-manager | #1576 is 24 days from deadline |
| P0 | Add #1398, #1541, #1540 to Board #30 | project-manager | Wave 1 scope incomplete |
| P0 | Create full beta invite flow implementation ticket | project-manager | Wave 1 exit gate |
| P0 | Create PostHog SPA install ticket (prerequisite to #1580) | project-manager | Wave 1 exit gate |
| P1 | Create 5 Storybook/Chromatic tickets | project-manager | Wave 1 close gate |
| P1 | Create T8 + T9 BFF hardening tickets | project-manager | Wave 1 scope |
| P1 | Ray decides shareable stats (Gap 8 Option A or B) | Ray | Roadmap/kickoff consistency |
| P2 | Remove/label Stripe, ML, sidelined tickets off Board #30 | project-manager | Board hygiene |
| P2 | Triage 38 stale pre-v0.3.0 tickets — close or reassign | PM + LE | Board hygiene |
| P3 | Create Wave 2 tickets (T6, T7, ML degraded, add #1542/#1543) | project-manager | Pre-Wave 1 close |

---

## Documents Requiring Updates

| Document | Change needed |
|----------|--------------|
| `kickoff.md` §5 Wave 0 | Add ticket numbers once project-manager creates T1/T3/T4/T5 |
| `kickoff.md` §5 Wave 1 | Add ticket numbers once project-manager creates beta invite, BFF hardening, Storybook tickets |
| `kickoff.md` §8 Out of Scope | Add ML ticket numbers (#1589, #1591–#1595) explicitly |
| `beta-roadmap.md` §3 Milestone 3 exit gate | Update shareable stats exit gate per Ray's decision (Gap 8) |
| `beta-roadmap.md` §3 Milestone 3 | Mark roadmap status as ACTIVE (currently shows v0.3.0 as ACTIVE, no mention of v0.4.0 active state) |
