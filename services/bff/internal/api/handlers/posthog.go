// Package handlers provides HTTP request handlers for the BFF service.
package handlers

import (
	"github.com/RdHamilton/hollowmark/services/bff/internal/identityhash"
)

// hashAccountID returns a privacy-safe representation of accountID for
// PostHog: SHA-256 hex, first 16 characters.  No raw PII is ever sent.
// The input must already be the string form of the account id (e.g.
// strconv.FormatInt(accountID, 10)).
//
// This is a thin shim so all callers within the handlers package continue to
// use the same call-site syntax.  The canonical one-implementation rule (FM-2)
// is enforced by TestNoHashDuplicates_GrepGuard in internal/identityhash.
func hashAccountID(accountID string) string {
	return identityhash.HashAccountID(accountID)
}
