#!/usr/bin/env bash
# cfn-iam-simulate — IAM simulate-principal-policy wrapper for VaultMTG infra PRs.
#
# Usage:
#   simulate.sh <role-arn> <"action1 action2 ..."> [--resources <arn1,arn2,...>] [--oidc-sub <sub>]
#
# Arguments:
#   role-arn     IAM role ARN to simulate (e.g. arn:aws:iam::901347789205:role/gha-infra-cfn-deploy)
#   actions      Space-separated list of IAM actions (quoted as one argument)
#
# Options:
#   --resources  Comma- or space-separated resource ARNs passed as --resource-arns.
#                Defaults to "*" when omitted.
#   --oidc-sub   OIDC subject claim expected in the trust policy.
#                When provided, the script prints an OIDC_SUB_CHECK advisory line
#                reminding the engineer to verify the sub pin in the trust policy
#                matches this value. (Live role inspection requires live AWS access;
#                the advisory makes the gate explicit without blocking on MFA.)
#
# Exit codes:
#   0  All actions allowed  (SIMULATION_RESULT: PASSED)
#   1  One or more actions denied  (SIMULATION_RESULT: FAILED)
#   2  Usage error
#
# Hard gate: do NOT open a CFN or IAM PR if any action is denied.
#
# Invokes aws with --profile personal (matches VaultMTG AWS CLI convention).
# Override by setting AWS_PROFILE in your environment before calling this script.

set -euo pipefail

PROFILE="${AWS_PROFILE:-personal}"

usage() {
  echo "USAGE: simulate.sh <role-arn> <\"action1 action2 ...\"> [--resources <arns>] [--oidc-sub <sub>]" >&2
  exit 2
}

# ---------------------------------------------------------------------------
# Parse positional arguments
# ---------------------------------------------------------------------------
ROLE_ARN="${1:-}"
ACTIONS_RAW="${2:-}"

if [ -z "$ROLE_ARN" ] || [ -z "$ACTIONS_RAW" ]; then
  usage
fi

shift 2

# ---------------------------------------------------------------------------
# Parse optional flags
# ---------------------------------------------------------------------------
RESOURCE_ARNS="*"
OIDC_SUB=""

while [ $# -gt 0 ]; do
  case "$1" in
    --resources)
      RESOURCE_ARNS="${2:-}"
      [ -z "$RESOURCE_ARNS" ] && { echo "ERROR: --resources requires a value" >&2; usage; }
      shift 2
      ;;
    --oidc-sub)
      OIDC_SUB="${2:-}"
      [ -z "$OIDC_SUB" ] && { echo "ERROR: --oidc-sub requires a value" >&2; usage; }
      shift 2
      ;;
    *)
      echo "ERROR: unknown option: $1" >&2
      usage
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Convert space/comma-separated actions into an array for the CLI
# ---------------------------------------------------------------------------
# shellcheck disable=SC2206
ACTIONS=( $ACTIONS_RAW )

# Convert comma-separated resources to an array for --resource-arns
# (word-splitting is intentional here — each ARN becomes a separate element)
# shellcheck disable=SC2207
RESOURCE_ARNS_ARR=( $(echo "$RESOURCE_ARNS" | tr ',' ' ') )

# ---------------------------------------------------------------------------
# Run the simulation
# ---------------------------------------------------------------------------
echo "Simulating IAM policy for role: $ROLE_ARN"
echo "Actions: ${ACTIONS[*]}"
echo "Resources: ${RESOURCE_ARNS_ARR[*]}"
echo ""

RAW_JSON=$(aws iam simulate-principal-policy \
  --profile "$PROFILE" \
  --policy-source-arn "$ROLE_ARN" \
  --action-names "${ACTIONS[@]}" \
  --resource-arns "${RESOURCE_ARNS_ARR[@]}" \
  --output json \
  2>&1) || {
    echo "ERROR: aws iam simulate-principal-policy failed:" >&2
    echo "$RAW_JSON" >&2
    exit 2
  }

# ---------------------------------------------------------------------------
# Parse results and print per-action verdict
# ---------------------------------------------------------------------------
DENIED_COUNT=0

echo "Per-action results:"
while IFS= read -r line; do
  ACTION=$(echo "$line" | python3 -c "import json,sys; r=json.loads(sys.stdin.read()); print(r['action'])" 2>/dev/null)
  DECISION=$(echo "$line" | python3 -c "import json,sys; r=json.loads(sys.stdin.read()); print(r['decision'])" 2>/dev/null)

  if [ "$DECISION" = "allowed" ]; then
    printf "  ALLOWED  %s\n" "$ACTION"
  else
    printf "  DENIED   %s  (%s)\n" "$ACTION" "$DECISION"
    DENIED_COUNT=$((DENIED_COUNT + 1))
  fi
done < <(echo "$RAW_JSON" | python3 -c "
import json, sys
data = json.load(sys.stdin)
results = data if isinstance(data, list) else data.get('EvaluationResults', data)
for r in results:
    action = r.get('EvalActionName', r.get('action', ''))
    decision = r.get('EvalDecision', r.get('decision', ''))
    print(json.dumps({'action': action, 'decision': decision}))
")

echo ""

# ---------------------------------------------------------------------------
# OIDC sub advisory (does not block the exit code)
# ---------------------------------------------------------------------------
if [ -n "$OIDC_SUB" ]; then
  echo "OIDC_SUB_CHECK: Verify the trust policy sub condition matches: $OIDC_SUB"
  echo "  Run: aws iam get-role --profile $PROFILE --role-name <name> | python3 -c \"import json,sys; doc=json.load(sys.stdin); print(json.dumps(doc['Role']['AssumeRolePolicyDocument'], indent=2))\""
  echo ""
fi

# ---------------------------------------------------------------------------
# Final verdict
# ---------------------------------------------------------------------------
if [ "$DENIED_COUNT" -eq 0 ]; then
  echo "SIMULATION_RESULT: PASSED — all actions allowed. Safe to open PR."
  exit 0
else
  echo "SIMULATION_RESULT: FAILED — $DENIED_COUNT action(s) denied. Do NOT open the PR."
  echo "Fix: add the denied actions to the matching Sid in cloudformation/iam-gha-roles.yml, then re-run."
  exit 1
fi
