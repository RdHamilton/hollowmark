package erasure_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/erasure"
)

// TestPostHogHTTPClient_DeletePerson_204IsSuccess verifies the happy path:
// a 204 No Content response is treated as success.
func TestPostHogHTTPClient_DeletePerson_204IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE method, got %s", r.Method)
		}
		if r.Header.Get("Authorization") == "" {
			t.Error("expected Authorization header to be set")
		}
		// Verify delete_events=true query param is set.
		if r.URL.Query().Get("delete_events") != "true" {
			t.Errorf("expected delete_events=true, got %q", r.URL.Query().Get("delete_events"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := erasure.NewPostHogHTTPClient("test-personal-key", "12345", srv.URL)
	if err := client.DeletePerson(context.Background(), "abc123hash"); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
}

// TestPostHogHTTPClient_DeletePerson_404IsIdempotent verifies that a 404
// response is treated as success (person already deleted — idempotency).
func TestPostHogHTTPClient_DeletePerson_404IsIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := erasure.NewPostHogHTTPClient("key", "proj", srv.URL)
	if err := client.DeletePerson(context.Background(), "gone-hash"); err != nil {
		t.Errorf("404 should be idempotent success, got: %v", err)
	}
}

// TestPostHogHTTPClient_DeletePerson_5xxReturnsError verifies that server errors
// are propagated as errors to the caller.
func TestPostHogHTTPClient_DeletePerson_5xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := erasure.NewPostHogHTTPClient("key", "proj", srv.URL)
	if err := client.DeletePerson(context.Background(), "hash"); err == nil {
		t.Error("expected error on 5xx, got nil")
	}
}
