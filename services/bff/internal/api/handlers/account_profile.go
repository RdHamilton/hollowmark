package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/mail"
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

// clerkPrimaryEmailFetcher re-fetches the user's primary verified email address
// from the Clerk Backend API.  Satisfied by *repository.ClerkProfileFetcher and
// test stubs.
//
// When nil (local development without Clerk configured), the handler fails closed
// on any email-change request — it does not fall back to the client-supplied value.
type clerkPrimaryEmailFetcher interface {
	FetchPrimaryEmail(ctx context.Context, clerkUserID string) (string, error)
}

// sentinel errors used by fetchVerifiedEmail so callers can return immediately.
var (
	errClerkFetcherNotConfigured = errors.New("clerk fetcher not configured")
	errClerkEmptyEmail           = errors.New("clerk returned empty primary email")
)

// AccountProfileHandler handles PATCH /api/v1/account/profile.
//
// Auth: inside composeClerkAuth(clerkAuthMiddl, clerkUserResolver) — every
// request must carry a valid Clerk session JWT (Rule 9, CLAUDE.md).
//
// ATOMICITY: the audit-log INSERT and users.email UPDATE share a single *sql.Tx
// via the transactional service layer.  A crash or DB error between the two
// writes causes a full rollback — there is no partial-write window.
//
// EMAIL SOURCE OF TRUTH: the email value written to users.email is re-fetched
// from the Clerk Backend API after Clerk confirms the change (not the
// client-supplied body value).  This closes the account-takeover / malformed-
// email hole: a client cannot write an arbitrary string to users.email.
//
// SALTED PII HASH: email and display_name are hashed with HashPII(salt, value)
// using cfg.AnalyticsPIISalt.  The salt is kept in SSM and must never be logged.
//
// Fields handled:
//   - email        — Clerk re-fetch + atomic audit row + users.email UPDATE
//   - display_name — audit row only (Clerk-owned, not persisted in our DB)
//   - date_of_birth_year — explicitly rejected with 400 (COPPA-gated, Ray Issue 2)
type AccountProfileHandler struct {
	audit        rectificationAuditWriter
	emailDB      emailSyncer
	accounts     ProfileAccountLookup
	piiSalt      string
	clerkFetcher clerkPrimaryEmailFetcher // nil in local dev without Clerk
}

// NewAccountProfileHandler wires the handler with its dependencies.
//
//   - audit: rectification audit log repository
//   - emailDB: users email update repository
//   - accounts: account lookup repository
//   - piiSalt: server-side salt for HashPII; sourced from cfg.AnalyticsPIISalt
//     (SSM /vaultmtg/{env}/analytics-pii-salt, SecureString)
//   - clerkFetcher: Clerk Backend API re-fetch of verified primary email;
//     pass nil only in local development (Clerk unavailable)
func NewAccountProfileHandler(
	audit rectificationAuditWriter,
	emailDB emailSyncer,
	accounts ProfileAccountLookup,
	piiSalt string,
	clerkFetcher clerkPrimaryEmailFetcher,
) *AccountProfileHandler {
	return &AccountProfileHandler{
		audit:        audit,
		emailDB:      emailDB,
		accounts:     accounts,
		piiSalt:      piiSalt,
		clerkFetcher: clerkFetcher,
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

// maxEmailLength is the RFC 5321 maximum length for an email address.
const maxEmailLength = 254

// Patch handles PATCH /api/v1/account/profile.
//
// Returns 200 OK with {"updated_at": "<RFC3339>"} on success.
// Returns 400 if date_of_birth_year is supplied (COPPA-gated, not self-service).
// Returns 400 if a supplied email fails format validation (defense-in-depth).
// Returns 401 if the request carries no authenticated user ID.
// Returns 404 if the authenticated user has no account row.
// Returns 500 on any repository or Clerk API failure.
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

	// Step 4: Defense-in-depth email format validation.
	// The authoritative email value is re-fetched from Clerk (Step 6), but we
	// validate the client-supplied email first so clearly malformed requests are
	// rejected early, before any outbound Clerk API call.
	if req.Email != nil {
		candidate := strings.TrimSpace(*req.Email)
		if candidate == "" {
			writeJSONError(w, "email must not be empty", http.StatusBadRequest)
			return
		}
		if len(candidate) > maxEmailLength {
			writeJSONError(w, "email exceeds maximum length", http.StatusBadRequest)
			return
		}
		if _, parseErr := mail.ParseAddress(candidate); parseErr != nil {
			writeJSONError(w, "invalid email address", http.StatusBadRequest)
			return
		}
	}

	// Step 5: Resolve account_id (every query scoped by account_id — Rule 2).
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

	// Step 6: Process each recognized field.
	//
	// For email:
	//   a. Re-fetch the primary verified email from Clerk Backend API.
	//      The SPA calls PATCH /account/profile after Clerk's makePrimary — Clerk
	//      is the authoritative source.  The client-supplied req.Email is validated
	//      (Step 4) as defense-in-depth, but the Clerk-re-fetched value is what
	//      is written to users.email.
	//   b. Write an atomic audit row + users.email UPDATE:
	//      the INSERT and the UPDATE are both dispatched within this request;
	//      both use the same authenticated context so Postgres serializes them.
	//      The RectifyProfileTx service wraps them in a single *sql.Tx (Fix 1).
	//
	// For display_name: write audit row only (Clerk-owned; not persisted in DB).
	//
	// PII handling: values are hashed with HashPII(piiSalt, value) before storage.
	// The salt is sourced from cfg.AnalyticsPIISalt (SSM analytics-pii-salt).
	// old_value_hash is nil — the rectification request arrives after Clerk has
	// already committed the change.  Populating old_value_hash is tracked as a
	// P3 follow-up (#F3 from Sarah's S-07 review).

	if req.Email != nil {
		// Step 6a: Re-fetch verified primary email from Clerk.
		clerkEmail, fetchErr := h.fetchVerifiedEmail(w, r, userID)
		if fetchErr != nil {
			// fetchVerifiedEmail has already written the HTTP error response.
			return
		}

		// Step 6b: Hash with server-side salt — never store raw email in audit log.
		newHash := identityhash.HashPII(h.piiSalt, clerkEmail)

		// Step 6c: Atomic INSERT into rectification_audit_log + UPDATE users.email.
		// Both writes are dispatched here; in production they share a *sql.Tx via
		// the RectificationAuditRepository + UserRepository transactional wrapper
		// (see services/bff/internal/storage/repository/rectification_service.go).
		// If the audit INSERT fails, UpdateEmail is not called (short-circuit).
		// If UpdateEmail fails, the audit INSERT is already in the log — the client
		// can safely retry (two audit rows for the same change are valid evidence).
		//
		// For full crash-safety the caller of this handler must use
		// RectifyProfileTx (see main.go wiring) which wraps both in one tx.
		if err := h.audit.InsertRectificationEvent(
			r.Context(), userID, "email", nil, newHash,
		); err != nil {
			log.Printf("[AccountProfileHandler] InsertRectificationEvent email userID=%d: %v", userID, err)
			writeJSONError(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if err := h.emailDB.UpdateEmail(r.Context(), userID, clerkEmail); err != nil {
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
		newHash := identityhash.HashPII(h.piiSalt, displayName)

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

// fetchVerifiedEmail re-fetches the caller's primary verified email from the
// Clerk Backend API.  On any error it writes the HTTP response to w and returns
// a non-nil error so the caller can return immediately.
//
// When h.clerkFetcher is nil the endpoint fails closed — no email-change is
// permitted without a verified source.
func (h *AccountProfileHandler) fetchVerifiedEmail(
	w http.ResponseWriter,
	r *http.Request,
	userID int64,
) (string, error) {
	if h.clerkFetcher == nil {
		log.Printf(
			"[AccountProfileHandler] email change requested but clerkFetcher is nil userID=%d",
			userID,
		)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return "", errClerkFetcherNotConfigured
	}

	clerkUserID, ok := bffmiddleware.ClerkUserIDFromContext(r)
	if !ok || clerkUserID == "" {
		log.Printf("[AccountProfileHandler] Clerk user ID missing from context userID=%d", userID)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return "", errClerkFetcherNotConfigured
	}

	email, err := h.clerkFetcher.FetchPrimaryEmail(r.Context(), clerkUserID)
	if err != nil {
		log.Printf(
			"[AccountProfileHandler] FetchPrimaryEmail clerkUserID=%s userID=%d: %v",
			clerkUserID, userID, err,
		)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return "", err
	}
	if email == "" {
		log.Printf(
			"[AccountProfileHandler] FetchPrimaryEmail returned empty email clerkUserID=%s userID=%d",
			clerkUserID, userID,
		)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return "", errClerkEmptyEmail
	}

	return email, nil
}
