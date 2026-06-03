#!/usr/bin/env bash
# build-pkg.sh — Build the macOS .pkg installer and wrap it in a .dmg.
#
# Usage:
#   BINARY_PATH=bin/vaultmtg-daemon \
#   VERSION=0.3.1 \
#   TEAM_ID=<Apple Team ID> \
#   bash services/daemon/install/macos/pkg/build-pkg.sh
#
# Required environment variables:
#   BINARY_PATH   Path to the darwin universal binary (already codesigned).
#   VERSION       Semver string (e.g. "0.3.1") — no leading "v".
#   TEAM_ID       Apple Developer Team ID for signing (omit to skip signing).
#
# Outputs (in the current directory):
#   vaultmtg-daemon-darwin-universal.pkg
#   vaultmtg-daemon-darwin-universal.dmg
#
# This script is intended to be called from the GoReleaser hooks or the
# daemon-release CI workflow after the binary has been built and signed.

set -euo pipefail

# DRY_RUN=1 — print what would be done and exit 0.  Used by CI to assert the
# pkg-root layout and by developers to preview actions without needing pkgbuild
# or hdiutil installed.  Pass --dry-run as the first argument for the same effect.
if [[ "${1:-}" == "--dry-run" ]]; then DRY_RUN=1; fi
DRY_RUN="${DRY_RUN:-0}"

BINARY_PATH="${BINARY_PATH:?BINARY_PATH is required}"
VERSION="${VERSION:?VERSION is required}"
TEAM_ID="${TEAM_ID:-}"

# ADR-049 channel separation — the .pkg identity must match the CI sign-macos
# job and common.sh.  CHANNEL defaults to "stable" (a developer running this
# directly with no channel gets the prod identity, like common.sh).  This must
# stay in lock-step with the suffix table in common.sh / internal/install.
: "${CHANNEL:=${VAULTMTG_DAEMON_CHANNEL:-stable}}"
case "$CHANNEL" in
  stable)
    BIN_SUFFIX=""; LABEL_SUFFIX=""; APP_SUFFIX="" ;;
  staging)
    BIN_SUFFIX="-staging"; LABEL_SUFFIX=".staging"; APP_SUFFIX=" Staging" ;;
  *)
    echo "[build-pkg] ERROR: CHANNEL='${CHANNEL}' must be 'stable' or 'staging'" >&2
    exit 64 ;;
esac
# Icon base name is derived (vaultmtg + binary suffix), never a per-channel
# literal — keeps the ADR-049 §2 fitness function (I-4) happy.
ICON_NAME="vaultmtg${BIN_SUFFIX}"

BINARY_NAME="vaultmtg-daemon${BIN_SUFFIX}"
PKG_ID="com.vaultmtg.daemon${LABEL_SUFFIX}"
PKG_NAME="vaultmtg-daemon${BIN_SUFFIX}-darwin-universal.pkg"
DMG_NAME="vaultmtg-daemon${BIN_SUFFIX}-darwin-universal.dmg"

# Share directory — where the uninstall script is installed on the target system.
# Referenced here (to populate the package root) and in postinstall (echo to user).
# Single constant — never copy-paste this path.
SHARE_DIR="/usr/local/share/vaultmtg"

# ADR-036 I-4 / I-8: single source of truth for the launcher app bundle path.
# This must stay in sync with appBundlePath in launchagent_darwin.go and with
# uninstall.sh.  Never copy-paste this path across scripts.
APP_BUNDLE_PATH="/Applications/VaultMTG${APP_SUFFIX}.app"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG_ROOT="$(mktemp -d)/pkg-root"
DMG_STAGING="$(mktemp -d)/dmg-staging"
mkdir -p "${DMG_STAGING}"

echo "[build-pkg] building .pkg version ${VERSION}"
# Always emit PKG_ROOT so callers can inspect the layout without parsing mktemp output.
echo "PKG_ROOT=${PKG_ROOT}"

# ---------------------------------------------------------------------------
# Populate the package root.
# The binary lives at /usr/local/bin/<name> — pkgbuild uses the directory
# structure under pkg-root as the install tree (--install-location /).
# ---------------------------------------------------------------------------
mkdir -p "${PKG_ROOT}/usr/local/bin"
cp "${BINARY_PATH}" "${PKG_ROOT}/usr/local/bin/${BINARY_NAME}"
chmod 755 "${PKG_ROOT}/usr/local/bin/${BINARY_NAME}"

# Install the uninstall script to the share directory so users have a clean
# removal path after install. The path is echoed by postinstall so users see
# it immediately after installation completes.
mkdir -p "${PKG_ROOT}${SHARE_DIR}"
cp "${SCRIPT_DIR}/../uninstall.sh" "${PKG_ROOT}${SHARE_DIR}/uninstall.sh"
chmod 755 "${PKG_ROOT}${SHARE_DIR}/uninstall.sh"

# ---------------------------------------------------------------------------
# Build the VaultMTG.app launcher bundle (ADR-036 I-8, ticket #278).
#
# This is a thin launcher — it is NOT the tray process itself.  The bundle
# contains a single shell script (CFBundleExecutable) that:
#   1. Runs `launchctl enable gui/<uid>/com.vaultmtg.daemon`  — clears any
#      disabled flag left by a prior `launchctl bootout` (which stopLaunchAgent
#      now calls on Quit).
#   2. Runs `launchctl bootstrap gui/<uid> ~/Library/LaunchAgents/com.vaultmtg.daemon.plist`
#      — re-registers the plist and starts the daemon process.
#
# The tray icon appears from the daemon process itself once it starts; the
# launcher exits immediately after the bootstrap call.
#
# Design: Option B from ticket #278 — thin launcher in /Applications, daemon
# binary stays at /usr/local/bin/vaultmtg-daemon (no path changes needed).
# ADR-036 I-4: APP_BUNDLE_PATH constant declared above; not copy-pasted here.
# ---------------------------------------------------------------------------
APP_BUNDLE_ROOT="${PKG_ROOT}${APP_BUNDLE_PATH}"
APP_CONTENTS="${APP_BUNDLE_ROOT}/Contents"
APP_MACOS="${APP_CONTENTS}/MacOS"

mkdir -p "${APP_MACOS}"

# Write Contents/Info.plist and the launcher from the shared template files,
# substituting the ADR-049 channel placeholders.  Using the same static
# templates as the CI sign-macos job (not a divergent heredoc) keeps a single
# source of truth — the divergence between this script and the CI job was the
# original root cause of the staging-overwrites-prod install collision.
sed \
  -e "s|__VAULTMTG_LABEL_SUFFIX__|${LABEL_SUFFIX}|g" \
  -e "s|__VAULTMTG_APP_SUFFIX__|${APP_SUFFIX}|g" \
  -e "s|__VAULTMTG_ICON_NAME__|${ICON_NAME}|g" \
  "${SCRIPT_DIR}/app-Info.plist" \
  > "${APP_CONTENTS}/Info.plist"

# Launcher shell script (the CFBundleExecutable) — channel-substituted so the
# staging launcher bootstraps the staging LaunchAgent label.
# LSUIElement=true suppresses the Dock icon while the launcher runs; the daemon
# itself (launched via launchctl) controls its own tray presence.
sed \
  -e "s|__VAULTMTG_LABEL_SUFFIX__|${LABEL_SUFFIX}|g" \
  "${SCRIPT_DIR}/app-launcher" \
  > "${APP_MACOS}/vaultmtg-launcher"
chmod 755 "${APP_MACOS}/vaultmtg-launcher"

# Fail closed: no channel placeholder may survive in the bundle.
if grep -rq '__VAULTMTG_[A-Z_]*__' "${APP_CONTENTS}/Info.plist" "${APP_MACOS}/vaultmtg-launcher"; then
  echo "[build-pkg] ERROR: unsubstituted channel placeholder in .app bundle" >&2
  grep -rn '__VAULTMTG_[A-Z_]*__' "${APP_CONTENTS}/Info.plist" "${APP_MACOS}/vaultmtg-launcher" >&2
  exit 1
fi

# App icon (#307) — Contents/Resources/<icon>.icns paired with the
# CFBundleIconFile key in Info.plist above. This is what gives the .app its
# Finder/Dock icon (staging ships the distinct vaultmtg-staging.icns).  pkgbuild
# has no --icon flag; the icon ships inside the embedded .app bundle.
APP_RESOURCES="${APP_CONTENTS}/Resources"
mkdir -p "${APP_RESOURCES}"
cp "${SCRIPT_DIR}/../../icons/${ICON_NAME}.icns" "${APP_RESOURCES}/${ICON_NAME}.icns"

# DRY_RUN: pkg-root is now populated — print layout and exit without
# calling pkgbuild or hdiutil (neither is required for layout assertions).
if [[ "${DRY_RUN}" == "1" ]]; then
  echo "[build-pkg] DRY_RUN — pkg-root layout (no .pkg or .dmg produced):"
  find "${PKG_ROOT}" -type f | sort
  echo "[build-pkg] DRY_RUN complete — Result: PASS"
  exit 0
fi

# ---------------------------------------------------------------------------
# Build the .pkg using the postinstall script for LaunchAgent setup.
# ---------------------------------------------------------------------------
PKGBUILD_ARGS=(
  --root "${PKG_ROOT}"
  --scripts "${SCRIPT_DIR}"
  --identifier "${PKG_ID}"
  --version "${VERSION}"
  --install-location /
)

if [[ -n "${TEAM_ID}" ]]; then
  PKGBUILD_ARGS+=(--sign "Developer ID Installer: ${TEAM_ID}" --timestamp)
fi

pkgbuild "${PKGBUILD_ARGS[@]}" "${PKG_NAME}"

echo "[build-pkg] .pkg built: ${PKG_NAME}"

# ---------------------------------------------------------------------------
# Wrap the .pkg in a .dmg.
# The .dmg gives users a familiar "drag to install" surface and allows the
# release to be distributed as a single downloadable file.
# ---------------------------------------------------------------------------
cp "${PKG_NAME}" "${DMG_STAGING}/"

hdiutil create \
  -volname "MTGA Companion Daemon ${VERSION}" \
  -srcfolder "${DMG_STAGING}" \
  -ov \
  -format UDZO \
  "${DMG_NAME}"

echo "[build-pkg] .dmg built: ${DMG_NAME}"

# ---------------------------------------------------------------------------
# Clean up temp dirs.
# ---------------------------------------------------------------------------
rm -rf "$(dirname "${PKG_ROOT}")" "$(dirname "${DMG_STAGING}")"

echo "[build-pkg] done"
echo "  pkg : ${PKG_NAME}"
echo "  dmg : ${DMG_NAME}"
