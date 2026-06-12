package email

import (
	"context"
	"sync"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"
)

// FakeSESv2Client is a test double for the sesv2API interface.
// It captures the most-recent SendEmail call so tests can assert
// on the subject, body, and from address without hitting SES.
//
// The zero value is ready to use. All methods are safe for concurrent use.
type FakeSESv2Client struct {
	mu       sync.Mutex
	lastFrom string
	lastSubj string
	lastBody string
}

// SendEmail records the from address, subject, and plain-text body from
// params and returns nil (no error). Only the Simple/Text path is captured
// because that is what SESv2Sender uses.
func (f *FakeSESv2Client) SendEmail(
	_ context.Context,
	params *sesv2.SendEmailInput,
	_ ...func(*sesv2.Options),
) (*sesv2.SendEmailOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if params.FromEmailAddress != nil {
		f.lastFrom = *params.FromEmailAddress
	}
	if params.Content != nil && params.Content.Simple != nil {
		if params.Content.Simple.Subject != nil && params.Content.Simple.Subject.Data != nil {
			f.lastSubj = *params.Content.Simple.Subject.Data
		}
		if params.Content.Simple.Body != nil &&
			params.Content.Simple.Body.Text != nil &&
			params.Content.Simple.Body.Text.Data != nil {
			f.lastBody = *params.Content.Simple.Body.Text.Data
		}
	}
	return &sesv2.SendEmailOutput{}, nil
}

// LastFrom returns the from address of the most-recent SendEmail call.
func (f *FakeSESv2Client) LastFrom() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastFrom
}

// LastSubject returns the subject of the most-recent SendEmail call.
func (f *FakeSESv2Client) LastSubject() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSubj
}

// LastBody returns the plain-text body of the most-recent SendEmail call.
func (f *FakeSESv2Client) LastBody() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastBody
}

// NewSESv2SenderForTest constructs an SESv2Sender backed by the given
// sesv2API implementation.  Use with FakeSESv2Client in unit tests to
// assert on the email content without making real SES calls.
func NewSESv2SenderForTest(client sesv2API) *SESv2Sender {
	return &SESv2Sender{client: client}
}
