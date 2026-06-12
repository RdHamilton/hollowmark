//go:build windows

package credstore

import (
	"github.com/RdHamilton/hollowmark/services/daemon/internal/keychain"
)

// New returns the platform-appropriate Store for the current channel identity.
//
// On windows: a KeychainStore backed by Windows Credential Manager via
// go-keyring. The keychainService parameter is the channel-derived service
// name (e.g. "com.hollowmark.daemon" for stable).
func New(credFile string, keychainService string) Store {
	return &keychainStore{service: keychainService}
}

// keychainStore implements Store using go-keyring (Windows Credential Manager).
type keychainStore struct {
	service string
}

func (s *keychainStore) Get() (string, error) {
	key, err := keychain.GetForService(s.service)
	if err != nil {
		if err.Error() == keychain.ErrNotFound.Error() {
			return "", ErrNotFound
		}
		return "", err
	}
	if key == "" {
		return "", ErrNotFound
	}
	return key, nil
}

func (s *keychainStore) Set(apiKey string) error {
	return keychain.SetForService(s.service, apiKey)
}

func (s *keychainStore) Delete() error {
	return keychain.Delete()
}
