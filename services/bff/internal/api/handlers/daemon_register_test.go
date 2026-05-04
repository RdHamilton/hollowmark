package handlers_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/ramonehamilton/mtga-bff/internal/api/handlers"
	"github.com/ramonehamilton/mtga-bff/internal/api/middleware"
)

const registerSecret = "register-test-secret"

func registerBody(t *testing.T, userID int64) *bytes.Reader {
	t.Helper()
	b, err := json.Marshal(map[string]int64{"user_id": userID})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return bytes.NewReader(b)
}

func TestDaemonRegister_Success(t *testing.T) {
	h := handlers.NewDaemonRegisterHandler(registerSecret)

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/register", registerBody(t, 5))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp struct {
		Token    string `json:"token"`
		DaemonID string `json:"daemon_id"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Token == "" {
		t.Error("expected non-empty token")
	}
	if resp.DaemonID == "" {
		t.Error("expected non-empty daemon_id")
	}

	// Validate the token can be parsed and contains the right user_id.
	var claims middleware.DaemonClaims
	tok, err := jwt.ParseWithClaims(resp.Token, &claims, func(t *jwt.Token) (any, error) {
		return []byte(registerSecret), nil
	})
	if err != nil || !tok.Valid {
		t.Fatalf("token invalid: %v", err)
	}
	if claims.UserID != 5 {
		t.Errorf("expected user_id=5, got %d", claims.UserID)
	}
	if claims.DaemonID != resp.DaemonID {
		t.Errorf("daemon_id mismatch: claims=%q body=%q", claims.DaemonID, resp.DaemonID)
	}
}

func TestDaemonRegister_InvalidJSON(t *testing.T) {
	h := handlers.NewDaemonRegisterHandler(registerSecret)

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/register",
		strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDaemonRegister_MissingUserID(t *testing.T) {
	h := handlers.NewDaemonRegisterHandler(registerSecret)

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/register",
		strings.NewReader(`{"user_id":0}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDaemonRegister_NegativeUserID(t *testing.T) {
	h := handlers.NewDaemonRegisterHandler(registerSecret)

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/register",
		strings.NewReader(`{"user_id":-1}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDaemonRegister_EmptySecret(t *testing.T) {
	h := handlers.NewDaemonRegisterHandler("")

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/register", registerBody(t, 1))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for missing secret, got %d", rr.Code)
	}
}

func TestDaemonRegister_TokenIsValidForIngestMiddleware(t *testing.T) {
	h := handlers.NewDaemonRegisterHandler(registerSecret)

	req := httptest.NewRequest(http.MethodPost, "/api/daemon/register", registerBody(t, 99))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.Register(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", rr.Code)
	}

	var resp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Now run the issued token through the DaemonJWTAuth middleware.
	var capturedUID int64
	capture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedUID, _ = middleware.DaemonUserIDFromContext(r.Context())
	})

	mwHandler := middleware.DaemonJWTAuth(registerSecret)(capture)
	ingestReq := httptest.NewRequest(http.MethodPost, "/v1/ingest/events", nil)
	ingestReq.Header.Set("Authorization", "Bearer "+resp.Token)
	ingestRR := httptest.NewRecorder()

	mwHandler.ServeHTTP(ingestRR, ingestReq)

	if ingestRR.Code != http.StatusOK {
		t.Fatalf("ingest middleware: expected 200, got %d", ingestRR.Code)
	}
	if capturedUID != 99 {
		t.Errorf("expected user_id=99, got %d", capturedUID)
	}
}
