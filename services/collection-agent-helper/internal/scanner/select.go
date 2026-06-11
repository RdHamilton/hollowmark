package scanner

import "fmt"

// DriftToken is the canary trigger string. server.go's drift log line and the
// T3 CloudWatch metric filter pattern-match on this exact string — do not
// rename it without updating the alarm filter and collection-canary.yml.
const DriftToken = "COLLECTION_SCAN_DRIFT"

// Sanity band for the extracted collection (hollowmark-tickets#1285).
// A result outside the hard band is treated as scanner drift and FAILS LOUDLY
// (DriftToken error) instead of silently returning a wrong/empty collection.
//
// Bounds per Prof's player-value consult (#1285 comment 4684603354):
//   - MinSaneCollection (hard floor): any real post-new-player-experience
//     account sits ~300+ distinct grpIds; a best candidate below 250 is a
//     stray dictionary, not the collection.
//   - SoftWarnCollection (soft signal): an unusually-large-but-valid
//     collection. Telemetry warning only — never a hard error. Selection
//     .Warning is populated and callers log it WITHOUT DriftToken so the
//     CloudWatch alarm does not page.
//   - MaxSaneCollection (hard ceiling): beyond any plausible collection —
//     a wrong-region scan. Hard DriftToken error.
//
// Reference point: a veteran near-complete collection measured 19,263 unique
// grpIds on 2026-06-11 (MTGA 2026.59.30).
const (
	MinSaneCollection  = 250
	SoftWarnCollection = 50_000
	MaxSaneCollection  = 100_000
)

// NPEGrantGrpIDCeiling is the highest grpId observed in the MTGA
// NPE-granted-cards dictionary (regions 161/262 in the 2026-06-11 dump,
// MTGA 2026.59.30, one veteran account — see collection_signatures.go
// entry 20260611-002).
//
// Derivation: the observed catalog max grpId is 106,219. MTGA assigns
// sequential integer grpIds to cards; modern sets post-Alchemy (~2021–2022)
// use grpIds in the 200k–1.1M range. The NPE pool was built against the
// paper-card pool only and has not been extended to Alchemy digital cards.
// 150,000 sits in the verified gap between the observed catalog ceiling
// (106,219) and the modern band floor (~200k), providing generous headroom
// for small NPE pool additions without requiring a re-derivation.
//
// INVARIANT: every signature re-derivation per ADR-040 §G4 MUST re-verify
// the NPE-dict profile (max grpId ≤ ceiling, all values ≤ 4). If a future
// MTGA patch adds NPE grants for cards with grpIds > 150k, the dict becomes
// Layer-1-marked (positive-marker path), which is safe — but this constant
// must be updated so the containment tie-break (Layer 2) remains correct.
//
// Source: hollowmark-tickets#1287; Ray's binding ruling M3 (2026-06-11).
const NPEGrantGrpIDCeiling = 150_000

// containmentRatio95 is the minimum fraction of candidate A's keys that must
// be present in candidate B's keyset for A to be considered "≥95% contained"
// in B (Layer 2 tie-break).
const containmentRatio95 = 0.95

// RegionScan is one memory region's scan result: every valid
// Dictionary<int,int> entry (grpId → quantity) recovered by ScanDictEntries.
type RegionScan struct {
	Addr    uint64
	Size    uint64
	Entries map[int]int
}

// Selection is the region chosen as the collection dictionary.
type Selection struct {
	Addr    uint64
	Entries map[int]int
	// RunnerUpEntries is the entry count of the second-best region (0 if none).
	// Logged for drift triage: a runner-up close to the winner means the
	// discriminator is weak and the next MTGA patch deserves scrutiny.
	RunnerUpEntries int
	// Warning is non-empty when the result is valid but unusual (above
	// SoftWarnCollection). Callers should log it for telemetry. It never
	// contains DriftToken — soft signals must not trip the hard alarm.
	Warning string
}

// isNPEDict reports whether a region's entry set matches the structural profile
// of the MTGA NPE-granted-cards dictionary: all values capped at exactly 4
// (the constructed-deck 4-copy limit applied at grant time) and all grpIds
// within the paper-card band (≤ NPEGrantGrpIDCeiling).
//
// A region that passes this filter is a catalog-profile candidate — it is not
// immediately a real collection. The caller handles it via the layered
// discriminator logic in SelectCollection.
//
// Two conditions must both hold (per Ray's binding ruling M4, hollowmark-tickets#1287):
//  1. All values ≤ 4  — the NPE pool uses constructed-max as the copy cap.
//  2. No grpId > NPEGrantGrpIDCeiling — the NPE pool predates Alchemy/digital cards.
//
// Either condition alone would false-reject real collections: all-values-≤4 alone
// rejects fresh accounts in their first weeks; no-modern-grpIds alone rejects
// paper-only veteran collections. Only the two-condition AND is safe as an exclusion
// marker.
func isNPEDict(entries map[int]int) bool {
	if len(entries) == 0 {
		return false
	}
	for k, v := range entries {
		if v > 4 {
			return false // value > 4 → real collection (Layer 1 positive marker)
		}
		if k > NPEGrantGrpIDCeiling {
			return false // modern grpId → real collection (Layer 1 positive marker)
		}
	}
	return true
}

// containedFraction returns the fraction of a's keys that are present in b.
// Returns 0 for an empty a.
func containedFraction(a, b map[int]int) float64 {
	if len(a) == 0 {
		return 0
	}
	overlap := 0
	for k := range a {
		if _, ok := b[k]; ok {
			overlap++
		}
	}
	return float64(overlap) / float64(len(a))
}

// SelectCollection picks the collection dictionary from per-region scan
// results using a three-layer discriminator (hollowmark-tickets#1287):
//
// Layer 1 — positive markers (strong accept):
//
//	A candidate with any value > 4 OR any grpId > NPEGrantGrpIDCeiling cannot be
//	the NPE dict — it is a real collection. Among marked candidates, the densest
//	wins (existing densest-pick logic). Catalog-profile (isNPEDict) candidates are
//	excluded whenever ≥1 marked candidate exists.
//
// Layer 2 — containment tie-break (small-collection fix):
//
//	If NO candidate carries a positive marker, fall back to keyset containment.
//	If candidate A's keyset is ≥95% contained in candidate B's and |A| < |B|
//	strictly, A (the subset) is the real collection — a fresh player's collection
//	is a proper subset of the NPE grant pool. If a unique such subset exists,
//	select it. Multiple distinct subsets or no containment relation → Layer 3.
//
// Layer 3 — fail loud:
//
//	Identical keysets or no containment relationship → DriftToken error. Never
//	silently export a catalog-profile region as the collection under ambiguity.
//
// This replaces the fixed minEntries/maxFillPct acceptance thresholds
// (removed in hollowmark-tickets#1285): absolute thresholds were tuned to one
// client build's heap profile and made every MTGA update a potential silent
// breakage. The densest-valid-region rule is layout-independent; the sanity
// band turns any residual drift into a loud DriftToken error instead of a
// silent empty export.
func SelectCollection(regions []RegionScan) (*Selection, error) {
	// Partition into marked (Layer 1 positive markers) and unmarked (NPE-profile)
	// candidates.
	var marked []*RegionScan
	var unmarked []*RegionScan

	for i := range regions {
		r := &regions[i]
		if len(r.Entries) == 0 {
			continue
		}
		if isNPEDict(r.Entries) {
			unmarked = append(unmarked, r)
		} else {
			marked = append(marked, r)
		}
	}

	// Layer 1: at least one marked candidate — pick the densest among them.
	if len(marked) > 0 {
		best := marked[0]
		runnerUp := 0
		for _, r := range marked[1:] {
			switch {
			case len(r.Entries) > len(best.Entries):
				runnerUp = len(best.Entries)
				best = r
			case len(r.Entries) > runnerUp:
				runnerUp = len(r.Entries)
			}
		}
		// Include unmarked runner-up count if it is larger than the current runner-up
		// (telemetry: warns if the catalog is suspiciously close to the winner).
		for _, r := range unmarked {
			if len(r.Entries) > runnerUp && r != best {
				runnerUp = len(r.Entries)
			}
		}
		return buildSelection(best, runnerUp)
	}

	// Layer 2: no marked candidate — use containment tie-break.
	if len(unmarked) == 0 {
		return nil, fmt.Errorf("%s: no Dictionary<int,int> candidate entries in any of %d regions — "+
			"probable Unity layout change (H2); re-derive per ADR-040 §G4", DriftToken, len(regions))
	}
	if len(unmarked) == 1 {
		// Exactly one catalog-profile candidate and no marked candidates. This is
		// an unusual state (only the NPE pool visible, no real collection); treat it
		// as drift rather than silently exporting the grant pool.
		return nil, fmt.Errorf("%s: only NPE-profile candidate found (0x%x, %d entries), "+
			"no marked real-collection region — probable scanner drift; re-derive per ADR-040 §G4",
			DriftToken, unmarked[0].Addr, len(unmarked[0].Entries))
	}

	// Check for identical keysets → Layer 3.
	if allKeysetsEqual(unmarked) {
		return nil, fmt.Errorf("%s: all %d NPE-profile candidates have identical keysets "+
			"(NPE-dict duplication, e.g. regions 161≡262) — no real collection candidate; "+
			"re-derive per ADR-040 §G4", DriftToken, len(unmarked))
	}

	// Find a unique subset candidate: the smallest candidate whose keyset is
	// ≥95% contained in a strictly-larger candidate.
	subset := findUniqueSubset(unmarked)
	if subset == nil {
		return nil, fmt.Errorf("%s: %d NPE-profile candidates with no ≥95%% containment relationship — "+
			"ambiguous region selection; re-derive per ADR-040 §G4", DriftToken, len(unmarked))
	}

	// Runner-up: the largest other candidate (the superset, i.e. the NPE pool).
	runnerUp := 0
	for _, r := range unmarked {
		if r != subset && len(r.Entries) > runnerUp {
			runnerUp = len(r.Entries)
		}
	}
	return buildSelection(subset, runnerUp)
}

// buildSelection validates the selected region against the sanity band and
// returns a Selection or a DriftToken error.
func buildSelection(best *RegionScan, runnerUp int) (*Selection, error) {
	n := len(best.Entries)
	if n < MinSaneCollection || n > MaxSaneCollection {
		return nil, fmt.Errorf("%s: best candidate region 0x%x has %d entries, outside sanity band [%d, %d] — "+
			"probable scanner drift; re-derive per ADR-040 §G4", DriftToken, best.Addr, n, MinSaneCollection, MaxSaneCollection)
	}
	sel := &Selection{Addr: best.Addr, Entries: best.Entries, RunnerUpEntries: runnerUp}
	if n > SoftWarnCollection {
		sel.Warning = fmt.Sprintf("collection unusually large: %d entries exceeds soft-warn threshold %d "+
			"(region 0x%x) — valid result, telemetry review advised", n, SoftWarnCollection, best.Addr)
	}
	return sel, nil
}

// allKeysetsEqual returns true if all candidates have keysets that are
// byte-for-byte identical (same keys, same count).
func allKeysetsEqual(rs []*RegionScan) bool {
	if len(rs) < 2 {
		return false
	}
	ref := rs[0]
	for _, r := range rs[1:] {
		if len(r.Entries) != len(ref.Entries) {
			return false
		}
		for k := range ref.Entries {
			if _, ok := r.Entries[k]; !ok {
				return false
			}
		}
	}
	return true
}

// findUniqueSubset returns the unique candidate r such that r's keyset is ≥95%
// contained in at least one strictly-larger candidate's keyset AND |r| is the
// smallest among all such subsets. Returns nil if no unique subset exists or if
// multiple distinct subsets of the same minimal size are found.
func findUniqueSubset(rs []*RegionScan) *RegionScan {
	var bestSubset *RegionScan
	ambiguous := false

	for _, candidate := range rs {
		// Check if candidate is ≥95% contained in any strictly-larger region.
		isSubset := false
		for _, other := range rs {
			if other == candidate {
				continue
			}
			if len(other.Entries) <= len(candidate.Entries) {
				continue // other is not strictly larger
			}
			if containedFraction(candidate.Entries, other.Entries) >= containmentRatio95 {
				isSubset = true
				break
			}
		}
		if !isSubset {
			continue
		}
		// candidate qualifies as a subset.
		switch {
		case bestSubset == nil:
			bestSubset = candidate
		case len(candidate.Entries) < len(bestSubset.Entries):
			bestSubset = candidate
			ambiguous = false
		case len(candidate.Entries) == len(bestSubset.Entries):
			ambiguous = true
		}
	}
	if ambiguous {
		return nil
	}
	return bestSubset
}
