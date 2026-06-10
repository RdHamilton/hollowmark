# Runbook: GDPR Art.17 Erasure Cascade Recovery (AC7)

**ADR**: ADR-056  
**Ticket**: #891  
**Owner**: On-call engineer  
**Trigger**: `deletion_audit_log` row with `completed_at IS NULL` older than 48 hours, OR a BFF log line `[erasure] cascade failed job_id=...`

---

## Background

The GDPR Art.17 right-to-erasure cascade runs asynchronously in a BFF background
goroutine.  It writes `deletion_audit_log.completed_at` on success.  If the cascade
fails at any step, `completed_at` stays `NULL` — which is the signal for this runbook.

The GDPR 48-hour SLA from the user's request is tracked by `requested_at`.

---

## Detection

### CloudWatch alarm (automated)

An alarm fires when any `deletion_audit_log` row has `completed_at IS NULL` AND
`requested_at < NOW() - INTERVAL '48 hours'`.

### Manual query

```sql
SELECT job_id, clerk_user_id, user_id, account_id, requested_at
FROM   deletion_audit_log
WHERE  completed_at IS NULL
ORDER  BY requested_at ASC;
```

Run this via SSM Session Manager (`ssm-session-manager`) to connect to the prod EC2 and
then `psql $DATABASE_URL`.

---

## Diagnosis: which step failed?

Check the BFF service logs for the job_id:

```bash
# On the prod EC2 via SSM Session Manager
journalctl -u vaultmtg-bff.service --since "2026-06-10" | grep "job_id=<JOB_ID>"
```

The log line format is:
```
[erasure] cascade failed job_id=<UUID> clerk_user_id=<CLERK_ID>: erasure step<N> (<description>): <error>
```

Identify the failing step from the error message.

---

## Recovery: manual re-trigger by job_id

**IMPORTANT**: The cascade is designed to be idempotent at every step.  Re-triggering
from Step 0 is safe — Steps that already succeeded will be no-ops (e.g. PostHog delete
on an already-deleted person returns 404 → success; accounts DELETE on a missing row
returns no rows affected → safe).

### Step 1: Confirm the job_id and parameters

```sql
SELECT job_id, clerk_user_id, user_id, account_id, requested_at
FROM   deletion_audit_log
WHERE  job_id = '<JOB_ID>';
```

### Step 2: Re-trigger via the admin re-trigger endpoint

```bash
# Admin re-trigger endpoint (see DELETE /api/v1/admin/account-deletion/retry)
curl -X POST https://api.vaultmtg.app/api/v1/admin/account-deletion/retry \
  -H "Authorization: Bearer $BFF_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"job_id": "<JOB_ID>"}'
```

The endpoint reads the `deletion_audit_log` row by `job_id`, reconstructs the
parameters, and re-dispatches the goroutine from Step 0.

> **Note**: The admin re-trigger endpoint is implemented in a follow-up ticket.
> Until it is available, use the manual DB-level procedure below.

### Step 2 (interim — until admin endpoint ships): Manual DB cascade

If the cascade failed after Steps 1–4 (Clerk and Mailchimp not yet called), the
remaining steps can be performed manually:

1. **Verify what has been deleted so far** by checking whether the users/accounts rows exist:
   ```sql
   SELECT id FROM users WHERE id = <USER_ID>;
   SELECT id FROM accounts WHERE id = <ACCOUNT_ID>;
   ```

2. **Re-run the remaining cascade** starting from the first failed step:
   - For PostHog: use the PostHog admin UI to delete the person by distinct_id
     (`identityhash.HashAccountID(strconv.FormatInt(accountID, 10))` — SHA-256[:16] of the account_id string).
   - For Clerk: use the Clerk dashboard to delete the user by clerk_user_id.
   - For Mailchimp: use the Mailchimp API or dashboard to `delete-permanent` the member.

3. **Mark the job complete** once all steps are confirmed done:
   ```sql
   UPDATE deletion_audit_log SET completed_at = NOW() WHERE job_id = '<JOB_ID>';
   ```

---

## Escalation

If the cascade cannot be completed within the 48-hour SLA:
1. Page Ray (architect) for incident command.
2. Page Sarah (security) — an incomplete erasure within the SLA window is a potential
   GDPR Art.17 violation requiring DPA notification assessment.
3. Document the failure in `hollowmark-docs/engineering/incidents/`.

---

## Prevention

- CloudWatch alarm on `deletion_audit_log.completed_at IS NULL` + age > 24h for early warning.
- Sentry alert on BFF log `[erasure] cascade failed`.
- Regular SQL query in the weekly health check:
  ```sql
  SELECT COUNT(*) FROM deletion_audit_log WHERE completed_at IS NULL;
  ```
  Any non-zero count older than 1 hour warrants investigation.
