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

// SelectCollection picks the collection dictionary from per-region scan
// results: the region with the MOST valid int→int entries (keys already
// validated by ScanDictEntries to be in the grpId range with sane quantities).
//
// This replaces the fixed minEntries/maxFillPct acceptance thresholds
// (removed in hollowmark-tickets#1285): absolute thresholds were tuned to one
// client build's heap profile and made every MTGA update a potential silent
// breakage. The densest-valid-region rule is layout-independent; the sanity
// band turns any residual drift into a loud DriftToken error instead of a
// silent empty export.
func SelectCollection(regions []RegionScan) (*Selection, error) {
	var best *RegionScan
	runnerUp := 0
	for i := range regions {
		r := &regions[i]
		if len(r.Entries) == 0 {
			continue
		}
		switch {
		case best == nil:
			best = r
		case len(r.Entries) > len(best.Entries):
			runnerUp = len(best.Entries)
			best = r
		case len(r.Entries) > runnerUp:
			runnerUp = len(r.Entries)
		}
	}

	if best == nil {
		return nil, fmt.Errorf("%s: no Dictionary<int,int> candidate entries in any of %d regions — "+
			"probable Unity layout change (H2); re-derive per ADR-040 §G4", DriftToken, len(regions))
	}
	if n := len(best.Entries); n < MinSaneCollection || n > MaxSaneCollection {
		return nil, fmt.Errorf("%s: best candidate region 0x%x has %d entries, outside sanity band [%d, %d] — "+
			"probable scanner drift; re-derive per ADR-040 §G4", DriftToken, best.Addr, n, MinSaneCollection, MaxSaneCollection)
	}

	sel := &Selection{Addr: best.Addr, Entries: best.Entries, RunnerUpEntries: runnerUp}
	if n := len(best.Entries); n > SoftWarnCollection {
		sel.Warning = fmt.Sprintf("collection unusually large: %d entries exceeds soft-warn threshold %d "+
			"(region 0x%x) — valid result, telemetry review advised", n, SoftWarnCollection, best.Addr)
	}
	return sel, nil
}
