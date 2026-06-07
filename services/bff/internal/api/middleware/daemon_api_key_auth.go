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
//
// Auth flow (O(1) index lookup + O(M) bcrypt where M is the prefix collision
// set, typically 1):
//
//  1. Extract the 16-byte prefix from the bearer token (token[:16]).
//  2. Query daemon_api_keys WHERE key_prefix = $1 AND revoked_at IS NULL
//     using the partial index daemon_api_keys_key_prefix_active_idx.
//  3. bcrypt-compare each returned row — the prefix NEVER gates auth; bcrypt
//     is ALWAYS the auth decision (Ray constraint #1).
//  4. On a prefix miss, run a dummy bcrypt to close the timing oracle that
//     would otherwise leak whether a prefix exists (Ray constraint #2).
//  5. The query has no LIMIT — prefix is not unique; all matching rows are
//     returned and bcrypt-looped so a collision cannot become an auth-bypass
//     (Ray constraint #3).
//  6. ListAllActive is retained in the repo for other callers (Ray constraint #4).

package middleware

import (
	"context"
	"log"
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// keyPrefixLen is the number of leading bytes used as the lookup prefix.
// Must match the length stored in daemon_api_keys.key_prefix at mint time.
const keyPrefixLen = 16

// dummyBcryptHash is a pre-computed bcrypt hash used for the constant-time
// dummy compare on prefix-miss. Generated at cost 10 (matching production
// mint cost) so timing is comparable to a real compare.
//
// Value: bcrypt("__vaultmtg_dummy_token__", cost=10).
// Pre-computed to avoid regenerating on every request (bcrypt.GenerateFromPassword
// at cost 10 takes ~100ms — doing it per-request on a miss would be worse than
// the O(N) scan we just fixed).
const dummyBcryptHash = "$2a$10$Ry0bJZrN3xLqMvWoK8P2uO2qZkL1jN4mH7vT9dW3cX8bE5yF6aG1i"

// daemonKeyPrefixLookup is the minimal interface required from the daemon api
// key repository for the prefix-indexed auth path.
type daemonKeyPrefixLookup interface {
	// GetByPrefix returns all non-revoked rows whose key_prefix matches.
	// Returns an empty slice (not an error) when no row matches.
	GetByPrefix(ctx context.Context, prefix string) ([]repository.DaemonAPIKey, error)
	// UpdateLastUsed sets last_used_at to now for the given key id.
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
// The hot path uses GetByPrefix to narrow candidates via the partial index
// daemon_api_keys_key_prefix_active_idx (migration 000085), then bcrypt-
// compares each candidate. This reduces the bcrypt work from O(N-total-keys)
// to O(M-prefix-collision-set), which is typically 1.
//
// Failure modes (all return 401):
//   - Missing/malformed Authorization header
//   - Token shorter than keyPrefixLen (16 bytes)
//   - No active row's hash matches the bearer token
//   - The matched key's account_id has no corresponding users row
//
// Use this in place of APIKeyAuth on routes the daemon hits after the PKCE
// register flow (currently POST /api/v1/ingest/events).
func DaemonAPIKeyAuth(keyRepo daemonKeyPrefixLookup, userRepo userResolver) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				writeUnauthorized(w)
				return
			}

			// Token must be at least keyPrefixLen bytes — shorter tokens cannot
			// have been minted by our register endpoint.
			if len(token) < keyPrefixLen {
				writeUnauthorized(w)
				return
			}

			prefix := token[:keyPrefixLen]

			candidates, err := keyRepo.GetByPrefix(r.Context(), prefix)
			if err != nil {
				log.Printf("[daemon_auth] GetByPrefix(prefix=%s): %v", prefix, err)
				writeUnauthorized(w)
				return
			}

			// Prefix miss — run a dummy bcrypt to prevent a timing oracle that
			// would leak whether a given prefix exists in the database.
			// Ray constraint #2: constant-time not-found path.
			if len(candidates) == 0 {
				_ = bcrypt.CompareHashAndPassword([]byte(dummyBcryptHash), []byte(token))
				writeUnauthorized(w)
				return
			}

			// bcrypt-loop the (tiny) candidate set.
			// Ray constraint #1: bcrypt MUST gate the auth decision on every
			// matched row — the prefix is a narrowing hint, never the auth guard.
			// Ray constraint #3: no LIMIT — all rows with the prefix are looped
			// so a prefix collision cannot become an auth-bypass.
			for _, k := range candidates {
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
