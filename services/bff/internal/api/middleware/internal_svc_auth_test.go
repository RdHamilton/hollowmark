package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	middleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
)

// sentinelHandler is a downstream handler that records whether it was called.
type sentinelHandler struct{ called bool }

func (h *sentinelHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.called = true
	w.WriteHeader(http.StatusOK)
}

const (
	testSecret = "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
	testIssuer = "vaultmtg-internal"
	testAud    = "vault-mtg-bff"
	testSub    = "mtga-user-sync"
)

var jtiCounter int

// mintJTI returns a unique JTI value per test run to avoid cross-test replay
// interference when the same JTI LRU cache is shared within a test binary.
func mintJTI() string {
	jtiCounter++
	return fmt.Sprintf("test-jti-%d-%d", time.Now().UnixNano(), jtiCounter)
}

// mintToken creates a signed HS256 JWT with the given overrides applied on top
// of a valid baseline.
func mintToken(t *testing.T, secret string, overrides func(*jwt.RegisteredClaims)) string {
	t.Helper()
	claims := &jwt.RegisteredClaims{
		Issuer:    testIssuer,
		Subject:   testSub,
		Audience:  jwt.ClaimStrings{testAud},
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		ID:        mintJTI(),
	}
	if overrides != nil {
		overrides(claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("mintToken: %v", err)
	}
	return signed
}

func newRequest(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/internal/v1/health", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

// --- Test cases ---

// TestRequireInternalSvcAuth_ValidToken verifies that a well-formed, correctly
// signed, non-expired token with the right aud/iss is accepted (HTTP 200 and
// the downstream handler is called).
func TestRequireInternalSvcAuth_ValidToken(t *testing.T) {
	sentinel := &sentinelHandler{}
	mw := middleware.RequireInternalSvcAuth(testSecret)
	handler := mw(sentinel)

	token := mintToken(t, testSecret, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(token))

	if w.Code != http.StatusOK {
		t.Errorf("valid token: want 200, got %d", w.Code)
	}
	if !sentinel.called {
		t.Error("valid token: downstream handler not called")
	}
}

// TestRequireInternalSvcAuth_ExpiredToken verifies that a token whose exp is
// well in the past (beyond the 30-second clock-skew leeway) is rejected with 401.
func TestRequireInternalSvcAuth_ExpiredToken(t *testing.T) {
	sentinel := &sentinelHandler{}
	mw := middleware.RequireInternalSvcAuth(testSecret)
	handler := mw(sentinel)

	// Expired 5 minutes ago — well outside the 30-second leeway window.
	token := mintToken(t, testSecret, func(c *jwt.RegisteredClaims) {
		c.IssuedAt = jwt.NewNumericDate(time.Now().Add(-10 * time.Minute))
		c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-5 * time.Minute))
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(token))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expired token: want 401, got %d", w.Code)
	}
	if sentinel.called {
		t.Error("expired token: downstream handler must not be called")
	}
}

// TestRequireInternalSvcAuth_WrongAudience verifies that a token with the wrong
// aud claim is rejected with 401.
func TestRequireInternalSvcAuth_WrongAudience(t *testing.T) {
	sentinel := &sentinelHandler{}
	mw := middleware.RequireInternalSvcAuth(testSecret)
	handler := mw(sentinel)

	token := mintToken(t, testSecret, func(c *jwt.RegisteredClaims) {
		c.Audience = jwt.ClaimStrings{"not-the-bff"}
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(token))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong aud: want 401, got %d", w.Code)
	}
	if sentinel.called {
		t.Error("wrong aud: downstream handler must not be called")
	}
}

// TestRequireInternalSvcAuth_WrongIssuer verifies that a token with the wrong
// iss claim is rejected with 401.
func TestRequireInternalSvcAuth_WrongIssuer(t *testing.T) {
	sentinel := &sentinelHandler{}
	mw := middleware.RequireInternalSvcAuth(testSecret)
	handler := mw(sentinel)

	token := mintToken(t, testSecret, func(c *jwt.RegisteredClaims) {
		c.Issuer = "not-vaultmtg"
	})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(token))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("wrong iss: want 401, got %d", w.Code)
	}
	if sentinel.called {
		t.Error("wrong iss: downstream handler must not be called")
	}
}

// TestRequireInternalSvcAuth_WrongAlgorithm verifies that a token signed with
// the "none" algorithm (wrong algorithm) is rejected via WithValidMethods
// enforcement — only HS256 is permitted.
func TestRequireInternalSvcAuth_WrongAlgorithm(t *testing.T) {
	sentinel := &sentinelHandler{}
	mw := middleware.RequireInternalSvcAuth(testSecret)
	handler := mw(sentinel)

	// Use jwt.SigningMethodNone — golang-jwt accepts it only with UnsafeAllowNoneSignatureType.
	// RequireInternalSvcAuth restricts to HS256 via WithValidMethods so "none" is rejected.
	unsecuredToken := jwt.NewWithClaims(jwt.SigningMethodNone, &jwt.RegisteredClaims{
		Issuer:    testIssuer,
		Subject:   testSub,
		Audience:  jwt.ClaimStrings{testAud},
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		ID:        mintJTI(),
	})
	signed, err := unsecuredToken.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("mintToken (none alg): %v", err)
	}

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(signed))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("none-alg token: want 401, got %d", w.Code)
	}
	if sentinel.called {
		t.Error("none-alg token: downstream handler must not be called")
	}
}

// TestRequireInternalSvcAuth_BadSignature verifies that a token signed with a
// different secret is rejected with 401.
func TestRequireInternalSvcAuth_BadSignature(t *testing.T) {
	sentinel := &sentinelHandler{}
	mw := middleware.RequireInternalSvcAuth(testSecret)
	handler := mw(sentinel)

	// Signed with a completely different secret.
	token := mintToken(t, "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(token))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("bad signature: want 401, got %d", w.Code)
	}
	if sentinel.called {
		t.Error("bad signature: downstream handler must not be called")
	}
}

// TestRequireInternalSvcAuth_MissingBearer verifies that a request with no
// Authorization header is rejected with 401.
func TestRequireInternalSvcAuth_MissingBearer(t *testing.T) {
	sentinel := &sentinelHandler{}
	mw := middleware.RequireInternalSvcAuth(testSecret)
	handler := mw(sentinel)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(""))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("missing bearer: want 401, got %d", w.Code)
	}
	if sentinel.called {
		t.Error("missing bearer: downstream handler must not be called")
	}
}

// TestRequireInternalSvcAuth_EmptySecret verifies that when the configured
// secret is empty, ALL requests are rejected (fail-closed, analogous to
// AdminTokenAuth behaviour). An empty secret means the BFF is misconfigured;
// even a correctly-signed-with-empty-secret token must not be accepted.
func TestRequireInternalSvcAuth_EmptySecret(t *testing.T) {
	sentinel := &sentinelHandler{}
	mw := middleware.RequireInternalSvcAuth("")
	handler := mw(sentinel)

	// Even a perfectly valid token (signed with empty secret) must be rejected.
	token := mintToken(t, "", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(token))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("empty secret: want 401, got %d", w.Code)
	}
	if sentinel.called {
		t.Error("empty secret: downstream handler must not be called")
	}
}

// TestRequireInternalSvcAuth_ReplayAttack verifies that using the same JTI
// twice within the TTL window is rejected on the second use.
func TestRequireInternalSvcAuth_ReplayAttack(t *testing.T) {
	sentinel := &sentinelHandler{}
	mw := middleware.RequireInternalSvcAuth(testSecret)
	handler := mw(sentinel)

	fixedJTI := fmt.Sprintf("replay-test-jti-%d", time.Now().UnixNano())
	token := mintToken(t, testSecret, func(c *jwt.RegisteredClaims) {
		c.ID = fixedJTI
	})

	// First use — must pass.
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, newRequest(token))
	if w1.Code != http.StatusOK {
		t.Fatalf("replay first use: want 200, got %d", w1.Code)
	}

	// Second use with the same JTI — must be rejected (replay).
	sentinel.called = false
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, newRequest(token))
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("replay second use: want 401, got %d", w2.Code)
	}
	if sentinel.called {
		t.Error("replay second use: downstream handler must not be called")
	}
}

// TestRequireInternalSvcAuth_ClockSkewWithinBound verifies that a token whose
// iat is slightly in the future (within 30-second leeway) is still accepted.
// Ray-approved: 30s clock-skew leeway with unexported clock injection.
func TestRequireInternalSvcAuth_ClockSkewWithinBound(t *testing.T) {
	sentinel := &sentinelHandler{}
	// Inject a clock that is 25 seconds behind "real" time to simulate the
	// verifier running slightly ahead of the token minter.
	skewedNow := time.Now().Add(-25 * time.Second)
	mw := middleware.RequireInternalSvcAuth(
		testSecret,
		middleware.WithClockFunc(func() time.Time { return skewedNow }),
	)
	handler := mw(sentinel)

	// Token minted at real now — which is 25s ahead of the verifier's clock.
	token := mintToken(t, testSecret, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(token))

	if w.Code != http.StatusOK {
		t.Errorf("clock skew within 30s: want 200, got %d", w.Code)
	}
	if !sentinel.called {
		t.Error("clock skew within 30s: downstream handler should be called")
	}
}

// TestRequireInternalSvcAuth_ClockSkewOutsideBound verifies that a token whose
// iat is more than 30 seconds in the future from the verifier's perspective is
// rejected.
func TestRequireInternalSvcAuth_ClockSkewOutsideBound(t *testing.T) {
	sentinel := &sentinelHandler{}
	// Verifier clock is 60 seconds behind real time. Token iat = real now.
	// From the verifier's perspective, the token is 60s in the future — outside leeway.
	skewedNow := time.Now().Add(-60 * time.Second)
	mw := middleware.RequireInternalSvcAuth(
		testSecret,
		middleware.WithClockFunc(func() time.Time { return skewedNow }),
	)
	handler := mw(sentinel)

	token := mintToken(t, testSecret, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, newRequest(token))

	if w.Code != http.StatusUnauthorized {
		t.Errorf("clock skew outside 30s: want 401, got %d", w.Code)
	}
	if sentinel.called {
		t.Error("clock skew outside 30s: downstream handler must not be called")
	}
}
