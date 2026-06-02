// Package classify provides the canonical log-entry classifier for the daemon.
//
// ClassifyEntry is the single source of truth for mapping a parsed Player.log
// entry to a semantic event-type string. Both the live daemon pipeline
// (internal/daemon) and the Layer-5 replay injector (replay) import this
// package so classifier behaviour is always identical.
package classify

import (
	"strings"

	"github.com/RdHamilton/vault-mtg/services/daemon/internal/logreader"
)

// ClassifyEntry maps a log entry to a semantic event type string.
// Returns "" if the entry is not a tracked event type.
//
// Precedence (highest to lowest):
//  1. Draft events — Premier probes run first; BotDraft branches only match
//     QuickDraft / bot-draft lines.
//  2. Scene changes (draft.started / draft.completed).
//  3. Match events — matchGameRoomStateChangedEvent path preferred over the
//     legacy CurrentEventState path.
//  4. Player authentication / rank.
//  5. Inventory, quests, collection, deck.
//  6. GRE game-state messages.
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

	// BotDraft pack: CurrentModule=BotDraft with a stringified Payload envelope
	// (QuickDraft / bot drafts). The Premier probes above short-circuit first.
	if mod, ok := entry.JSON["CurrentModule"].(string); ok && mod == "BotDraft" {
		if _, hasPayload := entry.JSON["Payload"].(string); hasPayload {
			return "draft.pack"
		}
	}
	// BotDraft pick: BotDraftDraftPick request carries a "request" JSON string
	// containing a PickInfo block. PickInfo distinguishes it from the Premier
	// EventPlayerDraftMakePick request (which carries DraftId).
	if req, ok := entry.JSON["request"].(string); ok && strings.Contains(req, `"PickInfo"`) {
		return "draft.pick"
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

	// GRE game-state messages — buffered into the GRE session manager for batch
	// dispatch. greToClientEvent lines are never dispatched individually.
	if _, ok := entry.JSON["greToClientEvent"]; ok {
		return "greToClientEvent"
	}

	return ""
}
