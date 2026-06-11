#!/usr/bin/env bash
# scripts/ops/erase-orphaned-user.sh
#
# One-shot ops script — GDPR Art.17 DB-side erasure for an orphaned user whose
# Clerk identity is from a previous Clerk instance (ADR-072 cutover orphan).
#
# This script executes the database-only steps of the erasure cascade defined in
# services/bff/internal/erasure/job.go for a user whose old-instance Clerk user
# ID no longer exists in the current Clerk instance. The external-API steps
# (PostHog bulk-delete, Clerk Admin API delete, Mailchimp delete-permanent) are
# intentionally skipped with documented justifications.
#
# SCOPE: user_id=2, account_id=3, client_id=user_3DSdWTRYGpTkPVvKiNvoWMFgJb5
#        This script is SINGLE-USE. It hard-codes the target IDs as a safety gate.
#
# USAGE:
#   # Dry-run (default — safe, no DB writes):
#   ./scripts/ops/erase-orphaned-user.sh --dry-run
#
#   # Execute (writes to prod DB — irreversible):
#   ./scripts/ops/erase-orphaned-user.sh --execute
#
# PREREQUISITES:
#   - SSM port-forward open: local port 15432 → prod RDS :5432
#   - PGPASSWORD env var set to the mtga_admin password (from Secrets Manager)
#   - psql in PATH
#
# SKIPPED STEPS AND JUSTIFICATIONS:
#   Step 2 PostHog  — Account uses synthetic identity (user_3DSdWTRYGpTkPVvKiNvoWMFgJb5@clerk.local).
#                     No real distinct_id in PostHog. Skip is safe.
#   Step 5 Clerk    — clerk_user_id user_3DSdWTRYGpTkPVvKiNvoWMFgJb5 is from the OLD Clerk
#                     instance. Calling DELETE on the new Clerk instance returns 404. Skip is safe.
#   Step 6 Mailchimp — Email is user_3DSdWTRYGpTkPVvKiNvoWMFgJb5@clerk.local (synthetic, not a
#                     real subscriber). Skip is safe.
#
# AUTHORIZATION: Ramone Hamilton, 2026-06-11 (ticket #1237 decision comment).

set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration — hard-coded target IDs (single-use safety gate)
# ---------------------------------------------------------------------------
TARGET_USER_ID=2
TARGET_ACCOUNT_ID=3
TARGET_CLIENT_ID="user_3DSdWTRYGpTkPVvKiNvoWMFgJb5"
TARGET_CLERK_USER_ID="user_3DSdWTRYGpTkPVvKiNvoWMFgJb5"

# DB connection (via SSM port-forward)
DB_HOST="${PGHOST:-127.0.0.1}"
DB_PORT="${PGPORT:-15432}"
DB_USER="${PGUSER:-mtga_admin}"
DB_NAME="${PGDATABASE:-vaultmtg}"

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
DRY_RUN=true

for arg in "$@"; do
    case "$arg" in
        --dry-run)  DRY_RUN=true ;;
        --execute)  DRY_RUN=false ;;
        *)
            echo "Usage: $0 [--dry-run|--execute]" >&2
            exit 1
            ;;
    esac
done

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()      { echo "[erase-orphan] $*"; }
log_step() { echo; echo "[erase-orphan] ===== $* ====="; }
die()      { echo "[erase-orphan] ERROR: $*" >&2; exit 1; }

psql_exec() {
    # Execute a SQL statement. In dry-run mode we print it instead of running it.
    local sql="$1"
    local description="$2"

    if [[ "$DRY_RUN" == true ]]; then
        log "  [DRY-RUN] Would execute: $description"
        log "  [DRY-RUN] SQL: $sql"
    else
        PGPASSWORD="${PGPASSWORD:?PGPASSWORD must be set}" \
            psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
                 -c "$sql" --no-password 2>&1
    fi
}

psql_query() {
    # Execute a read-only SQL query and return result. Always runs (even in dry-run).
    local sql="$1"
    PGPASSWORD="${PGPASSWORD:?PGPASSWORD must be set}" \
        psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
             -c "$sql" --no-password -t -A 2>&1
}

# ---------------------------------------------------------------------------
# Preflight: verify DB identity
# ---------------------------------------------------------------------------
log_step "PREFLIGHT — DB identity check"

DB_CHECK=$(psql_query "SELECT current_database() || '@' || host(inet_server_addr());" 2>/dev/null || true)
log "Connected to: $DB_CHECK"

if [[ "$DB_CHECK" != "vaultmtg@"* ]]; then
    die "Unexpected database: '$DB_CHECK'. Expected vaultmtg@<prod-addr>. Aborting."
fi
log "DB identity: PASS — production vaultmtg instance confirmed"

# ---------------------------------------------------------------------------
# Preflight: verify target user exists and is the expected orphan
# ---------------------------------------------------------------------------
log_step "PREFLIGHT — target user identity verification"

USER_ROW=$(psql_query "SELECT id || '|' || clerk_user_id FROM users WHERE id = $TARGET_USER_ID;")
log "users row: $USER_ROW"

if [[ -z "$USER_ROW" ]]; then
    die "User id=$TARGET_USER_ID not found. Nothing to erase."
fi

ACTUAL_CLERK_ID=$(echo "$USER_ROW" | cut -d'|' -f2)
if [[ "$ACTUAL_CLERK_ID" != "$TARGET_CLERK_USER_ID" ]]; then
    die "clerk_user_id mismatch: expected '$TARGET_CLERK_USER_ID', got '$ACTUAL_CLERK_ID'. Aborting — wrong user."
fi
log "User identity: PASS — id=$TARGET_USER_ID, clerk_user_id=$TARGET_CLERK_USER_ID"

# Verify account
ACCOUNT_ROW=$(psql_query "SELECT id || '|' || user_id || '|' || client_id FROM accounts WHERE id = $TARGET_ACCOUNT_ID AND user_id = $TARGET_USER_ID;")
log "accounts row: $ACCOUNT_ROW"

ACTUAL_CLIENT_ID=$(echo "$ACCOUNT_ROW" | cut -d'|' -f3)
if [[ "$ACTUAL_CLIENT_ID" != "$TARGET_CLIENT_ID" ]]; then
    die "account client_id mismatch: expected '$TARGET_CLIENT_ID', got '$ACTUAL_CLIENT_ID'. Aborting."
fi
log "Account identity: PASS — account_id=$TARGET_ACCOUNT_ID, client_id=$TARGET_CLIENT_ID"

# ---------------------------------------------------------------------------
# BEFORE snapshot
# ---------------------------------------------------------------------------
log_step "BEFORE snapshot"

DE_COUNT=$(psql_query "SELECT count(*) FROM daemon_events WHERE account_id = '$TARGET_CLIENT_ID';")
DAK_COUNT=$(psql_query "SELECT count(*) FROM daemon_api_keys WHERE account_id = '$TARGET_CLIENT_ID';")
DAK_ACTIVE=$(psql_query "SELECT count(*) FROM daemon_api_keys WHERE account_id = '$TARGET_CLIENT_ID' AND revoked_at IS NULL;")
PP_COUNT=$(psql_query "SELECT count(*) FROM user_play_patterns WHERE account_id = '$TARGET_CLIENT_ID';")
PE_COUNT=$(psql_query "SELECT count(*) FROM projection_errors WHERE account_id = '$TARGET_CLIENT_ID';")
CL_COUNT=$(psql_query "SELECT count(*) FROM consent_log WHERE account_id = $TARGET_ACCOUNT_ID;")
U_COUNT=$(psql_query "SELECT count(*) FROM users WHERE id = $TARGET_USER_ID;")
A_COUNT=$(psql_query "SELECT count(*) FROM accounts WHERE id = $TARGET_ACCOUNT_ID;")

log "  users (id=2):                    $U_COUNT"
log "  accounts (account_id=3):         $A_COUNT"
log "  daemon_events (text-keyed):      $DE_COUNT"
log "  daemon_api_keys (text-keyed):    $DAK_COUNT (active: $DAK_ACTIVE)"
log "  user_play_patterns (text-keyed): $PP_COUNT"
log "  projection_errors (text-keyed):  $PE_COUNT"
log "  consent_log (FK-keyed):          $CL_COUNT"

# Guardrail: if daemon_events > 200000, stop — unexpectedly large scope
if [[ "$DE_COUNT" -gt 200000 ]]; then
    die "daemon_events count ($DE_COUNT) exceeds safety threshold. Aborting."
fi
log "Scope guardrail: PASS — $DE_COUNT daemon_events within expected range"

# Guardrail: confirm only one distinct account in daemon_events
DISTINCT_ACCTS=$(psql_query "SELECT count(DISTINCT account_id) FROM daemon_events;")
if [[ "$DISTINCT_ACCTS" -ne 1 ]]; then
    die "daemon_events has $DISTINCT_ACCTS distinct account_ids (expected 1). Cross-tenant risk. Aborting."
fi
log "Cross-tenant guardrail: PASS — single account_id in daemon_events"

# ---------------------------------------------------------------------------
# Mode confirmation
# ---------------------------------------------------------------------------
if [[ "$DRY_RUN" == true ]]; then
    log
    log "MODE: DRY-RUN — no DB writes will occur."
    log "Run with --execute to perform the actual erasure."
    log
else
    log
    log "MODE: EXECUTE — irreversible production DB writes will occur."
    log "Target: user_id=$TARGET_USER_ID, account_id=$TARGET_ACCOUNT_ID"
    log "Authorization: Ramone Hamilton, 2026-06-11, #1237"
    log
fi

# ---------------------------------------------------------------------------
# Step 0: Pre-job data already captured above (email from users.email,
#         client_ids from accounts.client_id). Script uses hard-coded values
#         as a second safety gate.
# ---------------------------------------------------------------------------
log_step "Step 0 — Pre-job data (captured)"
log "  email:      (synthetic — not logged)"
log "  client_ids: [$TARGET_CLIENT_ID]"
log "Step 0: PASS"

# ---------------------------------------------------------------------------
# Step 1: Soft-delete gate — SET users.deleted_at = NOW()
# ---------------------------------------------------------------------------
log_step "Step 1 — Soft-delete user (blocks new ingest writes)"
psql_exec \
    "UPDATE users SET deleted_at = NOW() WHERE id = $TARGET_USER_ID AND deleted_at IS NULL;" \
    "soft-delete user id=$TARGET_USER_ID"
log "Step 1: PASS"

# ---------------------------------------------------------------------------
# Step 2: PostHog bulk-delete — SKIPPED
# ---------------------------------------------------------------------------
log_step "Step 2 — PostHog bulk-delete [SKIPPED]"
log "  Reason: synthetic clerk.local identity — no PostHog person record exists."
log "Step 2: SKIP (safe)"

# ---------------------------------------------------------------------------
# Step 3: ML data — no-op (anonymous per ADR-058 Option A)
# ---------------------------------------------------------------------------
log_step "Step 3 — ML data [NO-OP per ADR-058]"
log "Step 3: SKIP (by design)"

# ---------------------------------------------------------------------------
# Step 4a: Revoke daemon API keys + delete TEXT-keyed rows
#
# We revoke active daemon_api_keys first (sets revoked_at = NOW()) before
# deleting them entirely as part of the broader text-keyed delete pass.
# This matches the 'daemons revoke' intent from AC2 while keeping the
# cascade correct (revoked_at set → then hard-deleted by Step 4a).
# ---------------------------------------------------------------------------
log_step "Step 4a — Revoke active daemon API keys (AC2) then delete TEXT-keyed tables"

psql_exec \
    "UPDATE daemon_api_keys SET revoked_at = NOW(), updated_at = NOW() WHERE account_id = '$TARGET_CLIENT_ID' AND revoked_at IS NULL;" \
    "revoke active daemon_api_keys for client_id=$TARGET_CLIENT_ID"
log "  Daemon API key revocation (AC2): PASS"

psql_exec \
    "DELETE FROM daemon_events WHERE account_id = '$TARGET_CLIENT_ID';" \
    "delete daemon_events for client_id=$TARGET_CLIENT_ID"
log "  daemon_events delete: PASS"

psql_exec \
    "DELETE FROM daemon_api_keys WHERE account_id = '$TARGET_CLIENT_ID';" \
    "delete daemon_api_keys for client_id=$TARGET_CLIENT_ID"
log "  daemon_api_keys delete: PASS"

psql_exec \
    "DELETE FROM user_play_patterns WHERE account_id = '$TARGET_CLIENT_ID';" \
    "delete user_play_patterns for client_id=$TARGET_CLIENT_ID"
log "  user_play_patterns delete: PASS"

psql_exec \
    "DELETE FROM projection_errors WHERE account_id = '$TARGET_CLIENT_ID';" \
    "delete projection_errors for client_id=$TARGET_CLIENT_ID"
log "  projection_errors delete: PASS"

log "Step 4a: PASS"

# ---------------------------------------------------------------------------
# Step 4b: Anonymize consent_log in-place (null ip_address_hash + metadata)
# ---------------------------------------------------------------------------
log_step "Step 4b — Anonymize consent_log"
psql_exec \
    "UPDATE consent_log SET ip_address_hash = NULL, metadata = NULL WHERE account_id = $TARGET_ACCOUNT_ID;" \
    "anonymize consent_log for account_id=$TARGET_ACCOUNT_ID"
log "Step 4b: PASS"

# ---------------------------------------------------------------------------
# Step 4c: Delete waitlist_entries by email (CITEXT match)
# ---------------------------------------------------------------------------
log_step "Step 4c — Delete waitlist_entries"
# The email is synthetic (clerk.local) — no waitlist entry expected, but run for completeness.
psql_exec \
    "DELETE FROM waitlist_entries WHERE email = (SELECT email FROM users WHERE id = $TARGET_USER_ID);" \
    "delete waitlist_entries for user_id=$TARGET_USER_ID email"
log "Step 4c: PASS"

# ---------------------------------------------------------------------------
# Step 4d: Hard-delete users row (cascades to api_keys via users.id FK)
# ---------------------------------------------------------------------------
log_step "Step 4d — Hard-delete users row (cascades to api_keys)"
psql_exec \
    "DELETE FROM users WHERE id = $TARGET_USER_ID;" \
    "hard-delete users id=$TARGET_USER_ID"
log "Step 4d: PASS"

# ---------------------------------------------------------------------------
# Step 4e: Hard-delete accounts row (fires ON DELETE CASCADE on 25+ tables)
# ---------------------------------------------------------------------------
log_step "Step 4e — Hard-delete accounts row (ON DELETE CASCADE on all FK user-keyed tables)"
psql_exec \
    "DELETE FROM accounts WHERE id = $TARGET_ACCOUNT_ID;" \
    "hard-delete accounts id=$TARGET_ACCOUNT_ID"
log "Step 4e: PASS"

# ---------------------------------------------------------------------------
# Step 4-explicit: Defense-in-depth explicit DELETE of BIGINT-account_id tables
# that may be cascade-unreachable at certain schema versions (#1257).
#
# Covers:
#   matches, player_stats, rank_history, collection_history
#     — FKs added in 000119; absent on pre-119 DBs (P1 incident class).
#   game_plays
#     — account_id TEXT→BIGINT (000120) but NO FK CASCADE today.
#   inventory_history
#     — PROD incremental path (000068) has TEXT account_id; handled above in
#       Step 4a TEXT-keyed deletes. The fresh-init (000054) path has BIGINT FK
#       CASCADE and does not apply to this prod script (see job.go F1 gate).
#       No explicit BIGINT delete needed here for the prod-incremental assumption.
#
# These are idempotent no-ops if the accounts FK cascade already fired.
# ---------------------------------------------------------------------------
log_step "Step 4-explicit — Defense-in-depth explicit BIGINT-keyed deletes"

psql_exec \
    "DELETE FROM matches WHERE account_id = $TARGET_ACCOUNT_ID;" \
    "delete matches for account_id=$TARGET_ACCOUNT_ID"
log "  matches delete: PASS"

psql_exec \
    "DELETE FROM player_stats WHERE account_id = $TARGET_ACCOUNT_ID;" \
    "delete player_stats for account_id=$TARGET_ACCOUNT_ID"
log "  player_stats delete: PASS"

psql_exec \
    "DELETE FROM rank_history WHERE account_id = $TARGET_ACCOUNT_ID;" \
    "delete rank_history for account_id=$TARGET_ACCOUNT_ID"
log "  rank_history delete: PASS"

psql_exec \
    "DELETE FROM collection_history WHERE account_id = $TARGET_ACCOUNT_ID;" \
    "delete collection_history for account_id=$TARGET_ACCOUNT_ID"
log "  collection_history delete: PASS"

psql_exec \
    "DELETE FROM game_plays WHERE account_id = $TARGET_ACCOUNT_ID;" \
    "delete game_plays for account_id=$TARGET_ACCOUNT_ID"
log "  game_plays delete: PASS"

log "Step 4-explicit: PASS"

# ---------------------------------------------------------------------------
# Step 4-sweep: Residual sweep — assert zero rows remain for this account.
#
# Queries information_schema for every public base table with account_id or
# user_id, then COUNTs rows for the erased account/client_ids.  Retained
# tables (deletion_audit_log, restriction_audit_log, dsr_access_log,
# rectification_audit_log) are excluded per Ray's retention ruling (#1257).
#
# On failure: lists ALL offending tables, sets FAIL=1 before Step 5 runs.
# Step 5 (Clerk, skipped for this orphan) and Step 8 must NOT run if the
# sweep fails — completed_at stays NULL for AC7 re-trigger.
# ---------------------------------------------------------------------------
log_step "Step 4-sweep — Residual sweep (assert zero rows remain)"

SWEEP_FAIL=0

check_bigint_count() {
    local table="$1"
    local col="${2:-account_id}"
    local count
    count=$(psql_query "SELECT count(*) FROM \"$table\" WHERE \"$col\" = $TARGET_ACCOUNT_ID;" 2>/dev/null || echo "ERROR")
    if [[ "$count" == "ERROR" ]]; then
        log "  WARN: could not query $table.$col (table may not exist — skipping)"
    elif [[ "$count" -gt 0 ]]; then
        log "  FAIL: $table.$col has $count residual row(s) for account_id=$TARGET_ACCOUNT_ID"
        SWEEP_FAIL=1
    else
        log "  OK: $table.$col = 0"
    fi
}

check_text_count() {
    local table="$1"
    local col="${2:-account_id}"
    local count
    count=$(psql_query "SELECT count(*) FROM \"$table\" WHERE \"$col\" = '$TARGET_CLIENT_ID';" 2>/dev/null || echo "ERROR")
    if [[ "$count" == "ERROR" ]]; then
        log "  WARN: could not query $table.$col (table may not exist — skipping)"
    elif [[ "$count" -gt 0 ]]; then
        log "  FAIL: $table.$col has $count residual row(s) for client_id=$TARGET_CLIENT_ID"
        SWEEP_FAIL=1
    else
        log "  OK: $table.$col = 0"
    fi
}

# BIGINT-keyed tables (account_id = BIGINT)
check_bigint_count "accounts"
check_bigint_count "matches"
check_bigint_count "player_stats"
check_bigint_count "rank_history"
check_bigint_count "collection_history"
check_bigint_count "collection"
check_bigint_count "collection_new"
check_bigint_count "decks"
check_bigint_count "draft_sessions"
check_bigint_count "inventory"
check_bigint_count "quests"
check_bigint_count "user_settings"
check_bigint_count "recommendation_feedback"
check_bigint_count "card_inventory"
check_bigint_count "game_plays"
check_bigint_count "draft_picks"
check_bigint_count "draft_packs"
check_bigint_count "draft_match_results"
check_bigint_count "game_event_counters"
check_bigint_count "life_change_tracking"
check_bigint_count "matchup_statistics"
check_bigint_count "deck_performance_history"
check_bigint_count "currency_history"
check_bigint_count "match_game_results"
check_bigint_count "quest_session_tracking"
# PROD incremental path: inventory_history.account_id is TEXT (000068).
# No BIGINT sweep needed here. See job.go F1 gate for the fresh-init path.
# TEXT-keyed tables (account_id = TEXT / MTGA client_id)
check_text_count "daemon_events"
check_text_count "daemon_api_keys"
check_text_count "user_play_patterns"
check_text_count "projection_errors"
check_text_count "inventory_history"
# user_id-keyed check
check_bigint_count "users" "id"
# Retained tables (deletion_audit_log, restriction_audit_log, dsr_access_log,
# rectification_audit_log) are intentionally excluded from the sweep per Ray's
# retention ruling (#1257).

if [[ "$SWEEP_FAIL" -eq 0 ]]; then
    log "Step 4-sweep: PASS — zero residuals confirmed"
else
    die "Step 4-sweep: FAIL — residual rows found. Halting before Step 5/8. Inspect DB state."
fi

# ---------------------------------------------------------------------------
# Step 5: Clerk Admin API delete — SKIPPED
# ---------------------------------------------------------------------------
log_step "Step 5 — Clerk Admin API delete [SKIPPED]"
log "  Reason: clerk_user_id '$TARGET_CLERK_USER_ID' is from the OLD Clerk instance."
log "  The new Clerk instance has no record of this user — DELETE would 404."
log "  The old instance user is already orphaned and has no active session."
log "Step 5: SKIP (safe)"

# ---------------------------------------------------------------------------
# Step 6: Mailchimp delete-permanent — SKIPPED
# ---------------------------------------------------------------------------
log_step "Step 6 — Mailchimp delete-permanent [SKIPPED]"
log "  Reason: email is synthetic (clerk.local domain) — not a real Mailchimp subscriber."
log "Step 6: SKIP (safe)"

# ---------------------------------------------------------------------------
# Step 7: Cache invalidation — no-op (no Redis)
# ---------------------------------------------------------------------------
log_step "Step 7 — Cache invalidation [NO-OP]"
log "Step 7: SKIP (by design)"

# ---------------------------------------------------------------------------
# Step 8: Mark deletion_audit_log.completed_at — handled by comment on #1237
# ---------------------------------------------------------------------------
log_step "Step 8 — deletion_audit_log [NO audit row for this orphan erasure]"
log "  No deletion_audit_log row exists for this orphan (the cascade was not"
log "  triggered via DELETE /api/v1/account). The ops outcome is documented in"
log "  the GitHub issue comment on #1237."
log "Step 8: SKIP (no audit row)"

# ---------------------------------------------------------------------------
# AFTER snapshot (only in execute mode — in dry-run the counts are unchanged)
# ---------------------------------------------------------------------------
log_step "AFTER snapshot"

if [[ "$DRY_RUN" == true ]]; then
    log "  [DRY-RUN] After-counts not checked (no writes occurred)"
    log "  Run with --execute to see post-erasure counts"
else
    DE_AFTER=$(psql_query "SELECT count(*) FROM daemon_events WHERE account_id = '$TARGET_CLIENT_ID';")
    DAK_AFTER=$(psql_query "SELECT count(*) FROM daemon_api_keys WHERE account_id = '$TARGET_CLIENT_ID';")
    PP_AFTER=$(psql_query "SELECT count(*) FROM user_play_patterns WHERE account_id = '$TARGET_CLIENT_ID';")
    PE_AFTER=$(psql_query "SELECT count(*) FROM projection_errors WHERE account_id = '$TARGET_CLIENT_ID';")
    # inventory_history: prod incremental path (000068) has TEXT account_id.
    # This check uses the TEXT/client_id path. The BIGINT path (fresh-init) is
    # handled by job.go DeleteExplicitBigintRows — not applicable to this prod script.
    IH_AFTER=$(psql_query "SELECT count(*) FROM inventory_history WHERE account_id = '$TARGET_CLIENT_ID';" 2>/dev/null || echo "0")
    # Explicit-BIGINT tables (#1257)
    MATCHES_AFTER=$(psql_query "SELECT count(*) FROM matches WHERE account_id = $TARGET_ACCOUNT_ID;")
    PS_AFTER=$(psql_query "SELECT count(*) FROM player_stats WHERE account_id = $TARGET_ACCOUNT_ID;")
    RH_AFTER=$(psql_query "SELECT count(*) FROM rank_history WHERE account_id = $TARGET_ACCOUNT_ID;")
    CH_AFTER=$(psql_query "SELECT count(*) FROM collection_history WHERE account_id = $TARGET_ACCOUNT_ID;")
    GP_AFTER=$(psql_query "SELECT count(*) FROM game_plays WHERE account_id = $TARGET_ACCOUNT_ID;")
    U_AFTER=$(psql_query "SELECT count(*) FROM users WHERE id = $TARGET_USER_ID;")
    A_AFTER=$(psql_query "SELECT count(*) FROM accounts WHERE id = $TARGET_ACCOUNT_ID;")

    log "  users (id=2):                    $U_AFTER  (was: $U_COUNT)"
    log "  accounts (account_id=3):         $A_AFTER  (was: $A_COUNT)"
    log "  daemon_events (text-keyed):      $DE_AFTER (was: $DE_COUNT)"
    log "  daemon_api_keys (text-keyed):    $DAK_AFTER (was: $DAK_COUNT)"
    log "  user_play_patterns (text-keyed): $PP_AFTER (was: $PP_COUNT)"
    log "  projection_errors (text-keyed):  $PE_AFTER (was: $PE_COUNT)"
    log "  inventory_history (text-keyed):  $IH_AFTER"
    log "  matches (bigint explicit):       $MATCHES_AFTER"
    log "  player_stats (bigint explicit):  $PS_AFTER"
    log "  rank_history (bigint explicit):  $RH_AFTER"
    log "  collection_history (bigint):     $CH_AFTER"
    log "  game_plays (bigint, no FK):      $GP_AFTER"

    # Assertions — check all, collect failures, then die once.
    FAIL=0
    [[ "$DE_AFTER" -eq 0 ]]       || { log "FAIL: daemon_events not 0 after erasure (got $DE_AFTER)"; FAIL=1; }
    [[ "$DAK_AFTER" -eq 0 ]]      || { log "FAIL: daemon_api_keys not 0 after erasure (got $DAK_AFTER)"; FAIL=1; }
    [[ "$PP_AFTER" -eq 0 ]]       || { log "FAIL: user_play_patterns not 0 after erasure (got $PP_AFTER)"; FAIL=1; }
    [[ "$PE_AFTER" -eq 0 ]]       || { log "FAIL: projection_errors not 0 after erasure (got $PE_AFTER)"; FAIL=1; }
    [[ "$IH_AFTER" -eq 0 ]]       || { log "FAIL: inventory_history not 0 after erasure (got $IH_AFTER)"; FAIL=1; }
    [[ "$MATCHES_AFTER" -eq 0 ]]  || { log "FAIL: matches not 0 after erasure (got $MATCHES_AFTER)"; FAIL=1; }
    [[ "$PS_AFTER" -eq 0 ]]       || { log "FAIL: player_stats not 0 after erasure (got $PS_AFTER)"; FAIL=1; }
    [[ "$RH_AFTER" -eq 0 ]]       || { log "FAIL: rank_history not 0 after erasure (got $RH_AFTER)"; FAIL=1; }
    [[ "$CH_AFTER" -eq 0 ]]       || { log "FAIL: collection_history not 0 after erasure (got $CH_AFTER)"; FAIL=1; }
    [[ "$GP_AFTER" -eq 0 ]]       || { log "FAIL: game_plays not 0 after erasure (got $GP_AFTER)"; FAIL=1; }
    [[ "$U_AFTER"  -eq 0 ]]       || { log "FAIL: users row not deleted (got $U_AFTER)"; FAIL=1; }
    [[ "$A_AFTER"  -eq 0 ]]       || { log "FAIL: accounts row not deleted (got $A_AFTER)"; FAIL=1; }

    if [[ "$FAIL" -eq 0 ]]; then
        log "All AFTER assertions: PASS"
    else
        die "One or more AFTER assertions failed — inspect DB state."
    fi
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
log_step "SUMMARY"
if [[ "$DRY_RUN" == true ]]; then
    log "DRY-RUN complete — no DB writes occurred."
    log "All preflight checks PASSED."
    log "Re-run with --execute to apply the erasure."
else
    log "EXECUTE complete — orphaned user-2 data erased."
    log "Document outcome in #1237 comment."
fi
