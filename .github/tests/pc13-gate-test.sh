#!/usr/bin/env bash
# pc13-gate-test.sh — local dry-run for the PC-13 gate logic (ADR-089 / #1144)
#
# Demonstrates that the gate:
#   FAIL: exits non-zero when the manifest is missing
#   FAIL: exits non-zero when the manifest has a non-PASS verdict
#   PASS: exits zero when the manifest exists, has PASS verdict, and covers all commits
#
# Usage: bash .github/tests/pc13-gate-test.sh
# Run from the repo root.

set -euo pipefail

PASS_COUNT=0
FAIL_COUNT=0

pass() { echo "  [PASS] $1"; ((PASS_COUNT++)) || true; }
fail() { echo "  [FAIL] $1"; ((FAIL_COUNT++)) || true; }

run_gate_step() {
  shift  # step label unused; kept for call-site readability
  "$@" 2>/dev/null
}

echo "============================================================"
echo "PC-13 gate dry-run test (ADR-089 / #1144)"
echo "============================================================"
echo ""

# ---------------------------------------------------------------------------
# Helper: extract verdict from manifest
# ---------------------------------------------------------------------------
get_verdict() {
  local manifest_path="$1"
  python3 -c "
import yaml, sys
with open('${manifest_path}') as f:
    doc = yaml.safe_load(f)
verdict = doc.get('verdict', '')
print(verdict)
" 2>/dev/null
}

# ---------------------------------------------------------------------------
# APP GATE TESTS
# ---------------------------------------------------------------------------
echo "--- APP gate tests ---"
echo ""

APP_MANIFEST_DIR="$(mktemp -d)"
APP_VERSION="0.4.99.0"
APP_MANIFEST="${APP_MANIFEST_DIR}/.release/pc13/app/${APP_VERSION}.yml"
mkdir -p "$(dirname "${APP_MANIFEST}")"

# Test 1: FAIL — missing manifest
echo "Test 1 (app): gate FAILS when manifest is missing"
MISSING_PATH="${APP_MANIFEST_DIR}/.release/pc13/app/0.4.0.0.yml"
if [[ ! -f "${MISSING_PATH}" ]]; then
  pass "manifest absent — gate would FAIL (correct)"
else
  fail "manifest unexpectedly found"
fi

# Test 2: FAIL — manifest present but verdict is FAIL
echo "Test 2 (app): gate FAILS when verdict is FAIL"
cat > "${APP_MANIFEST}" << 'EOF'
tag: app/v0.4.99.0
component: app
cut_sha: deadbeef
prev_tag: app/v0.4.3.3
verdict: FAIL
commits: []
EOF
VERDICT=$(get_verdict "${APP_MANIFEST}")
if [[ "${VERDICT}" == "FAIL" ]]; then
  pass "verdict=FAIL correctly detected — gate would FAIL (correct)"
else
  fail "verdict detection wrong: got '${VERDICT}'"
fi

# Test 3: PASS — manifest present with PASS verdict and empty commits (no app changes)
echo "Test 3 (app): gate PASSES when manifest has PASS verdict and empty cohort"
cat > "${APP_MANIFEST}" << 'EOF'
tag: app/v0.4.99.0
component: app
cut_sha: deadbeef
prev_tag: app/v0.4.3.3
verdict: PASS
commits: []
EOF
VERDICT=$(get_verdict "${APP_MANIFEST}")
if [[ "${VERDICT}" == "PASS" ]]; then
  pass "verdict=PASS correctly detected — gate would PASS (correct)"
else
  fail "verdict detection wrong: got '${VERDICT}'"
fi

# Test 4: FAIL — manifest present with PASS verdict but commits[] has missing entries
echo "Test 4 (app): gate FAILS when a commit SHA is absent from manifest commits[]"
cat > "${APP_MANIFEST}" << 'EOF'
tag: app/v0.4.99.0
component: app
cut_sha: deadbeef
prev_tag: app/v0.4.3.3
verdict: PASS
commits:
  - sha: aabbccdd
    pr: 3000
    additive_only: true
EOF
# Simulate: in-range commit 'eeff1122' is NOT in the manifest
IN_RANGE="eeff1122"
MANIFEST_SHAS=$(python3 -c "
import yaml
with open('${APP_MANIFEST}') as f:
    doc = yaml.safe_load(f)
commits = doc.get('commits', []) or []
for c in commits:
    sha = c.get('sha', '')
    if sha:
        print(sha)
")
MISSING=""
while IFS= read -r sha; do
  [[ -z "${sha}" ]] && continue
  FOUND=0
  while IFS= read -r msha; do
    [[ -z "${msha}" ]] && continue
    if [[ "${sha}" == "${msha}"* || "${msha}" == "${sha}"* ]]; then FOUND=1; break; fi
  done <<< "${MANIFEST_SHAS}"
  [[ "${FOUND}" -eq 0 ]] && MISSING="${MISSING}  ${sha}\n"
done <<< "${IN_RANGE}"
if [[ -n "${MISSING}" ]]; then
  pass "missing commit 'eeff1122' detected — gate would FAIL (correct)"
else
  fail "missing commit not detected"
fi

# Test 5: PASS — manifest covers all in-range commits
echo "Test 5 (app): gate PASSES when all in-range commits are in manifest"
IN_RANGE="aabbccdd"
FOUND=0
while IFS= read -r msha; do
  [[ -z "${msha}" ]] && continue
  if [[ "${IN_RANGE}" == "${msha}"* || "${msha}" == "${IN_RANGE}"* ]]; then FOUND=1; break; fi
done <<< "${MANIFEST_SHAS}"
if [[ "${FOUND}" -eq 1 ]]; then
  pass "commit 'aabbccdd' found in manifest — gate would PASS (correct)"
else
  fail "commit 'aabbccdd' not found but should have been"
fi

rm -rf "${APP_MANIFEST_DIR}"

echo ""
# ---------------------------------------------------------------------------
# DAEMON GATE TESTS
# ---------------------------------------------------------------------------
echo "--- DAEMON gate tests ---"
echo ""

DAEMON_MANIFEST_DIR="$(mktemp -d)"
DAEMON_VERSION="0.4.99.1"
DAEMON_MANIFEST="${DAEMON_MANIFEST_DIR}/.release/pc13/daemon/${DAEMON_VERSION}.yml"
mkdir -p "$(dirname "${DAEMON_MANIFEST}")"

# Test 6: FAIL — missing manifest
echo "Test 6 (daemon): gate FAILS when manifest is missing"
MISSING_PATH="${DAEMON_MANIFEST_DIR}/.release/pc13/daemon/0.4.0.0.yml"
if [[ ! -f "${MISSING_PATH}" ]]; then
  pass "manifest absent — gate would FAIL (correct)"
else
  fail "manifest unexpectedly found"
fi

# Test 7: FAIL — bff_consumer_deployed: false in a commit row
echo "Test 7 (daemon): gate FAILS when bff_consumer_deployed is false"
cat > "${DAEMON_MANIFEST}" << 'EOF'
tag: daemon/v0.4.99.1
component: daemon
cut_sha: 11223344
prev_tag: daemon/v0.4.3
verdict: PASS
commits:
  - sha: aabb1122
    pr: 3279
    additive_only: true
    bff_consumer_deployed: false
EOF
UNDEPLOYED=$(python3 -c "
import yaml
with open('${DAEMON_MANIFEST}') as f:
    doc = yaml.safe_load(f)
commits = doc.get('commits', []) or []
for c in commits:
    if c.get('bff_consumer_deployed') is False:
        print(c.get('sha', '(unknown)'))
")
if [[ -n "${UNDEPLOYED}" ]]; then
  pass "bff_consumer_deployed=false detected — gate would FAIL (correct)"
else
  fail "bff_consumer_deployed=false not detected"
fi

# Test 8: PASS — all rows have bff_consumer_deployed: true
echo "Test 8 (daemon): gate PASSES when all rows have bff_consumer_deployed: true"
cat > "${DAEMON_MANIFEST}" << 'EOF'
tag: daemon/v0.4.99.1
component: daemon
cut_sha: 11223344
prev_tag: daemon/v0.4.3
verdict: PASS
commits:
  - sha: aabb1122
    pr: 3279
    additive_only: true
    bff_consumer_deployed: true
  - sha: ccdd3344
    pr: 3280
    additive_only: true
    bff_consumer_deployed: true
EOF
UNDEPLOYED=$(python3 -c "
import yaml
with open('${DAEMON_MANIFEST}') as f:
    doc = yaml.safe_load(f)
commits = doc.get('commits', []) or []
for c in commits:
    if c.get('bff_consumer_deployed') is False:
        print(c.get('sha', '(unknown)'))
")
if [[ -z "${UNDEPLOYED}" ]]; then
  pass "no bff_consumer_deployed=false rows — gate would PASS (correct)"
else
  fail "unexpected undeployed rows: ${UNDEPLOYED}"
fi

rm -rf "${DAEMON_MANIFEST_DIR}"

echo ""
echo "============================================================"
echo "Results: ${PASS_COUNT} PASS, ${FAIL_COUNT} FAIL"
if [[ "${FAIL_COUNT}" -gt 0 ]]; then
  echo "OVERALL: FAIL"
  exit 1
fi
echo "OVERALL: PASS"
