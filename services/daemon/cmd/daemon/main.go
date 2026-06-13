// Command mtga-daemon watches MTGA Player.log and forwards events to the BFF.
// Configuration is loaded from a JSON file (default: %APPDATA%\vaultmtg\daemon.json
// on Windows; ~/.vaultmtg/daemon.json on macOS/Linux) and can be overridden with
// environment variables. The cloud API URL is never hardcoded.
//
// Environment variables (ADR-022 Phase 2 dual-read shim: VAULTMTG_DAEMON_* wins
// when both are set; MTGA_DAEMON_* is the legacy fallback for existing service installs):
//
//	VAULTMTG_DAEMON_CLOUD_API_URL        Base URL of the cloud API / BFF (required if not in config file)
//	MTGA_DAEMON_CLOUD_API_URL            Legacy alias (fallback)
//	VAULTMTG_DAEMON_API_KEY              Bearer token for BFF authentication (legacy plaintext — migrated to keychain)
//	MTGA_DAEMON_API_KEY                  Legacy alias (fallback)
//	VAULTMTG_DAEMON_LOG_PATH             Override MTGA log file path (auto-detected by default)
//	MTGA_DAEMON_LOG_PATH                 Legacy alias (fallback)
//	VAULTMTG_DAEMON_ACCOUNT_ID           MTGA account ID to tag events
//	MTGA_DAEMON_ACCOUNT_ID               Legacy alias (fallback)
//	VAULTMTG_DAEMON_HEADLESS             Set to "1" to skip browser open and print the auth URL instead
//	MTGA_DAEMON_HEADLESS                 Legacy alias (fallback)
//	VAULTMTG_DAEMON_MAX_AUTH_ATTEMPTS    Max consecutive failed PKCE attempts before auth_paused (#2133)
//	MTGA_DAEMON_MAX_AUTH_ATTEMPTS        Legacy alias (fallback)
//	MTGA_COLLECTION_HELPER_DIR           Directory containing collection-helper binary and install/ subdir (dev override)
//	CLERK_PUBLISHABLE_KEY                Clerk publishable key (pk_live_* / pk_test_*) used for PKCE OAuth
//	CLERK_FRONTEND_API                   Clerk frontend API base URL (e.g. https://accounts.clerk.dev)
//	VAULTMTG_DAEMON_REPLAY_FILE         Path to a Player.log fixture; when set the daemon runs one-shot replay and exits (#640)
//
// Flags:
//
//	-config <path>           Path to JSON config file
//	-replay <path>           Path to a Player.log fixture for one-shot corpus replay (#640, ADR-042 Amendment 1)
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/config"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/credstore"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/daemon"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/daemonstate"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/dispatch"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/install"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/keychain"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/migrate"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/pkce"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/recovery"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/sentryhook"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/tray"

	"github.com/getsentry/sentry-go"
)

// Version is the build-time version string injected via -ldflags -X main.Version=<ver>.
// Defaults to "dev" for local builds.
var Version = "dev"

// DefaultCloudAPIURL is the build-time default for cloud_api_url, injected via
// -ldflags -X main.DefaultCloudAPIURL=<url>. The release workflow picks the value:
//
//	stable tags (daemon/v0.3.1) -> https://api.vaultmtg.app/api/v1
//	pre-release tags (-rc/-alpha/-beta/-pre) -> https://staging-api.vaultmtg.app/api/v1
//
// Local builds (`go run`, `go build` without -ldflags) get the localhost default
// so a developer running the daemon directly from source talks to a local BFF,
// not production. Issue #2560.
var DefaultCloudAPIURL = "http://localhost:8080/api/v1"

// DefaultSentryDSN is the build-time Sentry DSN, injected via
// -ldflags -X main.DefaultSentryDSN=<dsn>. The release workflow picks the value
// from secrets.SENTRY_DSN_DAEMON_PRODUCTION / SENTRY_DSN_DAEMON_STAGING based
// on the tag (mirrors DefaultCloudAPIURL). Empty value disables Sentry — all
// SDK calls become safe no-ops (used by `go run`, local `go build`, and any
// snapshot build). The DSN itself is never logged. Issue #1832.
var DefaultSentryDSN = ""

// DefaultSPAURL is the build-time default URL for the Hollowmark SPA, injected
// via -ldflags -X main.DefaultSPAURL=<url>. The release workflow picks the value:
//
//	stable tags (daemon/v0.3.1)           -> https://app.hollowmark.app
//	pre-release tags (-rc/-alpha/-beta/-pre) -> https://stg-app.vaultmtg.app
//
// Used by tray.New(...) so the tray "Open Hollowmark" menu item opens the
// correct SPA environment. Local builds default to the production URL — release
// workflow always overrides via -ldflags. Issue #637.
var DefaultSPAURL = "https://app.hollowmark.app"

// DefaultSetupURL is the build-time default URL for the first-run setup page,
// injected via -ldflags -X main.DefaultSetupURL=<url>. The release workflow
// picks the value:
//
//	stable tags (daemon/v0.3.1)           -> https://hollowmark.app/setup
//	pre-release tags (-rc/-alpha/-beta/-pre) -> https://stg.vaultmtg.app/setup
//
// Used by handleMissingConfig and the retry-setup loop. Issue #637.
var DefaultSetupURL = "https://hollowmark.app/setup"

// headlessKeychainFatalLog is the canonical FATAL log line emitted when the
// daemon exits in headless mode because the keychain is unavailable after all
// retries (#2136 AC6, REV-2). Extracted as a named constant so the launchd
// runbook grep pattern, any test fixtures that match this string, and the log
// call itself all share the same definition — a rename here is a single-place
// change that propagates to every consumer.
//
// Do NOT change this string without updating:
//   - engineering/runbooks/ (launchd log monitoring)
//   - any .sh or E2E fixtures that grep for this pattern
const headlessKeychainFatalLog = "[daemon] FATAL: keychain unavailable after retries — exiting"

// logAndExitHeadlessKeychain logs the canonical FATAL line via l and then
// calls exitFn(1) so the supervisor (launchd / systemd) will respawn the
// daemon. exitFn defaults to os.Exit in production; tests supply a no-op so
// the function is drivable without killing the test process. Extracting the
// log call lets TestHeadlessExitFatalLogLine assert that the *real* code path
// emits headlessKeychainFatalLog to whichever logger is active.
func logAndExitHeadlessKeychain(l *log.Logger, exitFn func(int)) {
	l.Println(headlessKeychainFatalLog)
	exitFn(1)
}

// stopLaunchAgentFn is the injectable wrapper around stopLaunchAgent (ADR-083
// SH-1). All production call sites call stopLaunchAgentFn() instead of
// stopLaunchAgent() directly — this allows tests to replace it with a spy to
// verify that only the tray-Quit path triggers launchctl bootout. The
// production value is assigned once at package init and never changed at
// runtime.
var stopLaunchAgentFn = stopLaunchAgent

// shutdownLogger is the logger used by handleSignalShutdown and
// trayQuitShutdown to emit reason-tagged shutdown lines (ADR-083 SH-5).
// Tests replace it to capture output without forking a subprocess.
var shutdownLogger = log.Default()

// handleSignalShutdown is called by the signal-handler goroutine on
// SIGTERM/SIGINT. It:
//  1. Calls logShutdown(ReasonSIGTERM) — structured log line + Sentry breadcrumb (SH-5).
//  2. Cancels the daemon context so the graceful drain fires (SH-2).
//  3. Does NOT call stopLaunchAgentFn — launchd KeepAlive=true must respawn
//     the daemon after a signal-induced stop (SH-1 / SH-2).
func handleSignalShutdown(cancel context.CancelFunc) {
	logShutdown(shutdownLogger, ReasonSIGTERM, nil)
	cancel()
}

// trayQuitShutdown is called by the tray onQuit callback — the ONLY sanctioned
// bootout site (ADR-083 SH-1). It:
//  1. Calls logShutdown(ReasonTrayQuit) — structured log line + Sentry breadcrumb (SH-5).
//  2. Calls stopLaunchAgentFn() — the one call site that runs launchctl bootout,
//     fully unregistering the daemon so KeepAlive=true does NOT respawn it.
//  3. Cancels the daemon context so the graceful drain fires.
func trayQuitShutdown(cancel context.CancelFunc) {
	logShutdown(shutdownLogger, ReasonTrayQuit, nil)
	stopLaunchAgentFn()
	cancel()
}

// headlessAuthCapExit is called when headless mode exhausts the PKCE auth
// attempt cap. It calls logShutdown(ReasonAuthCap) then exits WITHOUT calling
// stopLaunchAgentFn — the respawned process sees auth_paused=true and idles
// rather than looping into another bootout (ADR-083 SH-4).
// exitFn defaults to os.Exit in production; tests supply a no-op.
func headlessAuthCapExit(exitFn func(int)) {
	logShutdown(shutdownLogger, ReasonAuthCap, nil)
	exitFn(1)
}

// recoverSignalHandler is the deferred panic-recovery helper for the
// signal-handler goroutine. Unlike recovery.RecoverGoroutine (which swallows
// the panic after logging), this helper captures the panic via the Sentry
// capture function AND then re-panics — preventing silent signal loss (Sarah
// P2 #3256).
//
// Usage:
//
//	defer recoverSignalHandler(recovery.CaptureFn(sentry.CurrentHub().CaptureException))
func recoverSignalHandler(capture recovery.CaptureFunc) {
	r := recover()
	if r == nil {
		return
	}
	var err error
	switch v := r.(type) {
	case error:
		err = v
	default:
		err = fmt.Errorf("%v", v)
	}
	if capture != nil {
		capture(err)
	}
	panic(r) // re-panic so the signal is not silently lost
}

// runSvcWithQuit runs the svc-run loop and calls quitFn ONLY on genuine
// teardown exits — it is the extracted goroutine-funnel helper that fixes the
// defect Ray found on PR #3269 (ADR-083 SH-1/SH-3).
//
// The defect: the goroutine previously held an unconditional `defer app.Quit()`
// which fired on every goroutine return, including after a tray
// runWithDegrade permanent-failure return (SetRunStopped). That routed the
// SH-3 "stay alive" path straight into trayQuitShutdown → stopLaunchAgentFn()
// = launchctl bootout, the exact opposite of SH-3.
//
// Correct teardown semantics:
//   - Headless clean exit (svc.Run returns nil): call quitFn — unblocks the
//     no-CGO app.Run stub that blocks on <-a.quit (#1354 headless-hang fix).
//   - SIGTERM drain (ctx cancelled, runWithDegrade returns nil): call quitFn
//     — unblocks app.Run so main() can exit cleanly (SH-2).
//   - Tray permanent failure (runWithDegrade returns non-nil after all retries):
//     do NOT call quitFn — process stays alive in the tray "stopped" state (SH-3).
//   - Headless error: os.Exit via logAndExitHeadlessKeychain; quitFn never reached.
//
// quitFn is app.Quit in production; tests inject a spy.
// hooks is app (*tray.App) in production; tests inject runDegradeStateRecorder.
func runSvcWithQuit(
	ctx context.Context,
	svcRun func(context.Context) error,
	hooks runDegradeHooks,
	headless bool,
	quitFn func(),
) {
	if headless {
		if err := svcRun(ctx); err != nil {
			// SH-5 (ADR-083): structured log line + Sentry breadcrumb.
			// logShutdown does NOT flush; os.Exit bypasses defers so we
			// flush explicitly here.
			logShutdown(shutdownLogger, ReasonRunError, err)
			sentryhook.Flush()
			logAndExitHeadlessKeychain(log.Default(), os.Exit)
		}
		// Headless clean exit: call quitFn to unblock app.Run (no-CGO stub fix #1354).
		quitFn()
		return
	}

	// Tray path: degrade and retry (AC1–AC6, ADR-083 SH-3).
	// runWithDegrade NEVER calls app.Quit or stopLaunchAgent itself.
	// It returns nil on clean/ctx-cancel exit, non-nil on permanent failure.
	if runErr := runWithDegrade(ctx, svcRun, hooks, 3, defaultBackoff); runErr != nil {
		// Permanent failure: log the final error.
		// runWithDegrade has already called hooks.SetRunStopped().
		// DO NOT call quitFn — the process stays alive in the tray "stopped" state (SH-3).
		logShutdown(shutdownLogger, ReasonRunError, runErr)
		log.Printf("[mtga-daemon] run: all retries exhausted — daemon in stopped state: %v", runErr)
		return
	}
	// Clean exit or ctx-cancel (SIGTERM drain): call quitFn so app.Run unblocks (SH-2).
	quitFn()
}

func main() {
	// ADR-049 Ticket 2: resolve the channel-scoped identity once at startup.
	// All OS-level identifiers (keychain service, plist label, config dir) are
	// derived from this identity so stable and staging daemons never collide.
	identity := install.Identity(install.Channel)
	keychainService := identity.KeychainService

	// credStore is the platform credential backend (ADR-081):
	//   darwin  — 0600 file at identity.CredentialFile (replaces macOS Keychain
	//             which returns errSecInteractionNotAllowed under launchd, #1345).
	//   windows — Windows Credential Manager via go-keyring (unchanged).
	// All 10 credential call-sites in this file use credStore rather than
	// calling keychain.{Get,Set,Delete}ForService directly.
	credStore := credstore.New(identity.CredentialFile, keychainService)

	defaultCfgPath := defaultConfigPath(identity)
	cfgPath := flag.String("config", defaultCfgPath, "path to JSON config file")
	replayFile := flag.String("replay", "", "path to a Player.log fixture for one-shot corpus replay (ADR-042 Amendment 1, #640)")
	flag.Parse()

	// ── Replay mode (ADR-042 Amendment 1, #640) ────────────────────────────────
	// When -replay <file> (or VAULTMTG_DAEMON_REPLAY_FILE env) is set, the daemon
	// runs a one-shot headless corpus replay and exits (0 on replay:completed,
	// non-zero on error). It does NOT enter the live poller loop.
	// The env var is the canonical CI invocation; the flag is for direct testing.
	effectiveReplayFile := *replayFile
	if effectiveReplayFile == "" {
		effectiveReplayFile = config.EnvWithFallback("VAULTMTG_DAEMON_REPLAY_FILE", "")
	}
	if effectiveReplayFile != "" {
		os.Exit(runReplayEntryPoint(effectiveReplayFile, *cfgPath))
	}

	// ── Step 0: config-dir migration (ADR-022 Phase 2) ─────────────────────────
	// Copy old brand directories to the new VaultMTG-namespaced paths so users
	// retain their configuration after upgrading the daemon binary.
	// This is a copy-not-move: the old directories are retained for downgrade safety.
	// The migration is idempotent and a no-op on fresh installs.
	runConfigDirMigration()

	// ── Step 1: first-run config detection ─────────────────────────────────────
	// If daemon.json is missing, write a stub with cloud_api_url (if supplied via
	// env) and open the setup URL in the browser (or print it on headless).
	// The PKCE flow is then initiated so the user authenticates before the daemon starts.
	cfgExists := fileExists(*cfgPath)
	if !cfgExists {
		handleMissingConfig(*cfgPath)
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		// Config file may be a stub with no cloud_api_url — tolerate this
		// if we are about to run PKCE and will write the real config afterward.
		// For now exit on hard errors.
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	log.Printf("[mtga-daemon] version=%s default_cloud_api_url=%s", Version, DefaultCloudAPIURL)

	// ── Step 1b: Sentry init (#1832) ───────────────────────────────────────────
	// Boot before any goroutine starts so panics in setup steps are captured.
	// When DefaultSentryDSN is empty (local build, snapshot, dev), Init returns
	// sentryhook.ErrDisabled and SDK calls become no-ops — safe to leave the
	// downstream code unconditional.
	if err := sentryhook.Init(DefaultSentryDSN, Version, cfg.CloudAPIURL); err != nil {
		if errors.Is(err, sentryhook.ErrDisabled) {
			log.Printf("[mtga-daemon] Sentry disabled (no DSN baked in — local or snapshot build)")
		} else {
			log.Printf("[mtga-daemon] warn: sentry init failed: %v", err)
		}
	} else {
		log.Printf("[mtga-daemon] Sentry initialised (release=%s)", Version)
	}
	// Flush on graceful exit so in-flight events do not drop. Mirrors the BFF
	// pattern (services/bff/cmd/main.go). 2s timeout matches sentryhook.FlushTimeout.
	defer sentryhook.Flush()
	// Main-goroutine panic safety net: captures any panic that occurs on THIS
	// goroutine (main), reports it to Sentry, flushes, and re-panics so the
	// launchd / NSSM supervisor still sees a non-zero exit and respawns.
	//
	// NOTE: This recover() covers the main goroutine ONLY. Go's panic/recover
	// semantics are strictly per-goroutine — a panic in any spawned goroutine
	// (poller, localapi, tray-loop, batch-buffer, etc.) will NOT be caught here;
	// it will crash the entire process unless that goroutine has its own deferred
	// recover. Long-lived goroutines in this binary use recovery.RecoverGoroutine
	// for that purpose.
	defer func() {
		if r := recover(); r != nil {
			sentry.CurrentHub().Recover(r)
			sentryhook.Flush()
			panic(r)
		}
	}()

	// ── Step 2: credential migration (legacy plaintext api_key → credential store) ─
	if err := migrateLegacyAPIKey(cfg, credStore); err != nil {
		log.Printf("[mtga-daemon] warn: credential migration failed: %v", err)
	}

	// ── Step 2a: keychain service-name migration (vaultmtg → hollowmark shim) ─
	// ADR-022 Phase 3: migrate existing "com.vaultmtg.daemon" credentials to
	// "com.hollowmark.daemon" so the bundle-ID flip in v0.4.0 finds the key.
	// keychain.Get() handles the copy-forward atomically; we emit telemetry when
	// it reports a migration ran. The legacy entry is retained (not deleted here).
	if migrated := migrateKeychainServiceName(cfg, Version); migrated {
		dispatchKeychainMigrated(cfg, Version, credStore)
	}

	// ── Step 2b: load daemon-state.json (#2133 — RC2 load order) ──────────────
	// Runtime state (auth_paused, auth_attempts) is read BEFORE NeedsFirstRunAuth
	// so the consent loop guard is consulted before any browser open attempt.
	// Loading after NeedsFirstRunAuth would break the guard: the browser could
	// open before auth_paused is checked, defeating the entire feature.
	statePath := daemonstate.StateFilePath(*cfgPath)
	dState, stateErr := daemonstate.Load(statePath)
	if stateErr != nil {
		// Corrupt state file is non-fatal: treat as zero state (not paused, 0 attempts).
		// Log and continue — a bad write should not permanently brick the daemon.
		log.Printf("[mtga-daemon] warn: daemon-state.json load error (%v); treating as zero state", stateErr)
		dState = daemonstate.State{}
	}

	// maxAuthAttempts is the cap for consecutive failed PKCE attempts before
	// auth_paused is set. Configurable via dual-read env knob (RC2, Ray Q2 answer):
	//   VAULTMTG_DAEMON_MAX_AUTH_ATTEMPTS (canonical) → MTGA_DAEMON_MAX_AUTH_ATTEMPTS (fallback).
	// Default: 3. Values ≤ 0 revert to default (guard against misconfiguration).
	maxAuthAttempts := 3
	if v := config.EnvWithFallback("VAULTMTG_DAEMON_MAX_AUTH_ATTEMPTS", "MTGA_DAEMON_MAX_AUTH_ATTEMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxAuthAttempts = n
		}
	}

	// ── Step 2c: startup token-liveness probe (#1326 Fix A) ──────────────────
	// In keychain mode, the stored token may be stale (revoked key, Clerk instance
	// cutover, account deletion) without the daemon knowing — NeedsFirstRunAuth only
	// checks keychain PRESENCE, not validity. A stale token passes presence but will
	// be rejected by the BFF at ingest time, causing events to be buffered under the
	// wrong identity (the root cause of the 2026-06-12 P0 incident, #198).
	//
	// Before entering the main event loop, probe the BFF /api/v1/health/daemon with
	// the stored key. On 401/403: treat as NeedsRegistration and run PKCE now.
	// On 200: proceed normally. On 5xx/network error: assume valid (BFF may be down).
	//
	// Only run when:
	//   - keychain mode is active (cfg.Keychain == true),
	//   - auth is NOT already paused (no-op if we know we need PKCE),
	//   - the daemon is not in the first-run NeedsFirstRunAuth path (that already gates on keychain presence),
	//   - cfg.CloudAPIURL is known.
	keychainGetFn := credStore.Get
	firstRunNeeded, firstRunErr := cfg.NeedsFirstRunAuth(keychainGetFn)
	if firstRunErr != nil {
		// Credential access-denied or other non-first-run error (ADR-081 R1).
		// Do NOT run PKCE — the daemon will enter idle-degraded state via
		// retryKeychain once Run() is reached.
		log.Printf("[mtga-daemon] warn: credential read error at startup: %v — skipping PKCE, entering degraded state", firstRunErr)
	}
	if cfg.Keychain && !dState.AuthPaused && !firstRunNeeded && firstRunErr == nil && cfg.CloudAPIURL != "" {
		if storedKey, kcErr := keychainGetFn(); kcErr == nil && storedKey != "" {
			probeCtx, probeCancel := context.WithTimeout(context.Background(), 10*time.Second)
			live, probeErr := daemon.ProbeTokenLiveness(probeCtx, cfg.CloudAPIURL, storedKey)
			probeCancel()
			if probeErr != nil {
				log.Printf("[mtga-daemon] startup probe: transport error (BFF unreachable?) — skipping re-auth: %v", probeErr)
			} else if !live {
				log.Printf("[mtga-daemon] startup probe: stored key rejected by BFF (401/403) — treating as NeedsRegistration, starting PKCE")
				// Force a full PKCE re-registration to obtain a fresh key and
				// resolve the correct AccountID from the live Clerk identity.
				// This is the automatic self-heal path: zero user terminal commands required.
				if err := runPKCEAuth(cfg, *cfgPath, keychainService, credStore); err != nil {
					log.Printf("[mtga-daemon] startup probe: PKCE re-auth failed: %v", err)
					// Treat as a failed first-run attempt — increment paused counter.
					dState.AuthAttempts++
					if dState.AuthAttempts >= maxAuthAttempts {
						dState.AuthPaused = true
						log.Printf("[mtga-daemon] startup probe: auth attempt cap reached (%d/%d) — setting auth_paused=true",
							dState.AuthAttempts, maxAuthAttempts)
					}
					if saveErr := daemonstate.Save(statePath, dState); saveErr != nil {
						log.Printf("[mtga-daemon] warn: could not persist daemon-state.json after probe reauth: %v", saveErr)
					}
					if config.EnvWithFallback("VAULTMTG_DAEMON_HEADLESS", "MTGA_DAEMON_HEADLESS") == "1" {
						// ADR-083 SH-4: exit WITHOUT bootout so launchd KeepAlive
						// respawns the daemon. The respawned process sees
						// auth_paused=true and idles — no respawn loop.
						headlessAuthCapExit(os.Exit)
					}
				} else {
					log.Printf("[mtga-daemon] startup probe: re-auth succeeded — fresh identity written to daemon.json")
					dState.AuthAttempts = 0
					dState.AuthPaused = false
					if saveErr := daemonstate.Save(statePath, dState); saveErr != nil {
						log.Printf("[mtga-daemon] warn: could not persist daemon-state.json after probe reauth success: %v", saveErr)
					}
				}
			} else {
				log.Printf("[mtga-daemon] startup probe: key is live — proceeding with stored identity")
			}
		}
	}

	// ── Step 3: PKCE auth flow if no valid credentials ─────────────────────────
	// RC2 (CRITICAL CORRECTNESS): auth_paused is checked BEFORE NeedsFirstRunAuth.
	// If auth_paused is true, skip the initial PKCE attempt entirely — the daemon
	// enters paused state without opening the browser. The onReady goroutine will
	// surface the paused state in the tray (StatusSetupRequired + "Retry Setup").
	//
	// On failure:
	//   - headless mode: exit immediately (launchd will respawn; PKCE re-runs on boot).
	//   - tray mode: fall through to systray so the failure can be surfaced in the
	//     menu bar. The onReady goroutine re-checks NeedsFirstRunAuth and shows
	//     StatusSetupRequired + "Retry Setup…" so the user can retry without a
	//     daemon restart (#2132).
	// Re-evaluate NeedsFirstRunAuth here: the probe block above may have changed
	// cfg (new account/device IDs). Only run PKCE on confirmed ErrNotFound
	// (genuine first-run), never on ErrAccessDenied (R1).
	firstRunNeeded, firstRunErr = cfg.NeedsFirstRunAuth(keychainGetFn)
	if !dState.AuthPaused && firstRunNeeded && firstRunErr == nil && cfg.CloudAPIURL != "" {
		log.Printf("[mtga-daemon] first-run: no API key detected — starting PKCE auth flow")
		if err := runPKCEAuth(cfg, *cfgPath, keychainService, credStore); err != nil {
			fmt.Fprintf(os.Stderr, "auth error: %v\n", err)

			// Increment attempt counter and check cap (RC3: no timer reset).
			dState.AuthAttempts++
			if dState.AuthAttempts >= maxAuthAttempts {
				dState.AuthPaused = true
				log.Printf("[mtga-daemon] auth attempt cap reached (%d/%d) — setting auth_paused=true",
					dState.AuthAttempts, maxAuthAttempts)
			}
			if saveErr := daemonstate.Save(statePath, dState); saveErr != nil {
				log.Printf("[mtga-daemon] warn: could not persist daemon-state.json: %v", saveErr)
			}

			if config.EnvWithFallback("VAULTMTG_DAEMON_HEADLESS", "MTGA_DAEMON_HEADLESS") == "1" {
				// ADR-083 SH-4: exit WITHOUT bootout so launchd KeepAlive respawns
				// the daemon. The respawned process sees auth_paused=true and idles
				// rather than entering a bootout→respawn loop.
				headlessAuthCapExit(os.Exit)
			}
			// Non-headless: fall through — the tray onReady goroutine handles the
			// retry flow via NeedsFirstRunAuth + RetrySetup channel (#2132).
			log.Printf("[mtga-daemon] first-run: PKCE failed — will surface retry option in tray")
		} else {
			// PKCE succeeded on startup: reset the counter (RC3).
			dState.AuthAttempts = 0
			dState.AuthPaused = false
			if saveErr := daemonstate.Save(statePath, dState); saveErr != nil {
				log.Printf("[mtga-daemon] warn: could not persist daemon-state.json: %v", saveErr)
			}
		}
	} else if dState.AuthPaused {
		log.Printf("[mtga-daemon] auth_paused=true — skipping PKCE on startup, awaiting user retry")
	}

	// Attach the cached account_id as Sentry user context on every boot. On
	// the first run this is a no-op (cfg.AccountID is empty until PKCE runs);
	// runPKCEAuth also calls SetUser after registration. On subsequent runs
	// this is the only call site that fires. Issue #1832.
	sentryhook.SetUser(cfg.AccountID)

	ctx, cancel := context.WithCancel(context.Background())

	svc := daemon.New(cfg)
	svc.WithVersion(Version)

	// Wire the auth-paused flag from daemon-state.json (#2133, RC2).
	// Must be called before Run() so the initial /health response reflects the
	// paused state immediately rather than waiting for the first heartbeat tick.
	svc.WithAuthPaused(dState.AuthPaused)

	// Wire the in-process PKCE re-auth callback (AC-3, #2135).
	// When the daemon receives a 401 from the BFF in keychain mode, it runs this
	// callback in a goroutine rather than surfacing ErrReauthRequired immediately.
	// The callback re-runs the full PKCE flow and stores the new API key in the OS
	// keychain so the daemon can resume dispatching without a restart.
	//
	// We capture cfgPath from the outer scope (set via -config flag) so the
	// callback can persist the refreshed account_id / daemon_id if they change.
	svc.WithReauthFunc(func(ctx context.Context) error {
		return runInProcessReauth(ctx, cfg, *cfgPath, keychainService, credStore)
	})

	log.Printf("[mtga-daemon] starting, cloud_api=%s", cfg.CloudAPIURL)

	// systray.Run must own the main OS thread (macOS Cocoa requirement).
	// onReady starts the daemon service in a goroutine; onQuit calls
	// trayQuitShutdown — the ONLY sanctioned launchctl bootout site (ADR-083 SH-1).
	app := tray.New(DefaultSPAURL, Version, pkce.OpenBrowser, func() {
		// trayQuitShutdown is the ONLY sanctioned bootout site (ADR-083 SH-1).
		// It logs reason=tray_quit (SH-5), calls stopLaunchAgentFn(), and cancels
		// the context. The Sentry breadcrumb is flushed by the deferred
		// sentryhook.Flush in main() when app.Run returns.
		trayQuitShutdown(cancel)
	})

	svc.WithTray(daemon.TrayHooks{
		SyncNow:               app.SyncNow,
		GrantAccess:           app.GrantAccess,
		TryAgain:              app.TryAgain,
		RetrySetup:            app.RetrySetup,
		InstallUpdate:         app.InstallUpdate,
		SetHelperInstalled:    app.SetHelperInstalled,
		SetLastSync:           app.SetLastSync,
		SetKeychainError:      app.SetKeychainError,
		SetSetupRequired:      app.SetSetupRequired,
		SetWaitingForArena:    app.SetWaitingForArena,
		SetSyncDegraded:       app.SetSyncDegraded, // ingest-health axis (#1234)
		NotifySyncResult:      app.NotifySyncResult,
		NotifyUpdateAvailable: app.NotifyUpdateAvailable,
	})

	// Handle OS signals: on SIGTERM/SIGINT perform a graceful drain and exit
	// WITHOUT calling launchctl bootout — launchd KeepAlive=true must respawn
	// the daemon (ADR-083 SH-1 / SH-2). Only an explicit tray-Quit (above) runs
	// bootout.
	//
	// Sarah P2 #3256: use recoverSignalHandler (capture-then-re-panic) instead
	// of RecoverGoroutine (capture-then-swallow) so a panic in this goroutine is
	// never silently lost.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer recoverSignalHandler(recovery.CaptureFn(sentry.CurrentHub().CaptureException))
		<-sigCh
		// handleSignalShutdown logs reason=sigterm (SH-5), cancels the context
		// so the graceful drain fires (SH-2), and does NOT call stopLaunchAgentFn
		// — launchd KeepAlive=true must respawn the daemon (SH-1 / SH-2).
		handleSignalShutdown(cancel)
	}()

	// headless is true when the daemon is running without a display / tray
	// (e.g. CI, server, or a user-invoked terminal session with VAULTMTG_DAEMON_HEADLESS=1).
	// Evaluated once here so the Run error handler can branch without re-reading the env.
	headless := config.EnvWithFallback("VAULTMTG_DAEMON_HEADLESS", "MTGA_DAEMON_HEADLESS") == "1"

	app.Run(func() {
		app.SetStatus(tray.StatusConnected)
		go func() {
			defer recovery.RecoverGoroutine("daemon-run", recovery.CaptureFn(sentry.CurrentHub().CaptureException))
			// NOTE: do NOT add a blanket defer app.Quit() here. app.Quit() routes
			// through trayQuitShutdown → stopLaunchAgentFn() = launchctl bootout.
			// Teardown is conditional on intent: runSvcWithQuit calls app.Quit only
			// on genuine clean-exit/drain paths, NOT on tray permanent-failure (SH-3).
			// ── Auth-failure / auth-paused retry loop (#2132, #2133) ──────────────
			// Cases handled here:
			//  (A) Step 3 PKCE failed non-headlessly → NeedsFirstRunAuth still true,
			//      auth_paused possibly just set.
			//  (B) Daemon restarted with auth_paused=true in daemon-state.json →
			//      NeedsFirstRunAuth may or may not be true; we check auth_paused
			//      directly via dState and svc.WithAuthPaused (RC2).
			//
			// RC6: we block on app.RetrySetup (the RetrySetup channel from tray.App)
			// which mirrors the existing TryAgain pattern — NOT SetReauthRequired.
			for cfg.CloudAPIURL != "" {
				loopNeeds, loopErr := cfg.NeedsFirstRunAuth(keychainGetFn)
				if loopErr != nil {
					// ErrAccessDenied (or other non-first-run error): do NOT run PKCE.
					// retryKeychain will handle this via the idle-degraded path.
					log.Printf("[mtga-daemon] credential access error in retry loop: %v — exiting PKCE loop", loopErr)
					break
				}
				if !loopNeeds && !dState.AuthPaused {
					break
				}
				_ = loopNeeds // loop body uses dState.AuthPaused separately
				app.SetSetupRequired(true)

				if headless {
					// Headless — no tray to retry from. Log and exit.
					log.Printf("[mtga-daemon] PKCE auth failed or paused (headless) — exiting so supervisor can respawn")
					// Flush Sentry before os.Exit; defer on main() does not fire from a goroutine.
					sentryhook.Flush()
					os.Exit(1)
				}

				// Wait for user to click "Retry Setup…" (RC6: RetrySetup channel,
				// same pattern as TryAgain in the keychain retry loop) or context cancel.
				select {
				case <-ctx.Done():
					return
				case <-app.RetrySetup:
				}

				log.Printf("[mtga-daemon] retry setup: user requested re-auth — resetting attempt counter and opening setup page")

				// RC3: counter resets ONLY on explicit user Retry Setup action.
				// No timer-based reset.
				dState.AuthAttempts = 0
				dState.AuthPaused = false
				if saveErr := daemonstate.Save(statePath, dState); saveErr != nil {
					log.Printf("[mtga-daemon] warn: could not persist daemon-state.json on retry: %v", saveErr)
				}
				svc.ClearAuthPaused()

				// Open the setup page in the browser.
				if err := pkce.OpenBrowser(DefaultSetupURL); err != nil {
					log.Printf("[mtga-daemon] retry setup: could not open browser: %v", err)
				}

				if err := runPKCEAuth(cfg, *cfgPath, keychainService, credStore); err != nil {
					log.Printf("[mtga-daemon] retry setup: PKCE failed: %v — incrementing counter", err)

					// Increment attempt counter and check cap again (RC3).
					dState.AuthAttempts++
					if dState.AuthAttempts >= maxAuthAttempts {
						dState.AuthPaused = true
						svc.WithAuthPaused(true)
						log.Printf("[mtga-daemon] auth attempt cap reached (%d/%d) after retry — setting auth_paused=true",
							dState.AuthAttempts, maxAuthAttempts)
					}
					if saveErr := daemonstate.Save(statePath, dState); saveErr != nil {
						log.Printf("[mtga-daemon] warn: could not persist daemon-state.json: %v", saveErr)
					}
					// Loop to surface the retry item again.
					continue
				}

				// PKCE succeeded: clear the paused state and start the daemon.
				// The loop condition will now be false.
				dState.AuthAttempts = 0
				dState.AuthPaused = false
				if saveErr := daemonstate.Save(statePath, dState); saveErr != nil {
					log.Printf("[mtga-daemon] warn: could not persist daemon-state.json on success: %v", saveErr)
				}
				svc.ClearAuthPaused()
				app.SetSetupRequired(false)
				log.Printf("[mtga-daemon] retry setup: auth complete — starting daemon service")
			}

			// ── Propagate keychain token after Retry-Setup success (#275) ─────
			// runPKCEAuth stores the new api_key in the OS keychain and updates
			// daemon.json, but does NOT update the long-lived dispatcher that
			// svc.Run() uses. Without this call, Run() would start with an empty
			// bearer token and every ingest call would return 401 until the next
			// reactive PKCE cycle completes.
			//
			// We only call this when cfg.Keychain is true — non-keychain installs
			// do not use the dispatcher token path and PropagateKeychainToken
			// would immediately fail with ErrNotFound.
			if cfg.Keychain {
				if propErr := svc.PropagateKeychainToken(); propErr != nil {
					log.Printf("[mtga-daemon] warn: PropagateKeychainToken failed: %v — Run will attempt keychain retry", propErr)
				}
			}

			// ── Normal daemon run loop (ADR-083 SH-3) ────────────────────────
			// runSvcWithQuit handles headless/tray branching and calls app.Quit
			// ONLY on genuine teardown paths (clean exit / ctx-cancel drain).
			// It does NOT call app.Quit on tray permanent-failure (SH-3: process
			// stays alive in the tray "stopped" state).
			runSvcWithQuit(ctx, svc.Run, app, headless, app.Quit)
		}()
	})
}

// handleMissingConfig is called when no daemon.json exists (first install).
// It prints (or opens) the setup URL so the user knows where to go.
// A stub config is NOT written here — the full config is written after PKCE completes.
func handleMissingConfig(cfgPath string) {
	setupURL := DefaultSetupURL
	headless := config.EnvWithFallback("VAULTMTG_DAEMON_HEADLESS", "MTGA_DAEMON_HEADLESS") == "1"

	if headless {
		fmt.Printf("[mtga-daemon] First run: open %s to complete setup.\n", setupURL)
	} else {
		fmt.Printf("[mtga-daemon] First run: opening %s in your browser...\n", setupURL)
		if err := pkce.OpenBrowser(setupURL); err != nil {
			log.Printf("[mtga-daemon] warn: could not open browser: %v", err)
			fmt.Printf("[mtga-daemon] Please open: %s\n", setupURL)
		}
	}

	// Write a minimal stub so config.Load succeeds even without env vars.
	// The PKCE flow will fill in the real values.
	//
	// Resolution order for the stub cloud_api_url:
	//   1. VAULTMTG_DAEMON_CLOUD_API_URL env var (set by the postinstall plist on
	//      packaged installs).
	//   2. MTGA_DAEMON_CLOUD_API_URL env var (legacy fallback per ADR-022 Phase 2
	//      dual-read shim).
	//   3. main.DefaultCloudAPIURL — injected via -ldflags at build time. Stable
	//      releases get production; pre-release tags get staging; raw `go build`
	//      gets http://localhost:8080/api/v1 (Issue #2560).
	cloudAPIURL := config.EnvWithFallback("VAULTMTG_DAEMON_CLOUD_API_URL", "MTGA_DAEMON_CLOUD_API_URL")
	if cloudAPIURL == "" {
		cloudAPIURL = DefaultCloudAPIURL
	}

	stub := map[string]interface{}{
		"cloud_api_url": cloudAPIURL,
	}
	data, _ := json.MarshalIndent(stub, "", "  ")
	dir := filepath.Dir(cfgPath)
	if mkdirErr := os.MkdirAll(dir, 0o700); mkdirErr != nil {
		log.Printf("[mtga-daemon] warn: could not create config dir %q: %v", dir, mkdirErr)
		return
	}
	if writeErr := os.WriteFile(cfgPath, data, 0o600); writeErr != nil {
		log.Printf("[mtga-daemon] warn: could not write stub config to %q: %v", cfgPath, writeErr)
	}
}

// migrateLegacyAPIKey detects a plaintext api_key in the config file and migrates
// it to the credential store, rewriting daemon.json with keychain:true.
// This is a one-time, transparent upgrade per ADR-020 §Migration path.
// cs is the platform credential backend (ADR-081).
func migrateLegacyAPIKey(cfg *config.Config, cs credstore.Store) error {
	if cfg.Keychain || cfg.APIKey == "" || cfg.FilePath() == "" {
		return nil // nothing to migrate
	}

	log.Printf("[mtga-daemon] migrating plaintext api_key to credential store")

	if err := cs.Set(cfg.APIKey); err != nil {
		return fmt.Errorf("write to credential store: %w", err)
	}

	cfg.APIKey = ""
	cfg.Keychain = true

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("save config after credential migration: %w", err)
	}

	log.Printf("[mtga-daemon] api_key migrated to credential store; daemon.json updated")
	return nil
}

// migrateKeychainServiceName runs the ADR-022 Phase 3 keychain service-name
// migration (vaultmtg → hollowmark) by calling keychain.Get(), which handles
// the copy-forward atomically. Returns true if a migration was performed
// (i.e. the "com.vaultmtg.daemon" entry was copied to "com.hollowmark.daemon"),
// so the caller can emit the keychain.migrated telemetry event.
//
// This is a no-op when:
//   - cfg.Keychain is false (plaintext api_key mode — no keychain entry to migrate).
//   - The "com.hollowmark.daemon" entry already exists (migration already ran).
//   - Neither entry is present (fresh install — no migration needed).
func migrateKeychainServiceName(cfg *config.Config, _ string) bool {
	if !cfg.Keychain {
		return false // plaintext mode: no keychain entry to migrate
	}
	_, migrated, err := keychain.Get()
	if err != nil && !errors.Is(err, keychain.ErrNotFound) {
		log.Printf("[mtga-daemon] warn: keychain service-name migration check failed: %v", err)
		return false
	}
	return migrated
}

// keychainMigratedPayload is the JSON body of the keychain.migrated telemetry event.
// This event is emitted exactly once per install when the credential is migrated
// from "com.vaultmtg.daemon" to "com.hollowmark.daemon" (ADR-022 Phase 3).
// AC16 reads this event's daemon_version to gate the v0.4.0 legacy-entry deletion.
type keychainMigratedPayload struct {
	FromService   string `json:"from_service"`
	ToService     string `json:"to_service"`
	DaemonVersion string `json:"daemon_version"`
	Platform      string `json:"platform"`
}

// dispatchKeychainMigrated sends the keychain.migrated telemetry event to the BFF
// via a transient no-refresher dispatcher (same pattern as daemon.keychain_error).
// This is best-effort — errors are logged and swallowed.
// The event fires exactly once per install (idempotency is the credential file).
// cs is the platform credential backend (ADR-081).
func dispatchKeychainMigrated(cfg *config.Config, daemonVersion string, cs credstore.Store) {
	if cfg.CloudAPIURL == "" || cfg.AccountID == "" {
		// Pre-auth: no dispatcher available yet. The telemetry event will not fire,
		// which is acceptable — the migration still ran; only the metric is missed.
		log.Printf("[mtga-daemon] info: keychain.migrated event skipped (pre-auth, no cloud_api_url or account_id yet)")
		return
	}
	p := keychainMigratedPayload{
		FromService:   keychain.ServiceNameLegacy,
		ToService:     keychain.ServiceNameNew,
		DaemonVersion: daemonVersion,
		Platform:      runtime.GOOS,
	}
	evt, err := dispatch.BuildEvent("keychain.migrated", cfg.AccountID, "", p)
	if err != nil {
		log.Printf("[mtga-daemon] warn: build keychain.migrated event: %v", err)
		return
	}
	// Transient dispatcher without a refresher, matching daemon.keychain_error pattern.
	apiKey := ""
	if cfg.Keychain {
		if k, kErr := cs.Get(); kErr == nil {
			apiKey = k
		}
	} else {
		apiKey = cfg.APIKey
	}
	// Guard: if the API key resolved to empty (keychain entry absent or errored),
	// skip the dispatch — an empty-key ingest call produces a spurious BFF 401
	// with no benefit. The migration itself is unaffected. (#1017)
	if cfg.Keychain && apiKey == "" {
		log.Printf("[mtga-daemon] info: keychain.migrated telemetry skipped (keychain entry empty after migration)")
		return
	}
	d := dispatch.New(cfg.CloudAPIURL, "/ingest/events", apiKey)
	dispatchCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.SendOrBuffer(dispatchCtx, evt); err != nil {
		log.Printf("[mtga-daemon] warn: dispatch keychain.migrated event: %v", err)
		return
	}
	log.Printf("[mtga-daemon] keychain.migrated event dispatched (from=%s to=%s version=%s)",
		keychain.ServiceNameLegacy, keychain.ServiceNameNew, daemonVersion)
}

// runPKCEAuth executes the PKCE browser-redirect flow and:
//  1. Obtains a Clerk session JWT.
//  2. Calls POST /v1/daemon/register on the BFF to mint an API key.
//  3. On fresh registration (201 Created + non-empty api_key): stores the key
//     in the OS keychain and writes daemon.json with keychain:true.
//  4. On already-registered (200 OK + empty api_key): verifies the existing
//     keychain entry is still present and writes daemon.json without touching
//     the keychain.
//  5. On already-registered + keychain miss: calls DELETE /api/v1/daemons/{device_id}
//     to revoke the stale row, then re-registers with an empty device_id so the
//     BFF mints a fresh identity (ADR-034 §3, ADR-036 I-3). One attempt only;
//     if recovery fails, returns StatusSetupRequired and exits so launchd respawns.
//
// cs is the platform credential backend (ADR-081) used to read/write the API key.
func runPKCEAuth(cfg *config.Config, cfgPath string, keychainService string, cs credstore.Store) error {
	clerkFrontendAPI := os.Getenv("CLERK_FRONTEND_API")
	clientID := os.Getenv("CLERK_OAUTH_CLIENT_ID")
	if clerkFrontendAPI == "" || clientID == "" {
		return fmt.Errorf("CLERK_FRONTEND_API and CLERK_OAUTH_CLIENT_ID must be set for PKCE auth")
	}

	headless := config.EnvWithFallback("VAULTMTG_DAEMON_HEADLESS", "MTGA_DAEMON_HEADLESS") == "1"

	tokenEndpoint := strings.TrimRight(clerkFrontendAPI, "/") + "/oauth/token"

	pkceCfg := pkce.Config{
		ClerkFrontendAPI: clerkFrontendAPI,
		ClientID:         clientID,
		TokenEndpoint:    tokenEndpoint,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	log.Printf("[mtga-daemon] PKCE: opening browser for Clerk authentication")
	tok, err := pkce.Run(ctx, pkceCfg, headless)
	if err != nil {
		return fmt.Errorf("pkce flow: %w", err)
	}

	log.Printf("[mtga-daemon] PKCE: auth code received; registering with BFF")

	// Per ADR-028: the BFF is the source of truth for device_id.
	// Pass cfg.DaemonID as-is — empty on first install, cached value on
	// subsequent runs. The BFF mints a fresh UUIDv4 when it receives empty
	// and echoes the authoritative value back in the response.
	apiKey, accountID, serverDeviceID, alreadyRegistered, err := registerWithBFF(ctx, cfg.CloudAPIURL, tok.AccessToken, cfg.DaemonID, runtime.GOOS, Version)
	if err != nil {
		return fmt.Errorf("BFF registration: %w", err)
	}

	if alreadyRegistered {
		// BFF returned HTTP 200 + empty api_key: the device was already registered.
		// The API key is still in the OS keychain from the original install — do not
		// overwrite it. Just verify it is still there (OS keychain could have been
		// wiped by an OS reinstall even though daemon.json survived).
		log.Printf("[mtga-daemon] device already registered; using existing keychain key")

		existing, kcErr := cs.Get()
		if kcErr == nil && existing != "" {
			// Credential is intact. Write/refresh daemon.json with the account_id
			// and the BFF-authoritative device_id (ADR-028: daemon always persists the
			// server-echoed value, even when it matches the cached value — idempotent).
			cfg.Keychain = true
			cfg.APIKey = ""
			cfg.AccountID = accountID
			cfg.DaemonID = serverDeviceID

			if err := cfg.SaveTo(cfgPath); err != nil {
				return fmt.Errorf("write daemon.json: %w", err)
			}

			// Attach hashed account_id as Sentry user context (#1832).
			sentryhook.SetUser(accountID)
			log.Printf("[mtga-daemon] already-registered device — daemon.json refreshed, credential untouched")
			return nil
		}

		// Credential is missing (wiped after reinstall).
		// Recovery path (ADR-034 §3, ADR-036 I-3):
		//   1. Revoke the stale BFF row via DELETE /api/v1/daemons/{device_id}.
		//   2. Re-register with an empty device_id — BFF mints a fresh identity.
		// One attempt only. Failure exits with StatusSetupRequired so launchd respawns.
		log.Printf("[mtga-daemon] credential missing for registered device %s; attempting recovery", serverDeviceID)

		if delErr := revokeFromBFF(ctx, cfg.CloudAPIURL, tok.AccessToken, serverDeviceID); delErr != nil {
			log.Printf("[mtga-daemon] recovery: DELETE /api/v1/daemons/%s failed: %v; entering setup-required state", serverDeviceID, delErr)
			return fmt.Errorf("re-register recovery: revoke stale device: %w", delErr)
		}
		log.Printf("[mtga-daemon] recovery: stale device %s revoked; re-registering with empty device_id", serverDeviceID)

		// Clear the stale device_id so registerWithBFF sends "" and the BFF mints fresh.
		cfg.DaemonID = ""
		newAPIKey, newAccountID, newDeviceID, _, regErr := registerWithBFF(ctx, cfg.CloudAPIURL, tok.AccessToken, "", runtime.GOOS, Version)
		if regErr != nil {
			log.Printf("[mtga-daemon] recovery: re-registration failed: %v; entering setup-required state", regErr)
			return fmt.Errorf("re-register recovery: re-registration failed: %w", regErr)
		}

		log.Printf("[mtga-daemon] recovery: re-registered as device %s (account %s)", newDeviceID, newAccountID)
		if err := cs.Set(newAPIKey); err != nil {
			return fmt.Errorf("re-register recovery: store new API key in credential store: %w", err)
		}

		cfg.Keychain = true
		cfg.APIKey = ""
		cfg.AccountID = newAccountID
		cfg.DaemonID = newDeviceID

		if err := cfg.SaveTo(cfgPath); err != nil {
			return fmt.Errorf("re-register recovery: write daemon.json: %w", err)
		}

		// Attach hashed account_id as Sentry user context (#1832).
		sentryhook.SetUser(newAccountID)
		log.Printf("[mtga-daemon] recovery complete — new device_id=%s written to daemon.json", newDeviceID)
		return nil
	}

	// Fresh registration (201 Created + non-empty api_key).
	log.Printf("[mtga-daemon] BFF registered (account_id=%s); storing key in credential store", accountID)
	if err := cs.Set(apiKey); err != nil {
		return fmt.Errorf("store API key in credential store: %w", err)
	}

	// Write daemon.json with keychain:true, account_id, and the server-issued
	// device_id per ADR-028 §"Implementation Notes" item 2.
	cfg.Keychain = true
	cfg.APIKey = ""
	cfg.AccountID = accountID
	cfg.DaemonID = serverDeviceID

	if err := cfg.SaveTo(cfgPath); err != nil {
		return fmt.Errorf("write daemon.json: %w", err)
	}

	// Attach the (hashed) account_id as Sentry user context so events from
	// post-auth code paths are searchable per user without storing PII.
	// Mirrors the BFF pattern (hashAccountID in posthog.go). The daemon does
	// not see the raw Clerk user_id; account_id is the stable identifier the
	// daemon does see. Issue #1832.
	sentryhook.SetUser(accountID)

	log.Printf("[mtga-daemon] first-run auth complete — daemon.json written, key in OS keychain")
	return nil
}

// registerWithBFF calls POST /daemon/register (relative to the configured
// cloud_api_url, which already includes the /api/v1 prefix) with the Clerk JWT
// and returns the minted API key, account_id, the server-authoritative device_id,
// and whether the device was already registered.
//
// alreadyRegistered is true when the BFF returns HTTP 200 with an empty
// api_key field, meaning the device_id is already known to the BFF and the
// caller should reuse the existing OS keychain entry rather than storing a
// new key. On a fresh registration the BFF returns HTTP 201 with a non-empty
// api_key.
//
// deviceID may be empty on first install — the BFF will mint a fresh UUIDv4
// per ADR-028 and echo it back in the response. The returned serverDeviceID
// must be persisted to cfg.DaemonID by the caller before cfg.SaveTo.
//
// platform is runtime.GOOS, and daemonVer is the build-time version string —
// both are required by the BFF handler.
func registerWithBFF(ctx context.Context, bffBaseURL, clerkJWT, deviceID, platform, daemonVer string) (apiKey, accountID, serverDeviceID string, alreadyRegistered bool, err error) {
	url := strings.TrimRight(bffBaseURL, "/") + "/daemon/register"

	body, err := json.Marshal(map[string]string{
		"device_id":  deviceID,
		"platform":   platform,
		"daemon_ver": daemonVer,
	})
	if err != nil {
		return "", "", "", false, fmt.Errorf("marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", "", "", false, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+clerkJWT)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", false, fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", "", false, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", "", "", false, fmt.Errorf("BFF returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		APIKey    string `json:"api_key"`
		AccountID string `json:"account_id"`
		DeviceID  string `json:"device_id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", "", "", false, fmt.Errorf("decode response: %w", err)
	}

	// HTTP 200 + empty api_key means the BFF already has this device_id on file.
	// Signal the caller to reuse the existing OS keychain entry rather than
	// treating this as an error — previously this caused os.Exit(1) and a
	// launchd respawn loop every 10 s (Issue #2169).
	if resp.StatusCode == http.StatusOK && result.APIKey == "" {
		return "", result.AccountID, result.DeviceID, true, nil
	}

	return result.APIKey, result.AccountID, result.DeviceID, false, nil
}

// revokeFromBFF calls DELETE /api/v1/daemons/{deviceID} on the BFF using the
// supplied Clerk JWT as the bearer token. Returns nil on 204, an error on any
// other status or transport failure. Used by the keychain-miss recovery path in
// runPKCEAuth (ADR-034 §3).
func revokeFromBFF(ctx context.Context, bffBaseURL, clerkJWT, deviceID string) error {
	url := strings.TrimRight(bffBaseURL, "/") + "/daemons/" + deviceID

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+clerkJWT)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("BFF returned %d: %s", resp.StatusCode, string(body))
}

// runInProcessReauth executes an in-process PKCE re-auth when the daemon
// receives a 401 from the BFF in keychain mode (AC-3, #2135). Unlike the
// first-run flow (runPKCEAuth), this function:
//
//  1. Runs a PKCE flow to obtain a fresh Clerk JWT.
//  2. Calls POST /daemon/register with the fresh JWT.
//  3. Stores the returned API key in the OS keychain (fresh registration only).
//  4. Writes daemon.json with the updated account_id / device_id.
//
// On success the daemon's keychainRefresherAdapter reads the new key from the
// OS keychain and wires it into the dispatcher via SetToken — no daemon restart
// required (Ray Q1, #2135).
//
// This is NOT a first-run path: cfg must already have CloudAPIURL, AccountID,
// DaemonID, and Keychain=true. If CLERK_FRONTEND_API or CLERK_OAUTH_CLIENT_ID
// are not set, the call returns an error and the daemon's keychainErr is set to
// ErrReauthFailed so the user sees "Keychain unavailable" in the tray.
//
// cs is the platform credential backend (ADR-081).
func runInProcessReauth(ctx context.Context, cfg *config.Config, cfgPath string, keychainService string, cs credstore.Store) error {
	clerkFrontendAPI := os.Getenv("CLERK_FRONTEND_API")
	clientID := os.Getenv("CLERK_OAUTH_CLIENT_ID")
	if clerkFrontendAPI == "" || clientID == "" {
		return fmt.Errorf("in-process reauth: CLERK_FRONTEND_API and CLERK_OAUTH_CLIENT_ID must be set")
	}

	headless := config.EnvWithFallback("VAULTMTG_DAEMON_HEADLESS", "MTGA_DAEMON_HEADLESS") == "1"
	tokenEndpoint := strings.TrimRight(clerkFrontendAPI, "/") + "/oauth/token"

	pkceCfg := pkce.Config{
		ClerkFrontendAPI: clerkFrontendAPI,
		ClientID:         clientID,
		TokenEndpoint:    tokenEndpoint,
	}

	// Add a 10-minute wall-clock deadline to bound the entire reauth flow
	// (PKCE browser wait + BFF registration). Without this cap, a hung BFF
	// call would pin reauthInProgress=true permanently, blocking all subsequent
	// 401 recovery attempts with ErrReauthRequired forever.
	//
	// ctx here is context.Background() (set by keychainRefresherAdapter per
	// the S-07 fix, #2135) — so this WithTimeout creates a fresh 10-min budget
	// and does NOT reintroduce the 5-second dispatcher context that #2135
	// intentionally excluded. The S-07 invariant is preserved.
	reauthCtx, reauthCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer reauthCancel()

	log.Printf("[mtga-daemon] in-process reauth: starting PKCE flow")
	tok, err := pkce.Run(reauthCtx, pkceCfg, headless)
	if err != nil {
		return fmt.Errorf("in-process reauth: pkce flow: %w", err)
	}

	apiKey, accountID, serverDeviceID, alreadyRegistered, err := registerWithBFF(
		reauthCtx, cfg.CloudAPIURL, tok.AccessToken, cfg.DaemonID, runtime.GOOS, Version,
	)
	if err != nil {
		return fmt.Errorf("in-process reauth: BFF registration: %w", err)
	}

	if alreadyRegistered {
		// BFF returned 200 + empty api_key: the device is already registered.
		// The API key should already be in the keychain (the 401 may have been
		// a transient BFF hiccup). Nothing to store; daemon.json stays as-is.
		log.Printf("[mtga-daemon] in-process reauth: device still registered — no new key issued")
		cfg.AccountID = accountID
		cfg.DaemonID = serverDeviceID
		return cfg.SaveTo(cfgPath)
	}

	// Fresh key issued: store in credential store and update daemon.json.
	log.Printf("[mtga-daemon] in-process reauth: new API key issued; storing in credential store")
	if err := cs.Set(apiKey); err != nil {
		return fmt.Errorf("in-process reauth: store API key in credential store: %w", err)
	}

	cfg.Keychain = true
	cfg.APIKey = ""
	cfg.AccountID = accountID
	cfg.DaemonID = serverDeviceID

	if err := cfg.SaveTo(cfgPath); err != nil {
		return fmt.Errorf("in-process reauth: write daemon.json: %w", err)
	}

	sentryhook.SetUser(accountID)
	// NB-5: log a truncated SHA-256 hash rather than the raw device_id to avoid
	// emitting a stable correlating identifier in cleartext at INFO level.
	log.Printf("[mtga-daemon] in-process reauth: complete — new device_id hash=%s", deviceIDLogToken(serverDeviceID))
	return nil
}

// fileExists returns true when path exists and is readable.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// deviceIDLogToken returns a short log-safe token for a device_id: the first 8
// hex characters (4 bytes) of its SHA-256 hash. This satisfies NB-5 / S-07
// log-hygiene: the raw identifier is never emitted at INFO level while a stable
// short token still lets an operator confirm rotation in log output.
func deviceIDLogToken(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:4])
}

// defaultConfigPath returns the channel-appropriate default config path derived
// from the install identity (ADR-049 Ticket 2):
//   - stable / Windows:  %APPDATA%\vaultmtg\daemon.json
//   - stable / macOS:    ~/.vaultmtg/daemon.json
//   - staging / Windows: %APPDATA%\vaultmtg-staging\daemon.json
//   - staging / macOS:   ~/.vaultmtg-staging/daemon.json
//
// The -config flag overrides this; Task Scheduler on Windows always passes
// -config explicitly, so the default is only used when running the binary
// directly without that flag.
func defaultConfigPath(id install.IdentitySet) string {
	if id.ConfigDir != "" {
		return filepath.Join(id.ConfigDir, "daemon.json")
	}
	// Fallback: should never be reached since Identity always populates ConfigDir.
	home, err := os.UserHomeDir()
	if err != nil {
		return "daemon.json"
	}
	return filepath.Join(home, ".vaultmtg", "daemon.json")
}

// ─── Corpus replay mode (#640, ADR-042 Amendment 1) ─────────────────────────

// headlessPairConfig holds the parameters for a server-to-server Clerk auth +
// BFF registration — the same chain used by the production synthetic canary
// (#268, scheduled-prod-canary.yml). All fields are required.
type headlessPairConfig struct {
	// ClerkBackendAPIBase is the Clerk Backend API base URL (https://api.clerk.com).
	// Used for POST /v1/sign_in_tokens and POST /v1/sessions/{id}/tokens.
	// This is NEVER the FAPI subdomain — the FAPI returns 404 on backend-API paths.
	ClerkBackendAPIBase string
	// ClerkFAPIBase is the Clerk FAPI base URL (e.g. https://accounts.clerk.dev or
	// the tenant subdomain). Used for POST /v1/client/sign_ins?strategy=ticket.
	ClerkFAPIBase string
	// ClerkSecretKey is the Clerk Backend API secret key (sk_live_* / sk_test_*).
	ClerkSecretKey string
	// ClerkUserID is the pre-provisioned synthetic account user_id in the Clerk
	// instance. Required by the sign_in_tokens endpoint.
	ClerkUserID string
	// BFFBase is the base URL of the staging BFF (cloud_api_url, including /api/v1).
	BFFBase string
	// Platform is runtime.GOOS, forwarded to the BFF daemon/register endpoint.
	Platform string
	// DaemonVersion is the build-time version string forwarded to daemon/register.
	DaemonVersion string
	// DeviceID is the cached daemon device ID (empty on first pair; BFF mints new).
	DeviceID string
}

// headlessPair executes a server-to-server Clerk auth chain and registers the
// daemon with the BFF, returning the minted API key, accountID, and deviceID.
//
// The chain mirrors the production synthetic canary (scheduled-prod-canary.yml
// CHECK 2 + CHECK 4b) — do not change this flow without reading the canary
// comments. Specifically:
//   - POST /v1/sign_in_tokens at the Backend API (not the FAPI subdomain).
//   - Exchange the ticket at FAPI for a session, then mint the session JWT via
//     the Backend API POST /v1/sessions/{id}/tokens (not the FAPI token endpoint
//     which is bot-protected).
//   - POST <bffBase>/daemon/register with the JWT.
func headlessPair(ctx context.Context, cfg headlessPairConfig) (apiKey, accountID, deviceID string, err error) {
	client := &http.Client{Timeout: 30 * time.Second}

	// Step 1 — Mint a sign-in token (Backend API).
	sitBody, _ := json.Marshal(map[string]string{"user_id": cfg.ClerkUserID})
	sitURL := strings.TrimRight(cfg.ClerkBackendAPIBase, "/") + "/v1/sign_in_tokens"
	sitReq, err := http.NewRequestWithContext(ctx, http.MethodPost, sitURL, bytes.NewReader(sitBody))
	if err != nil {
		return "", "", "", fmt.Errorf("headless-pair: build sign-in token request: %w", err)
	}
	sitReq.Header.Set("Authorization", "Bearer "+cfg.ClerkSecretKey)
	sitReq.Header.Set("Content-Type", "application/json")

	sitResp, err := client.Do(sitReq)
	if err != nil {
		return "", "", "", fmt.Errorf("headless-pair: sign-in token request: %w", err)
	}
	defer func() { _ = sitResp.Body.Close() }()
	sitBytes, _ := io.ReadAll(sitResp.Body)
	if sitResp.StatusCode != http.StatusOK && sitResp.StatusCode != http.StatusCreated {
		return "", "", "", fmt.Errorf("headless-pair: sign-in token returned %d: %s", sitResp.StatusCode, string(sitBytes))
	}
	var sitResult struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(sitBytes, &sitResult); err != nil {
		return "", "", "", fmt.Errorf("headless-pair: decode sign-in token response: %w", err)
	}
	if sitResult.Token == "" {
		return "", "", "", fmt.Errorf("headless-pair: Clerk returned empty sign-in token")
	}

	// Step 2 — Exchange ticket for session via FAPI.
	fapiURL := strings.TrimRight(cfg.ClerkFAPIBase, "/") + "/v1/client/sign_ins?strategy=ticket"
	ticketBody, _ := json.Marshal(map[string]string{"strategy": "ticket", "ticket": sitResult.Token})
	fapiReq, err := http.NewRequestWithContext(ctx, http.MethodPost, fapiURL, bytes.NewReader(ticketBody))
	if err != nil {
		return "", "", "", fmt.Errorf("headless-pair: build FAPI sign-in request: %w", err)
	}
	fapiReq.Header.Set("Content-Type", "application/json")

	fapiResp, err := client.Do(fapiReq)
	if err != nil {
		return "", "", "", fmt.Errorf("headless-pair: FAPI sign-in request: %w", err)
	}
	defer func() { _ = fapiResp.Body.Close() }()
	fapiBytes, _ := io.ReadAll(fapiResp.Body)
	if fapiResp.StatusCode != http.StatusOK && fapiResp.StatusCode != http.StatusCreated {
		return "", "", "", fmt.Errorf("headless-pair: FAPI sign-in returned %d: %s", fapiResp.StatusCode, string(fapiBytes))
	}
	var fapiResult struct {
		Client struct {
			Sessions []struct {
				ID string `json:"id"`
			} `json:"sessions"`
		} `json:"client"`
	}
	if err := json.Unmarshal(fapiBytes, &fapiResult); err != nil {
		return "", "", "", fmt.Errorf("headless-pair: decode FAPI sign-in response: %w", err)
	}
	if len(fapiResult.Client.Sessions) == 0 {
		return "", "", "", fmt.Errorf("headless-pair: no sessions in FAPI sign-in response")
	}
	sessionID := fapiResult.Client.Sessions[0].ID

	// Step 3 — Mint session JWT via Clerk Backend API.
	// Content-Type is required here as it is in Steps 1 and 2; omitting it
	// causes Clerk to return 422 Unprocessable Entity (#812).
	jwtURL := strings.TrimRight(cfg.ClerkBackendAPIBase, "/") + "/v1/sessions/" + sessionID + "/tokens"
	jwtReq, err := http.NewRequestWithContext(ctx, http.MethodPost, jwtURL, nil)
	if err != nil {
		return "", "", "", fmt.Errorf("headless-pair: build JWT request: %w", err)
	}
	jwtReq.Header.Set("Authorization", "Bearer "+cfg.ClerkSecretKey)
	jwtReq.Header.Set("Content-Type", "application/json")

	jwtResp, err := client.Do(jwtReq)
	if err != nil {
		return "", "", "", fmt.Errorf("headless-pair: JWT request: %w", err)
	}
	defer func() { _ = jwtResp.Body.Close() }()
	jwtBytes, _ := io.ReadAll(jwtResp.Body)
	if jwtResp.StatusCode != http.StatusOK && jwtResp.StatusCode != http.StatusCreated {
		return "", "", "", fmt.Errorf("headless-pair: JWT minting returned %d: %s", jwtResp.StatusCode, string(jwtBytes))
	}
	var jwtResult struct {
		JWT string `json:"jwt"`
	}
	if err := json.Unmarshal(jwtBytes, &jwtResult); err != nil {
		return "", "", "", fmt.Errorf("headless-pair: decode JWT response: %w", err)
	}
	if jwtResult.JWT == "" {
		return "", "", "", fmt.Errorf("headless-pair: Clerk returned empty session JWT")
	}

	// Step 4 — Register with the BFF.
	newAPIKey, newAccountID, newDeviceID, _, err := registerWithBFF(ctx, cfg.BFFBase, jwtResult.JWT, cfg.DeviceID, cfg.Platform, cfg.DaemonVersion)
	if err != nil {
		return "", "", "", fmt.Errorf("headless-pair: register with BFF: %w", err)
	}
	return newAPIKey, newAccountID, newDeviceID, nil
}

// replayModeConfig holds the parameters passed to runReplayMode after a
// successful headless pair. All fields are required.
type replayModeConfig struct {
	// LogFile is the path to the Player.log fixture to replay.
	LogFile string
	// APIKey is the daemon API key obtained from headlessPair / config.
	APIKey string
	// AccountID is the MTGA account ID to tag events (from daemon config or pair).
	AccountID string
	// CloudAPIURL is the BFF base URL (cloud_api_url), e.g. https://staging-api.vaultmtg.app/api/v1.
	CloudAPIURL string
}

// runReplayMode replays the given Player.log fixture through the real
// parse → handleEntry → Dispatcher.SendOrBuffer → HTTPS seam and blocks until
// replay:completed (returns nil) or replay:error / context cancellation
// (returns a non-nil error). It does NOT enter the live poller loop.
//
// This is the one-shot execution contract for the corpus-replay harness:
// exit 0 on success, exit non-zero on any failure.
func runReplayMode(ctx context.Context, cfg replayModeConfig) error {
	if _, err := os.Stat(cfg.LogFile); err != nil {
		return fmt.Errorf("[replay-mode] log file not accessible: %w", err)
	}

	daemonCfg := &config.Config{
		CloudAPIURL: cfg.CloudAPIURL,
		IngestPath:  "/ingest/events",
		APIKey:      cfg.APIKey,
		AccountID:   cfg.AccountID,
		LogPath:     cfg.LogFile,
		SyncEnabled: true,
	}

	svc := daemon.New(daemonCfg)

	// Channel closed when the replay finishes (either completed or errored).
	done := make(chan error, 1)

	go func() {
		// Replay is synchronous inside the goroutine — it reads to EOF then
		// dispatches replay:completed. runReplayMode blocks on <-done.
		svc.Replay(ctx, false)
		done <- nil
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("[replay-mode] context cancelled: %w", ctx.Err())
	}
}

// runReplayEntryPoint is the top-level entry point called from main() when
// replay mode is active. It loads config, optionally headless-pairs, runs the
// replay, and returns an exit code (0 success, 1 failure).
func runReplayEntryPoint(logFile, cfgPath string) int {
	log.Printf("[replay-mode] starting: log_file=%s config=%s", logFile, cfgPath)

	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Printf("[replay-mode] config load error: %v", err)
		// If config is missing/invalid but we have enough env vars, build a minimal config.
		cloudAPIURL := config.EnvWithFallback("VAULTMTG_DAEMON_CLOUD_API_URL", "MTGA_DAEMON_CLOUD_API_URL")
		if cloudAPIURL == "" {
			cloudAPIURL = DefaultCloudAPIURL
		}
		cfg = &config.Config{
			CloudAPIURL: cloudAPIURL,
			SyncEnabled: true,
		}
	}

	// Resolve the API key: credential store → config → headless pair.
	// ADR-049 Ticket 2: use the channel-derived identity so replay mode reads
	// from the correct credential slot (staging vs. prod).
	replayIdentity := install.Identity(install.Channel)
	replayCS := credstore.New(replayIdentity.CredentialFile, replayIdentity.KeychainService)
	apiKey := ""
	accountID := cfg.AccountID
	if cfg.Keychain {
		if k, err := replayCS.Get(); err == nil {
			apiKey = k
		}
	}
	if apiKey == "" {
		apiKey = cfg.APIKey
	}

	// If we still have no API key, attempt headless pair using env-supplied Clerk creds.
	if apiKey == "" {
		clerkSecretKey := os.Getenv("VAULTMTG_REPLAY_CLERK_SECRET_KEY")
		clerkUserID := os.Getenv("VAULTMTG_REPLAY_CLERK_USER_ID")
		clerkBackendAPIBase := os.Getenv("VAULTMTG_REPLAY_CLERK_BACKEND_API")
		if clerkBackendAPIBase == "" {
			clerkBackendAPIBase = "https://api.clerk.com"
		}
		clerkFAPIBase := os.Getenv("VAULTMTG_REPLAY_CLERK_FAPI")

		if clerkSecretKey != "" && clerkUserID != "" && clerkFAPIBase != "" {
			log.Printf("[replay-mode] no API key in config/keychain — attempting headless pair")
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()

			pairedKey, pairedAccountID, pairedDeviceID, pairErr := headlessPair(ctx, headlessPairConfig{
				ClerkBackendAPIBase: clerkBackendAPIBase,
				ClerkFAPIBase:       clerkFAPIBase,
				ClerkSecretKey:      clerkSecretKey,
				ClerkUserID:         clerkUserID,
				BFFBase:             cfg.CloudAPIURL,
				Platform:            runtime.GOOS,
				DaemonVersion:       Version,
				DeviceID:            cfg.DaemonID,
			})
			if pairErr != nil {
				log.Printf("[replay-mode] headless pair failed: %v", pairErr)
				return 1
			}
			apiKey = pairedKey
			accountID = pairedAccountID
			cfg.DaemonID = pairedDeviceID
			log.Printf("[replay-mode] headless pair succeeded (account=%s)", accountID)
		}
	}

	if apiKey == "" {
		log.Printf("[replay-mode] no API key available and no Clerk creds for headless pair — set VAULTMTG_REPLAY_CLERK_SECRET_KEY/USER_ID/FAPI")
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runReplayMode(ctx, replayModeConfig{
		LogFile:     logFile,
		APIKey:      apiKey,
		AccountID:   accountID,
		CloudAPIURL: cfg.CloudAPIURL,
	}); err != nil {
		log.Printf("[replay-mode] replay failed: %v", err)
		return 1
	}

	log.Printf("[replay-mode] completed successfully")
	return 0
}

// runConfigDirMigration copies old brand-namespaced config directories to the
// new VaultMTG-namespaced paths on daemon startup (ADR-022 Phase 2).
//
// Old directories migrated:
//   - ~/.mtga-companion  (or %APPDATA%\mtga-companion on Windows) → new config root
//   - ~/.mtga-daemon     (or %APPDATA%\mtga-daemon on Windows)    → new config root
//
// Each migration is a copy-not-move: the old directories are retained so that
// users who downgrade the daemon binary still work. Deletion of the old
// directories is deferred to Phase 6, gated on uptake telemetry.
func runConfigDirMigration() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Printf("[mtga-daemon] warn: config-dir migration skipped: could not resolve home dir: %v", err)
		return
	}

	var oldDirs []string
	var newDir string

	if runtime.GOOS == "windows" {
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			log.Printf("[mtga-daemon] warn: config-dir migration skipped: APPDATA not set")
			return
		}
		oldDirs = []string{
			filepath.Join(appdata, "mtga-companion"),
			filepath.Join(appdata, "mtga-daemon"),
		}
		newDir = filepath.Join(appdata, "vaultmtg")
	} else {
		oldDirs = []string{
			filepath.Join(home, ".mtga-companion"),
			filepath.Join(home, ".mtga-daemon"),
		}
		newDir = filepath.Join(home, ".vaultmtg")
	}

	for _, oldDir := range oldDirs {
		if err := migrate.MigrateConfigDir(oldDir, newDir); err != nil {
			log.Printf("[mtga-daemon] warn: config-dir migration %q → %q failed: %v", oldDir, newDir, err)
		}
	}
}
