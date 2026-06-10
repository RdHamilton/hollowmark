// Tests for the corpus-replay assertion harness (#641).
//
// These tests exercise the diffing, golden-artifact loading, and -update path
// WITHOUT requiring a live staging BFF. The live integration path (calling the
// real BFF) is exercised by the staging-replay-gate.yml CI job (Ray's #642 ticket).
//
// All BFF calls are stubbed via httptest.NewServer so tests are hermetic and fast.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// goldenOutcome helpers
// ---------------------------------------------------------------------------

// TestLoadGoldenOutcome_HappyPath verifies that loadGoldenOutcome reads and
// unmarshals a well-formed staging-outcome.json from disk.
func TestLoadGoldenOutcome_HappyPath(t *testing.T) {
	dir := t.TempDir()
	want := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "win", PlayerWins: 1, OpponentWins: 0},
		},
		Quests: []goldenQuest{
			{QuestID: "11111111-0000-4000-8000-000000000001", Progress: 11, Goal: 30},
		},
		Decks: []goldenDeck{
			{DeckID: "11111111-0000-4000-8000-000000000007", Format: "Standard"},
		},
		ProjectionErrorCount: 0,
	}

	data, _ := json.MarshalIndent(want, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "staging-outcome.json"), data, 0o600))

	got, err := loadGoldenOutcome(filepath.Join(dir, "staging-outcome.json"))
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestLoadGoldenOutcome_MissingFile verifies that loadGoldenOutcome returns an
// error when the file does not exist.
func TestLoadGoldenOutcome_MissingFile(t *testing.T) {
	_, err := loadGoldenOutcome("/tmp/does-not-exist-xyzzy/staging-outcome.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open")
}

// ---------------------------------------------------------------------------
// fetchStagingOutcome (BFF read path)
// ---------------------------------------------------------------------------

// TestFetchStagingOutcome_HappyPath stubs the BFF GET /matches, /quests, /decks,
// /projection-errors endpoints and verifies fetchStagingOutcome returns a
// populated goldenOutcome with stable fields only (no timestamps, no sequence).
func TestFetchStagingOutcome_HappyPath(t *testing.T) {
	bffSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/matches") || r.URL.Path == "/api/v1/matches":
			_, _ = fmt.Fprintln(w, `{
				"Matches": [{"ID":"m1","Format":"QuickDraft_SOS_20260526","Result":"win","PlayerWins":1,"OpponentWins":0}],
				"HasMore": false, "Limit": 20
			}`)
		case strings.HasSuffix(r.URL.Path, "/quests") || r.URL.Path == "/api/v1/quests":
			_, _ = fmt.Fprintln(w, `{
				"quests": [
					{"quest_id":"11111111-0000-4000-8000-000000000001","quest_name":"Test Quest","starting_progress":0,"ending_progress":11,"goal":30}
				],
				"has_quest_data": true
			}`)
		case strings.HasSuffix(r.URL.Path, "/decks") || r.URL.Path == "/api/v1/decks":
			_, _ = fmt.Fprintln(w, `[{"deck_id":"d1","name":"Test Deck","format":"Standard"}]`)
		case strings.Contains(r.URL.Path, "projection-errors"):
			_, _ = fmt.Fprintln(w, `{"count":0}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer bffSrv.Close()

	out, err := fetchStagingOutcome(bffSrv.URL, "sk_test_api_key", "acc_test_001")
	require.NoError(t, err)

	require.Len(t, out.Matches, 1)
	assert.Equal(t, "QuickDraft_SOS_20260526", out.Matches[0].Format)
	assert.Equal(t, "win", out.Matches[0].Result)
	assert.Equal(t, 0, out.ProjectionErrorCount)
}

// TestFetchStagingOutcome_BFF404 verifies that fetchStagingOutcome returns an
// error when the BFF returns HTTP 404.
func TestFetchStagingOutcome_BFF404(t *testing.T) {
	bffSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
	}))
	defer bffSrv.Close()

	_, err := fetchStagingOutcome(bffSrv.URL, "sk_test", "acc_001")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// diffOutcome (assertion logic)
// ---------------------------------------------------------------------------

// TestDiffOutcome_MatchPass verifies that diffOutcome returns no diffs when
// actual matches the golden exactly.
func TestDiffOutcome_MatchPass(t *testing.T) {
	golden := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "win", PlayerWins: 1, OpponentWins: 0},
		},
		ProjectionErrorCount: 0,
	}
	actual := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "win", PlayerWins: 1, OpponentWins: 0},
		},
		ProjectionErrorCount: 0,
	}

	diffs := diffOutcome(golden, actual)
	assert.Empty(t, diffs, "no diffs expected when actual matches golden")
}

// TestDiffOutcome_FormatMismatch verifies that diffOutcome emits a structured
// diff entry when the match format does not match the golden.
func TestDiffOutcome_FormatMismatch(t *testing.T) {
	golden := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "win"},
		},
	}
	actual := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "WRONG_FORMAT", Result: "win"},
		},
	}

	diffs := diffOutcome(golden, actual)
	require.Len(t, diffs, 1)
	assert.Equal(t, "match[0]", diffs[0].EventClass)
	assert.Equal(t, "Format", diffs[0].Field)
	assert.Equal(t, "QuickDraft_SOS_20260526", diffs[0].Expected)
	assert.Equal(t, "WRONG_FORMAT", diffs[0].Actual)
}

// TestDiffOutcome_ProjectionErrorNonZero verifies that diffOutcome always emits
// a diff when ProjectionErrorCount > 0, even if all other fields match.
func TestDiffOutcome_ProjectionErrorNonZero(t *testing.T) {
	golden := goldenOutcome{
		ProjectionErrorCount: 0,
	}
	actual := goldenOutcome{
		ProjectionErrorCount: 3,
	}

	diffs := diffOutcome(golden, actual)
	require.NotEmpty(t, diffs)
	found := false
	for _, d := range diffs {
		if d.Field == "ProjectionErrorCount" {
			found = true
			assert.Equal(t, "0", d.Expected)
			assert.Equal(t, "3", d.Actual)
		}
	}
	assert.True(t, found, "diff for ProjectionErrorCount must be present")
}

// TestDiffOutcome_MatchCountMismatch verifies that diffOutcome emits a diff
// when the number of matches in actual differs from golden.
func TestDiffOutcome_MatchCountMismatch(t *testing.T) {
	golden := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "win"},
		},
	}
	actual := goldenOutcome{
		Matches: []goldenMatch{},
	}

	diffs := diffOutcome(golden, actual)
	require.NotEmpty(t, diffs)
	assert.Equal(t, "matches", diffs[0].EventClass)
	assert.Equal(t, "Count", diffs[0].Field)
}

// TestDiffOutcome_QuestProgressMismatch verifies that diffOutcome emits a diff
// when quest progress differs.
func TestDiffOutcome_QuestProgressMismatch(t *testing.T) {
	golden := goldenOutcome{
		Quests: []goldenQuest{
			{QuestID: "q1", Progress: 11, Goal: 30},
		},
	}
	actual := goldenOutcome{
		Quests: []goldenQuest{
			{QuestID: "q1", Progress: 5, Goal: 30},
		},
	}

	diffs := diffOutcome(golden, actual)
	require.NotEmpty(t, diffs)
	found := false
	for _, d := range diffs {
		if d.Field == "Progress" {
			found = true
		}
	}
	assert.True(t, found, "Progress diff must be present")
}

// TestDiffOutcome_EmptyProjectionSentinel_UnknownFormat verifies that
// diffOutcome hard-fails when any actual match has Format="Unknown" — the
// BFF projection default when the daemon dispatched player_team_id=0 (the
// clientId→accountId bug, fixed in c2fa895d). This is always a projection
// error regardless of what the golden artifact says.
func TestDiffOutcome_EmptyProjectionSentinel_UnknownFormat(t *testing.T) {
	golden := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "win", PlayerWins: 1, OpponentWins: 0},
		},
	}
	actual := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "Unknown", Result: "unknown", PlayerWins: 0, OpponentWins: 0},
		},
	}

	diffs := diffOutcome(golden, actual)
	require.NotEmpty(t, diffs, "should fail when actual match has Format=Unknown (empty-projection sentinel)")
	found := false
	for _, d := range diffs {
		if d.Field == "EmptyProjection" {
			found = true
			assert.Contains(t, d.Actual, "Unknown", "diff should identify the sentinel value")
		}
	}
	assert.True(t, found, "must emit an EmptyProjection diff for Format=Unknown")
}

// TestDiffOutcome_EmptyProjectionSentinel_EmptyFormat verifies that
// diffOutcome hard-fails when any actual match has Format="" — the empty
// format that the projection stores when the daemon emits format="" from a
// match event where mtgaUserID was not yet known.
func TestDiffOutcome_EmptyProjectionSentinel_EmptyFormat(t *testing.T) {
	golden := goldenOutcome{} // golden is empty — sentinel check is unconditional
	actual := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "", Result: "win", PlayerWins: 1, OpponentWins: 0},
		},
	}

	diffs := diffOutcome(golden, actual)
	require.NotEmpty(t, diffs, "should fail when actual match has Format='' (empty-projection sentinel)")
	found := false
	for _, d := range diffs {
		if d.Field == "EmptyProjection" {
			found = true
		}
	}
	assert.True(t, found, "must emit an EmptyProjection diff for Format=''")
}

// TestDiffOutcome_EmptyProjectionSentinel_UnknownResult verifies that
// diffOutcome hard-fails when any actual match has Result="unknown" — the
// BFF projection default when player_team_id=0 prevented win/loss derivation.
func TestDiffOutcome_EmptyProjectionSentinel_UnknownResult(t *testing.T) {
	golden := goldenOutcome{}
	actual := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "unknown", PlayerWins: 0, OpponentWins: 0},
		},
	}

	diffs := diffOutcome(golden, actual)
	require.NotEmpty(t, diffs, "should fail when actual match has Result=unknown (empty-projection sentinel)")
	found := false
	for _, d := range diffs {
		if d.Field == "EmptyProjection" {
			found = true
			assert.Contains(t, d.Actual, "unknown")
		}
	}
	assert.True(t, found, "must emit an EmptyProjection diff for Result=unknown")
}

// TestDiffOutcome_EmptyProjectionSentinel_EmptyResult verifies that
// diffOutcome hard-fails when any actual match has Result="" — an empty
// result string is never a valid projected match state.
func TestDiffOutcome_EmptyProjectionSentinel_EmptyResult(t *testing.T) {
	golden := goldenOutcome{}
	actual := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "", PlayerWins: 0, OpponentWins: 0},
		},
	}

	diffs := diffOutcome(golden, actual)
	require.NotEmpty(t, diffs, "should fail when actual match has Result='' (empty-projection sentinel)")
	found := false
	for _, d := range diffs {
		if d.Field == "EmptyProjection" {
			found = true
		}
	}
	assert.True(t, found, "must emit an EmptyProjection diff for Result=''")
}

// TestDiffOutcome_ValidMatch_NoSentinelDiff verifies that diffOutcome does NOT
// emit a sentinel diff for a correctly-projected match (win/loss with real format).
func TestDiffOutcome_ValidMatch_NoSentinelDiff(t *testing.T) {
	golden := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "win", PlayerWins: 1, OpponentWins: 0},
		},
	}
	actual := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "win", PlayerWins: 1, OpponentWins: 0},
		},
	}

	diffs := diffOutcome(golden, actual)
	for _, d := range diffs {
		assert.NotEqual(t, "EmptyProjection", d.Field,
			"valid match must not emit EmptyProjection diff")
	}
}

// TestDiffOutcome_NeverAssertsOccurredAt verifies that diffOutcome does NOT
// include OccurredAt or Sequence in its output even when they differ — these
// are non-deterministic fields per ADR-042 Amendment 1 §1 determinism note.
func TestDiffOutcome_NeverAssertsOccurredAt(t *testing.T) {
	golden := goldenOutcome{}
	actual := goldenOutcome{}
	// Both are empty — confirm no spurious OccurredAt / Sequence diffs.
	diffs := diffOutcome(golden, actual)
	for _, d := range diffs {
		assert.NotEqual(t, "OccurredAt", d.Field,
			"[replay-check] OccurredAt must never appear in diff output (ADR-042 §1)")
		assert.NotEqual(t, "Sequence", d.Field,
			"[replay-check] Sequence must never appear in diff output (ADR-042 §1)")
	}
}

// ---------------------------------------------------------------------------
// writeGoldenOutcome (-update path)
// ---------------------------------------------------------------------------

// TestWriteGoldenOutcome_CreatesFile verifies that writeGoldenOutcome writes a
// well-formed JSON file to disk that loadGoldenOutcome can round-trip.
func TestWriteGoldenOutcome_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "staging-outcome.json")

	out := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "win", PlayerWins: 1, OpponentWins: 0},
		},
		Quests: []goldenQuest{
			{QuestID: "q1", Progress: 11, Goal: 30},
		},
		ProjectionErrorCount: 0,
	}

	require.NoError(t, writeGoldenOutcome(path, out))

	roundTripped, err := loadGoldenOutcome(path)
	require.NoError(t, err)
	assert.Equal(t, out, roundTripped)
}

// TestWriteGoldenOutcome_IndentedJSON verifies that the output is
// human-readable (indented JSON, not a single line).
func TestWriteGoldenOutcome_IndentedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "staging-outcome.json")

	out := goldenOutcome{Matches: []goldenMatch{{Format: "f"}}}
	require.NoError(t, writeGoldenOutcome(path, out))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	// Indented JSON must have newlines.
	assert.Contains(t, string(data), "\n", "golden artifact must be indented JSON")
}

// ---------------------------------------------------------------------------
// formatDiffs (output formatting)
// ---------------------------------------------------------------------------

// TestFormatDiffs_NonEmpty verifies that formatDiffs produces a non-empty
// human-readable string listing each diff entry.
func TestFormatDiffs_NonEmpty(t *testing.T) {
	diffs := []outcomeDiff{
		{EventClass: "match[0]", Field: "Format", Expected: "QuickDraft_SOS_20260526", Actual: "WRONG"},
		{EventClass: "matches", Field: "Count", Expected: "1", Actual: "0"},
	}

	out := formatDiffs(diffs)
	assert.Contains(t, out, "match[0]")
	assert.Contains(t, out, "Format")
	assert.Contains(t, out, "QuickDraft_SOS_20260526")
	assert.Contains(t, out, "WRONG")
}

// TestFormatDiffs_Empty verifies that formatDiffs returns a non-empty "all
// pass" style message when diffs is empty.
func TestFormatDiffs_Empty(t *testing.T) {
	out := formatDiffs(nil)
	assert.NotEmpty(t, out)
}

// ---------------------------------------------------------------------------
// Flag validation — -golden required (#803 portability fix)
// ---------------------------------------------------------------------------

// TestValidateFlags_MissingGolden verifies that validateFlags returns a
// non-nil error when the -golden flag is empty.  The CWD-relative
// defaultGoldenPath() was removed in #803: -golden is now required, and the
// binary must produce a clear usage error rather than a silent file-not-found
// from the wrong working directory.
func TestValidateFlags_MissingGolden(t *testing.T) {
	err := validateFlags("http://staging-api.vaultmtg.app/api/v1", "sk_test_key", "acc_001", "")
	require.Error(t, err, "validateFlags must return an error when -golden is empty")
	assert.Contains(t, err.Error(), "golden", "error must mention the -golden flag so the user knows what to fix")
}

// TestValidateFlags_AllPresent verifies that validateFlags returns nil when
// all required flags are provided.
func TestValidateFlags_AllPresent(t *testing.T) {
	err := validateFlags("http://staging-api.vaultmtg.app/api/v1", "sk_test_key", "acc_001", "/tmp/staging-outcome.json")
	require.NoError(t, err, "validateFlags must not error when all required flags are provided")
}

// TestValidateFlags_MissingBff verifies that validateFlags still catches a
// missing -bff flag (regression: the portability change must not weaken
// existing required-flag enforcement).
func TestValidateFlags_MissingBff(t *testing.T) {
	err := validateFlags("", "sk_test_key", "acc_001", "/tmp/staging-outcome.json")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Corpus-parse regression guard (#803)
// ---------------------------------------------------------------------------

// TestLoadGoldenOutcome_CommittedCorpusRoundTrip loads the real checked-in
// staging-outcome.json from the corpus testdata directory and verifies it
// unmarshals into a structurally valid goldenOutcome.  This is a regression
// guard: if the JSON schema drifts from the goldenOutcome struct, this test
// will catch it in CI without needing a live BFF.
//
// The path is derived relative to this test file's package directory — it is
// stable under `go test` regardless of the caller's CWD (_shared.md §9).
func TestLoadGoldenOutcome_CommittedCorpusRoundTrip(t *testing.T) {
	// Derive path relative to the package source directory.  Under `go test`
	// the working directory is the package directory
	// (services/daemon/cmd/daemon-replay-check), so the relative hop to the
	// corpus testdata is stable.
	corpusGoldenPath := filepath.Join("..", "..", "testdata", "corpus", "replay-expected", "staging-outcome.json")

	got, err := loadGoldenOutcome(corpusGoldenPath)
	require.NoError(t, err, "committed staging-outcome.json must parse without error")

	// Structural invariants that must hold for any valid committed artifact.
	require.NotEmpty(t, got.Matches, "committed staging-outcome.json must contain at least one match row")
	for i, m := range got.Matches {
		assert.NotEmpty(t, m.Format, "match[%d].format must not be empty in the committed artifact", i)
		assert.NotEmpty(t, m.Result, "match[%d].result must not be empty in the committed artifact", i)
	}
	// projection_error_count must be zero in the committed golden artifact —
	// a committed non-zero value would mean the corpus has known projection
	// errors that were intentionally promoted, which is never valid.
	assert.Zero(t, got.ProjectionErrorCount,
		"committed staging-outcome.json must have projection_error_count=0")
}

// ---------------------------------------------------------------------------
// Min-match-count assertion regression guard (#803)
// ---------------------------------------------------------------------------

// TestDiffOutcome_MinMatchCountGuard is a named regression guard verifying
// that diffOutcome never silently passes when actual returns zero matches
// against a non-empty golden manifest.  This pins the Count-comparison branch
// in diffOutcome so that future refactors cannot accidentally suppress it.
func TestDiffOutcome_MinMatchCountGuard(t *testing.T) {
	golden := goldenOutcome{
		Matches: []goldenMatch{
			{Format: "QuickDraft_SOS_20260526", Result: "win", PlayerWins: 1, OpponentWins: 0},
		},
	}
	// Actual returns no matches — the harness must never silently PASS in this state.
	actual := goldenOutcome{Matches: []goldenMatch{}}

	diffs := diffOutcome(golden, actual)
	require.NotEmpty(t, diffs, "diffOutcome must emit at least one diff when actual has 0 matches and golden expects 1+")
	found := false
	for _, d := range diffs {
		if d.EventClass == "matches" && d.Field == "Count" {
			found = true
			assert.Equal(t, "1", d.Expected)
			assert.Equal(t, "0", d.Actual)
		}
	}
	assert.True(t, found, "a Count diff on the 'matches' EventClass must be present")
}

// ---------------------------------------------------------------------------
// Projection-errors 404 graceful-degrade (#803)
// ---------------------------------------------------------------------------

// TestFetchStagingOutcome_ProjectionErrors404_GraceDegrade verifies that when
// the /admin/projection-errors/count endpoint returns HTTP 404 (the endpoint
// may not yet exist on a staging build), fetchStagingOutcome still succeeds
// and returns ProjectionErrorCount=0.  The other endpoints (/matches, /quests,
// /decks) respond normally — this test isolates the 404 to the admin endpoint.
func TestFetchStagingOutcome_ProjectionErrors404_GraceDegrade(t *testing.T) {
	bffSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/matches"):
			_, _ = fmt.Fprintln(w, `{"Matches":[{"Format":"QuickDraft_SOS_20260526","Result":"win","PlayerWins":1,"OpponentWins":0}],"HasMore":false}`)
		case strings.HasSuffix(r.URL.Path, "/quests"):
			_, _ = fmt.Fprintln(w, `{"quests":[],"has_quest_data":false}`)
		case strings.HasSuffix(r.URL.Path, "/decks"):
			_, _ = fmt.Fprintln(w, `[]`)
		case strings.Contains(r.URL.Path, "projection-errors"):
			// Simulate endpoint not yet deployed on staging.
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintln(w, `{"error":"not found"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer bffSrv.Close()

	out, err := fetchStagingOutcome(bffSrv.URL, "sk_test_key", "acc_001")
	require.NoError(t, err, "fetchStagingOutcome must not return an error when projection-errors returns 404")
	assert.Equal(t, 0, out.ProjectionErrorCount,
		"ProjectionErrorCount must default to 0 when the admin endpoint returns 404 (graceful degrade)")
	// The match data from the other endpoints must still be populated.
	require.Len(t, out.Matches, 1, "matches must still be populated despite the projection-errors 404")
}
