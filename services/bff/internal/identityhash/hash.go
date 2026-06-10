// Package identityhash provides the canonical account-ID hashing function
// used throughout the BFF for privacy-safe PostHog distinct_ids and log
// annotations.
//
// Rule (FM-2 / I-10): there is exactly ONE implementation of this function in
// the codebase.  All callers import this package; a CI grep-guard in
// hash_test.go enforces that no duplicate sha256-based hashAccountID function
// exists elsewhere in services/bff.
package identityhash

import (
	"crypto/sha256"
	"fmt"
)

// HashAccountID returns a privacy-safe representation of accountID:
// SHA-256 hex, first 16 characters.  No raw PII is ever sent to PostHog or
// written to logs.
//
// The input must be the string form of the internal account id (e.g.
// strconv.FormatInt(accountID, 10)) — not the raw Clerk user id.
//
// The output is intentionally short (64 bits of collision-resistance is
// sufficient for an analytics distinct_id) and stable — the same input always
// produces the same output, which is required for PostHog person deduplication
// across sessions.
func HashAccountID(accountID string) string {
	sum := sha256.Sum256([]byte(accountID))
	return fmt.Sprintf("%x", sum)[:16]
}
