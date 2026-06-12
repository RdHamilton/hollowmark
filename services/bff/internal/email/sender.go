// Package email provides the transactional-email seam for the BFF service
// (ADR-076).
//
// All outbound transactional email goes through the Sender interface.
// Production uses SESv2Sender (wrapping aws-sdk-go-v2/service/sesv2).
// Tests use MockSender.
//
// The From address is always no-reply@hollowmark.app (technical sender domain,
// ADR-076 §Decision).  User-facing display copy uses "Hollowmark" (#1362).
package email

import "context"

// Sender is the transactional-email seam.  Any struct that can send the two
// account-deletion lifecycle emails satisfies this interface.
//
// Both methods must be safe to call from a background goroutine (the erasure
// cascade dispatches one).  Both methods must treat a nil receiver as a no-op
// (callers may pass a nil Sender when the SES client is not configured).
type Sender interface {
	// SendDeletionComplete sends a confirmation email to toEmail notifying the
	// user that their Hollowmark account deletion has completed.
	SendDeletionComplete(ctx context.Context, toEmail string) error

	// SendDeletionFailed sends a notification email to toEmail informing the
	// user that their Hollowmark account deletion could not be completed and they
	// should contact support.
	SendDeletionFailed(ctx context.Context, toEmail string) error
}
