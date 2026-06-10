package middleware

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	lru "github.com/hashicorp/golang-lru/v2"
)

const (
	// internalSvcIssuer is the expected iss claim on service-to-service JWTs.
	// Defined in ADR-070 §2.
	internalSvcIssuer = "vaultmtg-internal"

	// internalSvcAudience is the expected aud claim for tokens targeting the BFF.
	// Defined in ADR-070 §2.
	internalSvcAudience = "vault-mtg-bff"

	// clockSkewLeeway is the maximum allowed difference between the token's
	// IssuedAt and the verifier's wall clock. Approved by Ray (ticket #952).
	clockSkewLeeway = 30 * time.Second

	// jtiCacheSize is the maximum number of JTI entries in the replay-guard LRU.
	// ADR-070 §2: 1,000 entries. Each entry: jti string (≤36 bytes) + expiry
	// time.Time (24 bytes) ≈ 60 bytes ⇒ < 64KB total.
	jtiCacheSize = 1_000

	// jtiTTL is the duration a used JTI is remembered. ADR-070 §2: 10 minutes.
	// Must be > token TTL (5 minutes) so a replayed token is still rejected
	// within its validity window.
	jtiTTL = 10 * time.Minute
)

// internalSvcOption is the type for functional options on RequireInternalSvcAuth.
// Unexported per Ray's ruling (ticket #952): clock injection is for tests only;
// production callers must not be able to override the clock.
type internalSvcOption func(*internalSvcConfig)

type internalSvcConfig struct {
	clockFn func() time.Time
}

// WithClockFunc injects a custom clock function for unit-testing clock-skew
// behaviour. Unexported — production callers cannot access this option.
// The option name is exported (WithClockFunc) so the test package (middleware_test)
// can call it by name, but the underlying internalSvcOption type is not.
func WithClockFunc(fn func() time.Time) internalSvcOption {
	return func(c *internalSvcConfig) {
		c.clockFn = fn
	}
}

// RequireInternalSvcAuth returns middleware that validates service-to-service
// JWTs on the /internal/v1/* route group (ADR-070).
//
// Verification steps (all must pass):
//  1. Configured secret is non-empty (fail-closed when misconfigured).
//  2. Authorization: Bearer header is present and non-empty.
//  3. Token parses as a valid JWT (not tampered, correct structure).
//  4. Algorithm is exactly HS256 (WithValidMethods rejects none/RS256/etc.).
//  5. Signature is valid for the configured HMAC-SHA256 secret.
//  6. iss == "vaultmtg-internal".
//  7. aud contains "vault-mtg-bff".
//  8. exp > now (with clockSkewLeeway tolerance).
//  9. iat <= now + clockSkewLeeway (prevents far-future tokens).
//  10. jti is present, non-empty, and not already seen in the replay cache.
//
// On any failure the middleware writes 401 JSON and does not call next.
// nginx MUST deny /internal/ at the proxy layer — BFF enforcement is the
// primary gate (ADR-070 §3).
func RequireInternalSvcAuth(secret string, opts ...internalSvcOption) func(http.Handler) http.Handler {
	cfg := &internalSvcConfig{
		clockFn: time.Now,
	}
	for _, o := range opts {
		o(cfg)
	}

	// Allocate the replay-guard LRU once per middleware instance so it is
	// shared across all requests on the same BFF process. The LRU evicts the
	// least-recently-used entry when full, which is safe here because entries
	// older than jtiTTL are no longer within the token validity window anyway.
	jtiCache, err := lru.New[string, time.Time](jtiCacheSize)
	if err != nil {
		// lru.New only returns an error when size <= 0 — unreachable with a
		// positive constant. Panic so the misconfiguration is caught at startup.
		panic("internal_svc_auth: lru.New: " + err.Error())
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Step 1 — fail-closed when secret is not configured.
			if secret == "" {
				log.Printf("[internal_svc_auth] outcome=fail reason=secret_not_configured path=%s remote=%s",
					r.URL.Path, internalRemoteAddr(r))
				writeUnauthorized(w)
				return
			}

			// Step 2 — extract Bearer token.
			raw, ok := bearerToken(r)
			if !ok {
				log.Printf("[internal_svc_auth] outcome=fail reason=missing_bearer path=%s remote=%s",
					r.URL.Path, internalRemoteAddr(r))
				writeUnauthorized(w)
				return
			}

			now := cfg.clockFn()

			// Steps 3–8 — parse + verify signature, algorithm, iss, aud, exp.
			token, parseErr := jwt.ParseWithClaims(
				raw,
				&jwt.RegisteredClaims{},
				func(t *jwt.Token) (any, error) {
					return []byte(secret), nil
				},
				jwt.WithValidMethods([]string{"HS256"}),
				jwt.WithIssuer(internalSvcIssuer),
				jwt.WithAudience(internalSvcAudience),
				jwt.WithLeeway(clockSkewLeeway),
				jwt.WithTimeFunc(func() time.Time { return now }),
				jwt.WithExpirationRequired(),
			)
			if parseErr != nil || !token.Valid {
				log.Printf("[internal_svc_auth] outcome=fail reason=invalid_token err=%v path=%s remote=%s",
					parseErr, r.URL.Path, internalRemoteAddr(r))
				writeUnauthorized(w)
				return
			}

			claims, ok := token.Claims.(*jwt.RegisteredClaims)
			if !ok {
				log.Printf("[internal_svc_auth] outcome=fail reason=claims_type_error path=%s remote=%s",
					r.URL.Path, internalRemoteAddr(r))
				writeUnauthorized(w)
				return
			}

			// Step 9 — iat must not be more than clockSkewLeeway in the future.
			// golang-jwt's WithLeeway allows token to appear slightly future-issued
			// (leeway applies to both exp and nbf checks), but we also enforce that
			// iat is not more than clockSkewLeeway seconds ahead of our clock to
			// prevent tokens minted far in the future from slipping through.
			if claims.IssuedAt != nil {
				if claims.IssuedAt.Time.After(now.Add(clockSkewLeeway)) {
					log.Printf("[internal_svc_auth] outcome=fail reason=iat_too_far_future path=%s remote=%s",
						r.URL.Path, internalRemoteAddr(r))
					writeUnauthorized(w)
					return
				}
			}

			// Step 10 — replay guard: jti must be present and not already used.
			jti := strings.TrimSpace(claims.ID)
			if jti == "" {
				log.Printf("[internal_svc_auth] outcome=fail reason=missing_jti path=%s remote=%s",
					r.URL.Path, internalRemoteAddr(r))
				writeUnauthorized(w)
				return
			}

			// Sweep expired entries lazily on each request so the cache does not
			// retain stale entries beyond jtiTTL even when it is not full.
			sweepExpiredJTIs(jtiCache, now)

			if expiry, seen := jtiCache.Get(jti); seen {
				// Defensive: if the cached entry is already past jtiTTL, allow it
				// through (the token's exp would also be past, but belt-and-suspenders).
				if now.Before(expiry) {
					log.Printf("[internal_svc_auth] outcome=fail reason=replay_detected jti=%s path=%s remote=%s",
						jti, r.URL.Path, internalRemoteAddr(r))
					writeUnauthorized(w)
					return
				}
				// Entry has expired — remove it so the cache stays clean.
				jtiCache.Remove(jti)
			}

			// Record the JTI in the replay cache with a TTL anchored to now.
			jtiCache.Add(jti, now.Add(jtiTTL))

			log.Printf("[internal_svc_auth] outcome=ok sub=%s path=%s remote=%s",
				claims.Subject, r.URL.Path, internalRemoteAddr(r))

			next.ServeHTTP(w, r)
		})
	}
}

// sweepExpiredJTIs removes cache entries whose expiry has passed. Called lazily
// on each request to bound memory without a background goroutine.
func sweepExpiredJTIs(cache *lru.Cache[string, time.Time], now time.Time) {
	for _, key := range cache.Keys() {
		if expiry, ok := cache.Peek(key); ok && now.After(expiry) {
			cache.Remove(key)
		}
	}
}

// internalRemoteAddr returns X-Forwarded-For (first value) or r.RemoteAddr.
// Mirrors adminRemoteAddr — the IP is forensic context, never a secret.
func internalRemoteAddr(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}
	return r.RemoteAddr
}
