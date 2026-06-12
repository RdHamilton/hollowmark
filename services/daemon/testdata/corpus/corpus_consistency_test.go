// Package corpus_test — fixture-consistency lint (ticket vmt-t#698).
//
// TestCorpusConsistency verifies that the key identity fields extracted from
// daemon-emit/*.json are identical in the paired db-expected/*.json and
// api-expected/*.json assertion files.
//
// Background: PR #2864 promoted real session data into daemon-emit/ without
// updating db-expected/ and api-expected/; the resulting silent drift broke
// TestProjectionIntegration and was only caught after the fact (#2913). This
// test hard-fails on any such drift so the next promotion cannot regress.
//
// Pairing rules:
//
//   - Pairings are EXPLICIT in the pairings table below. A daemon-emit file
//     with no entry is silently skipped — not every event produces a DB write
//     or an API response (e.g. match-completed-missing-id goes to the DLQ).
//   - Adding a new daemon-emit fixture that has paired db/api assertions
//     REQUIRES adding a row to the pairings table and a corresponding sub-test
//     in this file. The test will not automatically detect new files; the
//     explicit table is the authoritative pairing record.
//
// Run: go test -race ./services/daemon/testdata/corpus/...
package corpus_test

import (
	"encoding/json"
	"testing"

	"github.com/RdHamilton/hollowmark/services/contract"
)

// matchPair describes a daemon-emit match fixture and its expected DB + API
// counterparts.  matchIDInDB and formatInDB are Go field names in the
// db-expected JSON (PascalCase Go structs serialised by encoding/json).
type matchPair struct {
	// daemonEmit is the path relative to corpusDir.
	daemonEmit string
	// dbExpected is the db-expected file to check (empty = no DB assertion).
	dbExpected string
	// apiExpected is the api-expected file to check (empty = no API assertion).
	apiExpected string
	// formatIsNormalized: when true the daemon-emit format="" is expected to
	// become "Unknown" in db/api fixtures; the check compares "Unknown" instead
	// of the raw empty string.
	formatIsNormalized bool
}

// questPair describes a daemon-emit quest fixture and its expected DB counterpart.
type questPair struct {
	daemonEmit string
	dbExpected string
}

// deckPair describes a daemon-emit deck fixture and its expected DB + API counterparts.
type deckPair struct {
	daemonEmit  string
	dbExpected  string
	apiExpected string
}

// matchPairings is the authoritative map of which match daemon-emit fixtures
// are paired with which db/api assertion files, and how to interpret them.
// Keep this table in sync with services/bff/integration_test.go.
var matchPairings = []matchPair{
	{
		daemonEmit:  "daemon-emit/match-completed.json",
		dbExpected:  "db-expected/match-completed.json",
		apiExpected: "api-expected/match-history-response.json",
	},
	{
		daemonEmit:         "daemon-emit/match-completed-empty-format.json",
		dbExpected:         "db-expected/match-completed-empty-format.json",
		apiExpected:        "", // no separate api-expected for empty-format variant
		formatIsNormalized: true,
	},
	{
		// Brawl corpus fixture (ADR-062 / ticket #917): asserts format="Brawl"
		// propagates consistently through daemon-emit → db-expected → api-expected.
		// format_class="commander" and opponent_commander_id will be asserted once
		// the ADR-062 DB migrations (#913, #914, #915) land.
		daemonEmit:  "daemon-emit/match-completed-brawl.json",
		dbExpected:  "db-expected/match-completed-brawl.json",
		apiExpected: "api-expected/match-history-brawl-response.json",
	},
	{
		// Brawl LOSS fixture (#1317 AC1): REAL-DERIVED from Jhixiaus/Kaito vs Kyle/Etali
		// capture (2026-06-11). winningTeamId=2 (opponent), result=loss.
		// Closes the win-only blindspot: a win/loss-inversion bug passes the harness
		// unchallenged without this fixture. Also promotes Brawl to REAL provenance
		// ahead of Ranked Brawl launch.
		daemonEmit:  "daemon-emit/match-completed-brawl-loss.json",
		dbExpected:  "db-expected/match-completed-brawl-loss.json",
		apiExpected: "api-expected/match-history-brawl-loss-response.json",
	},
	{
		// 2-1 multi-game fixture (#1317 AC4): SYNTHETIC. Exercises player_wins=2 /
		// opponent_wins=1 path and ResultReason_DamageDealt (non-Concede, closes AC6).
		// game 2 lost to DamageDealt; games 1+3 won by Concede / DamageDealt.
		daemonEmit:  "daemon-emit/match-completed-2-1.json",
		dbExpected:  "db-expected/match-completed-2-1.json",
		apiExpected: "api-expected/match-history-2-1-response.json",
	},
	// daemon-emit/match-completed-missing-id.json intentionally has no pair:
	// the projection worker dead-letters events with no match_id and writes
	// nothing to the match table. There is nothing to assert consistency against.
}

// questPairings maps quest daemon-emit files to their db-expected counterparts.
// The integration test (TestProjectionIntegration/QuestProgressDedup) ingests
// quest-progress.json + quest-progress-duplicate.json and asserts the final DB
// state in db-expected/quest-upsert-result.json.  The first quest_id in the
// daemon-emit fixture must appear in the db-expected fixture.
var questPairings = []questPair{
	{
		daemonEmit: "daemon-emit/quest-progress.json",
		dbExpected: "db-expected/quest-upsert-result.json",
	},
}

// deckPairings maps deck daemon-emit files to their db/api counterparts.
var deckPairings = []deckPair{
	{
		daemonEmit:  "daemon-emit/deck-updated.json",
		dbExpected:  "db-expected/deck-updated.json",
		apiExpected: "api-expected/deck-response.json",
	},
}

// TestCorpusConsistency is the hard-fail guard against daemon-emit / db-expected /
// api-expected drift.  It runs in under 5 ms on a cold runner (pure in-process
// JSON parsing; no network, no DB).
func TestCorpusConsistency(t *testing.T) {
	t.Run("match fixtures", func(t *testing.T) {
		for _, p := range matchPairings {
			p := p
			t.Run(p.daemonEmit, func(t *testing.T) {
				checkMatchPair(t, p)
			})
		}
	})

	t.Run("quest fixtures", func(t *testing.T) {
		for _, p := range questPairings {
			p := p
			t.Run(p.daemonEmit, func(t *testing.T) {
				checkQuestPair(t, p)
			})
		}
	})

	t.Run("deck fixtures", func(t *testing.T) {
		for _, p := range deckPairings {
			p := p
			t.Run(p.daemonEmit, func(t *testing.T) {
				checkDeckPair(t, p)
			})
		}
	})
}

// ---------------------------------------------------------------------------
// Per-event-type checkers
// ---------------------------------------------------------------------------

// checkMatchPair asserts that match_id and format in a daemon-emit fixture are
// consistent with the paired db-expected and api-expected files.
func checkMatchPair(t *testing.T, p matchPair) {
	t.Helper()

	// Parse the daemon-emit fixture.
	emitData := mustRead(t, p.daemonEmit)
	var emitEvt contract.DaemonEvent
	if err := json.Unmarshal(emitData, &emitEvt); err != nil {
		t.Fatalf("%s: unmarshal DaemonEvent: %v", p.daemonEmit, err)
	}
	var emitPayload contract.MatchCompletedPayload
	if err := json.Unmarshal(emitEvt.Payload, &emitPayload); err != nil {
		t.Fatalf("%s: unmarshal MatchCompletedPayload: %v", p.daemonEmit, err)
	}

	// Determine expected format: projection normalizes "" to "Unknown".
	expectedFormat := emitPayload.Format
	if p.formatIsNormalized && expectedFormat == "" {
		expectedFormat = "Unknown"
	}

	// Check db-expected.
	if p.dbExpected != "" {
		dbData := mustRead(t, p.dbExpected)
		var dbRow struct {
			ID     string `json:"ID"`
			Format string `json:"Format"`
		}
		if err := json.Unmarshal(dbData, &dbRow); err != nil {
			t.Fatalf("%s: unmarshal db-expected: %v", p.dbExpected, err)
		}
		if dbRow.ID != emitPayload.MatchID {
			t.Errorf("match_id drift: daemon-emit %s has match_id=%q but db-expected %s has ID=%q",
				p.daemonEmit, emitPayload.MatchID, p.dbExpected, dbRow.ID)
		}
		if dbRow.Format != expectedFormat {
			t.Errorf("format drift: daemon-emit %s has format=%q (expected %q after normalization) but db-expected %s has Format=%q",
				p.daemonEmit, emitPayload.Format, expectedFormat, p.dbExpected, dbRow.Format)
		}
	}

	// Check api-expected.
	if p.apiExpected != "" {
		apiData := mustRead(t, p.apiExpected)
		var apiResp struct {
			Matches []struct {
				ID     string `json:"ID"`
				Format string `json:"Format"`
			} `json:"Matches"`
		}
		if err := json.Unmarshal(apiData, &apiResp); err != nil {
			t.Fatalf("%s: unmarshal api-expected: %v", p.apiExpected, err)
		}
		if len(apiResp.Matches) == 0 {
			t.Fatalf("%s: api-expected Matches array is empty", p.apiExpected)
		}
		// The match-history response is ordered newest-first; the most-recently-projected
		// match appears at index 0.
		firstMatch := apiResp.Matches[0]
		if firstMatch.ID != emitPayload.MatchID {
			t.Errorf("match_id drift: daemon-emit %s has match_id=%q but api-expected %s Matches[0].ID=%q",
				p.daemonEmit, emitPayload.MatchID, p.apiExpected, firstMatch.ID)
		}
		if firstMatch.Format != expectedFormat {
			t.Errorf("format drift: daemon-emit %s has format=%q (expected %q after normalization) but api-expected %s Matches[0].Format=%q",
				p.daemonEmit, emitPayload.Format, expectedFormat, p.apiExpected, firstMatch.Format)
		}
	}
}

// checkQuestPair asserts that the first quest_id in the daemon-emit quest-progress
// fixture appears in the db-expected upsert-result fixture.  The dedup scenario
// ingests two quest events; db-expected/quest-upsert-result.json reflects the
// post-dedup state, which must reference a quest_id from the daemon-emit payload.
func checkQuestPair(t *testing.T, p questPair) {
	t.Helper()

	emitData := mustRead(t, p.daemonEmit)
	var emitEvt contract.DaemonEvent
	if err := json.Unmarshal(emitData, &emitEvt); err != nil {
		t.Fatalf("%s: unmarshal DaemonEvent: %v", p.daemonEmit, err)
	}
	var emitPayload contract.QuestProgressPayload
	if err := json.Unmarshal(emitEvt.Payload, &emitPayload); err != nil {
		t.Fatalf("%s: unmarshal QuestProgressPayload: %v", p.daemonEmit, err)
	}
	if len(emitPayload.Quests) == 0 {
		t.Fatalf("%s: quest payload must contain at least one quest", p.daemonEmit)
	}

	// Build a set of quest_ids present in the daemon-emit fixture.
	emitQuestIDs := make(map[string]bool, len(emitPayload.Quests))
	for _, q := range emitPayload.Quests {
		emitQuestIDs[q.QuestID] = true
	}

	dbData := mustRead(t, p.dbExpected)
	var dbRow struct {
		QuestID string `json:"QuestID"`
	}
	if err := json.Unmarshal(dbData, &dbRow); err != nil {
		t.Fatalf("%s: unmarshal db-expected: %v", p.dbExpected, err)
	}
	if !emitQuestIDs[dbRow.QuestID] {
		t.Errorf("quest_id drift: db-expected %s has QuestID=%q which is not present in daemon-emit %s (known quest_ids: %v)",
			p.dbExpected, dbRow.QuestID, p.daemonEmit, questIDSlice(emitPayload.Quests))
	}
}

// checkDeckPair asserts that deck_id and format in a daemon-emit deck fixture
// are consistent with the paired db-expected and api-expected files.
func checkDeckPair(t *testing.T, p deckPair) {
	t.Helper()

	emitData := mustRead(t, p.daemonEmit)
	var emitEvt contract.DaemonEvent
	if err := json.Unmarshal(emitData, &emitEvt); err != nil {
		t.Fatalf("%s: unmarshal DaemonEvent: %v", p.daemonEmit, err)
	}
	var emitPayload contract.DeckUpdatedPayload
	if err := json.Unmarshal(emitEvt.Payload, &emitPayload); err != nil {
		t.Fatalf("%s: unmarshal DeckUpdatedPayload: %v", p.daemonEmit, err)
	}

	if p.dbExpected != "" {
		dbData := mustRead(t, p.dbExpected)
		var dbRow struct {
			DeckID string `json:"DeckID"`
			Format string `json:"Format"`
		}
		if err := json.Unmarshal(dbData, &dbRow); err != nil {
			t.Fatalf("%s: unmarshal db-expected: %v", p.dbExpected, err)
		}
		if dbRow.DeckID != emitPayload.DeckID {
			t.Errorf("deck_id drift: daemon-emit %s has deck_id=%q but db-expected %s has DeckID=%q",
				p.daemonEmit, emitPayload.DeckID, p.dbExpected, dbRow.DeckID)
		}
		if dbRow.Format != emitPayload.Format {
			t.Errorf("format drift: daemon-emit %s has format=%q but db-expected %s has Format=%q",
				p.daemonEmit, emitPayload.Format, p.dbExpected, dbRow.Format)
		}
	}

	if p.apiExpected != "" {
		apiData := mustRead(t, p.apiExpected)
		var apiResp struct {
			DeckID string `json:"deck_id"`
			Format string `json:"format"`
		}
		if err := json.Unmarshal(apiData, &apiResp); err != nil {
			t.Fatalf("%s: unmarshal api-expected: %v", p.apiExpected, err)
		}
		if apiResp.DeckID != emitPayload.DeckID {
			t.Errorf("deck_id drift: daemon-emit %s has deck_id=%q but api-expected %s has deck_id=%q",
				p.daemonEmit, emitPayload.DeckID, p.apiExpected, apiResp.DeckID)
		}
		if apiResp.Format != emitPayload.Format {
			t.Errorf("format drift: daemon-emit %s has format=%q but api-expected %s has format=%q",
				p.daemonEmit, emitPayload.Format, p.apiExpected, apiResp.Format)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func questIDSlice(quests []contract.QuestEntry) []string {
	ids := make([]string, len(quests))
	for i, q := range quests {
		ids[i] = q.QuestID
	}
	return ids
}
