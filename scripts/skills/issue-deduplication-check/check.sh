#!/usr/bin/env bash
# Dedup check before creating a GitHub issue.
# Usage: ./check.sh "Proposed issue title"
# Exit 0 = SAFE to create. Exit 1 = duplicate found (open or closed). Exit 2 = usage error.
set -euo pipefail

TITLE="${1:-}"
if [ -z "$TITLE" ]; then
  echo "USAGE: check.sh \"Proposed issue title\"" >&2
  exit 2
fi

NEEDLE=$(echo "$TITLE" | tr '[:upper:]' '[:lower:]')
export NEEDLE

# Check open issues
OPEN_MATCH=$(gh issue list --repo RdHamilton/hollowmark-tickets --state open \
  --search "$TITLE" --json number,title,state \
  | python3 -c "
import json, sys, os
needle = os.environ.get('NEEDLE', '')
if not needle:
    print('ERROR: NEEDLE is empty — check.sh bug', file=sys.stderr)
    sys.exit(2)
for i in json.load(sys.stdin):
    t = i['title'].lower()
    if needle in t or t in needle:
        print(i['number'], i['title'])
        break
" 2>/dev/null || true)

if [ -n "$OPEN_MATCH" ]; then
  NUM=$(echo "$OPEN_MATCH" | awk '{print $1}')
  echo "DEDUP_STATUS: DUPLICATE"
  echo "MATCH_NUMBER: $NUM"
  echo "ACTION: use existing #$NUM"
  exit 1
fi

# Check closed issues
CLOSED_MATCH=$(gh issue list --repo RdHamilton/hollowmark-tickets --state closed \
  --search "$TITLE" --json number,title \
  | python3 -c "
import json, sys, os
needle = os.environ.get('NEEDLE', '')
if not needle:
    print('ERROR: NEEDLE is empty — check.sh bug', file=sys.stderr)
    sys.exit(2)
for i in json.load(sys.stdin):
    t = i['title'].lower()
    if needle in t or t in needle:
        print(i['number'], i['title'])
        break
" 2>/dev/null || true)

if [ -n "$CLOSED_MATCH" ]; then
  NUM=$(echo "$CLOSED_MATCH" | awk '{print $1}')
  echo "DEDUP_STATUS: CLOSED-DUPLICATE"
  echo "MATCH_NUMBER: $NUM"
  echo "ACTION: review and reopen #$NUM if needed"
  exit 1
fi

echo "DEDUP_STATUS: SAFE"
echo "MATCH_NUMBER: n/a"
echo "ACTION: create new"
exit 0
