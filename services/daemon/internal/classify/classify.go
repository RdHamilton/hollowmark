// Package classify provides the canonical log-entry classifier for the daemon.
//
// ClassifyEntry is the single source of truth for mapping a parsed Player.log
// entry to a semantic event-type string. Both the live daemon pipeline
// (internal/daemon) and the Layer-5 replay injector (replay) import this
// package so classifier behaviour is always identical.
package classify

import (
	"strings"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/logreader"
)

// IsMasteryEntry reports whether the log entry is an InventoryInfo entry that
// carries a MasteryPass nested object. Delegates to logreader.IsMasteryEntry.
// Exported from the classify package so handleEntry can call it without
// importing logreader directly where it already imports classify.
func IsMasteryEntry(entry *logreader.LogEntry) bool {
	return logreader.IsMasteryEntry(entry)
}

// ClassifyEntry maps a log entry to a semantic event type string.
// Returns "" if the entry is not a tracked event type.
//
// Precedence (highest to lowest):
//  1. Draft events — Premier probes run first; BotDraft branches only match
//     QuickDraft / bot-draft lines.
//  2. EventGetCoursesV2 completion: Courses[].CurrentModule=Complete with a
//     draft InternalEventName fires "draft.completed" (#1419 Defect E).
//  3. Scene changes (draft.started / draft.completed).
//  4. Match events — matchGameRoomStateChangedEvent path preferred over the
//     legacy CurrentEventState path.
//  5. Player authentication / rank.
//  6. Inventory, quests, collection, deck, periodic rewards.
//  7. GRE game-state messages.
func ClassifyEntry(entry *logreader.LogEntry) string {
	// Premier pack: Draft.Notify line carries draftId + PackCards (comma string).
	if _, hasDraftID := entry.JSON["draftId"]; hasDraftID {
		if _, hasPackCards := entry.JSON["PackCards"]; hasPackCards {
			return "draft.pack"
		}
	}
	// Premier pick: EventPlayerDraftMakePick request carries id + a "request"
	// JSON string with DraftId inside. The Contains shortcut is fine in the
	// classifier; ParsePremierDraftMakePick re-validates strictly.
	if _, hasID := entry.JSON["id"]; hasID {
		if req, hasReq := entry.JSON["request"].(string); hasReq && req != "" {
			if strings.Contains(req, `"DraftId"`) {
				return "draft.pick"
			}
		}
	}

	// BotDraft pack: CurrentModule=BotDraft with a Payload field.
	// Old format (≤2026.59): Payload is a JSON string. New format (≥2026.60):
	// Payload is a JSON object. Both shapes are accepted here; the parser
	// (ParseBotDraftStatusPack) handles the dual-decode. The Premier probes
	// above short-circuit first so this branch only fires for BotDraft lines.
	if mod, ok := entry.JSON["CurrentModule"].(string); ok && mod == "BotDraft" {
		if _, hasPayload := entry.JSON["Payload"]; hasPayload {
			return "draft.pack"
		}
	}
	// BotDraft pick: BotDraftDraftPick request carries PickInfo.
	// Old format: request is a JSON string containing "PickInfo". New format:
	// request is a JSON object with a "PickInfo" key. Both shapes are accepted.
	switch req := entry.JSON["request"].(type) {
	case string:
		// Old format — substring match is safe; ParseBotDraftPick re-validates.
		if strings.Contains(req, `"PickInfo"`) {
			return "draft.pick"
		}
	case map[string]interface{}:
		// New format — PickInfo is a direct map key.
		if _, hasPickInfo := req["PickInfo"]; hasPickInfo {
			return "draft.pick"
		}
	}

	// EventGetCoursesV2 draft completion: a Courses[] array response where at
	// least one course has a draft InternalEventName AND CurrentModule="Complete".
	// Arena emits this when a QuickDraft / PremierDraft event reaches terminal
	// state (all rounds played or player dropped). This is the authoritative
	// completion signal; the scene-change path below is preserved for the
	// pack-picking-done transition but cannot reliably carry session context
	// from a prior daemon invocation (#1419 Defect E).
	if courses, ok := entry.JSON["Courses"].([]interface{}); ok {
		for _, item := range courses {
			c, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			mod, _ := c["CurrentModule"].(string)
			name, _ := c["InternalEventName"].(string)
			if mod == "Complete" && strings.Contains(name, "Draft") {
				return "draft.completed"
			}
		}
	}

	// Scene change (draft start/end).
	if toScene, ok := entry.JSON["toSceneName"].(string); ok {
		if toScene == "Draft" {
			return "draft.started"
		}
		if fromScene, ok2 := entry.JSON["fromSceneName"].(string); ok2 && fromScene == "Draft" {
			return "draft.completed"
		}
	}

	// Match events — prefer the matchGameRoomStateChangedEvent path (single
	// log line with full result data) over the legacy CurrentEventState path.
	if logreader.IsMatchCompletedEntry(entry) {
		return "match.completed"
	}
	if state, ok := entry.JSON["CurrentEventState"].(string); ok {
		switch state {
		case "MatchCompleted":
			return "match.completed"
		case "MatchInProgress":
			return "match.started"
		}
	}

	// Player authentication / profile.
	if _, ok := entry.JSON["authenticateResponse"]; ok {
		return "player.authenticated"
	}

	// Rank update.
	if _, ok := entry.JSON["rankClass"]; ok {
		return "player.rank_updated"
	}

	// Inventory update (Arena 2026.58+: wrapped under "InventoryInfo" key).
	if logreader.IsInventoryEntry(entry) {
		return "inventory.updated"
	}

	// Quest events — check completed before progress (more specific).
	if logreader.IsQuestCompletedEntry(entry) {
		return "quest.completed"
	}
	if logreader.IsQuestProgressEntry(entry) {
		return "quest.progress"
	}

	// Collection snapshot (PlayerInventoryGetPlayerCardsV3).
	if logreader.IsCollectionEntry(entry) {
		return "collection.updated"
	}

	// Deck update (DeckUpsertDeckV2).
	if logreader.IsDeckEntry(entry) {
		return "deck.updated"
	}

	// Course deck submission — fires just before a match starts when the player
	// submits their deck to an event (Ladder, Play, draft, etc.). Carries the
	// Arena deck UUID via CourseDeckSummary.DeckId. Emitted before greToClientEvent
	// so deck linkage is available when match.completed fires.
	if logreader.IsCourseDeckEntry(entry) {
		return "course.deck_submitted"
	}

	// Periodic rewards snapshot (PeriodicRewardsGetStatus response).
	// The sequence ID keys appear directly at the top level of the entry, not
	// nested under ClientPeriodicRewards.
	if logreader.IsPeriodicEntry(entry) {
		return "periodic.updated"
	}

	// GRE game-state messages — buffered into the GRE session manager for batch
	// dispatch. greToClientEvent lines are never dispatched individually.
	if _, ok := entry.JSON["greToClientEvent"]; ok {
		return "greToClientEvent"
	}

	return ""
}
