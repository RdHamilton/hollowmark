# Product Manager Changelog

<!-- Entries are appended newest-first. Format:
## YYYY-MM-DD — [Initiative name]
**Triggered by**: [CS feedback / BA report / Finance alert / user request]
**Decision**: [what was prioritized and why]
**Output**: [PRD filename or ticket numbers created]
**RICE score**: [if applicable]
-->

## 2026-05-06 — v0.2.0 follow-on tickets from architect tech spec

**Triggered by**: User request — architect's v0.2.0-event-projection.md flagged two untracked work items
**Decision**: Created two tickets to cover gaps the architect identified: (1) SPA history page wiring (blocked on #1396, Wave 3, front-engineer), and (2) DaemonEvent contract type update to promote event_id to a top-level field (Wave 1, backend-engineer). Both added to v0.2.0 board #28 as Todo. Updated #1396 body with cross-reference to #1405.
**Output**: GitHub issues #1404 (SPA wiring) and #1405 (contract type); both on board #28 status=Todo; #1396 updated
**RICE score**: N/A — follow-on tickets mandated by approved tech spec, not discretionary prioritization

## 2026-05-06 — v0.2.0 Kickoff: P0 backlog review and user stories

**Triggered by**: User request — v0.2.0 sprint start
**Decision**: Confirmed P0 backlog in dependency wave order. Elevated daemon health indicator from P1 to P0 (named in exit gate). Confirmed #983 is out of v0.2.0 scope (tier enforcement is v0.4.0). Identified schema migration as a missing ticket that is a hard blocker for B3. Noted three PM action items: (1) Ray confirms daemon installer is publicly hosted, (2) Ray creates Sentry account and stores DSN in SSM, (3) project-manager confirms board #28 composition.
**Output**: docs/prd/v0.2.0-kickoff.md — full user stories with ACs for B3, B5, B7, health indicator, Sentry, schema migration, MatchHistory endpoint, DraftHistory endpoint. Confirmed 4-wave execution order.
**RICE score**: Health indicator: P0 (exit gate); EmptyState: 1900; Sentry: 1980; Projection layer: 263 (enabling)

## 2026-05-06 — Beta Roadmap PRD

**Triggered by**: Synthesis of 6 specialist agent reports (Architect, PM, CS, BA, Finance, Growth Marketing)
**Decision**: Defined 3-milestone roadmap (v0.2.0 Foundation → v0.3.0 Telemetry Parity → v0.4.0 Beta Launch). v0.3.0 is the internal beta gate; v0.4.0 is public beta. AI agents and RAG infrastructure explicitly deferred post-beta. Do not introduce Stripe before 1,000 MAU.
**Output**: docs/prd/beta-roadmap.md
**RICE score**: Auth+onboarding: 450 | EmptyState: 1900 | Sentry: 1980 | Full telemetry: 650 | Shareable stats: 1500 | AI agents: 333 (deferred)
