package erasure

import (
	"context"
	"fmt"
	"net/http"
)

// ClerkAdminClient calls the Clerk Admin REST API to delete a user identity.
//
// This client uses a plain *http.Client (not the clerk-sdk-go SDK) because
// the Clerk user-deletion endpoint is a simple HTTP DELETE — the SDK does not
// expose a first-class "delete user" wrapper that accepts an injected client.
//
// Timeout: the caller MUST supply a *http.Client with a timeout (e.g. 30s) so
// the cascade goroutine does not hang indefinitely on network failures.
type ClerkAdminClient struct {
	secretKey  string
	httpClient *http.Client
}

// NewClerkAdminClient constructs a ClerkAdminClient.
//
// secretKey is the Clerk secret key (sk_live_* in production, sk_test_* in
// staging).  httpClient MUST have a non-zero Timeout set by the caller.
func NewClerkAdminClient(secretKey string, httpClient *http.Client) *ClerkAdminClient {
	return &ClerkAdminClient{
		secretKey:  secretKey,
		httpClient: httpClient,
	}
}

// DeleteUser calls DELETE https://api.clerk.com/v1/users/{clerkUserID}.
//
// A 200 response is treated as success.  A 404 is treated as success
// (idempotent — user may have already been deleted).  Any other status code
// is an error.
//
// Implements ClerkDeleter.
func (c *ClerkAdminClient) DeleteUser(ctx context.Context, clerkUserID string) error {
	url := fmt.Sprintf("https://api.clerk.com/v1/users/%s", clerkUserID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("clerk admin delete user: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.secretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("clerk admin delete user: do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNotFound:
		// 200 = deleted; 404 = already gone — both are success for a GDPR cascade.
		return nil
	default:
		return fmt.Errorf("clerk admin delete user: unexpected status %d for user %s", resp.StatusCode, clerkUserID)
	}
}

// ── Noop types ────────────────────────────────────────────────────────────────
//
// Noop types are intentionally value types (not pointers) so that the
// mount-gate type assertions in buildAccountDeletionHandler match the concrete
// types returned by buildClerkAdminClient, buildPostHogDeleter, and
// buildMailchimpErasureClient when the corresponding SSM parameters are absent.
//
// The mount-gate does:
//   _, isNoop := clerkDeleter.(erasure.NoopClerkDeleter)
//
// This assertion is TRUE when clerkDeleter holds a value of type
// NoopClerkDeleter{}, and FALSE when it holds a *ClerkAdminClient or any other
// real implementation.  Using pointer types here would break the gate.

// NoopClerkDeleter is returned by buildClerkAdminClient when CLERK_SECRET_KEY
// is absent.  In production/staging the mount-gate fires when this type is
// detected.  In development the erasure route is still mounted with this noop.
type NoopClerkDeleter struct{}

// DeleteUser is a no-op.  Implements ClerkDeleter.
func (NoopClerkDeleter) DeleteUser(_ context.Context, _ string) error { return nil }

// Compile-time assertion: NoopClerkDeleter satisfies ClerkDeleter.
var _ ClerkDeleter = NoopClerkDeleter{}

// NoopPostHogDeleter is returned by buildPostHogDeleter when
// POSTHOG_PERSONAL_API_KEY or POSTHOG_PROJECT_ID are absent.
type NoopPostHogDeleter struct{}

// DeletePerson is a no-op.  Implements PostHogDeleter.
func (NoopPostHogDeleter) DeletePerson(_ context.Context, _ string) error { return nil }

// Compile-time assertion: NoopPostHogDeleter satisfies PostHogDeleter.
var _ PostHogDeleter = NoopPostHogDeleter{}

// NoopMailchimpDeleter is returned by buildMailchimpErasureClient when
// MAILCHIMP_API_KEY or MAILCHIMP_LIST_ID are absent.
type NoopMailchimpDeleter struct{}

// DeletePermanent is a no-op.  Implements MailchimpPermanentDeleter.
func (NoopMailchimpDeleter) DeletePermanent(_ context.Context, _ string) error { return nil }

// Compile-time assertion: NoopMailchimpDeleter satisfies MailchimpPermanentDeleter.
var _ MailchimpPermanentDeleter = NoopMailchimpDeleter{}
