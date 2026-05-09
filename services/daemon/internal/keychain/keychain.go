// Package keychain provides CGO-free OS keychain access for daemon API key storage.
//
// Service name: com.mtga-companion.daemon
// Account key:  api-key
//
// On macOS, go-keyring uses the Keychain Services API via security(1) subprocess —
// no CGO required.  On Windows it uses the Windows Credential Manager via
// golang.org/x/sys/windows syscalls — also CGO-free.
// Both targets cross-compile cleanly from a macOS/Linux CI runner.
//
// See ADR-020 §Keychain Storage for the full design rationale.
package keychain

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const (
	// ServiceName is the OS keychain service identifier for the daemon.
	ServiceName = "com.mtga-companion.daemon"

	// AccountKey is the OS keychain account name under which the API key is stored.
	AccountKey = "api-key"
)

// ErrNotFound is returned when no API key is stored in the keychain for this daemon.
var ErrNotFound = errors.New("keychain: api key not found")

// Get retrieves the daemon API key from the OS keychain.
// Returns ErrNotFound when no key has been stored yet.
func Get() (string, error) {
	val, err := keyring.Get(ServiceName, AccountKey)
	if err != nil {
		if isNotFound(err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("keychain: get: %w", err)
	}
	return val, nil
}

// Set stores the daemon API key in the OS keychain, creating or replacing any
// existing entry for ServiceName/AccountKey.
func Set(apiKey string) error {
	if err := keyring.Set(ServiceName, AccountKey, apiKey); err != nil {
		return fmt.Errorf("keychain: set: %w", err)
	}
	return nil
}

// Delete removes the daemon API key from the OS keychain.
// Returns nil if no key was stored (idempotent).
func Delete() error {
	err := keyring.Delete(ServiceName, AccountKey)
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
