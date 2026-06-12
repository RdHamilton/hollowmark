//go:build !darwin && !windows

package credstore

import (
	"github.com/RdHamilton/hollowmark/services/daemon/internal/keychain"
)

// New returns the platform-appropriate Store for the current channel identity.
//
// On non-darwin, non-windows platforms (e.g. Linux CI): a KeychainStore backed
// by go-keyring. In practice the daemon only ships for darwin and windows;
// this stub exists so the package compiles cleanly on Linux CI runners.
func New(credFile string, keychainService string) Store {
	return &keychainStore{service: keychainService}
}

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
