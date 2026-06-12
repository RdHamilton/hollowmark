// Package email_test contains unit tests for the email.Sender interface,
// its mock implementation, and the SESv2Sender display-string content.
//
// Test coverage:
//   - MockSender records calls correctly on success
//   - MockSender surfaces injected errors on send calls
//   - Sender interface satisfiability by MockSender (compile-time check)
//   - SESv2Sender sends Hollowmark-branded subjects and body copy (#1362)
package email_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/email"
)

// ---------------------------------------------------------------------------
// Compile-time interface check
// ---------------------------------------------------------------------------

// Ensure *MockSender implements Sender at compile time.
var _ email.Sender = (*email.MockSender)(nil)

// ---------------------------------------------------------------------------
// MockSender — success path
// ---------------------------------------------------------------------------

// TestMockSender_SendDeletionComplete_RecordsCall verifies that a successful
// SendDeletionComplete call is recorded on the mock with the correct recipient.
func TestMockSender_SendDeletionComplete_RecordsCall(t *testing.T) {
	m := &email.MockSender{}
	const addr = "user@example.com"

	if err := m.SendDeletionComplete(context.Background(), addr); err != nil {
		t.Fatalf("SendDeletionComplete: unexpected error: %v", err)
	}

	if n := m.DeletionCompleteCallCount(); n != 1 {
		t.Errorf("DeletionCompleteCallCount: got %d, want 1", n)
	}
	if got := m.LastDeletionCompleteAddr(); got != addr {
		t.Errorf("LastDeletionCompleteAddr: got %q, want %q", got, addr)
	}
}

// TestMockSender_SendDeletionFailed_RecordsCall verifies that a successful
// SendDeletionFailed call is recorded on the mock with the correct recipient.
func TestMockSender_SendDeletionFailed_RecordsCall(t *testing.T) {
	m := &email.MockSender{}
	const addr = "user@example.com"

	if err := m.SendDeletionFailed(context.Background(), addr); err != nil {
		t.Fatalf("SendDeletionFailed: unexpected error: %v", err)
	}

	if n := m.DeletionFailedCallCount(); n != 1 {
		t.Errorf("DeletionFailedCallCount: got %d, want 1", n)
	}
	if got := m.LastDeletionFailedAddr(); got != addr {
		t.Errorf("LastDeletionFailedAddr: got %q, want %q", got, addr)
	}
}

// ---------------------------------------------------------------------------
// MockSender — error path
// ---------------------------------------------------------------------------

// TestMockSender_SendDeletionComplete_ReturnsInjectedError verifies that the
// mock surfaces the error set via InjectSendError on SendDeletionComplete.
func TestMockSender_SendDeletionComplete_ReturnsInjectedError(t *testing.T) {
	m := &email.MockSender{}
	want := errors.New("ses: throttled")
	m.InjectSendError(want)

	err := m.SendDeletionComplete(context.Background(), "user@example.com")
	if !errors.Is(err, want) {
		t.Errorf("SendDeletionComplete error: got %v, want %v", err, want)
	}
}

// TestMockSender_SendDeletionFailed_ReturnsInjectedError verifies that the
// mock surfaces the error set via InjectSendError on SendDeletionFailed.
func TestMockSender_SendDeletionFailed_ReturnsInjectedError(t *testing.T) {
	m := &email.MockSender{}
	want := errors.New("ses: 5xx")
	m.InjectSendError(want)

	err := m.SendDeletionFailed(context.Background(), "user@example.com")
	if !errors.Is(err, want) {
		t.Errorf("SendDeletionFailed error: got %v, want %v", err, want)
	}
}

// TestMockSender_ZeroValueIsReady verifies that a zero-value MockSender is
// usable without explicit initialisation — no panics, zero call counts.
func TestMockSender_ZeroValueIsReady(t *testing.T) {
	var m email.MockSender

	if m.DeletionCompleteCallCount() != 0 {
		t.Errorf("zero-value DeletionCompleteCallCount: want 0, got %d", m.DeletionCompleteCallCount())
	}
	if m.DeletionFailedCallCount() != 0 {
		t.Errorf("zero-value DeletionFailedCallCount: want 0, got %d", m.DeletionFailedCallCount())
	}
	// No error injected — calls must succeed.
	if err := m.SendDeletionComplete(context.Background(), "a@b.com"); err != nil {
		t.Errorf("zero-value SendDeletionComplete: unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// SESv2Sender brand-string assertions (#1362)
//
// These tests use email.NewSESv2SenderForTest to inject a fake sesv2 client
// that captures the SendEmailInput.  They assert that the subject and body
// carry "Hollowmark" (not "VaultMTG") after the D16 display-string rename.
// ---------------------------------------------------------------------------

// TestSESv2Sender_DeletionComplete_SubjectIsHollowmark verifies that
// SendDeletionComplete passes a subject line containing "Hollowmark".
func TestSESv2Sender_DeletionComplete_SubjectIsHollowmark(t *testing.T) {
	fake := &email.FakeSESv2Client{}
	s := email.NewSESv2SenderForTest(fake)

	if err := s.SendDeletionComplete(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("SendDeletionComplete: unexpected error: %v", err)
	}

	subject := fake.LastSubject()
	if !strings.Contains(subject, "Hollowmark") {
		t.Errorf("subject %q does not contain %q", subject, "Hollowmark")
	}
	if strings.Contains(subject, "VaultMTG") {
		t.Errorf("subject %q still contains old brand %q", subject, "VaultMTG")
	}
}

// TestSESv2Sender_DeletionComplete_BodyIsHollowmark verifies that
// SendDeletionComplete passes a body containing "Hollowmark".
func TestSESv2Sender_DeletionComplete_BodyIsHollowmark(t *testing.T) {
	fake := &email.FakeSESv2Client{}
	s := email.NewSESv2SenderForTest(fake)

	if err := s.SendDeletionComplete(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("SendDeletionComplete: unexpected error: %v", err)
	}

	body := fake.LastBody()
	if !strings.Contains(body, "Hollowmark") {
		t.Errorf("body %q does not contain %q", body, "Hollowmark")
	}
	if strings.Contains(body, "VaultMTG") {
		t.Errorf("body %q still contains old brand %q", body, "VaultMTG")
	}
}

// TestSESv2Sender_DeletionFailed_SubjectIsHollowmark verifies that
// SendDeletionFailed passes a subject line containing "Hollowmark".
func TestSESv2Sender_DeletionFailed_SubjectIsHollowmark(t *testing.T) {
	fake := &email.FakeSESv2Client{}
	s := email.NewSESv2SenderForTest(fake)

	if err := s.SendDeletionFailed(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("SendDeletionFailed: unexpected error: %v", err)
	}

	subject := fake.LastSubject()
	if !strings.Contains(subject, "Hollowmark") {
		t.Errorf("subject %q does not contain %q", subject, "Hollowmark")
	}
	if strings.Contains(subject, "VaultMTG") {
		t.Errorf("subject %q still contains old brand %q", subject, "VaultMTG")
	}
}

// TestSESv2Sender_DeletionFailed_BodyIsHollowmark verifies that
// SendDeletionFailed passes a body containing "Hollowmark".
func TestSESv2Sender_DeletionFailed_BodyIsHollowmark(t *testing.T) {
	fake := &email.FakeSESv2Client{}
	s := email.NewSESv2SenderForTest(fake)

	if err := s.SendDeletionFailed(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("SendDeletionFailed: unexpected error: %v", err)
	}

	body := fake.LastBody()
	if !strings.Contains(body, "Hollowmark") {
		t.Errorf("body %q does not contain %q", body, "Hollowmark")
	}
	if strings.Contains(body, "VaultMTG") {
		t.Errorf("body %q still contains old brand %q", body, "VaultMTG")
	}
}

// TestSESv2Sender_FromAddressUnchanged verifies that the sending address
// (no-reply@hollowmark.app) is NOT altered by the display-string rename —
// only brand copy changes, never the delivery domain (I-57).
func TestSESv2Sender_FromAddressUnchanged(t *testing.T) {
	fake := &email.FakeSESv2Client{}
	s := email.NewSESv2SenderForTest(fake)

	if err := s.SendDeletionComplete(context.Background(), "user@example.com"); err != nil {
		t.Fatalf("SendDeletionComplete: unexpected error: %v", err)
	}

	const wantFrom = "no-reply@hollowmark.app"
	if got := fake.LastFrom(); got != wantFrom {
		t.Errorf("from address: got %q, want %q", got, wantFrom)
	}
}
