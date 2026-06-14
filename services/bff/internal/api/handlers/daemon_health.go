package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
)

// daemonHealthWindow is the look-back duration used to decide whether the
// daemon is connected.  A daemon_events row with received_at within this
// window means the daemon is actively polling.
//
// 90 s: the daemon sends a daemon.heartbeat event every 30 s, so three missed
// beats before the indicator flips — enough buffer for transient network hiccups
// without leaving the UI stale for long.
const daemonHealthWindow = 90 * time.Second

// DaemonHealthChecker is the minimal interface DaemonHealthHandler needs.
type DaemonHealthChecker interface {
	HasRecentEventByUserID(ctx context.Context, userID int64, window time.Duration) (bool, error)
	// GetLatestHeartbeatAuthStatus returns the auth_status string from the most
	// recent daemon.heartbeat payload for the given user. Returns ("unknown", nil)
	// when no heartbeat row exists or the field is absent (old daemon, pre-#144).
	// The "unknown" sentinel is BFF-only — the daemon never emits it (#144).
	GetLatestHeartbeatAuthStatus(ctx context.Context, userID int64) (string, error)
}

// DaemonHealthHandler handles GET /api/v1/health/daemon.
type DaemonHealthHandler struct {
	checker DaemonHealthChecker
}

// NewDaemonHealthHandler returns a DaemonHealthHandler backed by checker.
func NewDaemonHealthHandler(checker DaemonHealthChecker) *DaemonHealthHandler {
	return &DaemonHealthHandler{checker: checker}
}

// daemonHealthResponse is the JSON body for GET /api/v1/health/daemon.
type daemonHealthResponse struct {
	Status     string `json:"status"`      // "connected" | "disconnected"
	AuthStatus string `json:"auth_status"` // "authenticated" | "setup_required" | "keychain_error" | "auth_paused" | "unknown"
}

// GetDaemonHealth handles GET /api/v1/health/daemon.
//
// Always returns 200.  The body fields are:
//   - status:      "connected" | "disconnected" — based on recent heartbeat recency
//   - auth_status: one of the 5-value union from #144; "unknown" is a BFF-only
//     sentinel meaning "no heartbeat data yet or pre-#144 daemon" — not an error
//     state, not a reason to show a Retry affordance in the SPA (#112 contract).
func (h *DaemonHealthHandler) GetDaemonHealth(w http.ResponseWriter, r *http.Request) {
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	connected, err := h.checker.HasRecentEventByUserID(r.Context(), userID, daemonHealthWindow)
	if err != nil {
		log.Printf("[DaemonHealthHandler] HasRecentEventByUserID userID=%d: %v", userID, err)
		// Internal error — treat as disconnected rather than surfacing 500.
		connected = false
	}

	// Fetch auth_status from the most recent heartbeat payload. All error paths
	// (ErrNoRows, decode error, empty field) return ("unknown", nil) from the
	// repo; we absorb any repo error here and fall back to "unknown" so the
	// SPA never receives a 500 for this optional field (#144, Ray verdict §2).
	authStatus, err := h.checker.GetLatestHeartbeatAuthStatus(r.Context(), userID)
	if err != nil {
		log.Printf("[DaemonHealthHandler] GetLatestHeartbeatAuthStatus userID=%d: %v", userID, err)
		authStatus = "unknown"
	}

	status := "disconnected"
	if connected {
		status = "connected"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(daemonHealthResponse{
		Status:     status,
		AuthStatus: authStatus,
	}); err != nil {
		log.Printf("[DaemonHealthHandler] encode: %v", err)
	}
}
