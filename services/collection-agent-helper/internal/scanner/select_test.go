package scanner

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeEntries returns a synthetic valid collection map with n entries,
// keys starting at MinGRPID, all quantities = 4.
func makeEntries(n int) map[int]int {
	m := make(map[int]int, n)
	for i := 0; i < n; i++ {
		m[MinGRPID+i] = 4
	}
	return m
}

func TestCanarySelectCollection_PicksRegionWithMostValidEntries(t *testing.T) {
	regions := []RegionScan{
		{Addr: 0x1000, Size: 16 << 20, Entries: makeEntries(9149)},
		{Addr: 0x2000, Size: 16 << 20, Entries: makeEntries(19263)},
		{Addr: 0x3000, Size: 16 << 20, Entries: makeEntries(11129)},
	}

	got, err := SelectCollection(regions)
	require.NoError(t, err)
	assert.Equal(t, uint64(0x2000), got.Addr)
	assert.Len(t, got.Entries, 19263)
	assert.Equal(t, 11129, got.RunnerUpEntries)
}

func TestCanarySelectCollection_NoFillPctCeiling(t *testing.T) {
	// Regression guard for hollowmark-tickets#1285: a dense region (high fill
	// ratio) must NOT be rejected. The old maxFillPct=3.0 gate is removed.
	// 1 MiB region holding 60k 16-byte slots, 50k valid entries = ~76% fill.
	regions := []RegionScan{
		{Addr: 0x1000, Size: 1 << 20, Entries: makeEntries(50_000)},
	}

	got, err := SelectCollection(regions)
	require.NoError(t, err)
	assert.Len(t, got.Entries, 50_000)
}

func TestCanarySelectCollection_NoRegions_DriftError(t *testing.T) {
	_, err := SelectCollection(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), DriftToken)
}

func TestCanarySelectCollection_AllRegionsEmpty_DriftError(t *testing.T) {
	regions := []RegionScan{
		{Addr: 0x1000, Size: 16 << 20, Entries: map[int]int{}},
		{Addr: 0x2000, Size: 16 << 20, Entries: nil},
	}

	_, err := SelectCollection(regions)
	require.Error(t, err)
	assert.Contains(t, err.Error(), DriftToken)
	// Zero candidates anywhere = likely Unity layout drift (H2) — the error
	// must steer triage there, not at region thresholds.
	assert.Contains(t, err.Error(), "layout")
}

func TestCanarySelectCollection_BelowSanityFloor_DriftError(t *testing.T) {
	regions := []RegionScan{
		{Addr: 0x1000, Size: 16 << 20, Entries: makeEntries(MinSaneCollection - 1)},
	}

	_, err := SelectCollection(regions)
	require.Error(t, err)
	assert.Contains(t, err.Error(), DriftToken)
	assert.Contains(t, err.Error(), fmt.Sprintf("%d", MinSaneCollection-1),
		"error must report the rejected entry count for triage")
}

func TestCanarySelectCollection_AtSanityFloor_OK(t *testing.T) {
	regions := []RegionScan{
		{Addr: 0x1000, Size: 16 << 20, Entries: makeEntries(MinSaneCollection)},
	}

	got, err := SelectCollection(regions)
	require.NoError(t, err)
	assert.Len(t, got.Entries, MinSaneCollection)
}

func TestCanarySanityBand_ProfRecommendedValues(t *testing.T) {
	// Prof's player-value consult (hollowmark-tickets#1285 comment 4684603354):
	// floor 250 (a real post-NPE account sits ~300 distinct grpIds), soft-warn
	// 50k (unusually-large-but-valid collection — telemetry only), hard ceiling
	// 100k (wrong-region scan). Do not change without a fresh Prof consult.
	assert.Equal(t, 250, MinSaneCollection)
	assert.Equal(t, 50_000, SoftWarnCollection)
	assert.Equal(t, 100_000, MaxSaneCollection)
}

func TestCanarySelectCollection_AboveSoftWarn_NoError_WarnsLoudly(t *testing.T) {
	regions := []RegionScan{
		{Addr: 0x1000, Size: 64 << 20, Entries: makeEntries(SoftWarnCollection + 1)},
	}

	got, err := SelectCollection(regions)
	require.NoError(t, err, "above soft-warn but under hard ceiling must NOT hard-error")
	assert.Len(t, got.Entries, SoftWarnCollection+1)
	require.NotEmpty(t, got.Warning, "soft-warn band must produce a telemetry warning")
	assert.Contains(t, got.Warning, fmt.Sprintf("%d", SoftWarnCollection+1),
		"warning must report the entry count")
	assert.NotContains(t, got.Warning, DriftToken,
		"soft warning must NOT carry the hard-alarm token — CloudWatch filter would page on it")
}

func TestCanarySelectCollection_AtSoftWarnBoundary_NoWarning(t *testing.T) {
	regions := []RegionScan{
		{Addr: 0x1000, Size: 64 << 20, Entries: makeEntries(SoftWarnCollection)},
	}

	got, err := SelectCollection(regions)
	require.NoError(t, err)
	assert.Empty(t, got.Warning, "exactly SoftWarnCollection entries is still in the normal band")
}

func TestCanarySelectCollection_NormalResult_NoWarning(t *testing.T) {
	regions := []RegionScan{
		{Addr: 0x1000, Size: 16 << 20, Entries: makeEntries(19_263)},
	}

	got, err := SelectCollection(regions)
	require.NoError(t, err)
	assert.Empty(t, got.Warning)
}

func TestCanarySelectCollection_AboveSanityCeiling_DriftError(t *testing.T) {
	regions := []RegionScan{
		{Addr: 0x1000, Size: 64 << 20, Entries: makeEntries(MaxSaneCollection + 1)},
	}

	_, err := SelectCollection(regions)
	require.Error(t, err)
	assert.Contains(t, err.Error(), DriftToken)
}

func TestCanarySelectCollection_SingleRegion_NoRunnerUp(t *testing.T) {
	regions := []RegionScan{
		{Addr: 0x1000, Size: 16 << 20, Entries: makeEntries(5_000)},
	}

	got, err := SelectCollection(regions)
	require.NoError(t, err)
	assert.Equal(t, 0, got.RunnerUpEntries)
}

func TestCanaryDriftTokenMatchesCloudWatchFilter(t *testing.T) {
	// server.go's drift log line and the T3 CloudWatch metric filter both
	// pattern-match this exact string. Do not change it.
	assert.Equal(t, "COLLECTION_SCAN_DRIFT", DriftToken)
}

func TestCanarySelectCollection_ErrorMentionsSignatureProcedure(t *testing.T) {
	_, err := SelectCollection(nil)
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "ADR-040"),
		"drift errors must point on-call at the re-derivation procedure")
}
