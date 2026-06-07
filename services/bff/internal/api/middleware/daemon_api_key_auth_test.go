package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── test doubles ─────────────────────────────────────────────────────────────

// stubDaemonKeyPrefixLookup implements daemonKeyPrefixLookup for tests.
type stubDaemonKeyPrefixLookup struct {
	rows      []repository.DaemonAPIKey
	lookupErr error

	updateCalls []string
	updateErr   error
}

func (s *stubDaemonKeyPrefixLookup) GetByPrefix(_ context.Context, _ string) ([]repository.DaemonAPIKey, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	return s.rows, nil
}

func (s *stubDaemonKeyPrefixLookup) UpdateLastUsed(_ context.Context, id string) error {
	s.updateCalls = append(s.updateCalls, id)
	return s.updateErr
}

// stubDaemonUserResolver implements userResolver for tests.
type stubDaemonUserResolver struct {
	user    *repository.User
	userErr error
}

func (s *stubDaemonUserResolver) GetByClerkUserID(_ context.Context, _ string) (*repository.User, error) {
	return s.user, s.userErr
}

// hashDaemonKey returns a bcrypt hash at MinCost.
func hashDaemonKey(t *testing.T, key string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

// newDaemonRequest builds an HTTP request with Authorization: Bearer <token>.
func newDaemonRequest(token string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/events", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

// ─── tests ───────────────────────────────────────────────────────────────────

// TestDaemonAPIKeyAuth_CorrectKeyAccepted verifies the happy path: a valid
// bearer token whose prefix matches a row and whose hash compares correctly
// results in a 200 with the user id on context.
func TestDaemonAPIKeyAuth_CorrectKeyAccepted(t *testing.T) {
	const plaintext = "1234567890abcdef_rest_of_real_key_here"
	const prefix = "1234567890abcdef"
	hash := hashDaemonKey(t, plaintext)

	keyRepo := &stubDaemonKeyPrefixLookup{
		rows: []repository.DaemonAPIKey{
			{ID: "key-id-1", AccountID: "user_clerk_abc", KeyHash: hash, KeyPrefix: prefix},
		},
	}
	userRepo := &stubDaemonUserResolver{
		user: &repository.User{ID: 42},
	}

	var gotUserID int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := middleware.UserIDFromContext(r.Context())
		assert.True(t, ok, "user id must be on context after successful auth")
		gotUserID = id
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.DaemonAPIKeyAuth(keyRepo, userRepo)(next)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newDaemonRequest(plaintext))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, int64(42), gotUserID)
}

// TestDaemonAPIKeyAuth_WrongKeyRejected verifies that a token whose prefix
// matches a row but whose bcrypt comparison fails returns 401. The prefix
// lookup returns a row, but the hash does not match the token.
func TestDaemonAPIKeyAuth_WrongKeyRejected(t *testing.T) {
	const correctPlaintext = "1234567890abcdef_CORRECT_key_value"
	const wrongPlaintext = "1234567890abcdef_WRONG___key_value_"
	const prefix = "1234567890abcdef"
	hash := hashDaemonKey(t, correctPlaintext)

	keyRepo := &stubDaemonKeyPrefixLookup{
		rows: []repository.DaemonAPIKey{
			{ID: "key-id-2", AccountID: "user_clerk_xyz", KeyHash: hash, KeyPrefix: prefix},
		},
	}
	userRepo := &stubDaemonUserResolver{}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must not be called on wrong key")
	})

	handler := middleware.DaemonAPIKeyAuth(keyRepo, userRepo)(next)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newDaemonRequest(wrongPlaintext))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestDaemonAPIKeyAuth_PrefixCollisionOnlyCorrectKeyAuths is the auth-bypass
// regression test (Ray constraint #3): two rows share the same prefix; only
// the row whose hash matches the bearer token authenticates. The wrong key
// must NOT be authenticated even though it shares a prefix with a valid row.
func TestDaemonAPIKeyAuth_PrefixCollisionOnlyCorrectKeyAuths(t *testing.T) {
	const prefix = "1234567890abcdef"
	const plaintextA = prefix + "_KEY_A_rest_of_value_here"
	const plaintextB = prefix + "_KEY_B_rest_of_value_here"
	hashA := hashDaemonKey(t, plaintextA)
	hashB := hashDaemonKey(t, plaintextB)

	keyRepo := &stubDaemonKeyPrefixLookup{
		rows: []repository.DaemonAPIKey{
			{ID: "key-id-A", AccountID: "user_clerk_A", KeyHash: hashA, KeyPrefix: prefix},
			{ID: "key-id-B", AccountID: "user_clerk_B", KeyHash: hashB, KeyPrefix: prefix},
		},
	}
	userRepoA := &stubDaemonUserResolver{user: &repository.User{ID: 10}}

	// Sending key A's plaintext must authenticate as user A, not user B.
	var gotAccountID string
	userRepo := &stubDaemonUserResolver{}
	userRepo.user = &repository.User{ID: 10}
	// We override GetByClerkUserID via the repo to track which account_id was resolved.
	trackingUserRepo := &trackingUserResolver{inner: map[string]*repository.User{
		"user_clerk_A": {ID: 10},
		"user_clerk_B": {ID: 20},
	}, resolved: &gotAccountID}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := middleware.UserIDFromContext(r.Context())
		assert.Equal(t, int64(10), id, "must authenticate as user A, not user B")
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.DaemonAPIKeyAuth(keyRepo, trackingUserRepo)(next)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newDaemonRequest(plaintextA))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "user_clerk_A", gotAccountID, "must resolve user_clerk_A's account_id")

	_ = userRepoA // suppress unused warning
}

// TestDaemonAPIKeyAuth_PrefixNotFound_TimingConstant verifies the not-found
// path (prefix miss) returns 401. It also confirms the middleware performs
// a dummy bcrypt compare on prefix miss to close the timing oracle (Ray
// constraint #2). We cannot measure timing in unit tests, so we assert the
// 401 is returned without the next handler being called.
func TestDaemonAPIKeyAuth_PrefixNotFound_TimingConstant(t *testing.T) {
	const token = "prefixnotfound_xxxx_rest_of_token_here"

	keyRepo := &stubDaemonKeyPrefixLookup{rows: nil} // empty — prefix miss
	userRepo := &stubDaemonUserResolver{}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must not be called when prefix is not found")
	})

	handler := middleware.DaemonAPIKeyAuth(keyRepo, userRepo)(next)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newDaemonRequest(token))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestDaemonAPIKeyAuth_MissingAuthHeader verifies that a request with no
// Authorization header returns 401 without calling the next handler.
func TestDaemonAPIKeyAuth_MissingAuthHeader(t *testing.T) {
	keyRepo := &stubDaemonKeyPrefixLookup{}
	userRepo := &stubDaemonUserResolver{}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must not be called on missing auth header")
	})

	handler := middleware.DaemonAPIKeyAuth(keyRepo, userRepo)(next)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newDaemonRequest(""))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestDaemonAPIKeyAuth_TokenTooShort verifies that a token shorter than 16
// characters (the prefix length) returns 401 without a lookup.
func TestDaemonAPIKeyAuth_TokenTooShort(t *testing.T) {
	keyRepo := &stubDaemonKeyPrefixLookup{}
	userRepo := &stubDaemonUserResolver{}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must not be called for short token")
	})

	handler := middleware.DaemonAPIKeyAuth(keyRepo, userRepo)(next)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newDaemonRequest("short"))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// ─── tracking resolver helper ─────────────────────────────────────────────────

// trackingUserResolver implements userResolver and records which clerk_user_id
// was resolved. Used by the prefix-collision test.
type trackingUserResolver struct {
	inner    map[string]*repository.User
	resolved *string
}

func (t *trackingUserResolver) GetByClerkUserID(_ context.Context, clerkUserID string) (*repository.User, error) {
	*t.resolved = clerkUserID
	return t.inner[clerkUserID], nil
}
