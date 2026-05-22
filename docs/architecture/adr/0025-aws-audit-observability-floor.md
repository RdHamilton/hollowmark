# ADR-0025: AWS Audit / Observability Floor for a Solo-Operated Account

**Status**: Accepted
**Date**: 2026-05-21
**Decider**: Ramone Hamilton (account owner), Ray (Architect)
**Related**: ADR-0019 (staging environment)

## Context

VaultMTG's AWS account (`901347789205`) is solo-owned but operated by
roughly ten autonomous SDLC agents that hold AWS credentials. The account
is at beta scale: one EC2 BFF instance, one RDS instance, a handful of
S3 buckets, and a sync Lambda. There is no second human operator and no
formal compliance obligation (no SOC 2, no contractual audit-log
retention requirement).

The v0.3.1.1 "Infrastructure & Workflow Hardening" security backlog
(Sarah's H-01 review, plan item S-01) specified a CloudTrail standup with
an enterprise-grade feature set:

- a **multi-region** trail,
- **log-file integrity validation** (SHA-256 digest chain),
- a dedicated S3 bucket with **Object Lock** (Governance mode, later a
  proposed upgrade to Compliance mode — issue #2377),
- **S3 data-event selectors** on the artifact and trail buckets,
- and a five-alarm security suite (issue #2333: cross-tier SSM reads,
  IAM writes, failed `AssumeRole`, root usage, Secrets Manager access).

That feature set was codified in `cloudformation/cloudtrail.yml` and
merged via infra PR #51 on 2026-05-19. The issue (#2324) auto-closed on
merge — but **the stack was never deployed**: `describe-trails`
(including shadow trails, all regions) returns empty, no
`vault-mtg-cloudtrail` CloudFormation stack exists, and no logs bucket
exists. This is the same merged-but-not-deployed gap previously seen with
R-13.

Two problems surfaced:

1. **The trail genuinely needs to exist.** With no persistent trail, the
   only audit capability is the free 90-day CloudTrail Event History. Any
   security incident investigation older than 90 days loses all
   evidence. For an account whose credentials are held by ten autonomous
   agents, a basic audit trail is a real, justified control.
2. **The specified feature set is enterprise-tier ceremony.** Multi-region
   trails, integrity validation, Object Lock, data-event selectors, and a
   broad alarm suite are designed to defend against threats — a malicious
   insider tampering with audit logs, evidence forensics across regions,
   tamper-evident hash chains for legal proceedings — that do not exist
   for a solo-operated beta with no separate operators and no compliance
   mandate. Object Lock Compliance mode is additionally *irreversible*,
   which is an operational hazard with zero offsetting security gain
   here.

## Decision

**Adopt a deliberately minimal AWS audit/observability floor.** Keep
CloudTrail; descope every enterprise-tier feature whose threat model does
not apply to a solo-operated account.

### Audit trail (issue #2324, re-scoped)

A single CloudTrail trail, defined in `cloudformation/cloudtrail.yml`:

- **Single-region** (`us-east-1`, `IsMultiRegionTrail: false`).
- **Management events only** (`ReadWriteType: All`,
  `IncludeManagementEvents: true`). No data-event selectors.
- `IncludeGlobalServiceEvents: true` so IAM / STS / CloudFront
  global-service events are still captured.
- One dedicated S3 logs bucket: SSE-KMS encryption with a dedicated
  single-region KMS key, public access fully blocked, HTTPS-only bucket
  policy, versioning enabled.
- **No** log-file integrity validation.
- **No** Object Lock (Governance or Compliance).
- All resources managed as IaC; deployed via the infra `deploy.yml`
  workflow.

### Security alarms (issue #2333, trimmed)

Two **high-signal** CloudWatch alarms, not five:

- **Root account usage** — `{ $.userIdentity.type = "Root" }`. Root should
  never be used for routine operations; any occurrence is anomalous.
- **IAM policy / credential writes** — `CreateRole`, `AttachRolePolicy`,
  `PutRolePolicy`, `CreateAccessKey`, `CreatePolicy`,
  `UpdateAssumeRolePolicy`, etc. The privilege-escalation surface, with
  low steady-state event volume.

Dropped as low-signal / high-noise for this account: cross-tier SSM reads
(agents read across SSM paths routinely), failed `AssumeRole` (routine
CI/credential-rotation noise), and Secrets Manager access (normal app and
agent operation). A noisy alarm trains operators to ignore it.

### Explicitly out of scope

- Multi-region CloudTrail.
- CloudTrail log-file integrity validation.
- S3 Object Lock — Governance and Compliance mode (issue #2377 closed
  `wontfix`).
- CloudTrail S3 data-event selectors (issue #2415 closed `wontfix`).
- GuardDuty (issue #2338) — tracked separately, unaffected by this ADR.

## Consequences

**Positive**

- A persistent audit trail actually exists once the descoped stack
  deploys, closing the real "evidence lost after 90 days" gap.
- The alarm set is small enough that every firing is worth investigating
  — no alarm fatigue.
- No irreversible Object Lock configuration to regret; the bucket remains
  fully manageable.
- Lower cost and lower operational surface, appropriate to beta scale.

**Negative / accepted risk**

- No tamper-evidence on the audit log. A principal with sufficient AWS
  access could alter or delete log objects. **Accepted**: there is no
  separate operator this control would defend against; the credential
  holders *are* the trusted parties.
- Events originating in non-`us-east-1` regions are not captured beyond
  global-service events. **Accepted**: VaultMTG's entire footprint is in
  `us-east-1`; a region change would itself be a notable, reviewable
  event.
- No object-level (data-event) audit of S3 access. **Accepted**: bucket
  policies and public-access blocks are the primary S3 control;
  object-level forensics is not a beta requirement.

**Revisiting trigger**

If VaultMTG takes on a formal compliance obligation (SOC 2, a contractual
audit-log retention requirement) or adds separate human operators with
distinct trust levels, this floor is re-evaluated and a successor ADR
designed against that specific requirement. This ADR documents the
*current* floor, not a permanent ceiling.
