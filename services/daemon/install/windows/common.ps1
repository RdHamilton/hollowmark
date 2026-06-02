# common.ps1 — single source of truth for VaultMTG daemon Windows install identity
#
# ADR-036 I-4: all install scripts dot-source this file; no install constant appears
#              outside common.sh / common.ps1.
# ADR-049 §2: $Channel is injected exactly once; every constant is derived as
#             base + suffix.  No per-channel literal appears outside this file.
#
# $Channel injection:
#   • NSIS installer:  installer.nsi substitutes __VAULTMTG_CHANNEL__ before
#                      embedding, resolved from the daemon-release.yml suffix test.
#   • dev/direct install: pass -Channel staging to the script, or set
#                         $Env:VAULTMTG_DAEMON_CHANNEL before dot-sourcing.
#   • Default (unset):   "stable" — a developer running install.ps1 directly
#                        without an explicit channel gets the prod identity.
#
# Usage:
#   . "$PSScriptRoot\common.ps1" [-Channel <stable|staging>]
#
#   After dot-sourcing, use $BinaryName, $TaskName, $ConfigDir, etc.
#
# Parameters:
[CmdletBinding()]
param(
    [string]$Channel = ''
)

# ---------------------------------------------------------------------------
# Resolve Channel — default to "stable" if not passed or empty.
# Allow override via VAULTMTG_DAEMON_CHANNEL env var (installer-set) or
# the -Channel parameter (direct call).
# ---------------------------------------------------------------------------
if (-not $Channel) {
    $Channel = if ($Env:VAULTMTG_DAEMON_CHANNEL) { $Env:VAULTMTG_DAEMON_CHANNEL } else { 'stable' }
}

# ---------------------------------------------------------------------------
# A single, auditable suffix table — the ONLY place channel branches (ADR-049 §2).
# ---------------------------------------------------------------------------
switch ($Channel) {
    'stable' {
        $Suffix       = ''
        $LabelSuffix  = ''
        $AppSuffix    = ''
        $Display      = ''
    }
    'staging' {
        $Suffix       = '-staging'
        $LabelSuffix  = '-Staging'   # Windows uses PascalCase for Task names
        $AppSuffix    = ' Staging'
        $Display      = ' (Staging)'
    }
    default {
        Write-Error "common.ps1: unknown Channel '$Channel' — must be 'stable' or 'staging'"
        exit 64
    }
}

# ---------------------------------------------------------------------------
# Every constant is DERIVED from one base + the suffix.
# No per-channel literal appears below this line.
# ---------------------------------------------------------------------------

# Binary identity
$BinaryName  = "vaultmtg-daemon${Suffix}.exe"
$AssetName   = "vaultmtg-daemon-windows-amd64${Suffix}.exe"
# Install to %ProgramFiles%\VaultMTG; fallback to %LOCALAPPDATA%\VaultMTG
$InstallDir  = Join-Path $Env:ProgramFiles "VaultMTG"

# Task Scheduler task name
$TaskName       = "VaultMTG-Daemon${LabelSuffix}"
# Legacy task name — removed only on stable channel during upgrade
$LegacyTaskName = 'MTGA-Companion-Daemon'

# Windows Credential Manager target (go-keyring format: "<service>:<account>")
# Lowercase suffix to match the macOS keychain naming convention
$CredTarget  = "com.vaultmtg.daemon${Suffix}:api-key"
$CredService = "com.vaultmtg.daemon${Suffix}"

# Config dir: %APPDATA%\vaultmtg (stable) or %APPDATA%\vaultmtg-staging (staging)
$ConfigDir   = Join-Path $Env:APPDATA "vaultmtg${Suffix}"
$ConfigFile  = Join-Path $ConfigDir 'daemon.json'

# Legacy config dir — migrated on upgrade (stable channel only)
$LegacyConfigDir = Join-Path $Env:APPDATA 'mtga-companion'

# Tray label
$TrayLabel   = "VaultMTG${Display}"
