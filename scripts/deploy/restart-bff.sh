#!/bin/sh
# restart-bff.sh
# Restarts the production BFF systemd service.
# Runs ON the EC2 instance via SSM RunShellScript.
#
# Service name is sourced from infra/config/deploy-env.sh (BFF_SERVICE).
#
# Migration-skew guard (#1036, fixed #1151):
#   Before restarting, compares the STAGED binary's embedded migration version
#   (read via /usr/local/bin/${BFF_BINARY} --print-embedded-version) against
#   the database's current schema version (schema_migrations MAX(version)).
#
#   stage-binary.sh (deploy step 11) has already mv'd the new binary to
#   /usr/local/bin/${BFF_BINARY} before this script runs, so the staged binary
#   is already on disk.  Reading from the staged binary (not from the running
#   service's /healthz) is the correct comparison: we are about to start the
#   staged binary, and we need to know whether it is compatible with the DB.
#
#   Fix for #1151: the original guard read the running binary's /healthz,
#   which reports the OLD binary's embedded version.  On migration-bearing
#   releases (e.g. staged=109, DB=109 after run-migrations.sh, running=108)
#   this caused the guard to abort every deploy incorrectly.
#
#   If DB > staged binary (e.g. a genuine rollback to an older binary after
#   migrations have already run):
#     -> exit 1, abort the restart.
#
#   Inconclusive read (binary absent, --print-embedded-version fails,
#   DB unreachable, psql absent):
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
    STAGED_BINARY="/usr/local/bin/${BFF_BINARY}"

    echo "[restart-bff] Checking migration-skew guard before restart..."

    # Step 1: read the STAGED binary's embedded migration version directly.
    # stage-binary.sh (deploy step 11) has already placed the new binary at
    # the canonical install path before this script runs.
    BINARY_VERSION=""
    if [ -x "$STAGED_BINARY" ]; then
        BINARY_VERSION=$("$STAGED_BINARY" --print-embedded-version 2>/dev/null || true)
    fi

    if [ -z "$BINARY_VERSION" ] || [ "$BINARY_VERSION" = "unknown" ]; then
        echo "[restart-bff] WARN: could not read embedded version from $STAGED_BINARY -- skipping guard (fail-open)."
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
            printf '[restart-bff] ERROR: database schema version (%s) is ahead of staged binary embedded version (%s).\n' \
                "$DB_VERSION" "$BINARY_VERSION" >&2
            printf '[restart-bff] The staged binary does not include migration %s.\n' "$DB_VERSION" >&2
            printf '[restart-bff] Deploy a binary that includes migration %s, or set FORCE_RESTART=1 to override (document the incident).\n' \
                "$DB_VERSION" >&2
            exit 1
        else
            printf '[restart-bff] Migration-skew guard PASS: staged-binary=%s db=%s.\n' "$BINARY_VERSION" "$DB_VERSION"
        fi
    fi
fi

systemctl restart "$BFF_SERVICE"
echo "${BFF_SERVICE} service restarted."
