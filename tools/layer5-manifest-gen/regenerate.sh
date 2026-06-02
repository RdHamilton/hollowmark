#!/usr/bin/env bash
# tools/layer5-manifest-gen/regenerate.sh
#
# Layer 5 manifest regeneration tool (ADR-052).
#
# Usage:
#   ./tools/layer5-manifest-gen/regenerate.sh
#
# This script calls the local BFF read endpoints (which must be seeded with
# test data via test-data.sql) and extracts the assertion-relevant fields,
# then writes the layer5-expected/*.json manifest files.
#
# Prerequisites:
#   - BFF running at $BFF_URL (default: http://localhost:8080)
#   - Database seeded with: psql $DATABASE_URL < services/daemon/testdata/corpus/fixtures/test-data.sql
#   - BFF_TOKEN: a valid test auth token (set via environment or .env.test)
#
# This script is part of the ADR-041 G3 corpus-refresh protocol.
# Run it in the same PR as any corpus refresh. The manifest diff shows
# exactly what the MTGA patch changed.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
MANIFEST_DIR="${REPO_ROOT}/services/daemon/testdata/corpus/layer5-expected"
CORPUS_MTGA_VERSION="$(cat "${REPO_ROOT}/services/daemon/testdata/corpus/mtga-version.txt" 2>/dev/null || echo "unknown")"

BFF_URL="${BFF_URL:-http://localhost:8080}"
BFF_TOKEN="${BFF_TOKEN:-}"

if [[ -z "${BFF_TOKEN}" ]]; then
  echo "[layer5-manifest-gen] ERROR: BFF_TOKEN is not set." >&2
  echo "  Set it to a valid test Clerk token: export BFF_TOKEN=<token>" >&2
  echo "  In CI this is derived from the CLERK_BACKEND_API_URL sign-in flow." >&2
  exit 1
fi

echo "[layer5-manifest-gen] BFF_URL=${BFF_URL}"
echo "[layer5-manifest-gen] Manifest dir: ${MANIFEST_DIR}"
echo "[layer5-manifest-gen] MTGA version: ${CORPUS_MTGA_VERSION}"

# ── helpers ──────────────────────────────────────────────────────────────────

bff_get() {
  local path="$1"
  curl -sf \
    -H "Authorization: Bearer ${BFF_TOKEN}" \
    -H "Accept: application/json" \
    "${BFF_URL}${path}"
}

bff_post() {
  local path="$1"
  local body="$2"
  curl -sf \
    -X POST \
    -H "Authorization: Bearer ${BFF_TOKEN}" \
    -H "Content-Type: application/json" \
    -H "Accept: application/json" \
    -d "${body}" \
    "${BFF_URL}${path}"
}

require_jq() {
  if ! command -v jq &>/dev/null; then
    echo "[layer5-manifest-gen] ERROR: jq is required but not found. Install it first." >&2
    exit 1
  fi
}

require_jq

# ── 1. match-list ─────────────────────────────────────────────────────────────

echo "[layer5-manifest-gen] Fetching match-list (/api/v1/history/matches)..."
MATCH_RESP=$(bff_get "/api/v1/history/matches?limit=1" 2>/dev/null || echo "FETCH_ERROR")

if [[ "${MATCH_RESP}" == "FETCH_ERROR" ]]; then
  echo "[layer5-manifest-gen] WARNING: /api/v1/history/matches unreachable — skipping match-list update" >&2
else
  # Extract the first row from cursor-paginated response
  FIRST_FORMAT=$(echo "${MATCH_RESP}" | jq -r '.data[0].format // empty' 2>/dev/null || echo "")
  FIRST_RESULT=$(echo "${MATCH_RESP}" | jq -r '.data[0].result // empty' 2>/dev/null || echo "")
  FIRST_PW=$(echo "${MATCH_RESP}" | jq -r '.data[0].player_wins // 0' 2>/dev/null || echo "0")
  FIRST_OW=$(echo "${MATCH_RESP}" | jq -r '.data[0].opponent_wins // 0' 2>/dev/null || echo "0")

  cat > "${MANIFEST_DIR}/match-list.json" <<EOF
{
  "corpus_mtga_version": "${CORPUS_MTGA_VERSION}",
  "corpus_provenance": "REAL-DERIVED",
  "expected_empty": false,
  "generated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "assertions": [
    {
      "surface": "match-list",
      "bff_endpoint": "/api/v1/history/matches",
      "notes": "BFF returns cursor-paginated shape: { data: [...], has_more, limit }. The SPA reads data.data (not data.matches). A field rename that breaks this shape would produce an empty table.",
      "fields": {
        "response_shape": "cursor_paginated",
        "data_key": "data",
        "min_row_count": 1,
        "first_row": {
          "format": "${FIRST_FORMAT}",
          "result": "${FIRST_RESULT}",
          "player_wins": ${FIRST_PW},
          "opponent_wins": ${FIRST_OW}
        }
      }
    }
  ]
}
EOF
  echo "[layer5-manifest-gen] match-list.json updated (format=${FIRST_FORMAT}, result=${FIRST_RESULT})"
fi

# ── 2. quest-list ─────────────────────────────────────────────────────────────

echo "[layer5-manifest-gen] Fetching quest-list (/api/v1/quests/active)..."
QUEST_RESP=$(bff_get "/api/v1/quests/active" 2>/dev/null || echo "FETCH_ERROR")

if [[ "${QUEST_RESP}" == "FETCH_ERROR" ]]; then
  echo "[layer5-manifest-gen] WARNING: /api/v1/quests/active unreachable — skipping quest-list update" >&2
else
  # Check that first_seen_at is present (not assigned_at)
  HAS_FIRST_SEEN=$(echo "${QUEST_RESP}" | jq 'if .data then .data.quests[0] else .quests[0] end | has("first_seen_at")' 2>/dev/null || echo "false")
  HAS_ASSIGNED=$(echo "${QUEST_RESP}" | jq 'if .data then .data.quests[0] else .quests[0] end | has("assigned_at")' 2>/dev/null || echo "false")
  QUEST_COUNT=$(echo "${QUEST_RESP}" | jq 'if .data then (.data.quests // []) else (.quests // []) end | length' 2>/dev/null || echo "0")

  cat > "${MANIFEST_DIR}/quest-list.json" <<EOF
{
  "corpus_mtga_version": "${CORPUS_MTGA_VERSION}",
  "corpus_provenance": "REAL-DERIVED",
  "expected_empty": false,
  "generated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "assertions": [
    {
      "surface": "quest-list",
      "bff_endpoint": "/api/v1/quests/active",
      "notes": "The BFF emits first_seen_at (not assigned_at). The SPA must read first_seen_at and render a valid date. A field rename that produces 'Invalid Date' would fail data-testid='quest-date' assertion.",
      "fields": {
        "date_field_name": "first_seen_at",
        "forbidden_field_name": "assigned_at",
        "first_seen_at_present_in_wire": ${HAS_FIRST_SEEN},
        "assigned_at_absent_from_wire": $([ "${HAS_ASSIGNED}" = "false" ] && echo "true" || echo "false"),
        "min_quest_count": ${QUEST_COUNT},
        "rendered_date_must_not_be": "Invalid Date"
      }
    }
  ]
}
EOF
  echo "[layer5-manifest-gen] quest-list.json updated (quest_count=${QUEST_COUNT}, first_seen_at=${HAS_FIRST_SEEN})"
fi

# ── 3. win-rate-trend ─────────────────────────────────────────────────────────

echo "[layer5-manifest-gen] Fetching win-rate-trend (/api/v1/matches/trends)..."
NOW=$(date -u +%Y-%m-%d)
YEAR_AGO=$(date -u -v-1y +%Y-%m-%d 2>/dev/null || date -u -d "1 year ago" +%Y-%m-%d 2>/dev/null || echo "2025-06-01")
TREND_BODY="{\"startDate\":\"${YEAR_AGO}\",\"endDate\":\"${NOW}\",\"periodType\":\"week\"}"
TREND_RESP=$(bff_post "/api/v1/matches/trends" "${TREND_BODY}" 2>/dev/null || echo "FETCH_ERROR")

if [[ "${TREND_RESP}" == "FETCH_ERROR" ]]; then
  echo "[layer5-manifest-gen] WARNING: /api/v1/matches/trends unreachable — skipping win-rate-trend update" >&2
else
  # Verify key is 'Trends' not 'Periods'
  HAS_TRENDS=$(echo "${TREND_RESP}" | jq 'if .data then .data else . end | has("Trends")' 2>/dev/null || echo "false")
  HAS_PERIODS=$(echo "${TREND_RESP}" | jq 'if .data then .data else . end | has("Periods")' 2>/dev/null || echo "false")
  PERIOD_COUNT=$(echo "${TREND_RESP}" | jq 'if .data then (.data.Trends // []) else (.Trends // []) end | length' 2>/dev/null || echo "0")
  CHART_RENDERS=$([ "${PERIOD_COUNT}" -gt 0 ] && echo "true" || echo "false")

  cat > "${MANIFEST_DIR}/win-rate-trend.json" <<EOF
{
  "corpus_mtga_version": "${CORPUS_MTGA_VERSION}",
  "corpus_provenance": "SYNTHETIC",
  "expected_empty": false,
  "generated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "assertions": [
    {
      "surface": "win-rate-trend",
      "bff_endpoint": "/api/v1/matches/trends",
      "notes": "BFF emits json:'Trends' (capital T). The SPA TrendAnalysis class reads source['Trends']. Before the fix the SPA read source['Periods'] which is absent — chart was always empty. A Trends/Periods key mismatch would render win-rate-trend-empty.",
      "fields": {
        "response_key": "Trends",
        "forbidden_response_key": "Periods",
        "trends_key_present_in_wire": ${HAS_TRENDS},
        "periods_key_absent_from_wire": $([ "${HAS_PERIODS}" = "false" ] && echo "true" || echo "false"),
        "chart_must_render": ${CHART_RENDERS},
        "empty_state_must_not_render": ${CHART_RENDERS},
        "min_period_count": ${PERIOD_COUNT}
      }
    }
  ]
}
EOF
  echo "[layer5-manifest-gen] win-rate-trend.json updated (period_count=${PERIOD_COUNT}, has_Trends=${HAS_TRENDS})"
fi

# ── 4. rank-progression ───────────────────────────────────────────────────────

echo "[layer5-manifest-gen] Fetching rank-progression (/api/v1/matches/rank-progression-timeline)..."
RANK_RESP=$(bff_get "/api/v1/matches/rank-progression-timeline?format=constructed&limit=10" 2>/dev/null || echo "FETCH_ERROR")

if [[ "${RANK_RESP}" == "FETCH_ERROR" ]]; then
  echo "[layer5-manifest-gen] WARNING: /api/v1/matches/rank-progression-timeline unreachable — skipping rank-progression update" >&2
else
  ENTRY_COUNT=$(echo "${RANK_RESP}" | jq 'if .data then (.data.entries // []) else (.entries // []) end | length' 2>/dev/null || echo "0")
  FIRST_RANK=$(echo "${RANK_RESP}" | jq -r 'if .data then .data.entries[0].rank else .entries[0].rank end // empty' 2>/dev/null || echo "")
  # Verify rank_class is NOT in wire format (it should not be — SPA derives it client-side)
  HAS_RANK_CLASS=$(echo "${RANK_RESP}" | jq 'if .data then (.data.entries[0] // {}) else (.entries[0] // {}) end | has("rank_class")' 2>/dev/null || echo "false")
  CHART_RENDERS=$([ "${ENTRY_COUNT}" -gt 0 ] && echo "true" || echo "false")

  cat > "${MANIFEST_DIR}/rank-progression.json" <<EOF
{
  "corpus_mtga_version": "${CORPUS_MTGA_VERSION}",
  "corpus_provenance": "SYNTHETIC",
  "expected_empty": false,
  "generated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "assertions": [
    {
      "surface": "rank-progression",
      "bff_endpoint": "/api/v1/matches/rank-progression-timeline",
      "notes": "BFF emits { occurred_at, rank, result, match_id } per entry. rank_class and rank_level are NOT on the wire. The SPA uses parseRankString() to derive them client-side. Before the fix the SPA used undefined values, producing a flat zero chart.",
      "fields": {
        "wire_fields_present": ["occurred_at", "rank", "result", "match_id"],
        "rank_class_absent_from_wire": $([ "${HAS_RANK_CLASS}" = "false" ] && echo "true" || echo "false"),
        "chart_must_render": ${CHART_RENDERS},
        "empty_state_must_not_render": ${CHART_RENDERS},
        "chart_must_be_non_flat": ${CHART_RENDERS},
        "min_entry_count": ${ENTRY_COUNT},
        "sample_first_rank": "${FIRST_RANK}"
      }
    }
  ]
}
EOF
  echo "[layer5-manifest-gen] rank-progression.json updated (entry_count=${ENTRY_COUNT}, has_rank_class=${HAS_RANK_CLASS})"
fi

# ── 5. deck-builder-resolution ────────────────────────────────────────────────

echo "[layer5-manifest-gen] Fetching deck list (/api/v1/decks)..."
DECK_RESP=$(bff_get "/api/v1/decks" 2>/dev/null || echo "FETCH_ERROR")

if [[ "${DECK_RESP}" == "FETCH_ERROR" ]]; then
  echo "[layer5-manifest-gen] WARNING: /api/v1/decks unreachable — skipping deck-builder-resolution update" >&2
else
  FIRST_DECK_ID=$(echo "${DECK_RESP}" | jq -r 'if .data then .data[0].id else .[0].id end // empty' 2>/dev/null || echo "")
  FIRST_DECK_FORMAT=$(echo "${DECK_RESP}" | jq -r 'if .data then .data[0].format else .[0].format end // empty' 2>/dev/null || echo "")

  cat > "${MANIFEST_DIR}/deck-builder-resolution.json" <<EOF
{
  "corpus_mtga_version": "${CORPUS_MTGA_VERSION}",
  "corpus_provenance": "REAL-DERIVED",
  "expected_empty": false,
  "generated_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "assertions": [
    {
      "surface": "deck-builder-resolution",
      "bff_endpoint": "/api/v1/cards?grp_ids=...",
      "notes": "When the card catalog is populated, DeckList resolves card names from metadata. When the catalog is empty, getCardName() falls back to 'Unknown Card {id}'. This assertion catches the empty-catalog regression.",
      "fields": {
        "unknown_card_element_count_must_be": 0,
        "seeded_deck_id": "${FIRST_DECK_ID}",
        "seeded_deck_format": "${FIRST_DECK_FORMAT}"
      }
    }
  ]
}
EOF
  echo "[layer5-manifest-gen] deck-builder-resolution.json updated (deck_id=${FIRST_DECK_ID})"
fi

echo ""
echo "[layer5-manifest-gen] Done. Updated manifest files in ${MANIFEST_DIR}/"
echo "[layer5-manifest-gen] Review the diff before committing."
echo "[layer5-manifest-gen] draft-surface.json and match-detail-timeline.json are NOT auto-regenerated"
echo "[layer5-manifest-gen] (expected_empty: true — update manually when ADR-051 write paths land)."
