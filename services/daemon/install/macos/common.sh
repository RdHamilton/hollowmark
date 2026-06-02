#!/usr/bin/env bash
# common.sh — single source of truth for VaultMTG daemon macOS install identity
#
# ADR-036 I-4: all install scripts source this file; no install constant appears
#              outside common.sh / common.ps1.
# ADR-049 §2: CHANNEL is injected exactly once; every constant is derived as
#             base + suffix.  No per-channel literal anywhere outside this file.
#
# CHANNEL injection:
#   • .pkg postinstall:  build-pkg.sh substitutes __VAULTMTG_CHANNEL__ before
#                        embedding this file, resolved from the daemon-release.yml
#                        suffix test (the same if/else that picks cloud_api_url).
#   • dev/direct install: export CHANNEL=staging before sourcing, or set
#                         VAULTMTG_DAEMON_CHANNEL in the environment.
#   • Default (unset):   "stable" — a developer running install.sh directly
#                        without an explicit channel gets the prod identity.
#
# Usage:
#   source "$(dirname "$0")/common.sh"
#   echo "${BINARY_NAME}"   # → vaultmtg-daemon  (stable) or vaultmtg-daemon-staging  (staging)

# ---------------------------------------------------------------------------
# Resolve CHANNEL — default to "stable" if unset.
# Allow override via VAULTMTG_DAEMON_CHANNEL env var (installer-set) as well
# as the bare CHANNEL variable (direct export by the caller).
# ---------------------------------------------------------------------------
: "${CHANNEL:=${VAULTMTG_DAEMON_CHANNEL:-stable}}"

# ---------------------------------------------------------------------------
# A single, auditable suffix table — the ONLY place channel branches (ADR-049 §2).
# Adding a future channel (e.g. beta) is one new case arm, not N script edits.
# ---------------------------------------------------------------------------
case "$CHANNEL" in
  stable)
    SUFFIX=""
    LABEL_SUFFIX=""
    APP_SUFFIX=""
    DISPLAY=""
    ;;
  staging)
    SUFFIX="-staging"
    LABEL_SUFFIX=".staging"
    APP_SUFFIX=" Staging"
    DISPLAY=" (Staging)"
    ;;
  "")
    echo "common.sh: CHANNEL is empty — must be 'stable' or 'staging'" >&2
    exit 64
    ;;
  *)
    echo "common.sh: unknown CHANNEL '${CHANNEL}' — must be 'stable' or 'staging'" >&2
    exit 64
    ;;
esac

# ---------------------------------------------------------------------------
# Every constant is DERIVED from one base + the suffix.
# No per-channel literal appears below this line.
# ---------------------------------------------------------------------------

# Binary identity
BINARY_NAME="vaultmtg-daemon${SUFFIX}"
# Honor env override (used by tests and dry-run mode); default to canonical location.
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# LaunchAgent label
PLIST_LABEL="com.vaultmtg.daemon${LABEL_SUFFIX}"

# OS keychain service (matches keychain.go ServiceNameNew for stable channel)
KEYCHAIN_SERVICE="com.vaultmtg.daemon${LABEL_SUFFIX}"
KEYCHAIN_ACCOUNT="api-key"

# Config and state dirs / files
CONFIG_DIR="${HOME}/.vaultmtg${SUFFIX}"
LOG_FILE="${HOME}/Library/Logs/vaultmtg-daemon${SUFFIX}.log"
INSTALL_STATE="${CONFIG_DIR}/install-state.json"

# macOS launcher app bundle (ADR-036 I-4 / I-9).
# Honor env override (used by tests and installer sandbox mode).
APP_BUNDLE_PATH="${APP_BUNDLE_PATH:-/Applications/VaultMTG${APP_SUFFIX}.app}"

# User-visible tray label
TRAY_LABEL="VaultMTG${DISPLAY}"

# Local-API TCP port (loopback health/status endpoint).
# stable = 9001, staging = 9011 (stable + 10, offset matches install.stagingPortOffset in Go).
# Using offset 10 (not 1) avoids collision with any adjacent well-known port.
# Enforced by internal/install/crosscheck_test.go (ADR-049 §2 fitness function).
if [ "${CHANNEL}" = "staging" ]; then
  LOCAL_API_PORT=9011
else
  LOCAL_API_PORT=9001
fi

# ---------------------------------------------------------------------------
# Legacy handling — ONLY for the stable channel.
# The staging channel never had a legacy identity, so PLIST_LABEL_LEGACY is
# intentionally not set for staging.  Callers guard against unset with
# ${PLIST_LABEL_LEGACY:-} before referencing.
# ---------------------------------------------------------------------------
if [ "${CHANNEL}" = "stable" ]; then
  PLIST_LABEL_LEGACY="com.mtga-companion.daemon"
fi
