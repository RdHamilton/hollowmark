#!/usr/bin/env bash
# run-staging-sql-oninstance.sh
#
# On-instance script executed via SSM RunShellScript by run-staging-sql.yml.
# Token placeholders (__FOO__) are substituted by the workflow before
# base64-encoding and shipping to the EC2 instance.
#
# NEVER run this script directly -- it requires the placeholders to be
# substituted first.
set -euo pipefail

PROVISIONER_ROLE_ARN="__PROVISIONER_ROLE_ARN__"
EXPECTED_DB="__EXPECTED_DB__"
DB_ENDPOINT="__DB_ENDPOINT__"
DB_SECRET_ARN="__DB_SECRET_ARN__"
ALLOW_WRITE="__ALLOW_WRITE__"
ACTOR="__ACTOR__"
RUN_ID="__RUN_ID__"
ENCODED_SQL="__ENCODED_SQL__"
ENCODED_REASON="__ENCODED_REASON__"

USER_SQL=$(echo "$ENCODED_SQL" | base64 -d)
AUDIT_REASON=$(echo "$ENCODED_REASON" | base64 -d)

echo "[run-staging-sql] Actor     : ${ACTOR}"
echo "[run-staging-sql] Run ID    : ${RUN_ID}"
echo "[run-staging-sql] Reason    : ${AUDIT_REASON}"
echo "[run-staging-sql] allow_write: ${ALLOW_WRITE}"

# -----------------------------------------------------------------------
# Step 1: Assume the scoped provisioner role (same pattern as
# run-staging-migrations.sh) so all subsequent AWS calls are scoped to
# the staging SSM namespace.  Clean up temporary credentials on exit.
# -----------------------------------------------------------------------
cleanup_creds() {
    unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN
}
trap cleanup_creds EXIT

SESSION_NAME="staging-sql-$(date +%s)"

if [[ -z "${AWS_PROFILE:-}" ]]; then
    echo "[run-staging-sql] Assuming provisioner role..."
    ASSUME_OUTPUT=$(aws sts assume-role \
        --role-arn          "$PROVISIONER_ROLE_ARN" \
        --role-session-name "$SESSION_NAME" \
        --duration-seconds  900 \
        --region            us-east-1 \
        --query             'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' \
        --output            text)

    if [[ -z "$ASSUME_OUTPUT" ]]; then
        echo "[run-staging-sql] ERROR: assume-role returned empty credentials." >&2
        exit 1
    fi

    AWS_ACCESS_KEY_ID=$(echo     "$ASSUME_OUTPUT" | awk '{print $1}')
    AWS_SECRET_ACCESS_KEY=$(echo "$ASSUME_OUTPUT" | awk '{print $2}')
    AWS_SESSION_TOKEN=$(echo     "$ASSUME_OUTPUT" | awk '{print $3}')
    export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN

    CALLER_ARN=$(aws sts get-caller-identity --query Arn --output text)
    case "$CALLER_ARN" in
        *":assumed-role/vaultmtg-staging-deploy-provisioner/${SESSION_NAME}")
            echo "[run-staging-sql] Provisioner role confirmed: ${CALLER_ARN}"
            ;;
        *)
            echo "[run-staging-sql] ERROR: caller identity ${CALLER_ARN} is not the provisioner role." >&2
            exit 1
            ;;
    esac
fi

# -----------------------------------------------------------------------
# Step 2: Fetch staging master credentials from Secrets Manager.
# PGPASSWORD is set as an env var and never echoed or logged.
# -----------------------------------------------------------------------
SECRET_JSON=$(aws secretsmanager get-secret-value \
    --region    us-east-1 \
    --secret-id "$DB_SECRET_ARN" \
    --query     "SecretString" \
    --output    text)

PGPASSWORD=$(echo "$SECRET_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['password'])")
DB_USER=$(echo    "$SECRET_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['username'])")
export PGPASSWORD

# Never log PGPASSWORD.  Log the user so we know which role connected.
echo "[run-staging-sql] DB user: ${DB_USER}"
echo "[run-staging-sql] DB endpoint: ${DB_ENDPOINT}"

# -----------------------------------------------------------------------
# Step 3: Identity guard -- TWO independent factors.
#
# Factor 1 (server address): inet_server_addr() returns the IP of the RDS
# instance psql actually connected to.  We resolve the staging endpoint
# hostname to its IP on-instance (getent hosts) and compare.  A prod
# endpoint resolves to a different RDS instance IP -- mismatch = hard abort.
# This is a corroborating check, not the primary isolation barrier.
#
# Factor 2 (database name): current_database() must equal EXPECTED_DB
# (vaultmtg_staging).  This is the structural guard against executing SQL
# on the wrong database.
#
# NOTE: IAM does NOT provide prod isolation here (the provisioner role holds
# Secrets Manager read access that extends to the prod RDS credential path).
# Prod isolation rests on: (a) the staging-scoped SSM endpoint
# (/vaultmtg/app/staging/db-endpoint -- read-only, not user-supplied) which
# cannot be redirected to the prod RDS, and (b) the current_database()
# assertion (Factor 2).  inet_server_addr() (Factor 1) is a corroborating
# server-side address check -- it detects connection to an unexpected host but
# it is not the load-bearing isolation factor.
#
# Both factors must pass before any user SQL runs.
# -----------------------------------------------------------------------
echo "[run-staging-sql] Running identity guard..."
GUARD_OUTPUT=$(PGPASSWORD="$PGPASSWORD" psql \
    -h "$DB_ENDPOINT" \
    -U "$DB_USER" \
    -d "$EXPECTED_DB" \
    --no-password \
    -v ON_ERROR_STOP=1 \
    -t -A \
    -c "SELECT inet_server_addr()::text || '|' || current_database() || '|' || current_user;" \
    2>&1)

echo "[run-staging-sql] Guard query result: ${GUARD_OUTPUT}"

SERVER_ADDR=$(echo "$GUARD_OUTPUT"    | cut -d'|' -f1)
CURRENT_DB=$(echo  "$GUARD_OUTPUT"    | cut -d'|' -f2)
CURRENT_USER_DB=$(echo "$GUARD_OUTPUT" | cut -d'|' -f3)

# Factor 1: resolve the staging endpoint hostname to its IP and compare
# against the server-reported inet_server_addr().
RESOLVED_IP=$(getent hosts "$DB_ENDPOINT" | awk '{print $1}' | head -1)
if [[ -z "$RESOLVED_IP" ]]; then
    echo "[run-staging-sql] ERROR: could not resolve DB_ENDPOINT '${DB_ENDPOINT}' to an IP address." >&2
    exit 1
fi

echo "[run-staging-sql] Endpoint resolves to: ${RESOLVED_IP}"
echo "[run-staging-sql] Server inet_server_addr(): ${SERVER_ADDR}"

if [[ "$SERVER_ADDR" != "$RESOLVED_IP" ]]; then
    echo "[run-staging-sql] ABORT: inet_server_addr() '${SERVER_ADDR}' does not match resolved staging endpoint IP '${RESOLVED_IP}'." >&2
    echo "[run-staging-sql] ABORT: refusing to execute user SQL -- connected server is not the staging RDS instance." >&2
    exit 1
fi

echo "[run-staging-sql] Guard PASS factor 1: server address ${SERVER_ADDR} matches staging endpoint ${DB_ENDPOINT} (${RESOLVED_IP})"

# Factor 2: database name assertion.
if [[ "$CURRENT_DB" != "$EXPECTED_DB" ]]; then
    echo "[run-staging-sql] ABORT: current_database() is '${CURRENT_DB}', expected '${EXPECTED_DB}'." >&2
    echo "[run-staging-sql] ABORT: refusing to execute user SQL against a non-staging database." >&2
    exit 1
fi

echo "[run-staging-sql] Guard PASS factor 2: current_database()=${CURRENT_DB}, current_user=${CURRENT_USER_DB}"

# -----------------------------------------------------------------------
# Step 4: Build the final SQL to execute.
#
# Read-only (allow_write=false):
#   BEGIN;
#   SET TRANSACTION READ ONLY;
#   <user SQL>
#   COMMIT;
#
# Write-permitted (allow_write=true):
#   BEGIN;
#   <user SQL>
#   COMMIT;
#
# Both modes wrap in a transaction so any accidental multi-statement partial
# execution is rolled back by psql's ON_ERROR_STOP=1.
# -----------------------------------------------------------------------
if [[ "$ALLOW_WRITE" == "true" ]]; then
    FULL_SQL="BEGIN;
${USER_SQL}
COMMIT;"
    echo "[run-staging-sql] Mode: READ-WRITE (allow_write=true)"
else
    FULL_SQL="BEGIN;
SET TRANSACTION READ ONLY;
${USER_SQL}
COMMIT;"
    echo "[run-staging-sql] Mode: READ-ONLY (allow_write=false)"
fi

echo "[run-staging-sql] SQL to execute:"
echo "---"
echo "$FULL_SQL"
echo "---"

# -----------------------------------------------------------------------
# Step 5: Execute user SQL.
# -----------------------------------------------------------------------
echo "[run-staging-sql] Executing SQL..."
PGPASSWORD="$PGPASSWORD" psql \
    -h "$DB_ENDPOINT" \
    -U "$DB_USER" \
    -d "$EXPECTED_DB" \
    --no-password \
    -v ON_ERROR_STOP=1 \
    -c "$FULL_SQL"

echo "[run-staging-sql] SQL execution complete."
