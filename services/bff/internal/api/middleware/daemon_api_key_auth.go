// Package middleware — Daemon API key authentication.
//
// The PKCE-minted daemon api_key lives in the daemon_api_keys table, NOT the
// api_keys table that the regular APIKeyAuth middleware checks. The two
// systems have different key shapes:
//
//   - api_keys.user_id is int64 (foreign key to users.id)
//   - daemon_api_keys.account_id is a Clerk user_id string
//
// This middleware bridges the two: it validates a Bearer token against
// daemon_api_keys, resolves the matching daemon's account_id (Clerk user_id)
// to users.id via users.clerk_user_id, and sets the int64 user_id on the
// request context using the same key as APIKeyAuth so downstream handlers
// (UserIDFromContext) work unchanged.

package middleware

import (
	"context"
	"log"
	"net/http"

	"github.com/ramonehamilton/mtga-bff/internal/storage/repository"
	"golang.org/x/crypto/bcrypt"
)

// daemonKeyLister is the minimal interface required from the daemon api key
// repository. Returns all non-revoked daemon api keys for bcrypt comparison.
type daemonKeyLister interface {
	ListAllActive(ctx context.Context) ([]repository.DaemonAPIKey, error)
	UpdateLastUsed(ctx context.Context, id string) error
}

// userResolver is the minimal interface required to resolve a Clerk user_id
// (string) to the corresponding users.id (int64).
type userResolver interface {
	GetByClerkUserID(ctx context.Context, clerkUserID string) (*repository.User, error)
}

// DaemonAPIKeyAuth returns middleware that validates a daemon api_key
// (issued by POST /api/v1/daemon/register) on the Authorization: Bearer
// header. On success, sets the resolved int64 users.id on the request
// context — interoperable with UserIDFromContext.
//
// Failure modes (all return 401):
//   - Missing/malformed Authorization header
//   - No daemon_api_keys row matches the bcrypt-hashed bearer
//   - The matched key's account_id has no corresponding users row
//
// Use this in place of APIKeyAuth on routes the daemon hits after the PKCE
// register flow (currently POST /api/v1/ingest/events).
func DaemonAPIKeyAuth(keyRepo daemonKeyLister, userRepo userResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				writeUnauthorized(w)
				return
			}

			keys, err := keyRepo.ListAllActive(r.Context())
			if err != nil {
				log.Printf("[daemon_auth] ListAllActive: %v", err)
				writeUnauthorized(w)
				return
			}

			for _, k := range keys {
				if bcrypt.CompareHashAndPassword([]byte(k.KeyHash), []byte(token)) != nil {
					continue
				}

				// Matched. Resolve Clerk account_id → users.id (int64) so the
				// rest of the pipeline can use the standard UserIDFromContext.
				u, err := userRepo.GetByClerkUserID(r.Context(), k.AccountID)
				if err != nil || u == nil {
					if err != nil {
						log.Printf("[daemon_auth] GetByClerkUserID(%s): %v", k.AccountID, err)
					} else {
						log.Printf("[daemon_auth] daemon api key matched but no users row for clerk_user_id=%s — daemon_register did not upsert the user", k.AccountID)
					}
					writeUnauthorized(w)
					return
				}

				// Update last_used in the background to avoid blocking the request.
				keyID := k.ID
				go func() {
					if err := keyRepo.UpdateLastUsed(context.Background(), keyID); err != nil {
						log.Printf("[daemon_auth] UpdateLastUsed id=%s: %v", keyID, err)
					}
				}()

				ctx := context.WithValue(r.Context(), ctxKeyUserID, u.ID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			writeUnauthorized(w)
		})
	}
}
