#!/usr/bin/env bash
# Self-tests for cfn-iam-simulate/simulate.sh — hollowmark-tickets #1031
#
# All tests use a mock `aws` binary so no live AWS credentials or MFA are required.
#
# AC-T1: no args → usage error, exit 2
# AC-T2: role ARN only, no actions → usage error, exit 2
# AC-T3: all actions allowed → SIMULATION_RESULT: PASSED, exit 0
# AC-T4: explicit deny → SIMULATION_RESULT: FAILED, exit 1
# AC-T5: implicitDeny treated as denied → SIMULATION_RESULT: FAILED, exit 1
# AC-T6: per-action ALLOWED/DENIED lines present in output
# AC-T7: --resources flag accepted without error
# AC-T8: --oidc-sub flag prints OIDC_SUB_CHECK line
#
# Usage: ./test_simulate.sh [path-to-simulate.sh]
set -uo pipefail

SCRIPT="${1:-$(dirname "$0")/simulate.sh}"
if [ ! -f "$SCRIPT" ]; then
  echo "simulate.sh not found at: $SCRIPT" >&2
  exit 2
fi

PASS=0
FAIL=0

run_test() {
  local name="$1"
  local expected_exit="$2"
  local expected_pattern="$3"
  shift 3
  local actual_output
  local actual_exit=0
  actual_output=$("$@" 2>&1) || actual_exit=$?
  if [ "$actual_exit" -eq "$expected_exit" ] && echo "$actual_output" | grep -qF "$expected_pattern"; then
    echo "PASS: $name"
    PASS=$((PASS+1))
  else
    echo "FAIL: $name"
    echo "  expected exit=$expected_exit containing: $expected_pattern"
    echo "  got    exit=$actual_exit output:"
    while IFS= read -r out_line; do printf '    %s\n' "$out_line"; done <<< "$actual_output"
    FAIL=$((FAIL+1))
  fi
}

# ---------------------------------------------------------------------------
# Mock AWS CLI helpers
# ---------------------------------------------------------------------------
MOCK_DIR=$(mktemp -d)
trap 'rm -rf "$MOCK_DIR"' EXIT

write_aws_mock() {
  local payload="$1"
  printf '#!/usr/bin/env bash\nprintf '"'"'%%s\n'"'"' '"'"'%s'"'"'\n' "$payload" > "$MOCK_DIR/aws"
  chmod +x "$MOCK_DIR/aws"
}

ROLE_ARN="arn:aws:iam::901347789205:role/gha-infra-cfn-deploy"

# ---------------------------------------------------------------------------
# AC-T1: no args → usage error, exit 2
# ---------------------------------------------------------------------------
run_test "AC-T1: no args exits 2 with USAGE" 2 "USAGE" \
  bash "$SCRIPT"

# ---------------------------------------------------------------------------
# AC-T2: role ARN only, no actions → usage error, exit 2
# ---------------------------------------------------------------------------
run_test "AC-T2: role ARN but no actions exits 2 with USAGE" 2 "USAGE" \
  bash "$SCRIPT" "$ROLE_ARN"

# ---------------------------------------------------------------------------
# AC-T3: all actions allowed → PASSED, exit 0
# ---------------------------------------------------------------------------
ALL_ALLOWED='[{"EvalActionName":"s3:CreateBucket","EvalDecision":"allowed"},{"EvalActionName":"s3:DeleteBucket","EvalDecision":"allowed"}]'
write_aws_mock "$ALL_ALLOWED"

run_test "AC-T3: all allowed → SIMULATION_RESULT: PASSED, exit 0" 0 "SIMULATION_RESULT: PASSED" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "$ROLE_ARN" "s3:CreateBucket s3:DeleteBucket"

# ---------------------------------------------------------------------------
# AC-T6: per-action ALLOWED lines present
# ---------------------------------------------------------------------------
run_test "AC-T6a: per-action ALLOWED line for s3:CreateBucket" 0 "ALLOWED  s3:CreateBucket" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "$ROLE_ARN" "s3:CreateBucket s3:DeleteBucket"

run_test "AC-T6b: per-action ALLOWED line for s3:DeleteBucket" 0 "ALLOWED  s3:DeleteBucket" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "$ROLE_ARN" "s3:CreateBucket s3:DeleteBucket"

# ---------------------------------------------------------------------------
# AC-T4: explicit deny → FAILED, exit 1
# ---------------------------------------------------------------------------
EXPLICIT_DENY='[{"EvalActionName":"iam:CreateRole","EvalDecision":"explicitDeny"},{"EvalActionName":"s3:CreateBucket","EvalDecision":"allowed"}]'
write_aws_mock "$EXPLICIT_DENY"

run_test "AC-T4: explicitDeny → SIMULATION_RESULT: FAILED, exit 1" 1 "SIMULATION_RESULT: FAILED" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "$ROLE_ARN" "iam:CreateRole s3:CreateBucket"

run_test "AC-T4b: DENIED line present for iam:CreateRole" 1 "DENIED   iam:CreateRole" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "$ROLE_ARN" "iam:CreateRole s3:CreateBucket"

run_test "AC-T4c: ALLOWED line still present alongside DENIED" 1 "ALLOWED  s3:CreateBucket" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "$ROLE_ARN" "iam:CreateRole s3:CreateBucket"

# ---------------------------------------------------------------------------
# AC-T5: implicitDeny treated as denied → FAILED, exit 1
# ---------------------------------------------------------------------------
IMPLICIT_DENY='[{"EvalActionName":"guardduty:CreateDetector","EvalDecision":"implicitDeny"}]'
write_aws_mock "$IMPLICIT_DENY"

run_test "AC-T5: implicitDeny → SIMULATION_RESULT: FAILED, exit 1" 1 "SIMULATION_RESULT: FAILED" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "$ROLE_ARN" "guardduty:CreateDetector"

run_test "AC-T5b: DENIED line present for guardduty:CreateDetector" 1 "DENIED   guardduty:CreateDetector" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "$ROLE_ARN" "guardduty:CreateDetector"

# ---------------------------------------------------------------------------
# AC-T7: --resources flag accepted without error
# ---------------------------------------------------------------------------
write_aws_mock "$ALL_ALLOWED"

run_test "AC-T7: --resources flag accepted, result is PASSED" 0 "SIMULATION_RESULT: PASSED" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "$ROLE_ARN" "s3:CreateBucket" \
    --resources "arn:aws:s3:::my-bucket"

# ---------------------------------------------------------------------------
# AC-T8: --oidc-sub flag prints OIDC_SUB_CHECK line
# ---------------------------------------------------------------------------
OIDC_ALLOWED='[{"EvalActionName":"sts:AssumeRoleWithWebIdentity","EvalDecision":"allowed"}]'
write_aws_mock "$OIDC_ALLOWED"

run_test "AC-T8: --oidc-sub flag prints OIDC_SUB_CHECK line" 0 "OIDC_SUB_CHECK" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "$ROLE_ARN" "sts:AssumeRoleWithWebIdentity" \
    --oidc-sub "repo:RdHamilton/hollowmark:ref:refs/heads/main"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
