# Sync Lambda — DLQ and CloudWatch Alarm Runbook

**Last updated**: 2026-05-15
**Ticket**: [#1343](https://github.com/RdHamilton/MTGA-Companion/issues/1343)

---

## Overview

The sync Lambda (`mtga-sync`) runs daily at 02:00 UTC via EventBridge Scheduler. Two
CloudWatch alarms watch for silent failures:

| Alarm | Fires when |
|---|---|
| `mtga-companion-production-sync-lambda-errors` | Lambda invocation error count ≥ 1 in any 5-minute window |
| `mtga-companion-production-sync-dlq-depth` | Any message lands in the SQS DLQ |

Both alarms notify the SNS topic `mtga-companion-alarms-production`, which delivers email
to `ray.hamilton@stablekernel.com`.

---

## Infrastructure

All resources are in the `sync-lambda.yml` CloudFormation stack
(`mtga-companion-sync-lambda`):

| Resource | Name |
|---|---|
| SQS DLQ | `mtga-companion-production-sync-dlq` |
| Lambda error alarm | `mtga-companion-production-sync-lambda-errors` |
| DLQ depth alarm | `mtga-companion-production-sync-dlq-depth` |
| SNS topic | inherited from `mtga-companion-cloudwatch-alarms` stack |

---

## In-Handler Resilience

Before reaching the DLQ, the Lambda applies two layers of resilience:

### Per-set/format retry (transient upstream errors)

Each `FetchCardRatings`, `UpsertRatings`, and `FetchColorRatings` call is retried up to
`SYNC_MAX_RETRIES` times (default: 2) with exponential backoff (2s → 4s → 8s, capped
at 30s). This handles transient 17Lands or RDS connection failures without failing the
Lambda.

### Consecutive-skip guard (persistent zero-card responses)

When a set returns 0 cards for `SYNC_MAX_CONSECUTIVE_SKIP_DAYS` consecutive daily
invocations (default: 3), the Lambda returns an error for that set. This causes the
overall invocation to fail, which:

1. Triggers EventBridge Scheduler retries (2 attempts).
2. Routes the event to the DLQ after retries are exhausted.
3. Fires the `SyncLambdaErrorAlarm`.

A zero-card response typically means a 17Lands set-code mismatch (e.g. the expansion
code changed when a set renamed) or a prolonged upstream API outage.

---

## Responding to Alarms

### Lambda error alarm fires

1. Open CloudWatch Logs for the `mtga-sync` Lambda function:

   ```bash
   aws logs tail /aws/lambda/mtga-sync --since 2h --profile personal
   ```

2. Look for `[sync] syncSet <SET>:` lines — these contain the root error.

3. Look for `[sync] skip guard: set <SET> returned 0 cards for N consecutive invocation(s)`.

4. If the skip guard tripped: verify the 17Lands expansion code for the affected set
   matches `SYNC_ACTIVE_SETS` or the `is_draft_active = TRUE` rows in the `sets` table.
   The 17Lands expansion code sometimes differs from the MTGA set code (e.g. `MH3`
   vs `mh3`). Update the `sets` table or set `SYNC_ACTIVE_SETS` in the Lambda
   environment to override.

5. If the error is a network/RDS issue: check RDS status and Lambda VPC connectivity.
   The Lambda security group allows outbound to RDS on port 5432.

### DLQ depth alarm fires

1. Inspect the DLQ message to identify which schedule event failed:

   ```bash
   aws sqs receive-message \
     --queue-url https://sqs.us-east-1.amazonaws.com/901347789205/mtga-companion-production-sync-dlq \
     --profile personal
   ```

2. The message body contains the EventBridge Scheduler event payload.

3. Correlate with Lambda logs (step 1 above) to determine the root cause.

4. After resolving the root cause, trigger a one-off sync by enabling the
   `mtga-companion-production-sync-cards-manual` EventBridge schedule (it is
   `DISABLED` by default):

   ```bash
   aws scheduler update-schedule \
     --name mtga-companion-production-sync-cards-manual \
     --state ENABLED \
     --profile personal
   ```

   Disable it again after the manual sync completes.

5. Delete the DLQ messages after the root cause is resolved so the alarm returns to OK:

   ```bash
   # Use the ReceiptHandle from the receive-message output above.
   aws sqs delete-message \
     --queue-url https://sqs.us-east-1.amazonaws.com/901347789205/mtga-companion-production-sync-dlq \
     --receipt-handle <RECEIPT_HANDLE> \
     --profile personal
   ```

---

## Tuning Alarm Thresholds

CloudFormation parameters in `sync-lambda.yml` control alarm sensitivity:

| Parameter | Default | Effect |
|---|---|---|
| `LambdaErrorThreshold` | `1` | Fires on the first Lambda error |
| `LambdaErrorEvaluationPeriods` | `1` | Single 5-minute window |
| `DLQDepthThreshold` | `1` | Any DLQ message fires the alarm |
| `SYNC_MAX_RETRIES` (Lambda env) | `2` | Per-fetch/upsert retry attempts |
| `SYNC_MAX_CONSECUTIVE_SKIP_DAYS` (Lambda env) | `3` | Zero-card guard threshold |

To reduce noise on a set that reliably has sparse 17Lands coverage (e.g. older
masters sets), increase `SYNC_MAX_CONSECUTIVE_SKIP_DAYS` for that environment or
mark the set `is_draft_active = FALSE` in the database.

---

## Staleness Complement — `X-Cache-Degraded` Header

If a sync failure persists beyond 48 hours (default `DRAFT_RATINGS_STALENESS_THRESHOLD_HOURS`
on the BFF), the `GET /api/v1/draft-ratings/{setCode}/{format}` endpoint adds:

```
X-Cache-Degraded: true
X-Cache-Age-Hours: <N>
```

The frontend displays a staleness notice when it sees this header. This is a
best-effort complement to the CloudWatch alarm, not a replacement: by the time the
header fires, the DLQ alarm should already have notified.
