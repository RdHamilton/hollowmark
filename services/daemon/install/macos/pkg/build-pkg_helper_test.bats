#!/usr/bin/env bats
# build-pkg_helper_test.bats — asserts that build-pkg.sh includes the
# collection-agent-helper binary in the pkg-root payload (hollowmark-tickets#1286
# R2, ADR-059 privilege-model migration).
#
# Run with:
#   bats services/daemon/install/macos/pkg/build-pkg_helper_test.bats
#
# Strategy: run build-pkg.sh in DRY_RUN=1 mode (which skips pkgbuild/hdiutil
# and prints the pkg-root layout), then assert that the helper binary is
# present at SHARE_DIR/collection-helper and that the LaunchDaemon install/
# subdirectory (removed by ADR-059 / hollowmark-tickets#892) is ABSENT.
#
# DRY_RUN mode requires:
#   BINARY_PATH — path to a dummy daemon binary (content irrelevant)
#   VERSION     — any semver string
#   HELPER_BINARY_PATH — path to a dummy collection-helper binary (R2)

BUILD_PKG_SH="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/build-pkg.sh"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

# Create a minimal stub dir so commands that might be called in non-dry-run
# paths (pkgbuild, hdiutil) fail loudly rather than silently skipping.
_make_stub_dir() {
  local stub_dir; stub_dir="$(mktemp -d)"
  for cmd in pkgbuild hdiutil; do
    cat > "${stub_dir}/${cmd}" <<EOF
#!/usr/bin/env bash
echo "UNEXPECTED: ${cmd} called in DRY_RUN mode" >&2
exit 1
EOF
    chmod +x "${stub_dir}/${cmd}"
  done
  echo "${stub_dir}"
}

# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

# 1. pkg-root contains collection-helper binary at SHARE_DIR/collection-helper
@test "build-pkg.sh dry-run: pkg-root contains collection-helper at SHARE_DIR/collection-helper (R2)" {
  local stub_dir; stub_dir="$(_make_stub_dir)"
  local fake_binary; fake_binary="$(mktemp)"
  echo "fake daemon" > "${fake_binary}"
  local fake_helper; fake_helper="$(mktemp)"
  echo "fake helper" > "${fake_helper}"

  run env \
    PATH="${stub_dir}:${PATH}" \
    BINARY_PATH="${fake_binary}" \
    VERSION="0.4.3" \
    HELPER_BINARY_PATH="${fake_helper}" \
    DRY_RUN=1 \
    bash "${BUILD_PKG_SH}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  # Must list collection-helper under SHARE_DIR (/usr/local/share/vaultmtg/).
  echo "${output}" | grep -q "usr/local/share/vaultmtg/collection-helper"
}

# 2. ADR-059: pkg-root does NOT contain install/ subdirectory (LaunchDaemon
#    plist and install-helper.sh were removed; helper runs as user, not root).
@test "build-pkg.sh dry-run: pkg-root does NOT contain LaunchDaemon install/ subdir (ADR-059)" {
  local stub_dir; stub_dir="$(_make_stub_dir)"
  local fake_binary; fake_binary="$(mktemp)"
  echo "fake daemon" > "${fake_binary}"
  local fake_helper; fake_helper="$(mktemp)"
  echo "fake helper" > "${fake_helper}"

  run env \
    PATH="${stub_dir}:${PATH}" \
    BINARY_PATH="${fake_binary}" \
    VERSION="0.4.3" \
    HELPER_BINARY_PATH="${fake_helper}" \
    DRY_RUN=1 \
    bash "${BUILD_PKG_SH}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  # install/ subdir must be absent — its presence would mean the LaunchDaemon
  # model files are still being bundled (ADR-059 §Fitness Functions).
  ! echo "${output}" | grep -q "usr/local/share/vaultmtg/install/"
}

# 3. ADR-059: the LaunchDaemon plist must not appear anywhere in the pkg-root.
@test "build-pkg.sh dry-run: pkg-root does NOT contain com.vaultmtg.collection-helper.plist (ADR-059)" {
  local stub_dir; stub_dir="$(_make_stub_dir)"
  local fake_binary; fake_binary="$(mktemp)"
  echo "fake daemon" > "${fake_binary}"
  local fake_helper; fake_helper="$(mktemp)"
  echo "fake helper" > "${fake_helper}"

  run env \
    PATH="${stub_dir}:${PATH}" \
    BINARY_PATH="${fake_binary}" \
    VERSION="0.4.3" \
    HELPER_BINARY_PATH="${fake_helper}" \
    DRY_RUN=1 \
    bash "${BUILD_PKG_SH}"

  echo "status: ${status}"
  echo "output: ${output}"
  [ "${status}" -eq 0 ]
  ! echo "${output}" | grep -q "com.vaultmtg.collection-helper.plist"
}

# 4. build-pkg.sh exits non-zero when HELPER_BINARY_PATH is not set
@test "build-pkg.sh: exits non-zero when HELPER_BINARY_PATH is unset" {
  local stub_dir; stub_dir="$(_make_stub_dir)"
  local fake_binary; fake_binary="$(mktemp)"
  echo "fake daemon" > "${fake_binary}"

  run env \
    PATH="${stub_dir}:${PATH}" \
    BINARY_PATH="${fake_binary}" \
    VERSION="0.4.3" \
    DRY_RUN=1 \
    bash "${BUILD_PKG_SH}"

  echo "status: ${status}"
  echo "output: ${output}"
  # Must fail when HELPER_BINARY_PATH is missing.
  [ "${status}" -ne 0 ]
}
