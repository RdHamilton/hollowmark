#!/usr/bin/env bats
# app-launcher_test.bats — tests for services/daemon/install/macos/pkg/app-launcher
#
# Verifies that the VaultMTG.app launcher script:
#   1. Calls launchctl enable + bootstrap (existing behaviour, preserved)
#   2. Posts a macOS notification via osascript after bootstrapping (AC1, #636)
#
# Strategy:
#   - Stub launchctl and osascript in a temp stub dir prepended to PATH.
#   - Stub osascript records the call so we can assert it fired.
#   - Run the launcher script directly as a subprocess.
#
# Run with:
#   bats services/daemon/install/macos/pkg/app-launcher_test.bats

LAUNCHER_SCRIPT="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/app-launcher"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

# Build a stub directory whose executables replace privileged OS tools.
_make_stub_dir() {
  local stub_dir
  stub_dir="$(mktemp -d)"

  # launchctl stub — always succeeds, records calls
  cat > "${stub_dir}/launchctl" <<'EOF'
#!/usr/bin/env bash
echo "stub-launchctl: $*" >> "${BATS_TEST_TMPDIR}/launchctl_calls"
exit 0
EOF
  chmod +x "${stub_dir}/launchctl"

  # osascript stub — records call to confirm notification was posted
  cat > "${stub_dir}/osascript" <<'EOF'
#!/usr/bin/env bash
echo "stub-osascript: $*" >> "${BATS_TEST_TMPDIR}/osascript_calls"
exit 0
EOF
  chmod +x "${stub_dir}/osascript"

  echo "${stub_dir}"
}

# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

@test "launcher posts osascript notification after bootstrap" {
  stub_dir="$(_make_stub_dir)"
  export HOME="${BATS_TEST_TMPDIR}"

  run env PATH="${stub_dir}:${PATH}" bash "${LAUNCHER_SCRIPT}"

  [ "$status" -eq 0 ]

  # osascript must have been called at least once
  [ -f "${BATS_TEST_TMPDIR}/osascript_calls" ] || {
    echo "osascript was never called — no notification posted" >&2
    return 1
  }

  # The call must mention "display notification" (confirms notification form, not just any osascript)
  grep -q "display notification" "${BATS_TEST_TMPDIR}/osascript_calls" || {
    echo "osascript was called but without 'display notification':" >&2
    cat "${BATS_TEST_TMPDIR}/osascript_calls" >&2
    return 1
  }
}

@test "launcher calls launchctl enable and bootstrap" {
  stub_dir="$(_make_stub_dir)"
  export HOME="${BATS_TEST_TMPDIR}"

  run env PATH="${stub_dir}:${PATH}" bash "${LAUNCHER_SCRIPT}"

  [ "$status" -eq 0 ]

  # launchctl must have been called
  [ -f "${BATS_TEST_TMPDIR}/launchctl_calls" ] || {
    echo "launchctl was never called" >&2
    return 1
  }

  # must call 'enable' (existing behaviour)
  grep -q "enable" "${BATS_TEST_TMPDIR}/launchctl_calls" || {
    echo "launchctl enable not called:" >&2
    cat "${BATS_TEST_TMPDIR}/launchctl_calls" >&2
    return 1
  }

  # must call 'bootstrap' (existing behaviour)
  grep -q "bootstrap" "${BATS_TEST_TMPDIR}/launchctl_calls" || {
    echo "launchctl bootstrap not called:" >&2
    cat "${BATS_TEST_TMPDIR}/launchctl_calls" >&2
    return 1
  }
}

@test "launcher notification text mentions VaultMTG" {
  stub_dir="$(_make_stub_dir)"
  export HOME="${BATS_TEST_TMPDIR}"

  run env PATH="${stub_dir}:${PATH}" bash "${LAUNCHER_SCRIPT}"

  [ "$status" -eq 0 ]

  # notification must mention VaultMTG (brand consistency)
  grep -qi "VaultMTG" "${BATS_TEST_TMPDIR}/osascript_calls" || {
    echo "notification does not mention VaultMTG:" >&2
    cat "${BATS_TEST_TMPDIR}/osascript_calls" >&2
    return 1
  }
}
