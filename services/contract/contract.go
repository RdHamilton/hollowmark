package contract

import (
	"encoding/json"
	"time"
)

// DaemonEvent is the wire type the daemon sends to the BFF /v1/ingest/events endpoint.
type DaemonEvent struct {
	Type       string          `json:"type"`
	AccountID  string          `json:"account_id"`
	EventID    string          `json:"event_id"`
	SessionID  string          `json:"session_id"`
	Sequence   uint64          `json:"sequence"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}

// SyncRatingsPayload is embedded in a DaemonEvent with Type "sync:ratings".
type SyncRatingsPayload struct {
	SetCode      string `json:"set_code"`
	CardsUpdated int    `json:"cards_updated"`
	Source       string `json:"source"`
}

// SyncCardMetadataPayload is embedded in a DaemonEvent with Type "sync:card_metadata".
type SyncCardMetadataPayload struct {
	SetCode      string `json:"set_code"`
	CardsAdded   int    `json:"cards_added"`
	CardsUpdated int    `json:"cards_updated"`
}

// DraftEventPayload is embedded in a DaemonEvent with Type "draft:pick" or similar.
type DraftEventPayload struct {
	DraftID    string `json:"draft_id"`
	SetCode    string `json:"set_code"`
	PackNumber int    `json:"pack_number"`
	PickNumber int    `json:"pick_number"`
}

// MatchEventPayload is embedded in a DaemonEvent with Type "match:result" or similar.
type MatchEventPayload struct {
	MatchID      string `json:"match_id"`
	Format       string `json:"format"`
	OpponentName string `json:"opponent_name"`
}

// InventoryBooster represents a single booster pack in the player's inventory.
// Arena 2026.58+: on-wire field names are PascalCase (CollationId, SetCode, Count).
type InventoryBooster struct {
	CollationID int    `json:"collation_id"`
	SetCode     string `json:"set_code"`
	Count       int    `json:"count"`
}

// DeckSummary carries the identity and format of a single deck as reported by
// the DeckSummaries array in the Arena login blob (top-level sibling of
// InventoryInfo). It intentionally carries no card list — that comes from the
// separate DeckUpsertDeckV2 event which the daemon parses via ParseDeckEntry.
// Used by InventoryUpdatedPayload.Decks (additive field, #1337).
type DeckSummary struct {
	DeckID string `json:"deck_id"`
	Name   string `json:"name"`
	// Format is the value of the Attribute with name=="Format" in the summary's
	// Attributes array, e.g. "Standard", "Alchemy", "Historic". Empty when the
	// Attribute is absent.
	Format string `json:"format"`
}

// InventoryUpdatedPayload is embedded in a DaemonEvent with Type "inventory.updated".
// It carries the player's current gem/gold/wildcard counts, booster holdings,
// and — when parsed from a login blob — the full deck header library (Decks).
//
// Decks is additive: older daemon versions that do not populate it will produce
// an empty slice; the BFF projection worker skips the fan-out when Decks is
// empty. No contract version bump required (JSON omitempty on the wire).
type InventoryUpdatedPayload struct {
	Gems               int                `json:"gems"`
	Gold               int                `json:"gold"`
	TotalVaultProgress int                `json:"total_vault_progress"`
	WildCardCommons    int                `json:"wild_card_commons"`
	WildCardUncommons  int                `json:"wild_card_uncommons"`
	WildCardRares      int                `json:"wild_card_rares"`
	WildCardMythics    int                `json:"wild_card_mythics"`
	Boosters           []InventoryBooster `json:"boosters"`
	// Decks carries the player's full deck library headers from the Arena login
	// blob DeckSummaries array. Populated by the daemon's ParseInventoryEntry
	// when DeckSummaries is present (Arena 2026.60+). Omitted from the wire
	// payload when empty so older BFF versions ignore it gracefully.
	Decks []DeckSummary `json:"decks,omitempty"`
}

// QuestProgressPayload is embedded in a DaemonEvent with Type "quest.progress".
// It carries the state of all active quests from a QuestGetQuests response.
type QuestProgressPayload struct {
	Quests []QuestEntry `json:"quests"`
}

// QuestCompletedPayload is embedded in a DaemonEvent with Type "quest.completed".
// It is emitted when at least one quest in a QuestGetQuests response has
// endingProgress >= goal (i.e. the player has met the quest's completion target).
type QuestCompletedPayload struct {
	QuestID          string `json:"quest_id"`
	QuestName        string `json:"quest_name"`
	Progress         int    `json:"progress"`
	Goal             int    `json:"goal"`
	XPReward         int    `json:"xp_reward"`
	CompletionSource string `json:"completion_source"`
}

// QuestEntry represents a single quest within a QuestProgressPayload.
type QuestEntry struct {
	QuestID   string `json:"quest_id"`
	QuestName string `json:"quest_name"`
	Progress  int    `json:"progress"`
	Goal      int    `json:"goal"`
	CanSwap   bool   `json:"can_swap"`
}

// DeckCard represents a single card slot in a deck — arena grpId plus quantity.
type DeckCard struct {
	ArenaID  int `json:"arena_id"`
	Quantity int `json:"quantity"`
}

// DeckUpdatedPayload is embedded in a DaemonEvent with Type "deck.updated".
// It carries the identity and card list for a single player deck as reported
// by a DeckUpsertDeckV2 log entry.
type DeckUpdatedPayload struct {
	DeckID string     `json:"deck_id"`
	Name   string     `json:"name"`
	Format string     `json:"format"`
	Cards  []DeckCard `json:"cards"`
}

// CollectionCard represents a single card entry in a collection snapshot.
// ArenaID is the MTGA numeric card identifier; Count is the number of copies
// the player owns.
type CollectionCard struct {
	ArenaID int `json:"arena_id"`
	Count   int `json:"count"`
}

// CollectionUpdatedPayload is embedded in a DaemonEvent with Type
// "collection.updated". It carries a full snapshot of the player's collection
// as returned by PlayerInventoryGetPlayerCardsV3. The daemon may compute a
// delta before dispatch; when a delta is sent, Cards contains only the changed
// entries and IsDelta is true.
type CollectionUpdatedPayload struct {
	Cards   []CollectionCard `json:"cards"`
	IsDelta bool             `json:"is_delta"`
}

// MatchResult represents a single result entry from the MTGA resultList.
// Scope distinguishes whether the result applies to a single game or the
// overall match ("MatchScope_Game" / "MatchScope_Match").
type MatchResult struct {
	Scope         string `json:"scope"`
	Result        string `json:"result"`
	WinningTeamID int    `json:"winning_team_id"`
	Reason        string `json:"reason"`
}

// MatchCompletedPayload is embedded in a DaemonEvent with Type
// "match.completed". It is derived from the matchGameRoomStateChangedEvent
// with stateType "MatchGameRoomStateType_MatchCompleted" that Arena emits
// at the end of every match.
//
// WinningTeamID is the teamId of the winning side as reported in the
// MatchScope_Match result entry (0 if indeterminate).
// ResultList carries every result entry from finalMatchResult.resultList.
// OpponentName is the playerName of the opponent as listed in reservedPlayers;
// it is empty when the daemon cannot determine which seat belongs to the local
// player.
// Format is sourced from the eventId field in gameRoomConfig (e.g. "Ladder",
// "QuickDraft_SOS_20260430"); it is empty when absent.
//
// Result, PlayerTeamID, PlayerWins, and OpponentWins are derived when the
// daemon knows the local player's MTGA userId (from a preceding
// player.authenticated event). They are empty/zero when the player cannot
// be identified — the projection worker falls back to WinningTeamID +
// PlayerTeamID in that case.
type MatchCompletedPayload struct {
	MatchID       string        `json:"match_id"`
	WinningTeamID int           `json:"winning_team_id"`
	ResultList    []MatchResult `json:"result_list"`
	Format        string        `json:"format"`
	OpponentName  string        `json:"opponent_name"`
	// Derived fields — populated when the local player's MTGA userId is known.
	Result       string `json:"result"`         // "win" or "loss"; empty when indeterminate
	PlayerTeamID int    `json:"player_team_id"` // 0 when indeterminate
	PlayerWins   int    `json:"player_wins"`
	OpponentWins int    `json:"opponent_wins"`
	// DraftSessionID is the draft_sessions.id that produced the deck used in
	// this match. Non-nil when the daemon has an active in-memory draft session
	// whose CourseName matches the match's Format (event_name). Nil for all
	// constructed/ladder/other non-draft matches, and nil when the daemon
	// restarted between draft and match play.
	DraftSessionID *string `json:"draft_session_id,omitempty"`
	// DeckID is the MTGA Arena deck UUID extracted from the CourseDeck log entry
	// that fires when the player submits a deck to a match. It is derived from
	// the CourseDeckSummary.DeckId field. Empty when the daemon did not observe
	// a CourseDeck entry before this match (e.g. daemon started mid-match, or
	// the event format does not emit a CourseDeck).
	DeckID string `json:"deck_id,omitempty"`
}

// LifeChangeEntry records a single life-total mutation observed in a game.
type LifeChangeEntry struct {
	TeamID     int `json:"team_id"`
	LifeTotal  int `json:"life_total"`
	Delta      int `json:"delta"`
	TurnNumber int `json:"turn_number"`
}

// CardPlayEntry records a single card play, land drop, combat declaration, or zone transition
// observed during a game. Stored as game_summaries.card_plays_json (ADR-046).
type CardPlayEntry struct {
	GameNumber int    `json:"game_number"`
	TurnNumber int    `json:"turn_number"`
	Phase      string `json:"phase"`
	ArenaID    int    `json:"arena_id"`
	PlayerType string `json:"player_type"` // "player" or "opponent"
	ActionType string `json:"action_type"` // "play_card", "land_drop", "cast_spell", "attack", "block", etc.
	ZoneFrom   string `json:"zone_from"`
	ZoneTo     string `json:"zone_to"`
}

// GameSnapshotEntry captures per-turn board state.
// Stored as game_summaries.life_arc_json (ADR-046) and used for the variance score heuristic.
type GameSnapshotEntry struct {
	GameNumber          int `json:"game_number"`
	TurnNumber          int `json:"turn_number"`
	PlayerLife          int `json:"player_life"`
	OpponentLife        int `json:"opponent_life"`
	PlayerCardsInHand   int `json:"player_cards_in_hand"`
	OpponentCardsInHand int `json:"opponent_cards_in_hand"`
	PlayerLandsInPlay   int `json:"player_lands_in_play"`
	OpponentLandsInPlay int `json:"opponent_lands_in_play"`
}

// OpponentCardEntry records an opponent card that was observed during a game.
// Foundation for opponent archetype identification (ADR-046 §5).
// ArenaID is the MTGA GRPId (grpId) as observed in GRE game objects.
type OpponentCardEntry struct {
	ArenaID       int    `json:"arena_id"`
	ZoneObserved  string `json:"zone_observed"`
	TurnFirstSeen int    `json:"turn_first_seen"`
	TimesSeen     int    `json:"times_seen"`
}

// CounterChangeEntry records a single counter mutation observed on a permanent
// between consecutive GRE game state messages. Stored in
// daemon_events.payload (JSONB) for projection into game_event_counters
// (ADR-046 A2.1, v0.3.7).
//
// InstanceID is the GRE instanceId of the permanent that holds the counter.
// ArenaID is the GRPId (card ID) of the permanent.
// CounterType is the raw counter type string from the GRE (e.g. "loyalty",
// "+1/+1", "poison").
// Count is the new total after the change.
// Delta is Count minus the previous Count (negative for decrements).
// Controller is "player" or "opponent" relative to the local player.
// TurnNumber is the turn on which the change was observed.
type CounterChangeEntry struct {
	InstanceID  int    `json:"instance_id"`
	ArenaID     int    `json:"arena_id"`
	CounterType string `json:"counter_type"`
	Count       int    `json:"count"`
	Delta       int    `json:"delta"`
	Controller  string `json:"controller"`
	TurnNumber  int    `json:"turn_number"`
}

// MulliganEntry records the opening hand decision for a single game.
// Stored in daemon_events.payload (JSONB) for projection into
// game_summaries.mulligan_json (ADR-046 A2.2, v0.3.8).
//
// OpeningHandSize is the initial hand size (7 for a fresh game, less after
// mulligans under London rules because maxHandSize decrements by 1 per
// mulligan taken).
// MulliganCount is the total number of mulligans taken (0 = kept opening 7).
// KeptCardIDs contains the GRPIds of cards in hand at game start.
// BottomedCardIDs contains the GRPIds of cards placed on the bottom of the
// library under London mulligan rules (empty when MulliganCount == 0).
type MulliganEntry struct {
	OpeningHandSize int   `json:"opening_hand_size"`
	MulliganCount   int   `json:"mulligan_count"`
	KeptCardIDs     []int `json:"kept_card_ids"`
	BottomedCardIDs []int `json:"bottomed_card_ids"`
}

// GamePlayPayload is embedded in a DaemonEvent with Type "match.game_ended".
// It carries per-game telemetry collected from the GRE session buffer.
//
// Partial indicates the event was emitted before the game was confirmed
// complete — either because the GRE buffer reached its flush threshold or
// because the stale-buffer sweep evicted it.  When Partial is true the BFF
// must set partial=true on the corresponding game_plays row.
//
// SchemaVersion identifies the payload shape for the archival Lambda (ADR-046
// A1.4). The daemon sets SchemaVersion = 2 from this release onward. Rows
// without the field are treated as schema_version 0 (no backfill required).
//
// CardPlays, Snapshots, OpponentCards, CounterChanges, and Mulligan are
// omitted from the wire payload when empty/nil (omitempty). They are stored
// in daemon_events.payload (JSONB) for retroactive projection.
//
// WinningTeamID is zero in v0.3.7: GRE messages do not carry a final win
// signal; the BFF projection cross-references the matches table at projection
// time.
//
// Timing note: this event is emitted retrospectively from flushGREBuffer —
// the game has already ended when the event arrives at the BFF. For v0.3.7
// this is acceptable because the BFF projection is async. Real-time
// gre.game_started emission is a v0.3.8 enhancement.
type GamePlayPayload struct {
	MatchID        string               `json:"match_id"`
	GameNumber     int                  `json:"game_number"`
	WinningTeamID  int                  `json:"winning_team_id"`
	TurnCount      int                  `json:"turn_count"`
	DurationSecs   int                  `json:"duration_secs"`
	SchemaVersion  int                  `json:"schema_version"`
	LifeChanges    []LifeChangeEntry    `json:"life_changes"`
	CardPlays      []CardPlayEntry      `json:"card_plays,omitempty"`
	Snapshots      []GameSnapshotEntry  `json:"snapshots,omitempty"`
	OpponentCards  []OpponentCardEntry  `json:"opponent_cards,omitempty"`
	CounterChanges []CounterChangeEntry `json:"counter_changes,omitempty"`
	Mulligan       *MulliganEntry       `json:"mulligan,omitempty"`
	// PlayerOnPlay is true when the local player went first (was on the play)
	// in this game. False means the player was on the draw. Nil means the
	// daemon could not determine the starting player — the GRE buffer lacked
	// the first-turn GameStateMessage (e.g. stale-sweep partial flush).
	PlayerOnPlay *bool `json:"player_on_play,omitempty"`
	Partial      bool  `json:"partial"`
}
