package handlers

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// consentEventAllowlist is the exhaustive set of valid event_type values for
// the consent_log table. The DB also enforces this via a CHECK constraint;
// the application-layer check returns a clean 400 rather than a 500.
var consentEventAllowlist = map[string]bool{
	"signup":         true,
	"coppa_gate":     true,
	"cookie_accept":  true,
	"cookie_decline": true,
	"install_dialog": true,
}

// consentEventInserter is the minimal repository surface ConsentHandler needs.
// Using an interface instead of the concrete type allows unit tests to inject
// a stub without a real DB connection.
type consentEventInserter interface {
	InsertConsentEvent(ctx context.Context, e repository.ConsentEvent) error
}

// ConsentAccountLookup resolves a Clerk user ID to an internal account ID.
// Satisfied by AccountRepository and compatible stubs in tests.
type ConsentAccountLookup interface {
	GetAccountIDByUserID(ctx context.Context, userID int64) (int64, bool, error)
}

// ConsentConfig holds the server-canonical legal document version strings.
// These are sourced from the BFF config (ultimately from SSM) and applied
// to every signup consent event, regardless of any client-supplied value.
// Ray's Q2 ruling: client-supplied versions are ignored for compliance-evidence
// integrity; the server is the canonical record.
type ConsentConfig struct {
	// TOSVersion is the current Terms of Service version (e.g. "2026-06-10").
	// Sourced from config.Config.TOSVersion → BFF_TOS_VERSION env var → SSM.
	TOSVersion string

	// PrivacyPolicyVersion is the current Privacy Policy version.
	// Sourced from config.Config.PrivacyPolicyVersion → BFF_PRIVACY_POLICY_VERSION.
	PrivacyPolicyVersion string
}

// ConsentHandler serves POST /api/v1/account/consent.
//
// Auth: inside composeClerkAuth(clerkAuthMiddl, clerkUserResolver) — every
// request must carry a valid Clerk session JWT (Rule 9, CLAUDE.md).
//
// The handler accepts all five allowed event types through one generic endpoint.
// For signup events it fills in server-canonical tos_version and
// privacy_policy_version from ConsentConfig regardless of any client-supplied
// value. For all events it hashes the client IP to SHA-256 hex[:16].
//
// Returns 201 Created on success with no body.
// SPA CONTRACT: the SPA must block app entry until the 201 returns. A
// fire-and-forget call is not acceptable — the consent row must be written
// before the user transacts. (Frank/#884 must enforce this sequencing.)
type ConsentHandler struct {
	repo     consentEventInserter
	accounts ConsentAccountLookup
	cfg      ConsentConfig
}

// NewConsentHandler wires the handler with its dependencies.
func NewConsentHandler(repo consentEventInserter, accounts ConsentAccountLookup, cfg ConsentConfig) *ConsentHandler {
	return &ConsentHandler{
		repo:     repo,
		accounts: accounts,
		cfg:      cfg,
	}
}

// consentRequest is the JSON body for POST /api/v1/account/consent.
// Only event_type is required from the client. tos_version and
// privacy_policy_version are accepted but IGNORED for signup events —
// the server canonical values from ConsentConfig are always used.
type consentRequest struct {
	EventType string `json:"event_type"`
	// tos_version and privacy_policy_version are parsed but ignored for signup;
	// they are included in the struct for forward-compatibility with future
	// event types that might need client-supplied fields.
}

// RecordConsent handles POST /api/v1/account/consent.
func (h *ConsentHandler) RecordConsent(w http.ResponseWriter, r *http.Request) {
	// Step 1: Resolve authenticated user ID from context.
	// auth.UserIDFromContext — never raw JWT (Rule 9).
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Step 2: Decode and validate request body. Cap at 4 KB — consent
	// bodies are tiny; rejecting large payloads prevents abuse.
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		writeJSONError(w, "could not read request body", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	var req consentRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Step 3: Validate event_type against allowlist.
	if !consentEventAllowlist[req.EventType] {
		writeJSONError(w, "unknown event_type", http.StatusBadRequest)
		return
	}

	// Step 4: Resolve account_id. Every query scoped by account_id (Rule 2).
	accountID, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[ConsentHandler] GetAccountIDByUserID userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSONError(w, "account not found", http.StatusNotFound)
		return
	}

	// Step 5: Hash client IP. SHA-256 hex[:16] — never store raw IP (PII).
	// Uses the same realIP() helper as the waitlist handler.
	rawIP := realIP(r)
	ipHash := hashAccountID(rawIP) // SHA-256 hex[:16]

	// Step 6: Build the ConsentEvent. For signup events, apply server-canonical
	// version strings from config — ignore any client-supplied values.
	event := repository.ConsentEvent{
		AccountID:     accountID,
		EventType:     req.EventType,
		IPAddressHash: &ipHash,
	}

	if req.EventType == "signup" {
		tosVer := h.cfg.TOSVersion
		ppVer := h.cfg.PrivacyPolicyVersion
		event.TOSVersion = &tosVer
		event.PrivacyPolicyVersion = &ppVer
	}

	// Step 7: INSERT into consent_log (append-only; no update/delete path).
	if err := h.repo.InsertConsentEvent(r.Context(), event); err != nil {
		log.Printf("[ConsentHandler] InsertConsentEvent accountID=%d eventType=%s: %v", accountID, req.EventType, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
