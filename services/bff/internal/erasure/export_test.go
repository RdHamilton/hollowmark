// Package erasure — test exports.
//
// This file exposes test-only helpers so the external test package (erasure_test)
// can inject test servers into the HTTP clients without making those hooks part
// of the production surface.
package erasure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ResetClerkUserIDFromContextFn clears the package-level context-key extractor
// so tests can exercise the "fn not configured" error path in StartErasureJob.
// Only for use in tests — not part of the production surface.
func ResetClerkUserIDFromContextFn() {
	clerkUserIDFromContextFn.Store(nil)
}

// NewMailchimpErasureClientAtURL constructs a real MailchimpErasureClient that
// targets the given base URL instead of the live Mailchimp API. Used by tests
// to route the production DeletePermanent code path through an httptest.Server.
func NewMailchimpErasureClientAtURL(baseURL, listID string, httpClient *http.Client) *MailchimpErasureClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &MailchimpErasureClient{
		apiKey:     "testkey-us1",
		listID:     listID,
		datacenter: "us1",
		httpClient: httpClient,
		baseURL:    baseURL,
	}
}

// MailchimpErasureClientForTest is a test-double version of MailchimpErasureClient
// that accepts an arbitrary base URL instead of constructing one from an API key.
// Used by mailchimp_client_test.go to point the client at an httptest.Server.
type MailchimpErasureClientForTest struct {
	APIURL     string // Base URL of the test server (e.g. http://127.0.0.1:PORT)
	ListID     string
	HTTPClient *http.Client
}

// DeletePermanent implements MailchimpPermanentDeleter using the injected test URL.
func (c *MailchimpErasureClientForTest) DeletePermanent(ctx context.Context, email string) error {
	hash := mailchimpSubscriberHash(email)
	url := fmt.Sprintf("%s/3.0/lists/%s/members/%s/actions/delete-permanent",
		c.APIURL, c.ListID, hash)

	body, err := json.Marshal(mailchimpErasureBody{})
	if err != nil {
		return fmt.Errorf("test mailchimp delete-permanent: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("test mailchimp delete-permanent: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("test mailchimp delete-permanent: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("test mailchimp delete-permanent: unexpected status %d", resp.StatusCode)
	}
	return nil
}
