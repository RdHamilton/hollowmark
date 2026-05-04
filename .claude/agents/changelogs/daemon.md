# Daemon Agent Changelog

## 2026-05-04 — Issue #1094: feat(daemon): install scripts (PowerShell + launchd)
**PR**: #TBD
**Files changed**:
- `services/daemon/install/macos/install.sh` — new: detects arch, downloads binary from GitHub Releases, installs to /usr/local/bin, writes launchd plist, loads with launchctl
- `services/daemon/install/macos/uninstall.sh` — new: unloads launchd job, removes plist and binary
- `services/daemon/install/windows/install.ps1` — new: downloads Windows amd64 binary, writes daemon.yaml config, registers AtLogon Task Scheduler task (no UAC required)
- `services/daemon/install/windows/uninstall.ps1` — new: stops and removes scheduled task and binary
- `services/daemon/install/README.md` — new: one-liner install instructions for macOS and Windows
**Summary**: Added platform install scripts so users can install and autostart the daemon on macOS (via launchd) and Windows (via Task Scheduler) without admin elevation on Windows; binary is downloaded from GitHub Releases with auto-detection of the latest daemon/* tag.

## 2026-05-04 — Issue #1131: fix(daemon): JWT mid-session expiry refresh + CI and binary naming cleanup
**PR**: #1175
**Files changed**:
- `services/daemon/internal/dispatcher/dispatcher.go` — added 401 detection and JWT refresh logic for mid-session token expiry
- `services/daemon/internal/dispatcher/dispatcher_test.go` — unit tests for 401 refresh path
- `.github/workflows/release.yml` — consolidated dual CI workflow confusion; standardized binary naming
**Summary**: Fixed mid-session JWT expiry by adding 401-triggered refresh in the dispatcher, cleaned up dual CI workflow confusion, and standardized daemon binary naming across platforms.
**Merged**: 2026-05-04 — PR #1175 merged into main.

## 2026-05-03 — Issue #1014: daemon: investigate log preservation and MTGA log overwrite on startup
**PR**: #1042
**Files changed**:
- `services/daemon/internal/logreader/preservation.go` — new: `Snapshot`, `ListSnapshots`, `PruneSnapshots`, `copyFile`
- `services/daemon/internal/logreader/preservation_test.go` — new: 7 unit tests for all preservation functions
- `services/daemon/internal/logreader/poller.go` — drain bug fix: call `checkForUpdates()` before position reset on Remove/Rename fsnotify events
- `services/daemon/internal/logreader/reader_test.go` — added `TestPollerHandlesRotationDrain` integration test
- `services/daemon/internal/config/config.go` — added `LogArchiveDir`, `LogArchiveMaxAge` (7d default), `LogPreserveOnStart` (true default) with env var `MTGA_DAEMON_LOG_ARCHIVE_DIR`
- `services/daemon/internal/config/config_test.go` — 4 new config field tests
- `services/daemon/internal/daemon/service.go` — wired `Snapshot` + `PruneSnapshots` into `Run()` before poller starts
**Summary**: Fixed log preservation so the daemon snapshots Player.log on startup before MTGA can overwrite it; also fixed a bug where unread bytes in the old log were discarded when a rotation was detected via fsnotify. Filed follow-on issue #1041 for dead `models.go` structs that need alignment with actual MTGA JSON keys.
**Merged**: 2026-05-03 — PR #1042 merged into main.
