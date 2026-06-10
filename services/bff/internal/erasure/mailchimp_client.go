package erasure

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // Mailchimp subscriber hash is MD5 by Mailchimp API spec — not a security choice.
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// MailchimpErasureClient implements MailchimpPermanentDeleter using the
// Mailchimp Marketing API v3.
//
// The delete-permanent action (POST /3.0/lists/{id}/members/{hash}/actions/delete-permanent)
// is a GDPR-compliant erasure that:
//  1. Removes the contact record entirely (not just unsubscribe).
//  2. Adds a suppression hash so the address cannot be re-added.
//
// This is distinct from PUT /3.0/lists/{id}/members/{hash} with status=unsubscribed,
// which retains the contact record — using Unsubscribe for Art.17 compliance is
// incorrect (Q2 ruling in the approved plan).
type MailchimpErasureClient struct {
	apiKey     string
	listID     string
	datacenter string
	httpClient *http.Client
}

// NewMailchimpErasureClient constructs a client from an API key
// (format: <key>-<datacenter>, e.g. "abc123-us1") and listID.
func NewMailchimpErasureClient(apiKey, listID string) (*MailchimpErasureClient, error) {
	parts := strings.SplitN(apiKey, "-", 2)
	if len(parts) != 2 || parts[1] == "" {
		return nil, fmt.Errorf("mailchimp erasure: invalid API key format (expected <key>-<datacenter>)")
	}
	return &MailchimpErasureClient{
		apiKey:     apiKey,
		listID:     listID,
		datacenter: parts[1],
		httpClient: &http.Client{},
	}, nil
}

// mailchimpErasureBody is the (empty) request body for the delete-permanent action.
// Mailchimp's delete-permanent action requires an empty JSON object body.
type mailchimpErasureBody struct{}

// DeletePermanent implements MailchimpPermanentDeleter.
//
// Calls POST /3.0/lists/{list_id}/members/{subscriber_hash}/actions/delete-permanent
// where subscriber_hash = MD5(lower(email)) per Mailchimp API spec.
//
// This is an irreversible operation — the contact is removed from Mailchimp
// and a suppression hash is added.  The caller (erasure Step 6) must only
// invoke this after all other steps succeed.
func (c *MailchimpErasureClient) DeletePermanent(ctx context.Context, email string) error {
	hash := mailchimpSubscriberHash(email)
	url := fmt.Sprintf("https://%s.api.mailchimp.com/3.0/lists/%s/members/%s/actions/delete-permanent",
		c.datacenter, c.listID, hash)

	body, err := json.Marshal(mailchimpErasureBody{})
	if err != nil {
		return fmt.Errorf("mailchimp delete-permanent: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mailchimp delete-permanent: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth("anystring", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mailchimp delete-permanent: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// 204 No Content is the success response for delete-permanent.
	// 404 means the member was not found — treat as idempotent success (already deleted).
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("mailchimp delete-permanent: unexpected status %d for email hash %s", resp.StatusCode, hash)
	}
	return nil
}

// mailchimpSubscriberHash returns the MD5 hash of the lowercased email address,
// which is the subscriber hash Mailchimp requires for all member API calls.
//
// MD5 is required here by the Mailchimp API specification — it is not a security
// primitive in this context.
func mailchimpSubscriberHash(email string) string {
	h := md5.Sum([]byte(strings.ToLower(email))) //nolint:gosec
	return fmt.Sprintf("%x", h)
}
