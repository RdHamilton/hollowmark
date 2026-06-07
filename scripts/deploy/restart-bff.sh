#!/bin/sh
# restart-bff.sh
# Restarts the production BFF systemd service.
# Runs ON the EC2 instance via SSM RunShellScript.
#
# Service name is sourced from infra/config/deploy-env.sh (BFF_SERVICE).
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

# ---------------------------------------------------------------------------
# Migration-skew guard
# ---------------------------------------------------------------------------
if [ "${FORCE_RESTART:-0}" = "1" ]; then
    echo "[restart-bff] FORCE_RESTART=1 -- skipping migration-skew guard (break-glass)."
else
    BFF_PORT="${BFF_PORT:-8080}"
    HEALTHZ_URL="http://127.0.0.1:${BFF_PORT}/healthz"

    echo "[restart-bff] Checking migration-skew guard before restart..."

    # Step 1: read the running binary's embedded migration version from /healthz.
    BINARY_VERSION=""
    if curl -sf --max-time 5 "$HEALTHZ_URL" > /tmp/healthz_response.json 2>/dev/null; then
        if command -v python3 > /dev/null 2>&1; then
            BINARY_VERSION=$(python3 -c \
                "import json; d=json.load(open('/tmp/healthz_response.json')); print(d.get('migration_version',''))" \
                2>/dev/null || true)
        elif command -v python > /dev/null 2>&1; then
            BINARY_VERSION=$(python -c \
                "import json; d=json.load(open('/tmp/healthz_response.json')); print(d.get('migration_version',''))" \
                2>/dev/null || true)
        fi
    fi

    if [ -z "$BINARY_VERSION" ] || [ "$BINARY_VERSION" = "unknown" ]; then
        echo "[restart-bff] WARN: could not read migration_version from $HEALTHZ_URL -- skipping guard (fail-open)."
    else
        # Step 2: read the database's current schema version.
        DB_VERSION=""
        if [ -f "$BFF_ENV_FILE" ] && command -v psql > /dev/null 2>&1; then
            DB_URL=$(grep '^DATABASE_URL=' "$BFF_ENV_FILE" | sed 's/^DATABASE_URL=//' | tr -d '"' || true)
            if [ -n "$DB_URL" ]; then
                DB_VERSION=$(PGCONNECT_TIMEOUT=5 psql "$DB_URL" -t -A \
                    -c "SELECT COALESCE(MAX(version),0) FROM schema_migrations" 2>/dev/null || true)
            fi
        fi

        if [ -z "$DB_VERSION" ]; then
            echo "[restart-bff] WARN: could not read DB schema version -- skipping guard (fail-open)."
        elif [ "$DB_VERSION" -gt "$BINARY_VERSION" ] 2>/dev/null; then
            printf '[restart-bff] ERROR: database schema version (%s) is ahead of binary embedded version (%s).\n' \
                "$DB_VERSION" "$BINARY_VERSION" >&2
            printf '[restart-bff] The binary does not include migration %s.\n' "$DB_VERSION" >&2
            printf '[restart-bff] Deploy a binary that includes migration %s, or set FORCE_RESTART=1 to override (document the incident).\n' \
                "$DB_VERSION" >&2
            exit 1
        else
            printf '[restart-bff] Migration-skew guard PASS: binary=%s db=%s.\n' "$BINARY_VERSION" "$DB_VERSION"
        fi
    fi
fi

systemctl restart "$BFF_SERVICE"
echo "${BFF_SERVICE} service restarted."
