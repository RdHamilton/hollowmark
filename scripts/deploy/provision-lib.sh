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
#   write_param_value KEY VALUE
#   write_database_url DB_SECRET_JSON_VALUE DB_ENDPOINT_VALUE DB_NAME_VALUE
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
# write_param_value ENV_KEY VALUE
#
# Writes KEY=VALUE to $ENV_FILE from a pre-fetched value.  Performs ZERO
# SSM reads -- the caller is responsible for fetching the value under the
# correct AWS identity (the EC2 instance role) BEFORE any sts:AssumeRole.
#
# This is the companion to write_param for production provisioning:
# provision-prod-env.sh pre-fetches all prod SSM values in Step 1 (under the
# EC2 instance role) and then passes them by value here.  The provisioner
# role (vaultmtg-staging-deploy-provisioner) has no ssm:GetParameter on
# /vaultmtg/app/production/*, so any SSM read after assume-role would fail
# with AccessDeniedException.
#
# Aborts with exit 1 if VALUE is empty (guards against silent pre-fetch
# failures where a Step 1 SSM read returned empty instead of raising an error).
# ---------------------------------------------------------------------------
write_param_value() {
  local key="$1"
  local value="$2"

  if [ -z "$value" ]; then
    echo "ERROR: pre-fetched value for ${key} is empty (Step 1 SSM read failed or returned nothing)." >&2
    exit 1
  fi

  printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
  echo "${key} provisioned (pre-fetched value)."
}

# ---------------------------------------------------------------------------
# write_database_url DB_SECRET_JSON_VALUE DB_ENDPOINT_VALUE DB_NAME_VALUE
#
# Splices pre-fetched RDS credentials into DATABASE_URL and writes the
# complete URL to $ENV_FILE.
#
# IMPORTANT: this function does ZERO SSM or Secrets Manager reads.  All three
# arguments must be pre-fetched by the caller under the appropriate AWS
# identity (the EC2 instance role) BEFORE any sts:AssumeRole call.  Keeping
# reads in the caller's pre-assume-role Step 1 block ensures they run under
# the instance role, which has /vaultmtg/app/production/* read access that
# the provisioner role intentionally does not have.
#
# Arguments:
#   $1  DB_SECRET_JSON_VALUE  -- raw JSON string from secretsmanager
#                                get-secret-value, e.g. '{"username":"u","password":"p"}'
#   $2  DB_ENDPOINT_VALUE     -- RDS endpoint hostname
#   $3  DB_NAME_VALUE         -- Postgres database name
#
# The URL shape is:
#   postgresql://USER_ENC:PASS_ENC@ENDPOINT:PORT/NAME?sslmode=require
# where USER_ENC and PASS_ENC are jq @uri-encoded so that special characters
# in rotated passwords do not break URL parsing.
#
# No credentials remain in shell variables after this function returns:
# DB_USERNAME, DB_PASSWORD, DB_USERNAME_ENC, and DB_PASSWORD_ENC are all
# unset before return.
#
# Requires DB_PORT and DB_SSL_MODE to be set (sourced from deploy-env.sh).
# ---------------------------------------------------------------------------
write_database_url() {
  local db_secret_json="$1"
  local db_endpoint="$2"
  local db_name="$3"

  local DB_USERNAME DB_PASSWORD DB_USERNAME_ENC DB_PASSWORD_ENC

  DB_USERNAME=$(printf '%s' "$db_secret_json" | jq -r '.username // empty')
  DB_PASSWORD=$(printf '%s' "$db_secret_json" | jq -r '.password // empty')

  if [ -z "$DB_USERNAME" ] || [ -z "$DB_PASSWORD" ]; then
    echo "ERROR: RDS secret JSON missing username or password." >&2
    unset DB_USERNAME DB_PASSWORD
    exit 1
  fi

  # URL-encode credentials so special characters in rotated passwords do not
  # break postgresql:// URL parsing.
  DB_USERNAME_ENC=$(jq -rn --arg v "$DB_USERNAME" '$v|@uri')
  DB_PASSWORD_ENC=$(jq -rn --arg v "$DB_PASSWORD" '$v|@uri')

  printf 'DATABASE_URL=postgresql://%s:%s@%s:%s/%s?%s\n' \
    "$DB_USERNAME_ENC" "$DB_PASSWORD_ENC" \
    "$db_endpoint" "$DB_PORT" "$db_name" "$DB_SSL_MODE" \
    >> "$ENV_FILE"

  unset DB_USERNAME DB_PASSWORD DB_USERNAME_ENC DB_PASSWORD_ENC
  echo "DATABASE_URL provisioned (credentials spliced from pre-fetched Secrets Manager value)."
}
