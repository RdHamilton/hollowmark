#!/bin/sh
# restart-bff-staging.sh
# Restarts the staging BFF systemd service.
# Runs ON the EC2 instance via SSM RunShellScript (as root).
#
# Service name is sourced from infra/config/deploy-env.sh (BFF_STAGING_SERVICE).
#
# Migration-skew guard (#1036):
#   Before restarting, compares the running binary's embedded migration version
#   (from GET /healthz -> migration_version) against the database's current
#   schema version (schema_migrations MAX(version)).
#
#   If DB > binary (e.g. a rollback to an older binary after migrations have
#   already run):
#     -> exit 1, abort the restart.
#
#   Inconclusive read (healthz unreachable, DB unreachable, psql absent):
#     -> warn and proceed (fail-open).
#
#   Break-glass override: FORCE_RESTART=1 skips the guard entirely.
#   Use only in emergencies; document the override in the incident record.

set -e

# Source canonical deploy facts.  deploy-env.sh is downloaded alongside
# this script from S3 into /tmp/ before execution.
. /tmp/deploy-env.sh

SERVICE="$BFF_STAGING_SERVICE"
UNIT_FILE="/etc/systemd/system/${SERVICE}.service"

# Guard: verify the systemd unit exists before attempting restart.
# If the unit is missing the EC2 instance was never bootstrapped -- surface
# a clear error rather than a cryptic "Unit not found" from systemctl.
if [ ! -f "$UNIT_FILE" ]; then
    echo "[restart-bff-staging] ERROR: systemd unit not found at $UNIT_FILE"
    echo "  The EC2 instance has not been bootstrapped for the staging service."
    echo "  Run infra/scripts/install-staging-service.sh on the instance first,"
    echo "  or re-run the infra CloudFormation bootstrap stack."
    exit 1
fi

# ---------------------------------------------------------------------------
# Migration-skew guard
# ---------------------------------------------------------------------------
if [ "${FORCE_RESTART:-0}" = "1" ]; then
    echo "[restart-bff-staging] FORCE_RESTART=1 -- skipping migration-skew guard (break-glass)."
else
    BFF_STAGING_PORT="${BFF_STAGING_PORT:-8081}"
    HEALTHZ_URL="http://127.0.0.1:${BFF_STAGING_PORT}/healthz"

    echo "[restart-bff-staging] Checking migration-skew guard before restart..."

    # Step 1: read the running binary's embedded migration version from /healthz.
    BINARY_VERSION=""
    if curl -sf --max-time 5 "$HEALTHZ_URL" > /tmp/healthz_staging_response.json 2>/dev/null; then
        if command -v python3 > /dev/null 2>&1; then
            BINARY_VERSION=$(python3 -c \
                "import json; d=json.load(open('/tmp/healthz_staging_response.json')); print(d.get('migration_version',''))" \
                2>/dev/null || true)
        elif command -v python > /dev/null 2>&1; then
            BINARY_VERSION=$(python -c \
                "import json; d=json.load(open('/tmp/healthz_staging_response.json')); print(d.get('migration_version',''))" \
                2>/dev/null || true)
        fi
    fi

    if [ -z "$BINARY_VERSION" ] || [ "$BINARY_VERSION" = "unknown" ]; then
        echo "[restart-bff-staging] WARN: could not read migration_version from $HEALTHZ_URL -- skipping guard (fail-open)."
    else
        # Step 2: read the database's current schema version.
        DB_VERSION=""
        if [ -f "$BFF_STAGING_ENV_FILE" ] && command -v psql > /dev/null 2>&1; then
            DB_URL=$(grep '^DATABASE_URL=' "$BFF_STAGING_ENV_FILE" | sed 's/^DATABASE_URL=//' | tr -d '"' || true)
            if [ -n "$DB_URL" ]; then
                DB_VERSION=$(PGCONNECT_TIMEOUT=5 psql "$DB_URL" -t -A \
                    -c "SELECT COALESCE(MAX(version),0) FROM schema_migrations" 2>/dev/null || true)
            fi
        fi

        if [ -z "$DB_VERSION" ]; then
            echo "[restart-bff-staging] WARN: could not read DB schema version -- skipping guard (fail-open)."
        elif [ "$DB_VERSION" -gt "$BINARY_VERSION" ] 2>/dev/null; then
            printf '[restart-bff-staging] ERROR: database schema version (%s) is ahead of binary embedded version (%s).\n' \
                "$DB_VERSION" "$BINARY_VERSION" >&2
            printf '[restart-bff-staging] The binary does not include migration %s.\n' "$DB_VERSION" >&2
            printf '[restart-bff-staging] Deploy a binary that includes migration %s, or set FORCE_RESTART=1 to override (document the incident).\n' \
                "$DB_VERSION" >&2
            exit 1
        else
            printf '[restart-bff-staging] Migration-skew guard PASS: binary=%s db=%s.\n' "$BINARY_VERSION" "$DB_VERSION"
        fi
    fi
fi

systemctl daemon-reload
systemctl enable "$SERVICE"
systemctl restart "$SERVICE"

echo "[restart-bff-staging] ${SERVICE} restarted successfully."
systemctl status "$SERVICE" --no-pager --lines=5 || true
