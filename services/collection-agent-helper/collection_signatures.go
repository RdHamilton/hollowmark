package main

// knownSignatureVersions is a changelog of all collection-scan signatures.
// Add an entry here whenever re-deriving the scanDictEntries signature or
// tuning the region-filter constants in mem_darwin.go.
//
// Version format: YYYYMMDD-NNN (date of derivation + same-day sequence counter).
//
// Derivation procedure: see ADR-040 §G4 and the comment above
// CollectionSignatureVersion in mem_darwin.go.
var knownSignatureVersions = map[string]string{
	"20260512-001": "MTGA patch 2026-05-12; initial signature (minEntries=500, maxFillPct=3.0, stride=16)",
	"20260529-001": "MTGA patch 2026-05-29; re-derived for v0.3.4 — H1 confirmed; constants unchanged (minEntries=500, maxFillPct=3.0, stride=16); 19114 entries from region 0x389c30000",
	"20260611-001": "MTGA patch 2026.59.30; hollowmark-tickets#1285 — adaptive region selection (densest valid int→int region via scanner.SelectCollection), minEntries/maxFillPct thresholds removed, sanity band hard-floor 250 / soft-warn 50000 / hard-ceiling 100000 (Prof consult, #1285 comment 4684603354) with loud COLLECTION_SCAN_DRIFT on hard violation; stride=16 unchanged; 19263 entries from region 0x376940000",
	// 20260611-002: NPE-dict discriminator — hollowmark-tickets#1287.
	//
	// Observed catalog profile (regions 161/262 of the 2026-06-11 dump, MTGA 2026.59.30,
	// ONE VETERAN ACCOUNT — see M6: "the dict is always 10,140 for every account" is an
	// UNVERIFIED HYPOTHESIS; the discriminator design is correct under both static-pool
	// and per-account-grants hypotheses):
	//   - Entry count: 10,140
	//   - Max grpId:   106,219 (paper-card band only; no Alchemy/digital cards)
	//   - Max value:   4 (hard cap — constructed 4-copy limit applied at grant time)
	//   - Values > 4:  0 entries
	//   - grpIds > 200k: 0 entries
	//   - Relationship to real collection (region 0x376940000): 100% proper subset
	//
	// Discriminator added to scanner.SelectCollection: three-layer selection —
	//   Layer 1 (positive markers): any value >4 or grpId > NPEGrantGrpIDCeiling=150k → real collection.
	//   Layer 2 (containment tie-break): if no marked candidate, the ≥95%-subset is the real collection.
	//   Layer 3 (fail loud): identical keysets or no containment → COLLECTION_SCAN_DRIFT.
	//
	// NPEGrantGrpIDCeiling=150_000 sits in the verified gap between observed catalog max (106,219)
	// and the modern Alchemy band floor (~200k; 8,140 entries >200k in region 0x376940000).
	//
	// ADR-040 §G4 SOP addendum: every signature re-derivation MUST re-verify the NPE-dict
	// profile (max grpId ≤ NPEGrantGrpIDCeiling, all values ≤ 4). If WotC adds NPE grants
	// for cards with grpIds >150k between derivations, the dict becomes Layer-1-marked
	// (caught at re-derivation time). See scanner/select.go NPEGrantGrpIDCeiling constant.
	"20260611-002": "MTGA patch 2026.59.30 (same build as 20260611-001); hollowmark-tickets#1287 — NPE-grant-pool sibling-dict discriminator; observed NPE dict profile: 10140 entries, max grpId 106219, max value 4, 100% subset of real collection; NPEGrantGrpIDCeiling=150000; three-layer discriminator (positive markers → containment tie-break → COLLECTION_SCAN_DRIFT); ADR-040 §G4 SOP: re-verify NPE profile at every re-derivation",
}
