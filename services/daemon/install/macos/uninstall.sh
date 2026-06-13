#!/usr/bin/env bash
# uninstall.sh — removes the VaultMTG daemon from macOS.
#
# Usage:
#   bash uninstall.sh [--purge]
#
#   Set CHANNEL=stable (default) or CHANNEL=staging before invoking to target
#   a specific channel's install artifacts.  When CHANNEL is unset, defaults to
#   stable (backward-compatible).
#
# Options:
#   --purge   Also delete the daemon's API key from the macOS Keychain.
#             By default the keychain entry is retained for downgrade safety.
#
# Channel behaviour (ADR-049 §2 + I-2 cross-channel non-interference):
#   CHANNEL=stable  removes vaultmtg-daemon, com.vaultmtg.daemon plist, VaultMTG.app
#   CHANNEL=staging removes vaultmtg-daemon-staging, com.vaultmtg.daemon-staging plist,
#                   "VaultMTG Staging.app" — NEVER touches stable channel artifacts.
#
# Steps (ADR-022 Phase 2):
#   1. Unloads and disables the channel-appropriate launchd job.
#   2. Unloads the legacy launchd job (com.mtga-companion.daemon) if present —
#      upgrader path (stable channel only; staging never uses the legacy label).
#   3. Removes the channel-appropriate plist from ~/Library/LaunchAgents/.
#   4. Removes the channel-appropriate binary from INSTALL_DIR.
#   5. Removes the legacy binary (mtga-companion-daemon) if present (stable only).
#   6. Removes the channel-appropriate .app bundle from /Applications.
#   7. Removes the collection-agent-helper (hollowmark-tickets#1286; ADR-059):
#      - Best-effort legacy: boots out com.vaultmtg.collection-helper from
#        system launchd (|| true; no-op on post-ADR-059 installs).
#      - Best-effort legacy: removes HELPER_LAUNCHDAEMONS_DIR plist (|| true).
#      - Best-effort legacy: removes install/ artifacts from SHARE_DIR (|| true).
#      - Removes the helper binary from SHARE_DIR/collection-helper.
#      The helper is channel-agnostic (ADR-049 §R5: one shared helper identity).
#      All helper steps are non-fatal — helper may never have been installed.
#   8. (--purge only) Deletes the API key from the macOS Keychain.

set -euo pipefail

# ---------------------------------------------------------------------------
# Parse arguments.
# ---------------------------------------------------------------------------
PURGE=0
for arg in "$@"; do
  case "${arg}" in
    --purge) PURGE=1 ;;
    *) echo "Unknown argument: ${arg}" >&2; exit 1 ;;
  esac
done

# ---------------------------------------------------------------------------
# Channel-aware identity constants (ADR-049 §2, ADR-036 I-4).
# Source common.sh when it exists and CHANNEL is set; fall back to stable
# defaults for backward-compatibility with callers that do not set CHANNEL.
# ---------------------------------------------------------------------------
_SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
_COMMON_SH="${_SCRIPT_DIR}/common.sh"

if [[ -f "${_COMMON_SH}" ]]; then
  # common.sh sets CHANNEL=stable as the default when CHANNEL is unset.
  CHANNEL="${CHANNEL:-stable}"
  # shellcheck source=services/daemon/install/macos/common.sh
  source "${_COMMON_SH}"
  # common.sh (Bob's canonical version) already exports PLIST_LABEL directly.
  # Set BINARY_NAME_LEGACY and PLIST_LABEL_LEGACY for the legacy-cleanup path.
  BINARY_NAME_LEGACY="mtga-companion-daemon"
  PLIST_LABEL_LEGACY="${PLIST_LABEL_LEGACY:-com.mtga-companion.daemon}"
else
  # Fallback: pre-common.sh stable defaults (ADR-036 original behavior).
  # This branch is only reached when common.sh has not yet been introduced
  # (i.e., before ticket #650 lands in CI).  Production environments always
  # have common.sh present once the channel cluster is deployed.
  INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
  BINARY_NAME="vaultmtg-daemon"
  BINARY_NAME_LEGACY="mtga-companion-daemon"
  APP_BUNDLE_PATH="/Applications/VaultMTG.app"
  PLIST_LABEL="com.vaultmtg.daemon"
  PLIST_LABEL_LEGACY="com.mtga-companion.daemon"
  KEYCHAIN_SERVICE="com.vaultmtg.daemon"
  KEYCHAIN_ACCOUNT="api-key"
fi

PLIST_PATH="${HOME}/Library/LaunchAgents/${PLIST_LABEL}.plist"
PLIST_PATH_LEGACY="${HOME}/Library/LaunchAgents/${PLIST_LABEL_LEGACY}.plist"
# Future hollowmark label — from common.sh when sourced, else fallback.
PLIST_LABEL_HOLLOWMARK="${PLIST_LABEL_HOLLOWMARK:-com.hollowmark.daemon}"
PLIST_PATH_HOLLOWMARK="${HOME}/Library/LaunchAgents/${PLIST_LABEL_HOLLOWMARK}.plist"

# ---------------------------------------------------------------------------
# Unload the new launchd job (com.vaultmtg.daemon).
# -w removes the Disabled key from the launch database so the job does not
# reload on next login even if the plist is present.
# We use `|| true` because `launchctl unload` exits non-zero when the job
# was never loaded (e.g. running uninstall twice).
# ---------------------------------------------------------------------------
if [[ -f "${PLIST_PATH}" ]]; then
  echo "Unloading launchd job ${PLIST_LABEL}..."
  launchctl unload -w "${PLIST_PATH}" 2>/dev/null || true
  echo "Removing plist: ${PLIST_PATH}"
  rm -f "${PLIST_PATH}"
else
  echo "Plist not found (${PLIST_PATH}), skipping launchd unload."
fi

# ---------------------------------------------------------------------------
# CRITICAL (ADR-022 Constraint 1): Unload the legacy launchd job if present.
# This handles the case where a user had the old daemon installed and never
# ran the new installer — the legacy label may still be registered.
# Failures are non-fatal (|| true) — a fresh install has no legacy label.
# ---------------------------------------------------------------------------
if [[ -f "${PLIST_PATH_LEGACY}" ]]; then
  echo "Found legacy plist (${PLIST_PATH_LEGACY}) — unloading and removing..."
  launchctl unload -w "${PLIST_PATH_LEGACY}" 2>/dev/null || true
  rm -f "${PLIST_PATH_LEGACY}"
  echo "Legacy launchd job removed."
elif launchctl list "${PLIST_LABEL_LEGACY}" >/dev/null 2>&1; then
  # Label is loaded but plist is gone — use label-based bootout.
  echo "Found legacy launchd label ${PLIST_LABEL_LEGACY} (no plist) — booting out..."
  launchctl bootout "gui/$(id -u)/${PLIST_LABEL_LEGACY}" 2>/dev/null || true
else
  echo "Legacy launchd label (${PLIST_LABEL_LEGACY}) not found, skipping."
fi

# ---------------------------------------------------------------------------
# ADR-022 C1 cutover-safety (#999): Unload and remove the future hollowmark
# label if present.
#
# This handles two scenarios:
#   a) A user installs v0.4.0+ (which runs under com.hollowmark.daemon), then
#      runs v0.3.9 uninstall — the hollowmark job must not be left loaded.
#   b) A user has a partially-removed v0.4.0+ install (label loaded, plist
#      already gone) — fall back to label-based bootout.
#
# Symmetric to the PLIST_LABEL_LEGACY (com.mtga-companion.daemon) block above.
# All failures are non-fatal (|| true) — a fresh v0.3.9 install has no
# hollowmark label; this path is only hit on downgrade uninstall.
# ---------------------------------------------------------------------------
if [[ -f "${PLIST_PATH_HOLLOWMARK}" ]]; then
  echo "Found future hollowmark plist (${PLIST_PATH_HOLLOWMARK}) — unloading and removing..."
  launchctl unload -w "${PLIST_PATH_HOLLOWMARK}" 2>/dev/null || true
  rm -f "${PLIST_PATH_HOLLOWMARK}"
  echo "Future hollowmark launchd job (com.hollowmark.daemon) removed."
elif launchctl list "${PLIST_LABEL_HOLLOWMARK}" >/dev/null 2>&1; then
  # Label is loaded but plist is gone — use label-based bootout.
  echo "Found future launchd label ${PLIST_LABEL_HOLLOWMARK} (no plist) — booting out..."
  launchctl bootout "gui/$(id -u)/${PLIST_LABEL_HOLLOWMARK}" 2>/dev/null || true
else
  echo "Future hollowmark launchd label (${PLIST_LABEL_HOLLOWMARK}) not found, skipping."
fi

# ---------------------------------------------------------------------------
# Remove the binary.
# sudo is needed because /usr/local/bin is owned by root on stock macOS.
# ---------------------------------------------------------------------------
BINARY_PATH="${INSTALL_DIR}/${BINARY_NAME}"
if [[ -f "${BINARY_PATH}" ]]; then
  echo "Removing binary: ${BINARY_PATH} (may prompt for sudo)..."
  sudo rm -f "${BINARY_PATH}"
else
  echo "Binary not found (${BINARY_PATH}), skipping."
fi

# ---------------------------------------------------------------------------
# Remove the legacy binary (upgrader path — vault-mtg-tickets#48).
# Mirrors the pattern above. The guard ensures sudo is only invoked when the
# file is actually present — a fresh install (no legacy binary) skips cleanly.
# ---------------------------------------------------------------------------
LEGACY_BINARY_PATH="${INSTALL_DIR}/${BINARY_NAME_LEGACY}"
if [[ -f "${LEGACY_BINARY_PATH}" ]]; then
  echo "Found legacy binary: ${LEGACY_BINARY_PATH} — removing..."
  sudo rm -f "${LEGACY_BINARY_PATH}"
  echo "Legacy binary removed."
else
  echo "Legacy binary not found (${LEGACY_BINARY_PATH}), skipping."
fi

# ---------------------------------------------------------------------------
# Remove the VaultMTG.app launcher bundle (ADR-036 I-9, ticket #278).
# The bundle is placed in /Applications by the .pkg installer; sudo is required
# because /Applications is owned by root on stock macOS.
# ---------------------------------------------------------------------------
if [[ -d "${APP_BUNDLE_PATH}" ]]; then
  echo "Removing launcher bundle: ${APP_BUNDLE_PATH} (may prompt for sudo)..."
  sudo rm -rf "${APP_BUNDLE_PATH}"
  echo "Launcher bundle removed."
else
  echo "Launcher bundle not found (${APP_BUNDLE_PATH}), skipping."
fi

# ---------------------------------------------------------------------------
# 7. Remove the collection-agent-helper (hollowmark-tickets#1286, R3; ADR-059).
#
# Under ADR-059 the helper runs as the logged-in user — NOT as a root system
# LaunchDaemon.  New installs (post-ADR-059) will have no LaunchDaemon plist,
# no /Library/LaunchDaemons entry, and no install/ subdirectory under SHARE_DIR.
#
# For users upgrading from pre-ADR-059 installs the legacy root daemon may still
# be present.  All LaunchDaemon cleanup below is best-effort (|| true) so it is
# silent-safe on fresh ADR-059 installs where the artifacts never existed.
#
# Every step is non-fatal because:
#   - The user may have installed via a pre-ADR-059 .pkg (has LaunchDaemon).
#   - The user may have installed via a post-ADR-059 .pkg (no LaunchDaemon).
#   - The user may never have clicked "Grant Access" (helper binary absent).
#
# HELPER_LAUNCHDAEMONS_DIR defaults to /Library/LaunchDaemons; override in tests.
# SHARE_DIR is already set above from common.sh or the fallback.
# ---------------------------------------------------------------------------
HELPER_LABEL="com.vaultmtg.collection-helper"
HELPER_LAUNCHDAEMONS_DIR="${HELPER_LAUNCHDAEMONS_DIR:-/Library/LaunchDaemons}"
HELPER_PLIST="${HELPER_LAUNCHDAEMONS_DIR}/${HELPER_LABEL}.plist"
# SHARE_DIR is already set: /usr/local/share/vaultmtg (from common.sh or postinstall constant)
SHARE_DIR="${SHARE_DIR:-/usr/local/share/vaultmtg}"

# --- Legacy best-effort: remove root LaunchDaemon if it exists (pre-ADR-059 installs). ---
# Post-ADR-059 installs have no LaunchDaemon; these lines are silently skipped.
echo "Checking for legacy collection-agent-helper LaunchDaemon (${HELPER_LABEL})..."
launchctl bootout "system/${HELPER_LABEL}" 2>/dev/null || true
if [[ -f "${HELPER_PLIST}" ]]; then
  echo "Removing legacy helper plist: ${HELPER_PLIST}"
  rm -f "${HELPER_PLIST}" 2>/dev/null || true
  echo "Legacy helper plist removed."
else
  echo "Legacy helper plist not found (${HELPER_PLIST}) — skipping (expected on post-ADR-059 installs)."
fi

# --- Remove install/ artifacts if present (pre-ADR-059 installs only). ---
# post-ADR-059 .pkg does not stage install-helper.sh or the plist file.
if [[ -f "${SHARE_DIR}/install/install-helper.sh" ]]; then
  rm -f "${SHARE_DIR}/install/install-helper.sh" 2>/dev/null || true
  echo "Removed legacy install-helper.sh from SHARE_DIR."
fi
if [[ -f "${SHARE_DIR}/install/com.vaultmtg.collection-helper.plist" ]]; then
  rm -f "${SHARE_DIR}/install/com.vaultmtg.collection-helper.plist" 2>/dev/null || true
  echo "Removed legacy helper plist from SHARE_DIR."
fi

# --- Remove the helper binary from SHARE_DIR (all installs). ---
if [[ -f "${SHARE_DIR}/collection-helper" ]]; then
  rm -f "${SHARE_DIR}/collection-helper" 2>/dev/null || true
  echo "Removed helper binary from ${SHARE_DIR}/collection-helper."
else
  echo "Helper binary not found at ${SHARE_DIR}/collection-helper — skipping."
fi

# ---------------------------------------------------------------------------
# 8. Keychain entry — service and account from common.sh (if sourced) or defaults.
# Default behaviour: RETAIN the entry for downgrade safety — a user who
# reinstalls the daemon will not need to re-authenticate.
# --purge: delete the entry via security(1) so no credential remains on disk.
# Failure (entry already absent) is non-fatal — security exits 44 in that case.
# ---------------------------------------------------------------------------
# KEYCHAIN_SERVICE and KEYCHAIN_ACCOUNT are set by common.sh above.
# The default values below are only reached in the pre-common.sh fallback path.

if [[ "${PURGE}" -eq 1 ]]; then
  echo "Removing keychain entry (${KEYCHAIN_SERVICE} / ${KEYCHAIN_ACCOUNT})..."
  security delete-generic-password \
    -s "${KEYCHAIN_SERVICE}" \
    -a "${KEYCHAIN_ACCOUNT}" 2>/dev/null || true
  echo "Keychain entry removed (or was already absent)."
fi

echo ""
echo "VaultMTG daemon uninstalled."
echo "Log file (${LOG_FILE:-${HOME}/Library/Logs/vaultmtg-daemon.log}) was NOT removed."
echo "Config file (${CONFIG_DIR:-~/.vaultmtg}/daemon.json) was NOT removed."
echo "Remove those manually if desired."
if [[ "${PURGE}" -eq 0 ]]; then
  echo "API key retained in keychain for downgrade safety. Run with --purge to remove all data."
fi
