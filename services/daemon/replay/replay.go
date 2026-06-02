// Package replay exposes the daemon log-parse pipeline for use by the
// Layer-5 replay injector (ADR-052 Mode A, ticket #693).
//
// This is the ONLY package in services/daemon that is importable from
// services/bff across the go.work module boundary. All other daemon
// parsing logic lives under internal/ and is inaccessible to external
// modules by Go's internal-package rule.
//
// The public surface is intentionally minimal: ParseLogFile is the only
// entry point. It replicates the classifier+dispatcher pipeline from
// daemon/service.go (handleEntry + classifyEntry) without any network
// I/O or live-daemon state. The output is a slice of ParsedEvent values
// ready to be wrapped in contract.DaemonEvent and inserted into
// daemon_events for BFF projection.
package replay

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/RdHamilton/vault-mtg/services/contract"
	"github.com/RdHamilton/vault-mtg/services/daemon/internal/logreader"
)

// ParsedEvent is the result of parsing a single classified log entry.
// EventType is the semantic type string (e.g. "match.completed",
// "quest.progress"). Payload is the typed, JSON-marshalled payload
// ready for insertion into daemon_events.payload.
type ParsedEvent struct {
	EventType string
	Payload   json.RawMessage
}

// ParseResult is the full result of parsing one log file.
type ParseResult struct {
	// Events contains every classified event parsed from the log.
	Events []ParsedEvent
	// ClientID is the MTGA Arena clientId extracted from the
	// authenticateResponse entry in the log (empty if not present).
	ClientID string
	// MatchCount is the number of successfully parsed match.completed events.
	MatchCount int
	// QuestCount is the number of quest.progress events parsed.
	QuestCount int
	// DeckCount is the number of deck.updated events parsed.
	DeckCount int
	// DraftPackCount is the number of draft.pack events parsed.
	DraftPackCount int
	// DraftPickCount is the number of draft.pick events parsed.
	DraftPickCount int
	// ParseErrors contains non-fatal per-entry parse errors.
	ParseErrors []string
}

// ParseLogFile reads a Player.log archive file from path, classifies and
// parses each entry, and returns a ParseResult.  The function never returns
// an error for individual entry parse failures — those are recorded in
// ParseResult.ParseErrors so the caller can report them without stopping.
//
// A hard error (file not found, unreadable) is returned as the second
// return value.
func ParseLogFile(path string) (*ParseResult, error) {
	r, err := logreader.NewReader(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = r.Close() }()

	result := &ParseResult{}
	var mtgaUserID string // updated when we see authenticateResponse

	for {
		entry, err := r.ReadEntry()
		if err == io.EOF {
			break
		}
		if err != nil {
			result.ParseErrors = append(result.ParseErrors, fmt.Sprintf("read entry: %v", err))
			break
		}
		if entry == nil || !entry.IsJSON {
			continue
		}

		eventType := classifyEntry(entry)
		if eventType == "" {
			continue
		}

		// Extract clientId from authenticateResponse — same logic as
		// Service.handleEntry in daemon/service.go.
		if eventType == "player.authenticated" {
			if resp, ok := entry.JSON["authenticateResponse"].(map[string]interface{}); ok {
				if uid, ok := resp["clientId"].(string); ok && uid != "" {
					mtgaUserID = uid
					result.ClientID = uid
				}
			}
			// player.authenticated events are not inserted into daemon_events
			// (the BFF does not project them); skip payload build.
			continue
		}

		// GRE events are not yet supported in Layer 5 — the GRE session
		// buffer requires stateful accumulation. Skip without error.
		if eventType == "greToClientEvent" {
			continue
		}

		payload, parseErr := buildPayload(entry, eventType, mtgaUserID)
		if parseErr != nil {
			result.ParseErrors = append(result.ParseErrors,
				fmt.Sprintf("%s parse error: %v", eventType, parseErr))
			continue
		}

		raw, err := json.Marshal(payload)
		if err != nil {
			result.ParseErrors = append(result.ParseErrors,
				fmt.Sprintf("%s marshal error: %v", eventType, err))
			continue
		}

		result.Events = append(result.Events, ParsedEvent{
			EventType: eventType,
			Payload:   json.RawMessage(raw),
		})

		switch eventType {
		case "match.completed":
			result.MatchCount++
		case "quest.progress":
			result.QuestCount++
		case "deck.updated":
			result.DeckCount++
		case "draft.pack":
			result.DraftPackCount++
		case "draft.pick":
			result.DraftPickCount++
		}
	}

	return result, nil
}

// classifyEntry maps a log entry to a semantic event type string.
// This is a faithful copy of the classifyEntry function in
// daemon/internal/daemon/service.go — kept in sync manually.
// Returns "" if the entry is not a tracked event.
func classifyEntry(entry *logreader.LogEntry) string {
	// Premier draft pack: Draft.Notify carries draftId + PackCards.
	if _, hasDraftID := entry.JSON["draftId"]; hasDraftID {
		if _, hasPackCards := entry.JSON["PackCards"]; hasPackCards {
			return "draft.pack"
		}
	}
	// Premier draft pick: EventPlayerDraftMakePick request with DraftId.
	if _, hasID := entry.JSON["id"]; hasID {
		if req, ok := entry.JSON["request"].(string); ok && req != "" {
			if strings.Contains(req, `"DraftId"`) {
				return "draft.pick"
			}
		}
	}
	// BotDraft pack.
	if mod, ok := entry.JSON["CurrentModule"].(string); ok && mod == "BotDraft" {
		if _, hasPayload := entry.JSON["Payload"].(string); hasPayload {
			return "draft.pack"
		}
	}
	// BotDraft pick.
	if req, ok := entry.JSON["request"].(string); ok && strings.Contains(req, `"PickInfo"`) {
		return "draft.pick"
	}

	// Match completed.
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

	// Player authentication.
	if _, ok := entry.JSON["authenticateResponse"]; ok {
		return "player.authenticated"
	}

	// Inventory.
	if logreader.IsInventoryEntry(entry) {
		return "inventory.updated"
	}

	// Quest events — completed before progress (more specific first).
	if logreader.IsQuestCompletedEntry(entry) {
		return "quest.completed"
	}
	if logreader.IsQuestProgressEntry(entry) {
		return "quest.progress"
	}

	// Collection snapshot.
	if logreader.IsCollectionEntry(entry) {
		return "collection.updated"
	}

	// Deck update.
	if logreader.IsDeckEntry(entry) {
		return "deck.updated"
	}

	// GRE events.
	if _, ok := entry.JSON["greToClientEvent"]; ok {
		return "greToClientEvent"
	}

	return ""
}

// buildPayload parses the log entry into a typed payload struct.
// Mirrors the switch in Service.handleEntry in daemon/service.go.
func buildPayload(entry *logreader.LogEntry, eventType, mtgaUserID string) (interface{}, error) {
	switch eventType {
	case "match.completed":
		p, err := logreader.ParseMatchCompletedEntry(entry, mtgaUserID)
		if err != nil {
			return nil, err
		}
		return p, nil

	case "quest.progress":
		p, err := logreader.ParseQuestProgressEntry(entry)
		if err != nil {
			return nil, err
		}
		return p, nil

	case "quest.completed":
		p, err := logreader.ParseQuestCompletedEntry(entry)
		if err != nil {
			return nil, err
		}
		return p, nil

	case "inventory.updated":
		p, err := logreader.ParseInventoryEntry(entry)
		if err != nil {
			return nil, err
		}
		return p, nil

	case "deck.updated":
		p, err := logreader.ParseDeckEntry(entry)
		if err != nil {
			return nil, err
		}
		return p, nil

	case "draft.pack":
		var p *logreader.DraftPackPayload
		var err error
		if _, hasDraftID := entry.JSON["draftId"]; hasDraftID {
			p, err = logreader.ParsePremierDraftNotify(entry)
		} else {
			p, err = logreader.ParseBotDraftStatusPack(entry)
		}
		if err != nil {
			return nil, err
		}
		return p, nil

	case "draft.pick":
		var p *logreader.DraftPickPayload
		var err error
		if req, ok := entry.JSON["request"].(string); ok && strings.Contains(req, `"DraftId"`) {
			p, err = logreader.ParsePremierDraftMakePick(entry)
		} else {
			p, err = logreader.ParseBotDraftPick(entry)
		}
		if err != nil {
			return nil, err
		}
		return p, nil

	case "collection.updated":
		p, err := logreader.ParseCollectionEntry(entry)
		if err != nil {
			return nil, err
		}
		return p, nil

	default:
		// Fallback: return raw JSON map so the event is at least recorded.
		return entry.JSON, nil
	}
}

// WrapEvents converts a slice of ParsedEvent into contract.DaemonEvent
// values ready for insertion into daemon_events. accountID is the BFF
// account identifier (the MTGA clientId or a stable test substitute).
// sessionID is a per-replay session identifier for Sequence monotonicity.
func WrapEvents(events []ParsedEvent, accountID, sessionID string) ([]contract.DaemonEvent, error) {
	out := make([]contract.DaemonEvent, 0, len(events))
	for i, e := range events {
		wrapped, err := wrapOne(e, accountID, sessionID, uint64(i+1))
		if err != nil {
			return nil, fmt.Errorf("wrap event %d (%s): %w", i, e.EventType, err)
		}
		out = append(out, wrapped)
	}
	return out, nil
}

// wrapOne wraps a single ParsedEvent into a contract.DaemonEvent using
// a fixed OccurredAt (epoch start) so the test is deterministic.
// Sequence is the 1-based position in the replay sequence.
func wrapOne(e ParsedEvent, accountID, sessionID string, seq uint64) (contract.DaemonEvent, error) {
	eventID := fmt.Sprintf("replay-%s-%04d", sessionID[:8], seq)
	return contract.DaemonEvent{
		Type:      e.EventType,
		AccountID: accountID,
		EventID:   eventID,
		SessionID: sessionID,
		Sequence:  seq,
		// OccurredAt is fixed to a deterministic epoch so double-replay
		// produces identical rows. The BFF projection worker never uses
		// OccurredAt for deduplication; it uses match_id / quest_id /
		// deck_id via ON CONFLICT clauses.
		OccurredAt: deterministicEpoch(),
		Payload:    e.Payload,
	}, nil
}

// deterministicEpoch returns a fixed time.Time used as OccurredAt for all
// replay events. This ensures double-replay produces identical daemon_events
// rows (identical OccurredAt) so the determinism test is not sensitive to
// wall-clock time.
//
// The value 2026-06-02T00:00:00Z matches the corpus snapshot date per ADR-052.
func deterministicEpoch() time.Time {
	return time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
}
