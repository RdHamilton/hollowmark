---
name: cfn-iam-simulate
description: Simulate the gha-infra-cfn-deploy IAM role against every action a CloudFormation template invokes at runtime — run BEFORE opening any PR touching cloudformation/*.yml or iam-gha-roles.yml. Hard gate; catches runtime authorization failures that cfn-lint and changeset dry-runs cannot detect. Used by the back-end/infra engineers (Bob/Bianca/Ben) and Ray.
user-invocable: true
---

# Skill: /cfn-iam-simulate

## Purpose

Simulate IAM permissions for the `gha-infra-cfn-deploy` role against every action
a CloudFormation template can invoke at runtime. Engineers run this BEFORE opening a PR
that touches any CloudFormation template or IAM policy. It is a hard gate — do not open
the PR if any action is denied.

This catches runtime authorization failures that `cfn-lint` and changeset dry-runs
cannot detect.

## When to invoke

- Any PR touching `cloudformation/*.yml`
- Any PR touching `cloudformation/iam-gha-roles.yml` (new services require new action grants)
- Any PR adding new AWS resource types to an existing template

## Script

```bash
~/.claude/skills/cfn-iam-simulate/simulate.sh <role-arn> "<action1 action2 ...>" \
  [--resources <arn1,arn2,...>] \
  [--oidc-sub <subject-claim>]
```

**Arguments:**

| Argument | Required | Description |
|---|---|---|
| `role-arn` | Yes | IAM role ARN to simulate. Default deploy role: `arn:aws:iam::901347789205:role/gha-infra-cfn-deploy` |
| `"action1 action2 ..."` | Yes | Space-separated IAM action names, quoted as a single argument |
| `--resources` | No | Comma- or space-separated resource ARNs passed to `--resource-arns`. Defaults to `*`. |
| `--oidc-sub` | No | OIDC `sub` claim expected in the trust policy. Prints an advisory to verify the pin — see OIDC Trust section below. |

**Exit codes:**

| Code | Meaning |
|---|---|
| 0 | All actions allowed — `SIMULATION_RESULT: PASSED` |
| 1 | One or more actions denied — `SIMULATION_RESULT: FAILED` |
| 2 | Usage or AWS CLI error |

## Steps

### 1. Identify resource types in the template

```bash
grep "Type: AWS::" cloudformation/<template>.yml | awk '{print $2}' | sort -u
```

### 2. Map resource types to action sets

| CFN Resource Type | Action set key |
|---|---|
| AWS::Config::ConfigurationRecorder | config |
| AWS::Config::DeliveryChannel | config |
| AWS::Config::ConformancePack | config |
| AWS::GuardDuty::Detector | guardduty |
| AWS::AccessAnalyzer::Analyzer | access-analyzer |
| AWS::IAM::Role | iam-role |
| AWS::S3::Bucket | s3 |
| AWS::S3::BucketPolicy | s3 |
| AWS::CloudFormation::Stack | cloudformation |
| AWS::EC2::* | ec2 |
| AWS::RDS::* | rds |

### 3. Run the simulation

Deploy role ARN: `arn:aws:iam::901347789205:role/gha-infra-cfn-deploy`

```bash
# Example: template adds a GuardDuty detector and an IAM role
~/.claude/skills/cfn-iam-simulate/simulate.sh \
  "arn:aws:iam::901347789205:role/gha-infra-cfn-deploy" \
  "guardduty:CreateDetector guardduty:DeleteDetector guardduty:GetDetector \
   guardduty:UpdateDetector guardduty:ListDetectors \
   iam:CreateRole iam:DeleteRole iam:GetRole iam:PassRole \
   iam:AttachRolePolicy iam:DetachRolePolicy iam:ListAttachedRolePolicies"
```

**Common action sets by service:**

**Config:**
```
config:PutConfigurationRecorder config:StartConfigurationRecorder
config:StopConfigurationRecorder config:DeleteConfigurationRecorder
config:DescribeConfigurationRecorders
config:PutDeliveryChannel config:DeleteDeliveryChannel config:DescribeDeliveryChannels
config:PutConformancePack config:DeleteConformancePack
config:DescribeConformancePacks config:DescribeConformancePackStatus
```

**GuardDuty:**
```
guardduty:CreateDetector guardduty:DeleteDetector guardduty:GetDetector
guardduty:UpdateDetector guardduty:ListDetectors
guardduty:TagResource guardduty:UntagResource guardduty:ListTagsForResource
```

**AccessAnalyzer:**
```
access-analyzer:CreateAnalyzer access-analyzer:DeleteAnalyzer
access-analyzer:GetAnalyzer access-analyzer:ListAnalyzers
access-analyzer:UpdateAnalyzer access-analyzer:TagResource
access-analyzer:UntagResource access-analyzer:ListTagsForResource
access-analyzer:ValidatePolicy
access-analyzer:ListArchiveRules access-analyzer:CreateArchiveRule
access-analyzer:DeleteArchiveRule access-analyzer:UpdateArchiveRule
access-analyzer:GetArchiveRule
access-analyzer:ListFindings access-analyzer:GetFinding
access-analyzer:ListAnalyzedResources
```

**IAM role management:**
```
iam:CreateRole iam:DeleteRole iam:GetRole iam:PassRole
iam:AttachRolePolicy iam:DetachRolePolicy iam:ListAttachedRolePolicies
iam:PutRolePolicy iam:DeleteRolePolicy iam:GetRolePolicy iam:ListRolePolicies
iam:TagRole iam:UntagRole iam:ListRoleTags
```

**S3:**
```
s3:CreateBucket s3:DeleteBucket s3:GetBucketAcl
s3:PutBucketPolicy s3:GetBucketPolicy s3:DeleteBucketPolicy
s3:GetEncryptionConfiguration s3:PutEncryptionConfiguration
s3:GetBucketVersioning s3:PutBucketVersioning
s3:GetBucketTagging s3:PutBucketTagging
s3:GetBucketPublicAccessBlock s3:PutBucketPublicAccessBlock
```

### 4. Evaluate results

```
Per-action results:
  ALLOWED  s3:CreateBucket
  ALLOWED  s3:DeleteBucket
  DENIED   iam:CreateRole  (implicitDeny)

SIMULATION_RESULT: FAILED — 1 action(s) denied. Do NOT open the PR.
Fix: add the denied actions to the matching Sid in cloudformation/iam-gha-roles.yml, then re-run.
```

- **`SIMULATION_RESULT: PASSED`** — all actions allowed; safe to open PR.
- **`SIMULATION_RESULT: FAILED`** — one or more denied; do NOT open the PR.
  - Find the Sid in `cloudformation/iam-gha-roles.yml` that covers the service.
  - Add the missing actions to that Sid.
  - Re-run until PASSED.
  - Bundle the IAM fix with the template change in a single PR.

### 5. OIDC trust policy verification

When your PR touches an IAM role with an OIDC trust policy, pass `--oidc-sub` to
print the verification advisory:

```bash
~/.claude/skills/cfn-iam-simulate/simulate.sh \
  "arn:aws:iam::901347789205:role/gha-infra-cfn-deploy" \
  "sts:AssumeRoleWithWebIdentity" \
  --oidc-sub "repo:RdHamilton/hollowmark:ref:refs/heads/main"
```

Output includes:
```
OIDC_SUB_CHECK: Verify the trust policy sub condition matches: repo:RdHamilton/hollowmark:ref:refs/heads/main
  Run: aws iam get-role --profile personal --role-name <name> | python3 -c "..."
```

This surfaces stale `sub` pins — a common source of OIDC auth failures (root cause
of the 2026-06-07 P1 incident). A stale pin means the role was configured for a
different repo or branch; update the trust policy before opening the PR.

### 6. Include simulation evidence in the PR

Paste the full script output (from step 3) into the PR's `## Local Verification`
section under the heading `IAM simulation result`:

```
## Local Verification

IAM simulation result:
  ALLOWED  s3:CreateBucket
  ALLOWED  s3:DeleteBucket
  ...
SIMULATION_RESULT: PASSED — all actions allowed. Safe to open PR.
```

## Hard rules

- **Never open a CFN or IAM PR without a passing simulation transcript in Local Verification.**
- **Never open a template PR and an IAM fix PR separately** — if you discover a missing
  permission during simulation, bundle the fix with the template change in a single PR.
- If you cannot determine the correct action set for a new resource type, look it up in
  the AWS CloudFormation resource provider schema before assuming it is covered.
  Schema location: `https://raw.githubusercontent.com/aws-cloudformation/cloudformation-resource-providers-<service>/main/aws-<service>-<resource>/aws-<service>-<resource>.json`
  The `handlers.read.permissions` and `handlers.update.permissions` arrays are what CFN
  invokes at runtime beyond the obvious create/delete.

## AWS CLI profile

The script uses `--profile personal` by default (VaultMTG AWS CLI convention per
`hollowmark-docs/engineering/runbooks/aws-best-practices.md`). Override by setting
`AWS_PROFILE` in your environment:

```bash
AWS_PROFILE=my-profile ~/.claude/skills/cfn-iam-simulate/simulate.sh ...
```
