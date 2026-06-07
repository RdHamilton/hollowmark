package keychain_test

import (
	"testing"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/keychain"
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

// TestConstants verifies the exported constants hold the correct values so
// callers can rely on the string literals (e.g. for launchd label matching).
// ADR-022 Phase 3 (v0.3.9): ServiceNameNew advances to "com.hollowmark.daemon";
// ServiceNameLegacy is now "com.vaultmtg.daemon" (the previous write target).
func TestConstants(t *testing.T) {
	assert.Equal(t, "com.hollowmark.daemon", keychain.ServiceNameNew)
	assert.Equal(t, "com.vaultmtg.daemon", keychain.ServiceNameLegacy)
	assert.Equal(t, "api-key", keychain.AccountKey)
}

// ── Scenario 1: new entry present ────────────────────────────────────────────

// TestGet_NewEntryPresent verifies that when ServiceNameNew has an entry
// Get() returns it without touching the legacy service name, and migrated=false.
func TestGet_NewEntryPresent(t *testing.T) {
	useMemoryKeyring(t)

	const wantKey = "sk_live_newentry"
	require.NoError(t, keyring.Set(keychain.ServiceNameNew, keychain.AccountKey, wantKey))

	got, migrated, err := keychain.Get()
	require.NoError(t, err)
	assert.Equal(t, wantKey, got)
	assert.False(t, migrated, "new entry already present: migrated must be false")
}

// TestSet_WritesToNewServiceName confirms that Set() stores under ServiceNameNew.
func TestSet_WritesToNewServiceName(t *testing.T) {
	useMemoryKeyring(t)

	const wantKey = "sk_live_writtenkey"
	require.NoError(t, keychain.Set(wantKey))

	got, err := keyring.Get(keychain.ServiceNameNew, keychain.AccountKey)
	require.NoError(t, err)
	assert.Equal(t, wantKey, got)
}

// TestSetAndGet is the basic round-trip test: Set then Get returns the same value.
func TestSetAndGet(t *testing.T) {
	useMemoryKeyring(t)

	const key = "sk_live_test1234"
	require.NoError(t, keychain.Set(key))

	got, migrated, err := keychain.Get()
	require.NoError(t, err)
	assert.Equal(t, key, got)
	assert.False(t, migrated, "Set writes to ServiceNameNew directly: migrated must be false")
}

// TestSet_Overwrite verifies that a second Set() replaces the first.
func TestSet_Overwrite(t *testing.T) {
	useMemoryKeyring(t)

	require.NoError(t, keychain.Set("sk_live_first"))
	require.NoError(t, keychain.Set("sk_live_second"))

	got, _, err := keychain.Get()
	require.NoError(t, err)
	assert.Equal(t, "sk_live_second", got)
}

// ── Scenario 2: legacy entry present (upgrade path from vaultmtg→hollowmark) ─

// TestGet_LegacyEntryPresent_CopiedForward verifies that when only the legacy
// service name (com.vaultmtg.daemon) has an entry, Get() returns the key,
// copies it to ServiceNameNew (com.hollowmark.daemon), retains the old entry,
// and signals migrated=true.
func TestGet_LegacyEntryPresent_CopiedForward(t *testing.T) {
	useMemoryKeyring(t)

	const wantKey = "sk_live_legacykey"
	// Seed only the legacy entry — simulating an upgrade from the vaultmtg daemon.
	require.NoError(t, keyring.Set(keychain.ServiceNameLegacy, keychain.AccountKey, wantKey))

	got, migrated, err := keychain.Get()
	require.NoError(t, err, "Get() must succeed when only legacy entry is present")
	assert.Equal(t, wantKey, got, "Get() must return the legacy key")
	assert.True(t, migrated, "Get() must signal migrated=true when copy-forward ran")

	// ── Copy-forward assertion ────────────────────────────────────────────────
	copiedVal, copyErr := keyring.Get(keychain.ServiceNameNew, keychain.AccountKey)
	require.NoError(t, copyErr, "legacy key must have been copied to ServiceNameNew")
	assert.Equal(t, wantKey, copiedVal, "copied value must equal the original legacy key")

	// ── Retention assertion ───────────────────────────────────────────────────
	legacyVal, legacyErr := keyring.Get(keychain.ServiceNameLegacy, keychain.AccountKey)
	require.NoError(t, legacyErr, "legacy entry must be retained after migration (not deleted)")
	assert.Equal(t, wantKey, legacyVal, "retained legacy entry must be unchanged")
}

// TestGet_BothEntriesPresent_NoOp verifies that when both the new and legacy
// entries are present, Get() returns the new entry immediately with migrated=false
// and does NOT re-run the copy-forward (idempotency).
func TestGet_BothEntriesPresent_NoOp(t *testing.T) {
	useMemoryKeyring(t)

	const newKey = "sk_live_already_migrated"
	const legacyKey = "sk_live_old_vaultmtg_key"

	require.NoError(t, keyring.Set(keychain.ServiceNameNew, keychain.AccountKey, newKey))
	require.NoError(t, keyring.Set(keychain.ServiceNameLegacy, keychain.AccountKey, legacyKey))

	got, migrated, err := keychain.Get()
	require.NoError(t, err)
	assert.Equal(t, newKey, got, "Get() must return ServiceNameNew entry when both present")
	assert.False(t, migrated, "both entries present: migrated must be false (idempotent)")

	// Legacy entry must be untouched.
	legacyVal, legacyErr := keyring.Get(keychain.ServiceNameLegacy, keychain.AccountKey)
	require.NoError(t, legacyErr, "legacy entry must still be present")
	assert.Equal(t, legacyKey, legacyVal, "legacy entry must not be modified when new entry already present")
}

// TestGet_LegacyPresent_SubsequentCallHitsNew verifies that a second Get() call
// after the copy-forward reads from ServiceNameNew, not from legacy.
// This proves the copy-forward is effective and persistent within the same mock store.
func TestGet_LegacyPresent_SubsequentCallHitsNew(t *testing.T) {
	useMemoryKeyring(t)

	const wantKey = "sk_live_subseqcall"
	require.NoError(t, keyring.Set(keychain.ServiceNameLegacy, keychain.AccountKey, wantKey))

	// First call triggers migration.
	_, migrated1, err := keychain.Get()
	require.NoError(t, err)
	assert.True(t, migrated1, "first call with only legacy entry must set migrated=true")

	// Remove the legacy entry to confirm subsequent reads come from ServiceNameNew.
	require.NoError(t, keyring.Delete(keychain.ServiceNameLegacy, keychain.AccountKey))

	got, migrated2, err := keychain.Get()
	require.NoError(t, err)
	assert.Equal(t, wantKey, got)
	assert.False(t, migrated2, "second call hits new entry directly: migrated must be false")
}

// ── Scenario 3: neither entry present ────────────────────────────────────────

// TestGet_NotFound verifies that ErrNotFound is returned when no entry exists
// under either service name (fresh install), and migrated=false.
func TestGet_NotFound(t *testing.T) {
	useMemoryKeyring(t)
	_, migrated, err := keychain.Get()
	assert.ErrorIs(t, err, keychain.ErrNotFound)
	assert.False(t, migrated, "ErrNotFound path must return migrated=false")
}

// ── Scenario 4: neither entry present (via global mock error) ────────────────

// TestGet_NeitherEntryPresent_GlobalMockError verifies the neither-entry-present
// branch when go-keyring's MockInitWithError(ErrNotFound) is used. Because that
// mock applies the same error to every keyring call, both Get(ServiceNameNew)
// and Get(ServiceNameLegacy) return ErrNotFound — which routes through the
// "neither present" branch in keychain.Get() and returns ErrNotFound.
//
// This is intentionally distinct from TestGet_NotFound (which uses the
// in-memory mock and an empty store). It also distinct from
// TestGet_CorruptedLegacyEntry (in keychain_internal_test.go), which uses a
// per-service-name seam to truly exercise the corrupted-legacy branch — the
// branch this test was previously mis-claimed to cover (#2255).
func TestGet_NeitherEntryPresent_GlobalMockError(t *testing.T) {
	// MockInitWithError makes ALL keyring operations return the given error.
	keyring.MockInitWithError(keyring.ErrNotFound)
	t.Cleanup(func() { keyring.MockInitWithError(nil) })

	_, migrated, err := keychain.Get()
	assert.ErrorIs(t, err, keychain.ErrNotFound,
		"both keyring ops returning ErrNotFound must yield keychain.ErrNotFound")
	assert.False(t, migrated, "neither-entry-present path must return migrated=false")
}

// ── Delete tests ──────────────────────────────────────────────────────────────

// TestDelete_Existing verifies that Delete() removes the ServiceNameNew entry.
func TestDelete_Existing(t *testing.T) {
	useMemoryKeyring(t)

	require.NoError(t, keychain.Set("sk_live_todelete"))
	require.NoError(t, keychain.Delete())

	_, _, err := keychain.Get()
	// After Delete, the new entry is gone.  If no legacy entry exists either
	// this should be ErrNotFound.
	assert.ErrorIs(t, err, keychain.ErrNotFound)
}

// TestDelete_Idempotent verifies that Delete() on an empty keychain returns nil.
func TestDelete_Idempotent(t *testing.T) {
	useMemoryKeyring(t)
	assert.NoError(t, keychain.Delete())
}

// TestDelete_DoesNotRemoveLegacy verifies that Delete() only removes ServiceNameNew
// and leaves the legacy entry intact (important for downgrade safety).
func TestDelete_DoesNotRemoveLegacy(t *testing.T) {
	useMemoryKeyring(t)

	const legacyKey = "sk_live_legacyretained"
	require.NoError(t, keyring.Set(keychain.ServiceNameLegacy, keychain.AccountKey, legacyKey))
	require.NoError(t, keychain.Set("sk_live_new"))

	require.NoError(t, keychain.Delete())

	// Legacy entry must still be present.
	legacyVal, err := keyring.Get(keychain.ServiceNameLegacy, keychain.AccountKey)
	require.NoError(t, err, "legacy entry must survive Delete()")
	assert.Equal(t, legacyKey, legacyVal)
}

// ── GetForService / SetForService (ADR-049 Ticket 2) ─────────────────────────

// TestSetForService_UsesPassedServiceName verifies that SetForService writes to
// the service name passed in rather than the package-default ServiceNameNew.
func TestSetForService_UsesPassedServiceName(t *testing.T) {
	useMemoryKeyring(t)

	const service = "com.hollowmark.daemon.staging"
	const wantKey = "sk_live_staging_key"

	require.NoError(t, keychain.SetForService(service, wantKey))

	// The value must be readable under the passed service name.
	got, err := keyring.Get(service, keychain.AccountKey)
	require.NoError(t, err)
	assert.Equal(t, wantKey, got)

	// ServiceNameNew must be untouched.
	_, newErr := keyring.Get(keychain.ServiceNameNew, keychain.AccountKey)
	assert.ErrorIs(t, newErr, keyring.ErrNotFound,
		"SetForService must not write to ServiceNameNew when a different service is passed")
}

// TestGetForService_UsesPassedServiceName verifies that GetForService reads from
// the service name passed in and not the package-default ServiceNameNew.
func TestGetForService_UsesPassedServiceName(t *testing.T) {
	useMemoryKeyring(t)

	const service = "com.hollowmark.daemon.staging"
	const wantKey = "sk_live_staging_read"

	// Seed the staging slot, leaving ServiceNameNew empty.
	require.NoError(t, keyring.Set(service, keychain.AccountKey, wantKey))

	got, err := keychain.GetForService(service)
	require.NoError(t, err)
	assert.Equal(t, wantKey, got)
}

// TestGetForService_NotFound verifies that GetForService returns ErrNotFound when
// the named service has no entry.
func TestGetForService_NotFound(t *testing.T) {
	useMemoryKeyring(t)
	_, err := keychain.GetForService("com.vaultmtg.daemon.staging")
	assert.ErrorIs(t, err, keychain.ErrNotFound)
}

// TestGetForService_RoundTrip verifies that SetForService + GetForService
// constitutes a correct round-trip for an arbitrary service name.
func TestGetForService_RoundTrip(t *testing.T) {
	useMemoryKeyring(t)

	const service = "com.hollowmark.daemon.staging"
	const key = "sk_live_roundtrip"

	require.NoError(t, keychain.SetForService(service, key))
	got, err := keychain.GetForService(service)
	require.NoError(t, err)
	assert.Equal(t, key, got)
}

// TestSetForService_Overwrite verifies that a second SetForService call with the
// same service name replaces the first entry.
func TestSetForService_Overwrite(t *testing.T) {
	useMemoryKeyring(t)

	const service = "com.hollowmark.daemon.staging"
	require.NoError(t, keychain.SetForService(service, "sk_live_first"))
	require.NoError(t, keychain.SetForService(service, "sk_live_second"))

	got, err := keychain.GetForService(service)
	require.NoError(t, err)
	assert.Equal(t, "sk_live_second", got)
}

// TestChannelSlotIsolation is the FF-7 unit-level assertion: stable and staging
// keychain service names must be distinct (ADR-049 §concurrent dual-run invariant).
// With both daemons running simultaneously, each daemon reads/writes its own
// slot and cannot overwrite the other's API key.
func TestChannelSlotIsolation(t *testing.T) {
	useMemoryKeyring(t)

	const stableService = keychain.ServiceNameNew // "com.hollowmark.daemon"
	const stagingService = "com.hollowmark.daemon.staging"

	const stableKey = "sk_live_stable_key"
	const stagingKey = "sk_live_staging_key"

	// Write distinct keys to each slot.
	require.NoError(t, keychain.SetForService(stableService, stableKey))
	require.NoError(t, keychain.SetForService(stagingService, stagingKey))

	// FF-7 assertion: each slot holds its own key — no cross-contamination.
	gotStable, err := keychain.GetForService(stableService)
	require.NoError(t, err)
	assert.Equal(t, stableKey, gotStable, "FF-7: stable slot must not be overwritten by staging write")

	gotStaging, err := keychain.GetForService(stagingService)
	require.NoError(t, err)
	assert.Equal(t, stagingKey, gotStaging, "FF-7: staging slot must not be overwritten by stable write")

	// A write to the staging slot must not affect the stable slot.
	require.NoError(t, keychain.SetForService(stagingService, "sk_live_staging_updated"))
	gotStableAfterUpdate, err := keychain.GetForService(stableService)
	require.NoError(t, err)
	assert.Equal(t, stableKey, gotStableAfterUpdate,
		"FF-7: staging write must not modify the stable keychain slot")
}
