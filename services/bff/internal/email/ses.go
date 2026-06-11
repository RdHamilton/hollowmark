package email

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
)

const (
	// fromAddress is the RFC5321 envelope sender for all transactional email.
	// Technical sender domain is hollowmark.app (ADR-076 §Decision + D13).
	// User-facing copy uses "VaultMTG" per D16 brand-reveal deferral.
	fromAddress = "no-reply@hollowmark.app"

	subjectComplete = "Your VaultMTG account has been deleted"
	subjectFailed   = "Action required: your VaultMTG account deletion could not be completed"

	bodyComplete = `Hi,

Your VaultMTG account deletion request has been processed and your account data has been permanently deleted.

If you have any questions, please contact us at support@vaultmtg.app.

The VaultMTG Team`

	bodyFailed = `Hi,

We were unable to complete the deletion of your VaultMTG account. Our team has been alerted and will follow up.

If you need immediate assistance, please contact us at support@vaultmtg.app.

The VaultMTG Team`
)

// sesv2API is the subset of the SES v2 client used by SESv2Sender.
// It is an interface so tests can substitute a fake.
type sesv2API interface {
	SendEmail(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}

// SESv2Sender implements Sender using AWS SESv2 (ADR-076).
//
// Construct via NewSESv2Sender.  The zero value is not usable.
type SESv2Sender struct {
	client sesv2API
}

// NewSESv2Sender constructs an SESv2Sender from an initialised sesv2 client.
// The caller is responsible for building the client (typically via
// aws-sdk-go-v2/config.LoadDefaultConfig with the EC2 instance role).
func NewSESv2Sender(client *sesv2.Client) *SESv2Sender {
	return &SESv2Sender{client: client}
}

// SendDeletionComplete sends a deletion-completion email.  Returns an error if
// the SES API call fails; the caller (erasure cascade) must treat this as
// non-fatal — the cascade is already complete when this is called.
func (s *SESv2Sender) SendDeletionComplete(ctx context.Context, toEmail string) error {
	return s.send(ctx, toEmail, subjectComplete, bodyComplete)
}

// SendDeletionFailed sends a deletion-failure notification email.  Same
// non-fatal contract as SendDeletionComplete.
func (s *SESv2Sender) SendDeletionFailed(ctx context.Context, toEmail string) error {
	return s.send(ctx, toEmail, subjectFailed, bodyFailed)
}

func (s *SESv2Sender) send(ctx context.Context, toEmail, subject, body string) error {
	_, err := s.client.SendEmail(ctx, &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(fromAddress),
		Destination: &types.Destination{
			ToAddresses: []string{toEmail},
		},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{
					Data:    aws.String(subject),
					Charset: aws.String("UTF-8"),
				},
				Body: &types.Body{
					Text: &types.Content{
						Data:    aws.String(body),
						Charset: aws.String("UTF-8"),
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("email: sesv2 SendEmail to %s: %w", toEmail, err)
	}
	return nil
}
