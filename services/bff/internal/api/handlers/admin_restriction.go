package handlers

import (
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// AdminRestrictionHandler handles admin-token-gated GDPR Art.18 endpoints:
//   - POST   /admin/account/{userID}/restrict-processing   → AdminSetRestriction
//   - DELETE /admin/account/{userID}/restrict-processing   → AdminClearRestriction
//
// These routes MUST be mounted inside the AdminTokenAuthMiddl group.
// The {userID} path param is the internal users.id (int64).
type AdminRestrictionHandler struct {
	repo     restrictionWriter
	resolver accountIDResolver
}

// NewAdminRestrictionHandler returns an AdminRestrictionHandler backed by repo and resolver.
func NewAdminRestrictionHandler(repo restrictionWriter, resolver accountIDResolver) *AdminRestrictionHandler {
	return &AdminRestrictionHandler{repo: repo, resolver: resolver}
}

// AdminSetRestriction handles POST /admin/account/{userID}/restrict-processing.
//
// Returns 200 {"status":"restricted"} on success.
// Returns 400 when {userID} is not a valid integer.
// Returns 404 when the user has no account row yet.
// Returns 500 on repository errors.
func (h *AdminRestrictionHandler) AdminSetRestriction(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseUserIDParam(w, r)
	if !ok {
		return
	}

	accountID, found, err := h.resolver.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[AdminRestrictionHandler] GetAccountIDByUserID user_id=%d error=%v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSONError(w, "account not found", http.StatusNotFound)
		return
	}

	if err := h.repo.SetProcessingRestriction(r.Context(), userID); err != nil {
		log.Printf("[AdminRestrictionHandler] SetProcessingRestriction user_id=%d error=%v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := h.repo.InsertAuditLogEntry(r.Context(), userID, accountID, "restricted", "admin"); err != nil {
		log.Printf("[AdminRestrictionHandler] InsertAuditLogEntry user_id=%d error=%v", userID, err)
	}

	writeJSON(w, restrictionResponse{Status: "restricted"}, http.StatusOK)
}

// AdminClearRestriction handles DELETE /admin/account/{userID}/restrict-processing.
//
// Returns 200 {"status":"unrestricted"} on success.
// Returns 400 when {userID} is not a valid integer.
// Returns 404 when the user has no account row yet.
// Returns 500 on repository errors.
func (h *AdminRestrictionHandler) AdminClearRestriction(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseUserIDParam(w, r)
	if !ok {
		return
	}

	accountID, found, err := h.resolver.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[AdminRestrictionHandler] GetAccountIDByUserID user_id=%d error=%v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSONError(w, "account not found", http.StatusNotFound)
		return
	}

	if err := h.repo.ClearProcessingRestriction(r.Context(), userID); err != nil {
		log.Printf("[AdminRestrictionHandler] ClearProcessingRestriction user_id=%d error=%v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := h.repo.InsertAuditLogEntry(r.Context(), userID, accountID, "unrestricted", "admin"); err != nil {
		log.Printf("[AdminRestrictionHandler] InsertAuditLogEntry user_id=%d error=%v", userID, err)
	}

	writeJSON(w, restrictionResponse{Status: "unrestricted"}, http.StatusOK)
}

// parseUserIDParam extracts the {userID} chi path parameter as an int64.
// Writes 400 and returns (0, false) if the param is missing or non-integer.
func parseUserIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "userID")
	if raw == "" {
		writeJSONError(w, "missing userID parameter", http.StatusBadRequest)
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeJSONError(w, "invalid userID parameter", http.StatusBadRequest)
		return 0, false
	}
	return id, true
}
