package erasure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// PostHogHTTPClient implements PostHogDeleter using the PostHog bulk-delete API.
//
// PostHog's bulk delete removes all events for a given distinct_id from the
// PostHog person database.  The distinct_id is the identityhash of the
// account_id string (SHA-256 hex[:16]).
//
// Endpoint: POST https://app.posthog.com/api/projects/{pk}/persons/bulk_delete/
// Authentication: Personal API key (Bearer token) with person:write scope.
// SSM path: /vaultmtg/{env}/posthog-personal-api-key (Q3 ruling).
type PostHogHTTPClient struct {
	personalAPIKey string
	projectID      string
	host           string
	httpClient     *http.Client
}

// NewPostHogHTTPClient constructs a PostHog erasure client.
//
// personalAPIKey: PostHog personal API key (from SSM /vaultmtg/{env}/posthog-personal-api-key).
// projectID: PostHog project ID (e.g. "12345") — available from the PostHog UI.
// host: PostHog API host (e.g. "https://app.posthog.com" for US cloud).
func NewPostHogHTTPClient(personalAPIKey, projectID, host string) *PostHogHTTPClient {
	return &PostHogHTTPClient{
		personalAPIKey: personalAPIKey,
		projectID:      projectID,
		host:           host,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
	}
}

// bulkDeleteRequest is the request body for POST /persons/bulk_delete/.
type bulkDeleteRequest struct {
	DistinctIDs []string `json:"distinct_ids"`
}

// bulkDeleteResponse is the response body from POST /persons/bulk_delete/.
type bulkDeleteResponse struct {
	PersonsFound                int      `json:"persons_found"`
	PersonsDeleted              int      `json:"persons_deleted"`
	EventsQueuedForDeletion     bool     `json:"events_queued_for_deletion"`
	RecordingsQueuedForDeletion bool     `json:"recordings_queued_for_deletion"`
	DeletionErrors              []string `json:"deletion_errors"`
}

// DeletePerson implements PostHogDeleter.
//
// Calls POST /api/projects/{pk}/persons/bulk_delete/ with body
// {"distinct_ids":["<distinctID>"]} to permanently remove all PostHog events
// for the given distinct_id.
//
// A 202 Accepted response with an empty deletion_errors slice is success.
// persons_found:0 is treated as idempotent success — the person was never
// tracked in PostHog or was already deleted on a prior retry.
// Any non-202 status or a non-empty deletion_errors slice returns an error so
// the erasure cascade halts fail-closed before Clerk/Mailchimp.
func (c *PostHogHTTPClient) DeletePerson(ctx context.Context, distinctID string) error {
	url := fmt.Sprintf("%s/api/projects/%s/persons/bulk_delete/", c.host, c.projectID)

	body, err := json.Marshal(bulkDeleteRequest{DistinctIDs: []string{distinctID}})
	if err != nil {
		return fmt.Errorf("posthog delete person: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("posthog delete person: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.personalAPIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("posthog delete person: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Only 202 Accepted is a valid success response from bulk_delete/.
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("posthog delete person: unexpected status %d for distinct_id %s", resp.StatusCode, distinctID)
	}

	var result bulkDeleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("posthog delete person: decode response: %w", err)
	}

	// persons_found:0 means the distinct_id was never in PostHog (or was already
	// deleted on a prior retry).  This is idempotent success — mirror the old
	// 404 handling.
	if result.PersonsFound == 0 {
		return nil
	}

	// A non-empty deletion_errors slice indicates PostHog encountered an internal
	// failure deleting the person.  Return an error so the cascade halts
	// fail-closed before the irreversible Clerk and Mailchimp steps.
	if len(result.DeletionErrors) > 0 {
		return fmt.Errorf("posthog delete person: deletion errors for distinct_id %s: %v", distinctID, result.DeletionErrors)
	}

	return nil
}
