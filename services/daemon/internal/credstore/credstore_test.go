package credstore_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/credstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── FileStore unit tests ────────────────────────────────────────────────────

// TestFileStore_RoundTrip verifies Set/Get/Delete operate on the credential
// file at the expected path with correct permissions.
func TestFileStore_RoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FileStore is a darwin-only backend; skip on Windows")
	}
	dir := t.TempDir()
	credFile := filepath.Join(dir, "credentials")

	store := credstore.NewFileStore(credFile)

	// File should not exist yet — Get returns ErrNotFound.
	_, err := store.Get()
	require.ErrorIs(t, err, credstore.ErrNotFound, "Get on missing file must return ErrNotFound")

	// Set stores the key.
	require.NoError(t, store.Set("test-api-key-abc123"))

	// Credential file must exist with mode 0600.
	info, err := os.Stat(credFile)
	require.NoError(t, err, "credential file must exist after Set")
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
			"credential file must be mode 0600")
	}

	// Get returns the stored key.
	got, err := store.Get()
	require.NoError(t, err)
	assert.Equal(t, "test-api-key-abc123", got)

	// Delete removes the file; subsequent Get returns ErrNotFound.
	require.NoError(t, store.Delete())
	_, err = store.Get()
	require.ErrorIs(t, err, credstore.ErrNotFound, "Get after Delete must return ErrNotFound")
}

// TestFileStore_DeleteIdempotent verifies Delete is idempotent on a missing file.
func TestFileStore_DeleteIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FileStore is a darwin-only backend; skip on Windows")
	}
	dir := t.TempDir()
	store := credstore.NewFileStore(filepath.Join(dir, "credentials"))
	require.NoError(t, store.Delete())
	require.NoError(t, store.Delete())
}

// TestFileStore_EmptyContentIsNotFound verifies that a whitespace-only
// credential file is treated as ErrNotFound (defensive: empty = no key).
func TestFileStore_EmptyContentIsNotFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FileStore is a darwin-only backend; skip on Windows")
	}
	dir := t.TempDir()
	credFile := filepath.Join(dir, "credentials")
	require.NoError(t, os.WriteFile(credFile, []byte("   \n"), 0o600))

	store := credstore.NewFileStore(credFile)
	_, err := store.Get()
	require.ErrorIs(t, err, credstore.ErrNotFound, "whitespace-only content must return ErrNotFound")
}

// TestFileStore_DirectoryPermissions verifies the parent directory is created
// at mode 0700 when it does not exist yet.
func TestFileStore_DirectoryPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FileStore is a darwin-only backend; skip on Windows")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "config")
	store := credstore.NewFileStore(filepath.Join(dir, "credentials"))

	require.NoError(t, store.Set("key"))

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(),
		"parent directory must be created at mode 0700")
}

// TestFileStore_PermissionDenied_ReturnsErrAccessDenied verifies that a file
// that exists but is unreadable (mode 0000) returns ErrAccessDenied, not
// ErrNotFound. This is the launchd ACL scenario.
func TestFileStore_PermissionDenied_ReturnsErrAccessDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file-mode permission model differs on Windows; skip")
	}
	if os.Getuid() == 0 {
		t.Skip("running as root — chmod 000 does not restrict root; skip")
	}
	dir := t.TempDir()
	credFile := filepath.Join(dir, "credentials")
	require.NoError(t, os.WriteFile(credFile, []byte("secret-key"), 0o600))

	require.NoError(t, os.Chmod(credFile, 0o000))
	t.Cleanup(func() { _ = os.Chmod(credFile, 0o600) })

	store := credstore.NewFileStore(credFile)
	_, err := store.Get()
	require.ErrorIs(t, err, credstore.ErrAccessDenied,
		"unreadable credential file must return ErrAccessDenied, not ErrNotFound")
}

// TestFileStore_AtomicWrite verifies no temp files remain after successful Set.
func TestFileStore_AtomicWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FileStore is a darwin-only backend; skip on Windows")
	}
	dir := t.TempDir()
	store := credstore.NewFileStore(filepath.Join(dir, "credentials"))

	for i := 0; i < 5; i++ {
		require.NoError(t, store.Set("key-iteration-value"))
		got, err := store.Get()
		require.NoError(t, err)
		assert.Equal(t, "key-iteration-value", got)
	}

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp",
			"no temp files should remain after successful Set")
	}
}

// ─── Migration chain tests ────────────────────────────────────────────────────

// TestFileStore_MigrationChain_FromKeychainNew verifies that when the
// credential file is absent but the new keychain service has a key, Get
// migrates the key to the file and returns it (lazy first-read migration, R3).
func TestFileStore_MigrationChain_FromKeychainNew(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FileStore is a darwin-only backend; skip on Windows")
	}
	dir := t.TempDir()
	credFile := filepath.Join(dir, "credentials")

	var migratedEventFired bool
	store := credstore.NewFileStoreWithMigration(credFile, credstore.MigrationSources{
		KeychainNewGet:    func() (string, error) { return "migrated-key-from-new", nil },
		KeychainLegacyGet: func() (string, error) { return "", credstore.ErrNotFound },
		OnMigrated:        func() { migratedEventFired = true },
	})

	got, err := store.Get()
	require.NoError(t, err, "migration from keychain-new must succeed")
	assert.Equal(t, "migrated-key-from-new", got)
	assert.True(t, migratedEventFired, "keychain.migrated telemetry must fire on migration")

	// Key must be persisted in the file.
	info, err := os.Stat(credFile)
	require.NoError(t, err, "credential file must be written after migration")
	if runtime.GOOS != "windows" {
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	// Second Get must read from file without calling keychain again.
	var keychainNewCalls int
	store2 := credstore.NewFileStoreWithMigration(credFile, credstore.MigrationSources{
		KeychainNewGet: func() (string, error) {
			keychainNewCalls++
			return "", credstore.ErrNotFound
		},
		KeychainLegacyGet: func() (string, error) { return "", credstore.ErrNotFound },
	})
	got2, err2 := store2.Get()
	require.NoError(t, err2)
	assert.Equal(t, "migrated-key-from-new", got2)
	assert.Equal(t, 0, keychainNewCalls, "second Get must read from file, not keychain")
}

// TestFileStore_MigrationChain_FallsBackToLegacy verifies that when the new
// keychain slot is absent but the legacy slot has a key, migration reads from
// legacy and writes to the file.
func TestFileStore_MigrationChain_FallsBackToLegacy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FileStore is a darwin-only backend; skip on Windows")
	}
	dir := t.TempDir()
	credFile := filepath.Join(dir, "credentials")

	store := credstore.NewFileStoreWithMigration(credFile, credstore.MigrationSources{
		KeychainNewGet:    func() (string, error) { return "", credstore.ErrNotFound },
		KeychainLegacyGet: func() (string, error) { return "legacy-key", nil },
	})

	got, err := store.Get()
	require.NoError(t, err)
	assert.Equal(t, "legacy-key", got)

	// Key must be persisted in the file.
	got2, err2 := credstore.NewFileStore(credFile).Get()
	require.NoError(t, err2)
	assert.Equal(t, "legacy-key", got2)
}

// TestFileStore_MigrationChain_NeitherSource_ReturnsNotFound verifies that
// when no migration source has a key, Get returns ErrNotFound (fresh install).
func TestFileStore_MigrationChain_NeitherSource_ReturnsNotFound(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FileStore is a darwin-only backend; skip on Windows")
	}
	dir := t.TempDir()
	store := credstore.NewFileStoreWithMigration(filepath.Join(dir, "credentials"),
		credstore.MigrationSources{
			KeychainNewGet:    func() (string, error) { return "", credstore.ErrNotFound },
			KeychainLegacyGet: func() (string, error) { return "", credstore.ErrNotFound },
		})

	_, err := store.Get()
	require.ErrorIs(t, err, credstore.ErrNotFound)
}

// TestFileStore_MigrationChain_KeychainAccessDenied_ReturnsErrAccessDenied
// verifies that a keychain access-denied error during migration propagates as
// ErrAccessDenied rather than collapsing into ErrNotFound (R3 of ADR-081,
// ray verdict).
func TestFileStore_MigrationChain_KeychainAccessDenied_ReturnsErrAccessDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FileStore is a darwin-only backend; skip on Windows")
	}
	dir := t.TempDir()
	store := credstore.NewFileStoreWithMigration(filepath.Join(dir, "credentials"),
		credstore.MigrationSources{
			KeychainNewGet:    func() (string, error) { return "", credstore.ErrAccessDenied },
			KeychainLegacyGet: func() (string, error) { return "", credstore.ErrNotFound },
		})

	_, err := store.Get()
	require.ErrorIs(t, err, credstore.ErrAccessDenied,
		"keychain access-denied during migration must propagate as ErrAccessDenied")
}
