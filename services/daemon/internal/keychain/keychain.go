// Package keychain provides CGO-free OS keychain access for daemon API key storage.
//
// Service names (ADR-022 Phase 3 — v0.3.9 hollowmark credential shim):
//
//	ServiceNameNew    = "com.hollowmark.daemon"  (production — all writes go here)
//	ServiceNameLegacy = "com.vaultmtg.daemon"    (read-only fallback for v0.3.8 upgrade)
//
// Account key:  api-key
//
// On macOS, go-keyring uses the Keychain Services API via security(1) subprocess —
// no CGO required.  On Windows it uses the Windows Credential Manager via
// golang.org/x/sys/windows syscalls — also CGO-free.
// Both targets cross-compile cleanly from a macOS/Linux CI runner.
//
// Upgrade migration (ADR-022 Constraint 1):
// On startup, Get() first tries ServiceNameNew.  If the entry is absent it tries
// ServiceNameLegacy; when found there, it copies the key forward to ServiceNameNew
// (so subsequent reads hit the new name), returns migrated=true so the caller can
// emit the keychain.migrated telemetry event, and logs the migration at INFO.  The
// legacy entry is RETAINED — never deleted — to allow safe downgrade.  Deletion of
// the legacy entry is deferred to Phase 6, gated on AC16 adoption telemetry.
//
// NOTE: the com.mtga-companion.daemon constant (ADR-022 Phase 1) is no longer
// present in this package — any install that still had credentials only at that
// name would have been migrated to com.vaultmtg.daemon during the v0.3.x cycle
// (ADR-022 Phase 2), and that cohort is below the 5% threshold per ADR-022 §cleanup.
// The launchd orphan-job cleanup in install.sh still references com.mtga-companion.daemon
// independently and is NOT removed here.
//
// See ADR-020 §Keychain Storage for the full design rationale.
package keychain

import (
	"errors"
	"fmt"
	"log"

	"github.com/zalando/go-keyring"
)

const (
	// ServiceNameNew is the current OS keychain service identifier for the daemon
	// (ADR-022 Phase 3 hollowmark credential shim — v0.3.9).  All writes target this name.
	// In v0.4.0 the bundle ID will also rename to com.hollowmark.daemon; v0.3.9 ships
	// only this credential shim so existing users' keys survive the flip.
	ServiceNameNew = "com.hollowmark.daemon"

	// ServiceNameLegacy is the v0.3.8 OS keychain service identifier retained for
	// read-only upgrade migration.  Do NOT write to this name in production code.
	// This constant is used ONLY in the Get() fallback branch and in tests.
	// Deletion of the legacy entry is deferred to Phase 6, gated on AC16 adoption telemetry.
	ServiceNameLegacy = "com.vaultmtg.daemon"

	// AccountKey is the OS keychain account name under which the API key is stored.
	AccountKey = "api-key"
)

// ErrNotFound is returned when no API key is stored in the keychain for this daemon.
var ErrNotFound = errors.New("keychain: api key not found")

// keyringGet is the package-level indirection for keyring.Get used by Get().
// Tests substitute this variable to inject per-service-name behavior that
// the go-keyring mock backend cannot express (MockInitWithError applies the
// same error to every call).  Production code calls keyring.Get directly via
// this variable; do not reassign outside of tests.
var keyringGet = keyring.Get

// GetForService retrieves the daemon API key from the OS keychain using the
// supplied service name instead of the package-default ServiceNameNew.
//
// This is the channel-aware variant used by runtime consumers that have
// resolved install.Identity(install.Channel).KeychainService at startup
// (ADR-049 Ticket 2). Unlike Get(), it does NOT perform the legacy-migration
// fallback — the caller is responsible for supplying the correct service name
// for their channel.
//
// Returns ErrNotFound when no entry exists under service.
func GetForService(service string) (string, error) {
	val, err := keyringGet(service, AccountKey)
	if err == nil {
		return val, nil
	}
	if isNotFound(err) {
		return "", ErrNotFound
	}
	return "", fmt.Errorf("keychain: get %q: %w", service, err)
}

// SetForService stores the daemon API key in the OS keychain under the given
// service name, creating or replacing any existing entry.
//
// This is the channel-aware variant used by runtime consumers (ADR-049 Ticket 2).
// The caller is responsible for passing the correct channel-derived service name.
func SetForService(service, apiKey string) error {
	if err := keyring.Set(service, AccountKey, apiKey); err != nil {
		return fmt.Errorf("keychain: set %q: %w", service, err)
	}
	return nil
}

// Get retrieves the daemon API key from the OS keychain.
//
// Migration path (ADR-022 Constraint 1, Phase 3):
//  1. Try ServiceNameNew ("com.hollowmark.daemon").  If found → return (key, false, nil).
//  2. Try ServiceNameLegacy ("com.vaultmtg.daemon").  If found → copy key forward to
//     ServiceNameNew, log the migration at INFO, and return (key, true, nil).
//     The legacy entry is retained (NOT deleted) for downgrade safety.
//     The caller MUST emit the keychain.migrated telemetry event when migrated=true.
//  3. Neither entry present → return ("", false, ErrNotFound) (triggers normal PKCE re-auth).
//
// A corrupted / unreadable legacy entry is treated as absent and falls through
// to ErrNotFound so the caller initiates re-auth rather than crashing.
//
// The migrated bool is the idempotency signal: once the hollowmark entry is written,
// subsequent calls find it at step 1 and return migrated=false — the copy-forward
// and telemetry event fire exactly once per install.
func Get() (apiKey string, migrated bool, err error) {
	// ── 1. Try new service name first ────────────────────────────────────────
	val, getErr := keyringGet(ServiceNameNew, AccountKey)
	if getErr == nil {
		return val, false, nil
	}
	if !isNotFound(getErr) {
		return "", false, fmt.Errorf("keychain: get %q: %w", ServiceNameNew, getErr)
	}

	// ── 2. Fall back to legacy service name ──────────────────────────────────
	legacyVal, legacyErr := keyringGet(ServiceNameLegacy, AccountKey)
	if legacyErr != nil {
		if isNotFound(legacyErr) {
			// Neither entry present — fresh install or wiped keychain.
			return "", false, ErrNotFound
		}
		// Corrupted / unreadable legacy entry: log a warning and fall through
		// to ErrNotFound so normal PKCE re-auth is triggered rather than crashing.
		log.Printf("[keychain] warn: could not read legacy entry %q: %v — falling through to re-auth", ServiceNameLegacy, legacyErr)
		return "", false, ErrNotFound
	}

	// ── 3. Copy forward to new service name ──────────────────────────────────
	// The legacy entry is RETAINED (not deleted) for downgrade safety.
	// Deletion of the legacy entry is deferred to Phase 6.
	if copyErr := keyring.Set(ServiceNameNew, AccountKey, legacyVal); copyErr != nil {
		log.Printf("[keychain] warn: could not copy legacy keychain entry to %q: %v — proceeding with legacy key for this run", ServiceNameNew, copyErr)
		// Copy failed: return the key so the daemon continues, but migrated=false
		// since the new entry was not actually written (next run retries the copy).
		return legacyVal, false, nil
	}
	log.Printf("[keychain] INFO: migrated keychain entry from %q to %q (legacy entry retained for downgrade safety)", ServiceNameLegacy, ServiceNameNew)

	return legacyVal, true, nil
}

// Set stores the daemon API key in the OS keychain under ServiceNameNew,
// creating or replacing any existing entry.
// The legacy ServiceNameLegacy entry is never written by this function.
func Set(apiKey string) error {
	if err := keyring.Set(ServiceNameNew, AccountKey, apiKey); err != nil {
		return fmt.Errorf("keychain: set: %w", err)
	}
	return nil
}

// Delete removes the daemon API key from the OS keychain (ServiceNameNew only).
// The legacy entry is NOT deleted — it is retained for downgrade safety.
// Returns nil if no key was stored (idempotent).
func Delete() error {
	err := keyring.Delete(ServiceNameNew, AccountKey)
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("keychain: delete: %w", err)
	}
	return nil
}

// isNotFound detects the go-keyring "not found" sentinel.
// go-keyring returns keyring.ErrNotFound — compare by value.
func isNotFound(err error) bool {
	return errors.Is(err, keyring.ErrNotFound)
}
