#!/bin/sh
# scripts/deploy/provision-prod-env.sh
#
# Renders the production env file from the SSM parameter hierarchy using the
# manifest-driven model.  ADR-075 D3: converge prod+staging EC2 env provisioning
# on one manifest (ssm-key-manifest.sh) and one library (provision-lib.sh).
#
# Runs ON the EC2 instance via SSM RunShellScript.
#
# Replaces the legacy per-key upsert chain (provision-env.sh +
# provision-db-url.sh) that was previously called directly from deploy-bff.yml.
# The legacy scripts remain in the repo and are marked DEPRECATED below.
#
# Credential model (mirrors provision-db-url.sh, per ADR-022 sect4A.7):
#   1. The EC2 instance role (mtga-companion-ec2-role-production) is the AWS
#      calling identity for the SSM RunShellScript session.
#   2. The three prod DB SSM params AND the Secrets Manager secret are ALL read
#      BEFORE assuming the provisioner role, because the provisioner role's SSM
#      grant does NOT cover /vaultmtg/app/production/* -- only the instance role
#      does.  The secret JSON is kept in DB_SECRET_JSON for Step 3.
#   3. This script then sts:AssumeRoles into
#      vaultmtg-staging-deploy-provisioner (the only role with
#      secretsmanager:GetSecretValue on the RDS-managed credential).
#      The instance role's StagingDeployProvisionerAssumeRole policy already
#      grants sts:AssumeRole on this ARN; the provisioner role's
#      EC2InstanceRoleBridge trust statement permits the assume.
#   4. Temporary credentials are exported as AWS_ACCESS_KEY_ID /
#      AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN, scoping all subsequent
#      aws CLI calls to the provisioner role.
#   5. An EXIT trap clears the credentials after the env file is written.
#
# DATABASE_URL: Step 1 pre-fetches all DB values (SSM params + SM secret) under
#   the instance role.  write_database_url() in provision-lib.sh assembles the
#   URL from those passed VALUES -- it performs ZERO SSM/SM reads itself.
#   This design means every prod SSM/SM read happens exactly once, in Step 1,
#   under the instance role.  Step 2's assume-role is unaffected.
#   Uses SSM_PROD_APP_DB_SECRET_ARN (vaultmtg_app DML-only credential) so the
#   BFF connects as the least-privilege role.
#   run-migrations.sh independently uses SSM_PROD_DB_SECRET_ARN (master credential).
#   DB_SECRET_ARN and BFF_DB_RESOLVE_FROM_SM are deliberately NOT written
#   (prevents the #2461 crash-loop regression -- contract test C5).
#
# DAEMON_JWT_SECRET: scope=both in the manifest but bootstrap-carried on prod
#   (written by ec2-bootstrap.sh; survives every deploy).  This script does
#   NOT provision it so as not to overwrite the bootstrap-written value.
#   See ssm-key-manifest.sh entry 5 for the full annotation.
#
# Env file written: /etc/vaultmtg/env  (BFF_ENV_FILE from deploy-env.sh)
#
# SSM parameter names and file paths are sourced from
# infra/config/deploy-env.sh -- do NOT hardcode them here.

set -e

# Source canonical deploy facts.  deploy-env.sh is downloaded alongside
# this script from S3 into /tmp/ before execution.
. /tmp/deploy-env.sh

REGION="$DEPLOY_REGION"
ENV_FILE="$BFF_ENV_FILE"
ENV_DIR="$BFF_ENV_DIR"

# ---------------------------------------------------------------------------
# Credential isolation: clear any ambient AWS credential env vars BEFORE the
# Step 1 SSM reads so the default AWS credential chain falls through to the
# EC2 instance role (mtga-companion-ec2-role-production).
#
# Context: this script is invoked by deploy-bff.yml via SSM RunShellScript.
# The GHA deploy job runs after assuming vaultmtg-staging-deploy-provisioner,
# whose session credentials are exported as AWS_ACCESS_KEY_ID /
# AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN into the shell environment.  If
# those vars are present when Step 1 runs, the aws CLI resolves to the
# provisioner identity, whose SSM policy covers only
# /vaultmtg/{staging,app/staging}/* -- not /vaultmtg/app/production/*.
# The result is AccessDeniedException on the Step 1 GetParameter calls.
#
# Fix: unset all ambient credential env vars here so Step 1 uses the EC2
# instance role (the only identity with /vaultmtg/app/production/* read
# access).  Step 2 then explicitly assumes the provisioner role and exports
# its credentials for the remainder of the script -- the correct, already-
# designed flow.
#
# Per Ray's diagnosis (docs/status/ray-incident-24sx76.md): do NOT grant the
# staging provisioner prod SSM read access -- that violates least-privilege.
# The correct fix is to clear the inherited credentials here.
# ---------------------------------------------------------------------------
unset AWS_ACCESS_KEY_ID
unset AWS_SECRET_ACCESS_KEY
unset AWS_SESSION_TOKEN
unset AWS_PROFILE
unset AWS_CREDENTIAL_EXPIRATION

# ---------------------------------------------------------------------------
# Step 1: Read ALL production DB values under the EC2 instance role.
#
# These params live under /vaultmtg/app/production/*, which the instance role
# can read but the provisioner role cannot.  ALL reads (SSM params + the
# Secrets Manager secret) MUST happen here, BEFORE the assume-role below.
#
# Uses SSM_PROD_APP_DB_SECRET_ARN (vaultmtg_app application credential,
# not the master credential) so DATABASE_URL connects as the least-privilege
# DML-only role.  Mirrors provision-db-url.sh step 1 exactly.
#
# The resulting DB_SECRET_JSON, DB_ENDPOINT, and DB_NAME are passed by value
# to write_database_url() in Step 3; that function performs no further AWS
# reads, so the provisioner role's lack of prod SSM/SM access is irrelevant.
# ---------------------------------------------------------------------------
DB_SECRET_ARN_VALUE=$(aws ssm get-parameter \
  --name "$SSM_PROD_APP_DB_SECRET_ARN" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

DB_ENDPOINT=$(aws ssm get-parameter \
  --name "$SSM_PROD_DB_ENDPOINT" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

DB_NAME=$(aws ssm get-parameter \
  --name "$SSM_PROD_DB_NAME" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

if [ -z "$DB_SECRET_ARN_VALUE" ] || [ -z "$DB_ENDPOINT" ] || [ -z "$DB_NAME" ]; then
  echo "ERROR: one or more production DB SSM parameters returned empty." >&2
  echo "  DB_SECRET_ARN_VALUE (from ${SSM_PROD_APP_DB_SECRET_ARN}): '${DB_SECRET_ARN_VALUE}'" >&2
  echo "  DB_ENDPOINT (from ${SSM_PROD_DB_ENDPOINT}): '${DB_ENDPOINT}'" >&2
  echo "  DB_NAME (from ${SSM_PROD_DB_NAME}): '${DB_NAME}'" >&2
  exit 1
fi

# Fetch the RDS application-credential JSON from Secrets Manager under the
# instance role.  This value is passed to write_database_url() in Step 3;
# no further SM read is performed after the assume-role.
DB_SECRET_JSON=$(aws secretsmanager get-secret-value \
  --secret-id "$DB_SECRET_ARN_VALUE" \
  --region "$REGION" \
  --query SecretString \
  --output text)

if [ -z "$DB_SECRET_JSON" ]; then
  echo "ERROR: secretsmanager get-secret-value returned empty for ARN '${DB_SECRET_ARN_VALUE}'." >&2
  exit 1
fi

# ---------------------------------------------------------------------------
# Step 2: Assume the scoped provisioner role.
#
# Mirrors provision-staging-env.sh / provision-db-url.sh exactly.
# 900s == 15 min minimum; script completes in under 30s in practice.
# ---------------------------------------------------------------------------
PROVISIONER_ROLE_ARN="arn:aws:iam::901347789205:role/vaultmtg-staging-deploy-provisioner"
SESSION_NAME="prod-env-render-$(date +%s)"

cleanup_creds() {
  unset AWS_ACCESS_KEY_ID
  unset AWS_SECRET_ACCESS_KEY
  unset AWS_SESSION_TOKEN
  unset DB_SECRET_JSON
}
trap cleanup_creds EXIT

echo "Assuming role ${PROVISIONER_ROLE_ARN} as session ${SESSION_NAME}..."
ASSUME_OUTPUT=$(aws sts assume-role \
  --role-arn "$PROVISIONER_ROLE_ARN" \
  --role-session-name "$SESSION_NAME" \
  --duration-seconds 900 \
  --region "$REGION" \
  --query 'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' \
  --output text)

if [ -z "$ASSUME_OUTPUT" ]; then
  echo "ERROR: aws sts assume-role returned empty credentials." >&2
  exit 1
fi

AWS_ACCESS_KEY_ID=$(echo "$ASSUME_OUTPUT" | awk '{print $1}')
AWS_SECRET_ACCESS_KEY=$(echo "$ASSUME_OUTPUT" | awk '{print $2}')
AWS_SESSION_TOKEN=$(echo "$ASSUME_OUTPUT" | awk '{print $3}')

if [ -z "$AWS_ACCESS_KEY_ID" ] || [ -z "$AWS_SECRET_ACCESS_KEY" ] || [ -z "$AWS_SESSION_TOKEN" ]; then
  echo "ERROR: aws sts assume-role returned incomplete credentials." >&2
  exit 1
fi

export AWS_ACCESS_KEY_ID
export AWS_SECRET_ACCESS_KEY
export AWS_SESSION_TOKEN

CALLER_ARN=$(aws sts get-caller-identity --query Arn --output text)
case "$CALLER_ARN" in
  *":assumed-role/vaultmtg-staging-deploy-provisioner/${SESSION_NAME}")
    echo "Assumed role identity confirmed: ${CALLER_ARN}"
    ;;
  *)
    echo "ERROR: caller identity ${CALLER_ARN} is not the provisioner role -- refusing to continue." >&2
    exit 1
    ;;
esac

# ---------------------------------------------------------------------------
# Step 3: Render the production env file.
#
# Fully re-render on each deploy (truncate-then-write, not upsert).
# Ensures no stale keys survive from a previous provisioning run.
# ---------------------------------------------------------------------------
mkdir -p "$ENV_DIR"
: > "$ENV_FILE"
chmod 600 "$ENV_FILE"

# Source shared helpers (write_param, write_database_url).
# provision-lib.sh is downloaded alongside this script from S3 into /tmp/
# before execution.  Requires REGION and ENV_FILE to already be set.
. /tmp/provision-lib.sh

# Source the key manifest (pure declarative data -- no executable logic).
# ssm-key-manifest.sh is downloaded alongside this script from S3 into /tmp/.
# ADR-075 D3: single source of truth for the BFF env-var <-> SSM mapping.
. /tmp/ssm-key-manifest.sh

# AWS region -- required by the BFF's AWS clients at startup.
printf 'AWS_DEFAULT_REGION=%s\n' "$REGION" >> "$ENV_FILE"
echo "AWS_DEFAULT_REGION provisioned."

# DATABASE_URL: assemble from pre-fetched values (all read in Step 1 above,
# under the EC2 instance role).  write_database_url() performs ZERO SSM/SM
# reads -- it builds the URL entirely from the passed arguments.
# NOTE: DB_SECRET_ARN and BFF_DB_RESOLVE_FROM_SM are deliberately NOT written
# (prevents the #2461 crash-loop regression; contract test C5).
write_database_url "$DB_SECRET_JSON" "$DB_ENDPOINT" "$DB_NAME"
unset DB_SECRET_JSON

# Manifest-driven provisioning loop (ADR-075 D3).
#
# Iterates over every entry in ssm-key-manifest.sh and provisions keys
# scoped to "prod-only" or "both".  Keys scoped to "staging-only" are
# skipped.  DATABASE_URL is skipped (handled above by write_database_url).
# DAEMON_JWT_SECRET is skipped (bootstrap-carried on prod -- see header).
i=0
while [ "$i" -lt "$MANIFEST_KEY_COUNT" ]; do
  eval "KEY_NAME=\${MANIFEST_KEY_${i}_NAME}"
  eval "KEY_TYPE=\${MANIFEST_KEY_${i}_TYPE}"
  eval "KEY_SCOPE=\${MANIFEST_KEY_${i}_SCOPE}"
  eval "KEY_SSM_VAR=\${MANIFEST_KEY_${i}_SSM_VAR}"

  # Skip DATABASE_URL -- handled above by write_database_url().
  if [ "$KEY_NAME" = "DATABASE_URL" ]; then
    i=$((i + 1))
    continue
  fi

  # Skip DAEMON_JWT_SECRET -- bootstrap-carried on prod; must not overwrite.
  if [ "$KEY_NAME" = "DAEMON_JWT_SECRET" ]; then
    echo "DAEMON_JWT_SECRET skipped (bootstrap-carried on prod)."
    i=$((i + 1))
    continue
  fi

  # Skip staging-only entries.
  case "$KEY_SCOPE" in
    staging-only)
      i=$((i + 1))
      continue
      ;;
  esac

  # Resolve the prod SSM path from the manifest SSM_VAR.
  # All "both" and "prod-only" entries already point at prod paths in the manifest.
  eval "SSM_PATH=\${$KEY_SSM_VAR}"

  if [ -z "$SSM_PATH" ]; then
    echo "ERROR: manifest entry ${i} (${KEY_NAME}) has empty SSM path (SSM_VAR=${KEY_SSM_VAR})." >&2
    exit 1
  fi

  DECRYPT_FLAG=""
  if [ "$KEY_TYPE" = "secret" ]; then
    DECRYPT_FLAG="--with-decryption"
  fi

  write_param "$KEY_NAME" "$SSM_PATH" $DECRYPT_FLAG

  i=$((i + 1))
done

chmod 600 "$ENV_FILE"
echo "Production env provisioned at ${ENV_FILE}."
