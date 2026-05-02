package daemon

import (
	"testing"

	"github.com/ramonehamilton/mtga-daemon/internal/logreader"
	"github.com/stretchr/testify/assert"
)

func TestClassifyEntry_DraftPack(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"draftPack": []interface{}{"card1", "card2"}},
	}
	assert.Equal(t, "draft.pack", classifyEntry(entry))
}

func TestClassifyEntry_DraftPick(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"pickedCards": []interface{}{"card1"}},
	}
	assert.Equal(t, "draft.pick", classifyEntry(entry))
}

func TestClassifyEntry_MatchCompleted(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"CurrentEventState": "MatchCompleted"},
	}
	assert.Equal(t, "match.completed", classifyEntry(entry))
}

func TestClassifyEntry_DraftStarted(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"toSceneName": "Draft"},
	}
	assert.Equal(t, "draft.started", classifyEntry(entry))
}

func TestClassifyEntry_PlayerAuthenticated(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"authenticateResponse": map[string]interface{}{"screenName": "Ray"}},
	}
	assert.Equal(t, "player.authenticated", classifyEntry(entry))
}

func TestClassifyEntry_RankUpdated(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"rankClass": "Gold", "rankTier": float64(2)},
	}
	assert.Equal(t, "player.rank_updated", classifyEntry(entry))
}

func TestClassifyEntry_Unknown(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: true,
		JSON:   map[string]interface{}{"someOtherKey": "value"},
	}
	assert.Equal(t, "", classifyEntry(entry))
}

func TestClassifyEntry_NotJSON(t *testing.T) {
	entry := &logreader.LogEntry{
		IsJSON: false,
		Raw:    "plain text",
	}
	assert.Equal(t, "", classifyEntry(entry))
}
