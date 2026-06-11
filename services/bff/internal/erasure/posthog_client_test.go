package erasure_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/erasure"
)

// TestPostHogHTTPClient_DeletePerson_usesBulkDeleteEndpoint verifies that
// DeletePerson issues POST .../persons/bulk_delete/ with the correct
// JSON body and Authorization header (AC1 + AC4).
func TestPostHogHTTPClient_DeletePerson_usesBulkDeleteEndpoint(t *testing.T) {
	const distinctID = "abc123hash"
	const apiKey = "phx_test-personal-key"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Must be POST.
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		// Must target the bulk_delete path.
		const wantPath = "/api/projects/12345/persons/bulk_delete/"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		// Must carry the personal API key as a Bearer token.
		if got := r.Header.Get("Authorization"); got != "Bearer "+apiKey {
			t.Errorf("expected Authorization %q, got %q", "Bearer "+apiKey, got)
		}
		// Body must be {"distinct_ids":["abc123hash"]}.
		body, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("could not decode request body: %v", err)
		}
		ids, ok := payload["distinct_ids"].([]interface{})
		if !ok || len(ids) != 1 || ids[0] != distinctID {
			t.Errorf("expected distinct_ids:[%q], got body: %s", distinctID, body)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"persons_found":1,"persons_deleted":1,"events_queued_for_deletion":true,"recordings_queued_for_deletion":false,"deletion_errors":[]}`))
	}))
	defer srv.Close()

	client := erasure.NewPostHogHTTPClient(apiKey, "12345", srv.URL)
	if err := client.DeletePerson(context.Background(), distinctID); err != nil {
		t.Fatalf("DeletePerson: %v", err)
	}
}

// TestPostHogHTTPClient_DeletePerson_202IsSuccess verifies that a 202 Accepted
// response is treated as success (AC1).
func TestPostHogHTTPClient_DeletePerson_202IsSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"persons_found":1,"persons_deleted":1,"events_queued_for_deletion":true,"recordings_queued_for_deletion":false,"deletion_errors":[]}`))
	}))
	defer srv.Close()

	client := erasure.NewPostHogHTTPClient("key", "proj", srv.URL)
	if err := client.DeletePerson(context.Background(), "hash"); err != nil {
		t.Fatalf("202 should be success, got: %v", err)
	}
}

// TestPostHogHTTPClient_DeletePerson_personsFoundZeroIsIdempotent verifies that
// persons_found:0 (person not in PostHog) is treated as idempotent success
// rather than an error, mirroring the existing 404 handling (AC2).
func TestPostHogHTTPClient_DeletePerson_personsFoundZeroIsIdempotent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"persons_found":0,"persons_deleted":0,"events_queued_for_deletion":false,"recordings_queued_for_deletion":false,"deletion_errors":[]}`))
	}))
	defer srv.Close()

	client := erasure.NewPostHogHTTPClient("key", "proj", srv.URL)
	if err := client.DeletePerson(context.Background(), "not-in-posthog"); err != nil {
		t.Errorf("persons_found:0 should be idempotent success, got: %v", err)
	}
}

// TestPostHogHTTPClient_DeletePerson_deletionErrorsReturnError verifies that a
// 202 with a non-empty deletion_errors slice is treated as a failure so the
// cascade halts fail-closed (AC3).
func TestPostHogHTTPClient_DeletePerson_deletionErrorsReturnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"persons_found":1,"persons_deleted":0,"events_queued_for_deletion":false,"recordings_queued_for_deletion":false,"deletion_errors":["internal deletion failure"]}`))
	}))
	defer srv.Close()

	client := erasure.NewPostHogHTTPClient("key", "proj", srv.URL)
	if err := client.DeletePerson(context.Background(), "hash"); err == nil {
		t.Error("expected error when deletion_errors is non-empty, got nil (fail-closed regression)")
	}
}

// TestPostHogHTTPClient_DeletePerson_non202ReturnsError verifies that a
// non-202 response (e.g. 403, 500) returns an error so the cascade halts
// fail-closed (AC3).
func TestPostHogHTTPClient_DeletePerson_non202ReturnsError(t *testing.T) {
	cases := []struct {
		name   string
		status int
	}{
		{"403 Forbidden", http.StatusForbidden},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			client := erasure.NewPostHogHTTPClient("key", "proj", srv.URL)
			if err := client.DeletePerson(context.Background(), "hash"); err == nil {
				t.Errorf("expected error on status %d, got nil (fail-closed regression)", tc.status)
			}
		})
	}
}
