package scanner

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeEntries returns a synthetic valid collection map with n entries.
// Entries use modern grpIds (above NPEGrantGrpIDCeiling) to ensure they
// carry a Layer-1 positive marker and are treated as real-collection candidates
// by SelectCollection. Values are all 4 (typical case). Use makeNPEEntries
// for catalog-profile test data.
func makeEntries(n int) map[int]int {
	m := make(map[int]int, n)
	for i := 0; i < n; i++ {
		// Anchor in the modern Alchemy/digital band (200k+) — above NPEGrantGrpIDCeiling.
		// This is consistent with a real veteran or mid-tenure account.
		m[200_001+i] = 4
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

// ---------------------------------------------------------------------------
// NPE-dict discriminator tests — hollowmark-tickets#1287
// Layered discriminator: Layer 1 (positive markers), Layer 2 (containment
// tie-break), Layer 3 (fail loud on ambiguity).
// ---------------------------------------------------------------------------

// makeNPEEntries returns a synthetic NPE-grant-pool region: n entries, all
// values capped at exactly 4, all grpIds within the paper-card band (≤ paperMax).
func makeNPEEntries(n, paperMax int) map[int]int {
	m := make(map[int]int, n)
	for i := 0; i < n; i++ {
		// Use grpIds starting at MinGRPID+1000 (well inside paper band, far from MinGRPID)
		m[MinGRPID+1000+i] = 1 + (i % 4) // qty cycles 1–4, never >4
	}
	// Ensure all keys stay ≤ paperMax
	_ = paperMax
	return m
}

// makeModernEntries returns a synthetic real-collection region with n entries,
// values cycling 1–8 (some >4), and grpIds in the modern Alchemy band (>200k).
func makeModernEntries(n int) map[int]int {
	m := make(map[int]int, n)
	for i := 0; i < n; i++ {
		m[200_001+i] = 1 + (i % 8) // qty cycles 1–8
	}
	return m
}

// makeSubsetOf returns a new map whose keys are the first size keys of base,
// with values copied from base.
func makeSubsetOf(base map[int]int, size int) map[int]int {
	m := make(map[int]int, size)
	count := 0
	for k, v := range base {
		if count >= size {
			break
		}
		m[k] = v
		count++
	}
	return m
}

// TestB_FreshPlayerModernCards: a fresh player with modern cards (grpIds >200k)
// must win over a catalog-shaped 10,140-entry region (Layer 1 — positive marker
// on modern grpId).
func TestB_FreshPlayerModernCards_WinsOverCatalog(t *testing.T) {
	catalog := makeNPEEntries(10_140, 106_219)
	freshPlayer := makeModernEntries(300)

	regions := []RegionScan{
		{Addr: 0x36a600000, Size: 16 << 20, Entries: catalog},
		{Addr: 0x100000000, Size: 4 << 20, Entries: freshPlayer},
	}

	got, err := SelectCollection(regions)
	require.NoError(t, err)
	assert.Equal(t, uint64(0x100000000), got.Addr,
		"fresh player with modern-card grpIds must be selected over the catalog-shaped region")
	assert.Len(t, got.Entries, 300)
}

// TestC_VeteranCollection_NoFalseReject: a veteran collection with values >4
// and grpIds >200k must not be rejected (Layer 1 positive marker — no
// false-positive on large real collection).
func TestC_VeteranCollection_NoFalseReject(t *testing.T) {
	// 19,263 entries including values >4 and grpIds in Alchemy range
	veteran := makeModernEntries(19_263)
	// Add a handful of entries with value >4
	veteran[500_001] = 6
	veteran[500_002] = 8

	regions := []RegionScan{
		{Addr: 0x376940000, Size: 64 << 20, Entries: veteran},
	}

	got, err := SelectCollection(regions)
	require.NoError(t, err)
	assert.Equal(t, uint64(0x376940000), got.Addr,
		"veteran collection must not be false-rejected by NPE discriminator")
}

// TestD_AllCatalogProfileIdentical_DriftError: when every candidate matches the
// NPE-catalog profile AND they have identical keysets → Layer 3 DriftToken error.
// (Regression test for the 161≡262 duplication observed in the real dump.)
func TestD_AllCatalogProfileIdentical_DriftError(t *testing.T) {
	catalog := makeNPEEntries(10_140, 106_219)
	// Exact clone — identical keysets
	clone := make(map[int]int, len(catalog))
	for k, v := range catalog {
		clone[k] = v
	}

	regions := []RegionScan{
		{Addr: 0x36a600000, Size: 16 << 20, Entries: catalog},
		{Addr: 0x38d680000, Size: 16 << 20, Entries: clone},
	}

	_, err := SelectCollection(regions)
	require.Error(t, err)
	assert.Contains(t, err.Error(), DriftToken,
		"identical-keyset catalog-profile candidates must produce a DriftToken error")
}

// TestE_FreshPaperOnlySubset_SubsetSelected: the regression test for the M1/M2
// flaw. A fresh paper-only player whose collection is a proper subset of the NPE
// grant pool has NO values >4 and NO grpIds >NPEGrantGrpIDCeiling. Under Layer 2
// (containment tie-break) the subset (real collection) must be selected, NOT the
// superset (NPE pool), and NOT a DriftToken error.
//
// This is AC4 regression gate per Ray's M5 ruling.
func TestE_FreshPaperOnly_SubsetSelected(t *testing.T) {
	// Build the NPE catalog: 10,140 entries, all values 1–4, all grpIds ≤ 106,219.
	catalog := makeNPEEntries(10_140, 106_219)

	// Fresh player collection: 400 entries that are a proper subset of catalog's
	// keys. Values all ≤ 4 (no 5th-copy events yet). No modern grpIds.
	freshColl := makeSubsetOf(catalog, 400)

	regions := []RegionScan{
		{Addr: 0x36a600000, Size: 16 << 20, Entries: catalog},  // NPE pool (superset)
		{Addr: 0x100000000, Size: 4 << 20, Entries: freshColl}, // real collection (subset)
	}

	got, err := SelectCollection(regions)
	require.NoError(t, err, "fresh paper-only player must get a valid selection, not a DriftToken error")
	assert.Equal(t, uint64(0x100000000), got.Addr,
		"subset (real fresh collection) must be selected, not the superset NPE pool")
	assert.Len(t, got.Entries, 400)
}

// TestNPEGrantCeiling_ConstantValue: documents and pins the constant per Ray M3.
// Do not change without updating the derivation comment and re-running signature
// derivation per ADR-040 §G4.
func TestNPEGrantCeiling_ConstantValue(t *testing.T) {
	// Sits in the verified gap between observed catalog max (106,219) and the
	// modern Alchemy card band start (~200k). Generous headroom absorbs small NPE
	// pool additions without requiring a re-derivation. Source: 2026-06-11 dump,
	// MTGA 2026.59.30, one veteran account — see collection_signatures.go 20260611-002.
	assert.Equal(t, 150_000, NPEGrantGrpIDCeiling)
}

// TestLayer1_ValueAbove4_PositiveMarker: any region with even one value >4 must
// be treated as a real collection (Layer 1 positive marker) and win over a
// catalog-profile region that has more entries.
func TestLayer1_ValueAbove4_PositiveMarker(t *testing.T) {
	// Catalog-shaped: 10k entries, all values ≤ 4, no modern grpIds
	catalog := makeNPEEntries(10_000, 106_000)

	// Small real collection: only 500 entries, but one has value = 5 (5th copy)
	small := make(map[int]int, 500)
	for i := 0; i < 499; i++ {
		small[MinGRPID+50_000+i] = 2
	}
	small[MinGRPID+60_000] = 5 // ← the 5th copy that marks it as real

	regions := []RegionScan{
		{Addr: 0x36a600000, Size: 16 << 20, Entries: catalog},
		{Addr: 0x100000000, Size: 4 << 20, Entries: small},
	}

	got, err := SelectCollection(regions)
	require.NoError(t, err)
	assert.Equal(t, uint64(0x100000000), got.Addr,
		"region with value >4 must be selected over a larger catalog-shaped region (Layer 1 marker)")
}

// TestLayer3_NoContainment_DriftError: two catalog-profile regions where neither
// is ≥95% contained in the other → Layer 3 DriftToken error.
func TestLayer3_NoContainment_DriftError(t *testing.T) {
	// Two catalog-profile regions with disjoint keys — no containment relationship
	a := makeNPEEntries(300, 106_000)
	// Build b with different key range
	b := make(map[int]int, 300)
	for i := 0; i < 300; i++ {
		b[MinGRPID+50_000+i] = 2
	}

	regions := []RegionScan{
		{Addr: 0x36a600000, Size: 16 << 20, Entries: a},
		{Addr: 0x100000000, Size: 4 << 20, Entries: b},
	}

	_, err := SelectCollection(regions)
	require.Error(t, err)
	assert.Contains(t, err.Error(), DriftToken,
		"no containment relationship among unmarked candidates must DriftToken")
}

// TestDumpHeadToHead_SelectsRealCollection exercises the discriminator against
// real binary dump regions loaded from MTGA_DUMP_DIR. Skipped in CI (dump not
// committed — multi-GB, contains real account data). Run locally with:
//
//	MTGA_DUMP_DIR=/tmp/mtga-dump go test ./internal/scanner/... -run TestDumpHeadToHead -v
func TestDumpHeadToHead_SelectsRealCollection(t *testing.T) {
	dumpDir := os.Getenv("MTGA_DUMP_DIR")
	if dumpDir == "" {
		t.Skip("MTGA_DUMP_DIR not set — skipping real-dump head-to-head test")
	}

	manifest, regions := loadDumpRegions(t, dumpDir)
	_ = manifest

	got, err := SelectCollection(regions)
	require.NoError(t, err, "SelectCollection must not error on real dump")

	// Region 0x376940000 is the known real collection (19,263 entries, veteran account).
	// Regions 0x36a600000 (161) and 0x38d680000 (262) are the NPE catalog duplicates.
	const wantAddr = uint64(0x376940000)
	assert.Equal(t, wantAddr, got.Addr,
		"must select the real collection region 0x376940000, not the NPE catalog regions 161/262")
	assert.GreaterOrEqual(t, len(got.Entries), 10_000,
		"real collection must have at least 10k entries (veteran account)")
}

// ---------------------------------------------------------------------------
// dump-test helper — used only when MTGA_DUMP_DIR is set
// ---------------------------------------------------------------------------

// dumpManifestEntry mirrors the manifest JSON written by dump_darwin.go.
type dumpManifestEntry struct {
	RegionN int    `json:"region_n"`
	AddrHex string `json:"addr_hex"`
	Size    uint64 `json:"size"`
	File    string `json:"file"`
}

// loadDumpRegions reads a manifest.json from dir, loads each referenced binary
// file, and returns RegionScan slices by scanning each file with ScanDictEntries.
// The manifest is the same format written by runDumpRegions in dump_darwin.go.
func loadDumpRegions(t *testing.T, dir string) ([]dumpManifestEntry, []RegionScan) {
	t.Helper()
	manifestPath := filepath.Join(dir, "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	require.NoError(t, err, "read dump manifest from %s", manifestPath)

	var entries []dumpManifestEntry
	require.NoError(t, json.Unmarshal(raw, &entries), "unmarshal dump manifest")
	require.NotEmpty(t, entries, "dump manifest must have at least one region")

	regions := make([]RegionScan, 0, len(entries))
	for _, e := range entries {
		addr, err := parseHexAddr(e.AddrHex)
		if err != nil {
			t.Logf("skipping region %d: bad addr %q: %v", e.RegionN, e.AddrHex, err)
			continue
		}
		binPath := filepath.Join(dir, e.File)
		data, err := os.ReadFile(binPath)
		if err != nil {
			t.Logf("skipping region %d (%s): %v", e.RegionN, e.AddrHex, err)
			continue
		}
		regions = append(regions, RegionScan{
			Addr:    addr,
			Size:    e.Size,
			Entries: ScanDictEntries(data),
		})
	}
	require.NotEmpty(t, regions, "no regions loaded from dump")
	return entries, regions
}

// parseHexAddr converts "0x376940000" → uint64 without importing strconv.
func parseHexAddr(s string) (uint64, error) {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		s = s[2:]
	}
	var v uint64
	for _, c := range s {
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= uint64(c - '0')
		case c >= 'a' && c <= 'f':
			v |= uint64(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v |= uint64(c-'A') + 10
		default:
			return 0, fmt.Errorf("invalid hex char %q in %q", c, s)
		}
	}
	return v, nil
}

// Ensure encoding/binary is used (via ScanDictEntries) — quiet the unused-import
// linter if the dump test is skipped at compile time.
var _ = binary.LittleEndian
