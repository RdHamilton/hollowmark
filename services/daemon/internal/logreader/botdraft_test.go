package logreader

// botdraft_test.go — unit tests for the BotDraft (QuickDraft / bot-draft) wire
// format parsers added in #337.
//
// Real wire format (MTGA 2026.59.20, QuickDraft_SOS_20260526):
//
//   pack: {"CurrentModule":"BotDraft","Payload":"{\"Result\":\"Success\",
//          \"EventName\":\"QuickDraft_SOS_20260526\",\"DraftStatus\":\"PickNext\",
//          \"PackNumber\":0,\"PickNumber\":0,\"NumCardsToPick\":1,
//          \"DraftPack\":[\"102470\",...],\"PackStyles\":[],
//          \"PickedCards\":[],\"PickedStyles\":[]}"}
//   pick: {"id":"<uuid>","request":"{\"EventName\":\"QuickDraft_SOS_20260526\",
//          \"PickInfo\":{\"EventName\":\"QuickDraft_SOS_20260526\",
//          \"CardIds\":[\"102704\"],\"PackNumber\":0,\"PickNumber\":0}}"}
//
// Both shapes are doubly-nested: a stringified inner JSON envelope with
// CAPITALIZED keys and STRINGIFIED grpIds.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func botDraftPackEntry(t *testing.T, raw string) *LogEntry {
	t.Helper()
	entry := &LogEntry{Raw: raw}
	entry.parseJSON()
	require.True(t, entry.IsJSON, "fixture must parse as JSON")
	return entry
}

// ---------------------------------------------------------------------------
// ParseBotDraftStatusPack
// ---------------------------------------------------------------------------

// TestParseBotDraftStatusPack_FirstPick parses the real first-pick BotDraft
// pack envelope: pack 0 / pick 0, 14 stringified grpIds, no cards picked yet.
func TestParseBotDraftStatusPack_FirstPick(t *testing.T) {
	raw := `{"CurrentModule":"BotDraft","Payload":"{\"Result\":\"Success\",\"EventName\":\"QuickDraft_SOS_20260526\",\"DraftStatus\":\"PickNext\",\"PackNumber\":0,\"PickNumber\":0,\"NumCardsToPick\":1,\"DraftPack\":[\"102470\",\"102645\",\"102595\"],\"PackStyles\":[],\"PickedCards\":[],\"PickedStyles\":[]}"}`
	entry := botDraftPackEntry(t, raw)

	p, err := ParseBotDraftStatusPack(entry)
	require.NoError(t, err)
	require.NotNil(t, p)

	// EventName becomes CourseName so draftstate keys the session by event.
	assert.Equal(t, "QuickDraft_SOS_20260526", p.CourseName)
	// BotDraft sets no draftId.
	assert.Equal(t, "", p.DraftID)
	// pack 0 / pick 0 → cumulative 1-based SelfPick = 0*15 + 0 + 1 = 1.
	assert.Equal(t, 1, p.DraftPack.SelfPick)
	assert.Equal(t, []int{102470, 102645, 102595}, p.DraftPack.PackCards)
}

// TestParseBotDraftStatusPack_LaterPickInLaterPack verifies the SelfPick
// cumulative formula for a non-first pack/pick: pack 1 / pick 0.
func TestParseBotDraftStatusPack_LaterPickInLaterPack(t *testing.T) {
	raw := `{"CurrentModule":"BotDraft","Payload":"{\"Result\":\"Success\",\"EventName\":\"QuickDraft_SOS_20260526\",\"PackNumber\":1,\"PickNumber\":0,\"NumCardsToPick\":1,\"DraftPack\":[\"102524\",\"102613\"],\"PackStyles\":[],\"PickedCards\":[\"102628\"],\"PickedStyles\":[]}"}`
	entry := botDraftPackEntry(t, raw)

	p, err := ParseBotDraftStatusPack(entry)
	require.NoError(t, err)
	require.NotNil(t, p)

	// pack 1 / pick 0 → 1*15 + 0 + 1 = 16.
	assert.Equal(t, 16, p.DraftPack.SelfPick)
	assert.Equal(t, []int{102524, 102613}, p.DraftPack.PackCards)
}

// TestParseBotDraftStatusPack_RejectsNonBotDraftModule ensures the parser does
// not accept a wrapper whose CurrentModule is not "BotDraft".
func TestParseBotDraftStatusPack_RejectsNonBotDraftModule(t *testing.T) {
	raw := `{"CurrentModule":"Draft","Payload":"{\"PackNumber\":0,\"PickNumber\":0,\"DraftPack\":[\"1\"]}"}`
	entry := botDraftPackEntry(t, raw)

	_, err := ParseBotDraftStatusPack(entry)
	require.Error(t, err)
}

// TestParseBotDraftStatusPack_RejectsBadInnerPayload ensures a non-JSON inner
// Payload string is rejected rather than silently producing a zero payload.
func TestParseBotDraftStatusPack_RejectsBadInnerPayload(t *testing.T) {
	raw := `{"CurrentModule":"BotDraft","Payload":"not-json"}`
	entry := botDraftPackEntry(t, raw)

	_, err := ParseBotDraftStatusPack(entry)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// ParseBotDraftPick
// ---------------------------------------------------------------------------

// TestParseBotDraftPick_FirstPick parses the real BotDraftDraftPick request:
// pick 0 of pack 0, one card chosen.
func TestParseBotDraftPick_FirstPick(t *testing.T) {
	raw := `[UnityCrossThreadLogger]==> BotDraftDraftPick {"id":"ca1131f9-2033-418b-a726-4fd9b567af4d","request":"{\"EventName\":\"QuickDraft_SOS_20260526\",\"PickInfo\":{\"EventName\":\"QuickDraft_SOS_20260526\",\"CardIds\":[\"102704\"],\"PackNumber\":0,\"PickNumber\":0}}"}`
	entry := botDraftPackEntry(t, raw)

	p, err := ParseBotDraftPick(entry)
	require.NoError(t, err)
	require.NotNil(t, p)

	assert.Equal(t, "QuickDraft_SOS_20260526", p.CourseName)
	assert.Equal(t, "", p.DraftID)
	assert.Equal(t, 0, p.PackNumber)
	assert.Equal(t, 0, p.PickNumber)
	assert.Equal(t, []int{102704}, p.PickedCards)
}

// TestParseBotDraftPick_LaterPick verifies 0-based pack/pick passthrough on a
// later pick.
func TestParseBotDraftPick_LaterPick(t *testing.T) {
	raw := `[UnityCrossThreadLogger]==> BotDraftDraftPick {"id":"abc","request":"{\"EventName\":\"QuickDraft_SOS_20260526\",\"PickInfo\":{\"CardIds\":[\"102616\",\"102807\"],\"PackNumber\":1,\"PickNumber\":4}}"}`
	entry := botDraftPackEntry(t, raw)

	p, err := ParseBotDraftPick(entry)
	require.NoError(t, err)
	require.NotNil(t, p)

	assert.Equal(t, 1, p.PackNumber)
	assert.Equal(t, 4, p.PickNumber)
	assert.Equal(t, []int{102616, 102807}, p.PickedCards)
}

// TestParseBotDraftPick_RejectsMissingPickInfo ensures a request string without
// PickInfo is rejected (this is the Premier-vs-BotDraft distinguisher).
func TestParseBotDraftPick_RejectsMissingPickInfo(t *testing.T) {
	raw := `{"id":"abc","request":"{\"DraftId\":\"some-uuid\",\"GrpIds\":[102704],\"Pack\":1,\"Pick\":1}"}`
	entry := botDraftPackEntry(t, raw)

	_, err := ParseBotDraftPick(entry)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// New MTGA wire format (#1344, Defect 1) — Payload / request as native objects
// ---------------------------------------------------------------------------
//
// Around MTGA 2026.60 the BotDraftDraftStatus API-response stopped wrapping its
// payload in a JSON string. Instead of:
//
//	{"CurrentModule":"BotDraft","Payload":"{\"EventName\":\"...\", ...}"}
//
// the log now emits:
//
//	{"CurrentModule":"BotDraft","Payload":{"EventName":"...", ...}}
//
// Likewise BotDraftDraftPick request changed from a stringified object to a
// native object. The parsers must handle both shapes so that old-format
// (pre-2026.60) and new-format (2026.60+) daemons both work.

// TestParseBotDraftStatusPack_NewFormat_ObjectPayload parses the new-format
// BotDraft pack line where Payload is a native JSON object (not a string).
// Corpus fixture source: catalog/samples/api-response__BotDraftDraftStatus.json.
func TestParseBotDraftStatusPack_NewFormat_ObjectPayload(t *testing.T) {
	// New-format: Payload is a JSON object, not a stringified inner envelope.
	// DraftPack grpIds are still strings (["102644", "102523", ...]).
	raw := `{"CurrentModule":"BotDraft","Payload":{"Result":"Success","EventName":"QuickDraft_SOS_20260526","DraftStatus":"PickNext","PackNumber":0,"PickNumber":0,"NumCardsToPick":1,"DraftPack":["102644","102523","102702"],"PackStyles":[],"PickedCards":[],"PickedStyles":[]}}`
	entry := botDraftPackEntry(t, raw)

	p, err := ParseBotDraftStatusPack(entry)
	require.NoError(t, err)
	require.NotNil(t, p)

	assert.Equal(t, "QuickDraft_SOS_20260526", p.CourseName)
	assert.Equal(t, "", p.DraftID)
	// pack 0 / pick 0 → cumulative 1-based = 1.
	assert.Equal(t, 1, p.DraftPack.SelfPick)
	assert.Equal(t, []int{102644, 102523, 102702}, p.DraftPack.PackCards)
}

// TestParseBotDraftStatusPack_NewFormat_LaterPack verifies the cumulative
// SelfPick formula for the new format on pack 1 / pick 0.
func TestParseBotDraftStatusPack_NewFormat_LaterPack(t *testing.T) {
	raw := `{"CurrentModule":"BotDraft","Payload":{"EventName":"QuickDraft_SOS_20260526","PackNumber":1,"PickNumber":0,"DraftPack":["102668","102548"],"PickedCards":["102473"]}}`
	entry := botDraftPackEntry(t, raw)

	p, err := ParseBotDraftStatusPack(entry)
	require.NoError(t, err)
	require.NotNil(t, p)

	// pack 1 / pick 0 → 1*15 + 0 + 1 = 16.
	assert.Equal(t, 16, p.DraftPack.SelfPick)
	assert.Equal(t, []int{102668, 102548}, p.DraftPack.PackCards)
}

// TestParseBotDraftPick_NewFormat_ObjectRequest parses the new-format
// BotDraftDraftPick line where request is a native JSON object (not a string).
// Corpus fixture source: catalog/samples/api-request__BotDraftDraftPick.json.
func TestParseBotDraftPick_NewFormat_ObjectRequest(t *testing.T) {
	// New-format: request is a JSON object. CardIds are still strings.
	raw := `{"id":"11111111-0000-4000-8000-000000004762","request":{"EventName":"QuickDraft_SOS_20260526","PickInfo":{"EventName":"QuickDraft_SOS_20260526","CardIds":["102473"],"PackNumber":0,"PickNumber":0}}}`
	entry := botDraftPackEntry(t, raw)

	p, err := ParseBotDraftPick(entry)
	require.NoError(t, err)
	require.NotNil(t, p)

	assert.Equal(t, "QuickDraft_SOS_20260526", p.CourseName)
	assert.Equal(t, "", p.DraftID)
	assert.Equal(t, 0, p.PackNumber)
	assert.Equal(t, 0, p.PickNumber)
	assert.Equal(t, []int{102473}, p.PickedCards)
}

// TestParseBotDraftPick_NewFormat_RejectsObjectRequestWithoutPickInfo ensures
// that a new-format request object without PickInfo is rejected.
func TestParseBotDraftPick_NewFormat_RejectsObjectRequestWithoutPickInfo(t *testing.T) {
	raw := `{"id":"abc","request":{"EventName":"QuickDraft_SOS_20260526"}}`
	entry := botDraftPackEntry(t, raw)

	_, err := ParseBotDraftPick(entry)
	require.Error(t, err)
}
