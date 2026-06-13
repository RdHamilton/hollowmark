//go:build darwin

package credstore

import (
	"github.com/RdHamilton/hollowmark/services/daemon/internal/keychain"
)

// New returns the platform-appropriate Store for the current channel identity.
//
// On darwin: a FileStore backed by a 0600 file at identity.CredentialFile,
// with a migration chain that reads existing keychain entries on first Get()
// (lazy migration, ADR-081 §Decision 2). The keychain source functions are
// provided by the caller to avoid an import cycle between credstore and keychain.
//
// The MigrationSources.OnMigrated callback is intentionally left nil here;
// callers that need to emit telemetry (e.g. main.go dispatchKeychainMigrated)
// must wire it after construction via NewFileStoreWithMigration directly.
func New(credFile string, keychainService string) Store {
	return NewFileStoreWithMigration(credFile, MigrationSources{
		// Primary migration source: ADR-022 Phase 3 "com.hollowmark.daemon" slot.
		KeychainNewGet: func() (string, error) {
			key, err := keychain.GetForService(keychainService)
			if err != nil {
				if isKeychainNotFound(err) {
					return "", ErrNotFound
				}
				// go-keyring interaction-not-allowed error → ErrAccessDenied.
				return "", ErrAccessDenied
			}
			return key, nil
		},
		// Fallback migration source: ADR-022 Phase 2 "com.vaultmtg.daemon" legacy slot.
		KeychainLegacyGet: func() (string, error) {
			key, err := keychain.GetForService(keychain.ServiceNameLegacy)
			if err != nil {
				if isKeychainNotFound(err) {
					return "", ErrNotFound
				}
				return "", ErrAccessDenied
			}
			return key, nil
		},
		// OnMigrated: nil — caller wires telemetry if needed.
	})
}

// isKeychainNotFound detects go-keyring's ErrNotFound sentinel.
func isKeychainNotFound(err error) bool {
	// keychain.ErrNotFound is the package-level sentinel returned by GetForService
	// when no entry exists.  Compare directly rather than importing keyring to
	// avoid a dependency on the CGO-free keyring internals.
	return err != nil && err.Error() == keychain.ErrNotFound.Error()
}
