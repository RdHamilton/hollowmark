#!/usr/bin/env bash
# Smoke tests for issue-deduplication-check/check.sh (hollowmark-tickets #1117)
# Verifies NEEDLE env var is exported correctly so the dedup match is real, not vacuous.
#
# AC1: NEEDLE exported — python subprocess sees it
# AC2: Empty NEEDLE exits non-zero with diagnostic
# AC3: Known-non-duplicate title returns SAFE; matching title returns DUPLICATE
#
# Usage: ./test_check.sh [path-to-check.sh]
# Defaults to the co-located check.sh when run from this directory.
set -uo pipefail

SCRIPT="${1:-$(dirname "$0")/check.sh}"
if [ ! -f "$SCRIPT" ]; then
  echo "check.sh not found at: $SCRIPT" >&2
  exit 2
fi

PASS=0
FAIL=0

run_test() {
  local name="$1"
  local expected_exit="$2"
  local expected_output="$3"
  shift 3
  local actual_output
  local actual_exit=0
  actual_output=$("$@" 2>&1) || actual_exit=$?
  if [ "$actual_exit" -eq "$expected_exit" ] && echo "$actual_output" | grep -qF "$expected_output"; then
    echo "PASS: $name"
    PASS=$((PASS+1))
  else
    echo "FAIL: $name"
    echo "  expected exit=$expected_exit containing: $expected_output"
    echo "  got    exit=$actual_exit output: $actual_output"
    FAIL=$((FAIL+1))
  fi
}

# Set up a mock gh that returns a realistic non-matching issue list
MOCK_DIR=$(mktemp -d)
trap 'rm -rf "$MOCK_DIR"' EXIT

# Mock for non-matching case: issue list returns an unrelated ticket
cat > "$MOCK_DIR/gh" << 'MOCK'
#!/usr/bin/env bash
echo '[{"number":999,"title":"Completely unrelated billing configuration ticket","state":"open"}]'
MOCK
chmod +x "$MOCK_DIR/gh"

# AC2: no argument → usage error, exit 2
run_test "AC2: no-arg exits 2 with USAGE message" 2 "USAGE" bash "$SCRIPT"

# AC1+AC3 (the core bug): a clearly non-matching title must return SAFE, not DUPLICATE.
# Before the fix, empty NEEDLE caused '' in <any string> == True → always DUPLICATE.
run_test "AC1+AC3: non-matching title returns SAFE" 0 "DEDUP_STATUS: SAFE" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "A totally unique xyz987 title that exists nowhere"

# AC3: a title that genuinely matches the mocked issue must return DUPLICATE
cat > "$MOCK_DIR/gh" << 'MOCK'
#!/usr/bin/env bash
echo '[{"number":42,"title":"billing configuration error during checkout","state":"open"}]'
MOCK

run_test "AC3: matching title returns DUPLICATE with issue number" 1 "DEDUP_STATUS: DUPLICATE" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "billing configuration error"

run_test "AC3: MATCH_NUMBER present in DUPLICATE output" 1 "MATCH_NUMBER: 42" \
  env PATH="$MOCK_DIR:$PATH" bash "$SCRIPT" "billing configuration error"

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && exit 0 || exit 1
