#!/usr/bin/env bash
# check-mtgzone-archetypes.sh
#
# On-instance pre-check executed via SSM RunShellScript by
# wildcard-panel-visual-capture.yml.
#
# Token placeholders (__FOO__) are substituted by the workflow before
# base64-encoding and shipping to the EC2 instance via SSM.
# NEVER run this script directly -- it requires the placeholders to be
# substituted first.
#
# Purpose:
#   Assert that staging mtgzone_archetypes is populated with
#   Lambda-produced data before the wildcard-panel visual capture runs.
#   An empty table means the meta-scrape Lambda has not run for this
#   environment and the screenshots would be meaningless.
#
# Pass condition:
#   COUNT(*) WHERE format_name = 'Standard' >= 1
#
# Fail condition (exits 1):
#   COUNT = 0 -- no Lambda-produced archetypes exist in staging.
#   The workflow step fails with a clear diagnostic message.
#
# Security model (mirrors run-staging-sql-oninstance.sh):
#   - Assumes vaultmtg-staging-deploy-provisioner for Secrets Manager access
#   - Fetches staging master credentials on-instance (never in runner env)
#   - PGPASSWORD is set via env var and never echoed or logged
#   - DB endpoint is the staging-scoped SSM value (hardcoded by workflow)
#
# Requires on the EC2 instance: aws CLI, psql, python3
set -euo pipefail

PROVISIONER_ROLE_ARN="__PROVISIONER_ROLE_ARN__"
DB_ENDPOINT="__DB_ENDPOINT__"
DB_SECRET_ARN="__DB_SECRET_ARN__"
EXPECTED_DB="vaultmtg_staging"
RUN_ID="__RUN_ID__"

echo "[check-mtgzone-archetypes] Run ID    : ${RUN_ID}"
echo "[check-mtgzone-archetypes] DB endpoint: ${DB_ENDPOINT}"

# -----------------------------------------------------------------------
# Step 1: Assume the scoped provisioner role so Secrets Manager reads are
# scoped to staging.  Clean up temporary credentials on exit.
# -----------------------------------------------------------------------
cleanup_creds() {
    unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN
}
trap cleanup_creds EXIT

SESSION_NAME="mtgzone-check-$(date +%s)"

if [[ -z "${AWS_PROFILE:-}" ]]; then
    echo "[check-mtgzone-archetypes] Assuming provisioner role..."
    ASSUME_OUTPUT=$(aws sts assume-role \
        --role-arn          "$PROVISIONER_ROLE_ARN" \
        --role-session-name "$SESSION_NAME" \
        --duration-seconds  900 \
        --region            us-east-1 \
        --query             'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' \
        --output            text)

    if [[ -z "$ASSUME_OUTPUT" ]]; then
        echo "[check-mtgzone-archetypes] ERROR: assume-role returned empty credentials." >&2
        exit 1
    fi

    AWS_ACCESS_KEY_ID=$(echo     "$ASSUME_OUTPUT" | awk '{print $1}')
    AWS_SECRET_ACCESS_KEY=$(echo "$ASSUME_OUTPUT" | awk '{print $2}')
    AWS_SESSION_TOKEN=$(echo     "$ASSUME_OUTPUT" | awk '{print $3}')
    export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_SESSION_TOKEN

    CALLER_ARN=$(aws sts get-caller-identity --query Arn --output text)
    case "$CALLER_ARN" in
        *":assumed-role/vaultmtg-staging-deploy-provisioner/${SESSION_NAME}")
            echo "[check-mtgzone-archetypes] Provisioner role confirmed: ${CALLER_ARN}"
            ;;
        *)
            echo "[check-mtgzone-archetypes] ERROR: caller identity ${CALLER_ARN} is not the provisioner role." >&2
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
echo "[check-mtgzone-archetypes] DB user: ${DB_USER}"

# -----------------------------------------------------------------------
# Step 3: Query mtgzone_archetypes for a non-zero count of Standard rows.
# -----------------------------------------------------------------------
echo "[check-mtgzone-archetypes] Querying mtgzone_archetypes for Standard archetypes..."

COUNT=$(PGPASSWORD="$PGPASSWORD" psql \
    -h "$DB_ENDPOINT" \
    -U "$DB_USER" \
    -d "$EXPECTED_DB" \
    --no-password \
    -v ON_ERROR_STOP=1 \
    -t -A \
    -c "SELECT COUNT(*) FROM mtgzone_archetypes WHERE format_name = 'Standard';")

echo "[check-mtgzone-archetypes] mtgzone_archetypes Standard count: ${COUNT}"

if [[ -z "$COUNT" || "$COUNT" -eq 0 ]]; then
    echo "" >&2
    echo "====================================================================" >&2
    echo "PRECHECK FAILED: No Lambda-produced archetypes found in staging." >&2
    echo "" >&2
    echo "  SELECT COUNT(*) FROM mtgzone_archetypes WHERE format_name = 'Standard'" >&2
    echo "  returned: ${COUNT:-<empty>}" >&2
    echo "" >&2
    echo "  The meta-scrape Lambda has not run for this staging environment." >&2
    echo "  Run the staging meta-scrape Lambda first (#1098), then re-trigger" >&2
    echo "  this workflow once it has completed successfully." >&2
    echo "====================================================================" >&2
    exit 1
fi

echo "[check-mtgzone-archetypes] PASS: staging mtgzone_archetypes has ${COUNT} Standard archetypes (Lambda-produced)."
