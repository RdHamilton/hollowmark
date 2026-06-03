; installer.nsi — NSIS per-user installer for the VaultMTG daemon.
;
; Design constraints (ADR-011-C):
;   - Per-user install: binary to %LOCALAPPDATA%\VaultMTG\
;   - No UAC elevation — RequestExecutionLevel user
;   - No MSI, no WiX, no Windows Service
;   - Scheduled Task at logon using RunLevel LeastPrivilege (no UAC popup)
;   - Config file (daemon.json) written to %APPDATA%\vaultmtg\
;
; Build command (from the repo root):
;   makensis services/daemon/install/windows/nsis/installer.nsi
;
; GoReleaser calls makensis automatically via the `nfpms` / `before` hook
; in .goreleaser.yml (see goreleaser-nsis job in daemon-release.yml).
;
; The installer is self-contained — the daemon binary is embedded at compile
; time via the File directive.  VERSION and BINARY_PATH are passed in via
; /DVERSION=x.y.z and /DBINARY_PATH=path\to\binary on the makensis command
; line.

!ifndef VERSION
  !define VERSION "dev"
!endif

!ifndef BINARY_PATH
  !define BINARY_PATH "bin\vaultmtg-daemon-windows-amd64.exe"
!endif

; CLOUD_API_URL is the BFF endpoint baked in at build time via /DCLOUD_API_URL=
; on the makensis command line.  The installer uses this value to:
;   1. Detect a cross-env reinstall (stored URL != new URL) and clear the stale
;      Windows Credential Manager entry so the daemon doesn't auth against the
;      wrong environment (#194).
;   2. Write / update cloud_api_url in daemon.json during install.
; Default is empty so a developer build with no /D flag still compiles.
!ifndef CLOUD_API_URL
  !define CLOUD_API_URL ""
!endif

; CHANNEL is the build-time channel discriminator passed via /DCHANNEL= on the
; makensis command line (ADR-049 §1).  Values: "stable" or "staging".
; The goreleaser post hook passes -DCHANNEL=${VAULTMTG_CHANNEL} resolved by
; daemon-release.yml (pre-release suffix -> staging; bare tag -> stable).
; Default is "stable" so a developer build with no /D flag uses the bare identity.
!ifndef CHANNEL
  !define CHANNEL "stable"
!endif

;----------------------------------------------------------------------
; ADR-049 channel-separated INSTALL identity (Windows).
;
; The whole install identity below MUST be channel-differentiated, otherwise a
; staging installer overwrites the prod install at the same on-disk paths /
; Scheduled-Task name (this was the macOS root cause; Windows had the identical
; collision).  Every identifier is base + suffix, mirroring common.sh and
; internal/install.Identity():
;   BIN_SUFFIX    ""        / "-staging"   (binary, config dir)
;   APP_SUFFIX    ""        / " Staging"   (display / install dir / Start menu)
;   HEALTH_PORT   9001      / 9011         (local-API loopback port)
; The config dir uses the dash form ("vaultmtg" / "vaultmtg-staging") to match
; internal/install.Identity ConfigDir on Windows.  The Scheduled-Task name uses
; the display form ("VaultMTG-Daemon" / "VaultMTG Staging-Daemon").
;----------------------------------------------------------------------
!if "${CHANNEL}" == "staging"
  !define BIN_SUFFIX   "-staging"
  !define APP_SUFFIX   " Staging"
  !define HEALTH_PORT  "9011"
!else if "${CHANNEL}" == "stable"
  !define BIN_SUFFIX   ""
  !define APP_SUFFIX   ""
  !define HEALTH_PORT  "9001"
!else
  !error "CHANNEL must be 'stable' or 'staging' (got '${CHANNEL}')"
!endif

; Derived identity constants — no per-channel literal appears below this point.
!define DAEMON_EXE   "vaultmtg-daemon${BIN_SUFFIX}.exe"
!define CONFIG_DIR   "$APPDATA\vaultmtg${BIN_SUFFIX}"
!define TASK_NAME    "VaultMTG${APP_SUFFIX}-Daemon"
!define APP_NAME     "VaultMTG${APP_SUFFIX}"
!define CRED_TARGET  "vaultmtg-daemon${BIN_SUFFIX}-api-key"

;----------------------------------------------------------------------
; General attributes
;----------------------------------------------------------------------
Name              "${APP_NAME} Daemon ${VERSION}"
; makensis changes CWD to the .nsi file's directory before processing OutFile.
; Output is therefore written to install/windows/nsis/ relative to the repo root.
; GoReleaser extra_files uses glob services/daemon/install/windows/nsis/vaultmtg-daemon*-setup-*.exe.
OutFile           "vaultmtg-daemon${BIN_SUFFIX}-setup-${VERSION}.exe"

; Per-user install — no UAC prompt (RequestExecutionLevel user)
RequestExecutionLevel user

; Default install dir: %LOCALAPPDATA%\VaultMTG[ Staging]
InstallDir        "$LOCALAPPDATA\${APP_NAME}"

; Modern UI
!include MUI2.nsh
!define MUI_ABORTWARNING

; Installer / uninstaller icon (#307) — the VaultMTG app icon shown in the
; wizard title bar, the taskbar, and on the generated setup .exe in Explorer.
; Path is relative to this .nsi file (install/windows/nsis/ -> install/icons/).
; MUI_ICON drives the installer; MUI_UNICON the uninstaller — both required so
; Uninstall.exe carries the same brand icon.
!define MUI_ICON   "..\..\icons\vaultmtg.ico"
!define MUI_UNICON "..\..\icons\vaultmtg.ico"

;----------------------------------------------------------------------
; Beta unsigned-binary notice (vmt-t#394)
;
; When BETA_UNSIGNED is defined at build time (/DBETA_UNSIGNED=1 on the
; makensis command line), the WELCOME page body text is replaced with a
; notice explaining the expected SmartScreen warning and how to bypass it.
;
; GoReleaser passes /DBETA_UNSIGNED=1 in daemon-release.yml until Azure
; Trusted Signing is active (5 GitHub secrets populated, signing pipeline
; green).  Remove /DBETA_UNSIGNED=1 from the goreleaser-nsis job and delete
; this block once signing is confirmed working.
;
; Reference: vault-mtg-docs/engineering/runbooks/windows-trusted-signing.md
;----------------------------------------------------------------------
!ifdef BETA_UNSIGNED
  !define MUI_WELCOMEPAGE_TEXT "Welcome to the VaultMTG Daemon installer.$\r$\n$\r$\n\
IMPORTANT: This beta build is not yet code-signed.$\r$\n$\r$\n\
Windows SmartScreen may show 'Windows protected your PC' when you run \
this installer.  This is expected during the beta period.$\r$\n$\r$\n\
To proceed:$\r$\n\
  1. On the SmartScreen dialog, click 'More info'.$\r$\n\
  2. Click 'Run anyway'.$\r$\n$\r$\n\
The installer is safe: it is built by the VaultMTG CI pipeline (Ray \
Hamilton Engineering, LLC) and published via GitHub Releases.  Code \
signing will be active before the public launch.$\r$\n$\r$\n\
Click Next to continue."
!endif

;----------------------------------------------------------------------
; Pages
;----------------------------------------------------------------------
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

;----------------------------------------------------------------------
; Language
;----------------------------------------------------------------------
!insertmacro MUI_LANGUAGE "English"

;----------------------------------------------------------------------
; Installer section
;----------------------------------------------------------------------
Section "Install" SecInstall

  SetOutPath "$INSTDIR"

  ; Copy the binary (channel-distinct name so staging does not overwrite prod).
  File /oname=${DAEMON_EXE} "${BINARY_PATH}"

  ; Config-dir migration (ADR-022 Phase 2, upgrade path).
  ; Copy %APPDATA%\mtga-companion\daemon.json → channel config dir
  ; so existing users retain their configuration after upgrading.
  ; Copy-not-move: the legacy dir is retained for downgrade safety.
  ; The daemon binary also runs this migration at startup (idempotent).
  ; Legacy migration applies to the stable channel only — staging never had a
  ; legacy (mtga-companion) identity — but the IfFileExists guard makes it a
  ; safe no-op for staging regardless.
  CreateDirectory "${CONFIG_DIR}"
  IfFileExists "${CONFIG_DIR}\daemon.json" ConfigMigrationDone CheckLegacyConfig
  CheckLegacyConfig:
    IfFileExists "$APPDATA\mtga-companion\daemon.json" DoConfigMigration ConfigMigrationDone
    DoConfigMigration:
      CopyFiles "$APPDATA\mtga-companion\daemon.json" "${CONFIG_DIR}\daemon.json"
  ConfigMigrationDone:

  ; --- Cross-env reinstall guard (#194) ----------------------------------------
  ; If daemon.json already exists AND CLOUD_API_URL was supplied at build time,
  ; compare the stored cloud_api_url to the baked-in value.  On a mismatch (e.g.
  ; staging → prod reinstall) clear the stale Windows Credential Manager entry
  ; and update cloud_api_url in-place, preserving all other JSON fields.
  ;
  ; Implementation uses the .ps1 temp-file pattern (Ray correction #1) to avoid
  ; NSIS/PowerShell quote-nesting issues — same approach as the health-check block.
  ; $$ = NSIS escape for a literal dollar sign in the written PowerShell script.
  IfFileExists "${CONFIG_DIR}\daemon.json" CheckEnvMismatch SkipEnvMismatch
  CheckEnvMismatch:
    FileOpen  $2 "$TEMP\vaultmtg-env-check.ps1" w
    FileWrite $2 '$$configFile = "${CONFIG_DIR}\daemon.json"$\n'
    FileWrite $2 '$$newUrl     = "${CLOUD_API_URL}"$\n'
    FileWrite $2 'if ([string]::IsNullOrEmpty($$newUrl)) { exit 0 }$\n'
    FileWrite $2 'try {$\n'
    FileWrite $2 '    $$data = Get-Content $$configFile -Raw | ConvertFrom-Json$\n'
    FileWrite $2 '    $$oldUrl = $$data.cloud_api_url$\n'
    FileWrite $2 '    if ($$oldUrl -and $$oldUrl -ne $$newUrl) {$\n'
    FileWrite $2 '        Write-Host "cross-env reinstall: old=$${oldUrl} new=$${newUrl}"$\n'
    FileWrite $2 '        cmdkey /delete:${CRED_TARGET} 2>$$null$\n'
    FileWrite $2 '        $$data.cloud_api_url = $$newUrl$\n'
    FileWrite $2 '        $$data | ConvertTo-Json -Depth 10 | Set-Content $$configFile -Encoding UTF8$\n'
    FileWrite $2 '        Write-Host "cloud_api_url updated and stale credential cleared"$\n'
    FileWrite $2 '    } else {$\n'
    FileWrite $2 '        Write-Host "cloud_api_url unchanged — preserving existing config"$\n'
    FileWrite $2 '    }$\n'
    FileWrite $2 '} catch {$\n'
    FileWrite $2 '    Write-Host "env-check: could not read daemon.json ($${_}) — skipping"$\n'
    FileWrite $2 '}$\n'
    FileClose $2
    ExecWait 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$TEMP\vaultmtg-env-check.ps1"'
    Delete "$TEMP\vaultmtg-env-check.ps1"
  SkipEnvMismatch:

  ; Write a minimal daemon.json if one does not already exist.
  ; When CLOUD_API_URL is supplied at build time it is written into the skeleton
  ; so the daemon can reach the correct BFF from first launch.
  ; The daemon's first-run flow (issue #1643) will populate api_key on first
  ; launch; we write a skeleton so the file exists and is valid JSON from day one.
  IfFileExists "${CONFIG_DIR}\daemon.json" SkipWriteConfig WriteConfig
  WriteConfig:
    FileOpen  $0 "${CONFIG_DIR}\daemon.json" w
    FileWrite $0 '{$\n  "cloud_api_url": "${CLOUD_API_URL}",$\n  "api_key": ""$\n}$\n'
    FileClose $0
  SkipWriteConfig:

  ; Write uninstaller.
  WriteUninstaller "$INSTDIR\Uninstall.exe"

  ; Remove legacy MTGA-Companion-Daemon scheduled task before registering the
  ; new VaultMTG-Daemon task. This is CRITICAL on upgrade — without it, two
  ; daemon processes run simultaneously after the first logon.
  ; /F silences "task not found" so this is a no-op on fresh installs.
  ExecWait 'schtasks /End /TN "MTGA-Companion-Daemon"'
  ExecWait 'schtasks /Delete /TN "MTGA-Companion-Daemon" /F'

  ; Register Scheduled Task at logon — no UAC (RunLevel LeastPrivilege).
  ; We use schtasks.exe because it is available on all Windows versions
  ; without requiring PowerShell or admin rights for per-user tasks.
  ; /RL LIMITEDACCESS maps to TaskPrincipalRunLevel LeastPrivilege — the task
  ; runs with the user's standard token, no elevation prompt, no UAC.
  ExecWait 'schtasks /Delete /TN "${TASK_NAME}" /F'
  ExecWait 'schtasks /Create /TN "${TASK_NAME}" /TR "\"$INSTDIR\${DAEMON_EXE}\" -config \"${CONFIG_DIR}\daemon.json\"" /SC ONLOGON /RL LIMITED /F'

  ; Start the daemon immediately without requiring a logoff/logon.
  ExecWait 'schtasks /Run /TN "${TASK_NAME}"'

  ; Create a Start-menu shortcut so the user can relaunch the daemon after
  ; exiting the tray without opening a terminal (AC5 / ticket #278).
  ; The shortcut launches the daemon binary directly; the Scheduled Task handles
  ; auto-start at logon — the shortcut is the manual-relaunch affordance.
  CreateDirectory "$SMPROGRAMS\${APP_NAME}"
  CreateShortCut "$SMPROGRAMS\${APP_NAME}\${APP_NAME} Daemon.lnk" "$INSTDIR\${DAEMON_EXE}"

  ; Post-install health check (issue #2131).
  ; Poll GET http://127.0.0.1:9001/health for up to 15 s (5 attempts x 3 s delay).
  ; A healthy response has HTTP 200 with a non-empty "account_id" field, confirming
  ; the daemon started and authenticated.  Exit code 1 from the PowerShell script
  ; causes the installer to report a failure so the user sees an error dialog rather
  ; than a false "Installation complete" screen.
  ;
  ; The health-check logic is written to a temporary .ps1 file rather than passed
  ; inline via -Command, because NSIS single-quoted strings terminate at the next
  ; literal single-quote character — so any PowerShell string literal containing
  ; a single-quote (e.g. Write-Error 'msg') would split the NSIS token and cause
  ; "ExecWait expects 1-2 parameters, got N" at compile time (issue #147 / PR #2131
  ; regression fix).  Writing to a file sidesteps NSIS/PowerShell quote-nesting
  ; entirely and keeps the script readable.
  ; Note: $$ is the NSIS escape for a literal dollar sign — necessary so NSIS does
  ; not attempt to interpolate the PowerShell variable names written into the .ps1.
  FileOpen  $1 "$TEMP\vaultmtg-health-check.ps1" w
  ; SKIP_HEALTH_CHECK=1 lets CI steps that use a stub binary (which never
  ; answers /health) bypass the health check without hanging.  The env var is
  ; only set in CI workflow steps — production installs never set it.
  FileWrite $1 'if ($$env:SKIP_HEALTH_CHECK -eq "1") { Write-Host "SKIP: health check bypassed (SKIP_HEALTH_CHECK=1)"; exit 0 }$\n'
  FileWrite $1 '$$maxAttempts = 5$\n'
  FileWrite $1 '$$delay = 3$\n'
  FileWrite $1 '$$healthy = $$false$\n'
  FileWrite $1 'for ($$i = 1; $$i -le $$maxAttempts; $$i++) {$\n'
  FileWrite $1 '    try {$\n'
  FileWrite $1 '        $$r = Invoke-WebRequest -Uri http://127.0.0.1:${HEALTH_PORT}/health -UseBasicParsing -TimeoutSec 2 -ErrorAction Stop$\n'
  FileWrite $1 '        if ($$r.StatusCode -eq 200) {$\n'
  FileWrite $1 '            $$j = $$r.Content | ConvertFrom-Json$\n'
  FileWrite $1 '            if ($$j.account_id) { $$healthy = $$true; break }$\n'
  FileWrite $1 '        }$\n'
  FileWrite $1 '    } catch {}$\n'
  FileWrite $1 '    if ($$i -lt $$maxAttempts) { Start-Sleep -Seconds $$delay }$\n'
  FileWrite $1 '}$\n'
  FileWrite $1 'if (-not $$healthy) {$\n'
  FileWrite $1 '    Write-Error "${APP_NAME} daemon did not start or authenticate within 15s. Check ${CONFIG_DIR}\ for logs."$\n'
  FileWrite $1 '    exit 1$\n'
  FileWrite $1 '}$\n'
  FileClose $1
  ExecWait 'powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "$TEMP\vaultmtg-health-check.ps1"' $0
  Delete "$TEMP\vaultmtg-health-check.ps1"
  IntCmp $0 0 HealthOK HealthFail HealthFail
  HealthFail:
    ; IfSilent skips the modal dialog when the installer runs with /S (CI silent
    ; mode).  A modal MessageBox blocks indefinitely in non-interactive runners
    ; even when /S is active — use Abort-only in silent mode to avoid hangs.
    IfSilent +2
    MessageBox MB_OK|MB_ICONSTOP "${APP_NAME} daemon did not start correctly.$\n$\nThe daemon may have failed to start or has not yet authenticated.$\nCheck ${CONFIG_DIR}\ for log files and try reinstalling."
    Abort "Daemon health check failed — installation incomplete."
  HealthOK:

SectionEnd

;----------------------------------------------------------------------
; Uninstaller section
;----------------------------------------------------------------------
Section "Uninstall"

  ; Stop and remove THIS channel's scheduled task only.  A staging uninstall
  ; must never touch the prod task (or vice versa) — TASK_NAME is channel-keyed.
  ExecWait 'schtasks /End /TN "${TASK_NAME}"'
  ExecWait 'schtasks /Delete /TN "${TASK_NAME}" /F'

!if "${CHANNEL}" == "stable"
  ; Stable channel only: also remove the legacy MTGA-Companion-Daemon task if
  ; still present.  The staging channel never had a legacy identity, so this
  ; block is compiled out of the staging uninstaller.
  ExecWait 'schtasks /End /TN "MTGA-Companion-Daemon"'
  ExecWait 'schtasks /Delete /TN "MTGA-Companion-Daemon" /F'
!endif

  ; Remove Start-menu shortcut created during install (ticket #278).
  Delete "$SMPROGRAMS\${APP_NAME}\${APP_NAME} Daemon.lnk"
  RMDir  "$SMPROGRAMS\${APP_NAME}"

  ; Remove binary and uninstaller.
  Delete "$INSTDIR\${DAEMON_EXE}"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir  "$INSTDIR"

  ; Leave the channel config dir's daemon.json intact — the user may want to
  ; keep their config for a re-install.  A future "full uninstall" option can
  ; add a checkbox to remove config too.

SectionEnd
