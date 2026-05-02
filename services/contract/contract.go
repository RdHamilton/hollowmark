package contract

import (
	"encoding/json"
	"time"
)

// DaemonEvent is the wire type the daemon sends to the BFF /v1/ingest/events endpoint.
type DaemonEvent struct {
	Type       string          `json:"type"`
	AccountID  string          `json:"account_id"`
	SessionID  string          `json:"session_id"`
	OccurredAt time.Time       `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
}
