package erasure_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/erasure"
)

// TestMailchimpErasureClient_DeletePermanent_UsesCorrectPath verifies the
// HTTP call hits the delete-permanent action path (not the unsubscribe path).
// Ray's implementation note: the spy must assert the action path, not just
// that a call was made.
func TestMailchimpErasureClient_DeletePermanent_UsesCorrectPath(t *testing.T) {
	var capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	// Build a client that targets the test server.
	// We override the HTTP client via a transport that rewrites the host.
	client := &erasure.MailchimpErasureClientForTest{
		APIURL:     srv.URL,
		ListID:     "list123",
		HTTPClient: srv.Client(),
	}

	err := client.DeletePermanent(context.Background(), "User@Example.COM")
	if err != nil {
		t.Fatalf("DeletePermanent: %v", err)
	}

	// The path must include "actions/delete-permanent" — not "unsubscribe" or PUT.
	if !strings.Contains(capturedPath, "actions/delete-permanent") {
		t.Errorf("path %q does not contain 'actions/delete-permanent' — Q2 ruling: must use delete-permanent action", capturedPath)
	}

	// Verify the subscriber hash is present in the path.
	// MD5(lower("User@Example.COM")) = MD5("user@example.com") = b58996c504c5638798eb6b511e6f49af
	if !strings.Contains(capturedPath, "b58996c504c5638798eb6b511e6f49af") {
		t.Errorf("path %q does not contain expected subscriber hash for 'user@example.com'", capturedPath)
	}
}

// TestMailchimpErasureClient_DeletePermanent_404IsIdempotent verifies that a
// 404 from Mailchimp (member not found) is treated as a success — the contact
// may have already been deleted.
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

// TestMailchimpErasureClient_DeletePermanent_5xxReturnsError verifies that a
// 5xx response from Mailchimp is returned as an error to the caller.
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
