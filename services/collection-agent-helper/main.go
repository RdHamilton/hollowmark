//go:build darwin

// Command collection-helper exposes a Unix socket at
// /tmp/com.vaultmtg.collection-helper.sock. The VaultMTG daemon connects to
// this socket to request a collection scan. The helper calls task_for_pid
// against the running MTGA process (authorized via com.apple.TaskForPid-allow
// and the com.apple.security.cs.debugger entitlement — ADR-059) and returns
// the card inventory as JSON.
//
// The helper runs as the logged-in user (not root). task_for_pid succeeds
// because the signed+notarized binary carries the com.apple.security.cs.debugger
// entitlement and the user has completed the one-time admin authorization dialog
// (RequestOneTimeAuthorization) on first enhanced-mode enable.
//
// Derivation / diagnostic mode:
//
//	./collection-helper --dump-regions <PID> <outdir>
//
// Dumps all readable VM regions from <PID> to <outdir>/region_NNNN_0x<addr>.bin
// and writes <outdir>/manifest.json. Uses the same non-intrusive mach_vm_read
// path as production — no process suspension, no debugger.
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
)

// HelperVersion is injected at build time via -X main.HelperVersion=<tag>.
// It is the daemon release tag (e.g. "0.4.3") under which this binary was
// built.  Postinstall logs the value immediately after installation so the
// version is visible in the install log without launching the daemon.
// The socket VersionRequest / skew-check is a separate follow-on
// (hollowmark-tickets#1286, R6).
var HelperVersion = "dev"

func main() {
	// --version: print the build-time version and exit (R6 — postinstall logs it).
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(HelperVersion)
		return
	}

	// --authorize: perform the one-time admin authorization dialog for
	// com.apple.TaskForPid-allow (ADR-059) and exit.  The daemon's tray
	// "Grant Access" flow invokes this flag via triggerHelperAuthorization().
	// AuthorizationCopyRights surfaces the system admin-password dialog; the
	// grant is cached so subsequent --authorize calls are no-ops.
	if len(os.Args) == 2 && os.Args[1] == "--authorize" {
		if err := RequestOneTimeAuthorization(); err != nil {
			fmt.Fprintf(os.Stderr, "authorization failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("authorization granted")
		return
	}

	log.SetPrefix("[collection-helper] ")
	log.SetFlags(log.Ldate | log.Ltime)

	// --dump-regions <PID> <outdir>
	// One-shot dump mode for offline H1/H2 derivation (vault-mtg-tickets#202).
	// Uses the same listReadableRegions + readMemory path as production — safe,
	// non-intrusive, no process suspension.
	if len(os.Args) == 4 && os.Args[1] == "--dump-regions" {
		pid, err := strconv.Atoi(os.Args[2])
		if err != nil || pid <= 0 {
			fmt.Fprintf(os.Stderr, "invalid PID %q: %v\n", os.Args[2], err)
			os.Exit(1)
		}
		outdir := os.Args[3]
		log.Printf("dump-regions mode: pid=%d outdir=%s", pid, outdir)
		if err := runDumpRegions(pid, outdir); err != nil {
			log.Fatalf("dump-regions: %v", err)
		}
		return
	}

	log.Printf("starting (pid=%d)", os.Getpid())
	// Emit the active signature version at startup so CloudWatch / on-call triage
	// can correlate a COLLECTION_SCAN_DRIFT alarm with the known-good signature.
	// mtga_build=unknown until v0.3.5 adds Info.plist detection (ADR-040 §G4).
	log.Printf("signature_version=%s mtga_build=unknown note=%q",
		CollectionSignatureVersion, knownSignatureVersions[CollectionSignatureVersion])
	if err := runServer(); err != nil {
		log.Fatalf("server: %v", err)
	}
}
