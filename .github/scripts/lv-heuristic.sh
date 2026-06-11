#!/usr/bin/env bash
# .github/scripts/lv-heuristic.sh
#
# Local Verification heuristic checker.
#
# Called by pr-local-verification-check.yml with the PR body written to a
# temporary file. The workflow sets the following env vars before calling:
#
#   IS_DEPENDABOT  -- "yes" if github.actor == 'dependabot[bot]', else "no"
#   SHELL_TOUCHED  -- "yes" if the PR touches *.sh or scripts/deploy/, else "no"
#
# Usage:
#   IS_DEPENDABOT=no SHELL_TOUCHED=no bash lv-heuristic.sh /path/to/pr_body.txt
#
# Exit codes:
#   0 -- heuristic PASS (or Dependabot exemption)
#   1 -- heuristic FAIL
#
# WHAT IT CHECKS
# --------------
# 1. The PR body contains a `## Local Verification` section header.
# 2. The section contains >= 1 fenced code block (``` ... ```).
# 3. >= 1 fenced code block contains a shell-prompt indicator line (strict
#    path), OR the relaxed fallback: code block has >= 2 non-blank lines
#    AND at least one line matches a command/output shape (not pure prose).
#    Prose-in-fence detection (#831): when the relaxed path is taken, lines
#    are scanned for command/output shapes -- a block where every non-blank
#    line is prose text (no shell metacharacters, paths, or output keywords)
#    is rejected. This closes the gap where "This looks fine.\nPASS" would
#    have passed the simple >=2 non-blank lines check.
# 4. The section contains >= 1 explicit success marker: PASS | OK | SUCCESS
# 5. Shell-script PRs (SHELL_TOUCHED=yes) must show --dry-run or
#    sh -n/shellcheck for declaration-only changes.
#
# DEPENDABOT EXEMPTION (#826)
# ---------------------------
# IS_DEPENDABOT=yes short-circuits to exit 0 before any checks run.
# Dependabot PRs are automated dependency bumps with no human-authored code --
# requiring a ## Local Verification transcript adds no security value.

set -u

BODY_FILE="${1:-}"
IS_DEPENDABOT="${IS_DEPENDABOT:-no}"
SHELL_TOUCHED="${SHELL_TOUCHED:-no}"

if [ -z "$BODY_FILE" ]; then
  echo "Usage: IS_DEPENDABOT=no SHELL_TOUCHED=no $0 <pr_body_file>"
  exit 1
fi

if [ ! -f "$BODY_FILE" ]; then
  echo "ERROR: body file not found: $BODY_FILE"
  exit 1
fi

# ---------------------------------------------------------------------------
# Dependabot exemption (#826)
# ---------------------------------------------------------------------------
if [ "${IS_DEPENDABOT}" = "yes" ]; then
  echo "Actor: dependabot[bot]"
  echo "Dependabot PR -- Local Verification section not required."
  echo "permissionDecision: ALLOW"
  echo "Local Verification heuristic check: PASS (Dependabot exemption)"
  exit 0
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
fail() {
  local msg="$1"
  local hint="${2:-}"
  echo ""
  echo "permissionDecision: BLOCKED"
  echo "ERROR: Local Verification gate -- ${msg}"
  if [ -n "${hint}" ]; then
    echo ""
    echo "HINT: ${hint}"
  fi
  echo ""
  echo "Required structure (see PR template):"
  echo ""
  echo "  ## Local Verification"
  echo "  \`\`\`"
  echo "  \$ <command you actually ran>"
  echo "  <actual terminal output>"
  echo "  ... PASS"
  echo "  \`\`\`"
  echo ""
  echo "Rules:"
  echo "  - >=1 fenced code block in the section"
  echo "  - >=1 non-blank content lines inside the block (beyond the PASS marker)"
  echo "    that look like real command/output (not prose sentences)"
  echo "  - >=1 PASS / OK / SUCCESS marker in the section"
  if [ "${SHELL_TOUCHED}" = "yes" ]; then
    echo "  - Shell-script PR: transcript must contain --dry-run"
  fi
  exit 1
}

WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

LV_SECTION="$WORK_DIR/lv_section.txt"
LV_CODE="$WORK_DIR/lv_code.txt"

# ---- 1. Section header present ----
if ! grep -qE '^## Local Verification' "$BODY_FILE"; then
  fail "PR body is missing the '## Local Verification' section header."
fi

# ---- Extract section (between '## Local Verification' and next '## ') ----
awk '
  /^## Local Verification/ { inside=1; next }
  inside && /^## / { inside=0 }
  inside { print }
' "$BODY_FILE" > "$LV_SECTION"

if [ ! -s "$LV_SECTION" ]; then
  fail "Local Verification section is empty."
fi

echo "---- Local Verification section ----"
cat "$LV_SECTION"
echo "------------------------------------"

# ---- 2. >=1 fenced code block ----
FENCE_COUNT=$(grep -cE '^[[:space:]]*```' "$LV_SECTION" || true)
if [ "${FENCE_COUNT:-0}" -lt 2 ]; then
  echo ""
  echo "Parser expected: lines matching regex  ^[[:space:]]*\`\`\`  (raw triple-backtick at line start)"
  echo "Expected form (opening and closing fence on their own lines):"
  echo "  \`\`\`"
  echo "  \$ your-command"
  echo "  actual output"
  echo "  PASS"
  echo "  \`\`\`"
  echo ""
  ESCAPED_FENCE=$(grep -E "\\\\[\`]" "$LV_SECTION" | head -3 || true)
  if [ -n "${ESCAPED_FENCE}" ]; then
    echo "Possible escaped-fence lines detected (showing hex via cat -A):"
    printf '%s\n' "${ESCAPED_FENCE}" | head -3 | cat -A
    echo ""
    echo "Those backslash-escaped backticks are NOT recognized as fences."
    echo "Replace them with three raw backtick characters on their own line."
  fi
  fail "No fenced code block found in Local Verification (need an opening and closing \`\`\`)."
fi

# ---- 3. >=1 prompt indicator OR relaxed fallback with command/output shape ----
awk '
  /^[[:space:]]*```/ { fence = !fence; next }
  fence { print }
' "$LV_SECTION" > "$LV_CODE"

if [ ! -s "$LV_CODE" ]; then
  fail "Fenced code block(s) present but empty."
fi

# Prompt regex (strict path -- any of these means a real terminal transcript):
#   ^\s*\$\s    bash/zsh prompt
#   ^\s*>\s     PowerShell or quoted prompt
#   ^\s*#\s     root prompt / comment-style
#   ^\[.*\]\$   [user@host]$ style
#   ^\s*PS>     PowerShell explicit
#   ^\s*>>\s    secondary prompt (continuation)
PROMPT_FOUND=no
if grep -qE '(^[[:space:]]*\$ )|(^[[:space:]]*> )|(^[[:space:]]*# )|(^\[[^]]+\]\$)|(^[[:space:]]*PS> )|(^[[:space:]]*>> )' "$LV_CODE"; then
  PROMPT_FOUND=yes
fi

NONBLANK_COUNT=0

if [ "${PROMPT_FOUND}" = "no" ]; then
  # Relaxed fallback (#828): count non-blank lines.
  NONBLANK_COUNT=$(grep -cE '[^[:space:]]' "$LV_CODE" || true)
  if [ "${NONBLANK_COUNT:-0}" -lt 2 ]; then
    echo ""
    echo "No shell-prompt prefix found AND code block has fewer than 2 non-blank lines."
    echo ""
    echo "Parser expected EITHER:"
    echo "  (a) At least one line matching a prompt prefix:"
    echo "      \`\$ \`  \`> \`  \`# \`  \`[user@host]\$\`  \`PS> \`  \`>> \`"
    echo "  OR"
    echo "  (b) At least 2 non-blank lines inside the code block"
    echo "      (command + output content, beyond the PASS marker alone)"
    echo ""
    echo "A block containing only \`PASS\` (with no command or output) is not a real transcript."
    echo ""
    fail "Code block appears to have no real transcript content. Add your actual command output, or prefix command lines with \`\$ \`."
  fi

  # ---- Prose-in-fence detection (#831) ----
  # With >= 2 non-blank lines confirmed, check that at least one NON-MARKER
  # line looks like a command or output rather than a prose sentence.
  #
  # The PASS/OK/SUCCESS marker is explicitly excluded from this check --
  # its presence is what satisfies check #4 below. We need to find evidence
  # of a real command or output in the *other* lines of the block.
  #
  # A non-marker line is "command/output shaped" if it matches any of:
  #
  #   (a) Terminal output keywords: FAIL, error:, fatal:, warning: --
  #       typical stderr/stdout tokens distinct from PASS/OK/SUCCESS.
  #   (b) Go test output shape: "ok  <pkg>  <time>s"
  #   (c) A module/package path: github.com/, golang.org/
  #   (d) A path with slashes: <word>/<word> (e.g. ./services/bff/...)
  #   (e) A line starting with a common CLI tool name.
  #   (f) A count + unit (e.g. "0 errors", "3 tests").
  #   (g) A time suffix (e.g. "0.834s", "4.32s") -- typical test output.
  #   (h) A file:line reference (e.g. "main.go:12:").
  #
  # Pure prose lines ("This change looks correct to me.") will not match
  # any of these. If NO non-marker line matches ANY pattern, the block is
  # rejected as prose-in-fence.
  #
  # Design constraint (#831 Out of Scope): this remains shape-only (syntactic).
  # The human reviewer is the backstop for sophisticated fakes.

  # Strip the success marker lines before checking for command/output shapes.
  # This prevents the PASS/SUCCESS marker itself from satisfying the check.
  LV_CODE_NO_MARKER="$WORK_DIR/lv_code_no_marker.txt"
  grep -viE '^\s*(\bPASS\b|\bOK\b|\bSUCCESS\b)\s*$' "$LV_CODE" > "$LV_CODE_NO_MARKER" || true

  # Pattern: non-prose command/output shapes (does NOT include PASS/OK/SUCCESS)
  CMD_OUTPUT_PATTERN='(\bFAIL\b)|(error:)|(fatal:)|(warning:)|(^[[:space:]]*ok[[:space:]]+[a-zA-Z])|(github\.com/)|(golang\.org/)|(^[[:space:]]*[a-zA-Z0-9._-]+/[a-zA-Z0-9._/-]+)|(^[[:space:]]*(go|npm|npx|make|cargo|python|pip|docker|kubectl|bash|sh|golangci-lint|actionlint|shellcheck|gofumpt)[[:space:]])|(^[[:space:]]*[0-9]+[[:space:]]*(error|warning|issue|test|check))|(^[[:space:]]*[0-9]+\.[0-9]+s)|([0-9]+:[0-9]+:)'

  COMMAND_SHAPE_FOUND=no
  if [ -s "$LV_CODE_NO_MARKER" ] && grep -qE "$CMD_OUTPUT_PATTERN" "$LV_CODE_NO_MARKER"; then
    COMMAND_SHAPE_FOUND=yes
  fi

  if [ "${COMMAND_SHAPE_FOUND}" = "no" ]; then
    echo ""
    echo "Relaxed fallback: code block has ${NONBLANK_COUNT} non-blank lines but no non-marker"
    echo "line matches a recognizable command/output shape."
    echo ""
    echo "A block of prose sentences inside a fence does not constitute a real transcript."
    echo "Patterns checked on non-marker lines (any one sufficient):"
    echo "  - Terminal output: FAIL / error: / fatal: / warning:"
    echo "  - Go test output: 'ok  <pkg>  <time>s'"
    echo "  - Module path: github.com/ or golang.org/"
    echo "  - Path with slashes: <word>/<word> (e.g. ./services/bff/...)"
    echo "  - Known CLI tool name at line start: go, npm, npx, make, ..."
    echo "  - Count + unit: '0 errors', '3 tests'"
    echo "  - Time suffix: '0.834s'"
    echo "  - File:line reference: 'main.go:12:'"
    echo ""
    echo "[LV-WARN] Local Verification blocked via prose-in-fence detection (#831)."
    echo "  The code block content looks like prose, not a command/output transcript."
    echo "  Reviewer: if this is a false positive, prefix your command lines with '\$ '."
    echo ""
    fail "Code block appears to contain prose rather than a real terminal transcript. Add actual command output, or prefix command lines with \`\$ \`."
  fi

  echo "No prompt prefix found -- relaxed fallback accepted (${NONBLANK_COUNT} non-blank lines, command/output shape confirmed)."
fi

# ---- 4. >=1 PASS/OK/SUCCESS marker (case-insensitive) ----
if ! grep -qiE '(\bPASS\b)|(\bOK\b)|(\bSUCCESS\b)' "$LV_SECTION"; then
  echo ""
  echo "Parser expected: at least one line in the section matching regex (case-insensitive, word-boundary):"
  printf '  (\\bPASS\\b)|(\\bOK\\b)|(\\bSUCCESS\\b)\n'
  echo "Accepted markers: PASS  OK  SUCCESS  (any case; must be a whole word)"
  echo ""
  fail "No PASS / OK / SUCCESS marker found in Local Verification section."
fi

# ---- 5. Shell-script PRs require --dry-run / dry-run ----
HAS_DRY_RUN=no
HAS_SYNTAX_CHECK=no
if [ "${SHELL_TOUCHED}" = "yes" ]; then
  if grep -qE '(--dry-run|dry-run)' "$LV_SECTION"; then
    HAS_DRY_RUN=yes
  fi
  if grep -qE '(\bsh[[:space:]]+-n[[:space:]]|shellcheck[[:space:]])' "$LV_SECTION"; then
    HAS_SYNTAX_CHECK=yes
  fi
  if [ "${HAS_DRY_RUN}" = "no" ] && [ "${HAS_SYNTAX_CHECK}" = "no" ]; then
    echo ""
    echo "Parser expected: at least one of:"
    echo "  (a) regex  (--dry-run|dry-run)  for executable entrypoint changes"
    echo "  (b) regex  (sh -n |shellcheck )  for declaration-only changes"
    echo "This PR touches a *.sh file or scripts/deploy/."
    echo "Executable entrypoint change -> include a --dry-run transcript."
    echo "Declaration-only change (function/constant/source only) -> include sh -n or shellcheck."
    echo ""
    fail "Shell-script PR (touches *.sh or scripts/deploy/): Local Verification must include either (a) a --dry-run transcript showing the actual code path was exercised, or (b) a syntax/lint check (sh -n or shellcheck) for declaration-only changes that add no executable entrypoint." \
         "If your change only adds a function, constant, or source call, use: sh -n <file> && shellcheck <file>"
  fi
fi

echo ""
echo "permissionDecision: ALLOW"
echo "Local Verification heuristic check: PASS"
echo "  - Section header: present"
echo "  - Fenced code blocks: ${FENCE_COUNT} fence markers found"
if [ "${PROMPT_FOUND}" = "yes" ]; then
  echo "  - Shell-prompt indicator: present"
else
  echo "  - Shell-prompt indicator: absent (relaxed fallback -- ${NONBLANK_COUNT} non-blank lines, command/output shape confirmed)"
fi
echo "  - Success marker: present"
if [ "${SHELL_TOUCHED}" = "yes" ]; then
  if [ "${HAS_DRY_RUN}" = "yes" ]; then
    echo "  - Shell-script verification: --dry-run present"
  else
    echo "  - Shell-script verification: declaration-only (sh -n / shellcheck accepted)"
  fi
fi
