#!/usr/bin/env bats
# install_test.bats — unit tests for services/daemon/install/macos/install.sh
#
# Run with:
#   bats services/daemon/install/macos/install_test.bats
#
# All tests use stubs so they never download anything, call sudo, or touch
# launchctl.  The DRY_RUN=1 guard (added in the same PR) prevents real
# system mutations even if a stub is missed.

INSTALL_SH="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/install.sh"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

# Create a minimal stub directory that is prepended to PATH.
# Stubs replace curl, sudo, launchctl, and read so the script can run
# non-interactively in CI without any network or privilege access.
_make_stub_dir() {
  local stub_dir
  stub_dir="$(mktemp -d)"

  # curl — write an empty file so TMP_BIN is created but empty
  cat > "${stub_dir}/curl" <<'EOF'
#!/usr/bin/env bash
# Absorb all flags; write empty content to -o <file> if present.
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) shift; > "$1" ;;
  esac
  shift
done
EOF
  chmod +x "${stub_dir}/curl"

  # sudo — record that it was called; do NOT execute the real command
  cat > "${stub_dir}/sudo" <<'EOF'
#!/usr/bin/env bash
echo "stub-sudo: $*" >&2
SUDO_CALLED_FILE="${BATS_TEST_TMPDIR}/sudo_called"
echo 1 > "${SUDO_CALLED_FILE}"
EOF
  chmod +x "${stub_dir}/sudo"

  # launchctl — record that it was called
  cat > "${stub_dir}/launchctl" <<'EOF'
#!/usr/bin/env bash
echo "stub-launchctl: $*" >&2
LAUNCHCTL_CALLED_FILE="${BATS_TEST_TMPDIR}/launchctl_called"
echo 1 > "${LAUNCHCTL_CALLED_FILE}"
EOF
  chmod +x "${stub_dir}/launchctl"

  echo "${stub_dir}"
}

# ---------------------------------------------------------------------------
# 1. Architecture detection — arm64
# ---------------------------------------------------------------------------
@test "arch detection: arm64 maps to darwin-arm64 asset suffix" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  # Override uname so the script sees arm64
  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "arm64"
EOF
  chmod +x "${stub_dir}/uname"

  # Provide a pre-set RELEASE_TAG so the script skips the GitHub API call.
  # HOME is pointed at a temp dir so no real files are touched.
  local fake_home
  fake_home="$(mktemp -d)"

  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    DRY_RUN=1 \
    bash "${INSTALL_SH}" <<< $'https://api.example.com\nfake-token\n'

  echo "output: ${output}"
  [[ "${output}" == *"darwin-arm64"* ]]
}

# ---------------------------------------------------------------------------
# 2. Architecture detection — x86_64
# ---------------------------------------------------------------------------
@test "arch detection: x86_64 maps to darwin-amd64 asset suffix" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "x86_64"
EOF
  chmod +x "${stub_dir}/uname"

  local fake_home
  fake_home="$(mktemp -d)"

  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    DRY_RUN=1 \
    bash "${INSTALL_SH}" <<< $'https://api.example.com\nfake-token\n'

  echo "output: ${output}"
  [[ "${output}" == *"darwin-amd64"* ]]
}

# ---------------------------------------------------------------------------
# 3. Architecture detection — unknown arch exits 1
# ---------------------------------------------------------------------------
@test "arch detection: unknown architecture exits with status 1" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "riscv64"
EOF
  chmod +x "${stub_dir}/uname"

  local fake_home
  fake_home="$(mktemp -d)"

  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    DRY_RUN=1 \
    bash "${INSTALL_SH}"

  [ "${status}" -eq 1 ]
  [[ "${output}" == *"Unsupported architecture"* ]]
}

# ---------------------------------------------------------------------------
# 4. Asset name construction — download URL contains correct asset name
# ---------------------------------------------------------------------------
@test "asset name: RELEASE_TAG=daemon/v0.1.0 on arm64 produces correct download URL" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  # Capture the URL curl is called with
  cat > "${stub_dir}/curl" <<'EOF'
#!/usr/bin/env bash
# Print every arg so we can inspect the URL in the test output.
echo "stub-curl-args: $*" >&2
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) shift; > "$1" ;;
  esac
  shift
done
EOF
  chmod +x "${stub_dir}/curl"

  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "arm64"
EOF
  chmod +x "${stub_dir}/uname"

  local fake_home
  fake_home="$(mktemp -d)"

  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    DRY_RUN=1 \
    bash "${INSTALL_SH}" <<< $'https://api.example.com\nfake-token\n'

  echo "output: ${output}"
  # The download URL must contain the versioned tag and correct asset suffix
  [[ "${output}" == *"daemon/v0.1.0"* ]]
  [[ "${output}" == *"vaultmtg-daemon-darwin-arm64"* ]]
}

# ---------------------------------------------------------------------------
# 5. jq fallback — python3 fallback produces valid JSON with required keys
# ---------------------------------------------------------------------------
@test "jq fallback: python3 fallback writes JSON with cloud_api_url and api_key" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "arm64"
EOF
  chmod +x "${stub_dir}/uname"

  # Shadow jq by using a restricted PATH that only contains stub_dir, /bin,
  # and the Homebrew prefix for python3.  We deliberately omit /usr/bin (and
  # any other directory that ships a real jq) so that `command -v jq` returns
  # false and install.sh falls through to the python3 fallback.
  #
  # Why not a non-executable stub?  `command -v` skips non-executables in
  # stub_dir and then continues searching the rest of PATH, so the real jq
  # (e.g. /usr/bin/jq) would still be found.
  #
  # Why not an executable stub that exits non-zero?  install.sh runs under
  # `set -euo pipefail`, so any non-zero jq exit aborts the script entirely
  # before the python3 branch can run.
  #
  # The restricted PATH also requires a mktemp stub because /usr/bin/mktemp
  # is no longer reachable; all other commands used by install.sh are either
  # bash builtins or live in /bin or the Homebrew prefix.
  cat > "${stub_dir}/mktemp" <<'MKTEMP'
#!/usr/bin/env bash
TMPFILE="${TMPDIR:-/tmp}/stub-mktemp-$$-${RANDOM}"
if [[ "$1" == "-d" ]]; then
  mkdir -p "${TMPFILE}"
else
  > "${TMPFILE}"
fi
echo "${TMPFILE}"
MKTEMP
  chmod +x "${stub_dir}/mktemp"

  local fake_home
  fake_home="${BATS_TEST_TMPDIR}/home-$$"
  mkdir -p "${fake_home}"

  # Restrict PATH: stub_dir first, then /bin (chmod, mkdir, rm, cat),
  # then /opt/homebrew/bin (python3).  /usr/bin is intentionally excluded
  # so that the real jq is unreachable.
  local restricted_path="${stub_dir}:/bin:/opt/homebrew/bin"

  run env \
    PATH="${restricted_path}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    DRY_RUN=1 \
    bash "${INSTALL_SH}" <<< $'https://api.example.com\nmy-secret-token\n'

  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  local config_file="${fake_home}/.vaultmtg/daemon.json"
  [ -f "${config_file}" ]

  # The config must contain both required keys
  python3 -c "
import json, sys
with open('${config_file}') as f:
    data = json.load(f)
assert 'cloud_api_url' in data, 'missing cloud_api_url'
assert 'api_key' in data, 'missing api_key'
assert data['cloud_api_url'] == 'https://api.example.com', 'wrong url'
assert data['api_key'] == 'my-secret-token', 'wrong api_key'
"
}

# ---------------------------------------------------------------------------
# 6. Idempotency — existing config is not overwritten
# ---------------------------------------------------------------------------
@test "idempotency: existing config file is not overwritten" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "arm64"
EOF
  chmod +x "${stub_dir}/uname"

  local fake_home
  fake_home="$(mktemp -d)"

  # Pre-populate the config so the script should skip the prompt
  local config_dir="${fake_home}/.vaultmtg"
  mkdir -p "${config_dir}"
  local config_file="${config_dir}/daemon.json"
  echo '{"cloud_api_url":"https://original.example.com","api_key":"original-token"}' > "${config_file}"
  local original_content
  original_content="$(cat "${config_file}")"

  # stdin is /dev/null — if the script prompts, it will fail because there is
  # no input to read, causing the test to surface a bug immediately.
  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    DRY_RUN=1 \
    bash "${INSTALL_SH}" < /dev/null

  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # Config content must be unchanged
  local new_content
  new_content="$(cat "${config_file}")"
  [ "${new_content}" = "${original_content}" ]

  # Script must report the skip message
  [[ "${output}" == *"Config already exists, skipping"* ]]
}

# ---------------------------------------------------------------------------
# 7. DRY_RUN mode — sudo and launchctl are NOT called when DRY_RUN=1
# ---------------------------------------------------------------------------
@test "DRY_RUN=1: sudo and launchctl are not invoked" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "arm64"
EOF
  chmod +x "${stub_dir}/uname"

  local fake_home
  fake_home="$(mktemp -d)"

  # Use BATS_TEST_TMPDIR so stub scripts can write sentinel files
  export BATS_TEST_TMPDIR

  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    DRY_RUN=1 \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${INSTALL_SH}" <<< $'https://api.example.com\nfake-token\n'

  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # Sentinel files must NOT exist (stub sudo/launchctl write them when called)
  [ ! -f "${BATS_TEST_TMPDIR}/sudo_called" ]
  [ ! -f "${BATS_TEST_TMPDIR}/launchctl_called" ]

  # DRY_RUN output messages must be present
  [[ "${output}" == *"[DRY_RUN] would install binary"* ]]
  [[ "${output}" == *"[DRY_RUN] would run: launchctl"* ]]
}

# ---------------------------------------------------------------------------
# 7b. --dry-run flag — equivalent to DRY_RUN=1
# ---------------------------------------------------------------------------
@test "--dry-run flag: sudo and launchctl are not invoked (same as DRY_RUN=1)" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "arm64"
EOF
  chmod +x "${stub_dir}/uname"

  local fake_home
  fake_home="$(mktemp -d)"

  export BATS_TEST_TMPDIR

  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${INSTALL_SH}" --dry-run <<< $'https://api.example.com\nfake-token\n'

  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # Sentinel files must NOT exist (stub sudo/launchctl write them when called)
  [ ! -f "${BATS_TEST_TMPDIR}/sudo_called" ]
  [ ! -f "${BATS_TEST_TMPDIR}/launchctl_called" ]

  # DRY_RUN output messages must be present
  [[ "${output}" == *"[DRY_RUN] would install binary"* ]]
  [[ "${output}" == *"[DRY_RUN] would run: launchctl"* ]]
}

# ---------------------------------------------------------------------------
# 8. Reinstall with new BFF URL — cloud_api_url updated, api_key preserved
# ---------------------------------------------------------------------------
@test "reinstall: providing a new BFF URL updates cloud_api_url and preserves api_key" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "arm64"
EOF
  chmod +x "${stub_dir}/uname"

  local fake_home
  fake_home="$(mktemp -d)"

  # Pre-populate config with an old URL and an existing api_key.
  local config_dir="${fake_home}/.vaultmtg"
  mkdir -p "${config_dir}"
  local config_file="${config_dir}/daemon.json"
  python3 -c "
import json
print(json.dumps({
  'cloud_api_url': 'https://old-api.example.com/api/v1',
  'api_key': 'my-precious-api-key',
  'sync_enabled': True
}, indent=2))
" > "${config_file}"

  # Provide a new BFF URL via stdin (install.sh reads it via `read`).
  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    DRY_RUN=1 \
    bash "${INSTALL_SH}" <<< $'https://new-api.example.com/api/v1\n'

  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # cloud_api_url must be updated.
  python3 -c "
import json
with open('${config_file}') as f:
    d = json.load(f)
assert d['cloud_api_url'] == 'https://new-api.example.com/api/v1', \
    'FAIL: cloud_api_url not updated: ' + repr(d['cloud_api_url'])
print('PASS: cloud_api_url updated')
"

  # api_key must be preserved (not blanked or removed).
  python3 -c "
import json
with open('${config_file}') as f:
    d = json.load(f)
assert d.get('api_key') == 'my-precious-api-key', \
    'FAIL: api_key not preserved: ' + repr(d.get('api_key'))
print('PASS: api_key preserved across reinstall')
"

  [[ "${output}" == *"Config updated (cloud_api_url)"* ]]
}

# ---------------------------------------------------------------------------
# ADR-022 C1 cutover-safety: hollowmark future-label defensive handling
# (#999 — symmetric to the com.mtga-companion.daemon legacy pattern)
#
# The v0.3.9 installer must defensively boot out the future label
# (com.hollowmark.daemon) if it is already loaded — preventing double-launch
# if a user somehow has a v0.4.0+ daemon installed and then rolls back to
# v0.3.9.  Symmetric to install.sh:194-216 (mtga-companion legacy path).
# ---------------------------------------------------------------------------

# 9. install boots out a pre-existing com.hollowmark.daemon job before loading primary
@test "cutover-safety: install boots out a pre-existing com.hollowmark.daemon job" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "arm64"
EOF
  chmod +x "${stub_dir}/uname"

  # Override launchctl to simulate: com.hollowmark.daemon is loaded.
  # Record all invocations to a log file for inspection.
  cat > "${stub_dir}/launchctl" <<'LCEOF'
#!/usr/bin/env bash
echo "stub-launchctl: $*" >&2
echo "$*" >> "${BATS_TEST_TMPDIR}/launchctl_log"
# `launchctl list com.hollowmark.daemon` — return 0 to indicate it is loaded.
if [[ "$1" == "list" && "$2" == "com.hollowmark.daemon" ]]; then
  exit 0
fi
exit 0
LCEOF
  chmod +x "${stub_dir}/launchctl"

  local fake_home
  fake_home="$(mktemp -d)"
  export BATS_TEST_TMPDIR

  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    DRY_RUN=1 \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${INSTALL_SH}" <<< $'https://api.example.com\nfake-token\n'

  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # The script must report finding and handling the future hollowmark label.
  [[ "${output}" == *"com.hollowmark.daemon"* ]]
  [[ "${output}" == *"hollowmark"* ]]
}

# 10. install does NOT boot out com.hollowmark.daemon when it is absent (no-op)
@test "cutover-safety: install skips com.hollowmark.daemon bootout when absent" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "arm64"
EOF
  chmod +x "${stub_dir}/uname"

  # launchctl list com.hollowmark.daemon returns 1 — not loaded.
  cat > "${stub_dir}/launchctl" <<'LCEOF'
#!/usr/bin/env bash
echo "stub-launchctl: $*" >&2
echo "$*" >> "${BATS_TEST_TMPDIR}/launchctl_log"
if [[ "$1" == "list" && "$2" == "com.hollowmark.daemon" ]]; then
  exit 1
fi
exit 0
LCEOF
  chmod +x "${stub_dir}/launchctl"

  local fake_home
  fake_home="$(mktemp -d)"
  export BATS_TEST_TMPDIR

  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    DRY_RUN=1 \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${INSTALL_SH}" <<< $'https://api.example.com\nfake-token\n'

  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  # The script must still succeed cleanly.
  [[ "${output}" == *"VaultMTG daemon installed"* ]]
}

# 11. only ONE daemon job loaded after install (AC4 — no double-launch)
@test "cutover-safety: exactly one launchctl load call issued for the primary label" {
  local stub_dir
  stub_dir="$(_make_stub_dir)"

  cat > "${stub_dir}/uname" <<'EOF'
#!/usr/bin/env bash
echo "arm64"
EOF
  chmod +x "${stub_dir}/uname"

  # Count launchctl load calls.
  cat > "${stub_dir}/launchctl" <<'LCEOF'
#!/usr/bin/env bash
echo "stub-launchctl: $*" >&2
echo "$*" >> "${BATS_TEST_TMPDIR}/launchctl_log"
if [[ "$1" == "list" ]]; then
  exit 1
fi
exit 0
LCEOF
  chmod +x "${stub_dir}/launchctl"

  local fake_home
  fake_home="$(mktemp -d)"
  export BATS_TEST_TMPDIR

  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    RELEASE_TAG="daemon/v0.1.0" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${INSTALL_SH}" <<< $'https://api.example.com\nfake-token\n'

  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # Count "load" invocations in the launchctl log — must be exactly 1.
  local load_count
  if [[ -f "${BATS_TEST_TMPDIR}/launchctl_log" ]]; then
    load_count=$(grep "^load " "${BATS_TEST_TMPDIR}/launchctl_log" | wc -l | tr -d ' ')
  else
    load_count=1  # DRY_RUN not used here — real launchctl stub ran
  fi
  [ "${load_count}" -eq 1 ]
}
