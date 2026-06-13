// Package credstore provides a platform-appropriate credential storage backend
// for the VaultMTG daemon API key.
//
// On darwin, credentials are stored in a 0600 file in the daemon's ConfigDir
// (ADR-081 §Decision 1). This replaces the macOS Keychain, which returns
// errSecInteractionNotAllowed (−25308) in headless LaunchAgent context,
// causing every startup to see a "missing" credential and trigger a full PKCE
// re-auth + device re-registration cycle (#1345).
//
// On Windows/Linux, credentials remain in the OS credential manager (Windows
// Credential Manager via go-keyring). The interface is identical so callers
// never branch on platform.
//
// # Error contract
//
// ErrNotFound is the ONLY error that means "no credential stored; run PKCE."
// ErrAccessDenied means the credential file (or OS keychain entry) exists but
// cannot be read due to a permission error. Callers MUST NOT conflate
// ErrAccessDenied with ErrNotFound — conflation is the root bug (#1345).
package credstore

import "errors"

// ErrNotFound is returned when no credential is stored. This is the only
// sentinel that authorises a PKCE first-run flow.
var ErrNotFound = errors.New("credstore: credential not found")

// ErrAccessDenied is returned when the credential storage exists but the
// process cannot read it (OS permission / ACL denial). Callers must NOT
// treat this as first-run; instead they must enter an idle-degraded state
// (ADR-081 §Decision 3, Ray R1).
var ErrAccessDenied = errors.New("credstore: credential access denied")

// Store is the credential-storage interface.
// Implementations must be safe for concurrent use from a single goroutine
// (the daemon service owns all credential I/O; no concurrent access).
type Store interface {
	// Get retrieves the stored API key.
	//   - Returns (key, nil) on success.
	//   - Returns ("", ErrNotFound) when no credential is stored.
	//   - Returns ("", ErrAccessDenied) when the credential exists but is unreadable.
	//   - Returns ("", <other error>) for unexpected failures.
	Get() (string, error)

	// Set stores the API key, creating or replacing any existing credential.
	Set(apiKey string) error

	// Delete removes the stored credential. Returns nil if no credential
	// exists (idempotent).
	Delete() error
}

// MigrationSources supplies the keychain fallback chain used by the FileStore
// on the first Get() call after upgrade (lazy migration, R3).
// All fields are optional — nil functions are skipped.
type MigrationSources struct {
	// KeychainNewGet reads from the "com.hollowmark.daemon" keychain slot
	// (ADR-022 Phase 3 production name).
	KeychainNewGet func() (string, error)

	// KeychainLegacyGet reads from the "com.vaultmtg.daemon" keychain slot
	// (ADR-022 Phase 2 legacy name).
	KeychainLegacyGet func() (string, error)

	// OnMigrated is called (once, synchronously) when a keychain value is
	// successfully read and written to the file. Use this to emit the
	// "keychain.migrated" telemetry event (ADR-022 Phase 3 AC16).
	// May be nil.
	OnMigrated func()
}
