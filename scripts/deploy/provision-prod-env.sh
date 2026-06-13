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
# Credential model (ADR-022 sect4A.7 + Option A fix, incident ray-incident-9ntxqv.md):
#   1. The EC2 instance role (mtga-companion-ec2-role-production) is the AWS
#      calling identity for the SSM RunShellScript session.
#   2. ALL /vaultmtg/app/production/* SSM parameters AND the Secrets Manager
#      secret are read BEFORE assuming the provisioner role.  The provisioner
#      role's SSM grant does NOT cover /vaultmtg/app/production/* -- only the
#      instance role does.  This applies to both DB params and ALL manifest
#      "both"/"prod-only" keys (ALLOWED_ORIGINS, CLERK_SECRET_KEY, etc.).
#      Option A fix (ray-incident-9ntxqv.md): extend the Step 1 pre-fetch to
#      cover the FULL manifest, not just DATABASE_URL.  The manifest loop
#      writes pre-fetched VALUES via write_param_value() -- zero SSM reads
#      after assume-role.
#   3. This script then sts:AssumeRoles into
#      vaultmtg-staging-deploy-provisioner (the only role with
#      secretsmanager:GetSecretValue on the RDS-managed credential).
#      The instance role's StagingDeployProvisionerAssumeRole policy already
#      grants sts:AssumeRole on this ARN; the provisioner role's
#      EC2InstanceRoleBridge trust statement permits the assume.
#   4. Temporary credentials are exported as AWS_ACCESS_KEY_ID /
#      AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN, scoping all subsequent
#      aws CLI calls to the provisioner role.
#   5. An EXIT trap clears the credentials and all sensitive pre-fetched
#      variables after the env file is written.
#
# DATABASE_URL: Step 1 pre-fetches all DB values (SSM params + SM secret) under
#   the instance role.  write_database_url() in provision-lib.sh assembles the
#   URL from those passed VALUES -- it performs ZERO SSM/SM reads itself.
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
# Step 1: Pre-fetch ALL production SSM values under the EC2 instance role.
#
# These params live under /vaultmtg/app/production/*, which the instance role
# can read but the provisioner role (assumed in Step 2) cannot.  EVERY prod
# SSM read MUST happen here, BEFORE assume-role.
#
# This covers:
#   a) DB params (app-db-secret-arn, db-endpoint, db-name) + SM secret --
#      used to build DATABASE_URL via write_database_url() in Step 3.
#   b) ALL "both" and "prod-only" manifest keys -- fetched into PREFETCH_*
#      variables and written via write_param_value() in Step 3.
#
# Option A fix (ray-incident-9ntxqv.md): #3271 cleared ambient creds (#3271)
# and #3272 moved DB reads to Step 1; this change extends the same pattern to
# the FULL manifest.  Before this fix the manifest loop called write_param()
# post-assume-role, which triggered aws ssm get-parameter under the provisioner
# identity -- access denied on every /vaultmtg/app/production/* path.
#
# After this change: zero SSM reads occur after sts:AssumeRole.
# ---------------------------------------------------------------------------

# --- DB params (used by write_database_url) ---
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
# instance role.  Passed to write_database_url() in Step 3; no further SM
# read is performed after the assume-role.
DB_SECRET_JSON=$(aws secretsmanager get-secret-value \
  --secret-id "$DB_SECRET_ARN_VALUE" \
  --region "$REGION" \
  --query SecretString \
  --output text)

if [ -z "$DB_SECRET_JSON" ]; then
  echo "ERROR: secretsmanager get-secret-value returned empty for ARN '${DB_SECRET_ARN_VALUE}'." >&2
  exit 1
fi

# --- Manifest "both" and "prod-only" keys (16 keys; all scope=both or prod-only) ---
# Pre-fetched here under the instance role.  Written via write_param_value()
# in Step 3 -- zero SSM reads after assume-role.
#
# Enumeration mirrors ssm-key-manifest.sh entries exactly; if a key is added
# to the manifest with prod scope, it MUST be added here too.  The C9
# integration test (Phase 1e) enforces exhaustiveness: any prod SSM read
# after assume-role fails CI.

# Entry 0: ALLOWED_ORIGINS (plain, both)
PREFETCH_ALLOWED_ORIGINS=$(aws ssm get-parameter \
  --name "$SSM_PROD_ALLOWED_ORIGINS" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 1: CLERK_SECRET_KEY (secret, both)
PREFETCH_CLERK_SECRET_KEY=$(aws ssm get-parameter \
  --name "$SSM_PROD_CLERK_SECRET_KEY" \
  --with-decryption \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 2: CLERK_FRONTEND_API (plain, both)
PREFETCH_CLERK_FRONTEND_API=$(aws ssm get-parameter \
  --name "$SSM_PROD_CLERK_FRONTEND_API" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 4: SENTRY_DSN (secret, both)
PREFETCH_SENTRY_DSN=$(aws ssm get-parameter \
  --name "$SSM_PROD_SENTRY_DSN_BFF" \
  --with-decryption \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 9: MAILCHIMP_API_KEY (secret, both)
PREFETCH_MAILCHIMP_API_KEY=$(aws ssm get-parameter \
  --name "$SSM_PROD_MAILCHIMP_API_KEY" \
  --with-decryption \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 10: MAILCHIMP_LIST_ID (plain, both)
PREFETCH_MAILCHIMP_LIST_ID=$(aws ssm get-parameter \
  --name "$SSM_PROD_MAILCHIMP_LIST_ID" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 12: POSTHOG_API_KEY (secret, prod-only)
PREFETCH_POSTHOG_API_KEY=$(aws ssm get-parameter \
  --name "$SSM_PROD_POSTHOG_API_KEY" \
  --with-decryption \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 13: POSTHOG_HOST (plain, prod-only)
PREFETCH_POSTHOG_HOST=$(aws ssm get-parameter \
  --name "$SSM_PROD_POSTHOG_HOST" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 14: BFF_DAEMON_LATEST_VERSION (plain, both)
PREFETCH_BFF_DAEMON_LATEST_VERSION=$(aws ssm get-parameter \
  --name "$SSM_PROD_BFF_DAEMON_LATEST_VERSION" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 15: BFF_DAEMON_RELEASED_AT (plain, both)
PREFETCH_BFF_DAEMON_RELEASED_AT=$(aws ssm get-parameter \
  --name "$SSM_PROD_BFF_DAEMON_RELEASED_AT" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 18: ANALYTICS_PII_SALT (secret, both)
PREFETCH_ANALYTICS_PII_SALT=$(aws ssm get-parameter \
  --name "$SSM_PROD_ANALYTICS_PII_SALT" \
  --with-decryption \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 19: INTERNAL_SVC_SECRET (secret, both)
PREFETCH_INTERNAL_SVC_SECRET=$(aws ssm get-parameter \
  --name "$SSM_PROD_INTERNAL_SVC_SECRET" \
  --with-decryption \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 20: POSTHOG_PERSONAL_API_KEY (secret, both)
PREFETCH_POSTHOG_PERSONAL_API_KEY=$(aws ssm get-parameter \
  --name "$SSM_PROD_POSTHOG_PERSONAL_API_KEY" \
  --with-decryption \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 21: POSTHOG_PROJECT_ID (plain, both)
PREFETCH_POSTHOG_PROJECT_ID=$(aws ssm get-parameter \
  --name "$SSM_PROD_POSTHOG_PROJECT_ID" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 22: BFF_TOS_VERSION (plain, both)
PREFETCH_BFF_TOS_VERSION=$(aws ssm get-parameter \
  --name "$SSM_PROD_BFF_TOS_VERSION" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Entry 23: BFF_PRIVACY_POLICY_VERSION (plain, both)
PREFETCH_BFF_PRIVACY_POLICY_VERSION=$(aws ssm get-parameter \
  --name "$SSM_PROD_BFF_PRIVACY_POLICY_VERSION" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)

# Validate all pre-fetched values are non-empty.  Fail loudly here (before
# assume-role) so the error message names the missing key and its SSM path.
check_prefetch() {
  # $1=key $2=value $3=ssm_path  (POSIX sh: no local)
  if [ -z "$2" ]; then
    echo "ERROR: Step 1 pre-fetch returned empty for ${1} (SSM path: ${3})." >&2
    exit 1
  fi
}

check_prefetch "ALLOWED_ORIGINS"             "$PREFETCH_ALLOWED_ORIGINS"             "$SSM_PROD_ALLOWED_ORIGINS"
check_prefetch "CLERK_SECRET_KEY"            "$PREFETCH_CLERK_SECRET_KEY"            "$SSM_PROD_CLERK_SECRET_KEY"
check_prefetch "CLERK_FRONTEND_API"          "$PREFETCH_CLERK_FRONTEND_API"          "$SSM_PROD_CLERK_FRONTEND_API"
check_prefetch "SENTRY_DSN"                  "$PREFETCH_SENTRY_DSN"                  "$SSM_PROD_SENTRY_DSN_BFF"
check_prefetch "MAILCHIMP_API_KEY"           "$PREFETCH_MAILCHIMP_API_KEY"           "$SSM_PROD_MAILCHIMP_API_KEY"
check_prefetch "MAILCHIMP_LIST_ID"           "$PREFETCH_MAILCHIMP_LIST_ID"           "$SSM_PROD_MAILCHIMP_LIST_ID"
check_prefetch "POSTHOG_API_KEY"             "$PREFETCH_POSTHOG_API_KEY"             "$SSM_PROD_POSTHOG_API_KEY"
check_prefetch "POSTHOG_HOST"                "$PREFETCH_POSTHOG_HOST"                "$SSM_PROD_POSTHOG_HOST"
check_prefetch "BFF_DAEMON_LATEST_VERSION"   "$PREFETCH_BFF_DAEMON_LATEST_VERSION"   "$SSM_PROD_BFF_DAEMON_LATEST_VERSION"
check_prefetch "BFF_DAEMON_RELEASED_AT"      "$PREFETCH_BFF_DAEMON_RELEASED_AT"      "$SSM_PROD_BFF_DAEMON_RELEASED_AT"
check_prefetch "ANALYTICS_PII_SALT"          "$PREFETCH_ANALYTICS_PII_SALT"          "$SSM_PROD_ANALYTICS_PII_SALT"
check_prefetch "INTERNAL_SVC_SECRET"         "$PREFETCH_INTERNAL_SVC_SECRET"         "$SSM_PROD_INTERNAL_SVC_SECRET"
check_prefetch "POSTHOG_PERSONAL_API_KEY"    "$PREFETCH_POSTHOG_PERSONAL_API_KEY"    "$SSM_PROD_POSTHOG_PERSONAL_API_KEY"
check_prefetch "POSTHOG_PROJECT_ID"          "$PREFETCH_POSTHOG_PROJECT_ID"          "$SSM_PROD_POSTHOG_PROJECT_ID"
check_prefetch "BFF_TOS_VERSION"             "$PREFETCH_BFF_TOS_VERSION"             "$SSM_PROD_BFF_TOS_VERSION"
check_prefetch "BFF_PRIVACY_POLICY_VERSION"  "$PREFETCH_BFF_PRIVACY_POLICY_VERSION"  "$SSM_PROD_BFF_PRIVACY_POLICY_VERSION"

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
  unset PREFETCH_CLERK_SECRET_KEY
  unset PREFETCH_SENTRY_DSN
  unset PREFETCH_MAILCHIMP_API_KEY
  unset PREFETCH_ANALYTICS_PII_SALT
  unset PREFETCH_INTERNAL_SVC_SECRET
  unset PREFETCH_POSTHOG_API_KEY
  unset PREFETCH_POSTHOG_PERSONAL_API_KEY
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
#
# All prod SSM values were pre-fetched in Step 1.  write_param_value() writes
# a pre-fetched value directly -- zero SSM reads in this step.
# ---------------------------------------------------------------------------
mkdir -p "$ENV_DIR"
: > "$ENV_FILE"
chmod 600 "$ENV_FILE"

# Source shared helpers (write_param, write_param_value, write_database_url).
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
#
# Option A: write_param_value() is used here, NOT write_param().
# All prod SSM values were pre-fetched in Step 1; the loop maps each key name
# to its PREFETCH_* variable and writes the pre-fetched value.  Zero SSM reads
# occur in this loop.  If any "both"/"prod-only" key is added to the manifest
# without a corresponding Step 1 pre-fetch and PREFETCH_* entry here, the
# C9 integration test (Phase 1e) will catch it.
i=0
while [ "$i" -lt "$MANIFEST_KEY_COUNT" ]; do
  eval "KEY_NAME=\${MANIFEST_KEY_${i}_NAME}"
  eval "KEY_SCOPE=\${MANIFEST_KEY_${i}_SCOPE}"

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

  # Map KEY_NAME to its pre-fetched value (all fetched in Step 1 above).
  # write_param_value() validates the value is non-empty and writes KEY=VALUE.
  case "$KEY_NAME" in
    ALLOWED_ORIGINS)            write_param_value "$KEY_NAME" "$PREFETCH_ALLOWED_ORIGINS" ;;
    CLERK_SECRET_KEY)           write_param_value "$KEY_NAME" "$PREFETCH_CLERK_SECRET_KEY" ;;
    CLERK_FRONTEND_API)         write_param_value "$KEY_NAME" "$PREFETCH_CLERK_FRONTEND_API" ;;
    SENTRY_DSN)                 write_param_value "$KEY_NAME" "$PREFETCH_SENTRY_DSN" ;;
    MAILCHIMP_API_KEY)          write_param_value "$KEY_NAME" "$PREFETCH_MAILCHIMP_API_KEY" ;;
    MAILCHIMP_LIST_ID)          write_param_value "$KEY_NAME" "$PREFETCH_MAILCHIMP_LIST_ID" ;;
    POSTHOG_API_KEY)            write_param_value "$KEY_NAME" "$PREFETCH_POSTHOG_API_KEY" ;;
    POSTHOG_HOST)               write_param_value "$KEY_NAME" "$PREFETCH_POSTHOG_HOST" ;;
    BFF_DAEMON_LATEST_VERSION)  write_param_value "$KEY_NAME" "$PREFETCH_BFF_DAEMON_LATEST_VERSION" ;;
    BFF_DAEMON_RELEASED_AT)     write_param_value "$KEY_NAME" "$PREFETCH_BFF_DAEMON_RELEASED_AT" ;;
    ANALYTICS_PII_SALT)         write_param_value "$KEY_NAME" "$PREFETCH_ANALYTICS_PII_SALT" ;;
    INTERNAL_SVC_SECRET)        write_param_value "$KEY_NAME" "$PREFETCH_INTERNAL_SVC_SECRET" ;;
    POSTHOG_PERSONAL_API_KEY)   write_param_value "$KEY_NAME" "$PREFETCH_POSTHOG_PERSONAL_API_KEY" ;;
    POSTHOG_PROJECT_ID)         write_param_value "$KEY_NAME" "$PREFETCH_POSTHOG_PROJECT_ID" ;;
    BFF_TOS_VERSION)            write_param_value "$KEY_NAME" "$PREFETCH_BFF_TOS_VERSION" ;;
    BFF_PRIVACY_POLICY_VERSION) write_param_value "$KEY_NAME" "$PREFETCH_BFF_PRIVACY_POLICY_VERSION" ;;
    *)
      echo "ERROR: manifest entry ${i} (${KEY_NAME}) scope=${KEY_SCOPE} has no pre-fetched value mapping." >&2
      echo "       Add a Step 1 pre-fetch for ${KEY_NAME} and a case arm here." >&2
      exit 1
      ;;
  esac

  i=$((i + 1))
done

# Unset non-secret pre-fetched variables (secrets are unset in cleanup_creds trap).
unset PREFETCH_ALLOWED_ORIGINS
unset PREFETCH_CLERK_FRONTEND_API
unset PREFETCH_MAILCHIMP_LIST_ID
unset PREFETCH_POSTHOG_HOST
unset PREFETCH_BFF_DAEMON_LATEST_VERSION
unset PREFETCH_BFF_DAEMON_RELEASED_AT
unset PREFETCH_POSTHOG_PROJECT_ID
unset PREFETCH_BFF_TOS_VERSION
unset PREFETCH_BFF_PRIVACY_POLICY_VERSION

chmod 600 "$ENV_FILE"
echo "Production env provisioned at ${ENV_FILE}."
