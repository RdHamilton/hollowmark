package repository

import (
	"context"
	"fmt"
)

// ConsentEvent holds the fields for one append-only row in the consent_log table.
//
// PII BOUNDARY: this struct must never carry raw email, raw Clerk user ID, or
// raw IP address. IPAddressHash must be a SHA-256 hex[:16] digest.
// Metadata must have PII hashed or removed by the caller before write.
type ConsentEvent struct {
	// AccountID is the internal int64 PK of the accounts table.
	// Must be the authenticated principal's own account_id — never trust a
	// client-supplied value directly.
	AccountID int64

	// EventType must be one of the allowlisted values enforced by the DB CHECK
	// constraint: "signup", "coppa_gate", "cookie_accept", "cookie_decline",
	// "install_dialog". The application layer validates this before calling
	// InsertConsentEvent; the DB provides a belt-and-suspenders CHECK.
	EventType string

	// TOSVersion is the server-canonical ToS version in effect at consent time.
	// Nil for events that do not involve the Terms of Service.
	TOSVersion *string

	// PrivacyPolicyVersion is the server-canonical Privacy Policy version.
	// Nil for events that do not involve the Privacy Policy.
	PrivacyPolicyVersion *string

	// IPAddressHash is SHA-256(rawIP) hex[:16]. Nil for server-initiated events
	// or after erasure anonymization (account_id IS NULL rows).
	// NEVER store raw IP addresses.
	IPAddressHash *string

	// Metadata is optional JSONB for per-event-type structured fields
	// (e.g. {"dob_year_verified": true, "locale": "en-GB"}).
	// Nil for events with no additional metadata.
	Metadata []byte
}

// ConsentLogRepository provides INSERT-only access to the consent_log table.
//
// APPEND-ONLY INVARIANT: this repository exposes no Update or Delete methods.
// The consent_log is compliance evidence under Art.7(1) accountability and
// must not be modified after write. The DB schema's ON DELETE SET NULL FK
// (account_id) allows the #891 erasure cascade to anonymize the PII linkage
// without requiring an explicit Delete on this repository.
type ConsentLogRepository struct {
	db DB
}

// NewConsentLogRepository returns a ConsentLogRepository backed by db.
func NewConsentLogRepository(db DB) *ConsentLogRepository {
	return &ConsentLogRepository{db: db}
}

// InsertConsentEvent appends one consent event row to consent_log.
//
// The INSERT uses $N positional placeholders (pgx convention, Rule 3).
// account_id is scoped to the authenticated principal's account — callers
// MUST NOT pass a client-supplied account_id directly.
//
// This is the ONLY write method on ConsentLogRepository. There are no
// Update or Delete methods — the append-only invariant is enforced here.
func (r *ConsentLogRepository) InsertConsentEvent(ctx context.Context, e ConsentEvent) error {
	const q = `
		INSERT INTO consent_log
			(account_id, event_type, tos_version, privacy_policy_version, ip_address_hash, metadata)
		VALUES
			($1, $2, $3, $4, $5, $6)`

	_, err := r.db.ExecContext(
		ctx, q,
		e.AccountID,
		e.EventType,
		e.TOSVersion,
		e.PrivacyPolicyVersion,
		e.IPAddressHash,
		e.Metadata,
	)
	if err != nil {
		return fmt.Errorf("ConsentLogRepository.InsertConsentEvent: %w", err)
	}
	return nil
}
