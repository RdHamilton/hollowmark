package classify

import (
	"testing"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/logreader"
	"github.com/stretchr/testify/assert"
)

// TestClassifyEntry_CourseDeckSubmitted verifies that a CourseDeck log entry —
// which fires just before a match starts and carries the player's deck UUID —
// is classified as "course.deck_submitted".
//
// This event is the only reliable source of deck_id linkage to a match.
func TestClassifyEntry_CourseDeckSubmitted(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"CourseId":          "bd46df66-ba9d-4dbf-81a5-861ecc483c61",
			"InternalEventName": "Play",
			"CourseDeckSummary": map[string]interface{}{
				"DeckId": "6bfa48aa-9840-48f1-9318-9eacedc3e84a",
				"Name":   "(Standard) Antiquities on the Loose",
			},
			"CourseDeck": map[string]interface{}{
				"MainDeck":  []interface{}{},
				"Sideboard": []interface{}{},
			},
		},
	}
	assert.Equal(t, "course.deck_submitted", ClassifyEntry(entry))
}

// TestClassifyEntry_UnknownEntry verifies that a JSON entry with no recognised
// keys returns "" (not a tracked event type).
func TestClassifyEntry_UnknownEntry(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"unknownKey": "value"},
	}
	assert.Equal(t, "", ClassifyEntry(entry))
}

// TestClassifyEntry_CourseDeckNotCapturedWithoutSummary verifies that a
// CourseDeck entry without a valid CourseDeckSummary.DeckId is NOT classified
// as course.deck_submitted (guards against spurious matches on partial events).
func TestClassifyEntry_CourseDeckNotCapturedWithoutSummary(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"CourseDeck": map[string]interface{}{},
			// CourseDeckSummary present but DeckId missing
			"CourseDeckSummary": map[string]interface{}{},
		},
	}
	assert.NotEqual(t, "course.deck_submitted", ClassifyEntry(entry))
}

// TestClassifyEntry_DeckUpdatedStillClassified verifies that the existing
// deck.updated classification is unaffected by the new course.deck_submitted check.
// IsDeckEntry looks for a top-level "request" string that parses as JSON with
// a Summary.DeckId field — a completely different shape from CourseDeck.
func TestClassifyEntry_DeckUpdatedStillClassified(t *testing.T) {
	// This is the DeckUpsertDeckV2 wire format: a top-level "request" string
	// whose value is a JSON-encoded object with a Summary.DeckId field.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"request": `{"Summary":{"DeckId":"abc-123","Name":"Test Deck"},"Deck":{"MainDeck":[]}}`,
		},
	}
	assert.Equal(t, "deck.updated", ClassifyEntry(entry))
}

// ---------------------------------------------------------------------------
// BotDraft pack classifier — old vs new MTGA wire format (#1344, Defect 1)
// ---------------------------------------------------------------------------

// TestClassifyEntry_BotDraftPack_OldFormat_StringPayload verifies the old wire
// shape (CurrentModule=BotDraft + Payload as a JSON string) still classifies as
// draft.pack — regression guard for pre-2026.60 clients.
func TestClassifyEntry_BotDraftPack_OldFormat_StringPayload(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"CurrentModule": "BotDraft",
			"Payload":       `{"EventName":"QuickDraft_SOS_20260526","PackNumber":0,"PickNumber":0,"DraftPack":["102470"]}`,
		},
	}
	assert.Equal(t, "draft.pack", ClassifyEntry(entry))
}

// TestClassifyEntry_BotDraftPack_NewFormat_ObjectPayload verifies the new wire
// shape (CurrentModule=BotDraft + Payload as a JSON object, not a string)
// classifies as draft.pack.
//
// MTGA drifted from a doubly-nested stringified envelope to native objects
// around 2026.60. Without the Defect-1 fix ClassifyEntry returns "" for all
// QuickDraft / BotDraft pack lines on new-format clients, making draft packs
// completely invisible to the daemon.
func TestClassifyEntry_BotDraftPack_NewFormat_ObjectPayload(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"CurrentModule": "BotDraft",
			"Payload": map[string]interface{}{
				"EventName":  "QuickDraft_SOS_20260526",
				"PackNumber": float64(0),
				"PickNumber": float64(0),
				"DraftPack":  []interface{}{"102470", "102645"},
			},
		},
	}
	assert.Equal(t, "draft.pack", ClassifyEntry(entry))
}

// ---------------------------------------------------------------------------
// BotDraft pick classifier — old vs new MTGA wire format (#1344, Defect 1)
// ---------------------------------------------------------------------------

// TestClassifyEntry_BotDraftPick_OldFormat_StringRequest verifies the old wire
// shape (request as a JSON string containing PickInfo) still classifies as
// draft.pick — regression guard for pre-2026.60 clients.
func TestClassifyEntry_BotDraftPick_OldFormat_StringRequest(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"id":      "ca1131f9-2033-418b-a726-4fd9b567af4d",
			"request": `{"EventName":"QuickDraft_SOS_20260526","PickInfo":{"CardIds":["102704"],"PackNumber":0,"PickNumber":0}}`,
		},
	}
	assert.Equal(t, "draft.pick", ClassifyEntry(entry))
}

// TestClassifyEntry_BotDraftPick_NewFormat_ObjectRequest verifies the new wire
// shape (request as a JSON object, not a string) classifies as draft.pick.
//
// MTGA drifted from a doubly-nested stringified request to a native object
// around 2026.60. Without the Defect-1 fix ClassifyEntry returns "" for all
// BotDraft pick lines on new-format clients, making picks invisible to the
// daemon. H1 (Ray's plan gate) — the PickInfo substring match on a string
// request — does NOT fire on the new format because the type assertion
// entry.JSON["request"].(string) returns ("", false).
func TestClassifyEntry_BotDraftPick_NewFormat_ObjectRequest(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"id": "11111111-0000-4000-8000-000000004762",
			"request": map[string]interface{}{
				"EventName": "QuickDraft_SOS_20260526",
				"PickInfo": map[string]interface{}{
					"EventName":  "QuickDraft_SOS_20260526",
					"CardIds":    []interface{}{"102473"},
					"PackNumber": float64(0),
					"PickNumber": float64(0),
				},
			},
		},
	}
	assert.Equal(t, "draft.pick", ClassifyEntry(entry))
}

// ---------------------------------------------------------------------------
// periodic.updated classifier (#1344 quest/mastery fix)
// ---------------------------------------------------------------------------

// TestClassifyEntry_PeriodicUpdated verifies that a PeriodicRewardsGetStatus
// response carrying the top-level "_dailyRewardSequenceId" key is classified
// as "periodic.updated".
func TestClassifyEntry_PeriodicUpdated(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"_dailyRewardSequenceId":         float64(4),
			"_weeklyRewardSequenceId":        float64(7),
			"_dailyRewardResetTimestamp":     "2026-06-12T09:00:00Z",
			"_weeklyRewardResetTimestamp":    "2026-06-09T09:00:00Z",
			"_dailyRewardChestDescriptions":  map[string]interface{}{},
			"_weeklyRewardChestDescriptions": map[string]interface{}{},
		},
	}
	assert.Equal(t, "periodic.updated", ClassifyEntry(entry))
}

// TestClassifyEntry_PeriodicUpdated_WeeklyOnly verifies that
// _weeklyRewardSequenceId alone is sufficient to classify the event.
func TestClassifyEntry_PeriodicUpdated_WeeklyOnly(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"_weeklyRewardSequenceId":        float64(3),
			"_weeklyRewardChestDescriptions": map[string]interface{}{},
		},
	}
	assert.Equal(t, "periodic.updated", ClassifyEntry(entry))
}

// TestClassifyEntry_PeriodicUpdated_ChestDescriptionsAloneNotMatched verifies
// that a PeriodicRewards entry with only chest descriptions (no sequence IDs)
// is NOT classified as periodic.updated.
func TestClassifyEntry_PeriodicUpdated_ChestDescriptionsAloneNotMatched(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"_dailyRewardChestDescriptions": map[string]interface{}{}},
	}
	assert.NotEqual(t, "periodic.updated", ClassifyEntry(entry))
}

// ---------------------------------------------------------------------------
// mastery.updated classifier (#1344 quest/mastery fix)
// ---------------------------------------------------------------------------

// TestIsMasteryEntry_WithMasteryPass verifies that an InventoryInfo entry
// containing a MasteryPass nested object is reported as a mastery entry.
func TestIsMasteryEntry_WithMasteryPass(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems": float64(1200),
				"Gold": float64(5000),
				"MasteryPass": map[string]interface{}{
					"CurrentLevel": float64(18),
					"PassType":     "Standard",
					"MaxLevel":     float64(80),
				},
			},
		},
	}
	assert.True(t, IsMasteryEntry(entry))
}

// TestIsMasteryEntry_WithoutMasteryPass verifies that an InventoryInfo entry
// lacking a MasteryPass key is NOT a mastery entry.
func TestIsMasteryEntry_WithoutMasteryPass(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"InventoryInfo": map[string]interface{}{
				"Gems": float64(1200),
				"Gold": float64(5000),
			},
		},
	}
	assert.False(t, IsMasteryEntry(entry))
}

// TestIsMasteryEntry_NonInventoryEntry verifies that a non-inventory entry
// (no InventoryInfo key) is not a mastery entry.
func TestIsMasteryEntry_NonInventoryEntry(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"quests": []interface{}{}},
	}
	assert.False(t, IsMasteryEntry(entry))
}
