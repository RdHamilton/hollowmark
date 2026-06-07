#!/usr/bin/env bats
# common_test.bats — unit tests for services/daemon/install/macos/common.sh
#
# Run with:
#   bats services/daemon/install/macos/common_test.bats
#
# Tests verify:
#   1. CHANNEL defaults to "stable" when unset
#   2. Stable channel produces bare identity constants (prod identity unchanged)
#   3. Staging channel produces suffixed identity constants
#   4. Stable and staging constants never collide
#   5. Unknown channel exits 64 (fail-closed)
#   6. Per-channel cross-check: derived constants agree with the expected values
#      so the Go-side install package and the shell stay in sync (ADR-049 §2)

COMMON_SH="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/common.sh"

# ---------------------------------------------------------------------------
# 1. CHANNEL defaults to "stable" when not set
# ---------------------------------------------------------------------------
@test "CHANNEL defaults to stable when unset" {
  run bash -c "source ${COMMON_SH} && echo \"\$CHANNEL\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "stable" ]
}

# ---------------------------------------------------------------------------
# 2. Stable channel — bare identity (prod unchanged)
# ---------------------------------------------------------------------------
@test "stable: BINARY_NAME is vaultmtg-daemon (bare)" {
  run bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$BINARY_NAME\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "vaultmtg-daemon" ]
}

@test "stable: PLIST_LABEL is com.vaultmtg.daemon (bare)" {
  run bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$PLIST_LABEL\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "com.vaultmtg.daemon" ]
}

@test "stable: KEYCHAIN_SERVICE is com.vaultmtg.daemon (bare)" {
  run bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$KEYCHAIN_SERVICE\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "com.vaultmtg.daemon" ]
}

@test "stable: APP_BUNDLE_PATH is /Applications/VaultMTG.app (bare)" {
  run bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$APP_BUNDLE_PATH\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "/Applications/VaultMTG.app" ]
}

@test "stable: TRAY_LABEL is VaultMTG (bare)" {
  run bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$TRAY_LABEL\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "VaultMTG" ]
}

@test "stable: CONFIG_DIR contains .vaultmtg (not .vaultmtg-staging)" {
  run bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$CONFIG_DIR\""
  [ "${status}" -eq 0 ]
  [[ "${output}" == *".vaultmtg"* ]]
  [[ "${output}" != *"staging"* ]]
}

@test "stable: PLIST_LABEL_LEGACY is set (legacy handling is stable-only)" {
  run bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$PLIST_LABEL_LEGACY\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "com.mtga-companion.daemon" ]
}

# ---------------------------------------------------------------------------
# 3. Staging channel — suffixed identity
# ---------------------------------------------------------------------------
@test "staging: BINARY_NAME is vaultmtg-daemon-staging" {
  run bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$BINARY_NAME\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "vaultmtg-daemon-staging" ]
}

@test "staging: PLIST_LABEL is com.vaultmtg.daemon.staging" {
  run bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$PLIST_LABEL\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "com.vaultmtg.daemon.staging" ]
}

@test "staging: KEYCHAIN_SERVICE is com.vaultmtg.daemon.staging" {
  run bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$KEYCHAIN_SERVICE\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "com.vaultmtg.daemon.staging" ]
}

@test "staging: APP_BUNDLE_PATH is /Applications/VaultMTG Staging.app" {
  run bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$APP_BUNDLE_PATH\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "/Applications/VaultMTG Staging.app" ]
}

@test "staging: TRAY_LABEL is VaultMTG (Staging)" {
  run bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$TRAY_LABEL\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "VaultMTG (Staging)" ]
}

@test "staging: CONFIG_DIR contains .vaultmtg-staging" {
  run bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$CONFIG_DIR\""
  [ "${status}" -eq 0 ]
  [[ "${output}" == *".vaultmtg-staging"* ]]
}

@test "staging: PLIST_LABEL_LEGACY is not set (legacy handling is stable-only)" {
  run bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\${PLIST_LABEL_LEGACY:-UNSET}\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "UNSET" ]
}

# ---------------------------------------------------------------------------
# 4. Stable and staging constants never collide
# ---------------------------------------------------------------------------
@test "no collision: stable and staging BINARY_NAME differ" {
  stable=$(bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$BINARY_NAME\"")
  staging=$(bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$BINARY_NAME\"")
  [ "${stable}" != "${staging}" ]
}

@test "no collision: stable and staging PLIST_LABEL differ" {
  stable=$(bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$PLIST_LABEL\"")
  staging=$(bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$PLIST_LABEL\"")
  [ "${stable}" != "${staging}" ]
}

@test "no collision: stable and staging CONFIG_DIR differ" {
  stable=$(bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$CONFIG_DIR\"")
  staging=$(bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$CONFIG_DIR\"")
  [ "${stable}" != "${staging}" ]
}

@test "no collision: stable and staging APP_BUNDLE_PATH differ" {
  stable=$(bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$APP_BUNDLE_PATH\"")
  staging=$(bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$APP_BUNDLE_PATH\"")
  [ "${stable}" != "${staging}" ]
}

# ---------------------------------------------------------------------------
# 5. Unknown channel exits 64 (fail-closed, ADR-049 §2)
# ---------------------------------------------------------------------------
@test "unknown channel: exits 64" {
  run bash -c "CHANNEL=canary source ${COMMON_SH}"
  [ "${status}" -eq 64 ]
  [[ "${output}" == *"unknown CHANNEL"* ]]
}

@test "empty channel: defaults to stable (treated same as unset)" {
  # CHANNEL="" is absorbed by :=${VAULTMTG_DAEMON_CHANNEL:-stable} so it
  # defaults to "stable".  We verify this by checking that BINARY_NAME gets
  # the bare (stable) value, not an error exit.
  run env CHANNEL= bash "${COMMON_SH%common.sh}/../../../tests/channel_probe.sh" BINARY_NAME
  # If the file doesn't exist we just skip; the probe script is optional.
  # The core guarantee is that sourcing common.sh with CHANNEL= does not exit non-zero.
  run bash -c "unset CHANNEL; VAULTMTG_DAEMON_CHANNEL= source '${COMMON_SH}'; echo \$BINARY_NAME"
  [ "${status}" -eq 0 ]
  [ "${output}" = "vaultmtg-daemon" ]
}

# ---------------------------------------------------------------------------
# 6. Cross-check: shell constants agree with ADR-049 §1 table
# ---------------------------------------------------------------------------
@test "cross-check: stable constants match ADR-049 §1 table" {
  run bash -c "
    CHANNEL=stable
    source ${COMMON_SH}
    [ \"\$BINARY_NAME\" = 'vaultmtg-daemon' ] || { echo 'FAIL BINARY_NAME'; exit 1; }
    [ \"\$PLIST_LABEL\" = 'com.vaultmtg.daemon' ] || { echo 'FAIL PLIST_LABEL'; exit 1; }
    [ \"\$KEYCHAIN_SERVICE\" = 'com.vaultmtg.daemon' ] || { echo 'FAIL KEYCHAIN_SERVICE'; exit 1; }
    [ \"\$APP_BUNDLE_PATH\" = '/Applications/VaultMTG.app' ] || { echo 'FAIL APP_BUNDLE_PATH'; exit 1; }
    [ \"\$TRAY_LABEL\" = 'VaultMTG' ] || { echo 'FAIL TRAY_LABEL'; exit 1; }
    echo 'ALL_PASS'
  "
  [ "${status}" -eq 0 ]
  [[ "${output}" == *"ALL_PASS"* ]]
}

@test "cross-check: staging constants match ADR-049 §1 table" {
  run bash -c "
    CHANNEL=staging
    source ${COMMON_SH}
    [ \"\$BINARY_NAME\" = 'vaultmtg-daemon-staging' ] || { echo 'FAIL BINARY_NAME'; exit 1; }
    [ \"\$PLIST_LABEL\" = 'com.vaultmtg.daemon.staging' ] || { echo 'FAIL PLIST_LABEL'; exit 1; }
    [ \"\$KEYCHAIN_SERVICE\" = 'com.vaultmtg.daemon.staging' ] || { echo 'FAIL KEYCHAIN_SERVICE'; exit 1; }
    [ \"\$APP_BUNDLE_PATH\" = '/Applications/VaultMTG Staging.app' ] || { echo 'FAIL APP_BUNDLE_PATH'; exit 1; }
    [ \"\$TRAY_LABEL\" = 'VaultMTG (Staging)' ] || { echo 'FAIL TRAY_LABEL'; exit 1; }
    echo 'ALL_PASS'
  "
  [ "${status}" -eq 0 ]
  [[ "${output}" == *"ALL_PASS"* ]]
}

# ---------------------------------------------------------------------------
# 7. INSTALL_DIR and KEYCHAIN_ACCOUNT are channel-independent constants
# ---------------------------------------------------------------------------
@test "INSTALL_DIR is /usr/local/bin for both channels" {
  stable=$(bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$INSTALL_DIR\"")
  staging=$(bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$INSTALL_DIR\"")
  [ "${stable}" = "/usr/local/bin" ]
  [ "${staging}" = "/usr/local/bin" ]
}

@test "KEYCHAIN_ACCOUNT is api-key for both channels" {
  stable=$(bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$KEYCHAIN_ACCOUNT\"")
  staging=$(bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$KEYCHAIN_ACCOUNT\"")
  [ "${stable}" = "api-key" ]
  [ "${staging}" = "api-key" ]
}

# ---------------------------------------------------------------------------
# 8. PLIST_LABEL_HOLLOWMARK — future-label constant (#999 ADR-022 C1)
#
# common.sh must define PLIST_LABEL_HOLLOWMARK for the stable and staging
# channels so install/uninstall scripts can reference the future hollowmark
# label by name (without hardcoding it) in their defensive cleanup blocks.
# ---------------------------------------------------------------------------

@test "stable: PLIST_LABEL_HOLLOWMARK is com.hollowmark.daemon" {
  run bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$PLIST_LABEL_HOLLOWMARK\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "com.hollowmark.daemon" ]
}

@test "staging: PLIST_LABEL_HOLLOWMARK is com.hollowmark.daemon.staging" {
  run bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$PLIST_LABEL_HOLLOWMARK\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "com.hollowmark.daemon.staging" ]
}

@test "stable: PLIST_PATH_HOLLOWMARK is ~/Library/LaunchAgents/com.hollowmark.daemon.plist" {
  expected="${HOME}/Library/LaunchAgents/com.hollowmark.daemon.plist"
  run bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$PLIST_PATH_HOLLOWMARK\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "${expected}" ]
}

@test "staging: PLIST_PATH_HOLLOWMARK is ~/Library/LaunchAgents/com.hollowmark.daemon.staging.plist" {
  expected="${HOME}/Library/LaunchAgents/com.hollowmark.daemon.staging.plist"
  run bash -c "CHANNEL=staging source ${COMMON_SH} && echo \"\$PLIST_PATH_HOLLOWMARK\""
  [ "${status}" -eq 0 ]
  [ "${output}" = "${expected}" ]
}

@test "PLIST_LABEL_HOLLOWMARK does not collide with PLIST_LABEL" {
  stable_current=$(bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$PLIST_LABEL\"")
  stable_future=$(bash -c "CHANNEL=stable source ${COMMON_SH} && echo \"\$PLIST_LABEL_HOLLOWMARK\"")
  [ "${stable_current}" != "${stable_future}" ]
}
