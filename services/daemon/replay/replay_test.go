package replay_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/RdHamilton/vault-mtg/services/daemon/replay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Aliases for convenience in the test body.
var (
	ParseLogFile = replay.ParseLogFile
	WrapEvents   = replay.WrapEvents
)

// ParsedEvent is a type alias so tests can construct values directly.
type ParsedEvent = replay.ParsedEvent

// corpusPlayerLogPath returns the absolute path to a corpus player-log fixture.
// Resolved relative to this file so it works in any working directory.
func corpusPlayerLogPath(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile: .../services/daemon/replay/replay_test.go
	// corpus:   .../services/daemon/testdata/corpus/player-log/<name>
	return filepath.Clean(filepath.Join(
		filepath.Dir(thisFile), "..", "testdata", "corpus", "player-log", name,
	))
}

// corpusExists reports whether the corpus player-log directory is present.
func corpusExists(t *testing.T) bool {
	t.Helper()
	dir := filepath.Clean(filepath.Join(
		func() string {
			_, f, _, _ := runtime.Caller(0)
			return f
		}(),
		"..", "..", "testdata", "corpus", "player-log",
	))
	_, err := os.Stat(dir)
	return err == nil
}

// ─── ParseLogFile unit tests ─────────────────────────────────────────────────

// TestParseLogFile_MatchCompleted verifies that ParseLogFile correctly parses
// the committed corpus match-completed fixture and returns exactly one
// match.completed event with the expected format.
func TestParseLogFile_MatchCompleted(t *testing.T) {
	path := corpusPlayerLogPath(t, "match-completed.log")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("corpus fixture absent: %s", path)
	}

	result, err := ParseLogFile(path)
	require.NoError(t, err)
	require.NotNil(t, result)

	// The corpus match-completed.log carries one match event.
	if result.MatchCount == 0 {
		t.Error("MatchCount: want >0, got 0 — match.completed not parsed from corpus fixture")
	}

	// The parsed event must have event type "match.completed".
	found := false
	for _, evt := range result.Events {
		if evt.EventType == "match.completed" {
			found = true
			// Payload must be valid JSON with a non-empty format field.
			var p struct {
				Format string `json:"format"`
				Result string `json:"result"`
			}
			require.NoError(t, json.Unmarshal(evt.Payload, &p),
				"match.completed payload must unmarshal")
			assert.NotEmpty(t, p.Format,
				"match.completed payload: format must be non-empty")
			// Result may be empty if clientId was not in the log (no preceding
			// authenticateResponse) — acceptable for the corpus fixture.
			break
		}
	}
	assert.True(t, found, "expected a match.completed event in parsed output")
}

// TestParseLogFile_QuestProgress verifies that ParseLogFile parses the
// committed corpus quest-progress fixture.
func TestParseLogFile_QuestProgress(t *testing.T) {
	path := corpusPlayerLogPath(t, "quest-progress.log")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("corpus fixture absent: %s", path)
	}

	result, err := ParseLogFile(path)
	require.NoError(t, err)
	require.NotNil(t, result)

	if result.QuestCount == 0 {
		t.Error("QuestCount: want >0, got 0 — quest.progress not parsed")
	}
}

// TestParseLogFile_DeckUpdated verifies that ParseLogFile parses the
// committed corpus deck-updated fixture.
func TestParseLogFile_DeckUpdated(t *testing.T) {
	path := corpusPlayerLogPath(t, "deck-updated.log")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("corpus fixture absent: %s", path)
	}

	result, err := ParseLogFile(path)
	require.NoError(t, err)
	require.NotNil(t, result)

	if result.DeckCount == 0 {
		t.Error("DeckCount: want >0, got 0 — deck.updated not parsed")
	}
}

// TestParseLogFile_PremierDraftPack verifies that ParseLogFile parses the
// committed corpus premier-draft-pack fixture and classifies it as draft.pack.
func TestParseLogFile_PremierDraftPack(t *testing.T) {
	path := corpusPlayerLogPath(t, "premier-draft-pack.log")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("corpus fixture absent: %s", path)
	}

	result, err := ParseLogFile(path)
	require.NoError(t, err)
	require.NotNil(t, result)

	if result.DraftPackCount == 0 {
		t.Error("DraftPackCount: want >0, got 0 — draft.pack not parsed from premier fixture")
	}
}

// TestParseLogFile_NonExistentFile verifies that ParseLogFile returns an
// error when the file does not exist.
func TestParseLogFile_NonExistentFile(t *testing.T) {
	_, err := ParseLogFile("/tmp/does-not-exist-layer5-replay-xyz/Player.log")
	require.Error(t, err, "should return error for non-existent file")
}

// TestParseLogFile_EmptyFile verifies that ParseLogFile returns a zero-entry
// result (no error) when the file exists but is empty.
func TestParseLogFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Player.log")
	require.NoError(t, os.WriteFile(path, []byte(""), 0o600))

	result, err := ParseLogFile(path)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Events)
	assert.Equal(t, 0, result.MatchCount)
}

// TestParseLogFile_NonJSONLines verifies that ParseLogFile skips non-JSON
// lines (plain text timestamps etc.) without error.
func TestParseLogFile_NonJSONLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Player.log")
	content := "not json at all\n[UnityCrossThreadLogger]12/30/1899 12:00:00 AM \nalso not json"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	result, err := ParseLogFile(path)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Events, "non-JSON lines must produce no events")
}

// TestParseLogFile_ClientIDExtraction verifies that ParseLogFile extracts
// the clientId from an authenticateResponse entry in the log.
func TestParseLogFile_ClientIDExtraction(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Player.log")
	// Single-line log entry with authenticateResponse.
	line := `{"authenticateResponse":{"clientId":"TESTCLIENTID123","sessionId":"sess-abc","screenName":"TestPlayer#0001"}}`
	require.NoError(t, os.WriteFile(path, []byte(line+"\n"), 0o600))

	result, err := ParseLogFile(path)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "TESTCLIENTID123", result.ClientID,
		"clientId must be extracted from authenticateResponse")
	// player.authenticated events are not inserted into daemon_events.
	assert.Empty(t, result.Events, "player.authenticated must not produce a daemon event")
}

// TestParseLogFile_GREEventsSkipped verifies that greToClientEvent entries
// are skipped without error (GRE buffering is stateful — not supported in
// the stateless replay injector).
func TestParseLogFile_GREEventsSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Player.log")
	line := `{"greToClientEvent":{"type":"GREMessageType_GameStateMessage","msgId":1}}`
	require.NoError(t, os.WriteFile(path, []byte(line+"\n"), 0o600))

	result, err := ParseLogFile(path)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Events, "greToClientEvent must be skipped by the stateless injector")
}

// ─── WrapEvents unit tests ───────────────────────────────────────────────────

// TestWrapEvents_StableEventIDs verifies that WrapEvents produces identical
// event_ids when called twice with the same input. This is the property
// that makes the determinism test work: ON CONFLICT DO NOTHING on event_id
// prevents duplicate inserts on the second replay.
func TestWrapEvents_StableEventIDs(t *testing.T) {
	events := []ParsedEvent{
		{EventType: "match.completed", Payload: json.RawMessage(`{"format":"QuickDraft_SOS"}`)},
		{EventType: "quest.progress", Payload: json.RawMessage(`{"quests":[]}`)},
	}

	wrapped1, err := WrapEvents(events, "acc001", "session-fixed-uuid-0000000000001")
	require.NoError(t, err)
	wrapped2, err := WrapEvents(events, "acc001", "session-fixed-uuid-0000000000001")
	require.NoError(t, err)

	require.Equal(t, len(wrapped1), len(wrapped2), "wrapped count must be equal")
	for i := range wrapped1 {
		assert.Equal(t, wrapped1[i].EventID, wrapped2[i].EventID,
			"event_id[%d] must be identical across two WrapEvents calls with the same sessionID", i)
		assert.Equal(t, wrapped1[i].OccurredAt, wrapped2[i].OccurredAt,
			"OccurredAt[%d] must be deterministic (not time.Now)", i)
		assert.Equal(t, wrapped1[i].Sequence, wrapped2[i].Sequence,
			"Sequence[%d] must be stable", i)
	}
}

// TestWrapEvents_SequenceMonotonic verifies that WrapEvents assigns
// monotonically increasing Sequence values starting at 1.
func TestWrapEvents_SequenceMonotonic(t *testing.T) {
	events := []ParsedEvent{
		{EventType: "match.completed", Payload: json.RawMessage(`{}`)},
		{EventType: "quest.progress", Payload: json.RawMessage(`{}`)},
		{EventType: "deck.updated", Payload: json.RawMessage(`{}`)},
	}

	wrapped, err := WrapEvents(events, "acc001", "sess-000000000000000000000000000001")
	require.NoError(t, err)
	require.Len(t, wrapped, 3)

	assert.Equal(t, uint64(1), wrapped[0].Sequence)
	assert.Equal(t, uint64(2), wrapped[1].Sequence)
	assert.Equal(t, uint64(3), wrapped[2].Sequence)
}

// TestWrapEvents_AccountIDPropagated verifies that WrapEvents sets the
// AccountID field on every wrapped event.
func TestWrapEvents_AccountIDPropagated(t *testing.T) {
	events := []ParsedEvent{
		{EventType: "match.completed", Payload: json.RawMessage(`{}`)},
	}

	wrapped, err := WrapEvents(events, "test-account-abc", "sess-000000000000000000000000000001")
	require.NoError(t, err)
	require.Len(t, wrapped, 1)
	assert.Equal(t, "test-account-abc", wrapped[0].AccountID)
}

// TestWrapEvents_EmptyInput verifies that WrapEvents returns an empty slice
// without error for an empty input.
func TestWrapEvents_EmptyInput(t *testing.T) {
	wrapped, err := WrapEvents(nil, "acc", "sess-000000000000000000000000000001")
	require.NoError(t, err)
	assert.Empty(t, wrapped)
}

// ─── re-export shims for package-level test access ────────────────────────────
// These are in the same package (_test suffix — external test package) so we
// import the public API; no shims needed.
