// Package replay exposes the daemon log-parse pipeline for use by the
// Layer-5 replay injector (ADR-052 Mode A, ticket #693).
//
// This is the ONLY package in services/daemon that is importable from
// services/bff across the go.work module boundary. All other daemon
// parsing logic lives under internal/ and is inaccessible to external
// modules by Go's internal-package rule.
//
// The public surface is intentionally minimal: ParseLogFile is the only
// entry point. It uses classify.ClassifyEntry — the single source of
// truth for log-entry classification — so the replay injector and the
// live daemon pipeline always agree on event types.
//
// GRE session buffering (ticket #808):
// ParseLogFile now wires a per-file gre.Manager so that greToClientEvent
// lines are buffered and flushed as match.game_ended events.  When
// match.completed fires the session is flushed non-partially with the
// authoritative match_id (mirrors the live daemon path in service.go).
// At EOF, FlushAll flushes any remaining buffered entries.  The resulting
// match.game_ended ParsedEvents are appended to the result so the Layer-5
// injector can project game_plays rows.
package replay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	logparse "github.com/RdHamilton/hollowmark/pkg/logparse"
	"github.com/RdHamilton/hollowmark/services/contract"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/classify"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/gre"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/logreader"
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
	// This includes match.game_ended events synthesised from GRE buffers.
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
	// GREEventCount is the number of greToClientEvent lines buffered.
	// Non-zero after the GRE session manager is wired.
	GREEventCount int
	// GameEndedCount is the number of match.game_ended events produced by the
	// GRE session manager flush path.
	GameEndedCount int
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
//
// GRE buffering: greToClientEvent lines are routed through a per-file
// gre.Manager with a high flush threshold (so no partial flushes occur).
// When match.completed fires, FlushSession is called non-partially with the
// authoritative match_id; at EOF FlushAll flushes any remainder.  The flush
// callback appends match.game_ended ParsedEvents to the result — these carry
// the per-turn CardPlays that project into game_plays rows.
func ParseLogFile(path string) (*ParseResult, error) {
	r, err := logreader.NewReader(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = r.Close() }()

	result := &ParseResult{}
	var mtgaUserID string // updated when we see authenticateResponse

	// sessionID is the per-file stable GRE session identifier.
	// It must be derived from path so that replaying the same file twice
	// produces the same match.game_ended event_ids (determinism requirement).
	sessionID := greSessionID(path)

	// Wire a gre.Manager with a very high threshold (no partial mid-file
	// flushes).  The flush callback builds a match.game_ended ParsedEvent
	// and appends it to result.Events.
	greManager := gre.NewManager(gre.ManagerConfig{
		FlushThreshold: 100_000, // effectively never triggers mid-file
		StaleMinutes:   60,
		Flush: func(_ context.Context, sid, matchID string, entries []json.RawMessage, partial bool) error {
			evt, buildErr := buildGameEndedEvent(sid, matchID, entries, partial)
			if buildErr != nil {
				result.ParseErrors = append(result.ParseErrors,
					fmt.Sprintf("gre flush build error: %v", buildErr))
				return nil
			}
			if evt == nil {
				// Empty buffer — no event produced.
				return nil
			}
			result.Events = append(result.Events, *evt)
			result.GameEndedCount++
			return nil
		},
	})

	ctx := context.Background()

	for {
		entry, readErr := r.ReadEntry()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			result.ParseErrors = append(result.ParseErrors, fmt.Sprintf("read entry: %v", readErr))
			break
		}
		if entry == nil || !entry.IsJSON {
			continue
		}

		eventType := classify.ClassifyEntry(entry)
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

		// GRE events are buffered in the session manager (not inserted
		// individually).  When match.completed fires below, FlushSession is
		// called non-partially with the authoritative match_id.
		if eventType == "greToClientEvent" {
			result.GREEventCount++
			if appendErr := greManager.Append(ctx, sessionID, json.RawMessage(entry.Raw)); appendErr != nil {
				result.ParseErrors = append(result.ParseErrors,
					fmt.Sprintf("gre append error: %v", appendErr))
			}
			continue
		}

		payload, parseErr := buildPayload(entry, eventType, mtgaUserID)
		if parseErr != nil {
			result.ParseErrors = append(result.ParseErrors,
				fmt.Sprintf("%s parse error: %v", eventType, parseErr))
			continue
		}

		raw, marshalErr := json.Marshal(payload)
		if marshalErr != nil {
			result.ParseErrors = append(result.ParseErrors,
				fmt.Sprintf("%s marshal error: %v", eventType, marshalErr))
			continue
		}

		result.Events = append(result.Events, ParsedEvent{
			EventType: eventType,
			Payload:   json.RawMessage(raw),
		})

		switch eventType {
		case "match.completed":
			result.MatchCount++
			// Non-partial game-end flush: mirrors service.go line 1704.
			// Extract match_id from the parsed match payload so the GRE
			// session is anchored to the authoritative match_id.
			matchID := extractMatchID(raw)
			if flushErr := greManager.FlushSession(ctx, sessionID, matchID, false); flushErr != nil {
				result.ParseErrors = append(result.ParseErrors,
					fmt.Sprintf("gre FlushSession match_id=%s: %v", matchID, flushErr))
			}
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

	// Flush any buffered GRE entries that did not have a paired match.completed
	// (threshold flushes, log truncation, end-of-session).
	greManager.FlushAll(ctx)

	return result, nil
}

// greSessionID derives a stable per-file GRE session identifier from the
// log file path.  Stability is required so that replaying the same corpus
// twice produces identical match.game_ended event_ids (determinism guarantee).
func greSessionID(path string) string {
	// Use last 28 chars of path (enough to uniquely identify the file in the
	// corpus) formatted to the 36-char session ID slot used by WrapEvents.
	base := path
	if idx := strings.LastIndexByte(path, '/'); idx >= 0 {
		base = path[idx+1:]
	}
	base = strings.TrimSuffix(base, ".log")
	// Sanitise to alphanumeric + dash.
	var sb strings.Builder
	for _, ch := range base {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' {
			sb.WriteRune(ch)
		} else {
			sb.WriteRune('-')
		}
	}
	s := sb.String()
	// Truncate or pad to 36 characters.
	for len(s) < 36 {
		s += "0"
	}
	if len(s) > 36 {
		s = s[len(s)-36:]
	}
	return s
}

// extractMatchID extracts the match_id field from a serialised
// match.completed payload JSON.  Returns "" when not present.
func extractMatchID(raw []byte) string {
	var p struct {
		MatchID string `json:"match_id"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return ""
	}
	return p.MatchID
}

// buildGameEndedEvent converts the GRE session buffer flush into a
// match.game_ended ParsedEvent using the same logic as flushGREBuffer in
// service.go.  Returns nil when the buffer is empty (nothing to emit).
func buildGameEndedEvent(sessionID, matchID string, entries []json.RawMessage, partial bool) (*ParsedEvent, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	// Convert raw GRE JSON to LogEntry values — same as service.go.
	logEntries := make([]*logparse.LogEntry, 0, len(entries))
	for _, raw := range entries {
		e := logparse.ParseLine(string(raw))
		if e.IsJSON {
			logEntries = append(logEntries, e)
		}
	}

	playerConn := logparse.GetPlayerSeatID(logEntries)
	result, err := logparse.ParseGamePlaysResult(logEntries, playerConn)
	if err != nil {
		// Log but do not hard-fail — mirrors service.go behaviour.
		return nil, fmt.Errorf("ParseGamePlaysResult: %w", err)
	}

	payload := contract.GamePlayPayload{
		Partial:       partial,
		SchemaVersion: 2,
		LifeChanges:   []contract.LifeChangeEntry{},
	}

	maxTurn := 0
	for _, play := range result.Plays {
		if play.TurnNumber > maxTurn {
			maxTurn = play.TurnNumber
		}
		if payload.MatchID == "" && play.MatchID != "" {
			payload.MatchID = play.MatchID
		}
		if payload.GameNumber == 0 && play.GameNumber > 0 {
			payload.GameNumber = play.GameNumber
		}
		switch play.ActionType {
		case "life_change":
			payload.LifeChanges = append(payload.LifeChanges, contract.LifeChangeEntry{
				TeamID:     play.TeamID,
				LifeTotal:  play.LifeTo,
				Delta:      play.LifeTo - play.LifeFrom,
				TurnNumber: play.TurnNumber,
			})
		default:
			payload.CardPlays = append(payload.CardPlays, contract.CardPlayEntry{
				GameNumber: play.GameNumber,
				TurnNumber: play.TurnNumber,
				Phase:      play.Phase,
				ArenaID:    play.CardID,
				PlayerType: play.PlayerType,
				ActionType: play.ActionType,
				ZoneFrom:   play.ZoneFrom,
				ZoneTo:     play.ZoneTo,
			})
		}
	}
	if maxTurn > 0 {
		payload.TurnCount = maxTurn
	}

	for _, snap := range result.Snapshots {
		if payload.MatchID == "" && snap.MatchID != "" {
			payload.MatchID = snap.MatchID
		}
		if payload.GameNumber == 0 && snap.GameNumber > 0 {
			payload.GameNumber = snap.GameNumber
		}
		if snap.TurnNumber > payload.TurnCount {
			payload.TurnCount = snap.TurnNumber
		}
		payload.Snapshots = append(payload.Snapshots, contract.GameSnapshotEntry{
			GameNumber:          snap.GameNumber,
			TurnNumber:          snap.TurnNumber,
			PlayerLife:          snap.PlayerLife,
			OpponentLife:        snap.OpponentLife,
			PlayerCardsInHand:   snap.PlayerCardsInHand,
			OpponentCardsInHand: snap.OpponentCardsInHand,
			PlayerLandsInPlay:   snap.PlayerLandsInPlay,
			OpponentLandsInPlay: snap.OpponentLandsInPlay,
		})
	}

	for _, oc := range result.OpponentCards {
		payload.OpponentCards = append(payload.OpponentCards, contract.OpponentCardEntry{
			ArenaID:       oc.CardID,
			ZoneObserved:  oc.ZoneObserved,
			TurnFirstSeen: oc.TurnFirstSeen,
			TimesSeen:     oc.TimesSeen,
		})
	}

	for _, cc := range result.CounterChanges {
		payload.CounterChanges = append(payload.CounterChanges, contract.CounterChangeEntry{
			InstanceID:  cc.InstanceID,
			ArenaID:     cc.ArenaID,
			CounterType: cc.CounterType,
			Count:       cc.Count,
			Delta:       cc.Delta,
			Controller:  cc.Controller,
			TurnNumber:  cc.TurnNumber,
		})
	}

	if result.Mulligan != nil {
		payload.Mulligan = &contract.MulliganEntry{
			OpeningHandSize: result.Mulligan.OpeningHandSize,
			MulliganCount:   result.Mulligan.MulliganCount,
			KeptCardIDs:     result.Mulligan.KeptCardIDs,
			BottomedCardIDs: result.Mulligan.BottomedCardIDs,
		}
	}

	if result.FirstTurnActivePlayerSeatID > 0 && playerConn != nil {
		onPlay := result.FirstTurnActivePlayerSeatID == playerConn.SeatID
		payload.PlayerOnPlay = &onPlay
	}

	// Match-id fallback — same as service.go (#807): GRE-derived match_id
	// keeps precedence; explicit arg fills in when GRE match_id is absent.
	if payload.MatchID == "" && matchID != "" {
		payload.MatchID = matchID
	}

	_ = sessionID // sessionID is used by the caller for routing; not embedded

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal GamePlayPayload: %w", err)
	}

	return &ParsedEvent{
		EventType: "match.game_ended",
		Payload:   json.RawMessage(raw),
	}, nil
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
