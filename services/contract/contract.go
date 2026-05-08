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
