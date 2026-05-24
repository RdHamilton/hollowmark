#!/usr/bin/env bash
# run-staging-migrations.sh
#
# Applies all golang-migrate migrations from
# services/bff/internal/storage/migrations/postgres/ to the vaultmtg_staging
# database on the shared RDS instance.
#
# The BFF binary embeds migrations at compile time (migrate.go). For the
# staging bootstrap and CI deploy we run migrations via the standalone
# golang-migrate CLI so we don't need to build the full binary first.
#
# Idempotent: golang-migrate tracks applied versions in the schema_migrations
# table. Re-running this script when already at HEAD is a no-op.
#
# SSM parameter names are sourced from infra/config/deploy-env.sh —
# do NOT hardcode them here.
#
# Credential model (Path A bridge, per ADR-022 sect4A.7 -- mirrors PR #2537
# refactor of scripts/deploy/provision-staging-env.sh):
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
#      (and aws s3 sync / cp for the S3 deploy path) to the provisioner
#      role's permissions:
#        - ssm:GetParameter on /vaultmtg/{staging,app/staging}/*
#                          and /mtga-companion/staging/*
#        - secretsmanager:GetSecretValue on rds!db-12c647a0-* (RDS master
#                          credentials, granted by infra PR #187)
#        - kms:Decrypt on alias/aws/{ssm,secretsmanager} (ViaService scoped)
#   4. An EXIT trap unsets the env vars on script exit (success or failure)
#      so no leftover creds remain in the SSM shell environment.
#
# Negative test (manual, AC5 -- see EC-6 proof on PR #2537):
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
# Prerequisites:
#   - golang-migrate CLI installed (see https://github.com/golang-migrate/migrate/tree/master/cmd/migrate)
#     Install: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
#   - Access to AWS SSM (personal profile) to read SSM_STAGING_DATABASE_URL
#   - Network access to RDS (run from the EC2 instance via SSM, or from a
#     machine with VPC access)
#
# Usage:
#   # Run locally (requires VPN or EC2 tunnel):
#   AWS_PROFILE=personal bash infra/scripts/run-staging-migrations.sh
#
#   # Run on EC2 via SSM:
#   aws ssm send-command --profile personal \
#     --instance-ids <EC2_INSTANCE_ID> \
#     --document-name AWS-RunShellScript \
#     --parameters 'commands=["cd /opt/vaultmtg && bash infra/scripts/run-staging-migrations.sh"]'

set -euo pipefail

# Source canonical deploy facts.
# On EC2 (SSM path): deploy-env.sh is downloaded alongside this script into /tmp/.
# Locally: source from the repo root.
if [[ -f /tmp/deploy-env.sh ]]; then
    . /tmp/deploy-env.sh
else
    _SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    . "${_SCRIPT_DIR}/../../infra/config/deploy-env.sh"
fi

REGION="${AWS_REGION:-$DEPLOY_REGION}"

# ---------------------------------------------------------------------------
# Step 0: Assume the scoped provisioner role.
#
# Calls aws sts assume-role using the EC2 instance role (the SSM session's
# default credentials) as the calling principal. Exports the returned
# temporary credentials so every subsequent aws CLI call in this script
# runs as vaultmtg-staging-deploy-provisioner.
#
# 900s == 15 minutes, the minimum allowed by IAM. The migration step
# completes in well under 15 minutes in practice (typically < 60s).
#
# Locally (AWS_PROFILE set, e.g. running from a developer laptop or break-
# glass admin session) the assume-role guard is skipped -- the dev profile
# is not in the provisioner role's trust policy, so the assume call would
# fail with AccessDenied. Local runs continue to use the named profile.
# ---------------------------------------------------------------------------
PROVISIONER_ROLE_ARN="arn:aws:iam::901347789205:role/vaultmtg-staging-deploy-provisioner"
SESSION_NAME="migrations-$(date +%s)"

# Defense in depth: clear temporary credentials on any exit (success or
# failure) so the SSM shell environment never carries them past this script.
cleanup_creds() {
    unset AWS_ACCESS_KEY_ID
    unset AWS_SECRET_ACCESS_KEY
    unset AWS_SESSION_TOKEN
}
trap cleanup_creds EXIT

if [[ -z "${AWS_PROFILE:-}" ]]; then
    echo "[run-staging-migrations] Assuming role ${PROVISIONER_ROLE_ARN} as session ${SESSION_NAME}..."
    ASSUME_OUTPUT=$(aws sts assume-role \
        --role-arn          "$PROVISIONER_ROLE_ARN" \
        --role-session-name "$SESSION_NAME" \
        --duration-seconds  900 \
        --region            "$REGION" \
        --query             'Credentials.[AccessKeyId,SecretAccessKey,SessionToken]' \
        --output            text)

    if [[ -z "$ASSUME_OUTPUT" ]]; then
        echo "[run-staging-migrations] ERROR: aws sts assume-role returned empty credentials." >&2
        exit 1
    fi

    # Tab-separated by --output text; split into the three variables.
    AWS_ACCESS_KEY_ID=$(echo     "$ASSUME_OUTPUT" | awk '{print $1}')
    AWS_SECRET_ACCESS_KEY=$(echo "$ASSUME_OUTPUT" | awk '{print $2}')
    AWS_SESSION_TOKEN=$(echo     "$ASSUME_OUTPUT" | awk '{print $3}')

    if [[ -z "$AWS_ACCESS_KEY_ID" || -z "$AWS_SECRET_ACCESS_KEY" || -z "$AWS_SESSION_TOKEN" ]]; then
        echo "[run-staging-migrations] ERROR: aws sts assume-role returned incomplete credentials." >&2
        exit 1
    fi

    export AWS_ACCESS_KEY_ID
    export AWS_SECRET_ACCESS_KEY
    export AWS_SESSION_TOKEN

    # Verify the assumed identity before proceeding -- guards against any
    # silent fallback to instance-role credentials.
    CALLER_ARN=$(aws sts get-caller-identity --query Arn --output text)
    case "$CALLER_ARN" in
        *":assumed-role/vaultmtg-staging-deploy-provisioner/${SESSION_NAME}")
            echo "[run-staging-migrations] Assumed role identity confirmed: ${CALLER_ARN}"
            ;;
        *)
            echo "[run-staging-migrations] ERROR: caller identity ${CALLER_ARN} is not the provisioner role -- refusing to continue." >&2
            exit 1
            ;;
    esac
else
    echo "[run-staging-migrations] AWS_PROFILE=${AWS_PROFILE} set -- skipping assume-role (local/dev path)."
fi

# DEPLOY_BUCKET is set by the staging deploy workflow (injected via SSM command
# environment). When set, migrations are downloaded from S3 instead of read from
# a local repo checkout -- this is the normal path on the EC2 staging instance.
DEPLOY_BUCKET="${DEPLOY_BUCKET:-}"

# When run on EC2 via SSM (from /tmp), BASH_SOURCE[0] resolves to /tmp and
# the relative ../../ traversal produces a broken path. Use the canonical EC2
# repo location when the relative path doesn't contain a services/ tree, then
# fall back to the relative path for local development use.
_SCRIPT_RELATIVE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
if [[ -d "$_SCRIPT_RELATIVE_ROOT/services/bff" ]]; then
    REPO_ROOT="$_SCRIPT_RELATIVE_ROOT"
else
    REPO_ROOT="/opt/mtga-companion"
fi
MIGRATIONS_DIR="$REPO_ROOT/services/bff/internal/storage/migrations/postgres"

if [[ ! -d "$MIGRATIONS_DIR" ]]; then
    if [[ -n "$DEPLOY_BUCKET" ]]; then
        # CI deploy path: download migrations from S3 (uploaded by staging-deploy.yml)
        echo "[run-staging-migrations] Repo not at $REPO_ROOT -- downloading migrations from s3://$DEPLOY_BUCKET/migrations/postgres/ ..."
        MIGRATIONS_DIR="/tmp/staging-migrations-postgres"
        mkdir -p "$MIGRATIONS_DIR"
        aws s3 sync "s3://$DEPLOY_BUCKET/migrations/postgres/" "$MIGRATIONS_DIR/" --region "$REGION"
        echo "[run-staging-migrations] Migrations downloaded to $MIGRATIONS_DIR"
    else
        echo "[run-staging-migrations] ERROR: migrations directory not found at $MIGRATIONS_DIR"
        echo "  Expected: $MIGRATIONS_DIR"
        echo "  On EC2 set DEPLOY_BUCKET to download migrations from S3."
        echo "  Locally: run from the repo root or set REPO_ROOT."
        exit 1
    fi
fi

# Install golang-migrate CLI if not present.
# On the EC2 staging instance this is not pre-installed; we fetch the latest
# linux/amd64 release tarball from GitHub and install to /usr/local/bin.
if ! command -v migrate &>/dev/null; then
    echo "[run-staging-migrations] migrate CLI not found -- installing ..."
    MIGRATE_VERSION="v4.18.3"
    MIGRATE_TARBALL="migrate.linux-amd64.tar.gz"
    curl -fsSL \
        "https://github.com/golang-migrate/migrate/releases/download/${MIGRATE_VERSION}/${MIGRATE_TARBALL}" \
        -o "/tmp/${MIGRATE_TARBALL}"
    tar -xzf "/tmp/${MIGRATE_TARBALL}" -C /tmp
    install -m 0755 /tmp/migrate /usr/local/bin/migrate
    echo "[run-staging-migrations] migrate ${MIGRATE_VERSION} installed."
fi

echo "[run-staging-migrations] Fetching staging DATABASE_URL from SSM..."

# On EC2 the instance IAM role provides credentials -- no named profile exists.
# Locally, AWS_PROFILE can be set to override (defaults to 'personal').
_PROFILE_ARG=()
if [[ -n "${AWS_PROFILE:-}" ]]; then
    _PROFILE_ARG=(--profile "$AWS_PROFILE")
fi

DATABASE_URL=$(aws ssm get-parameter \
    "${_PROFILE_ARG[@]}" \
    --region   "$REGION" \
    --name     "$SSM_STAGING_DATABASE_URL" \
    --with-decryption \
    --query    "Parameter.Value" \
    --output   text)

if [[ -z "$DATABASE_URL" ]]; then
    echo "[run-staging-migrations] ERROR: ${SSM_STAGING_DATABASE_URL} is empty."
    echo "  Run infra/scripts/create-staging-db.sh first."
    exit 1
fi

# golang-migrate expects a postgres:// DSN (not pgx5://).
# Normalize to postgres:// if the SSM value uses a different scheme.
DATABASE_URL="${DATABASE_URL/pgx5:\/\//postgres://}"
DATABASE_URL="${DATABASE_URL/postgresql:\/\//postgres://}"

echo "[run-staging-migrations] Applying migrations from $MIGRATIONS_DIR ..."
echo "[run-staging-migrations] Target DB: ${DATABASE_URL%%@*}@<host redacted>"

# ---------------------------------------------------------------------------
# Pre-migration: fetch master credentials and transfer table ownership to the
# migration user. ALTER TABLE (e.g. DROP COLUMN) requires the executing user
# to OWN the table. Tables created by a superuser on initial staging setup
# will block the migration without this step.
# ---------------------------------------------------------------------------
echo "[run-staging-migrations] Fetching master credentials for pre-migration ownership grant..."

SECRET_ARN=$(aws ssm get-parameter \
    "${_PROFILE_ARG[@]}" \
    --region  "$REGION" \
    --name    "$SSM_STAGING_DB_SECRET_ARN" \
    --query   "Parameter.Value" \
    --output  text)

SECRET_JSON=$(aws secretsmanager get-secret-value \
    "${_PROFILE_ARG[@]}" \
    --region    "$REGION" \
    --secret-id "$SECRET_ARN" \
    --query     "SecretString" \
    --output    text)

MASTER_PASSWORD=$(echo "$SECRET_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['password'])")
MASTER_USER=$(echo     "$SECRET_JSON" | python3 -c "import json,sys; print(json.load(sys.stdin)['username'])")

DB_ENDPOINT=$(aws ssm get-parameter \
    "${_PROFILE_ARG[@]}" \
    --region  "$REGION" \
    --name    "$SSM_STAGING_DB_ENDPOINT" \
    --query   "Parameter.Value" \
    --output  text)

# Extract the username from postgres://user:password@host/dbname
_NO_SCHEME="${DATABASE_URL#postgres://}"
MIGRATION_USER="${_NO_SCHEME%%:*}"

echo "[run-staging-migrations] Transferring table ownership to ${MIGRATION_USER} ..."
PGPASSWORD="$MASTER_PASSWORD" psql \
    -h "$DB_ENDPOINT" \
    -U "$MASTER_USER" \
    -d "$DB_STAGING_NAME" \
    -v ON_ERROR_STOP=1 \
    -c "DO \$\$
DECLARE
    r RECORD;
BEGIN
    FOR r IN SELECT tablename FROM pg_tables WHERE schemaname = 'public' LOOP
        EXECUTE format('ALTER TABLE public.%I OWNER TO %I', r.tablename, '${MIGRATION_USER}');
    END LOOP;
END \$\$;"
echo "[run-staging-migrations] Table ownership transferred."

# If a previous migration run failed mid-flight, golang-migrate marks the
# schema_migrations table as dirty and refuses to proceed.  Detect that state
# and force the version back to the last clean version so the fixed migration
# can re-run automatically without manual intervention.
VERSION_OUTPUT=$(migrate \
    -path     "$MIGRATIONS_DIR" \
    -database "$DATABASE_URL" \
    version 2>&1 || true)
if echo "$VERSION_OUTPUT" | grep -qi "dirty"; then
    DIRTY_VER=$(echo "$VERSION_OUTPUT" | grep -oE '[0-9]+' | head -1)
    CLEAN_VER=$((DIRTY_VER - 1))
    echo "[run-staging-migrations] Dirty state detected at version $DIRTY_VER — forcing back to $CLEAN_VER ..."
    migrate \
        -path     "$MIGRATIONS_DIR" \
        -database "$DATABASE_URL" \
        force "$CLEAN_VER"
    echo "[run-staging-migrations] Forced to version $CLEAN_VER."
fi

migrate \
    -path    "$MIGRATIONS_DIR" \
    -database "$DATABASE_URL" \
    up

echo "[run-staging-migrations] Migrations complete."

# ---------------------------------------------------------------------------
# Post-migration: grant table and sequence privileges to the migration user.
# Master credentials were already fetched in the pre-migration step above.
# ---------------------------------------------------------------------------
echo "[run-staging-migrations] Applying table-level grants..."

GRANT_SQL="$REPO_ROOT/infra/db/grant-staging-tables.sql"
if [[ ! -f "$GRANT_SQL" ]] && [[ -n "$DEPLOY_BUCKET" ]]; then
    echo "[run-staging-migrations] Downloading grant-staging-tables.sql from S3 ..."
    aws s3 cp "s3://$DEPLOY_BUCKET/infra-db/grant-staging-tables.sql" /tmp/grant-staging-tables.sql --region "$REGION"
    GRANT_SQL="/tmp/grant-staging-tables.sql"
fi

PGPASSWORD="$MASTER_PASSWORD" psql \
    -h "$DB_ENDPOINT" \
    -U "$MASTER_USER" \
    -d "$DB_STAGING_NAME" \
    -v ON_ERROR_STOP=1 \
    -f "$GRANT_SQL"

echo "[run-staging-migrations] Table grants applied."
echo ""
echo "[run-staging-migrations] vaultmtg_staging is fully initialized and ready."
echo ""
echo "Verify migration head:"
echo "  migrate -path $MIGRATIONS_DIR -database \"\$DATABASE_URL\" version"
