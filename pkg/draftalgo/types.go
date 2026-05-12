// Package draftalgo holds pure-Go draft analysis algorithms: grading,
// pick-quality scoring, and win-rate prediction. Each subpackage is
// decoupled from any storage layer — callers (the daemon's local API
// in PR #17b, or future BFF analytics) inject the data they need
// through small interfaces defined here.
//
// Historical note: these algorithms originally lived under
// internal/mtga/draft/{grading,pickquality,prediction} and were deleted
// by commit 783cf66 during the Phase 1 log-reader extraction. They've
// been restored here without their old internal/storage coupling.
package draftalgo

// Pick is the per-pick input the algorithms consume. The daemon (or
// any other caller) builds a []Pick from its own state — live MTGA
// log events for the current session, or a stored session for a
// completed draft — and passes it to the algorithms.
//
// PickedCardGIHWR and PickQualityGrade are optional and represent
// post-hoc evaluations of the pick. When nil, algorithms fall back to
// default scoring.
type Pick struct {
	// CardID is the Arena card ID as a string. Arena 2026.58+ uses
	// string IDs natively; older numeric IDs are accepted as the
	// stringified form.
	CardID string

	// PackNumber / PickNumber are 1-indexed pack and pick positions.
	PackNumber int
	PickNumber int

	// PickedCardGIHWR is the 17Lands "games in hand win rate" for the
	// card actually picked (0–100). nil when no rating data is
	// available.
	PickedCardGIHWR *float64

	// PickQualityGrade is a letter grade for the pick, computed by
	// pickquality.Analyze (or stored from a prior run). Values match
	// the SPA's expectations: "A+", "A", "A-", "B+", "B", "B-", "C+",
	// "C", "C-", "D", "F". nil when no grade is available.
	PickQualityGrade *string
}

// SessionInfo carries the per-session context the algorithms need.
// Kept minimal — anything not used by the algorithms below is left out.
type SessionInfo struct {
	SessionID string
	SetCode   string
	Format    string // "PremierDraft", "QuickDraft", etc.
}

// CardLookup resolves an Arena card ID to display metadata. The daemon
// will satisfy this via its in-memory set cache (populated from the
// BFF's /api/v1/cards/sets endpoint).
type CardLookup interface {
	// CardName returns the printed name of the card. Empty string when
	// unknown.
	CardName(arenaID string) string
}

// RatingsLookup resolves an Arena card ID to its 17Lands ratings. The
// daemon will satisfy this via a TTL-cached fetch of the BFF's
// /api/v1/draft-ratings/{set}/{format} endpoint.
type RatingsLookup interface {
	// GIHWR returns the 17Lands "games in hand win rate" for the card
	// in the given format. The bool is false when no rating is on file.
	GIHWR(arenaID string, format string) (float64, bool)
}

// LetterGrade converts a 0–100 score to the SPA-compatible letter grade.
// Shared by grading + pickquality so both packages agree on bucket
// boundaries.
func LetterGrade(score int) string {
	switch {
	case score >= 97:
		return "A+"
	case score >= 93:
		return "A"
	case score >= 90:
		return "A-"
	case score >= 87:
		return "B+"
	case score >= 83:
		return "B"
	case score >= 80:
		return "B-"
	case score >= 77:
		return "C+"
	case score >= 73:
		return "C"
	case score >= 70:
		return "C-"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}
