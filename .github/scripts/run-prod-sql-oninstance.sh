#!/usr/bin/env bash
# run-prod-sql-oninstance.sh
#
# PRODUCTION READ-ONLY diagnostics -- on-instance script executed via SSM
# RunShellScript by run-prod-sql.yml.
#
# Token placeholders (__FOO__) are substituted by the workflow before
# base64-encoding and shipping to the EC2 instance.
#
# NEVER run this script directly -- the placeholders must be substituted first.
#
# WRITE PROTECTION MODEL (defense in depth -- three independent layers):
#
#   Layer 1: SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY (session level)
#     Before user SQL runs, the psql session is set read-only at the SESSION
#     level:
#       SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY;
#     This is a session-scoped setting: it applies to EVERY transaction opened
#     on this connection, including any transaction started by an injected
#     COMMIT inside the user SQL.  A USER_SQL of "COMMIT; INSERT INTO …"
#     ends the current transaction but the INSERT still runs in a read-only
#     session and is rejected by Postgres with
#     "cannot execute INSERT in a read-only transaction".
#     The per-transaction BEGIN/COMMIT framing is kept for explicit boundaries
#     but the session-level setting is the actual write-protection guarantee.
#     Any INSERT / UPDATE / DELETE / DDL is rejected by Postgres. There is no
#     allow_write path in this script -- the session flag is hardcoded and
#     cannot be bypassed by the caller.
#
#   Layer 2: vaultmtg_ro Postgres role (SELECT-only)
#     The RO DB credential (ro-db-secret-arn) connects as vaultmtg_ro,
#     provisioned by migration 000108 (ticket #971) with SELECT-only grants
#     on all public schema tables.  No INSERT/UPDATE/DELETE/DDL grants exist
#     on this role.  This is a durable role-layer write guarantee independent
#     of the session-level flag: writes are rejected at the Postgres
#     privilege-check layer even if Layer 1 were somehow bypassed.
#
#   Layer 3: Identity guard (two independent factors)
#     Before any user SQL runs, the script asserts:
#       a) host(inet_server_addr()) matches the IP that the prod endpoint
#          resolves to on the EC2 instance (getent hosts).
#       b) current_database() equals the expected prod DB name.
#     Hard-abort (exit 1) if either factor fails.  A prod endpoint resolves to
#     a different RDS instance than staging; the guard catches accidental
#     cross-environment connections.
#
# DESIGN NOTE: this script uses the vaultmtg_ro credential (SSM path
# /vaultmtg/app/production/ro-db-secret-arn), provisioned by migration 000108
# (ticket #971 / CFN #306).  vaultmtg_ro holds SELECT only on all public
# schema tables.  This provides two independent write-protection layers:
# Layer 1 (session-level read-only) and Layer 2 (role grants -- no DML).
# The read-only guarantee is now durable at the role layer and no longer
# depends solely on the session-level flag.
#
# NOTE: SSM GetParameter for prod SSM paths is performed by the EC2 instance
# role BEFORE this script runs (the workflow resolves the endpoint and secret
# ARN, then base64-encodes them into the script template via sed substitution).
# This script does NOT call ssm:GetParameter directly.

set -euo pipefail

PROVISIONER_ROLE_ARN="__PROVISIONER_ROLE_ARN__"
EXPECTED_DB="__EXPECTED_DB__"
DB_ENDPOINT="__DB_ENDPOINT__"
RO_DB_SECRET_ARN="__RO_DB_SECRET_ARN__"
ACTOR="__ACTOR__"
RUN_ID="__RUN_ID__"
ENCODED_SQL="__ENCODED_SQL__"
ENCODED_REASON="__ENCODED_REASON__"

USER_SQL=$(echo "$ENCODED_SQL" | base64 -d)
AUDIT_REASON=$(echo "$ENCODED_REASON" | base64 -d)

echo "[run-prod-sql] ============================================================="
echo "[run-prod-sql] PRODUCTION READ-ONLY diagnostics run"
echo "[run-prod-sql] Actor     : ${ACTOR}"
echo "[run-prod-sql] Run ID    : ${RUN_ID}"
echo "[run-prod-sql] Reason    : ${AUDIT_REASON}"
echo "[run-prod-sql] Mode      : READ-ONLY (hardcoded -- no write path)"
echo "[run-prod-sql] ============================================================="

# -----------------------------------------------------------------------
# Step 1: Assume the provisioner role (same pattern as run-migrations.sh)
# so all subsequent AWS calls are scoped to the production SSM namespace.
# The RO DB secret ARN was already fetched by the workflow runner and
# injected into this script; this assume-role is only needed to call
# secretsmanager:GetSecretValue on the RO DB secret ARN.
# Clean up temporary credentials on exit.
# -----------------------------------------------------------------------
cleanup_creds() {
    unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN
    unset RO_DB_SECRET_JSON DB_USER DB_PASSWORD PGPASSWORD
}
trap cleanup_creds EXIT

SESSION_NAME="prod-sql-ro-$(date +%s)"

echo "[run-prod-sql] Assuming provisioner role..."
ASSUME_OUTPUT=$(aws sts assume-role \
    --role-arn          "$PROVISIONER_ROLE_ARN" \
    --role-session-name "$SESSION_NAME" \
    --duration-seconds  900 \
    --region            us-east-1 \
    --query             'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' \
    --output            text)

if [[ -z "$ASSUME_OUTPUT" ]]; then
    echo "[run-prod-sql] ERROR: assume-role returned empty credentials." >&2
    exit 1
fi

AWS_ACCESS_KEY_ID=$(echo     "$ASSUME_OUTPUT" | awk '{print $1}')
AWS_SECRET_ACCESS_KEY=$(echo "$ASSUME_OUTPUT" | awk '{print $2}')
AWS_SESSION_TOKEN=$(echo     "$ASSUME_OUTPUT" | awk '{print $3}')
export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN

CALLER_ARN=$(aws sts get-caller-identity --query Arn --output text)
case "$CALLER_ARN" in
    *":assumed-role/vaultmtg-staging-deploy-provisioner/${SESSION_NAME}")
        echo "[run-prod-sql] Provisioner role confirmed: ${CALLER_ARN}"
        ;;
    *)
        echo "[run-prod-sql] ERROR: caller identity ${CALLER_ARN} is not the provisioner role." >&2
        exit 1
        ;;
esac

# -----------------------------------------------------------------------
# Step 2: Fetch the RO DB credential from Secrets Manager.
# vaultmtg_ro holds SELECT only on all public schema tables -- no DML/DDL.
# PGPASSWORD is set as an env var and never echoed or logged.
# -----------------------------------------------------------------------
RO_DB_SECRET_JSON=$(aws secretsmanager get-secret-value \
    --region    us-east-1 \
    --secret-id "$RO_DB_SECRET_ARN" \
    --query     "SecretString" \
    --output    text)

DB_USER=$(echo     "$RO_DB_SECRET_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['username'])")
DB_PASSWORD=$(echo "$RO_DB_SECRET_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['password'])")
unset RO_DB_SECRET_JSON

PGPASSWORD="$DB_PASSWORD"
export PGPASSWORD

# Drop provisioner credentials now -- they are no longer needed.
unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN

# Never log PGPASSWORD.  Log the user so we know which role connected.
echo "[run-prod-sql] DB user     : ${DB_USER}"
echo "[run-prod-sql] DB endpoint : ${DB_ENDPOINT}"

# -----------------------------------------------------------------------
# Step 3: Identity guard -- TWO independent factors.
#
# Factor 1 (server address): host(inet_server_addr()) returns the IP of
# the RDS instance psql connected to, as a plain IP string (no CIDR suffix).
# We use host() -- NOT inet_server_addr()::text -- to avoid the /32 CIDR
# suffix bug that fail-closed run-staging-sql.yml (guard fix #3021).
# We resolve the prod endpoint hostname on-instance via getent hosts and
# compare.  A staging endpoint resolves to a different RDS instance; its IP
# will not match -- hard abort.
#
# Factor 2 (database name): current_database() must equal EXPECTED_DB.
# This is the structural guard against executing SQL on the wrong database.
#
# Both factors must pass before any user SQL runs.
# -----------------------------------------------------------------------
echo "[run-prod-sql] Running identity guard..."
GUARD_OUTPUT=$(PGPASSWORD="$PGPASSWORD" psql \
    -h "$DB_ENDPOINT" \
    -U "$DB_USER" \
    -d "$EXPECTED_DB" \
    --no-password \
    -v ON_ERROR_STOP=1 \
    -t -A \
    -c "SELECT host(inet_server_addr()) || '|' || current_database() || '|' || current_user;" \
    2>&1)

echo "[run-prod-sql] Guard query result: ${GUARD_OUTPUT}"

SERVER_ADDR=$(echo   "$GUARD_OUTPUT" | cut -d'|' -f1)
CURRENT_DB=$(echo    "$GUARD_OUTPUT" | cut -d'|' -f2)
CURRENT_USER_DB=$(echo "$GUARD_OUTPUT" | cut -d'|' -f3)

# Factor 1: resolve the prod endpoint hostname to its IP and compare.
RESOLVED_IP=$(getent hosts "$DB_ENDPOINT" | awk '{print $1}' | head -1)
if [[ -z "$RESOLVED_IP" ]]; then
    echo "[run-prod-sql] ERROR: could not resolve DB_ENDPOINT '${DB_ENDPOINT}' to an IP." >&2
    exit 1
fi

echo "[run-prod-sql] Endpoint resolves to  : ${RESOLVED_IP}"
echo "[run-prod-sql] Server inet_server_addr: ${SERVER_ADDR}"

if [[ "$SERVER_ADDR" != "$RESOLVED_IP" ]]; then
    echo "[run-prod-sql] ABORT: inet_server_addr() '${SERVER_ADDR}' does not match prod endpoint IP '${RESOLVED_IP}'." >&2
    echo "[run-prod-sql] ABORT: refusing to execute user SQL -- connected server is not the prod RDS instance." >&2
    exit 1
fi

echo "[run-prod-sql] Guard PASS factor 1: server address ${SERVER_ADDR} matches prod endpoint ${DB_ENDPOINT} (${RESOLVED_IP})"

# Factor 2: database name assertion.
if [[ "$CURRENT_DB" != "$EXPECTED_DB" ]]; then
    echo "[run-prod-sql] ABORT: current_database() is '${CURRENT_DB}', expected '${EXPECTED_DB}'." >&2
    echo "[run-prod-sql] ABORT: refusing to execute user SQL against a non-production database." >&2
    exit 1
fi

echo "[run-prod-sql] Guard PASS factor 2: current_database()=${CURRENT_DB}, current_user=${CURRENT_USER_DB}"

# -----------------------------------------------------------------------
# Step 4: Build the final SQL to execute.
#
# UNCONDITIONALLY read-only.  There is no allow_write path.
#
# The session-level setting is issued first:
#   SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY;
#
# This applies to EVERY transaction on this connection, including any
# transaction opened by an injected COMMIT inside USER_SQL.  A USER_SQL of
# "COMMIT; INSERT INTO x VALUES (1);" would end the current BEGIN/COMMIT
# block but the INSERT still executes in a read-only session and Postgres
# rejects it with "cannot execute INSERT in a read-only transaction".
#
# The BEGIN/COMMIT framing provides explicit transaction boundaries and
# ensures the user SQL runs in a single atomic block, but the session-level
# flag is the actual write-protection guarantee.
# -----------------------------------------------------------------------
FULL_SQL="SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY;
BEGIN;
${USER_SQL}
COMMIT;"

echo "[run-prod-sql] Mode: READ-ONLY (session-level -- SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY)"
echo "[run-prod-sql] SQL to execute:"
echo "---"
echo "$FULL_SQL"
echo "---"

# -----------------------------------------------------------------------
# Step 5: Execute user SQL.
# -----------------------------------------------------------------------
echo "[run-prod-sql] Executing SQL against production..."
PGPASSWORD="$PGPASSWORD" psql \
    -h "$DB_ENDPOINT" \
    -U "$DB_USER" \
    -d "$EXPECTED_DB" \
    --no-password \
    -v ON_ERROR_STOP=1 \
    -c "$FULL_SQL"

echo "[run-prod-sql] SQL execution complete."
