#!/usr/bin/env bash
# scripts/deploy/provision-lib.sh
#
# Shared provisioning helpers for EC2 env-file rendering.
# ADR-075 D3: single provisioning model; helpers shared between
# provision-staging-env.sh and any future provision-prod-env.sh.
#
# Sourced by provisioning scripts AFTER they source deploy-env.sh.
# Requires the following variables to be set by the caller:
#   REGION    -- AWS region (typically $DEPLOY_REGION from deploy-env.sh)
#   ENV_FILE  -- absolute path to the env file being written
#
# Functions exported by this library:
#   write_param KEY SSM_PATH [--with-decryption]
#   write_database_url SSM_DB_SECRET_ARN SSM_DB_ENDPOINT SSM_DB_NAME
#
# shellcheck disable=SC2034  # REGION / ENV_FILE are set by the caller.

# ---------------------------------------------------------------------------
# write_param ENV_KEY SSM_PATH [--with-decryption]
#
# Fetches one SSM parameter and appends KEY=VALUE to $ENV_FILE.
# Aborts with exit 1 if the parameter value is empty.
# ---------------------------------------------------------------------------
write_param() {
  local key="$1"
  local path="$2"
  local decrypt="${3:-}"
  local VALUE

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

# ---------------------------------------------------------------------------
# write_database_url SSM_DB_SECRET_ARN_PATH SSM_DB_ENDPOINT_PATH SSM_DB_NAME_PATH
#
# Splices fresh RDS credentials into DATABASE_URL from Secrets Manager and
# writes the complete URL to $ENV_FILE.  The caller must have already assumed
# the scoped provisioner role; this function does NOT assume a role itself.
#
# The URL shape is:
#   postgresql://USER_ENC:PASS_ENC@ENDPOINT:PORT/NAME?sslmode=require
# where USER_ENC and PASS_ENC are jq @uri-encoded so that special characters
# in rotated passwords do not break URL parsing.
#
# No credentials remain in shell variables after this function returns:
# DB_SECRET_JSON, DB_USERNAME, DB_PASSWORD, DB_USERNAME_ENC, and
# DB_PASSWORD_ENC are all unset before return.
#
# Requires DB_PORT and DB_SSL_MODE to be set (sourced from deploy-env.sh).
# ---------------------------------------------------------------------------
write_database_url() {
  local ssm_secret_arn_path="$1"
  local ssm_endpoint_path="$2"
  local ssm_name_path="$3"

  local DB_SECRET_ARN_VALUE DB_ENDPOINT DB_NAME DB_SECRET_JSON
  local DB_USERNAME DB_PASSWORD DB_USERNAME_ENC DB_PASSWORD_ENC

  DB_SECRET_ARN_VALUE=$(aws ssm get-parameter \
    --name "$ssm_secret_arn_path" \
    --region "$REGION" \
    --query Parameter.Value \
    --output text)

  DB_ENDPOINT=$(aws ssm get-parameter \
    --name "$ssm_endpoint_path" \
    --region "$REGION" \
    --query Parameter.Value \
    --output text)

  DB_NAME=$(aws ssm get-parameter \
    --name "$ssm_name_path" \
    --region "$REGION" \
    --query Parameter.Value \
    --output text)

  DB_SECRET_JSON=$(aws secretsmanager get-secret-value \
    --secret-id "$DB_SECRET_ARN_VALUE" \
    --region "$REGION" \
    --query SecretString \
    --output text)

  DB_USERNAME=$(printf '%s' "$DB_SECRET_JSON" | jq -r '.username // empty')
  DB_PASSWORD=$(printf '%s' "$DB_SECRET_JSON" | jq -r '.password // empty')

  if [ -z "$DB_USERNAME" ] || [ -z "$DB_PASSWORD" ]; then
    echo "ERROR: RDS secret JSON missing username or password." >&2
    unset DB_SECRET_JSON DB_USERNAME DB_PASSWORD
    exit 1
  fi

  # URL-encode credentials so special characters in rotated passwords do not
  # break postgresql:// URL parsing.
  DB_USERNAME_ENC=$(jq -rn --arg v "$DB_USERNAME" '$v|@uri')
  DB_PASSWORD_ENC=$(jq -rn --arg v "$DB_PASSWORD" '$v|@uri')

  printf 'DATABASE_URL=postgresql://%s:%s@%s:%s/%s?%s\n' \
    "$DB_USERNAME_ENC" "$DB_PASSWORD_ENC" \
    "$DB_ENDPOINT" "$DB_PORT" "$DB_NAME" "$DB_SSL_MODE" \
    >> "$ENV_FILE"

  unset DB_SECRET_JSON DB_USERNAME DB_PASSWORD DB_USERNAME_ENC DB_PASSWORD_ENC
  echo "DATABASE_URL provisioned (credentials spliced from Secrets Manager under provisioner role)."
}
