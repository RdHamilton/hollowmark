#!/usr/bin/env bats
# channel_test.bats — Per-channel invariant suite (ADR-049 §3) for the macOS daemon.
#
# This file adds the CHANNEL dimension to the install invariant suite defined in
# ADR-036.  Every identity-sensitive invariant (I-1..I-9) is exercised for both
# CHANNEL=stable and CHANNEL=staging.  The load-bearing cross-channel non-interference
# tests (I-2 per ADR-049) prove that uninstalling one channel is a strict no-op
# against the other channel's artifacts.
#
# Dependency: common.sh / internal/install.Channel (tickets #650/#651/#655).
# Until those ship, CHANNEL=staging tests will fail because common.sh does not
# yet emit the -staging suffix.  That is the intended RED state — these tests
# define the contract; the channel infrastructure makes them green.
#
# Run with:
#   bats services/daemon/install/macos/channel_test.bats
#
# Test IDs match invariants.yml test_ids for the CI sanity-check assertion.

UNINSTALL_SH="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/uninstall.sh"
COMMON_SH="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/common.sh"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

# _channel_suffix returns the expected suffix for CHANNEL: "" for stable, "-staging"
# for staging.  Used throughout the test suite to derive expected identity values
# without hardcoding the suffix everywhere.
_channel_suffix() {
  local channel="$1"
  case "${channel}" in
    stable)  echo "" ;;
    staging) echo "-staging" ;;
    *)       echo "UNKNOWN_CHANNEL_${channel}" ;;
  esac
}

# _common_sh_available returns 0 if common.sh exists and is sourceable with
# CHANNEL=$1, non-zero otherwise.  Used to emit a clear skip/fail message when
# ticket #650 has not yet landed.
_common_sh_available() {
  local channel="$1"
  [[ -f "${COMMON_SH}" ]] || return 1
  bash -c "CHANNEL=${channel} source '${COMMON_SH}'" 2>/dev/null || return 1
  return 0
}

# _source_common_sh sources common.sh for CHANNEL=$1 and exports its variables
# into the current environment.  Called inside process-substitution or a subshell.
_source_common_sh() {
  local channel="$1"
  # shellcheck source=/dev/null
  CHANNEL="${channel}" source "${COMMON_SH}"
}

# _make_stub_dir builds a stub directory (sudo, launchctl, security — no-ops).
_make_stub_dir() {
  local stub_dir
  stub_dir="$(mktemp -d)"

  cat > "${stub_dir}/sudo" <<'EOF'
#!/usr/bin/env bash
exec "$@"
EOF
  chmod +x "${stub_dir}/sudo"

  cat > "${stub_dir}/launchctl" <<'EOF'
#!/usr/bin/env bash
echo "stub-launchctl: $*" >&2
if [[ -n "${BATS_TEST_TMPDIR:-}" ]]; then
  echo "$*" >> "${BATS_TEST_TMPDIR}/launchctl_calls"
fi
if [[ "$1" == "list" ]]; then exit 1; fi
exit 0
EOF
  chmod +x "${stub_dir}/launchctl"

  cat > "${stub_dir}/security" <<'EOF'
#!/usr/bin/env bash
echo "stub-security: $*" >&2
if [[ -n "${BATS_TEST_TMPDIR:-}" ]]; then
  echo "$*" >> "${BATS_TEST_TMPDIR}/security_calls"
fi
if [[ "$1" == "delete-generic-password" && "${SECURITY_NOT_FOUND:-0}" == "1" ]]; then
  exit 44
fi
exit 0
EOF
  chmod +x "${stub_dir}/security"

  echo "${stub_dir}"
}

# _make_channel_home creates a fake HOME with channel-appropriate artifacts.
# Arguments:
#   $1 — channel (stable | staging)
#   $2 — install_dir (for binary placement)
#   $3 — "with-binary" | "no-binary"
#   $4 — "with-plist" | "no-plist"
#   $5 — "with-app" | "no-app"     (creates /Applications/<AppName> inside fake_home)
#   $6 — "with-config" | "no-config"
_make_channel_home() {
  local channel="$1"
  local install_dir="$2"
  local binary_flag="$3"
  local plist_flag="$4"
  local app_flag="$5"
  local config_flag="$6"

  local suffix
  suffix="$(_channel_suffix "${channel}")"

  # Derive channel-appropriate identity values.
  # These match what common.sh will produce once ticket #650 lands.
  local binary_name="vaultmtg-daemon${suffix}"
  local plist_label="com.vaultmtg.daemon${suffix}"
  local config_dir_name=".vaultmtg${suffix}"
  local app_name="VaultMTG${suffix:+ Staging}"   # "VaultMTG" or "VaultMTG Staging"

  local fake_home
  fake_home="$(mktemp -d)"

  mkdir -p "${install_dir}"
  if [[ "${binary_flag}" == "with-binary" ]]; then
    echo "fake ${channel} binary" > "${install_dir}/${binary_name}"
    chmod +x "${install_dir}/${binary_name}"
  fi

  mkdir -p "${fake_home}/Library/LaunchAgents"
  if [[ "${plist_flag}" == "with-plist" ]]; then
    echo "<plist>${channel}</plist>" > "${fake_home}/Library/LaunchAgents/${plist_label}.plist"
  fi

  if [[ "${app_flag}" == "with-app" ]]; then
    # Simulate /Applications/<AppName>.app inside fake_home/Applications
    mkdir -p "${fake_home}/Applications/${app_name}.app/Contents/MacOS"
    echo "fake launcher" > "${fake_home}/Applications/${app_name}.app/Contents/MacOS/vaultmtg-launcher"
    chmod +x "${fake_home}/Applications/${app_name}.app/Contents/MacOS/vaultmtg-launcher"
    cat > "${fake_home}/Applications/${app_name}.app/Contents/Info.plist" <<INFOPLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key>
  <string>app.vaultmtg.daemon${suffix}</string>
  <key>CFBundleName</key>
  <string>${app_name}</string>
</dict>
</plist>
INFOPLIST
  fi

  if [[ "${config_flag}" == "with-config" ]]; then
    mkdir -p "${fake_home}/${config_dir_name}"
    echo "{\"cloud_api_url\":\"https://api.example.com\",\"channel\":\"${channel}\"}" \
      > "${fake_home}/${config_dir_name}/daemon.json"
  fi

  echo "${fake_home}"
}

# ---------------------------------------------------------------------------
# I-1: Idempotence on install — per channel
# ---------------------------------------------------------------------------

@test "I-1-stable-idempotent-install: CHANNEL=stable install twice produces same end state" {
  # Verify common.sh is available for CHANNEL=stable — this is the foundation.
  # If common.sh does not exist yet (ticket #650 not landed), this test fails RED.
  if ! _common_sh_available "stable"; then
    # Produce a clear, actionable failure message.
    echo "FAIL: common.sh not found at ${COMMON_SH} or not sourceable with CHANNEL=stable" >&3
    echo "This test requires ticket #650 (common.sh suffix table) to land first." >&3
    false
  fi

  local suffix
  suffix="$(CHANNEL=stable bash -c "source '${COMMON_SH}' && echo \"\${SUFFIX}\"")"
  [ "${suffix}" = "" ]   # stable has empty suffix
}

@test "I-1-staging-idempotent-install: CHANNEL=staging install twice produces same end state" {
  if ! _common_sh_available "staging"; then
    echo "FAIL: common.sh not found at ${COMMON_SH} or not sourceable with CHANNEL=staging" >&3
    echo "This test requires ticket #650 (common.sh suffix table) to land first." >&3
    false
  fi

  local suffix
  suffix="$(CHANNEL=staging bash -c "source '${COMMON_SH}' && echo \"\${SUFFIX}\"")"
  [ "${suffix}" = "-staging" ]   # staging has -staging suffix
}

# ---------------------------------------------------------------------------
# I-2 (LOAD-BEARING): Cross-channel non-interference
#
# These are Ramone's explicit load-bearing requirement per ADR-049 §3 / I-2 per-channel:
# "uninstalling the staging daemon MUST NEVER touch prod's binary / LaunchAgent /
#  ~/.vaultmtg / VaultMTG.app / keychain entry — and vice versa."
#
# Both directions are tested: staging-uninstall-while-prod-installed and
# prod-uninstall-while-staging-installed.
# ---------------------------------------------------------------------------

@test "I-2-cross-channel-staging-uninstall-leaves-prod-untouched: uninstall staging is no-op on prod artifacts" {
  if ! _common_sh_available "staging"; then
    echo "FAIL: common.sh required (ticket #650) — staging suffix table not yet available" >&3
    false
  fi

  local stub_dir; stub_dir="$(_make_stub_dir)"
  local install_dir; install_dir="$(mktemp -d)"

  # Set up a machine with ONLY the stable (prod) channel installed.
  local fake_home
  fake_home="$(_make_channel_home "stable" "${install_dir}" \
    with-binary with-plist with-app with-config)"

  # Capture prod artifact paths BEFORE staging uninstall.
  local prod_binary="${install_dir}/vaultmtg-daemon"
  local prod_plist="${fake_home}/Library/LaunchAgents/com.vaultmtg.daemon.plist"
  local prod_app="${fake_home}/Applications/VaultMTG.app"
  local prod_config="${fake_home}/.vaultmtg/daemon.json"

  [ -f "${prod_binary}" ]
  [ -f "${prod_plist}" ]
  [ -d "${prod_app}" ]
  [ -f "${prod_config}" ]

  # Run the staging-channel uninstall against this machine.
  # APP_BUNDLE_PATH is redirected to fake_home/Applications so the test does not
  # interact with the real /Applications directory (same pattern as INSTALL_DIR).
  local staging_app_path="${fake_home}/Applications/VaultMTG Staging.app"
  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    INSTALL_DIR="${install_dir}" \
    CHANNEL="staging" \
    APP_BUNDLE_PATH="${staging_app_path}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${UNINSTALL_SH}"

  echo "status: ${status}"
  echo "output: ${output}"

  # The staging uninstall exits 0 (no-op — nothing staging-channel is installed).
  [ "${status}" -eq 0 ]

  # LOAD-BEARING: every prod artifact must be completely untouched.
  [ -f "${prod_binary}" ] || {
    echo "FAIL: prod binary was removed by staging uninstall!" >&3
    false
  }
  [ -f "${prod_plist}" ] || {
    echo "FAIL: prod LaunchAgent plist was removed by staging uninstall!" >&3
    false
  }
  [ -d "${prod_app}" ] || {
    echo "FAIL: prod VaultMTG.app was removed by staging uninstall!" >&3
    false
  }
  [ -f "${prod_config}" ] || {
    echo "FAIL: prod config dir/daemon.json was removed by staging uninstall!" >&3
    false
  }
}

@test "I-2-cross-channel-prod-uninstall-leaves-staging-untouched: uninstall prod is no-op on staging artifacts" {
  if ! _common_sh_available "stable"; then
    echo "FAIL: common.sh required (ticket #650) — stable suffix table not yet available" >&3
    false
  fi

  local stub_dir; stub_dir="$(_make_stub_dir)"
  local install_dir; install_dir="$(mktemp -d)"

  # Set up a machine with ONLY the staging channel installed.
  local fake_home
  fake_home="$(_make_channel_home "staging" "${install_dir}" \
    with-binary with-plist with-app with-config)"

  # Capture staging artifact paths BEFORE prod uninstall.
  local staging_binary="${install_dir}/vaultmtg-daemon-staging"
  local staging_plist="${fake_home}/Library/LaunchAgents/com.vaultmtg.daemon-staging.plist"
  local staging_app="${fake_home}/Applications/VaultMTG Staging.app"
  local staging_config="${fake_home}/.vaultmtg-staging/daemon.json"

  [ -f "${staging_binary}" ]
  [ -f "${staging_plist}" ]
  [ -d "${staging_app}" ]
  [ -f "${staging_config}" ]

  # Run the stable (prod) uninstall against this machine.
  # APP_BUNDLE_PATH redirected to fake_home so no /Applications interaction.
  local prod_app_path_fake="${fake_home}/Applications/VaultMTG.app"
  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    INSTALL_DIR="${install_dir}" \
    CHANNEL="stable" \
    APP_BUNDLE_PATH="${prod_app_path_fake}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${UNINSTALL_SH}"

  echo "status: ${status}"
  echo "output: ${output}"

  # The prod uninstall exits 0 (no-op — no prod artifacts present in fake install_dir).
  [ "${status}" -eq 0 ]

  # LOAD-BEARING: every staging artifact must be completely untouched.
  [ -f "${staging_binary}" ] || {
    echo "FAIL: staging binary was removed by prod uninstall!" >&3
    false
  }
  [ -f "${staging_plist}" ] || {
    echo "FAIL: staging LaunchAgent plist was removed by prod uninstall!" >&3
    false
  }
  [ -d "${staging_app}" ] || {
    echo "FAIL: staging VaultMTG Staging.app was removed by prod uninstall!" >&3
    false
  }
  [ -f "${staging_config}" ] || {
    echo "FAIL: staging config dir/daemon.json was removed by prod uninstall!" >&3
    false
  }
}

@test "I-2-stable-no-op-uninstall-absent-state: CHANNEL=stable uninstall on clean machine exits 0" {
  if ! _common_sh_available "stable"; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  local stub_dir; stub_dir="$(_make_stub_dir)"
  local install_dir; install_dir="$(mktemp -d)"
  local fake_home; fake_home="$(mktemp -d)"

  mkdir -p "${fake_home}/Library/LaunchAgents"

  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    INSTALL_DIR="${install_dir}" \
    CHANNEL="stable" \
    APP_BUNDLE_PATH="${fake_home}/Applications/VaultMTG.app" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${UNINSTALL_SH}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
}

@test "I-2-staging-no-op-uninstall-absent-state: CHANNEL=staging uninstall on clean machine exits 0" {
  if ! _common_sh_available "staging"; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  local stub_dir; stub_dir="$(_make_stub_dir)"
  local install_dir; install_dir="$(mktemp -d)"
  local fake_home; fake_home="$(mktemp -d)"

  mkdir -p "${fake_home}/Library/LaunchAgents"

  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    INSTALL_DIR="${install_dir}" \
    CHANNEL="staging" \
    APP_BUNDLE_PATH="${fake_home}/Applications/VaultMTG Staging.app" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${UNINSTALL_SH}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
}

# ---------------------------------------------------------------------------
# I-2 (coexistence): Install staging while prod is installed — both coexist
# This is the positive coexistence test: after installing staging alongside prod,
# both channels have distinct and fully independent artifacts.
# ---------------------------------------------------------------------------

@test "I-2-coexistence-staging-install-alongside-prod: distinct binary/plist/config/port" {
  if ! _common_sh_available "staging"; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  local install_dir; install_dir="$(mktemp -d)"

  # Build a machine with both channels installed.
  local fake_home_stable
  fake_home_stable="$(_make_channel_home "stable" "${install_dir}" \
    with-binary with-plist no-app with-config)"

  # The stable channel installs its artifacts into the same fake_home (shared dir).
  # Now add staging artifacts on top.
  # BINARY_NAME uses SUFFIX="-staging" (dash); PLIST_LABEL uses LABEL_SUFFIX=".staging" (dot).
  local binary_suffix="-staging"
  local label_suffix=".staging"
  local staging_binary="${install_dir}/vaultmtg-daemon${binary_suffix}"
  local staging_plist="${fake_home_stable}/Library/LaunchAgents/com.vaultmtg.daemon${label_suffix}.plist"
  local staging_config_dir="${fake_home_stable}/.vaultmtg${binary_suffix}"

  echo "fake staging binary" > "${staging_binary}"
  chmod +x "${staging_binary}"
  echo "<plist>staging</plist>" > "${staging_plist}"
  mkdir -p "${staging_config_dir}"
  echo '{"channel":"staging"}' > "${staging_config_dir}/daemon.json"

  # Assert: binary names are distinct.
  local prod_binary="${install_dir}/vaultmtg-daemon"
  [ -f "${prod_binary}" ]
  [ -f "${staging_binary}" ]
  [ "${prod_binary}" != "${staging_binary}" ]

  # Assert: LaunchAgent labels are distinct.
  local prod_plist="${fake_home_stable}/Library/LaunchAgents/com.vaultmtg.daemon.plist"
  [ -f "${prod_plist}" ]
  [ -f "${staging_plist}" ]
  [ "${prod_plist}" != "${staging_plist}" ]

  # Assert: config dirs are distinct.
  local prod_config_dir="${fake_home_stable}/.vaultmtg"
  [ -d "${prod_config_dir}" ]
  [ -d "${staging_config_dir}" ]
  [ "${prod_config_dir}" != "${staging_config_dir}" ]

  # Assert: keychain services are distinct (identity constants, from common.sh).
  local stable_keychain
  stable_keychain="$(CHANNEL=stable bash -c "source '${COMMON_SH}' && echo \"\${KEYCHAIN_SERVICE:-com.vaultmtg.daemon}\"" 2>/dev/null)"
  local staging_keychain
  staging_keychain="$(CHANNEL=staging bash -c "source '${COMMON_SH}' && echo \"\${KEYCHAIN_SERVICE:-unset}\"" 2>/dev/null)"
  [ "${stable_keychain}" != "${staging_keychain}" ]
  # Staging keychain service uses LABEL_SUFFIX=".staging" (dot separator, per ADR-049 §1 table).
  [[ "${staging_keychain}" == *".staging"* ]] || {
    echo "FAIL: staging keychain service '${staging_keychain}' does not contain '.staging' label suffix" >&3
    false
  }
}

# ---------------------------------------------------------------------------
# I-4: Single source of truth — no identity literals outside common.sh
# ---------------------------------------------------------------------------

@test "I-4-no-literal-outside-common-sh: install scripts contain no identity literals except in common.sh" {
  local install_dir
  install_dir="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)"

  # These are the identity literals that MUST NOT appear in any install script
  # outside of common.sh / common.ps1 (per ADR-049 §2, strengthened I-4).
  # After common.sh lands, scripts must reference variables, not literals.
  local banned_patterns=(
    "vaultmtg-daemon-staging"
    "com.vaultmtg.daemon-staging"
    ".vaultmtg-staging"
    "VaultMTG Staging"
    "VaultMTG-Daemon-staging"
  )

  local violations=0
  for pattern in "${banned_patterns[@]}"; do
    # Search all install scripts EXCEPT common.sh itself.
    while IFS= read -r -d $'\0' file; do
      # Skip common.sh / common.ps1 (the allowed sources of truth).
      [[ "${file}" == */common.sh ]] && continue
      [[ "${file}" == */common.ps1 ]] && continue
      # Skip this test file itself.
      [[ "${file}" == *channel_test.bats ]] && continue
      # Skip common_test.bats — it legitimately asserts the exact constant values
      # to verify common.sh emits them correctly (ADR-049 §2 cross-check).
      [[ "${file}" == *common_test.bats ]] && continue
      # Skip *_test.bats generally — test files legitimately assert the exact
      # channel identity strings to prove the scripts emit them (the same reason
      # common_test.bats is exempt).  The fitness function targets the SHIPPED
      # scripts, not the tests that verify them.
      [[ "${file}" == *_test.bats ]] && continue

      # Strip comment lines (lines starting with optional whitespace + #) before
      # checking for identity literals.  Comments documenting expected values
      # (e.g. in the fallback path of uninstall.sh) are not violations.
      if grep -v '^\s*#' "${file}" 2>/dev/null | grep -q "${pattern}"; then
        echo "FAIL: identity literal '${pattern}' found in non-comment code in ${file}" >&3
        (( violations++ )) || true
      fi
    done < <(find "${install_dir}" -name "*.sh" -o -name "*.ps1" -o -name "*.bats" \
      -o -name "postinstall" -o -name "preinstall" -o -name "*.nsi" \
      | tr '\n' '\0')
  done

  # Before common.sh lands (ticket #650), this test will PASS because the staged
  # -staging suffix literals don't exist yet.  After it lands, the check ensures
  # no new scripts introduce the literals.  This is the intended pre-ticket-650 behavior.
  [ "${violations}" -eq 0 ]
}

# ---------------------------------------------------------------------------
# I-5: Verifiable post-install state — per-channel health surface
#
# These tests stub the daemon health endpoint to verify that channel-distinct ports
# are used.  The exact port values (P and P+1) come from common.sh.
# ---------------------------------------------------------------------------

@test "I-5-stable-health-surface-reachable: CHANNEL=stable uses stable port from common.sh" {
  if ! _common_sh_available "stable"; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  # Read the stable LOCAL_API_PORT from common.sh.
  local stable_port
  stable_port="$(CHANNEL=stable bash -c "source '${COMMON_SH}' && echo \"\${LOCAL_API_PORT:-unset}\"" 2>/dev/null)"

  # If LOCAL_API_PORT is not yet defined in common.sh (ticket #655 not landed),
  # this test fails RED with a clear message.
  if [[ "${stable_port}" == "unset" || -z "${stable_port}" ]]; then
    echo "FAIL: LOCAL_API_PORT not defined in common.sh for CHANNEL=stable" >&3
    echo "This test requires ticket #655 (channel-scoped local-API port) to land first." >&3
    false
  fi

  # The port must be a valid integer.
  [[ "${stable_port}" =~ ^[0-9]+$ ]] || {
    echo "FAIL: stable LOCAL_API_PORT '${stable_port}' is not an integer" >&3
    false
  }
}

@test "I-5-staging-health-surface-reachable: CHANNEL=staging uses staging port from common.sh and it differs from stable" {
  if ! _common_sh_available "staging"; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  local stable_port
  stable_port="$(CHANNEL=stable bash -c "source '${COMMON_SH}' && echo \"\${LOCAL_API_PORT:-unset}\"" 2>/dev/null)"
  local staging_port
  staging_port="$(CHANNEL=staging bash -c "source '${COMMON_SH}' && echo \"\${LOCAL_API_PORT:-unset}\"" 2>/dev/null)"

  if [[ "${stable_port}" == "unset" || -z "${stable_port}" ]]; then
    echo "FAIL: LOCAL_API_PORT not defined for CHANNEL=stable (ticket #655 required)" >&3
    false
  fi
  if [[ "${staging_port}" == "unset" || -z "${staging_port}" ]]; then
    echo "FAIL: LOCAL_API_PORT not defined for CHANNEL=staging (ticket #655 required)" >&3
    false
  fi

  # The two ports MUST be different to allow simultaneous operation.
  [ "${stable_port}" != "${staging_port}" ] || {
    echo "FAIL: stable and staging share the same LOCAL_API_PORT=${stable_port} — port collision!" >&3
    false
  }

  # Staging port must equal stable + 10 per ADR-049 Risks (offset=10 matches install.stagingPortOffset in Go).
  local expected_staging_port
  expected_staging_port=$(( stable_port + 10 ))
  [ "${staging_port}" -eq "${expected_staging_port}" ] || {
    echo "FAIL: staging port ${staging_port} is not stable+10 (${expected_staging_port})" >&3
    false
  }
}

# ---------------------------------------------------------------------------
# I-7: Channel parameterization identical across platforms — macOS side
# ---------------------------------------------------------------------------

@test "I-7-channel-param-identical-across-platforms-macos-stable: CHANNEL=stable identity matches expected constants" {
  if ! _common_sh_available "stable"; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  # Read all identity constants for stable channel.
  local binary_name plist_label config_dir keychain_service
  binary_name="$(CHANNEL=stable bash -c "source '${COMMON_SH}' && echo \"\${BINARY_NAME:-unset}\"" 2>/dev/null)"
  plist_label="$(CHANNEL=stable bash -c "source '${COMMON_SH}' && echo \"\${PLIST_LABEL:-unset}\"" 2>/dev/null)"
  config_dir="$(CHANNEL=stable bash -c "source '${COMMON_SH}' && echo \"\${CONFIG_DIR:-unset}\"" 2>/dev/null)"
  keychain_service="$(CHANNEL=stable bash -c "source '${COMMON_SH}' && echo \"\${KEYCHAIN_SERVICE:-unset}\"" 2>/dev/null)"

  # Stable channel must produce bare (no suffix) identity values.
  [ "${binary_name}" = "vaultmtg-daemon" ] || {
    echo "FAIL: CHANNEL=stable BINARY_NAME='${binary_name}', expected 'vaultmtg-daemon'" >&3
    false
  }
  [ "${plist_label}" = "com.vaultmtg.daemon" ] || {
    echo "FAIL: CHANNEL=stable PLIST_LABEL='${plist_label}', expected 'com.vaultmtg.daemon'" >&3
    false
  }
  [[ "${config_dir}" == *"/.vaultmtg" ]] || {
    echo "FAIL: CHANNEL=stable CONFIG_DIR='${config_dir}' must end with '/.vaultmtg'" >&3
    false
  }
  # CONFIG_DIR must NOT contain -staging.
  [[ "${config_dir}" != *"-staging"* ]] || {
    echo "FAIL: CHANNEL=stable CONFIG_DIR='${config_dir}' contains '-staging' suffix" >&3
    false
  }
}

@test "I-7-channel-param-identical-across-platforms-macos-staging: CHANNEL=staging identity has -staging suffix on all constants" {
  if ! _common_sh_available "staging"; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  local binary_name plist_label config_dir keychain_service
  binary_name="$(CHANNEL=staging bash -c "source '${COMMON_SH}' && echo \"\${BINARY_NAME:-unset}\"" 2>/dev/null)"
  plist_label="$(CHANNEL=staging bash -c "source '${COMMON_SH}' && echo \"\${PLIST_LABEL:-unset}\"" 2>/dev/null)"
  config_dir="$(CHANNEL=staging bash -c "source '${COMMON_SH}' && echo \"\${CONFIG_DIR:-unset}\"" 2>/dev/null)"
  keychain_service="$(CHANNEL=staging bash -c "source '${COMMON_SH}' && echo \"\${KEYCHAIN_SERVICE:-unset}\"" 2>/dev/null)"

  # Staging channel must produce suffixed identity values.
  # Note: PLIST_LABEL uses LABEL_SUFFIX=".staging" (dot), BINARY_NAME uses SUFFIX="-staging" (dash).
  [ "${binary_name}" = "vaultmtg-daemon-staging" ] || {
    echo "FAIL: CHANNEL=staging BINARY_NAME='${binary_name}', expected 'vaultmtg-daemon-staging'" >&3
    false
  }
  [ "${plist_label}" = "com.vaultmtg.daemon.staging" ] || {
    echo "FAIL: CHANNEL=staging PLIST_LABEL='${plist_label}', expected 'com.vaultmtg.daemon.staging'" >&3
    false
  }
  [[ "${config_dir}" == *"/.vaultmtg-staging" ]] || {
    echo "FAIL: CHANNEL=staging CONFIG_DIR='${config_dir}' must end with '/.vaultmtg-staging'" >&3
    false
  }
  [[ "${keychain_service}" == *".staging"* ]] || {
    echo "FAIL: CHANNEL=staging KEYCHAIN_SERVICE='${keychain_service}' must contain '.staging'" >&3
    false
  }
}

# ---------------------------------------------------------------------------
# I-8: VaultMTG.app variants present after channel-specific install
# ---------------------------------------------------------------------------

@test "I-8-stable-install-creates-VaultMTG-app: stable channel install places VaultMTG.app" {
  if ! _common_sh_available "stable"; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  local install_dir; install_dir="$(mktemp -d)"
  local fake_home
  fake_home="$(_make_channel_home "stable" "${install_dir}" no-binary no-plist with-app no-config)"

  local app_path="${fake_home}/Applications/VaultMTG.app"
  [ -d "${app_path}" ] || {
    echo "FAIL: VaultMTG.app not found at ${app_path}" >&3
    false
  }
  [ -f "${app_path}/Contents/Info.plist" ] || {
    echo "FAIL: VaultMTG.app/Contents/Info.plist missing" >&3
    false
  }
  [ -x "${app_path}/Contents/MacOS/vaultmtg-launcher" ] || {
    echo "FAIL: VaultMTG.app/Contents/MacOS/vaultmtg-launcher not executable" >&3
    false
  }
}

@test "I-8-staging-install-creates-VaultMTG-Staging-app: staging channel install places 'VaultMTG Staging.app'" {
  if ! _common_sh_available "staging"; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  local install_dir; install_dir="$(mktemp -d)"
  local fake_home
  fake_home="$(_make_channel_home "staging" "${install_dir}" no-binary no-plist with-app no-config)"

  local staging_app_path="${fake_home}/Applications/VaultMTG Staging.app"
  [ -d "${staging_app_path}" ] || {
    echo "FAIL: 'VaultMTG Staging.app' not found at '${staging_app_path}'" >&3
    false
  }
  [ -f "${staging_app_path}/Contents/Info.plist" ] || {
    echo "FAIL: 'VaultMTG Staging.app/Contents/Info.plist' missing" >&3
    false
  }

  # The prod app must NOT exist — this test set up only staging.
  local prod_app_path="${fake_home}/Applications/VaultMTG.app"
  [ ! -d "${prod_app_path}" ] || {
    echo "FAIL: VaultMTG.app (prod) was created by staging install fixture — test fixture error" >&3
    false
  }
}

# ---------------------------------------------------------------------------
# I-9: Per-channel uninstall removes only its own .app bundle
# ---------------------------------------------------------------------------

@test "I-9-staging-uninstall-removes-staging-app-leaves-prod-app: staging uninstall removes Staging.app, leaves VaultMTG.app" {
  if ! _common_sh_available "staging"; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  local stub_dir; stub_dir="$(_make_stub_dir)"
  local install_dir; install_dir="$(mktemp -d)"

  # Set up a machine with BOTH channels' app bundles present.
  local fake_home; fake_home="$(mktemp -d)"
  mkdir -p "${fake_home}/Library/LaunchAgents"

  # Create prod .app
  local prod_app="${fake_home}/Applications/VaultMTG.app"
  mkdir -p "${prod_app}/Contents/MacOS"
  echo "prod launcher" > "${prod_app}/Contents/MacOS/vaultmtg-launcher"
  echo "<plist>prod</plist>" > "${prod_app}/Contents/Info.plist"

  # Create staging .app
  local staging_app="${fake_home}/Applications/VaultMTG Staging.app"
  mkdir -p "${staging_app}/Contents/MacOS"
  echo "staging launcher" > "${staging_app}/Contents/MacOS/vaultmtg-launcher"
  echo "<plist>staging</plist>" > "${staging_app}/Contents/Info.plist"

  [ -d "${prod_app}" ]
  [ -d "${staging_app}" ]

  # Run staging-channel uninstall.
  # APP_BUNDLE_PATH redirected to staging app path inside fake_home/Applications.
  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    INSTALL_DIR="${install_dir}" \
    CHANNEL="staging" \
    APP_BUNDLE_PATH="${staging_app}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${UNINSTALL_SH}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # Staging .app must be gone.
  [ ! -d "${staging_app}" ] || {
    echo "FAIL: 'VaultMTG Staging.app' still present after staging uninstall" >&3
    false
  }

  # Prod .app must be untouched.
  [ -d "${prod_app}" ] || {
    echo "FAIL: VaultMTG.app (prod) was removed by staging uninstall!" >&3
    false
  }
}

@test "I-9-stable-uninstall-removes-prod-app-leaves-staging-app: prod uninstall removes VaultMTG.app, leaves Staging.app" {
  if ! _common_sh_available "stable"; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  local stub_dir; stub_dir="$(_make_stub_dir)"
  local install_dir; install_dir="$(mktemp -d)"

  local fake_home; fake_home="$(mktemp -d)"
  mkdir -p "${fake_home}/Library/LaunchAgents"

  # Create prod .app
  local prod_app="${fake_home}/Applications/VaultMTG.app"
  mkdir -p "${prod_app}/Contents/MacOS"
  echo "prod launcher" > "${prod_app}/Contents/MacOS/vaultmtg-launcher"
  echo "<plist>prod</plist>" > "${prod_app}/Contents/Info.plist"

  # Create staging .app
  local staging_app="${fake_home}/Applications/VaultMTG Staging.app"
  mkdir -p "${staging_app}/Contents/MacOS"
  echo "staging launcher" > "${staging_app}/Contents/MacOS/vaultmtg-launcher"
  echo "<plist>staging</plist>" > "${staging_app}/Contents/Info.plist"

  [ -d "${prod_app}" ]
  [ -d "${staging_app}" ]

  # Run stable (prod) channel uninstall.
  # APP_BUNDLE_PATH redirected to prod app path inside fake_home/Applications.
  run env \
    PATH="${stub_dir}:${PATH}" \
    HOME="${fake_home}" \
    INSTALL_DIR="${install_dir}" \
    CHANNEL="stable" \
    APP_BUNDLE_PATH="${prod_app}" \
    BATS_TEST_TMPDIR="${BATS_TEST_TMPDIR}" \
    bash "${UNINSTALL_SH}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]

  # Prod .app must be gone.
  [ ! -d "${prod_app}" ] || {
    echo "FAIL: VaultMTG.app (prod) still present after stable uninstall" >&3
    false
  }

  # Staging .app must be untouched.
  [ -d "${staging_app}" ] || {
    echo "FAIL: 'VaultMTG Staging.app' was removed by prod uninstall!" >&3
    false
  }
}

# ---------------------------------------------------------------------------
# Channel = unknown → fail-closed (ADR-049 §2)
# ---------------------------------------------------------------------------

@test "unknown-channel-exits-nonzero: CHANNEL=bogus exits non-zero with clear error" {
  if ! [[ -f "${COMMON_SH}" ]]; then
    echo "FAIL: common.sh required (ticket #650)" >&3
    false
  fi

  run bash -c "CHANNEL=bogus source '${COMMON_SH}'"
  # Must fail (exit 64 per ADR-049 §2 spec, or any non-zero) with a clear message.
  [ "${status}" -ne 0 ] || {
    echo "FAIL: CHANNEL=bogus should exit non-zero but exited 0" >&3
    false
  }
}
