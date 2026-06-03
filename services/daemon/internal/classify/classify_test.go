package classify

import (
	"testing"

	"github.com/RdHamilton/vault-mtg/services/daemon/internal/logreader"
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
