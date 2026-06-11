// Command analyze_dump runs scanDictEntries against each region .bin captured by
// `collection-helper --dump-regions` and prints per-region entry counts, fillPct,
// and a sample of GRP IDs. Use this to resolve H1 vs H2 without live MTGA contact.
//
// Usage:
//
//	go run ./cmd/analyze_dump <outdir> <outdir>/manifest.json
//
// The verdict is produced by running the dump through scanner.SelectCollection —
// the EXACT selection logic production scanProcess uses (hollowmark-tickets#1285;
// the previous canned-text verdict asserted mechanisms production didn't have).
// H2 (Unity layout drift): no region shows any matches — inspect bytes around a known GRP ID.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/RdHamilton/hollowmark/services/collection-agent-helper/internal/scanner"
)

type manifestEntry struct {
	RegionN int    `json:"region_n"`
	AddrHex string `json:"addr_hex"`
	Size    uint64 `json:"size_bytes"`
	File    string `json:"file"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: analyze_dump <outdir> <manifest.json>")
		os.Exit(1)
	}
	outdir := os.Args[1]
	manifestPath := os.Args[2]

	mf, err := os.Open(manifestPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open manifest: %v\n", err)
		os.Exit(1)
	}
	var entries []manifestEntry
	if err := json.NewDecoder(mf).Decode(&entries); err != nil {
		_ = mf.Close()
		fmt.Fprintf(os.Stderr, "decode manifest: %v\n", err)
		os.Exit(1)
	}
	_ = mf.Close()

	fmt.Printf("%-6s  %-18s  %-12s  %-8s  %-8s  %s\n",
		"REGION", "ADDR", "SIZE_MB", "ENTRIES", "FILL%", "SAMPLE_GRP_IDS")
	fmt.Println("------  ------------------  ------------  --------  --------  -------------------------")

	var totalEntries int
	var bestRegion *manifestEntry
	var bestEntries map[int]int
	var scans []scanner.RegionScan

	for i := range entries {
		e := &entries[i]
		binPath := filepath.Join(outdir, e.File)
		data, err := os.ReadFile(binPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "read %s: %v\n", binPath, err)
			continue
		}

		got := scanner.ScanDictEntries(data)
		sizeMB := float64(e.Size) / (1024 * 1024)

		var fillPct float64
		if e.Size >= 16 {
			fillPct = 100 * float64(len(got)) / float64(e.Size/16)
		}

		// Collect sorted sample GRP IDs (first 10).
		ids := make([]int, 0, len(got))
		for id := range got {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		sample := ids
		if len(sample) > 10 {
			sample = sample[:10]
		}

		fmt.Printf("%-6d  %-18s  %-12.2f  %-8d  %-8.4f  %v\n",
			e.RegionN, e.AddrHex, sizeMB, len(got), fillPct, sample)

		totalEntries += len(got)
		if bestRegion == nil || len(got) > len(bestEntries) {
			bestRegion = e
			bestEntries = got
		}

		var addr uint64
		if _, scanErr := fmt.Sscanf(e.AddrHex, "0x%x", &addr); scanErr != nil {
			fmt.Fprintf(os.Stderr, "parse addr %q: %v\n", e.AddrHex, scanErr)
		}
		scans = append(scans, scanner.RegionScan{Addr: addr, Size: e.Size, Entries: got})
	}

	fmt.Printf("\nTotal entries across all regions: %d\n", totalEntries)
	if bestRegion != nil && len(bestEntries) > 0 {
		fmt.Printf("Best region: %s (region_%04d) — %d entries\n",
			bestRegion.AddrHex, bestRegion.RegionN, len(bestEntries))
	}

	// Production-selection verdict: run the dump through the EXACT selector
	// production scanProcess uses, so this tool reports what production would
	// actually do — never an inferred mechanism.
	fmt.Println()
	sel, err := scanner.SelectCollection(scans)
	if err != nil {
		fmt.Printf("VERDICT: production scan FAILS on this dump — %v\n", err)
		if totalEntries == 0 {
			fmt.Println("  -> H2 (likely): Unity dictionary layout may have changed.")
			fmt.Println("  -> Search a .bin for a known GRP ID in little-endian to inspect stride.")
			fmt.Println("     Example: card 96804 = 0x17A64 -> bytes: 64 7A 01 00")
		}
		return
	}
	fmt.Printf("VERDICT: production scan SUCCEEDS on this dump.\n")
	fmt.Printf("  -> scanner.SelectCollection picks region 0x%x: %d entries (runner-up %d), sanity band [%d, %d] OK.\n",
		sel.Addr, len(sel.Entries), sel.RunnerUpEntries, scanner.MinSaneCollection, scanner.MaxSaneCollection)
	fmt.Println("  -> If users still see scan failures on this MTGA build, the defect is NOT region selection —")
	fmt.Println("     check the installed helper binary version, socket path, and daemon-side logs.")
}
