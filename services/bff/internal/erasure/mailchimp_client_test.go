package erasure_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/erasure"
)

func TestMailchimpErasureClient_DeletePermanent_UsesCorrectPath(t *testing.T) {
	var capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := &erasure.MailchimpErasureClientForTest{
		APIURL:     srv.URL,
		ListID:     "list123",
		HTTPClient: srv.Client(),
	}

	err := client.DeletePermanent(context.Background(), "User@Example.COM")
	if err != nil {
		t.Fatalf("DeletePermanent: %v", err)
	}

	if !strings.Contains(capturedPath, "actions/delete-permanent") {
		t.Errorf("path %q does not contain 'actions/delete-permanent' — Q2 ruling: must use delete-permanent action", capturedPath)
	}

	// MD5(lower("User@Example.COM")) = MD5("user@example.com") = b58996c504c5638798eb6b511e6f49af
	if !strings.Contains(capturedPath, "b58996c504c5638798eb6b511e6f49af") {
		t.Errorf("path %q does not contain expected subscriber hash for 'user@example.com'", capturedPath)
	}
}

func TestMailchimpErasureClient_DeletePermanent_404IsIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := &erasure.MailchimpErasureClientForTest{
		APIURL:     srv.URL,
		ListID:     "list123",
		HTTPClient: srv.Client(),
	}

	if err := client.DeletePermanent(context.Background(), "gone@example.com"); err != nil {
		t.Errorf("404 should be treated as idempotent success, got error: %v", err)
	}
}

func TestMailchimpErasureClient_DeletePermanent_5xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := &erasure.MailchimpErasureClientForTest{
		APIURL:     srv.URL,
		ListID:     "list123",
		HTTPClient: srv.Client(),
	}

	if err := client.DeletePermanent(context.Background(), "error@example.com"); err == nil {
		t.Error("expected error on 5xx response, got nil")
	}
}

// TestMailchimpErasureClient_DeletePermanent_ErrorDoesNotLeakSubscriberHash
// verifies that the error returned on a non-success HTTP response does NOT
// contain the MD5 subscriber hash of the email address.
//
// MD5(email) is PII-linkable (GDPR Recital 26: reversible via rainbow tables /
// known-email enumeration) and must not appear in server error logs.
// Uses NewMailchimpErasureClientAtURL (export_test.go) to exercise the REAL
// MailchimpErasureClient code path, not the MailchimpErasureClientForTest double.
func TestMailchimpErasureClient_DeletePermanent_ErrorDoesNotLeakSubscriberHash(t *testing.T) {
	const email = "user@example.com"
	// MD5("user@example.com") is the Mailchimp subscriber hash.
	// Its presence in an error string is a GDPR Recital 26 PII leak.
	const subscriberHash = "b58996c504c5638798eb6b511e6f49af"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := erasure.NewMailchimpErasureClientAtURL(srv.URL, "list123", srv.Client())

	err := client.DeletePermanent(context.Background(), email)
	if err == nil {
		t.Fatal("expected error on 500 response, got nil")
	}

	if strings.Contains(err.Error(), subscriberHash) {
		t.Errorf("error must not contain MD5 subscriber hash (GDPR Recital 26 PII leak): %q", err.Error())
	}
	if strings.Contains(err.Error(), email) {
		t.Errorf("error must not contain raw email address (PII leak): %q", err.Error())
	}
}
