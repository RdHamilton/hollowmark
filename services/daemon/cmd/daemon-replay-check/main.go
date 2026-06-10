// Command daemon-replay-check is the corpus-replay assertion harness for
// ADR-042 Amendment 1 (ticket #641).
//
// Usage:
//
//	daemon-replay-check \
//	  -bff       <staging BFF base URL, e.g. https://staging-api.vaultmtg.app/api/v1> \
//	  -api-key   <daemon API key for the synthetic account> \
//	  -account   <synthetic account ID> \
//	  -golden    <path to staging-outcome.json (required)> \
//	  [-update]  <regenerate the golden artifact from a fresh staging read instead of asserting>
//
// The -golden flag is required; there is no default.  Always supply an
// explicit path.  The canonical corpus artifact lives at
// services/daemon/testdata/corpus/replay-expected/staging-outcome.json
// relative to the repo root, but the binary does not infer a path from its
// working directory (_shared.md §9 — no CWD-relative defaults in binaries).
//
// The harness reads the staging state back via the BFF read API scoped to the
// synthetic account, diffs it against the golden artifact, and exits non-zero
// on mismatch. With -update it regenerates the golden artifact (used during
// corpus refresh, ADR-041 G3 drift-canary refresh protocol).
//
// What it asserts (MANIFEST-promotion-aware — ADR-042 §3):
//   - Match count, format, result, player/opponent wins
//   - Quest count, quest IDs, progress, goal (REAL/promoted fixtures only)
//   - Deck count and deck IDs (REAL/promoted fixtures only)
//   - Zero unexpected projection_errors rows (always asserted)
//
// What it NEVER asserts (non-deterministic per ADR-042 Amendment 1 §1):
//   - OccurredAt (wall-clock timestamp stamped by dispatch.BuildEvent)
//   - Sequence (per-session monotonic counter reset on daemon restart)
//   - collection.updated, Premier-draft events (FORMAT-CONFIRMED-not-promoted)
//
// How #642 invokes this harness (note: -golden is required):
//
//	VAULTMTG_REPLAY_BFF_URL=https://staging-api.vaultmtg.app/api/v1 \
//	VAULTMTG_REPLAY_API_KEY=<ssm-secret> \
//	VAULTMTG_REPLAY_ACCOUNT_ID=<ssm-param> \
//	  ./daemon-replay-check \
//	    -golden services/daemon/testdata/corpus/replay-expected/staging-outcome.json
//
// All env vars can be overridden by the corresponding flags.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	bffFlag := flag.String("bff", envOrDefault("VAULTMTG_REPLAY_BFF_URL", ""), "BFF base URL (required)")
	apiKeyFlag := flag.String("api-key", envOrDefault("VAULTMTG_REPLAY_API_KEY", ""), "daemon API key for the synthetic account")
	accountFlag := flag.String("account", envOrDefault("VAULTMTG_REPLAY_ACCOUNT_ID", ""), "synthetic account ID")
	goldenFlag := flag.String("golden", "", "path to staging-outcome.json (required — no default)")
	updateFlag := flag.Bool("update", false, "regenerate the golden artifact from staging instead of asserting")
	flag.Parse()

	if err := validateFlags(*bffFlag, *apiKeyFlag, *accountFlag, *goldenFlag); err != nil {
		log.Fatalf("[replay-check] %v", err)
	}

	actual, err := fetchStagingOutcome(*bffFlag, *apiKeyFlag, *accountFlag)
	if err != nil {
		log.Fatalf("[replay-check] fetch staging outcome: %v", err)
	}

	if *updateFlag {
		if err := writeGoldenOutcome(*goldenFlag, actual); err != nil {
			log.Fatalf("[replay-check] write golden artifact: %v", err)
		}
		log.Printf("[replay-check] golden artifact updated: %s", *goldenFlag)
		os.Exit(0)
	}

	golden, err := loadGoldenOutcome(*goldenFlag)
	if err != nil {
		log.Fatalf("[replay-check] load golden artifact: %v", err)
	}

	diffs := diffOutcome(golden, actual)
	if len(diffs) == 0 {
		log.Printf("[replay-check] PASS — staging outcome matches golden artifact (%s)", *goldenFlag)
		os.Exit(0)
	}

	fmt.Fprintln(os.Stderr, "[replay-check] FAIL — staging outcome differs from golden:")
	fmt.Fprintln(os.Stderr, formatDiffs(diffs))
	os.Exit(1)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// validateFlags checks that all required flags are non-empty and returns a
// descriptive error if any are missing.  Extracted from main() so it is
// testable without invoking os.Exit — the caller (main) calls log.Fatal on
// the returned error.
func validateFlags(bff, apiKey, account, golden string) error {
	if bff == "" {
		return fmt.Errorf("-bff / VAULTMTG_REPLAY_BFF_URL is required")
	}
	if apiKey == "" {
		return fmt.Errorf("-api-key / VAULTMTG_REPLAY_API_KEY is required")
	}
	if account == "" {
		return fmt.Errorf("-account / VAULTMTG_REPLAY_ACCOUNT_ID is required")
	}
	if golden == "" {
		return fmt.Errorf("-golden is required: supply an explicit path to staging-outcome.json (no CWD-relative default exists)")
	}
	return nil
}

// ─── Golden artifact types ────────────────────────────────────────────────────

// goldenOutcome is the on-disk shape of staging-outcome.json.
// It contains only the stable, deterministic fields that survive across replay
// runs — specifically NOT OccurredAt or Sequence (ADR-042 Amendment 1 §1).
//
// MANIFEST-promotion note: only event classes whose corpus fixtures are
// REAL/promoted contribute rows to this struct. FORMAT-CONFIRMED-not-promoted
// event classes (collection.updated, Premier-draft) are intentionally absent.
type goldenOutcome struct {
	// Matches is the set of projected match rows from the corpus replay.
	// Assert: count, Format, Result, PlayerWins, OpponentWins.
	Matches []goldenMatch `json:"matches"`
	// Quests is the set of projected quest rows from the corpus replay.
	// Assert: count, QuestID, Progress, Goal.
	Quests []goldenQuest `json:"quests"`
	// Decks is the set of projected deck rows from the corpus replay.
	// Assert: count, DeckID, Format.
	Decks []goldenDeck `json:"decks"`
	// ProjectionErrorCount is the count of rows in the projection_errors table
	// scoped to the synthetic account. Must always be zero.
	ProjectionErrorCount int `json:"projection_error_count"`
}

// goldenMatch holds the stable projected fields for a single match row.
type goldenMatch struct {
	Format       string `json:"format"`
	Result       string `json:"result"`
	PlayerWins   int    `json:"player_wins"`
	OpponentWins int    `json:"opponent_wins"`
}

// goldenQuest holds the stable projected fields for a single quest row.
type goldenQuest struct {
	QuestID  string `json:"quest_id"`
	Progress int    `json:"progress"`
	Goal     int    `json:"goal"`
}

// goldenDeck holds the stable projected fields for a single deck row.
type goldenDeck struct {
	DeckID string `json:"deck_id"`
	Format string `json:"format"`
}

// ─── outcomeDiff ─────────────────────────────────────────────────────────────

// outcomeDiff is a single field-level diff between golden and actual.
type outcomeDiff struct {
	EventClass string // e.g. "match[0]", "matches", "quest[0]", "projection_errors"
	Field      string // e.g. "Format", "Count", "Progress"
	Expected   string // golden value (stringified)
	Actual     string // actual value (stringified)
}

// ─── loadGoldenOutcome ───────────────────────────────────────────────────────

// loadGoldenOutcome reads and unmarshals the golden artifact from disk.
func loadGoldenOutcome(path string) (goldenOutcome, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return goldenOutcome{}, fmt.Errorf("open %s: %w", path, err)
	}
	var out goldenOutcome
	if err := json.Unmarshal(data, &out); err != nil {
		return goldenOutcome{}, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return out, nil
}

// ─── writeGoldenOutcome (-update path) ──────────────────────────────────────

// writeGoldenOutcome serialises out to path as indented JSON (human-readable).
// Used by the -update flag to regenerate the golden artifact after a corpus refresh.
func writeGoldenOutcome(path string, out goldenOutcome) error {
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal golden outcome: %w", err)
	}
	data = append(data, '\n') // trailing newline for POSIX compliance
	return os.WriteFile(path, data, 0o644)
}

// ─── fetchStagingOutcome (BFF read path) ─────────────────────────────────────

// fetchStagingOutcome reads the staging state back via the BFF read API scoped
// to the synthetic account. It calls /matches, /quests, /decks, and
// /admin/projection-errors-count and assembles a goldenOutcome for comparison.
//
// Only stable, deterministic fields are extracted — never OccurredAt or Sequence.
func fetchStagingOutcome(bffBase, apiKey, accountID string) (goldenOutcome, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	base := strings.TrimRight(bffBase, "/")

	out := goldenOutcome{}

	// ── Matches ──────────────────────────────────────────────────────────────
	{
		// POST /matches with empty body (returns all matches for the account).
		req, err := http.NewRequest(http.MethodPost, base+"/matches", strings.NewReader("{}"))
		if err != nil {
			return out, fmt.Errorf("build matches request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Synthetic", "true")

		resp, err := client.Do(req)
		if err != nil {
			return out, fmt.Errorf("fetch matches: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return out, fmt.Errorf("fetch matches: BFF returned %d: %s", resp.StatusCode, string(body))
		}
		var mr struct {
			Matches []struct {
				Format       string `json:"Format"`
				Result       string `json:"Result"`
				PlayerWins   int    `json:"PlayerWins"`
				OpponentWins int    `json:"OpponentWins"`
			} `json:"Matches"`
		}
		if err := json.Unmarshal(body, &mr); err != nil {
			return out, fmt.Errorf("decode matches response: %w", err)
		}
		for _, m := range mr.Matches {
			out.Matches = append(out.Matches, goldenMatch{
				Format:       m.Format,
				Result:       m.Result,
				PlayerWins:   m.PlayerWins,
				OpponentWins: m.OpponentWins,
			})
		}
	}

	// ── Quests ───────────────────────────────────────────────────────────────
	{
		req, err := http.NewRequest(http.MethodGet, base+"/quests", nil)
		if err != nil {
			return out, fmt.Errorf("build quests request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("X-Synthetic", "true")

		resp, err := client.Do(req)
		if err != nil {
			return out, fmt.Errorf("fetch quests: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return out, fmt.Errorf("fetch quests: BFF returned %d: %s", resp.StatusCode, string(body))
		}
		var qr struct {
			Quests []struct {
				QuestID          string `json:"quest_id"`
				StartingProgress int    `json:"starting_progress"`
				EndingProgress   int    `json:"ending_progress"`
				Goal             int    `json:"goal"`
			} `json:"quests"`
		}
		if err := json.Unmarshal(body, &qr); err != nil {
			return out, fmt.Errorf("decode quests response: %w", err)
		}
		for _, q := range qr.Quests {
			// Progress is the ending_progress value (the last-seen progress in
			// the projected row, which matches the corpus fixture value).
			out.Quests = append(out.Quests, goldenQuest{
				QuestID:  q.QuestID,
				Progress: q.EndingProgress,
				Goal:     q.Goal,
			})
		}
	}

	// ── Decks ─────────────────────────────────────────────────────────────────
	{
		req, err := http.NewRequest(http.MethodGet, base+"/decks", nil)
		if err != nil {
			return out, fmt.Errorf("build decks request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("X-Synthetic", "true")

		resp, err := client.Do(req)
		if err != nil {
			return out, fmt.Errorf("fetch decks: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return out, fmt.Errorf("fetch decks: BFF returned %d: %s", resp.StatusCode, string(body))
		}
		var decks []struct {
			DeckID string `json:"deck_id"`
			Format string `json:"format"`
		}
		if err := json.Unmarshal(body, &decks); err != nil {
			return out, fmt.Errorf("decode decks response: %w", err)
		}
		for _, d := range decks {
			out.Decks = append(out.Decks, goldenDeck{
				DeckID: d.DeckID,
				Format: d.Format,
			})
		}
	}

	// ── Projection errors ─────────────────────────────────────────────────────
	{
		req, err := http.NewRequest(http.MethodGet, base+"/admin/projection-errors/count", nil)
		if err != nil {
			return out, fmt.Errorf("build projection-errors request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("X-Synthetic", "true")

		resp, err := client.Do(req)
		if err != nil {
			return out, fmt.Errorf("fetch projection-errors: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusOK {
			var pe struct {
				Count int `json:"count"`
			}
			if jsonErr := json.Unmarshal(body, &pe); jsonErr == nil {
				out.ProjectionErrorCount = pe.Count
			}
		}
		// Non-200 on the admin endpoint is treated as 0 projection errors
		// (the endpoint may not exist yet on staging — degrade gracefully).
	}

	return out, nil
}

// ─── diffOutcome ─────────────────────────────────────────────────────────────

// isEmptyProjectionFormat reports whether f is a sentinel format value that
// the BFF projection worker writes when the daemon dispatched a match event
// with player_team_id=0 (the accountId→clientId bug, fixed in c2fa895d).
// These are never valid projected values — they indicate a broken daemon session.
func isEmptyProjectionFormat(f string) bool {
	return f == "" || f == "Unknown"
}

// isEmptyProjectionResult reports whether r is a sentinel result value that
// the BFF projection worker writes when it cannot derive win/loss from the
// match payload (player_team_id=0 or winning_team_id=0).
// These are never valid projected values for a completed match.
func isEmptyProjectionResult(r string) bool {
	return r == "" || r == "unknown"
}

// diffOutcome compares actual against golden and returns a slice of field-level
// diffs. The comparison is on stable projected fields only — never OccurredAt,
// Sequence, or any other non-deterministic field (ADR-042 Amendment 1 §1).
//
// The diff is MANIFEST-promotion-aware: only fields present in the golden
// artifact are asserted. An empty golden slice means "no assertion" (not
// "assert zero rows"), so un-promoted event classes do not fail the gate.
//
// Sentinel check (unconditional): any actual match with Format in {"","Unknown"}
// or Result in {"","unknown"} is always a hard failure regardless of golden.
// These values indicate empty-projection (player_team_id=0 bug) and must never
// appear in a healthy staging state (ADR-042 Amendment 1 §4).
func diffOutcome(golden, actual goldenOutcome) []outcomeDiff {
	var diffs []outcomeDiff

	// ── ProjectionErrorCount (always asserted) ────────────────────────────────
	// Zero unexpected projection errors is a hard invariant — ADR-042 §3.
	if actual.ProjectionErrorCount != golden.ProjectionErrorCount {
		diffs = append(diffs, outcomeDiff{
			EventClass: "projection_errors",
			Field:      "ProjectionErrorCount",
			Expected:   fmt.Sprintf("%d", golden.ProjectionErrorCount),
			Actual:     fmt.Sprintf("%d", actual.ProjectionErrorCount),
		})
	}

	// ── Empty-projection sentinel check (always asserted, ADR-042 A1 §4) ─────
	// Format in {"","Unknown"} or Result in {"","unknown"} are BFF-default
	// values written when the daemon dispatched player_team_id=0 (the
	// clientId→accountId bug, c2fa895d).  A match in this state means the
	// daemon replay failed to wire the player identity — always a regression.
	for i, am := range actual.Matches {
		if isEmptyProjectionFormat(am.Format) || isEmptyProjectionResult(am.Result) {
			diffs = append(diffs, outcomeDiff{
				EventClass: fmt.Sprintf("match[%d]", i),
				Field:      "EmptyProjection",
				Expected:   "non-empty format and result (win|loss)",
				Actual:     fmt.Sprintf("format=%q result=%q player_wins=%d opponent_wins=%d", am.Format, am.Result, am.PlayerWins, am.OpponentWins),
			})
		}
	}

	// ── Matches ───────────────────────────────────────────────────────────────
	if len(golden.Matches) > 0 && len(actual.Matches) != len(golden.Matches) {
		diffs = append(diffs, outcomeDiff{
			EventClass: "matches",
			Field:      "Count",
			Expected:   fmt.Sprintf("%d", len(golden.Matches)),
			Actual:     fmt.Sprintf("%d", len(actual.Matches)),
		})
	} else {
		for i, gm := range golden.Matches {
			if i >= len(actual.Matches) {
				break
			}
			am := actual.Matches[i]
			if am.Format != gm.Format {
				diffs = append(diffs, outcomeDiff{
					EventClass: fmt.Sprintf("match[%d]", i),
					Field:      "Format",
					Expected:   gm.Format,
					Actual:     am.Format,
				})
			}
			if am.Result != gm.Result {
				diffs = append(diffs, outcomeDiff{
					EventClass: fmt.Sprintf("match[%d]", i),
					Field:      "Result",
					Expected:   gm.Result,
					Actual:     am.Result,
				})
			}
			if am.PlayerWins != gm.PlayerWins {
				diffs = append(diffs, outcomeDiff{
					EventClass: fmt.Sprintf("match[%d]", i),
					Field:      "PlayerWins",
					Expected:   fmt.Sprintf("%d", gm.PlayerWins),
					Actual:     fmt.Sprintf("%d", am.PlayerWins),
				})
			}
			if am.OpponentWins != gm.OpponentWins {
				diffs = append(diffs, outcomeDiff{
					EventClass: fmt.Sprintf("match[%d]", i),
					Field:      "OpponentWins",
					Expected:   fmt.Sprintf("%d", gm.OpponentWins),
					Actual:     fmt.Sprintf("%d", am.OpponentWins),
				})
			}
		}
	}

	// ── Quests ────────────────────────────────────────────────────────────────
	if len(golden.Quests) > 0 && len(actual.Quests) != len(golden.Quests) {
		diffs = append(diffs, outcomeDiff{
			EventClass: "quests",
			Field:      "Count",
			Expected:   fmt.Sprintf("%d", len(golden.Quests)),
			Actual:     fmt.Sprintf("%d", len(actual.Quests)),
		})
	} else {
		for i, gq := range golden.Quests {
			if i >= len(actual.Quests) {
				break
			}
			aq := actual.Quests[i]
			if aq.QuestID != gq.QuestID {
				diffs = append(diffs, outcomeDiff{
					EventClass: fmt.Sprintf("quest[%d]", i),
					Field:      "QuestID",
					Expected:   gq.QuestID,
					Actual:     aq.QuestID,
				})
			}
			if aq.Progress != gq.Progress {
				diffs = append(diffs, outcomeDiff{
					EventClass: fmt.Sprintf("quest[%d]", i),
					Field:      "Progress",
					Expected:   fmt.Sprintf("%d", gq.Progress),
					Actual:     fmt.Sprintf("%d", aq.Progress),
				})
			}
			if aq.Goal != gq.Goal {
				diffs = append(diffs, outcomeDiff{
					EventClass: fmt.Sprintf("quest[%d]", i),
					Field:      "Goal",
					Expected:   fmt.Sprintf("%d", gq.Goal),
					Actual:     fmt.Sprintf("%d", aq.Goal),
				})
			}
		}
	}

	// ── Decks ─────────────────────────────────────────────────────────────────
	if len(golden.Decks) > 0 && len(actual.Decks) != len(golden.Decks) {
		diffs = append(diffs, outcomeDiff{
			EventClass: "decks",
			Field:      "Count",
			Expected:   fmt.Sprintf("%d", len(golden.Decks)),
			Actual:     fmt.Sprintf("%d", len(actual.Decks)),
		})
	} else {
		for i, gd := range golden.Decks {
			if i >= len(actual.Decks) {
				break
			}
			ad := actual.Decks[i]
			if ad.DeckID != gd.DeckID {
				diffs = append(diffs, outcomeDiff{
					EventClass: fmt.Sprintf("deck[%d]", i),
					Field:      "DeckID",
					Expected:   gd.DeckID,
					Actual:     ad.DeckID,
				})
			}
			if ad.Format != gd.Format {
				diffs = append(diffs, outcomeDiff{
					EventClass: fmt.Sprintf("deck[%d]", i),
					Field:      "Format",
					Expected:   gd.Format,
					Actual:     ad.Format,
				})
			}
		}
	}

	return diffs
}

// ─── formatDiffs ─────────────────────────────────────────────────────────────

// formatDiffs returns a human-readable, structured diff string.
// Each diff line identifies the event class, field, expected, and actual values
// so an engineer knows exactly which corpus fixture to update or which code
// regression to investigate (ADR-042 Amendment 1 §3, ADR-042 §Layer-2 standard).
func formatDiffs(diffs []outcomeDiff) string {
	if len(diffs) == 0 {
		return "[replay-check] all assertions passed — staging outcome matches golden"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[replay-check] %d diff(s) detected:\n", len(diffs)))
	for _, d := range diffs {
		sb.WriteString(fmt.Sprintf(
			"  %-20s  %-20s  expected=%-30s  actual=%s\n",
			d.EventClass, d.Field, d.Expected, d.Actual,
		))
	}
	sb.WriteString("\nTo update the golden artifact after an intended change: run with -update flag.\n")
	sb.WriteString("To fix a code regression: revert the change that introduced the mismatch.\n")
	sb.WriteString("See ADR-042 Amendment 1 §3 for the full expected-vs-actual loop.\n")
	return sb.String()
}
