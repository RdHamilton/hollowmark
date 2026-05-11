// Package localapi serves a small HTTP API on localhost so the VaultMTG SPA
// can detect that the daemon is running and (eventually) read live state.
//
// The server binds to 127.0.0.1 only — never an external interface — and
// listens on port 9001 by default. The SPA polls /health to drive the
// "daemon connected" indicator on Setup.tsx; future phases will add system
// status and proxy endpoints (see docs/product/milestones/v0.3.1/daemon-local-api-plan.md).
package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

// DefaultPort is the loopback TCP port the daemon's local HTTP API listens on.
// Hardcoded in both daemon and SPA (frontend/src/pages/Setup.tsx) to avoid a
// discovery handshake; users do not configure this.
const DefaultPort = 9001

// shutdownTimeout caps how long the local API server takes to drain on stop.
const shutdownTimeout = 5 * time.Second

// State is the subset of daemon state exposed by the local API. Populated by
// the daemon at construction time; fields the daemon does not yet know
// (e.g. account_id before PKCE completes) may be empty.
type State struct {
	Version   string
	SessionID string
	StartedAt time.Time
	AccountID string
}

// Server is the loopback HTTP server. Construct with New, then call Start
// before the daemon enters its main run loop and Stop on shutdown.
type Server struct {
	port  int
	state State
	srv   *http.Server
	ln    net.Listener
}

// New returns a Server bound to 127.0.0.1:port. Use DefaultPort unless tests
// need an ephemeral port (pass 0 to let the OS pick).
func New(port int, state State) *Server {
	return &Server{port: port, state: state}
}

// Start binds the listener and serves in a background goroutine. Returns once
// the listener is accepting connections, so callers can rely on /health being
// reachable as soon as Start returns nil.
//
// CORS: every response includes Access-Control-Allow-Origin: * because the
// SPA is served from a different origin (e.g. https://stg-app.vaultmtg.app)
// and browser fetch() to localhost requires CORS even though the daemon
// binary is local. The data exposed here is non-sensitive liveness info.
func (s *Server) Start() error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("localapi: listen %s: %w", addr, err)
	}
	s.ln = ln

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)

	s.srv = &http.Server{
		Handler:           withCORS(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[localapi] serve error: %v", err)
		}
	}()

	log.Printf("[localapi] listening on http://%s", ln.Addr().String())
	return nil
}

// Addr returns the bound TCP address (host:port). Useful for tests that pass
// port=0 and need to know the OS-assigned port.
func (s *Server) Addr() string {
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Stop drains in-flight requests and closes the listener. Safe to call before
// Start has been called (no-op) or multiple times.
func (s *Server) Stop() error {
	if s.srv == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return s.srv.Shutdown(ctx)
}

// healthResponse is the JSON body returned by GET /health.
type healthResponse struct {
	Status    string `json:"status"`
	Version   string `json:"version"`
	SessionID string `json:"session_id"`
	StartedAt string `json:"started_at"`
	AccountID string `json:"account_id,omitempty"`
}

// handleHealth returns the daemon's liveness snapshot. The "status" field is
// always "ok" while the server is running — if the server is down the SPA's
// fetch fails outright, which is the actual offline signal.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	resp := healthResponse{
		Status:    "ok",
		Version:   s.state.Version,
		SessionID: s.state.SessionID,
		StartedAt: s.state.StartedAt.UTC().Format(time.RFC3339),
		AccountID: s.state.AccountID,
	}

	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = json.NewEncoder(w).Encode(resp)
}

// withCORS wraps a handler with permissive CORS headers. The daemon serves
// only loopback traffic so the value of Access-Control-Allow-Origin is not a
// security boundary — the firewall (binding 127.0.0.1) is.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// isAllowedOrigin checks if the origin should be echoed back in
// Access-Control-Allow-Origin. We allow the production + staging SPAs and any
// http(s)://localhost:* origin (local dev). Other origins still get "*" which
// is also acceptable for non-credentialed loopback traffic.
func isAllowedOrigin(origin string) bool {
	allow := []string{
		"https://app.vaultmtg.app",
		"https://stg-app.vaultmtg.app",
		"https://vaultmtg.app",
		"https://www.vaultmtg.app",
	}
	for _, o := range allow {
		if origin == o {
			return true
		}
	}
	return strings.HasPrefix(origin, "http://localhost:") ||
		strings.HasPrefix(origin, "https://localhost:") ||
		strings.HasPrefix(origin, "http://127.0.0.1:")
}
