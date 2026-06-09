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

# Helper: fetch an SSM parameter value and append KEY=VALUE to the env file.
# Usage: write_param ENV_KEY SSM_PATH [--with-decryption]
write_param() {
  local key="$1"
  local path="$2"
  local decrypt="${3:-}"

  if [ "$decrypt" = "--with-decryption" ]; then
    VALUE=$(aws ssm get-parameter \
      --name "$path" \
      --with-decryption \
      --region "$REGION" \
      --query Parameter.Value \
      --output text)
  else
    VALUE=$(aws ssm get-parameter \
      --name "$path" \
      --region "$REGION" \
      --query Parameter.Value \
      --output text)
  fi

  if [ -z "$VALUE" ]; then
    echo "ERROR: SSM parameter ${path} is empty." >&2
    exit 1
  fi

  printf '%s=%s\n' "$key" "$VALUE" >> "$ENV_FILE"
  echo "${key} provisioned."
}

# AWS region — required by the BFF's Secrets Manager client at startup.
printf 'AWS_DEFAULT_REGION=%s\n' "$REGION" >> "$ENV_FILE"
echo "AWS_DEFAULT_REGION provisioned."

# Core BFF settings
write_param PORT                    "$SSM_STAGING_PORT"
write_param ALLOWED_ORIGINS         "$SSM_STAGING_ALLOWED_ORIGINS"
write_param CLERK_PUBLISHABLE_KEY   "$SSM_STAGING_CLERK_PUBLISHABLE_KEY"
write_param CLERK_SECRET_KEY        "$SSM_STAGING_CLERK_SECRET_KEY" --with-decryption
write_param CLERK_FRONTEND_API      "$SSM_STAGING_CLERK_FRONTEND_API"

# DB credentials: provisioner-side fetch + splice (#2461).
#
# Previously the BFF binary called Secrets Manager at startup to resolve
# DB_SECRET_ARN. That required secretsmanager:GetSecretValue on the EC2
# instance role, which is intentionally narrowed (S-02 / #2375). The
# scoped vaultmtg-staging-deploy-provisioner role this script already
# assumes (see step 1 above) holds the grant on the staging RDS secret
# arn:aws:secretsmanager:...:secret:rds!db-12c647a0-* via the
# StagingProvisioningSecretsManager statement in staging-deploy-role.yml.
#
# We fetch the JSON secret once, here, and write a credential-laden
# DATABASE_URL into the env file. The BFF reads it inline at startup,
# never constructs an AWS SDK client for SM, and never needs the EC2
# instance role to be re-widened. DB_SECRET_ARN is deliberately NOT
# written — the BFF's runtime-resolution path is now opt-in via
# BFF_DB_RESOLVE_FROM_SM=true (also not written) and stays dormant.
#
# Rotation impact: when AWS rotates the RDS secret, the staging deploy
# must be re-run to pick up the new password. This trade-off is accepted
# until automated rotation (S-19 / #2356) is wired.
DB_SECRET_ARN_VALUE=$(aws ssm get-parameter \
  --name "$SSM_STAGING_DB_SECRET_ARN" \
  --region "$REGION" \
  --query Parameter.Value \
  --output text)
DB_ENDPOINT=$(aws ssm get-parameter --name "$SSM_STAGING_DB_ENDPOINT" --region "$REGION" --query Parameter.Value --output text)
DB_NAME=$(aws ssm get-parameter --name "$SSM_STAGING_DB_NAME" --region "$REGION" --query Parameter.Value --output text)
DB_SECRET_JSON=$(aws secretsmanager get-secret-value \
  --secret-id "$DB_SECRET_ARN_VALUE" \
  --region "$REGION" \
  --query SecretString \
  --output text)
DB_USERNAME=$(printf '%s' "$DB_SECRET_JSON" | jq -r '.username // empty')
DB_PASSWORD=$(printf '%s' "$DB_SECRET_JSON" | jq -r '.password // empty')
if [ -z "$DB_USERNAME" ] || [ -z "$DB_PASSWORD" ]; then
  echo "ERROR: RDS secret JSON missing username or password." >&2
  exit 1
fi
# URL-encode credentials so any special characters (`@`, `:`, `/`, `?`,
# `#`, `%`, etc.) in the rotated password do not break URL parsing.
DB_USERNAME_ENC=$(jq -rn --arg v "$DB_USERNAME" '$v|@uri')
DB_PASSWORD_ENC=$(jq -rn --arg v "$DB_PASSWORD" '$v|@uri')
printf 'DATABASE_URL=postgresql://%s:%s@%s:%s/%s?%s\n' \
  "$DB_USERNAME_ENC" "$DB_PASSWORD_ENC" "$DB_ENDPOINT" "$DB_PORT" "$DB_NAME" "$DB_SSL_MODE" \
  >> "$ENV_FILE"
unset DB_SECRET_JSON DB_USERNAME DB_PASSWORD DB_USERNAME_ENC DB_PASSWORD_ENC
echo "DATABASE_URL provisioned (credentials spliced from Secrets Manager under provisioner role)."

# VaultMTG service keys
write_param RESEND_API_KEY          "$SSM_VAULTMTG_STAGING_RESEND_API_KEY"          --with-decryption
# SENTRY_DSN reads canonical sentry-dsn-bff (added by #1072; matches prod name).
write_param SENTRY_DSN              "$SSM_VAULTMTG_STAGING_SENTRY_DSN"              --with-decryption
write_param DISCORD_BOT_TOKEN       "$SSM_VAULTMTG_STAGING_DISCORD_BOT_TOKEN"       --with-decryption
write_param DISCORD_GUILD_ID        "$SSM_VAULTMTG_STAGING_DISCORD_GUILD_ID"
write_param MAILCHIMP_API_KEY       "$SSM_VAULTMTG_STAGING_MAILCHIMP_API_KEY"       --with-decryption
write_param MAILCHIMP_LIST_ID       "$SSM_VAULTMTG_STAGING_MAILCHIMP_LIST_ID"
write_param CRISP_WEBSITE_ID        "$SSM_VAULTMTG_STAGING_CRISP_WEBSITE_ID"

# Daemon JWT secret -- M2M auth for daemon->BFF ingest (added by #1072).
write_param DAEMON_JWT_SECRET       "$SSM_STAGING_DAEMON_JWT_SECRET"                --with-decryption

# PostHog analytics (added by #1072 -- was previously absent from staging).
write_param POSTHOG_API_KEY         "$SSM_STAGING_POSTHOG_API_KEY"                  --with-decryption
write_param POSTHOG_HOST            "$SSM_STAGING_POSTHOG_HOST"

# Daemon version metadata -- lets BFF serve correct data from
# GET /api/v1/daemon/version instead of falling back to the 0.1.0 default.
write_param BFF_DAEMON_LATEST_VERSION "$SSM_STAGING_BFF_DAEMON_LATEST_VERSION"
write_param BFF_DAEMON_RELEASED_AT    "$SSM_STAGING_BFF_DAEMON_RELEASED_AT"

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
