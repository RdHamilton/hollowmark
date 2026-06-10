package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
)

// restrictionWriter is the minimal repository surface needed by RestrictionHandler.
type restrictionWriter interface {
	SetProcessingRestriction(ctx context.Context, userID int64) error
	ClearProcessingRestriction(ctx context.Context, userID int64) error
	InsertAuditLogEntry(ctx context.Context, userID, accountID int64, action, actor string) error
}

// accountIDResolver resolves a users.id to an accounts.id.
// Satisfied by *repository.AccountRepository.
type accountIDResolver interface {
	GetAccountIDByUserID(ctx context.Context, userID int64) (int64, bool, error)
}

// RestrictionHandler handles GDPR Art.18 Right to Restriction endpoints for
// the authenticated user:
//   - POST   /api/v1/account/restrict-processing  → SetRestriction
//   - DELETE /api/v1/account/restrict-processing  → ClearRestriction
//
// Both routes MUST be mounted inside the ClerkAuthMiddleware group.
type RestrictionHandler struct {
	repo     restrictionWriter
	resolver accountIDResolver
}

// NewRestrictionHandler returns a RestrictionHandler backed by repo and resolver.
func NewRestrictionHandler(repo restrictionWriter, resolver accountIDResolver) *RestrictionHandler {
	return &RestrictionHandler{repo: repo, resolver: resolver}
}

// restrictionResponse is the JSON body returned by both set and clear endpoints.
type restrictionResponse struct {
	Status string `json:"status"` // "restricted" or "unrestricted"
}

// SetRestriction handles POST /api/v1/account/restrict-processing.
//
// Returns 200 {"status":"restricted"} on success.
// Returns 401 when the Clerk user ID is not in context.
// Returns 404 when the user has no account row yet.
// Returns 500 on repository errors.
func (h *RestrictionHandler) SetRestriction(w http.ResponseWriter, r *http.Request) {
	userID, clerkUserID, ok := h.resolveIdentity(w, r)
	if !ok {
		return
	}

	accountID, found, err := h.resolver.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[RestrictionHandler] GetAccountIDByUserID clerk_user_id=%s error=%v", clerkUserID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSONError(w, "account not found", http.StatusNotFound)
		return
	}

	if err := h.repo.SetProcessingRestriction(r.Context(), userID); err != nil {
		log.Printf("[RestrictionHandler] SetProcessingRestriction user_id=%d error=%v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := h.repo.InsertAuditLogEntry(r.Context(), userID, accountID, "restricted", "user"); err != nil {
		log.Printf("[RestrictionHandler] InsertAuditLogEntry user_id=%d error=%v", userID, err)
		// Audit log failure is logged but does not fail the request — the
		// restriction flag is already set. A missed audit row is recoverable
		// from logs; failing the user's restriction request is not acceptable.
	}

	writeJSON(w, restrictionResponse{Status: "restricted"}, http.StatusOK)
}

// ClearRestriction handles DELETE /api/v1/account/restrict-processing.
//
// Returns 200 {"status":"unrestricted"} on success.
// Returns 401 when the Clerk user ID is not in context.
// Returns 404 when the user has no account row yet.
// Returns 500 on repository errors.
func (h *RestrictionHandler) ClearRestriction(w http.ResponseWriter, r *http.Request) {
	userID, clerkUserID, ok := h.resolveIdentity(w, r)
	if !ok {
		return
	}

	accountID, found, err := h.resolver.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[RestrictionHandler] GetAccountIDByUserID clerk_user_id=%s error=%v", clerkUserID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSONError(w, "account not found", http.StatusNotFound)
		return
	}

	if err := h.repo.ClearProcessingRestriction(r.Context(), userID); err != nil {
		log.Printf("[RestrictionHandler] ClearProcessingRestriction user_id=%d error=%v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := h.repo.InsertAuditLogEntry(r.Context(), userID, accountID, "unrestricted", "user"); err != nil {
		log.Printf("[RestrictionHandler] InsertAuditLogEntry user_id=%d error=%v", userID, err)
	}

	writeJSON(w, restrictionResponse{Status: "unrestricted"}, http.StatusOK)
}

// resolveIdentity extracts the Clerk user ID and internal user ID from context.
// Returns (0, "", false) and writes a 401 if auth is missing.
func (h *RestrictionHandler) resolveIdentity(w http.ResponseWriter, r *http.Request) (userID int64, clerkUserID string, ok bool) {
	var hasClerk bool
	clerkUserID, hasClerk = bffmiddleware.ClerkUserIDFromContext(r)
	if !hasClerk || clerkUserID == "" {
		log.Printf("[RestrictionHandler] missing Clerk user ID — ClerkAuthMiddleware not applied")
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return 0, "", false
	}

	userID, ok = bffmiddleware.UserIDFromContext(r.Context())
	if !ok || userID == 0 {
		log.Printf("[RestrictionHandler] missing userID from context clerk_user_id=%s", clerkUserID)
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return 0, "", false
	}

	return userID, clerkUserID, true
}

// writeJSON encodes v as JSON and writes status to the response.
func writeJSON(w http.ResponseWriter, v any, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[handlers] writeJSON encode: %v", err)
	}
}
