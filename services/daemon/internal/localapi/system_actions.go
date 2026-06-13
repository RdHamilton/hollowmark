// Mutating POST handlers for the system action endpoints.
//
// POST /api/v1/system/sync-now  — triggers a manual collection memory-scan.
// POST /api/v1/system/grant-access — triggers the one-time helper-authorization
//     dialog (darwin: com.apple.TaskForPid-allow; no-op on other platforms).
//
// Both follow the ReplayFunc callback-injection pattern from replay.go:
//   - localapi defines the function-type alias and a Server setter method.
//   - The daemon service wires the real implementation in Run().
//   - A nil trigger → 503 Service Unavailable.
//   - 202 Accepted on success; fire-and-forget goroutine.
//   - 409 Conflict when the action is already in flight (atomic.Bool guard).
//   - 405 Method Not Allowed for non-POST.
//
// Kept in a separate file to avoid collision with Bianca's #1439 which edits
// handleSystemStatus / connectionStatusResponse in system.go.

package localapi

import (
	"context"
	"net/http"
)

// SyncNowFunc is the callback the daemon service registers so the localapi
// server can trigger a manual collection scan without importing the daemon
// package (import-cycle avoidance). The ctx is the server lifecycle context;
// it is cancelled when the daemon stops. The function runs in a goroutine
// and must not block the caller.
type SyncNowFunc func(ctx context.Context)

// GrantAccessFunc is the callback the daemon service registers for the
// grant-access action. authorizeCollectionHelper takes no ctx (it manages
// its own deadline internally), so the type alias reflects that.
type GrantAccessFunc func()

// SetSyncNowTrigger wires the callback that POST /api/v1/system/sync-now
// invokes. Call this from daemon.Service.Run before the localapi server has a
// chance to receive sync-now requests. Passing nil clears the trigger (makes
// the endpoint return 503).
func (s *Server) SetSyncNowTrigger(fn SyncNowFunc) {
	s.syncNowTrigger = fn
}

// SetGrantAccessTrigger wires the callback that POST /api/v1/system/grant-access
// invokes. Call this from daemon.Service.Run before the localapi server has a
// chance to receive grant-access requests. Passing nil clears the trigger.
func (s *Server) SetGrantAccessTrigger(fn GrantAccessFunc) {
	s.grantAccessTrigger = fn
}

// handleSystemSyncNow handles POST /api/v1/system/sync-now.
//
// Behaviour:
//   - 405 when the method is not POST.
//   - 503 when no SyncNowFunc has been registered.
//   - 409 when a sync is already in progress (atomic.Bool guard).
//   - 202 otherwise: launches the sync in a background goroutine that holds
//     the in-flight flag for its lifetime; response is sent immediately.
func (s *Server) handleSystemSyncNow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if s.syncNowTrigger == nil {
		writeJSON(w, r, http.StatusServiceUnavailable, struct {
			Error string `json:"error"`
		}{"sync-now not available — daemon not fully initialised"})
		return
	}

	if !s.syncNowInFlight.CompareAndSwap(false, true) {
		writeJSON(w, r, http.StatusConflict, struct {
			Error string `json:"error"`
		}{"sync already in progress"})
		return
	}

	// Capture trigger and lifecycle ctx before launching the goroutine.
	fn := s.syncNowTrigger
	ctx := s.ctx

	// The defer MUST run in the work goroutine so the in-flight flag is held
	// for the entire duration of the work, not just until the response is sent.
	go func() {
		defer s.syncNowInFlight.Store(false)
		fn(ctx)
	}()

	writeJSON(w, r, http.StatusAccepted, struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}{"accepted", "collection sync started"})
}

// handleSystemGrantAccess handles POST /api/v1/system/grant-access.
//
// Behaviour:
//   - 405 when the method is not POST.
//   - 503 when no GrantAccessFunc has been registered.
//   - 409 when a grant-access is already in progress.
//   - 202 otherwise: launches the action in a background goroutine.
//
// The endpoint is OS-agnostic: authorizeCollectionHelper already short-circuits
// on non-darwin internally, so we always return 202 when a trigger is registered.
func (s *Server) handleSystemGrantAccess(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if s.grantAccessTrigger == nil {
		writeJSON(w, r, http.StatusServiceUnavailable, struct {
			Error string `json:"error"`
		}{"grant-access not available — daemon not fully initialised"})
		return
	}

	if !s.grantAccessInFlight.CompareAndSwap(false, true) {
		writeJSON(w, r, http.StatusConflict, struct {
			Error string `json:"error"`
		}{"grant-access already in progress"})
		return
	}

	fn := s.grantAccessTrigger

	go func() {
		defer s.grantAccessInFlight.Store(false)
		fn()
	}()

	writeJSON(w, r, http.StatusAccepted, struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}{"accepted", "grant-access started"})
}
