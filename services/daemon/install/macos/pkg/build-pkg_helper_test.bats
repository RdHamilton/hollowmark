#!/usr/bin/env bats
# build-pkg_helper_test.bats — asserts that build-pkg.sh includes the
# collection-agent-helper binary and install/ directory in the pkg-root
# payload (hollowmark-tickets#1286, R2).
#
# Run with:
#   bats services/daemon/install/macos/pkg/build-pkg_helper_test.bats
#
# Strategy: run build-pkg.sh in DRY_RUN=1 mode (which skips pkgbuild/hdiutil
# and prints the pkg-root layout), then assert that the helper binary and
# install/ subdirectory are present at the expected SHARE_DIR paths.
#
# DRY_RUN mode requires:
#   BINARY_PATH — path to a dummy daemon binary (content irrelevant)
#   VERSION     — any semver string
#   HELPER_BINARY_PATH — path to a dummy collection-helper binary (R2 addition)
#
# HELPER_BINARY_PATH is the new env var added by this ticket; asserting it
# produces the correct layout is the CI-executable part of AC4.

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

# 2. pkg-root contains install-helper.sh at SHARE_DIR/install/install-helper.sh
@test "build-pkg.sh dry-run: pkg-root contains install-helper.sh at SHARE_DIR/install/ (R2)" {
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
  echo "${output}" | grep -q "usr/local/share/vaultmtg/install/install-helper.sh"
}

# 3. pkg-root contains the collection-helper plist at SHARE_DIR/install/com.vaultmtg.collection-helper.plist
@test "build-pkg.sh dry-run: pkg-root contains collection-helper.plist at SHARE_DIR/install/ (R2)" {
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
  echo "${output}" | grep -q "com.vaultmtg.collection-helper.plist"
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
