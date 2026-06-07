package middleware_test

// clerk_cross_env_test.go — FF-2 cross-environment JWT rejection tests.
//
// These tests verify that a JWT issued by one Clerk environment (e.g. staging)
// is rejected by a BFF keyed for a different environment (e.g. prod), and vice
// versa.  The attack surface is a mis-configured deployment where a developer
// or attacker presents a valid staging token against a prod BFF (or vice versa).
//
// Design:
//   - Each "environment" is modelled as an independent RSA key pair.
//   - The BFF for each environment is initialised via RequireClerkAuth with a
//     distinct dummy secret key ("sk_live_prod_dummy" / "sk_test_staging_dummy").
//   - The JWKS served by each environment's mock backend contains ONLY its own
//     public key; the other environment's key is absent.
//   - A token signed with env-A's key must be rejected by env-B's BFF (the
//     verifying JWKS contains no key with that KID), and accepted by env-A's BFF.
//
// No integration build tag — runs under the existing `go test ./services/bff/...`
// suite alongside the other middleware unit tests.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/clerk/clerk-sdk-go/v2/clerktest"
)

// buildCrossEnvClaims returns a minimal, valid Clerk JWT claim set for use in
// cross-environment tests.  The issuer follows the "https://clerk.*" pattern
// required by the Clerk SDK's jwt.Verify.
func buildCrossEnvClaims(sub string) map[string]any {
	now := time.Now()
	return map[string]any{
		"sub": sub,
		"sid": "sess_cross_env",
		"iss": "https://clerk.test",
		"iat": now.Add(-1 * time.Minute).Unix(),
		"nbf": now.Add(-1 * time.Minute).Unix(),
		"exp": now.Add(1 * time.Hour).Unix(),
	}
}

// TestCrossEnv_StagingTokenRejectedByProdBFF verifies that a JWT signed with
// the staging Clerk key is rejected with 401 when presented to a BFF that only
// knows about the prod JWKS.  This is the primary FF-2 threat: a staging token
// must never grant access to a prod-keyed BFF.
func TestCrossEnv_StagingTokenRejectedByProdBFF(t *testing.T) {
	// Generate an independent staging key pair.
	stagingKID := "staging-kid-ff2"
	stagingToken, _ := clerktest.GenerateJWT(t, buildCrossEnvClaims("user_staging"), stagingKID)

	// Generate an independent prod key pair.
	prodKID := "prod-kid-ff2"
	_, prodPubKey := clerktest.GenerateJWT(t, buildCrossEnvClaims("user_prod"), prodKID)

	// Point the SDK at a JWKS that contains ONLY the prod public key.
	// The staging token was signed with a different key (stagingKID) — the
	// prod JWKS has no entry for it, so verification must fail.
	withClerkBackend(t, prodKID, prodPubKey)

	handler := middleware.RequireClerkAuth("sk_live_prod_dummy")(clerkOKHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+stagingToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("staging token against prod BFF: want 401, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
}

// TestCrossEnv_ProdTokenRejectedByStagingBFF verifies the mirror case: a JWT
// signed with the prod Clerk key is rejected with 401 by a BFF that only knows
// about the staging JWKS.
func TestCrossEnv_ProdTokenRejectedByStagingBFF(t *testing.T) {
	// Generate an independent prod key pair.
	prodKID := "prod-kid-ff2-mirror"
	prodToken, _ := clerktest.GenerateJWT(t, buildCrossEnvClaims("user_prod"), prodKID)

	// Generate an independent staging key pair.
	stagingKID := "staging-kid-ff2-mirror"
	_, stagingPubKey := clerktest.GenerateJWT(t, buildCrossEnvClaims("user_staging"), stagingKID)

	// Point the SDK at a JWKS that contains ONLY the staging public key.
	// The prod token was signed with a different key (prodKID) — absent from
	// the staging JWKS — so verification must fail.
	withClerkBackend(t, stagingKID, stagingPubKey)

	handler := middleware.RequireClerkAuth("sk_test_staging_dummy")(clerkOKHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+prodToken)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("prod token against staging BFF: want 401, got %d — body: %s",
			rr.Code, rr.Body.String())
	}
}

// TestCrossEnv_SameEnvTokenAccepted is the same-environment sanity check:
// a token signed with the env's own key, verified against the env's own JWKS,
// must pass with 200 and the correct subject claim returned.  This confirms the
// test harness is wired correctly and that the two rejection tests above cannot
// trivially pass because the middleware is broken.
func TestCrossEnv_SameEnvTokenAccepted(t *testing.T) {
	kid := "same-env-kid-ff2"
	claims := buildCrossEnvClaims("user_same_env")
	token, pubKey := clerktest.GenerateJWT(t, claims, kid)

	// Point the SDK at a JWKS that contains the matching public key.
	withClerkBackend(t, kid, pubKey)

	handler := middleware.RequireClerkAuth("sk_test_same_env_dummy")(clerkOKHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("same-env token: want 200, got %d — body: %s",
			rr.Code, rr.Body.String())
	}

	if rr.Body.String() != "user_same_env" {
		t.Errorf("subject: want \"user_same_env\", got %q", rr.Body.String())
	}
}
