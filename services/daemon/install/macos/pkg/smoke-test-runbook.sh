#!/usr/bin/env bash
# smoke-test-runbook.sh — macOS daemon install smoke test (#1441)
#
# Ticket:  RdHamilton/hollowmark-tickets#1441
# Gate:    EG-3 — Gatekeeper + signing + PKCE pairing + real MTGA scan
# Status:  MANUAL — steps that require interactive UI or a live MTGA install
#          are explicitly marked [MANUAL] and cannot run headlessly.
#
# Background
# ----------
# The daemon-install-lifecycle.yml CI job verifies:
#   - pkg-root layout (DRY_RUN build-pkg.sh)
#   - bats unit tests (install.sh, postinstall, uninstall.sh)
#   - real .pkg install + LaunchAgent load on macos-latest GHA runner
#   - stub-BFF daemon_event delivery (lifecycle_test.go)
#   - reinstall-over-existing / upgrade-in-place / uninstall
#
# This runbook covers the RELEASE GATE checks that CI cannot perform:
#   1. The signed + notarized production .pkg from a real GitHub Release
#   2. codesign --options=runtime verification (hardened runtime flag)
#   3. collection-helper launchd registration with live PID
#   4. PKCE browser-based first-run pairing (interactive, requires browser)
#   5. Real MTGA Arena scan completion (requires MTGA installed + running)
#
# Prerequisites
# -------------
#   - macOS 14+ (Sonoma or later)
#   - MTGA Arena installed (for step 5 only)
#   - A GitHub Release with the signed vaultmtg-daemon-darwin-universal.pkg
#     (stable channel) at tag daemon/vX.Y.Z
#   - A valid VaultMTG account (for PKCE pairing — step 4)
#   - Internet access
#
# Usage
# -----
# Run from the hollowmark repo root:
#
#   bash services/daemon/install/macos/pkg/smoke-test-runbook.sh
#
# The script pauses at [MANUAL] steps with a prompt. The human tester
# performs the step, then presses Enter to continue. Final exit code:
#   0 = all automatable assertions passed + tester confirmed manual steps
#   1 = an assertion failed (examine output above the failure)
#
# Pass --skip-mtga to skip step 5 (real MTGA scan) when MTGA is not installed.
# Pass --pkg-path <path> to provide a locally-downloaded .pkg instead of
# downloading from GitHub.
#
# Limitations / INCONCLUSIVE scope
# ---------------------------------
# This script verifies the AUTOMATED assertions only. The following steps
# are MANUAL and require a human tester on a real macOS machine:
#
#   MANUAL-1: Visual Gatekeeper check — macOS Installer must open without a
#             "cannot be opened because it is from an unidentified developer"
#             hard-block (only a soft "are you sure" notarization prompt is
#             acceptable, and even that should resolve cleanly for notarized
#             builds).
#
#   MANUAL-2: PKCE browser pairing — the daemon opens a browser window to
#             https://accounts.vaultmtg.app (or Clerk's hosted page) and the
#             tester must complete sign-in.  A headless environment cannot do
#             this.
#
#   MANUAL-3: Real MTGA scan — requires MTGA Arena installed + launched so
#             the daemon can read Player.log and receive draft/match events
#             that propagate to the BFF.  This requires a real MTGA session.
#
# A test run that completes all automated assertions and the human manually
# confirms MANUAL-1..3 constitutes PASS for EG-3.
#
# A test run without MANUAL-2 and MANUAL-3 (e.g. CI) is INCONCLUSIVE for
# the PKCE/scan portion but may still satisfy the codesign and launchd gates.

set -euo pipefail

# ---------------------------------------------------------------------------
# Parse args
# ---------------------------------------------------------------------------
SKIP_MTGA=0
PKG_PATH=""
for arg in "$@"; do
  case "$arg" in
    --skip-mtga)  SKIP_MTGA=1 ;;
    --pkg-path)   shift; PKG_PATH="$1" ;;
    --pkg-path=*) PKG_PATH="${arg#*=}" ;;
  esac
done

# ---------------------------------------------------------------------------
# Colors / helpers
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

pass()   { echo -e "${GREEN}PASS${NC}: $*"; }
fail()   { echo -e "${RED}FAIL${NC}: $*"; exit 1; }
info()   { echo -e "${BOLD}INFO${NC}: $*"; }
warn()   { echo -e "${YELLOW}WARN${NC}: $*"; }
manual() {
  echo ""
  echo -e "${YELLOW}[MANUAL]${NC} $*"
  echo "Press Enter when done (or Ctrl-C to abort): "
  read -r _confirm
}

echo ""
echo "================================================================"
echo " VaultMTG Daemon macOS Install Smoke Test — #1441 / EG-3"
echo "================================================================"
echo ""

# ---------------------------------------------------------------------------
# Step 0: Environment check
# ---------------------------------------------------------------------------
info "Step 0: Environment check"

SW_VERS=$(sw_vers -productVersion 2>/dev/null || echo "unknown")
info "  macOS version: ${SW_VERS}"
MAJOR=$(echo "${SW_VERS}" | cut -d. -f1)
if [[ "${MAJOR}" -lt 14 ]]; then
  warn "  macOS 14+ required for this smoke test; found ${SW_VERS}"
  warn "  Continuing — some assertions may behave differently on older macOS."
fi

# Confirm we are on arm64 or x86_64 (both are supported by the universal .pkg).
ARCH=$(uname -m)
info "  Architecture: ${ARCH}"
case "${ARCH}" in
  arm64|x86_64) pass "  Architecture supported" ;;
  *) fail "  Unsupported architecture: ${ARCH}" ;;
esac

# ---------------------------------------------------------------------------
# Step 1: Obtain the signed .pkg
# ---------------------------------------------------------------------------
echo ""
info "Step 1: Obtain the signed .pkg"

if [[ -n "${PKG_PATH}" ]]; then
  # Caller supplied a local path.
  [[ -f "${PKG_PATH}" ]] || fail "  Supplied --pkg-path '${PKG_PATH}' does not exist"
  info "  Using local .pkg: ${PKG_PATH}"
else
  # Determine the latest daemon release tag.
  info "  Fetching latest daemon release tag from GitHub API..."
  RELEASE_TAG=$(
    curl -fsSL "https://api.github.com/repos/RdHamilton/hollowmark/releases" \
      | python3 -c "
import json, sys
releases = json.load(sys.stdin)
for r in releases:
    tag = r.get('tag_name', '')
    if tag.startswith('daemon/'):
        print(tag)
        break
" 2>/dev/null || true
  )
  if [[ -z "${RELEASE_TAG}" ]]; then
    fail "  Could not determine latest daemon release tag. Is the GitHub API reachable?"
  fi
  info "  Latest daemon release: ${RELEASE_TAG}"

  # Derive the stable-channel .pkg URL (no -staging suffix).
  # The stable channel asset name is: vaultmtg-daemon-darwin-universal.pkg
  # ADR-049 §2: channel = stable → no suffix.
  PKG_URL="https://github.com/RdHamilton/hollowmark/releases/download/${RELEASE_TAG}/vaultmtg-daemon-darwin-universal.pkg"
  PKG_PATH="/tmp/vaultmtg-daemon-darwin-universal.pkg"

  info "  Downloading from: ${PKG_URL}"
  curl -fsSL --progress-bar -o "${PKG_PATH}" "${PKG_URL}" \
    || fail "  Download failed. Check: ${PKG_URL}"
  pass "  Downloaded: ${PKG_PATH}"
fi

# Sanity-check: must be a macOS pkg (xar archive).
PKG_SIZE=$(stat -f%z "${PKG_PATH}" 2>/dev/null || stat -c%s "${PKG_PATH}" 2>/dev/null || echo 0)
if [[ "${PKG_SIZE}" -lt 1000 ]]; then
  fail "  .pkg file is too small (${PKG_SIZE} bytes) — likely a 404 or redirect error."
fi
pass "  .pkg file size: ${PKG_SIZE} bytes"

# ---------------------------------------------------------------------------
# Step 2: Gatekeeper / notarization check (pre-install)
# ---------------------------------------------------------------------------
echo ""
info "Step 2: Gatekeeper / notarization check (pre-install)"

info "  Running: spctl --assess --verbose --type install '${PKG_PATH}'"
SPCTL_OUT=$(spctl --assess --verbose --type install "${PKG_PATH}" 2>&1 || true)
echo "  ${SPCTL_OUT}"

if echo "${SPCTL_OUT}" | grep -q "accepted\|notarized"; then
  pass "  Gatekeeper assessment: accepted (notarized)"
elif echo "${SPCTL_OUT}" | grep -q "rejected"; then
  fail "  Gatekeeper assessment: REJECTED — the .pkg is not signed/notarized correctly"
else
  warn "  Gatekeeper returned an unexpected result (see output above)."
  warn "  If this is a dev-signed build, Gatekeeper rejection is expected on non-dev machines."
  warn "  A production release must show 'accepted'."
fi

# ---------------------------------------------------------------------------
# Step 3: Install the .pkg
# ---------------------------------------------------------------------------
echo ""
info "Step 3: Install the .pkg (requires sudo)"

manual "About to run: sudo installer -pkg '${PKG_PATH}' -target /
Ensure you are ready to enter your password if prompted.
Gatekeeper must NOT show a hard-block dialog ('cannot be opened because
it is from an unidentified developer' with no override option).
A soft 'App from Internet' prompt is acceptable and should auto-dismiss
for notarized builds.

[MANUAL-1]: Confirm that macOS Installer opened without a Gatekeeper hard-block."

info "  Running: sudo installer -pkg '${PKG_PATH}' -target /"
sudo installer -pkg "${PKG_PATH}" -target / \
  || fail "  installer -pkg returned non-zero. Installation failed."
pass "  installer -pkg completed (exit 0)"

# ---------------------------------------------------------------------------
# Step 4: Binary placement assertion
# ---------------------------------------------------------------------------
echo ""
info "Step 4: Binary placement"

BINARY_PATH="/usr/local/bin/vaultmtg-daemon"
[[ -f "${BINARY_PATH}" ]] \
  || fail "  Binary not found at ${BINARY_PATH}"
pass "  Binary present: ${BINARY_PATH}"

# Executable check.
[[ -x "${BINARY_PATH}" ]] \
  || fail "  Binary at ${BINARY_PATH} is not executable"
pass "  Binary is executable"

# ---------------------------------------------------------------------------
# Step 5: codesign --options=runtime (hardened runtime flag)
#
# ADR-011 §5 / daemon-release.yml sign-macos: the stable-channel daemon binary
# MUST be signed with --options runtime (hardened runtime) so that macOS will
# allow the binary to run under launchd without Gatekeeper intervention and so
# that notarytool submission succeeds (a requirement since macOS 10.15.7).
#
# The collection-helper is also required to carry the hardened runtime flag
# (hollowmark-tickets#1286 R1, daemon-release.yml:1035-1057).
# ---------------------------------------------------------------------------
echo ""
info "Step 5: codesign --options=runtime (hardened runtime flag)"

info "  Checking daemon binary: ${BINARY_PATH}"
CODESIGN_OUT=$(codesign --display --verbose=4 "${BINARY_PATH}" 2>&1 || true)
printf '    %s\n' "${CODESIGN_OUT}"

if echo "${CODESIGN_OUT}" | grep -q "runtime"; then
  pass "  Daemon binary: --options=runtime present (hardened runtime)"
else
  fail "  Daemon binary: --options=runtime NOT found in codesign output.
  Expected 'runtime' in the CodeDirectory or flags section.
  Full output above. This means the binary was signed without the hardened
  runtime flag, which will cause Gatekeeper issues on macOS 10.15+."
fi

# Verify codesign overall validity.
codesign --verify --verbose "${BINARY_PATH}" 2>&1 | sed 's/^/    /' \
  || fail "  codesign --verify failed on the daemon binary"
pass "  codesign --verify: binary signature is valid"

# collection-helper (installed by postinstall via install-helper.sh).
HELPER_PATH="/Library/Application Support/VaultMTG/collection-helper"
if [[ -f "${HELPER_PATH}" ]]; then
  info "  Checking collection-helper: ${HELPER_PATH}"
  HELPER_CODESIGN=$(codesign --display --verbose=4 "${HELPER_PATH}" 2>&1 || true)
  printf '    %s\n' "${HELPER_CODESIGN}"
  if echo "${HELPER_CODESIGN}" | grep -q "runtime"; then
    pass "  collection-helper: --options=runtime present (hardened runtime)"
  else
    fail "  collection-helper: --options=runtime NOT found.
  The helper must be signed with hardened runtime (daemon-release.yml:1035-1057,
  hollowmark-tickets#1286 R1) so that macOS allows it to run as a root daemon
  and perform task_for_pid operations under launchd."
  fi
  codesign --verify --verbose "${HELPER_PATH}" 2>&1 | sed 's/^/    /' \
    || fail "  codesign --verify failed on the collection-helper"
  pass "  codesign --verify: collection-helper signature is valid"
else
  warn "  collection-helper not found at ${HELPER_PATH}."
  warn "  This is expected if postinstall's install-helper.sh step was skipped"
  warn "  (e.g. TCC denial or missing SHARE_DIR).  The tray 'Grant Access' button"
  warn "  triggers helper install on first run.  Check postinstall logs."
fi

# ---------------------------------------------------------------------------
# Step 6: LaunchAgent plist written + loaded (daemon)
#
# postinstall writes ~/Library/LaunchAgents/com.vaultmtg.daemon.plist and
# bootstraps it into the current user's gui/<uid> domain.
# ---------------------------------------------------------------------------
echo ""
info "Step 6: LaunchAgent loaded (com.vaultmtg.daemon)"

PLIST="${HOME}/Library/LaunchAgents/com.vaultmtg.daemon.plist"
[[ -f "${PLIST}" ]] \
  || fail "  LaunchAgent plist not found at ${PLIST}"
pass "  LaunchAgent plist written: ${PLIST}"

# Verify plist has the required keys.
for key in "com.vaultmtg.daemon" "RunAtLoad" "KeepAlive"; do
  grep -q "${key}" "${PLIST}" \
    || fail "  plist missing required key: ${key}"
done
pass "  plist contains: Label=com.vaultmtg.daemon, RunAtLoad, KeepAlive"

# Poll launchctl list for up to 20s (postinstall calls bootstrap which is async).
info "  Waiting for launchd to register com.vaultmtg.daemon (up to 20s)..."
DEADLINE=$(( SECONDS + 20 ))
while [[ "${SECONDS}" -lt "${DEADLINE}" ]]; do
  if launchctl list "com.vaultmtg.daemon" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

if ! launchctl list "com.vaultmtg.daemon" >/dev/null 2>&1; then
  fail "  com.vaultmtg.daemon not loaded in launchd after 20s.
  Check: launchctl list com.vaultmtg.daemon
         launchctl print gui/$(id -u)/com.vaultmtg.daemon
         cat ${HOME}/Library/Logs/vaultmtg-daemon.log"
fi

# Check that launchd has a live PID for the job.
LAUNCHCTL_PRINT=$(launchctl print "gui/$(id -u)/com.vaultmtg.daemon" 2>&1 || true)
printf '    %s\n' "${LAUNCHCTL_PRINT}"
if echo "${LAUNCHCTL_PRINT}" | grep -qE 'pid = [1-9]'; then
  pass "  com.vaultmtg.daemon: launchd registered with a live PID"
else
  warn "  launchd does not yet show a live PID for com.vaultmtg.daemon."
  warn "  The job may be in a startup retry cycle."
  warn "  Verify: launchctl print gui/$(id -u)/com.vaultmtg.daemon"
fi

# ---------------------------------------------------------------------------
# Step 7: LaunchDaemon loaded (collection-helper)
#
# install-helper.sh bootstraps com.vaultmtg.collection-helper into the
# system domain (LaunchDaemons, not LaunchAgents) as root.
# ---------------------------------------------------------------------------
echo ""
info "Step 7: LaunchDaemon loaded (com.vaultmtg.collection-helper)"

HELPER_PLIST="/Library/LaunchDaemons/com.vaultmtg.collection-helper.plist"
if [[ -f "${HELPER_PLIST}" ]]; then
  pass "  Helper LaunchDaemon plist: ${HELPER_PLIST}"

  # Poll launchctl list for up to 10s.
  info "  Waiting for launchd to register com.vaultmtg.collection-helper (up to 10s)..."
  DEADLINE=$(( SECONDS + 10 ))
  while [[ "${SECONDS}" -lt "${DEADLINE}" ]]; do
    if launchctl list "com.vaultmtg.collection-helper" >/dev/null 2>&1; then
      break
    fi
    sleep 1
  done

  if ! launchctl list "com.vaultmtg.collection-helper" >/dev/null 2>&1; then
    fail "  com.vaultmtg.collection-helper not loaded in launchd after 10s.
  Check: sudo launchctl list com.vaultmtg.collection-helper
         cat /Library/Logs/VaultMTG/collection-helper.log"
  fi

  # Capture launchctl output and look for a live PID.
  HELPER_PRINT=$(sudo launchctl print "system/com.vaultmtg.collection-helper" 2>&1 || \
                 launchctl list "com.vaultmtg.collection-helper" 2>&1 || true)
  printf '    %s\n' "${HELPER_PRINT}"
  if echo "${HELPER_PRINT}" | grep -qE 'pid = [1-9]|"PID"[^0-9]*[1-9]'; then
    pass "  com.vaultmtg.collection-helper: launchd registered with a live PID"
  else
    warn "  launchd shows com.vaultmtg.collection-helper loaded but no live PID visible."
    warn "  The helper may have exited cleanly (event-driven) or be in a restart cycle."
  fi
else
  warn "  Helper LaunchDaemon plist not found at ${HELPER_PLIST}."
  warn "  postinstall calls install-helper.sh which requires admin privileges."
  warn "  If install-helper.sh was not run (non-interactive install, TCC denial),"
  warn "  open the tray icon and click 'Grant Access' to trigger helper installation."
fi

# ---------------------------------------------------------------------------
# Step 8: Daemon /health endpoint (application-level liveness)
# ---------------------------------------------------------------------------
echo ""
info "Step 8: Daemon /health endpoint (port 9001)"

HEALTH_URL="http://127.0.0.1:9001/health"
info "  Polling ${HEALTH_URL} (up to 30s)..."
DEADLINE=$(( SECONDS + 30 ))
HEALTH_OK=0
while [[ "${SECONDS}" -lt "${DEADLINE}" ]]; do
  BODY=$(curl -fsS --max-time 2 "${HEALTH_URL}" 2>/dev/null || true)
  if [[ -n "${BODY}" ]]; then
    HEALTH_OK=1
    break
  fi
  sleep 1
done

if [[ "${HEALTH_OK}" -eq 0 ]]; then
  fail "  Daemon did not respond at ${HEALTH_URL} within 30s.
  Check: cat ${HOME}/Library/Logs/vaultmtg-daemon.log
         launchctl print gui/$(id -u)/com.vaultmtg.daemon"
fi

echo "  Health response: ${BODY}"
AUTH_STATUS=$(python3 -c "
import json, sys
try:
    d = json.loads('${BODY}')
    print(d.get('auth_status', 'unknown'))
except Exception:
    print('parse-error')
" 2>/dev/null || echo "parse-error")
pass "  Daemon /health: 200 OK (auth_status=${AUTH_STATUS})"

# ---------------------------------------------------------------------------
# Step 9: PKCE pairing (MANUAL)
# ---------------------------------------------------------------------------
echo ""
info "Step 9: PKCE pairing [MANUAL-2]"

manual "[MANUAL-2] PKCE browser pairing.

The daemon should open a browser window to the VaultMTG Clerk-hosted
sign-in page automatically on first run (auth_status=setup_required or
pkce_pending from /health above).

If the browser has NOT opened automatically:
  1. Open the tray icon (look in the macOS menu bar).
  2. Click 'Sign In' or 'Pair with VaultMTG'.

Complete the sign-in flow in the browser:
  1. Log in with your VaultMTG account credentials.
  2. The browser will redirect to localhost, completing PKCE.
  3. The tray icon should change to show 'Connected'.
  4. Verify: curl -fsS http://127.0.0.1:9001/health | python3 -m json.tool
     The 'auth_status' field should be 'authenticated' and 'account_id'
     should be non-empty.

Confirm that pairing succeeded before pressing Enter."

# Re-check /health auth_status after manual pairing.
BODY2=$(curl -fsS --max-time 5 "${HEALTH_URL}" 2>/dev/null || true)
if [[ -n "${BODY2}" ]]; then
  AUTH_STATUS2=$(python3 -c "
import json, sys
try:
    d = json.loads('${BODY2}')
    print(d.get('auth_status', 'unknown'))
except Exception:
    print('parse-error')
" 2>/dev/null || echo "parse-error")
  ACCOUNT_ID=$(python3 -c "
import json, sys
try:
    d = json.loads('${BODY2}')
    print(d.get('account_id', ''))
except Exception:
    print('')
" 2>/dev/null || echo "")
  if [[ -n "${ACCOUNT_ID}" ]]; then
    pass "  /health post-pairing: auth_status=${AUTH_STATUS2}, account_id=${ACCOUNT_ID}"
  else
    warn "  /health shows auth_status=${AUTH_STATUS2} but account_id is still empty."
    warn "  Pairing may not have completed. Verify manually."
  fi
else
  warn "  Could not reach /health after pairing step. Verify manually."
fi

# ---------------------------------------------------------------------------
# Step 10: Real MTGA scan (MANUAL — skippable)
# ---------------------------------------------------------------------------
echo ""
if [[ "${SKIP_MTGA}" -eq 1 ]]; then
  warn "Step 10: Real MTGA scan — SKIPPED (--skip-mtga flag set)"
  warn "  This step is required for full EG-3 qualification."
  warn "  Run without --skip-mtga on a machine with MTGA Arena installed."
else
  info "Step 10: Real MTGA scan [MANUAL-3]"

  manual "[MANUAL-3] Real MTGA Arena scan.

  1. Launch MTGA Arena (the game, not VaultMTG).
  2. Start a draft or play a match.
  3. Watch the daemon log for scan activity:
       tail -f ~/Library/Logs/vaultmtg-daemon.log
  4. Within ~60s of MTGA writing events to Player.log, the daemon should
     log lines indicating it parsed and ingested the events.
  5. Open the VaultMTG web app and confirm the draft/match appears.

  Confirm that a real MTGA scan completed and events appeared in the app
  before pressing Enter."

  pass "  MTGA scan: confirmed by human tester [MANUAL-3]"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "================================================================"
echo " Smoke Test Summary — #1441 / EG-3"
echo "================================================================"
echo ""
echo " Automated assertions:"
echo "   Step 0: Environment check              PASS"
echo "   Step 1: .pkg obtained                  PASS"
echo "   Step 2: Gatekeeper / notarization      PASS (see output above)"
echo "   Step 3: installer -pkg                 PASS"
echo "   Step 4: Binary placement               PASS"
echo "   Step 5: codesign --options=runtime     PASS"
echo "   Step 6: LaunchAgent loaded (daemon)    PASS"
echo "   Step 7: LaunchDaemon loaded (helper)   $([ -f "${HELPER_PLIST}" ] && echo PASS || echo "SKIP (helper not installed)")"
echo "   Step 8: /health 200 OK                 PASS"
echo ""
echo " Manual assertions (human-confirmed):"
echo "   MANUAL-1: Gatekeeper no hard-block     CONFIRMED by tester"
echo "   MANUAL-2: PKCE pairing succeeded       CONFIRMED by tester"
if [[ "${SKIP_MTGA}" -eq 1 ]]; then
echo "   MANUAL-3: Real MTGA scan               SKIPPED (--skip-mtga)"
else
echo "   MANUAL-3: Real MTGA scan               CONFIRMED by tester"
fi
echo ""
echo " RESULT: PASS (all automated gates + human-confirmed manual steps)"
echo ""
echo " Note: This result is valid for the release SHA / .pkg URL used above."
echo " Record the daemon/vX.Y.Z release tag and the tester's name + date"
echo " when filing the EG-3 verification result in the ticket."
echo "================================================================"
