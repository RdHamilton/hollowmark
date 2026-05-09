package keychain_test

import (
	"testing"

	"github.com/ramonehamilton/mtga-daemon/internal/keychain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// useMemoryKeyring switches go-keyring to its in-memory mock backend for the
// duration of the test.  This avoids touching the real OS keychain and works
// on every platform including headless CI runners.
func useMemoryKeyring(t *testing.T) {
	t.Helper()
	keyring.MockInit()
	t.Cleanup(func() { keyring.MockInitWithError(nil) }) // reset after test
}

func TestGet_NotFound(t *testing.T) {
	useMemoryKeyring(t)
	_, err := keychain.Get()
	assert.ErrorIs(t, err, keychain.ErrNotFound)
}

func TestSetAndGet(t *testing.T) {
	useMemoryKeyring(t)

	const key = "sk_live_test1234"
	require.NoError(t, keychain.Set(key))

	got, err := keychain.Get()
	require.NoError(t, err)
	assert.Equal(t, key, got)
}

func TestSet_Overwrite(t *testing.T) {
	useMemoryKeyring(t)

	require.NoError(t, keychain.Set("sk_live_first"))
	require.NoError(t, keychain.Set("sk_live_second"))

	got, err := keychain.Get()
	require.NoError(t, err)
	assert.Equal(t, "sk_live_second", got)
}

func TestDelete_Existing(t *testing.T) {
	useMemoryKeyring(t)

	require.NoError(t, keychain.Set("sk_live_todelete"))
	require.NoError(t, keychain.Delete())

	_, err := keychain.Get()
	assert.ErrorIs(t, err, keychain.ErrNotFound)
}

func TestDelete_Idempotent(t *testing.T) {
	useMemoryKeyring(t)
	// Delete on empty keychain must not error.
	assert.NoError(t, keychain.Delete())
}

func TestConstants(t *testing.T) {
	assert.Equal(t, "com.mtga-companion.daemon", keychain.ServiceName)
	assert.Equal(t, "api-key", keychain.AccountKey)
}
