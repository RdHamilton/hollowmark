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
// draft.completed — EventGetCoursesV2 Courses[].CurrentModule=Complete (#1419)
// ---------------------------------------------------------------------------

// TestClassifyEntry_DraftCompleted_CoursesEnvelope_QuickDraft verifies that an
// EventGetCoursesV2 response containing a QuickDraft course with
// CurrentModule="Complete" is classified as "draft.completed".
//
// This is the primary completion signal for QuickDraft / bot-draft events:
// Arena emits it after the player finishes all three rounds (or drops) and the
// event reaches terminal state. The daemon had no classifier branch for this
// envelope, causing draft_sessions to remain permanently in_progress and the
// Draft History view to appear empty (Defect E, #1419).
//
// The fixture shape mirrors the real EventGetCoursesV2 response observed in
// Ramone's Player.log: a top-level {"Courses": [...]} array where each element
// carries InternalEventName and CurrentModule. We use the QuickDraftEmblem name
// from the active SOS session as a real identifier.
func TestClassifyEntry_DraftCompleted_CoursesEnvelope_QuickDraft(t *testing.T) {
	// Real wire shape: EventGetCoursesV2 response where the QuickDraft course
	// has reached CurrentModule="Complete". Other courses in the same payload
	// may be in various states; only the draft course matters for this classifier.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"Courses": []interface{}{
				// Non-draft courses in the same payload (real data — should not trigger)
				map[string]interface{}{
					"CourseId":          "49bbc08b-814f-477d-8aef-9fdfd63ee187",
					"InternalEventName": "SparkyStarterDeckDuel",
					"CurrentModule":     "CreateMatch",
				},
				map[string]interface{}{
					"CourseId":          "26c87c40-97fc-4235-b9c7-45ffdadac41f",
					"InternalEventName": "Explorer_Ladder",
					"CurrentModule":     "Complete",
				},
				// The QuickDraft course reaching terminal state — this is the trigger.
				map[string]interface{}{
					"CourseId":          "56c6eed8-bec8-4f4c-a8b5-b8beeb94ea1e",
					"InternalEventName": "QuickDraftEmblem_SOS_20260611",
					"CurrentModule":     "Complete",
				},
			},
		},
	}
	assert.Equal(t, "draft.completed", ClassifyEntry(entry))
}

// TestClassifyEntry_DraftCompleted_CoursesEnvelope_PremierDraft verifies that a
// PremierDraft course with CurrentModule="Complete" also classifies as
// "draft.completed". PremierDraft uses the same Courses envelope shape.
func TestClassifyEntry_DraftCompleted_CoursesEnvelope_PremierDraft(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"Courses": []interface{}{
				map[string]interface{}{
					"CourseId":          "aaaaaaaa-0000-0000-0000-000000000001",
					"InternalEventName": "PremierDraft_SOS_20260526",
					"CurrentModule":     "Complete",
				},
			},
		},
	}
	assert.Equal(t, "draft.completed", ClassifyEntry(entry))
}

// TestClassifyEntry_DraftNotCompleted_CoursesEnvelope_NoDraftComplete verifies
// that a Courses envelope where ALL draft-complete markers belong to
// non-draft events does NOT classify as "draft.completed".
//
// Real fixture: the actual Courses array observed in Ramone's Player.log where
// Explorer_Ladder, Alchemy_Ladder, Play, etc. are Complete but there is no draft
// course in the payload at all.
func TestClassifyEntry_DraftNotCompleted_CoursesEnvelope_NoDraftComplete(t *testing.T) {
	// Real Courses payload from Ramone's Player.log — 0 draft courses.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"Courses": []interface{}{
				map[string]interface{}{
					"CourseId":          "49bbc08b-814f-477d-8aef-9fdfd63ee187",
					"InternalEventName": "SparkyStarterDeckDuel",
					"CurrentModule":     "CreateMatch",
				},
				map[string]interface{}{
					"CourseId":          "26c87c40-97fc-4235-b9c7-45ffdadac41f",
					"InternalEventName": "Explorer_Ladder",
					"CurrentModule":     "Complete",
				},
				map[string]interface{}{
					"CourseId":          "18bc15ce-a17f-4a24-846c-d5b208ad77a7",
					"InternalEventName": "Play",
					"CurrentModule":     "Complete",
				},
				map[string]interface{}{
					"CourseId":          "5e431093-bcae-415f-86c0-0243f7a14dca",
					"InternalEventName": "Alchemy_Ladder",
					"CurrentModule":     "Complete",
				},
			},
		},
	}
	assert.NotEqual(t, "draft.completed", ClassifyEntry(entry))
}

// TestClassifyEntry_DraftNotCompleted_CoursesEnvelope_DraftInProgress verifies
// that a Courses envelope where a draft course is present but NOT Complete does
// NOT classify as "draft.completed". Guards against classifying mid-draft
// EventGetCoursesV2 polls.
func TestClassifyEntry_DraftNotCompleted_CoursesEnvelope_DraftInProgress(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"Courses": []interface{}{
				map[string]interface{}{
					"CourseId":          "56c6eed8-bec8-4f4c-a8b5-b8beeb94ea1e",
					"InternalEventName": "QuickDraftEmblem_SOS_20260611",
					"CurrentModule":     "CreateMatch", // still playing matches
				},
			},
		},
	}
	assert.NotEqual(t, "draft.completed", ClassifyEntry(entry))
}

// TestClassifyEntry_DraftCompleted_SceneChange_Regression is a regression guard
// for the existing scene-change path (fromSceneName="Draft"): it must continue
// to fire "draft.completed" so no prior draft.completed signal path is broken.
//
// This scene-change fires when the player finishes picking cards and transitions
// from the Draft scene to DeckBuilder. The Courses-based completion signal
// (this defect's fix) fires later, when all rounds are complete — but the
// scene-change path is preserved to avoid any regression.
func TestClassifyEntry_DraftCompleted_SceneChange_Regression(t *testing.T) {
	// Wire shape: [UnityCrossThreadLogger]Client.SceneChange JSON, prefix stripped
	// by logreader.parseJSON leaving only the inner JSON object.
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON: map[string]interface{}{
			"fromSceneName": "Draft",
			"toSceneName":   "DeckBuilder",
			"initiator":     "System",
			"context":       "deck builder",
		},
	}
	assert.Equal(t, "draft.completed", ClassifyEntry(entry))
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
