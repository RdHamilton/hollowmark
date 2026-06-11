#!/usr/bin/env bash
# provision-staging-env.sh
# Renders the staging env file from SSM parameter hierarchy.
# Runs ON the EC2 instance via SSM RunShellScript.
# Canonical copy -- do not duplicate into mtga-companion-infra.
#
# Credential model (Path A bridge, per ADR-022 sect4A.7):
#   1. The EC2 instance role (mtga-companion-ec2-role-production) is the
#      AWS calling identity inherited from the SSM RunShellScript session.
#   2. This script's first AWS call is sts:AssumeRole into the scoped
#      vaultmtg-staging-deploy-provisioner role. The instance role has
#      sts:AssumeRole permission on exactly that one ARN (granted by
#      cloudformation/ec2.yml StagingDeployProvisionerAssumeRole policy),
#      and the provisioner role's trust policy permits the instance role
#      to assume it (EC2InstanceRoleBridge statement on staging-deploy-role.yml).
#   3. The temporary credentials returned by AssumeRole are exported as
#      AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY / AWS_SESSION_TOKEN, scoping
#      every subsequent aws ssm get-parameter and aws secretsmanager call
#      to the provisioner role's permissions (/vaultmtg/app/staging/* +
#      kms:Decrypt via SSM + secretsmanager on mtga-companion/staging/*).
#   4. An EXIT trap unsets the env vars after the env file is written so
#      that no leftover creds remain in the SSM shell environment.
#
# Negative test (manual, AC5 -- see EC-6 proof):
#   To prove the script cannot silently fall back to instance-role creds,
#   temporarily delete the EC2InstanceRoleBridge statement from
#   staging-deploy-role.yml and redeploy that stack, then re-run this
#   script via the staging deploy. The aws sts assume-role call must fail
#   with AccessDenied and the script must abort with exit 1 (set -e).
#   Restore the bridge statement immediately afterwards. DO NOT run this
#   in CI -- it would break every subsequent staging deploy until manual
#   restoration. Run only as a one-off audit step with the on-call
#   engineer available to revert.
#
# SSM parameter names and file paths are sourced from
# infra/config/deploy-env.sh -- do NOT hardcode them here.
#
# ============================================================================
# STAGING SSM PARAMETER INVENTORY (ADR-075 D4 -- updated by tickets #1072, #1097)
# ============================================================================
#
# NAMESPACE SPLIT:
#   /vaultmtg/app/staging/*  -- BFF runtime params (read by THIS script)
#   /vaultmtg/staging/*      -- SPA/build-time + CI params (see KEEP table below)
#
# /vaultmtg/app/staging/* BFF-RUNTIME PARAMS READ BY THIS SCRIPT:
# (types match prod mirrors in ADR-075 D2; all under ec2.yml IAM Statement 3)
#
#   PARAM                        TYPE          ENV KEY WRITTEN
#   ---------------------------  ------------  ---------------------------
#   PORT                         String        PORT
#   ALLOWED_ORIGINS              String        ALLOWED_ORIGINS
#   CLERK_PUBLISHABLE_KEY        String        CLERK_PUBLISHABLE_KEY
#   CLERK_SECRET_KEY             SecureString  CLERK_SECRET_KEY
#   CLERK_FRONTEND_API           String        CLERK_FRONTEND_API
#   db-secret-arn                String        (used to build DATABASE_URL)
#   db-endpoint                  String        (used to build DATABASE_URL)
#   db-name                      String        (used to build DATABASE_URL)
#   resend-api-key               SecureString  RESEND_API_KEY
#   sentry-dsn-bff               SecureString  SENTRY_DSN   (canonical name; #1072)
#   daemon-jwt-secret            SecureString  DAEMON_JWT_SECRET  (added by #1072)
#   discord-bot-token            SecureString  DISCORD_BOT_TOKEN
#   discord-guild-id             String        DISCORD_GUILD_ID
#   mailchimp-api-key            SecureString  MAILCHIMP_API_KEY
#   mailchimp-list-id            String        MAILCHIMP_LIST_ID
#   crisp-website-id             String        CRISP_WEBSITE_ID
#   posthog-api-key              SecureString  POSTHOG_API_KEY    (added by #1072)
#   posthog-host                 String        POSTHOG_HOST       (added by #1072)
#   BFF_DAEMON_LATEST_VERSION    String        BFF_DAEMON_LATEST_VERSION
#   BFF_DAEMON_RELEASED_AT       String        BFF_DAEMON_RELEASED_AT
#   analytics-pii-salt           SecureString  ANALYTICS_PII_SALT  (added by #1597)
#   internal-svc-secret          SecureString  INTERNAL_SVC_SECRET (added by #952)
#
# /vaultmtg/app/staging/* LAMBDA M2M DB-PASSWORD PARAMS (seeded by this script, NOT
# written to BFF env file -- consumed by Lambda execution roles at deploy time):
#   PARAM                        TYPE          CONSUMER
#   ---------------------------  ------------  -----------------------------------
#   meta-scrape-db-password      SecureString  meta-scrape Lambda (ssm:GetParameter)
#   sync-db-password             SecureString  sync Lambda (ssm:GetParameter)
#   Seeded idempotently via --no-overwrite (ticket #1097, ADR-075 Amendment II SS B-7).
#   To rotate: change the DB role password, delete the params, then re-run; or use
#   aws ssm put-parameter --overwrite directly after updating the mtga_sync role.
#
# SENTRY NAME ASYMMETRY (resolved by #1072, ADR-075 D4):
#   prod canonical    = /vaultmtg/app/production/sentry-dsn-bff
#   staging canonical = /vaultmtg/app/staging/sentry-dsn-bff   <-- added #1072
#   staging legacy    = /vaultmtg/app/staging/sentry-bff-dsn   <-- to be deleted after
#   This script reads the canonical name. Legacy alias stays in SSM until a
#   follow-up confirms no remaining consumers reference it.
#
# PARAMS INTENTIONALLY ABSENT FROM /vaultmtg/app/staging/:
#   bff-admin-token      -- prod bootstrap-carried; staging does not provision
#                           admin endpoints via this path (ticket #1074)
#   canary-clerk-*       -- prod canary is prod-only; no staging canary service
#   ro-db-secret-arn / app-db-secret-arn       -- prod-only DB access patterns
#
# /vaultmtg/staging/* SPA/BUILD-TIME + CI PARAMS (KEEP -- do NOT delete):
#   PARAM                    TYPE          CONSUMER
#   -----------------------  ------------  -----------------------------------------
#   spa-bucket-name          String        deploy-spa-staging.yml (S3 deploy target)
#   spa-distribution-id      String        deploy-spa-staging.yml (CF invalidation)
#   sentry-spa-dsn           SecureString  deploy-spa-staging.yml (VITE_SENTRY_DSN)
#   sentry-auth-token        SecureString  deploy-spa-staging.yml (Sentry release)
#   ci-smoke-token           SecureString  deploy-spa-staging.yml (smoke auth JWT)
#   CLERK_PUBLISHABLE_KEY    String        deploy-spa-staging.yml (VITE_CLERK_PK)
#   CLERK_FRONTEND_API       String        staging-auth-smoke.sh:41 (cross-check)
#   canary-clerk-secret-key  SecureString  staging-replay-gate.yml:162
#   canary-clerk-user-id     SecureString  staging-replay-gate.yml:163
#   ec2-instance-id          String        staging-replay-gate.yml:581 (teardown)
#
# Any new BFF-runtime parameter added here MUST also be granted in the
# provisioner role's StagingProvisioningSSMRead policy in
# hollowmark-infra/cloudformation/staging-deploy-role.yml.
# ============================================================================

set -e

# Source canonical deploy facts.  deploy-env.sh is downloaded alongside
# this script from S3 into /tmp/ before execution.
. /tmp/deploy-env.sh

REGION="$DEPLOY_REGION"
ENV_FILE="$BFF_STAGING_ENV_FILE"
ENV_DIR="$BFF_STAGING_ENV_DIR"

# ---------------------------------------------------------------------------
# Step 1: Assume the scoped provisioner role.
#
# Calls aws sts assume-role using the EC2 instance role (the SSM session's
# default credentials) as the calling principal. Exports the returned
# temporary credentials so every subsequent aws CLI call in this script
# runs as vaultmtg-staging-deploy-provisioner.
#
# 900s == 15 minutes, the minimum allowed by IAM. The script completes in
# under 30s in practice, so the short TTL is fine and reduces blast radius
# if the credentials leak.
# ---------------------------------------------------------------------------
PROVISIONER_ROLE_ARN="arn:aws:iam::901347789205:role/vaultmtg-staging-deploy-provisioner"
SESSION_NAME="env-render-$(date +%s)"

# Defense in depth: clear temporary credentials on any exit (success or
# failure) so the SSM shell environment never carries them past this script.
cleanup_creds() {
  unset AWS_ACCESS_KEY_ID
  unset AWS_SECRET_ACCESS_KEY
  unset AWS_SESSION_TOKEN
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

# Tab-separated by --output text; split into the three variables.
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

# Verify the assumed identity before proceeding -- guards against any silent
# fallback to instance-role credentials.
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

mkdir -p "$ENV_DIR"
# Start with an empty env file -- fully re-render on each deploy.
: > "$ENV_FILE"
chmod 600 "$ENV_FILE"

# Source shared helpers (write_param, write_database_url) from provision-lib.sh.
# provision-lib.sh is downloaded alongside this script from S3 into /tmp/
# before execution.  It requires REGION and ENV_FILE to already be set.
. /tmp/provision-lib.sh

# Source the key manifest (pure declarative data -- no executable logic).
# ssm-key-manifest.sh is downloaded alongside this script from S3 into /tmp/.
# ADR-075 D3: single source of truth for the BFF env-var <-> SSM mapping.
. /tmp/ssm-key-manifest.sh

# AWS region -- required by the BFF's Secrets Manager client at startup.
printf 'AWS_DEFAULT_REGION=%s\n' "$REGION" >> "$ENV_FILE"
echo "AWS_DEFAULT_REGION provisioned."

# DATABASE_URL: provisioner-side fetch + credential splice (#2461).
#
# The scoped vaultmtg-staging-deploy-provisioner role this script already
# assumes holds the grant on the staging RDS secret via the
# StagingProvisioningSecretsManager statement in staging-deploy-role.yml.
# write_database_url() fetches the JSON secret, URL-encodes credentials via
# jq @uri, and writes the complete DATABASE_URL to the env file.  No
# DB_SECRET_ARN is written -- the BFF's runtime SM path stays dormant.
# Rotation impact: re-run the staging deploy to pick up a rotated password.
write_database_url "$SSM_STAGING_DB_SECRET_ARN" "$SSM_STAGING_DB_ENDPOINT" "$SSM_STAGING_DB_NAME"

# Manifest-driven provisioning loop (ADR-075 D3).
#
# Iterates over every entry in ssm-key-manifest.sh and provisions keys
# scoped to "staging" or "both".  Keys scoped to "prod-only" are skipped.
# Per-env SSM path overrides are applied in the case statement below for
# entries whose prod path (recorded in the manifest) differs from staging.
#
# Note on DAEMON_JWT_SECRET: scope is "both" in the manifest; staging
# provisions it from SSM_STAGING_DAEMON_JWT_SECRET.  On prod this key is
# bootstrap-carried (Option B -- ADR-075 D3) and is NOT in the deploy loop
# there; see ssm-key-manifest.sh entry 5 for the full annotation.
i=0
while [ "$i" -lt "$MANIFEST_KEY_COUNT" ]; do
  eval "KEY_NAME=\${MANIFEST_KEY_${i}_NAME}"
  eval "KEY_TYPE=\${MANIFEST_KEY_${i}_TYPE}"
  eval "KEY_SCOPE=\${MANIFEST_KEY_${i}_SCOPE}"

  # Skip DATABASE_URL -- handled above by write_database_url().
  if [ "$KEY_NAME" = "DATABASE_URL" ]; then
    i=$((i + 1))
    continue
  fi

  # Skip prod-only entries.
  case "$KEY_SCOPE" in
    prod-only)
      i=$((i + 1))
      continue
      ;;
  esac

  # Resolve the staging SSM path.
  # Most staging-only entries already use the staging SSM_VAR from the manifest.
  # For "both" entries, the manifest records the prod SSM_VAR; we override here
  # to the staging mirror path.
  case "$KEY_NAME" in
    SENTRY_DSN)
      SSM_PATH="$SSM_VAULTMTG_STAGING_SENTRY_DSN"
      ;;
    DAEMON_JWT_SECRET)
      SSM_PATH="$SSM_STAGING_DAEMON_JWT_SECRET"
      ;;
    BFF_DAEMON_LATEST_VERSION)
      SSM_PATH="$SSM_STAGING_BFF_DAEMON_LATEST_VERSION"
      ;;
    BFF_DAEMON_RELEASED_AT)
      SSM_PATH="$SSM_STAGING_BFF_DAEMON_RELEASED_AT"
      ;;
    ALLOWED_ORIGINS)
      SSM_PATH="$SSM_STAGING_ALLOWED_ORIGINS"
      ;;
    CLERK_SECRET_KEY)
      SSM_PATH="$SSM_STAGING_CLERK_SECRET_KEY"
      ;;
    CLERK_FRONTEND_API)
      SSM_PATH="$SSM_STAGING_CLERK_FRONTEND_API"
      ;;
    ANALYTICS_PII_SALT)
      SSM_PATH="$SSM_STAGING_ANALYTICS_PII_SALT"
      ;;
    INTERNAL_SVC_SECRET)
      SSM_PATH="$SSM_STAGING_INTERNAL_SVC_SECRET"
      ;;
    MAILCHIMP_API_KEY)
      SSM_PATH="$SSM_VAULTMTG_STAGING_MAILCHIMP_API_KEY"
      ;;
    MAILCHIMP_LIST_ID)
      SSM_PATH="$SSM_VAULTMTG_STAGING_MAILCHIMP_LIST_ID"
      ;;
    POSTHOG_PERSONAL_API_KEY)
      SSM_PATH="$SSM_STAGING_POSTHOG_PERSONAL_API_KEY"
      ;;
    POSTHOG_PROJECT_ID)
      SSM_PATH="$SSM_STAGING_POSTHOG_PROJECT_ID"
      ;;
    *)
      # staging-only entries: the manifest SSM_VAR already points to the staging path.
      eval "SSM_VAR=\${MANIFEST_KEY_${i}_SSM_VAR}"
      eval "SSM_PATH=\${$SSM_VAR}"
      ;;
  esac

  DECRYPT_FLAG=""
  if [ "$KEY_TYPE" = "secret" ]; then
    DECRYPT_FLAG="--with-decryption"
  fi

  write_param "$KEY_NAME" "$SSM_PATH" $DECRYPT_FLAG

  i=$((i + 1))
done

# PostHog analytics -- staging instance added by #1072.
# Provisioned outside the manifest loop because the v2 manifest records
# POSTHOG_* as prod-only (the staging params did not exist when the plan was
# approved).  Ticket #1075 (FF-2 parity check) will reconcile the manifest
# scope if the staging PostHog params are promoted to long-term status.
write_param POSTHOG_API_KEY "$SSM_STAGING_POSTHOG_API_KEY" --with-decryption
write_param POSTHOG_HOST    "$SSM_STAGING_POSTHOG_HOST"

chmod 600 "$ENV_FILE"
echo "Staging env provisioned at ${ENV_FILE}."

# ---------------------------------------------------------------------------
# Lambda M2M DB-password SSM params (ticket #1097, ADR-075 Amendment II SS B-7)
#
# Seed the two SecureString params consumed by the staging meta-scrape and sync
# Lambda stacks at deploy time (ticket #1098). Both params use --no-overwrite so
# a re-run never silently clobbers a live (possibly rotated) credential. On
# ParameterAlreadyExists the AWS CLI exits non-zero; we swallow that specific
# error and continue so the rest of the provisioning script is unaffected.
#
# The password value (STAGING_MTGA_SYNC_PASSWORD) must be supplied by the caller
# as an environment variable. It must equal the password on the mtga_sync role in
# the staging RDS instance. To rotate: change the DB role password first, then
# delete the SSM params and re-run (or use put-parameter --overwrite directly).
#
# The provisioner role (vaultmtg-staging-deploy-provisioner) assumed above holds
# ssm:PutParameter on /vaultmtg/app/staging/* via StagingProvisioningSSMWrite in
# staging-deploy-role.yml; kms:GenerateDataKey on alias/aws/ssm is included.
# ---------------------------------------------------------------------------
seed_lambda_ssm_param() {
  local name="$1"
  local value="$2"

  if [ -z "$value" ]; then
    echo "ERROR: password value for ${name} is empty -- set STAGING_MTGA_SYNC_PASSWORD." >&2
    exit 1
  fi

  local output
  if output=$(aws ssm put-parameter \
    --name "$name" \
    --type SecureString \
    --value "$value" \
    --no-overwrite \
    --region "$REGION" 2>&1); then
    echo "Lambda SSM param seeded: ${name} (Version 1)."
  else
    case "$output" in
      *ParameterAlreadyExists*)
        echo "Lambda SSM param already exists (skipped, --no-overwrite): ${name}."
        ;;
      *)
        echo "ERROR seeding Lambda SSM param ${name}: ${output}" >&2
        exit 1
        ;;
    esac
  fi
}

if [ -n "${STAGING_MTGA_SYNC_PASSWORD:-}" ]; then
  seed_lambda_ssm_param "/vaultmtg/app/staging/meta-scrape-db-password" "$STAGING_MTGA_SYNC_PASSWORD"
  seed_lambda_ssm_param "/vaultmtg/app/staging/sync-db-password"         "$STAGING_MTGA_SYNC_PASSWORD"
else
  echo "STAGING_MTGA_SYNC_PASSWORD not set -- skipping Lambda SSM param seeding (params already exist or first-time setup not yet run)."
fi
