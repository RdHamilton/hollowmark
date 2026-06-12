package credstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileStore implements Store using a mode-0600 file on disk.
//
// On darwin this is the authoritative backend (ADR-081). On other platforms
// it is used only in tests; production non-darwin builds use KeychainStore.
//
// File path: supplied at construction (typically
// filepath.Join(identity.ConfigDir, "credentials")).
//
// Write contract: atomic temp+rename within the same directory so the file
// is never partially written. Directory is created at mode 0700 if absent.
type FileStore struct {
	path    string
	migrate *MigrationSources
}

// NewFileStore returns a FileStore that reads/writes to path.
// No migration fallback is wired (use NewFileStoreWithMigration for that).
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// NewFileStoreWithMigration returns a FileStore that, on the first Get() call
// when no file is present, consults src to migrate an existing keychain entry
// into the file (lazy migration, ADR-081 §Decision 2, Ray R3).
func NewFileStoreWithMigration(path string, src MigrationSources) *FileStore {
	return &FileStore{path: path, migrate: &src}
}

// Get retrieves the stored API key.
//
// Read order (ADR-081 §Decision 2):
//  1. Credential file — if present and non-empty, return it.
//  2. KeychainNewGet migration source — if provided and returns a key, write
//     file, fire OnMigrated, return key.
//  3. KeychainLegacyGet migration source — same treatment.
//  4. Neither source has a key → ErrNotFound.
//
// Error semantics:
//   - File present but unreadable (EACCES) → ErrAccessDenied (NOT ErrNotFound).
//   - File absent with no migration source → ErrNotFound.
//   - Migration source returns ErrAccessDenied → propagate (do not collapse).
func (s *FileStore) Get() (string, error) {
	// ── 1. Try the credential file ────────────────────────────────────────────
	raw, err := os.ReadFile(s.path)
	if err == nil {
		key := strings.TrimSpace(string(raw))
		if key == "" {
			return "", ErrNotFound
		}
		return key, nil
	}
	if errors.Is(err, os.ErrPermission) {
		return "", ErrAccessDenied
	}
	if !errors.Is(err, os.ErrNotExist) {
		// Unexpected OS error (e.g. I/O error on a device) — propagate wrapped.
		return "", fmt.Errorf("credstore: read credential file: %w", err)
	}

	// File is absent — try migration sources if wired.
	if s.migrate == nil {
		return "", ErrNotFound
	}

	// ── 2. Try keychain "new" slot ────────────────────────────────────────────
	if s.migrate.KeychainNewGet != nil {
		key, kcErr := s.migrate.KeychainNewGet()
		if kcErr == nil && key != "" {
			return s.migrateKey(key)
		}
		if kcErr != nil && !errors.Is(kcErr, ErrNotFound) {
			// Access-denied or unexpected error — propagate; do not fall through.
			return "", kcErr
		}
	}

	// ── 3. Try keychain legacy slot ───────────────────────────────────────────
	if s.migrate.KeychainLegacyGet != nil {
		key, kcErr := s.migrate.KeychainLegacyGet()
		if kcErr == nil && key != "" {
			return s.migrateKey(key)
		}
		if kcErr != nil && !errors.Is(kcErr, ErrNotFound) {
			return "", kcErr
		}
	}

	return "", ErrNotFound
}

// migrateKey writes key to the credential file, fires the OnMigrated callback,
// and returns the key. Called only when the file was absent and a keychain
// source had a key.
func (s *FileStore) migrateKey(key string) (string, error) {
	if err := s.Set(key); err != nil {
		// Migration write failed — still return the key so the daemon continues
		// this run; next run retries the migration. Log line not emitted here
		// (callers log via their own logger).
		return key, nil
	}
	if s.migrate != nil && s.migrate.OnMigrated != nil {
		s.migrate.OnMigrated()
	}
	return key, nil
}

// Set stores apiKey atomically: write to a temp file, chmod 0600, rename.
// The parent directory is created at mode 0700 if absent.
func (s *FileStore) Set(apiKey string) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("credstore: mkdir %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".cred-*.tmp")
	if err != nil {
		return fmt.Errorf("credstore: create temp file: %w", err)
	}
	tmpName := tmp.Name()

	if _, werr := tmp.WriteString(apiKey); werr != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("credstore: write temp file: %w", werr)
	}
	if cerr := tmp.Close(); cerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("credstore: close temp file: %w", cerr)
	}
	if cherr := os.Chmod(tmpName, 0o600); cherr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("credstore: chmod temp file: %w", cherr)
	}
	if rerr := os.Rename(tmpName, s.path); rerr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("credstore: rename to %q: %w", s.path, rerr)
	}
	return nil
}

// Delete removes the credential file. Returns nil if the file does not exist
// (idempotent).
func (s *FileStore) Delete() error {
	err := os.Remove(s.path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("credstore: delete %q: %w", s.path, err)
	}
	return nil
}
