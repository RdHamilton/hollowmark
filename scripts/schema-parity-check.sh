#!/usr/bin/env bash
# schema-parity-check.sh — Two-path schema-parity gate (hollowmark-tickets#1192)
#
# WHY THIS SCRIPT EXISTS
# ----------------------
# The incremental migration path (000001 → HEAD applied one-by-one) and the
# consolidated fresh-start path (migrate force 53 → migrate up, which starts
# from 000054_initial_schema) can silently diverge. This class of defect hit
# twice before this harness existed:
#   - #2545 scope correction: incremental left a stale artifact
#   - #1185 game_plays TEXT/BIGINT: pgx encode-plan failure caught late in prod
#
# Both times the divergence was caught by a production failure or a deep PR
# review, not by automated CI. Ray's verdict on PR #1185 authorized this
# standing harness as the class fix.
#
# TWO-PHASE MODEL
# ---------------
# Phase A — FRESH PATH:
#   Start with an empty DB. Mark it as version 53 via `migrate force 53`
#   (no SQL executed — just sets the migration tracker). Then `migrate up`
#   applies 000054_initial_schema onward. This reproduces the production
#   initialization path: a DB that was never walked through 000001–000053.
#
# Phase B — INCREMENTAL PATH:
#   Start with a separate empty DB. Apply `migrate up` with no force, walking
#   every migration from 000001 to HEAD. This is the path the integration test
#   suite uses.
#
# Both databases are dumped with `pg_dump --schema-only`. The dumps are
# normalized (comments, owner lines, SET/SELECT preamble stripped) and diffed.
# Any divergence exits non-zero with the full diff printed to stderr so a
# developer can immediately identify which table/column diverged.
#
# DIVERGENCE FAILURE MODE
# -----------------------
# When the two paths produce different schemas it means either:
#   (a) 000054_initial_schema.up.sql has drifted from what 000001–000053
#       cumulatively produce (the consolidated snapshot is out of sync), OR
#   (b) A migration added after 000054 was written to compensate for a schema
#       state that only exists on the incremental path (or vice versa).
#
# The fix is always to reconcile 000054 or add a corrective migration so that
# both paths converge to the same schema.
#
# USAGE (local)
# -------------
#   ./scripts/schema-parity-check.sh
#
# Requirements:
#   - Docker (pulls pgvector/pgvector:pg16 on first run; subsequent runs use
#     the cached image)
#   - golang-migrate in PATH or at /opt/homebrew/bin/migrate
#     Install: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3
#   - pg_dump (bundled with PostgreSQL client tools)
#     macOS:  brew install postgresql@16  (then add to PATH)
#     Ubuntu: apt-get install -y postgresql-client
#
# Environment overrides (optional):
#   MIGRATE_BIN        Path to the migrate binary (default: auto-detected)
#   PG_PORT_FRESH      Host port for the fresh-path container  (default: 15442)
#   PG_PORT_INCR       Host port for the incremental container (default: 15443)
#   KEEP_CONTAINERS    Set to "1" to leave containers running after exit (debug)
#
# Exit codes:
#   0  Both paths produce identical schemas — parity confirmed
#   1  Schema divergence detected — diff printed to stderr
#   2  Script configuration error (missing binary, container failure, etc.)

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MIGRATIONS_DIR="${REPO_ROOT}/services/bff/internal/storage/migrations/postgres"

PG_PORT_FRESH="${PG_PORT_FRESH:-15442}"
PG_PORT_INCR="${PG_PORT_INCR:-15443}"
PG_USER="postgres"
PG_PASSWORD="postgres"
PG_DB_FRESH="schema_parity_fresh"
PG_DB_INCR="schema_parity_incr"
CONTAINER_FRESH="schema-parity-fresh-$$"
CONTAINER_INCR="schema-parity-incr-$$"
KEEP_CONTAINERS="${KEEP_CONTAINERS:-0}"

# ---------------------------------------------------------------------------
# Locate migrate binary
# ---------------------------------------------------------------------------
if [[ -n "${MIGRATE_BIN:-}" ]]; then
    MIGRATE="${MIGRATE_BIN}"
elif command -v migrate &>/dev/null; then
    MIGRATE="$(command -v migrate)"
elif [[ -x /opt/homebrew/bin/migrate ]]; then
    MIGRATE="/opt/homebrew/bin/migrate"
else
    echo "ERROR: golang-migrate binary not found." >&2
    echo "  Install: go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3" >&2
    exit 2
fi

# ---------------------------------------------------------------------------
# Locate pg_dump
# ---------------------------------------------------------------------------
if ! command -v pg_dump &>/dev/null; then
    echo "ERROR: pg_dump not found. Install PostgreSQL client tools." >&2
    echo "  macOS: brew install postgresql@16  (then add to PATH)" >&2
    echo "  Ubuntu/CI: apt-get install -y postgresql-client" >&2
    exit 2
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { echo "[schema-parity] $*"; }
ok()   { echo "[schema-parity] OK: $*"; }

TMPDIR_PARITY="$(mktemp -d)"
DUMP_FRESH="${TMPDIR_PARITY}/schema_fresh.sql"
DUMP_INCR="${TMPDIR_PARITY}/schema_incr.sql"

cleanup() {
    rm -rf "${TMPDIR_PARITY}" 2>/dev/null || true
    if [[ "${KEEP_CONTAINERS}" != "1" ]]; then
        log "Stopping containers ..."
        docker rm -f "${CONTAINER_FRESH}" &>/dev/null || true
        docker rm -f "${CONTAINER_INCR}" &>/dev/null || true
    else
        log "KEEP_CONTAINERS=1: leaving containers running (${CONTAINER_FRESH}, ${CONTAINER_INCR})."
    fi
}
trap cleanup EXIT

wait_for_postgres() {
    local container="$1"
    local db="$2"
    log "Waiting for postgres in ${container} ..."
    for i in $(seq 1 30); do
        if docker exec "${container}" pg_isready -U "${PG_USER}" -d "${db}" -q 2>/dev/null; then
            ok "Postgres ready in ${container} after ${i}s"
            return 0
        fi
        sleep 1
    done
    echo "ERROR: Postgres in ${container} did not become ready within 30 seconds." >&2
    exit 2
}

# ---------------------------------------------------------------------------
# Start two ephemeral postgres containers (in parallel)
# ---------------------------------------------------------------------------
log "Starting two ephemeral pgvector/pgvector:pg16 containers ..."

docker run -d \
    --name "${CONTAINER_FRESH}" \
    -e POSTGRES_USER="${PG_USER}" \
    -e POSTGRES_PASSWORD="${PG_PASSWORD}" \
    -e POSTGRES_DB="${PG_DB_FRESH}" \
    -p "${PG_PORT_FRESH}:5432" \
    pgvector/pgvector:pg16 \
    >/dev/null

docker run -d \
    --name "${CONTAINER_INCR}" \
    -e POSTGRES_USER="${PG_USER}" \
    -e POSTGRES_PASSWORD="${PG_PASSWORD}" \
    -e POSTGRES_DB="${PG_DB_INCR}" \
    -p "${PG_PORT_INCR}:5432" \
    pgvector/pgvector:pg16 \
    >/dev/null

DB_URL_FRESH="postgres://${PG_USER}:${PG_PASSWORD}@localhost:${PG_PORT_FRESH}/${PG_DB_FRESH}?sslmode=disable"
DB_URL_INCR="postgres://${PG_USER}:${PG_PASSWORD}@localhost:${PG_PORT_INCR}/${PG_DB_INCR}?sslmode=disable"

wait_for_postgres "${CONTAINER_FRESH}" "${PG_DB_FRESH}"
wait_for_postgres "${CONTAINER_INCR}" "${PG_DB_INCR}"

# ---------------------------------------------------------------------------
# Phase A — FRESH PATH
#
# Reproduce the production initialization path: mark the empty DB as version
# 53 via `migrate force 53` (no SQL executed — just sets the schema_migrations
# tracker row), then apply all migrations starting from 000054_initial_schema.
# ---------------------------------------------------------------------------
log "Phase A: Fresh-path migration (force 53 → up from 000054) ..."

"${MIGRATE}" \
    -database "${DB_URL_FRESH}" \
    -path "${MIGRATIONS_DIR}" \
    force 53 2>&1

"${MIGRATE}" \
    -database "${DB_URL_FRESH}" \
    -path "${MIGRATIONS_DIR}" \
    up 2>&1

ok "Phase A: Fresh-path migration complete."

# ---------------------------------------------------------------------------
# Phase B — INCREMENTAL PATH
#
# Walk every migration from 000001 to HEAD, one step at a time — exactly
# what the standard integration test suite does (see integration.yml).
# ---------------------------------------------------------------------------
log "Phase B: Incremental migration (000001 → HEAD) ..."

"${MIGRATE}" \
    -database "${DB_URL_INCR}" \
    -path "${MIGRATIONS_DIR}" \
    up 2>&1

ok "Phase B: Incremental migration complete."

# ---------------------------------------------------------------------------
# Dump and normalize both schemas
#
# pg_dump --schema-only produces deterministic DDL in object-creation order.
# Flags used:
#   --no-comments   omit pg_dump-generated comments
#   --no-owner      omit ALTER ... OWNER TO statements (deployment detail)
#   --no-acl        omit GRANT/REVOKE statements (deployment detail)
#
# Post-processing greps strip:
#   - Residual "--" comment lines (defensive — --no-comments handles most)
#   - SET session-var lines (search_path, client_encoding, etc.)
#   - SELECT pg_catalog.set_config lines (pg_dump preamble)
#   - Blank lines (cosmetic noise)
#
# What remains is pure structural DDL: CREATE TABLE, CREATE INDEX,
# CREATE SEQUENCE, ALTER TABLE ADD CONSTRAINT, CREATE TYPE, etc.
# ---------------------------------------------------------------------------
log "Dumping and normalizing schemas ..."

normalize_dump() {
    local port="$1"
    local db="$2"
    local out="$3"

    PGPASSWORD="${PG_PASSWORD}" pg_dump \
        --host=localhost \
        --port="${port}" \
        --username="${PG_USER}" \
        --schema-only \
        --no-comments \
        --no-owner \
        --no-acl \
        "${db}" \
    | grep -v '^$' \
    | grep -v '^--' \
    | grep -v '^SET ' \
    | grep -v '^SELECT ' \
    > "${out}"
}

normalize_dump "${PG_PORT_FRESH}" "${PG_DB_FRESH}" "${DUMP_FRESH}"
normalize_dump "${PG_PORT_INCR}"  "${PG_DB_INCR}"  "${DUMP_INCR}"

ok "Schema dumps normalized."

# ---------------------------------------------------------------------------
# Compare — the parity assertion
# ---------------------------------------------------------------------------
log "Comparing fresh-path schema vs incremental-path schema ..."

if diff --unified=5 "${DUMP_FRESH}" "${DUMP_INCR}" > "${TMPDIR_PARITY}/schema.diff" 2>&1; then
    echo ""
    echo "[schema-parity] ============================================"
    echo "[schema-parity] PARITY CHECK PASSED"
    echo "[schema-parity]   Phase A (fresh 000054+)       — schema S_fresh"
    echo "[schema-parity]   Phase B (incremental 000001+) — schema S_incr"
    echo "[schema-parity]   Result: S_fresh == S_incr     (no divergence)"
    echo "[schema-parity] ============================================"
    exit 0
else
    echo "" >&2
    echo "[schema-parity] ============================================" >&2
    echo "[schema-parity] PARITY CHECK FAILED — SCHEMA DIVERGENCE DETECTED" >&2
    echo "[schema-parity] ============================================" >&2
    echo "" >&2
    echo "[schema-parity] The fresh-path schema (Phase A: 000054 consolidated)" >&2
    echo "[schema-parity] differs from the incremental-path schema (Phase B: 000001→HEAD)." >&2
    echo "" >&2
    echo "[schema-parity] Diff (--- S_fresh  +++ S_incr):" >&2
    cat "${TMPDIR_PARITY}/schema.diff" >&2
    echo "" >&2
    echo "[schema-parity] HOW TO FIX:" >&2
    echo "[schema-parity]   (a) If 000054_initial_schema.up.sql is stale: update it to match" >&2
    echo "[schema-parity]       the schema produced by 000001–000053 at the relevant point in time." >&2
    echo "[schema-parity]   (b) If a post-054 migration compensates for incremental-only state:" >&2
    echo "[schema-parity]       add a corrective migration so both paths converge." >&2
    echo "[schema-parity]   Reference: hollowmark-tickets#1192" >&2
    echo "[schema-parity] ============================================" >&2
    exit 1
fi
