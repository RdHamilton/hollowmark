package middleware_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RdHamilton/vault-mtg/services/bff/internal/api/middleware"
	"github.com/RdHamilton/vault-mtg/services/bff/internal/storage/repository"

	"github.com/clerk/clerk-sdk-go/v2"
)

// stubUpsertRepo implements clerkUserUpsertRepo for resolver tests.
type stubUpsertRepo struct {
	user    *repository.User
	failErr error
}

func (s *stubUpsertRepo) UpsertByClerkUserID(_ context.Context, _ string) (*repository.User, error) {
	if s.failErr != nil {
		return nil, s.failErr
	}

	return s.user, nil
}

// setClerkClaims injects Clerk session claims onto the request context so that
// ClerkUserIDFromContext can read them — simulating what RequireClerkAuth does.
func setClerkClaims(r *http.Request, sub string) *http.Request {
	claims := &clerk.SessionClaims{}
	claims.Subject = sub
	ctx := clerk.ContextWithSessionClaims(r.Context(), claims)

	return r.WithContext(ctx)
}

// TestClerkUserResolver_MissingClerkID verifies that if no Clerk claims are on
// the context (RequireClerkAuth was not run first), the resolver returns 500.
func TestClerkUserResolver_MissingClerkID(t *testing.T) {
	repo := &stubUpsertRepo{user: &repository.User{ID: 42}}
	handler := middleware.ClerkUserResolver(repo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	// No Clerk claims on context.
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("missing Clerk ID: want 500, got %d", rr.Code)
	}
}

// TestClerkUserResolver_DBError verifies that a repo failure returns 500.
func TestClerkUserResolver_DBError(t *testing.T) {
	repo := &stubUpsertRepo{failErr: errors.New("connection refused")}
	handler := middleware.ClerkUserResolver(repo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = setClerkClaims(req, "user_abc")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("DB error: want 500, got %d", rr.Code)
	}
}

// TestClerkUserResolver_Success verifies that a valid Clerk ID resolves to an
// int64 user ID and the next handler is called with that ID in context.
func TestClerkUserResolver_Success(t *testing.T) {
	clerkID := "user_resolver_test"
	repo := &stubUpsertRepo{user: &repository.User{ID: 99, Email: "test@example.com", ClerkUserID: &clerkID}}

	var gotUserID int64
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, ok := middleware.UserIDFromContext(r.Context())
		if !ok {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		gotUserID = uid
		w.WriteHeader(http.StatusOK)
	})

	handler := middleware.ClerkUserResolver(repo)(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = setClerkClaims(req, clerkID)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("success: want 200, got %d", rr.Code)
	}

	if gotUserID != 99 {
		t.Errorf("user ID: want 99, got %d", gotUserID)
	}
}

// TestClerkUserResolver_EmptyClerkID verifies that an empty Clerk subject
// results in 500 (guards against malformed tokens that pass JWT verification
// but have no subject).
func TestClerkUserResolver_EmptyClerkID(t *testing.T) {
	repo := &stubUpsertRepo{user: &repository.User{ID: 1}}
	handler := middleware.ClerkUserResolver(repo)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = setClerkClaims(req, "") // empty subject
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("empty Clerk ID: want 500, got %d", rr.Code)
	}
}
