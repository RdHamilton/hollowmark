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
}
