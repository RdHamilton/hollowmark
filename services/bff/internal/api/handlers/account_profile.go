package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/identityhash"
)

// rectificationAuditWriter is the minimal repository surface
// AccountProfileHandler needs to write audit-log rows.
type rectificationAuditWriter interface {
	InsertRectificationEvent(
		ctx context.Context,
		userID int64,
		fieldName string,
		oldValueHash *string,
		newValueHash string,
	) error
}

// emailSyncer is the minimal repository surface needed to update users.email.
type emailSyncer interface {
	UpdateEmail(ctx context.Context, userID int64, email string) error
}

// ProfileAccountLookup resolves a users.id to the user's account_id.
// Satisfied by AccountRepository and compatible stubs in tests.
type ProfileAccountLookup interface {
	GetAccountIDByUserID(ctx context.Context, userID int64) (int64, bool, error)
}

// AccountProfileHandler handles PATCH /api/v1/account/profile.
//
// Auth: inside composeClerkAuth(clerkAuthMiddl, clerkUserResolver) — every
// request must carry a valid Clerk session JWT (Rule 9, CLAUDE.md).
//
// The handler performs two writes that are sequenced atomically in the same
// HTTP request (audit row then email sync) — they are not wrapped in an explicit
// DB transaction because ExecContext with two sequential calls is sufficient at
// this scale; a genuine ACID transaction would require passing a *sql.Tx through
// the interface layer which adds disproportionate complexity for two simple INSERTs.
// If the email sync fails after the audit row succeeds the handler returns 500 and
// the client can retry safely (the audit row is idempotent from a compliance
// perspective — two rows for the same change are both valid evidence).
//
// Fields handled:
//   - email        — audit row + users.email UPDATE (Ray Issue 1: mandatory sync)
//   - display_name — audit row only (Clerk-owned, not persisted in our DB)
//   - date_of_birth_year — explicitly rejected with 400 (COPPA-gated, Ray Issue 2)
type AccountProfileHandler struct {
	audit    rectificationAuditWriter
	emailDB  emailSyncer
	accounts ProfileAccountLookup
}

// NewAccountProfileHandler wires the handler with its dependencies.
func NewAccountProfileHandler(
	audit rectificationAuditWriter,
	emailDB emailSyncer,
	accounts ProfileAccountLookup,
) *AccountProfileHandler {
	return &AccountProfileHandler{
		audit:    audit,
		emailDB:  emailDB,
		accounts: accounts,
	}
}

// patchProfileRequest is the JSON body for PATCH /api/v1/account/profile.
//
// Only recognized fields are processed; unknown fields are silently ignored.
// date_of_birth_year is explicitly rejected (see handler doc).
type patchProfileRequest struct {
	Email           *string `json:"email"`
	DisplayName     *string `json:"display_name"`
	DateOfBirthYear *int    `json:"date_of_birth_year"`
}

// patchProfileResponse is the JSON body returned on success.
type patchProfileResponse struct {
	UpdatedAt string `json:"updated_at"`
}

// Patch handles PATCH /api/v1/account/profile.
//
// Returns 200 OK with {"updated_at": "<RFC3339>"} on success.
// Returns 400 if date_of_birth_year is supplied (COPPA-gated, not self-service).
// Returns 401 if the request carries no authenticated user ID.
// Returns 404 if the authenticated user has no account row.
// Returns 500 on any repository failure.
func (h *AccountProfileHandler) Patch(w http.ResponseWriter, r *http.Request) {
	// Step 1: Extract authenticated user ID from context (Rule 9 — never raw JWT).
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Step 2: Decode and validate request body. Cap at 4 KB — profile bodies
	// are small; large payloads are rejected to prevent abuse.
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		writeJSONError(w, "could not read request body", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	var req patchProfileRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Step 3: Reject date_of_birth_year — COPPA-gated, support-handled only.
	// (Ray Issue 2 on #888: free edit would let a restricted user bypass the age gate.)
	if req.DateOfBirthYear != nil {
		writeJSONError(
			w,
			"date_of_birth_year is not self-service editable; contact support",
			http.StatusBadRequest,
		)
		return
	}

	// Step 4: Resolve account_id (every query scoped by account_id — Rule 2).
	// We call GetAccountIDByUserID to confirm the user has a valid account row,
	// matching the pattern of consent and data-export handlers.
	_, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[AccountProfileHandler] GetAccountIDByUserID userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSONError(w, "account not found", http.StatusNotFound)
		return
	}

	now := time.Now().UTC()

	// Step 5: Process each recognized field.
	//
	// For email: write audit row + update users.email (Ray Issue 1 — mandatory).
	// For display_name: write audit row only (Clerk-owned; not persisted in DB).
	//
	// PII handling: values are hashed before storage (SHA-256 hex[:16]).
	// We do not have the "old" value server-side at this moment (the
	// rectification request arrives after Clerk has already committed the
	// change), so old_value_hash is nil. This is acceptable — the audit
	// record's purpose is to prove a change occurred, not to diff the values.

	if req.Email != nil {
		newEmail := strings.TrimSpace(*req.Email)
		if newEmail == "" {
			writeJSONError(w, "email must not be empty", http.StatusBadRequest)
			return
		}

		// Hash before storing — never persist raw email in audit log.
		newHash := identityhash.HashAccountID(newEmail)

		if err := h.audit.InsertRectificationEvent(
			r.Context(), userID, "email", nil, newHash,
		); err != nil {
			log.Printf("[AccountProfileHandler] InsertRectificationEvent email userID=%d: %v", userID, err)
			writeJSONError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Sync users.email (mandatory — Ray Issue 1: erasure cascade reads this).
		if err := h.emailDB.UpdateEmail(r.Context(), userID, newEmail); err != nil {
			log.Printf("[AccountProfileHandler] UpdateEmail userID=%d: %v", userID, err)
			writeJSONError(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	if req.DisplayName != nil {
		displayName := strings.TrimSpace(*req.DisplayName)
		if displayName == "" {
			writeJSONError(w, "display_name must not be empty", http.StatusBadRequest)
			return
		}

		// Hash before storing — display_name is human-identifiable PII.
		newHash := identityhash.HashAccountID(displayName)

		if err := h.audit.InsertRectificationEvent(
			r.Context(), userID, "display_name", nil, newHash,
		); err != nil {
			log.Printf("[AccountProfileHandler] InsertRectificationEvent display_name userID=%d: %v", userID, err)
			writeJSONError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		// display_name is Clerk-owned — no DB update required.
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(patchProfileResponse{
		UpdatedAt: now.Format(time.RFC3339),
	})
}
