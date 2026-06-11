#!/usr/bin/env bash
# migration-round-trip.sh
#
# Verifies down-migration correctness by running a full incremental round-trip:
#   1. Apply all migrations UP to HEAD.
#   2. Roll all migrations DOWN to zero.
#   3. Assert post-down end-state: public schema is empty (only schema_migrations
#      remains), which catches incomplete downs that CASCADE masking would hide.
#   4. Apply all migrations UP again to confirm a clean re-apply from zero.
#
# Usage:
#   ./scripts/migration-round-trip.sh
#
# Requirements:
#   - Docker (pulls postgres:15 on first run; subsequent runs use the cached image)
#   - golang-migrate (https://github.com/golang-migrate/migrate) in PATH or
#     at /opt/homebrew/bin/migrate
#
# Environment variables (optional overrides):
#   MIGRATE_BIN    Path to the migrate binary (default: auto-detected)
#   PG_PORT        Host port to map to the ephemeral container (default: 15432)
#   PG_USER        Postgres superuser (default: postgres)
#   PG_PASSWORD    Postgres password  (default: postgres)
#   PG_DB          Database name      (default: round_trip_test)
#   KEEP_CONTAINER Set to "1" to leave the container running after exit (debug)
#
# Exit codes:
#   0  All three assertions passed (up, post-down end-state, re-up)
#   1  A migration step or assertion failed (stderr shows the failing command)
#
# Notes on scope:
#   This script covers the INCREMENTAL path only (000001 → HEAD one-by-one via
#   golang-migrate). The FRESH path (migrate force 53 → up, exercising
#   000054_initial_schema) is tracked in the follow-on #1185 ticket; it is NOT
#   wired to CI yet — that gate belongs to a subsequent PR that adds the fresh
#   path here.

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MIGRATIONS_DIR="${REPO_ROOT}/services/bff/internal/storage/migrations/postgres"

PG_PORT="${PG_PORT:-15432}"
PG_USER="${PG_USER:-postgres}"
PG_PASSWORD="${PG_PASSWORD:-postgres}"
PG_DB="${PG_DB:-round_trip_test}"
CONTAINER_NAME="migration-round-trip-$$"
KEEP_CONTAINER="${KEEP_CONTAINER:-0}"

# Locate migrate binary
if [[ -n "${MIGRATE_BIN:-}" ]]; then
    MIGRATE="${MIGRATE_BIN}"
elif command -v migrate &>/dev/null; then
    MIGRATE="$(command -v migrate)"
elif [[ -x /opt/homebrew/bin/migrate ]]; then
    MIGRATE="/opt/homebrew/bin/migrate"
else
    echo "ERROR: golang-migrate binary not found. Install from https://github.com/golang-migrate/migrate" >&2
    exit 1
fi

DATABASE_URL="postgres://${PG_USER}:${PG_PASSWORD}@localhost:${PG_PORT}/${PG_DB}?sslmode=disable"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { echo "[round-trip] $*"; }
ok()   { echo "[round-trip] OK: $*"; }
fail() { echo "[round-trip] FAIL: $*" >&2; exit 1; }

cleanup() {
    if [[ "${KEEP_CONTAINER}" != "1" ]]; then
        log "Stopping container ${CONTAINER_NAME} ..."
        docker rm -f "${CONTAINER_NAME}" &>/dev/null || true
    else
        log "KEEP_CONTAINER=1: leaving container ${CONTAINER_NAME} running."
    fi
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Start ephemeral postgres:15 container
# ---------------------------------------------------------------------------
log "Starting postgres:15 container on host port ${PG_PORT} ..."
docker run -d \
    --name "${CONTAINER_NAME}" \
    -e POSTGRES_USER="${PG_USER}" \
    -e POSTGRES_PASSWORD="${PG_PASSWORD}" \
    -e POSTGRES_DB="${PG_DB}" \
    -p "${PG_PORT}:5432" \
    pgvector/pgvector:pg15 \
    >/dev/null

# Wait for postgres to accept connections (up to 30 s).
log "Waiting for postgres to be ready ..."
for i in $(seq 1 30); do
    if docker exec "${CONTAINER_NAME}" pg_isready -U "${PG_USER}" -d "${PG_DB}" -q 2>/dev/null; then
        ok "Postgres ready after ${i}s"
        break
    fi
    if [[ "${i}" -eq 30 ]]; then
        fail "Postgres did not become ready within 30 seconds"
    fi
    sleep 1
done

# ---------------------------------------------------------------------------
# Step 1: Full UP
# ---------------------------------------------------------------------------
log "Step 1: Applying all migrations UP ..."
"${MIGRATE}" \
    -database "${DATABASE_URL}" \
    -path "${MIGRATIONS_DIR}" \
    up 2>&1

ok "Step 1 passed: full UP completed without errors."

# ---------------------------------------------------------------------------
# Step 2: Full DOWN (all migrations to zero)
# ---------------------------------------------------------------------------
log "Step 2: Rolling all migrations DOWN to zero ..."

# Count the number of migration pairs to know how many steps to down.
# golang-migrate 'down N' steps is safer than 'down' with a dirty DB.
MIGRATION_COUNT=$(find "${MIGRATIONS_DIR}" -name '*.up.sql' | wc -l | tr -d ' ')
log "  Found ${MIGRATION_COUNT} up-migration files; rolling down ${MIGRATION_COUNT} steps."

"${MIGRATE}" \
    -database "${DATABASE_URL}" \
    -path "${MIGRATIONS_DIR}" \
    down "${MIGRATION_COUNT}" 2>&1

ok "Step 2 passed: full DOWN completed without errors."

# ---------------------------------------------------------------------------
# Step 3: Post-down end-state assertion
#
# After a complete down, the public schema MUST be empty — only the
# schema_migrations table (created/owned by golang-migrate itself, NOT one of
# our migrations) may remain. Any user table left behind is an incomplete down.
# This assertion compensates for CASCADE masking that could hide an incomplete
# down by silently dropping dependents.
# ---------------------------------------------------------------------------
log "Step 3: Asserting post-down end-state (public schema must be empty) ..."

LEFTOVER_TABLES=$(docker exec "${CONTAINER_NAME}" psql \
    -U "${PG_USER}" \
    -d "${PG_DB}" \
    -t -A \
    -c "SELECT tablename FROM pg_tables
        WHERE schemaname = 'public'
          AND tablename != 'schema_migrations'
        ORDER BY tablename;" 2>&1)

if [[ -n "${LEFTOVER_TABLES}" ]]; then
    echo "[round-trip] FAIL: Post-down end-state check failed." >&2
    echo "[round-trip]   The following user tables still exist in 'public' after a full DOWN:" >&2
    echo "${LEFTOVER_TABLES}" | while read -r tbl; do
        echo "[round-trip]     - ${tbl}" >&2
    done
    echo "[round-trip]   This indicates an incomplete down migration." >&2
    echo "[round-trip]   Tip: check for missing DROP TABLE or DROP EXTENSION statements in the" >&2
    echo "[round-trip]   down migration files for whichever migration(s) created the listed tables." >&2
    exit 1
fi

ok "Step 3 passed: public schema is empty after full DOWN (schema_migrations only)."

# Also check for leftover extensions that our migrations create
LEFTOVER_EXTENSIONS=$(docker exec "${CONTAINER_NAME}" psql \
    -U "${PG_USER}" \
    -d "${PG_DB}" \
    -t -A \
    -c "SELECT extname FROM pg_extension
        WHERE extname NOT IN ('plpgsql')
        ORDER BY extname;" 2>&1)

if [[ -n "${LEFTOVER_EXTENSIONS}" ]]; then
    echo "[round-trip] FAIL: Post-down end-state check failed." >&2
    echo "[round-trip]   The following extensions still exist after a full DOWN:" >&2
    echo "${LEFTOVER_EXTENSIONS}" | while read -r ext; do
        echo "[round-trip]     - ${ext}" >&2
    done
    exit 1
fi

ok "Step 3 passed: no leftover extensions after full DOWN."

# ---------------------------------------------------------------------------
# Step 4: Re-UP from zero (confirms migrations are self-consistent)
# ---------------------------------------------------------------------------
log "Step 4: Re-applying all migrations UP from zero ..."
"${MIGRATE}" \
    -database "${DATABASE_URL}" \
    -path "${MIGRATIONS_DIR}" \
    up 2>&1

ok "Step 4 passed: re-UP from zero completed without errors."

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
echo ""
echo "[round-trip] ============================================"
echo "[round-trip] ALL ASSERTIONS PASSED"
echo "[round-trip]   Step 1: Full UP       - OK"
echo "[round-trip]   Step 2: Full DOWN     - OK"
echo "[round-trip]   Step 3: End-state     - OK (schema empty)"
echo "[round-trip]   Step 4: Re-UP         - OK"
echo "[round-trip] ============================================"
