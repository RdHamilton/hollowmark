package main

import (
	"encoding/json"
	"testing"

	"github.com/RdHamilton/hollowmark/services/daemon/replay"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInferDraftMeta verifies that inferDraftMeta correctly extracts the
// set_code and event_name from a draft.pack payload shaped like a
// logreader.DraftPackPayload.
func TestInferDraftMeta(t *testing.T) {
	payload, err := json.Marshal(map[string]interface{}{
		"CourseName": "QuickDraft_SOS_20260526",
		"set_code":   "SOS",
	})
	require.NoError(t, err)

	events := []replay.ParsedEvent{
		{EventType: "draft.pack", Payload: json.RawMessage(payload)},
	}

	setCode, eventName := inferDraftMeta(events)
	assert.Equal(t, "SOS", setCode)
	assert.Equal(t, "QuickDraft_SOS_20260526", eventName)
}

// TestInferDraftMeta_Fallback exercises the case where set_code is absent in
// the payload and must be derived from CourseName.
func TestInferDraftMeta_Fallback(t *testing.T) {
	payload, err := json.Marshal(map[string]interface{}{
		"CourseName": "PremierDraft_BLB_20260601",
	})
	require.NoError(t, err)

	events := []replay.ParsedEvent{
		{EventType: "draft.pack", Payload: json.RawMessage(payload)},
	}

	setCode, eventName := inferDraftMeta(events)
	assert.Equal(t, "BLB", setCode)
	assert.Equal(t, "PremierDraft_BLB_20260601", eventName)
}

// TestInferDraftMeta_NoPackEvents exercises the fallback when no draft.pack
// events are present — should return safe sentinel values.
func TestInferDraftMeta_NoPackEvents(t *testing.T) {
	setCode, eventName := inferDraftMeta(nil)
	assert.Equal(t, "???", setCode)
	assert.Equal(t, "QuickDraft_UNKNOWN", eventName)
}

// TestInferDraftMeta_OnlyNonPackEvents exercises the path where the event
// list is non-empty but contains no draft.pack events.
func TestInferDraftMeta_OnlyNonPackEvents(t *testing.T) {
	events := []replay.ParsedEvent{
		{EventType: "match.completed", Payload: json.RawMessage(`{}`)},
	}
	setCode, eventName := inferDraftMeta(events)
	assert.Equal(t, "???", setCode)
	assert.Equal(t, "QuickDraft_UNKNOWN", eventName)
}
