package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/ramonehamilton/mtga-bff/internal/api/middleware"
)

// daemonRegisterRequest is the JSON body expected at POST /api/daemon/register.
type daemonRegisterRequest struct {
	// UserID identifies the MTGA Companion user that owns this daemon.
	UserID int64 `json:"user_id"`
}

// daemonRegisterResponse is the JSON body returned on a successful registration.
type daemonRegisterResponse struct {
	Token    string `json:"token"`
	DaemonID string `json:"daemon_id"`
}

// DaemonRegisterHandler handles daemon registration requests.
type DaemonRegisterHandler struct {
	jwtSecret string
}

// NewDaemonRegisterHandler returns a handler that issues daemon JWTs signed
// with jwtSecret.
func NewDaemonRegisterHandler(jwtSecret string) *DaemonRegisterHandler {
	return &DaemonRegisterHandler{jwtSecret: jwtSecret}
}

// Register handles POST /api/daemon/register.
//
// The caller supplies a user_id identifying the MTGA Companion user. The
// handler allocates a new daemon_id UUID, signs a JWT (HS256, 30-day expiry),
// and returns it. The token must then be sent as "Authorization: Bearer <token>"
// on all /v1/ingest/events requests.
func (h *DaemonRegisterHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req daemonRegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.UserID <= 0 {
		writeJSONError(w, "user_id must be a positive integer", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(h.jwtSecret) == "" {
		log.Println("[DaemonRegisterHandler] DAEMON_JWT_SECRET is not configured")
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	daemonID := uuid.NewString()

	token, err := middleware.IssueDaemonJWT(h.jwtSecret, req.UserID, daemonID)
	if err != nil {
		log.Printf("[DaemonRegisterHandler] IssueDaemonJWT: %v", err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	resp := daemonRegisterResponse{
		Token:    token,
		DaemonID: daemonID,
	}
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("[DaemonRegisterHandler] encode response: %v", err)
	}
}
