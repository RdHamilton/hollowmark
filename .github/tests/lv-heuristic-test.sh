#!/usr/bin/env bash
# .github/tests/lv-heuristic-test.sh
#
# Test harness for the LV heuristic script (.github/scripts/lv-heuristic.sh).
#
# Covers the three cluster tickets:
#   #826 -- dependabot[bot] exemption  (IS_DEPENDABOT=yes skips all checks)
#   #828 -- bare command lines without $ prefix accepted (relaxed fallback)
#   #831 -- prose-in-fence must NOT pass the relaxed fallback
#
# Run:
#   bash .github/tests/lv-heuristic-test.sh
#
# Exit code: 0 = all passed, non-zero = at least one failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SCRIPT="${SCRIPT_DIR}/../scripts/lv-heuristic.sh"

if [ ! -f "$SCRIPT" ]; then
  echo "FATAL: heuristic script not found at $SCRIPT"
  exit 1
fi

PASS_COUNT=0
FAIL_COUNT=0
WORK_DIR=$(mktemp -d)

cleanup() {
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

# run_case <name> <expected_exit: 0|1> <shell_touched: yes|no> <is_dependabot: yes|no> <body_content>
run_case() {
  local name="$1"
  local expected_exit="$2"
  local shell_touched="$3"
  local is_dependabot="$4"
  local body_content="$5"

  local case_dir
  case_dir=$(mktemp -d "$WORK_DIR/case.XXXXXX")
  local body_file="$case_dir/pr_body.txt"
  printf '%s\n' "$body_content" > "$body_file"

  local actual_exit=0
  SHELL_TOUCHED="$shell_touched" IS_DEPENDABOT="$is_dependabot" \
    bash "$SCRIPT" "$body_file" > "$case_dir/output.txt" 2>&1 || actual_exit=$?

  if [ "$actual_exit" -eq "$expected_exit" ]; then
    echo "PASS: $name"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    echo "FAIL: $name"
    echo "      Expected exit $expected_exit, got $actual_exit"
    echo "      --- output ---"
    sed 's/^/        /' "$case_dir/output.txt"
    echo "      --- end output ---"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

# ---------------------------------------------------------------------------
# #826 -- Dependabot exemption
# A dependabot[bot] PR with NO Local Verification section must exit 0.
# A human PR with no LV section must still exit 1.
# ---------------------------------------------------------------------------
run_case \
  "#826: dependabot PR with no LV section is exempt (exits 0)" \
  0 \
  "no" \
  "yes" \
  "## Summary
Bump golang.org/x/net from 0.22.0 to 0.23.0"

run_case \
  "#826: human PR with no LV section still fails (exits 1)" \
  1 \
  "no" \
  "no" \
  "## Summary
Fix a bug"

# ---------------------------------------------------------------------------
# #828 -- Bare command lines (no $ prefix) accepted by relaxed fallback
# A genuine transcript with a recognizable command shape + marker must pass.
# A block with only a PASS marker (1 non-blank line) must fail.
# ---------------------------------------------------------------------------
run_case \
  "#828: bare command + output + SUCCESS exits 0 (relaxed fallback)" \
  0 \
  "no" \
  "no" \
  '## Local Verification
```
npx tsc --noEmit
0 errors
SUCCESS
```'

run_case \
  "#828: bare command-only block, single PASS marker exits 1 (no real content)" \
  1 \
  "no" \
  "no" \
  '## Local Verification
```
PASS
```'

# ---------------------------------------------------------------------------
# #831 -- Prose-in-fence must NOT pass the relaxed fallback
# A fenced block with prose sentences (no command/output shape) + PASS
# must be rejected even though it has >= 2 non-blank lines.
# ---------------------------------------------------------------------------
run_case \
  "#831: single prose sentence + PASS exits 1 (prose-in-fence rejected)" \
  1 \
  "no" \
  "no" \
  '## Local Verification
```
This change looks correct to me.
PASS
```'

run_case \
  "#831: two prose sentences + PASS exits 1 (all-prose block rejected)" \
  1 \
  "no" \
  "no" \
  '## Local Verification
```
I ran the tests and they all passed locally.
Everything looks good.
PASS
```'

run_case \
  "#831: prose migration description + SUCCESS exits 1 (no command/output shape)" \
  1 \
  "no" \
  "no" \
  '## Local Verification
```
The migration applied without errors.
Verified the new column is present in the schema.
SUCCESS
```'

# ---------------------------------------------------------------------------
# Regression: existing PASS cases must still work after the #831 change.
# ---------------------------------------------------------------------------
run_case \
  "regression A: dollar-prefix transcript still passes" \
  0 \
  "no" \
  "no" \
  '## Local Verification
```
$ go test ./...
ok  github.com/RdHamilton/hollowmark/services/bff  0.834s
PASS
```'

run_case \
  "regression C: bracket-host prompt transcript still passes" \
  0 \
  "no" \
  "no" \
  '## Local Verification
```
[user@host]$ npm run build
> frontend@0.0.0 build
built in 4.32s
SUCCESS
```'

run_case \
  "regression L: bare go test path output (path:line shape) passes" \
  0 \
  "no" \
  "no" \
  '## Local Verification
```
go test ./services/bff/...
ok  github.com/RdHamilton/hollowmark/services/bff  1.2s
PASS
```'

run_case \
  "regression: bare command with error: output shape passes" \
  0 \
  "no" \
  "no" \
  '## Local Verification
```
golangci-lint run ./...
error: no issues found
PASS
```'

run_case \
  "regression D: no LV section header exits 1" \
  1 \
  "no" \
  "no" \
  '## Summary
Some changes'

run_case \
  "regression F: LV section but no fenced block exits 1" \
  1 \
  "no" \
  "no" \
  '## Local Verification
I ran the tests and it passed.'

run_case \
  "regression G: fenced block with only PASS exits 1" \
  1 \
  "no" \
  "no" \
  '## Local Verification
```
PASS
```'

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "Results: ${PASS_COUNT} passed, ${FAIL_COUNT} failed"
if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
