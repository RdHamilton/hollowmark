#!/usr/bin/env bash
# .github/tests/pr-checklist-gate-test.sh
#
# Test harness for the pr-checklist-gate branch-prefix exemption logic in
# .github/workflows/process-gates.yml (job: pr-checklist-gate).
#
# The job's `if:` condition exempts certain branch prefixes from the
# Pre-Review Checklist requirement. This test verifies that the exemption
# function correctly classifies every known exempt and non-exempt prefix.
#
# Ticket: #661 — fix/release/ prefix not in exemption list
#
# Run:
#   bash .github/tests/pr-checklist-gate-test.sh
#
# Exit code: 0 = all passed, non-zero = at least one failure.

set -euo pipefail

PASS_COUNT=0
FAIL_COUNT=0

# ---------------------------------------------------------------------------
# is_exempt <branch>
#
# Mirrors the `if:` condition logic from process-gates.yml pr-checklist-gate.
# The job runs (checklist IS required) when none of the startsWith guards match.
# The job is SKIPPED (branch IS exempt) when any startsWith matches.
#
# Returns 0 (exit-success) if the branch IS exempt (job would be skipped).
# Returns 1 (exit-failure) if the branch is NOT exempt (job would run).
# ---------------------------------------------------------------------------
is_exempt() {
  local branch="$1"
  case "$branch" in
    chore/*)      return 0 ;;
    docs/*)       return 0 ;;
    fix/ci/*)     return 0 ;;
    fix/infra/*)  return 0 ;;
    fix/staging/*) return 0 ;;
    fix/release/*) return 0 ;;
    *) return 1 ;;
  esac
}

# ---------------------------------------------------------------------------
# run_case <name> <branch> <expect_exempt: yes|no>
# ---------------------------------------------------------------------------
run_case() {
  local name="$1"
  local branch="$2"
  local expect_exempt="$3"

  local actual_exempt="no"
  if is_exempt "$branch"; then
    actual_exempt="yes"
  fi

  if [ "$actual_exempt" = "$expect_exempt" ]; then
    echo "  PASS: $name"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    echo "  FAIL: $name"
    echo "        branch='$branch' expected_exempt=$expect_exempt got=$actual_exempt"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

echo "=== pr-checklist-gate branch-prefix exemption tests ==="
echo ""

echo "-- Exempt branches (checklist NOT required) --"
run_case "chore/ prefix"            "chore/update-deps"              "yes"
run_case "docs/ prefix"             "docs/update-adr-070"            "yes"
run_case "fix/ci/ prefix"           "fix/ci/flaky-smoke-gate"        "yes"
run_case "fix/infra/ prefix"        "fix/infra/nginx-timeout"        "yes"
run_case "fix/staging/ prefix"      "fix/staging/bff-env-var"        "yes"
run_case "fix/release/ prefix"      "fix/release/goreleaser-config"  "yes"
run_case "fix/release/ nested path" "fix/release/v0.4.3/daemon-tag"  "yes"

echo ""
echo "-- Non-exempt branches (checklist IS required) --"
run_case "feat/ prefix"             "feat/new-draft-advisor"         "no"
run_case "fix/ (no sub-namespace)"  "fix/661-checklist-gate-prefix"  "no"
run_case "fix/foo/ prefix"          "fix/foo/some-change"            "no"
run_case "main branch"              "main"                           "no"
run_case "bare fix/release (no trailing slash)" "fix/release"       "no"

echo ""
echo "=== Results: ${PASS_COUNT} passed, ${FAIL_COUNT} failed ==="

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
