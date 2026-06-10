package erasure

import (
	"context"
	"fmt"
	"net/http"
)

// PostHogHTTPClient implements PostHogDeleter using the PostHog bulk-delete API.
//
// PostHog's bulk delete removes all events for a given distinct_id from the
// PostHog person database.  The distinct_id is the identityhash of the
// account_id string (SHA-256 hex[:16]).
//
// Endpoint: DELETE https://app.posthog.com/api/projects/{pk}/persons/?distinct_id={id}
// Authentication: Personal API key (Bearer token, NOT the project API key).
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
		httpClient:     &http.Client{},
	}
}

// DeletePerson implements PostHogDeleter.
//
// Calls DELETE /api/projects/{pk}/persons/?distinct_id={id}&delete_events=true
// to permanently remove all PostHog events for the given distinct_id.
//
// 404 is treated as idempotent success — the person may have already been
// deleted on a previous retry.
func (c *PostHogHTTPClient) DeletePerson(ctx context.Context, distinctID string) error {
	url := fmt.Sprintf("%s/api/projects/%s/persons/?distinct_id=%s&delete_events=true",
		c.host, c.projectID, distinctID)

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("posthog delete person: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.personalAPIKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("posthog delete person: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 204 No Content = success.
	// 404 = person not found — treat as idempotent.
	// 200 = some PostHog versions return 200 with the deleted person object.
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK, http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("posthog delete person: unexpected status %d for distinct_id %s", resp.StatusCode, distinctID)
	}
}
