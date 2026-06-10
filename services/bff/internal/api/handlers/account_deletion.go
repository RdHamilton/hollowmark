package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
)

// userAndAccountResolver resolves a Clerk user ID to internal database IDs.
type userAndAccountResolver interface {
	ResolveUserAndAccount(ctx context.Context, clerkUserID string) (userID, accountID int64, err error)
}

// erasureJobStarter creates the audit log entry and dispatches the background
// erasure goroutine.  Returns the job_id for the 202 response body.
type erasureJobStarter interface {
	StartErasureJob(ctx context.Context, userID, accountID int64) (jobID string, err error)
}

// AccountDeletionHandler handles DELETE /api/v1/account (Art.17 erasure entry
// point).
//
// The route MUST be mounted inside the ClerkAuthMiddleware group.  The handler
// returns 202 Accepted immediately; the erasure cascade runs asynchronously in
// a background goroutine dispatched from the BFF root context.
//
// This handler is the #887 entry point that invokes the #891 erasure cascade.
// Both must ship together per BROADCAST rule 5 (Coupled-With #887).
type AccountDeletionHandler struct {
	resolver userAndAccountResolver
	starter  erasureJobStarter
}

// NewAccountDeletionHandler returns an AccountDeletionHandler.
func NewAccountDeletionHandler(resolver userAndAccountResolver, starter erasureJobStarter) *AccountDeletionHandler {
	return &AccountDeletionHandler{
		resolver: resolver,
		starter:  starter,
	}
}

// accountDeletionResponse is the JSON body returned by DELETE /api/v1/account.
type accountDeletionResponse struct {
	JobID   string `json:"job_id"`
	Message string `json:"message"`
}

// Delete handles DELETE /api/v1/account.
//
// Returns 202 Accepted with a job_id that the client can poll via
// GET /api/v1/account/deletion-status/{job_id}.
func (h *AccountDeletionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	clerkUserID, ok := bffmiddleware.ClerkUserIDFromContext(r)
	if !ok || clerkUserID == "" {
		log.Printf("[account_deletion] missing Clerk user ID — ClerkAuthMiddleware not applied")
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	userID, accountID, err := h.resolver.ResolveUserAndAccount(r.Context(), clerkUserID)
	if err != nil {
		log.Printf("[account_deletion] ResolveUserAndAccount clerk_user_id=%s error=%v", clerkUserID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	jobID, err := h.starter.StartErasureJob(r.Context(), userID, accountID)
	if err != nil {
		log.Printf("[account_deletion] StartErasureJob user_id=%d account_id=%d error=%v", userID, accountID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(accountDeletionResponse{
		JobID:   jobID,
		Message: "Your account deletion request has been accepted and is being processed.",
	})
}
