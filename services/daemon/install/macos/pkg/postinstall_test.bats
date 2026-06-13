#!/usr/bin/env bats
# postinstall_test.bats — tests for services/daemon/install/macos/pkg/postinstall
#
# These tests verify the LaunchAgent plist written by postinstall contains all
# required keys, including the env vars introduced in issue #2127.
#
# Strategy:
#   - We produce a test-variant of postinstall using sed that:
#     (a) replaces build-time __PLACEHOLDER__ values with real-looking values
#     (b) overrides PLIST_DIR and CONFIG_DIR to point at BATS_TEST_TMPDIR
#         so we can inspect written files without touching the real ~
#   - OS-level privileged calls (xattr, launchctl, install -o) are stubbed.
#   - SUDO_USER is set to the real invoking user so tilde expansion in
#     "eval echo ~$REAL_USER" resolves to a real path (then overridden).
#
# Run with:
#   bats services/daemon/install/macos/pkg/postinstall_test.bats

POSTINSTALL_SCRIPT="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/postinstall"
REAL_USER="$(whoami)"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

# Build a stub directory whose executables replace privileged OS tools.
_make_stub_dir() {
  local stub_dir
  stub_dir="$(mktemp -d)"

  # xattr — no-op (cannot clear quarantine in test)
  cat > "${stub_dir}/xattr" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "${stub_dir}/xattr"

  # launchctl — record calls, always succeed
  cat > "${stub_dir}/launchctl" <<'EOF'
#!/usr/bin/env bash
echo "stub-launchctl: $*" >&2
touch "${BATS_TEST_TMPDIR}/launchctl_called"
exit 0
EOF
  chmod +x "${stub_dir}/launchctl"

  # install — strip -o <owner> (requires root); perform operation unprivileged.
  # Supported forms used by postinstall:
  #   install -d -m 755 -o <user> <dir>
  #   install -m 600 -o <user> /dev/null <file>
  #   install -m 644 -o <user> /dev/null <file>
  cat > "${stub_dir}/install" <<'EOF'
#!/usr/bin/env bash
positional=()
skip_next=0
is_dir=0
for arg in "$@"; do
  if [[ "${skip_next}" == "1" ]]; then
    skip_next=0
    continue
  fi
  case "${arg}" in
    -o) skip_next=1 ;;
    -m) skip_next=1 ;;
    -d) is_dir=1 ;;
    *)  positional+=("${arg}") ;;
  esac
done
count="${#positional[@]}"
last_idx=$(( count - 1 ))
target="${positional[${last_idx}]}"
if [[ "${is_dir}" == "1" ]]; then
  mkdir -p "${target}"
else
  mkdir -p "$(dirname "${target}")"
  touch "${target}"
fi
EOF
  chmod +x "${stub_dir}/install"

  # curl — default stub returns a healthy /health response so existing tests
  # (which test plist content, not health-check behaviour) continue to pass.
  # Health-check-specific tests override this stub in their own setup.
  cat > "${stub_dir}/curl" <<'EOF'
#!/usr/bin/env bash
echo '{"status":"ok","account_id":"user_stub","auth_status":"authenticated"}'
EOF
  chmod +x "${stub_dir}/curl"

  # sleep — no-op so tests do not incur the real delay between retries.
  cat > "${stub_dir}/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "${stub_dir}/sleep"

  # pkill — no-op (no real daemon process to kill in tests)
  cat > "${stub_dir}/pkill" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "${stub_dir}/pkill"

  echo "${stub_dir}"
}

# Produce a test-variant of postinstall with:
#   - all __PLACEHOLDER__ values substituted
#   - REAL_HOME redirected to TEST_DIR so all home-relative paths (PLIST_DIR,
#     CONFIG_DIR, LOG_FILE) are written to an inspectable BATS_TEST_TMPDIR
#     location without touching the real ~.
# Optional channel parameter defaults to "stable"; pass "staging" to test the
# staging install identity (ADR-049 §2, ticket #664).
_make_test_script() {
  local dest="$1"
  local test_dir="$2"
  local cloud_url="${3:-https://staging-api.vaultmtg.app/api/v1}"
  local clerk_api="${4:-https://settled-martin-99.clerk.accounts.dev}"
  local clerk_key="${5:-pk_test_abc123}"
  local clerk_client="${6:-oauth_testclient}"
  local channel="${7:-stable}"

  sed \
    -e "s|__VAULTMTG_CLOUD_API_URL__|${cloud_url}|g" \
    -e "s|__CLERK_FRONTEND_API__|${clerk_api}|g" \
    -e "s|__CLERK_PUBLISHABLE_KEY__|${clerk_key}|g" \
    -e "s|__CLERK_OAUTH_CLIENT_ID__|${clerk_client}|g" \
    -e "s|__VAULTMTG_CHANNEL__|${channel}|g" \
    -e "s|REAL_HOME=\$(eval echo \"~\${REAL_USER}\")|REAL_HOME=\"${test_dir}\"|g" \
    "${POSTINSTALL_SCRIPT}" > "${dest}"
  chmod +x "${dest}"
}

# ---------------------------------------------------------------------------
# Shared setup
# ---------------------------------------------------------------------------

setup() {
  export BATS_TEST_TMPDIR
  TEST_DIR="${BATS_TEST_TMPDIR}/postinstall-$$"
  mkdir -p "${TEST_DIR}"
  TMP_SCRIPT="${BATS_TEST_TMPDIR}/postinstall-subst-$$"
  STUB_DIR="$(_make_stub_dir)"
  # Default test script uses stable channel.
  _make_test_script "${TMP_SCRIPT}" "${TEST_DIR}"

  # Stable channel: no suffix — com.vaultmtg.daemon / .vaultmtg.
  # postinstall writes PLIST_DIR="${REAL_HOME}/Library/LaunchAgents" (now TEST_DIR).
  PLIST_PATH="${TEST_DIR}/Library/LaunchAgents/com.vaultmtg.daemon.plist"
  CONFIG_FILE="${TEST_DIR}/.vaultmtg/daemon.json"
}

# ---------------------------------------------------------------------------
# 1. Plist contains VAULTMTG_DAEMON_CLOUD_API_URL (issue #2127 + #2564 regression test).
#    The canonical env var name is VAULTMTG_DAEMON_*; the daemon's EnvWithFallback
#    shim still reads MTGA_DAEMON_* for existing legacy installs, but new
#    installs must inject the canonical name (#2564).
# ---------------------------------------------------------------------------
@test "plist: VAULTMTG_DAEMON_CLOUD_API_URL key is present with correct value" {
  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [ -f "${PLIST_PATH}" ]

  grep -q "VAULTMTG_DAEMON_CLOUD_API_URL" "${PLIST_PATH}"
  grep -q "staging-api.vaultmtg.app/api/v1" "${PLIST_PATH}"
  # Guard: must not perpetuate the legacy name in new installs (#2564).
  ! grep -q "<key>MTGA_DAEMON_CLOUD_API_URL</key>" "${PLIST_PATH}"
}

# ---------------------------------------------------------------------------
# 2. Plist contains ThrottleInterval (issue #2127 — prevent restart storm)
# ---------------------------------------------------------------------------
@test "plist: ThrottleInterval key is present with value 10" {
  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [ -f "${PLIST_PATH}" ]

  grep -q "ThrottleInterval" "${PLIST_PATH}"
  grep -q "<integer>10</integer>" "${PLIST_PATH}"
}

# ---------------------------------------------------------------------------
# 3. Plist contains all Clerk EnvironmentVariables keys
# ---------------------------------------------------------------------------
@test "plist: all Clerk EnvironmentVariables keys are present" {
  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [ -f "${PLIST_PATH}" ]

  grep -q "CLERK_FRONTEND_API" "${PLIST_PATH}"
  grep -q "CLERK_PUBLISHABLE_KEY" "${PLIST_PATH}"
  grep -q "CLERK_OAUTH_CLIENT_ID" "${PLIST_PATH}"
}

# ---------------------------------------------------------------------------
# 4. Plist contains VAULTMTG_DAEMON_CHANNEL with the substituted channel value
#    (ADR-049 §1 — wired by #2894; regression guard so CI detects a missing
#    __VAULTMTG_CHANNEL__ substitution in the lifecycle workflow).
# ---------------------------------------------------------------------------
@test "plist: VAULTMTG_DAEMON_CHANNEL key is present with substituted value" {
  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [ -f "${PLIST_PATH}" ]

  grep -q "VAULTMTG_DAEMON_CHANNEL" "${PLIST_PATH}"
  # _make_test_script substitutes __VAULTMTG_CHANNEL__ -> "stable" (default arg).
  grep -q "<string>stable</string>" "${PLIST_PATH}"
  # Guard: the raw placeholder must not survive into the plist.
  ! grep -q "__VAULTMTG_CHANNEL__" "${PLIST_PATH}"
}

# ---------------------------------------------------------------------------
# 4b. Plist contains KeepAlive=true and RunAtLoad=true
# ---------------------------------------------------------------------------
@test "plist: KeepAlive and RunAtLoad are set to true" {
  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [ -f "${PLIST_PATH}" ]

  grep -q "KeepAlive" "${PLIST_PATH}"
  grep -q "RunAtLoad" "${PLIST_PATH}"
}

# ---------------------------------------------------------------------------
# 5. Placeholder validation — script exits 1 when substitution did not happen
# ---------------------------------------------------------------------------
@test "placeholder check: exits 1 when build-time placeholders are not replaced" {
  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${POSTINSTALL_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 1 ]
  [[ "${output}" == *"build-time substitution did not run"* ]]
}

# ---------------------------------------------------------------------------
# 6. daemon.json is written on first install with cloud_api_url
# ---------------------------------------------------------------------------
@test "daemon.json: written on fresh install with cloud_api_url" {
  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [ -f "${CONFIG_FILE}" ]
  grep -q "staging-api.vaultmtg.app/api/v1" "${CONFIG_FILE}"
}

# ---------------------------------------------------------------------------
# 7. daemon.json: same-env reinstall — non-auth fields preserved; auth fields cleared
#
#    When the existing cloud_api_url matches the baked-in value (same-env
#    reinstall), cloud_api_url and non-auth fields are retained.
#    Auth fields (keychain, api_key, daemon_jwt) are unconditionally cleared
#    by §2b so the daemon's ProbeTokenLiveness probe runs on next launch
#    (#1330 stale-auth-clear, 2026-06-12 incident fix).
# ---------------------------------------------------------------------------
@test "daemon.json: same-env reinstall preserves non-auth fields and clears auth fields" {
  mkdir -p "${TEST_DIR}/.vaultmtg"
  python3 -c "
import json
print(json.dumps({
    'cloud_api_url': 'https://staging-api.vaultmtg.app/api/v1',
    'api_key': 'my-existing-key',
    'sync_enabled': True,
    'account_id': 'user_abc'
}, indent=2))
" > "${CONFIG_FILE}"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [ -f "${CONFIG_FILE}" ]

  # cloud_api_url and non-auth fields are preserved.
  python3 -c "
import json
with open('${CONFIG_FILE}') as f:
    d = json.load(f)
assert d['cloud_api_url'] == 'https://staging-api.vaultmtg.app/api/v1', \
    'FAIL: cloud_api_url changed on same-env reinstall'
assert d.get('sync_enabled') == True, \
    'FAIL: sync_enabled not preserved'
assert d.get('account_id') == 'user_abc', \
    'FAIL: account_id not preserved'
# Auth fields must be cleared by §2b stale-auth-clear (#1330).
assert not d.get('api_key'), \
    'FAIL: api_key not cleared by stale-auth-clear: ' + repr(d.get('api_key'))
print('PASS: non-auth fields preserved; api_key cleared by stale-auth-clear')
"
  [[ "${output}" == *"cloud_api_url unchanged"* ]]
  [[ "${output}" == *"stale auth cleared"* ]]
}

# ---------------------------------------------------------------------------
# 8. Reinstall: bootout is attempted before bootstrap (stop before reload)
# ---------------------------------------------------------------------------
@test "reinstall: bootout is called before bootstrap on reinstall" {
  # Run the script twice in sequence to simulate a reinstall.
  # After both runs, launchctl should have been called at least twice:
  # once for bootout and once for bootstrap. We verify the stub was invoked
  # and that both "bootout" and "bootstrap" appear in its call log.

  local call_log="${BATS_TEST_TMPDIR}/launchctl_calls"

  # Override the launchctl stub to log every invocation with its arguments.
  cat > "${STUB_DIR}/launchctl" <<'EOF'
#!/usr/bin/env bash
echo "$*" >> "${BATS_TEST_TMPDIR}/launchctl_calls"
exit 0
EOF
  chmod +x "${STUB_DIR}/launchctl"

  # First install
  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"
  [ "${status}" -eq 0 ]

  # Second install (reinstall)
  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"
  [ "${status}" -eq 0 ]

  # The call log must contain both "bootout" and "bootstrap" invocations.
  grep -q "bootout" "${call_log}"
  grep -q "bootstrap" "${call_log}"

  # bootout must appear before the final bootstrap in the log.
  local bootout_line bootstrap_line
  bootout_line=$(grep -n "bootout" "${call_log}" | tail -1 | cut -d: -f1)
  bootstrap_line=$(grep -n "bootstrap" "${call_log}" | tail -1 | cut -d: -f1)
  [ "${bootout_line}" -lt "${bootstrap_line}" ]
}

# ---------------------------------------------------------------------------
# 9. Postinstall echoes the uninstall path using the SHARE_DIR constant
# ---------------------------------------------------------------------------
@test "postinstall: output contains uninstall echo referencing /usr/local/share/vaultmtg/uninstall.sh" {
  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [[ "${output}" == *"To uninstall: sudo /usr/local/share/vaultmtg/uninstall.sh"* ]]
}

# ---------------------------------------------------------------------------
# Liveness-check tests (vault-mtg-tickets#334 — health gate fix).
#
# Gate is now: binary-present + daemon-process-responding (HTTP 200), NOT
# account_id.  First-run installs have no account_id until async PKCE pairing
# completes; requiring it caused PackageKit Code=112 on every fresh install.
#
# Strategy: add a curl stub to STUB_DIR that echoes a configurable JSON body
# or simulates a timeout.  We override STUB_DIR's curl for each test.
# ---------------------------------------------------------------------------

# 10. Liveness check passes when daemon responds with a non-empty account_id
#     (already-paired reinstall path).
@test "liveness check: exits 0 when daemon responds with non-empty account_id" {
  # curl stub returns a healthy JSON body immediately.
  cat > "${STUB_DIR}/curl" <<'EOF'
#!/usr/bin/env bash
echo '{"status":"ok","account_id":"user_abc123","auth_status":"authenticated"}'
EOF
  chmod +x "${STUB_DIR}/curl"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [[ "${output}" == *"daemon healthy"* ]]
  [[ "${output}" == *"post-install liveness check passed"* ]]
}

# 11. First-run install: daemon responds but has no account_id (not yet paired).
#     This is the normal state for every fresh install — must exit 0.
#     (vault-mtg-tickets#334 regression guard)
@test "liveness check: exits 0 when daemon responds without account_id (first-run / not yet paired)" {
  # curl stub returns setup_required state — daemon is running but unpaired.
  cat > "${STUB_DIR}/curl" <<'EOF'
#!/usr/bin/env bash
echo '{"status":"ok","auth_status":"setup_required"}'
EOF
  chmod +x "${STUB_DIR}/curl"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  # Must succeed — pairing is async, missing account_id is not a fatal error.
  [ "${status}" -eq 0 ]
  [[ "${output}" == *"not yet paired"* ]]
  [[ "${output}" == *"post-install liveness check passed"* ]]
}

# 12. Liveness check: daemon never responds — WARNING logged but exits 0.
#     Binary and LaunchAgent are correctly installed; daemon starts at next login.
#     (vault-mtg-tickets#334 — liveness timeout must NOT fail the .pkg)
@test "liveness check: exits 0 with warning when daemon never responds within retry limit" {
  # curl stub always exits non-zero (connection refused simulation).
  # Also stub sleep so the test does not actually wait 15 s.
  cat > "${STUB_DIR}/curl" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "${STUB_DIR}/curl"

  cat > "${STUB_DIR}/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "${STUB_DIR}/sleep"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  # Non-fatal: binary + LaunchAgent installed correctly.
  [ "${status}" -eq 0 ]
  [[ "${output}" == *"WARNING"* ]]
  [[ "${output}" == *"daemon will start automatically at next login"* ]]
}

# 13. Liveness check retries the correct number of times before giving up.
@test "liveness check: makes exactly HEALTH_MAX_ATTEMPTS curl calls before warning" {
  local call_count_file="${BATS_TEST_TMPDIR}/curl_calls"

  cat > "${STUB_DIR}/curl" <<EOF
#!/usr/bin/env bash
count=\$(cat "${call_count_file}" 2>/dev/null || echo 0)
count=\$(( count + 1 ))
echo "\$count" > "${call_count_file}"
exit 1
EOF
  chmod +x "${STUB_DIR}/curl"

  cat > "${STUB_DIR}/sleep" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "${STUB_DIR}/sleep"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  # Non-fatal even after all attempts.
  [ "${status}" -eq 0 ]
  local calls
  calls=$(cat "${call_count_file}")
  echo "curl call count: ${calls}"
  [ "${calls}" -eq 5 ]
}

# ---------------------------------------------------------------------------
# Tests for cross-env reinstall guard (vault-mtg-tickets#194)
# ---------------------------------------------------------------------------

# 14. Cross-env reinstall: when existing cloud_api_url differs from the baked-in
#     value, the keychain entry is cleared and cloud_api_url is updated in-place
#     while other fields (api_key, sync_enabled) are preserved.
@test "cross-env reinstall: keychain cleared and cloud_api_url updated when URL changes" {
  local security_calls="${BATS_TEST_TMPDIR}/security_calls_14"

  # Override security stub to record calls.
  cat > "${STUB_DIR}/security" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "${security_calls}"
exit 0
EOF
  chmod +x "${STUB_DIR}/security"

  # Pre-create config with an OLD URL and extra fields that must be preserved.
  mkdir -p "${TEST_DIR}/.vaultmtg"
  python3 -c "
import json
data = {
  'cloud_api_url': 'https://old-staging-api.example.com/api/v1',
  'api_key': 'my-existing-api-key',
  'sync_enabled': True,
  'account_id': 'user_abc123'
}
print(json.dumps(data, indent=2))
" > "${CONFIG_FILE}"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # Security delete-generic-password must have been called (cross-env + §2b stale-auth-clear).
  # §2 targets KEYCHAIN_SERVICE (com.hollowmark.daemon); §2b also targets com.vaultmtg.daemon.
  [ -f "${security_calls}" ]
  grep -q "delete-generic-password" "${security_calls}"
  grep -q "com.hollowmark.daemon" "${security_calls}"
  grep -q "api-key" "${security_calls}"

  # cloud_api_url must now be the new (baked-in) value.
  python3 -c "
import json
with open('${CONFIG_FILE}') as f:
    d = json.load(f)
assert d['cloud_api_url'] == 'https://staging-api.vaultmtg.app/api/v1', \
    'FAIL: cloud_api_url not updated: ' + repr(d['cloud_api_url'])
print('PASS: cloud_api_url updated to new value')
"

  # Non-auth identity fields (account_id, sync_enabled) are preserved by §2b.
  # Auth fields (api_key, keychain, daemon_jwt) are cleared by §2b stale-auth-clear (#1330).
  python3 -c "
import json
with open('${CONFIG_FILE}') as f:
    d = json.load(f)
assert not d.get('api_key'), \
    'FAIL: api_key not cleared by stale-auth-clear: ' + repr(d.get('api_key'))
assert d.get('sync_enabled') == True, \
    'FAIL: sync_enabled changed: ' + repr(d.get('sync_enabled'))
assert d.get('account_id') == 'user_abc123', \
    'FAIL: account_id changed: ' + repr(d.get('account_id'))
print('PASS: api_key cleared; sync_enabled, account_id preserved after cross-env reinstall')
"

  [[ "${output}" == *"cross-env reinstall detected"* ]]
  [[ "${output}" == *"keychain entry cleared"* ]]
  [[ "${output}" == *"cloud_api_url updated"* ]]
  [[ "${output}" == *"stale auth cleared"* ]]
}

# 15. (Previously "ADR-011-C: same-env reinstall is byte-exact no-op")
#
#     ADR-011-C is superseded by #1330: §2b stale-auth-clear ALWAYS writes
#     daemon.json to remove auth fields, so the SHA WILL change when auth
#     fields are present.  The relevant invariant is now:
#       - Non-auth fields (cloud_api_url, account_id, sync_enabled) are preserved
#       - Auth fields (keychain, api_key, daemon_jwt) are cleared
#       - The file exits valid JSON after postinstall completes
#
#     The byte-exact SHA invariant no longer applies when auth fields were present.
@test "ADR-011-C (updated #1330): same-env reinstall preserves non-auth fields and exits with valid JSON" {
  mkdir -p "${TEST_DIR}/.vaultmtg"
  python3 -c "
import json
data = {
    'cloud_api_url': 'https://staging-api.vaultmtg.app/api/v1',
    'keychain': True,
    'api_key': '',
    'account_id': 'user_adr011c',
    'sync_enabled': True
}
print(json.dumps(data, indent=2))
" > "${CONFIG_FILE}"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # daemon.json must be valid JSON and non-auth fields preserved.
  python3 -c "
import json
with open('${CONFIG_FILE}') as f:
    d = json.load(f)
assert d.get('cloud_api_url') == 'https://staging-api.vaultmtg.app/api/v1', \
    'FAIL: cloud_api_url changed'
assert d.get('account_id') == 'user_adr011c', \
    'FAIL: account_id changed'
assert d.get('sync_enabled') == True, \
    'FAIL: sync_enabled changed'
# Auth fields cleared by §2b.
assert not d.get('keychain'), 'FAIL: keychain not cleared'
assert not d.get('api_key'), 'FAIL: api_key not cleared'
print('PASS: non-auth fields preserved; auth fields cleared; valid JSON')
"
  [[ "${output}" == *"cloud_api_url unchanged"* ]]
  [[ "${output}" == *"stale auth cleared"* ]]
}

@test "same-env reinstall: §2b stale-auth-clear always calls security delete and clears auth fields" {
  # #1330: §2b runs UNCONDITIONALLY — security delete is always called (both service names)
  # and auth fields are always zeroed, even on same-env reinstall.
  # The §2 cross-env guard (cloud_api_url mismatch) is a separate path that does NOT
  # gate the §2b stale-auth-clear.
  local security_calls="${BATS_TEST_TMPDIR}/security_calls_17"

  cat > "${STUB_DIR}/security" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "${security_calls}"
exit 0
EOF
  chmod +x "${STUB_DIR}/security"

  mkdir -p "${TEST_DIR}/.vaultmtg"
  python3 -c "
import json
data = {
  'cloud_api_url': 'https://staging-api.vaultmtg.app/api/v1',
  'keychain': True,
  'api_key': '',
  'sync_enabled': False,
  'account_id': 'user_xyz'
}
print(json.dumps(data, indent=2))
" > "${CONFIG_FILE}"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # §2b always calls security delete for both service names.
  [ -f "${security_calls}" ]
  grep -q "delete-generic-password" "${security_calls}"
  grep -q "com.hollowmark.daemon" "${security_calls}"

  python3 -c "
import json
with open('${CONFIG_FILE}') as f:
    d = json.load(f)
# Auth fields cleared.
assert not d.get('keychain'), 'FAIL: keychain not cleared'
assert not d.get('api_key'), 'FAIL: api_key not cleared'
# Non-auth fields preserved.
assert d.get('sync_enabled') == False, \
    'FAIL: sync_enabled changed: ' + repr(d.get('sync_enabled'))
assert d.get('account_id') == 'user_xyz', \
    'FAIL: account_id changed: ' + repr(d.get('account_id'))
print('PASS: auth fields cleared; non-auth fields preserved')
"

  [[ "${output}" == *"cloud_api_url unchanged"* ]]
}

# ---------------------------------------------------------------------------
# 17. CHANNEL=staging install lands the staging install identity.
#
#     Regression guard for #664: a STAGING .pkg must install with the staging
#     identity so it can coexist with a prod install (no path/label collision).
#
#     Expected outcomes (ADR-049 §2 / common.sh suffix table):
#       binary name  : vaultmtg-daemon-staging
#       LaunchAgent  : com.vaultmtg.daemon.staging.plist
#       config dir   : ${TEST_DIR}/.vaultmtg-staging/daemon.json
#       -config flag : points at ${TEST_DIR}/.vaultmtg-staging/daemon.json
#       CHANNEL env  : VAULTMTG_DAEMON_CHANNEL = staging in plist
#       no legacy    : staging channel has no legacy label to clean up
# ---------------------------------------------------------------------------
@test "staging channel: installs staging identity (binary, label, config dir, -config flag)" {
  local staging_test_dir="${BATS_TEST_TMPDIR}/postinstall-staging-$$"
  mkdir -p "${staging_test_dir}"

  local staging_script="${BATS_TEST_TMPDIR}/postinstall-staging-subst-$$"
  _make_test_script \
    "${staging_script}" \
    "${staging_test_dir}" \
    "https://staging-api.vaultmtg.app/api/v1" \
    "https://settled-martin-99.clerk.accounts.dev" \
    "pk_test_abc123" \
    "oauth_testclient" \
    "staging"

  # Expected staging paths.
  # postinstall writes PLIST_DIR="${REAL_HOME}/Library/LaunchAgents" (now staging_test_dir).
  local staging_plist="${staging_test_dir}/Library/LaunchAgents/com.vaultmtg.daemon.staging.plist"
  local staging_config="${staging_test_dir}/.vaultmtg-staging/daemon.json"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${staging_script}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # LaunchAgent label must be staging variant.
  [ -f "${staging_plist}" ] || \
    { echo "FAIL: staging plist not written at ${staging_plist}"; false; }
  grep -q "com.vaultmtg.daemon.staging" "${staging_plist}" || \
    { echo "FAIL: plist Label is not com.vaultmtg.daemon.staging"; false; }

  # Binary name in ProgramArguments must be the staging binary.
  grep -q "vaultmtg-daemon-staging" "${staging_plist}" || \
    { echo "FAIL: plist ProgramArguments does not reference vaultmtg-daemon-staging"; false; }

  # -config flag must point at the staging config dir.
  grep -q "\.vaultmtg-staging/daemon\.json" "${staging_plist}" || \
    { echo "FAIL: plist -config flag does not point at .vaultmtg-staging/daemon.json"; false; }

  # VAULTMTG_DAEMON_CHANNEL env var in plist must be "staging".
  grep -q "<string>staging</string>" "${staging_plist}" || \
    { echo "FAIL: plist VAULTMTG_DAEMON_CHANNEL is not 'staging'"; false; }

  # Config file must be written to the staging config dir.
  [ -f "${staging_config}" ] || \
    { echo "FAIL: staging daemon.json not written at ${staging_config}"; false; }
  grep -q "staging-api.vaultmtg.app" "${staging_config}" || \
    { echo "FAIL: staging daemon.json missing cloud_api_url"; false; }

  # Staging channel must NOT try to clean up a legacy label.
  [[ "${output}" == *"staging channel — no legacy label to clean up"* ]] || \
    { echo "FAIL: expected staging-channel legacy-skip message"; false; }

  # Confirm no stable-identity paths were created.
  [ ! -f "${staging_test_dir}/Library/LaunchAgents/com.vaultmtg.daemon.plist" ] || \
    { echo "FAIL: stable plist was written during a staging install"; false; }

  echo "PASS: staging channel install landed staging identity"
}

# ---------------------------------------------------------------------------
# collection-agent-helper install in postinstall (R4 — hollowmark-tickets#1286)
#
# postinstall now calls install-helper.sh (sourced from SHARE_DIR) after
# bootstrapping the daemon LaunchAgent.  Key constraints:
#
#   R4: The install-helper.sh call is NON-FATAL.  An error from install-helper.sh
#       must NOT roll back the entire .pkg install (the #334/Code=112 failure
#       class).  postinstall must log a WARNING and continue.
#
#   R6: postinstall logs the installed helper version after a successful install.
#       The version is embedded as HelperVersion in the binary at build time.
#
# Tests use SHARE_DIR override so they can write under BATS_TEST_TMPDIR.
# ---------------------------------------------------------------------------

# 18. Helper install is non-fatal: postinstall exits 0 even when install-helper.sh fails
@test "helper install: postinstall exits 0 even when install-helper.sh returns non-zero (R4)" {
  # Produce a test postinstall and add a SHARE_DIR with a failing install-helper.sh.
  local test_dir="${BATS_TEST_TMPDIR}/r4-nonfatal-$$"
  mkdir -p "${test_dir}"
  local tmp_script="${BATS_TEST_TMPDIR}/postinstall-r4-$$"
  _make_test_script "${tmp_script}" "${test_dir}"

  # Create a SHARE_DIR with an install-helper.sh that always fails.
  local fake_share="${BATS_TEST_TMPDIR}/share-r4-$$"
  mkdir -p "${fake_share}/install"
  cat > "${fake_share}/install/install-helper.sh" <<'EOF'
#!/usr/bin/env bash
echo "stub install-helper: simulating failure" >&2
exit 1
EOF
  chmod +x "${fake_share}/install/install-helper.sh"
  # Place a fake helper binary so the install attempt finds one to pass.
  echo "fake helper" > "${fake_share}/collection-helper"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    SHARE_DIR="${fake_share}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${tmp_script}"

  echo "status: ${status}"
  echo "output: ${output}"
  # postinstall must exit 0 — helper failure is non-fatal.
  [ "${status}" -eq 0 ]
  [[ "${output}" == *"WARNING"* ]] || [[ "${output}" == *"warning"* ]]
}

# 19. Helper install is attempted when SHARE_DIR/collection-helper exists
@test "helper install: install-helper.sh is called when helper binary is present in SHARE_DIR" {
  local test_dir="${BATS_TEST_TMPDIR}/r4-called-$$"
  mkdir -p "${test_dir}"
  local tmp_script="${BATS_TEST_TMPDIR}/postinstall-r4called-$$"
  _make_test_script "${tmp_script}" "${test_dir}"

  local fake_share="${BATS_TEST_TMPDIR}/share-r4called-$$"
  mkdir -p "${fake_share}/install"
  local install_called="${BATS_TEST_TMPDIR}/install_helper_called"
  cat > "${fake_share}/install/install-helper.sh" <<EOF
#!/usr/bin/env bash
echo "stub install-helper: called with args: \$*" >&2
touch "${install_called}"
exit 0
EOF
  chmod +x "${fake_share}/install/install-helper.sh"
  echo "fake helper" > "${fake_share}/collection-helper"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    SHARE_DIR="${fake_share}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${tmp_script}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [ -f "${install_called}" ]
}

# 20. Helper install is skipped (non-fatal) when SHARE_DIR/collection-helper is absent
@test "helper install: skipped gracefully when SHARE_DIR/collection-helper is absent" {
  local test_dir="${BATS_TEST_TMPDIR}/r4-absent-$$"
  mkdir -p "${test_dir}"
  local tmp_script="${BATS_TEST_TMPDIR}/postinstall-r4absent-$$"
  _make_test_script "${tmp_script}" "${test_dir}"

  # SHARE_DIR exists but has no collection-helper binary.
  local fake_share="${BATS_TEST_TMPDIR}/share-r4absent-$$"
  mkdir -p "${fake_share}"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    SHARE_DIR="${fake_share}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${tmp_script}"

  echo "status: ${status}"
  echo "output: ${output}"
  # Must not fail — helper absent is not an error.
  [ "${status}" -eq 0 ]
}

# ---------------------------------------------------------------------------
# #1330 — stale auth clear on every install/update (belt-and-suspenders for
# the 2026-06-12 incident where a stale Clerk-instance keychain key bypassed
# first-run auth).
#
# Design:
#   On EVERY install/update postinstall clears the auth state so the daemon's
#   startup ProbeTokenLiveness (#3238 Fix A) runs unconditionally and decides
#   whether PKCE re-auth is needed.  This is safe because:
#     - archives/ dir is never touched
#     - account_id and daemon_id are preserved in daemon.json
#     - only keychain, api_key, daemon_jwt are cleared
#
#   Two keychain service names are cleared for full field coverage:
#     com.hollowmark.daemon  — ServiceNameNew  (all current installs)
#     com.vaultmtg.daemon    — ServiceNameLegacy (pre-ADR-022-Phase3 installs)
#
# Tests:
#   21. Stale auth is cleared on install: security delete called for both
#       com.hollowmark.daemon and com.vaultmtg.daemon / api-key; daemon.json
#       keychain+api_key+daemon_jwt cleared; exits 0.
#   22. Archives directory is preserved (not deleted or modified) by the auth clear.
#   23. account_id and daemon_id fields are preserved after the auth clear.
# ---------------------------------------------------------------------------

# 21. stale-auth clear: security delete called for both keychain services; auth fields zeroed; exits 0
@test "stale-auth clear: security delete-generic-password called for hollowmark+vaultmtg services and auth fields zeroed in daemon.json" {
  local security_calls="${BATS_TEST_TMPDIR}/security_calls_21"

  cat > "${STUB_DIR}/security" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "${security_calls}"
exit 0
EOF
  chmod +x "${STUB_DIR}/security"

  # Pre-create config with keychain=true, api_key, daemon_jwt set — simulates
  # an install with stale Clerk-instance credentials from the incident.
  mkdir -p "${TEST_DIR}/.vaultmtg"
  python3 -c "
import json
data = {
    'cloud_api_url': 'https://staging-api.vaultmtg.app/api/v1',
    'keychain': True,
    'api_key': '',
    'daemon_jwt': 'stale.jwt.token',
    'daemon_id': 'daemon-uuid-123',
    'account_id': 'user_abc123',
    'sync_enabled': True
}
print(json.dumps(data, indent=2))
" > "${CONFIG_FILE}"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # security must have been called for both service names.
  [ -f "${security_calls}" ]
  grep -q "delete-generic-password" "${security_calls}"
  grep -q "com.hollowmark.daemon" "${security_calls}"
  grep -q "com.vaultmtg.daemon" "${security_calls}"
  grep -q "api-key" "${security_calls}"

  # daemon.json: auth fields must be cleared; account_id and daemon_id preserved.
  python3 -c "
import json
with open('${CONFIG_FILE}') as f:
    d = json.load(f)
assert not d.get('keychain'), \
    'FAIL: keychain flag not cleared: ' + repr(d.get('keychain'))
assert not d.get('api_key'), \
    'FAIL: api_key not cleared: ' + repr(d.get('api_key'))
assert not d.get('daemon_jwt'), \
    'FAIL: daemon_jwt not cleared: ' + repr(d.get('daemon_jwt'))
print('PASS: keychain, api_key, daemon_jwt all cleared')
"

  [[ "${output}" == *"stale auth cleared"* ]]
}

# 22. stale-auth clear: archives directory is preserved (not removed)
@test "stale-auth clear: archives directory is preserved on install/update" {
  # Pre-create config + archives dir with a fake snapshot.
  mkdir -p "${TEST_DIR}/.vaultmtg/archives"
  echo "fake log snapshot" > "${TEST_DIR}/.vaultmtg/archives/Player.log.2026-06-12.gz"
  python3 -c "
import json
data = {
    'cloud_api_url': 'https://staging-api.vaultmtg.app/api/v1',
    'keychain': True,
    'daemon_id': 'daemon-uuid-123',
    'account_id': 'user_abc123',
    'sync_enabled': True
}
print(json.dumps(data, indent=2))
" > "${CONFIG_FILE}"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # Archives dir and its contents must still exist.
  [ -d "${TEST_DIR}/.vaultmtg/archives" ]
  [ -f "${TEST_DIR}/.vaultmtg/archives/Player.log.2026-06-12.gz" ]
}

# 23. stale-auth clear: account_id and daemon_id are preserved after auth clear
@test "stale-auth clear: account_id and daemon_id are preserved in daemon.json after auth clear" {
  local security_calls="${BATS_TEST_TMPDIR}/security_calls_23"

  cat > "${STUB_DIR}/security" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "${security_calls}"
exit 0
EOF
  chmod +x "${STUB_DIR}/security"

  mkdir -p "${TEST_DIR}/.vaultmtg"
  python3 -c "
import json
data = {
    'cloud_api_url': 'https://staging-api.vaultmtg.app/api/v1',
    'keychain': True,
    'api_key': '',
    'daemon_jwt': 'stale.jwt.token',
    'daemon_id': 'daemon-uuid-preserve-me',
    'account_id': 'user_preserve_me',
    'sync_enabled': True
}
print(json.dumps(data, indent=2))
" > "${CONFIG_FILE}"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  python3 -c "
import json
with open('${CONFIG_FILE}') as f:
    d = json.load(f)
assert d.get('account_id') == 'user_preserve_me', \
    'FAIL: account_id changed: ' + repr(d.get('account_id'))
assert d.get('daemon_id') == 'daemon-uuid-preserve-me', \
    'FAIL: daemon_id changed: ' + repr(d.get('daemon_id'))
assert d.get('sync_enabled') == True, \
    'FAIL: sync_enabled changed: ' + repr(d.get('sync_enabled'))
print('PASS: account_id, daemon_id, sync_enabled all preserved after stale auth clear')
"
}

# ---------------------------------------------------------------------------
# §6b — launchd liveness check (ADR-083 SH-2 / tickets#1355)
#
# After launchctl bootstrap, postinstall verifies launchd has actually
# registered the service with a live PID using:
#   launchctl print gui/<uid>/<label>
# and checks for a "pid" field in the output.
#
# Behaviour under test:
#   24. First print shows PID — logged as "launchd service live" + exits 0.
#   25. First print has no PID; retry fires; second print shows PID —
#       logged as "launchd service live (retry)" + exits 0.
#   26. Both print calls return no PID — WARNING logged but exits 0 (installer
#       MUST NOT fail; Apple PKG rolls back on non-zero exit).
#
# Strategy: the launchctl stub in _make_stub_dir already records calls and
# always succeeds. These tests override it with a smarter stub that returns
# different output on the first vs subsequent "print" subcommand invocations,
# using a call-count file in BATS_TEST_TMPDIR. All other subcommands
# (bootstrap, bootout, enable, list, asuser) still exit 0 silently.
# ---------------------------------------------------------------------------

# 24. launchd liveness check: first print shows PID → logged as live, exits 0
@test "launchd liveness (§6b): exits 0 and logs live when first launchctl print shows pid" {
  local lc_calls="${BATS_TEST_TMPDIR}/launchctl_calls_24"

  # launchctl stub: "print" subcommand returns a pid line immediately.
  # All other subcommands succeed silently.
  cat > "${STUB_DIR}/launchctl" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "${lc_calls}"
case "\${*}" in
  *print*)
    printf '\tpid = 12345\n'
    printf '\tstate = running\n'
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
EOF
  chmod +x "${STUB_DIR}/launchctl"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [[ "${output}" == *"launchd service live"* ]]
}

# 25. launchd liveness: first print has no PID; retry fires; second print shows PID → exits 0
@test "launchd liveness (§6b): retries once and exits 0 when second launchctl print shows pid" {
  local lc_calls="${BATS_TEST_TMPDIR}/launchctl_calls_25"
  local print_count="${BATS_TEST_TMPDIR}/launchctl_print_count_25"
  echo "0" > "${print_count}"

  # First "print" invocation returns no PID; second returns a PID.
  cat > "${STUB_DIR}/launchctl" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "${lc_calls}"
case "\${*}" in
  *print*)
    count=\$(cat "${print_count}" 2>/dev/null || echo 0)
    count=\$(( count + 1 ))
    echo "\${count}" > "${print_count}"
    if [ "\${count}" -eq 1 ]; then
      printf '\tstate = spawning\n'
      exit 0
    else
      printf '\tpid = 12345\n'
      printf '\tstate = running\n'
      exit 0
    fi
    ;;
  *)
    exit 0
    ;;
esac
EOF
  chmod +x "${STUB_DIR}/launchctl"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  [[ "${output}" == *"launchd service live"* ]]
  # A retry message must be present.
  [[ "${output}" == *"retry"* ]]
  # Exactly 2 print calls must have been made.
  local count
  count=$(cat "${print_count}")
  echo "launchctl print call count: ${count}"
  [ "${count}" -eq 2 ]
}

# 26. launchd liveness: both print calls return no PID → WARNING logged, exits 0 (non-fatal)
@test "launchd liveness (§6b): exits 0 with warning when both launchctl print calls show no pid" {
  local lc_calls="${BATS_TEST_TMPDIR}/launchctl_calls_26"
  local print_count="${BATS_TEST_TMPDIR}/launchctl_print_count_26"
  echo "0" > "${print_count}"

  # Both "print" invocations return output with no PID.
  cat > "${STUB_DIR}/launchctl" <<EOF
#!/usr/bin/env bash
echo "\$*" >> "${lc_calls}"
case "\${*}" in
  *print*)
    count=\$(cat "${print_count}" 2>/dev/null || echo 0)
    count=\$(( count + 1 ))
    echo "\${count}" > "${print_count}"
    printf '\tstate = spawning\n'
    exit 0
    ;;
  *)
    exit 0
    ;;
esac
EOF
  chmod +x "${STUB_DIR}/launchctl"

  run env \
    PATH="${STUB_DIR}:${PATH}" \
    SUDO_USER="${REAL_USER}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${TMP_SCRIPT}"

  echo "status: ${status}"
  echo "output: ${output}"
  # Non-fatal: installer must exit 0 even when launchd check fails.
  [ "${status}" -eq 0 ]
  [[ "${output}" == *"WARNING"* ]]
  # Both attempts must have been made before giving up.
  local count
  count=$(cat "${print_count}")
  echo "launchctl print call count: ${count}"
  [ "${count}" -eq 2 ]
}
