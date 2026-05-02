// Package dispatch handles encoding and posting contract.DaemonEvent payloads to the BFF.
package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/ramonehamilton/mtga-contract"
)

// Dispatcher POSTs DaemonEvents to the BFF ingest endpoint.
type Dispatcher struct {
	bffURL     string
	ingestPath string
	jwt        string
	client     *http.Client
}

// New creates a Dispatcher.
//
// bffURL: base URL of the BFF, e.g. "https://api.example.com"
// ingestPath: path of the ingest endpoint, e.g. "/v1/ingest/events"
// jwt: bearer token for Authorization header
func New(bffURL, ingestPath, jwt string) *Dispatcher {
	return &Dispatcher{
		bffURL:     bffURL,
		ingestPath: ingestPath,
		jwt:        jwt,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Send encodes event as JSON and POSTs it to the BFF.
func (d *Dispatcher) Send(ctx context.Context, event contract.DaemonEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	url := d.bffURL + d.ingestPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if d.jwt != "" {
		req.Header.Set("Authorization", "Bearer "+d.jwt)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("post event: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("BFF returned %d", resp.StatusCode)
	}

	log.Printf("[dispatch] event %q sent (session=%s)", event.Type, event.SessionID)
	return nil
}

// BuildEvent constructs a contract.DaemonEvent from raw log entry data.
//
// eventType: semantic event type, e.g. "draft.pick"
// accountID: MTGA account ID
// sessionID: current monitoring session ID
// payload: any JSON-serialisable value
func BuildEvent(eventType, accountID, sessionID string, payload interface{}) (contract.DaemonEvent, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return contract.DaemonEvent{}, fmt.Errorf("marshal payload: %w", err)
	}
	return contract.DaemonEvent{
		Type:       eventType,
		AccountID:  accountID,
		SessionID:  sessionID,
		OccurredAt: time.Now().UTC(),
		Payload:    json.RawMessage(raw),
	}, nil
}
