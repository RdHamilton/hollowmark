package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Public types shared with the handler layer
// ---------------------------------------------------------------------------

// ExportManifestEntry describes one table included in the export.
type ExportManifestEntry struct {
	Source   string `json:"source"`
	RowCount int    `json:"row_count"`
}

// ClerkProfile is the subset of Clerk user data included in the Art.15 export.
// Raw email is included per Ray Q2 ruling — it is the subject's primary identifier.
// clerk_user_id is intentionally omitted (internal Clerk identifier, not subject data).
type ClerkProfile struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
	// CreatedAt is the Clerk account creation timestamp (Unix milliseconds -> UTC).
	CreatedAt time.Time `json:"created_at"`
}

// UserExport is the full data export payload returned by DataExportRepository.
// The handler serialises this to JSON and streams it to the client.
type UserExport struct {
	// ExportID is a UUID that uniquely identifies this export run.
	ExportID string `json:"export_id"`

	// ExportedAt is the timestamp at which the gather began (UTC).
	ExportedAt time.Time `json:"exported_at"`

	// AccountIDHash is SHA-256 hex[:16] of the internal account_id.
	// The raw account_id is never included -- it is an internal identifier.
	// (Contrast: clerk_user_id -> also omitted; raw email IS included in ClerkProfile per Ray Q2.)
	AccountIDHash string `json:"account_id_hash"`

	// SchemaVersion is the export schema version for forward compatibility.
	SchemaVersion string `json:"schema_version"`

	// Format is "access" for GET /api/v1/account/data-export (Art.15).
	// "portable" (Art.20) reuses the same payload with additional metadata --
	// that addition is ticket #889's responsibility.
	Format string `json:"format"`

	// ClerkProfile contains the subject's primary identifier data (email, name,
	// account creation timestamp) fetched from Clerk's Backend API (Art.15 Q2).
	// nil when the Clerk API call fails -- the export is still served (non-fatal).
	ClerkProfile *ClerkProfile `json:"clerk_profile,omitempty"`

	// Manifest lists every table included in the export with its row count.
	Manifest []ExportManifestEntry `json:"manifest"`

	// Data holds the gathered rows keyed by table name.
	// Values are []map[string]any -- raw rows from QueryContext.
	Data map[string]any `json:"data"`
}

// ---------------------------------------------------------------------------
// DataExportRepository
// ---------------------------------------------------------------------------

// clerkProfileFetcher is a narrow interface for fetching a user's Clerk profile.
// Satisfied by ClerkProfileFetcher in production and by a stub in tests.
type clerkProfileFetcher interface {
	// FetchClerkProfile returns the Clerk profile for the given Clerk user ID.
	// Returns (nil, nil) when the user is not found in Clerk.
	FetchClerkProfile(ctx context.Context, clerkUserID string) (*ClerkProfile, error)
}

// DataExportRepository gathers all user-keyed personal data for a GDPR Art.15
// access export.  The export scope is the FM-3 knownUserKeyedTables registry
// with dispositions "cascade", "explicit", and "anonymize", minus:
//   - deletion_audit_log ("retain" -- compliance evidence, not subject data)
//   - dsr_access_log ("retain" -- compliance evidence, not subject data)
//   - waitlist_entries (email-keyed, not account-keyed)
//   - the four non-keyed draft aggregate tables (D1: no account_id/user_id col)
//
// TableNames() returns the canonical list so the fitness test
// TestExportCoverage_MirrorsFM3 can assert coverage without a DB connection.
type DataExportRepository struct {
	db           DB
	clerkFetcher clerkProfileFetcher // may be nil; Clerk profile gather is non-fatal
}

// NewDataExportRepository returns a DataExportRepository backed by db.
// db may be nil when only TableNames() is called (used by the fitness test).
// clerkFetcher may be nil; when nil the clerk_profile section is omitted from
// the export (non-fatal -- the Art.15 DB tables are still included).
func NewDataExportRepository(db DB, clerkFetcher clerkProfileFetcher) *DataExportRepository {
	return &DataExportRepository{db: db, clerkFetcher: clerkFetcher}
}

// tableSpec describes one table in the export and how to query it.
type tableSpec struct {
	// name is the table name (used as the manifest source and data key).
	name string

	// query is the SQL used to gather rows for this table.
	// Always uses $1 for the single positional argument (either userID or
	// accountID, depending on which args function is used).
	query string

	// args builds the query arguments from userID and accountID.
	// Returns a single-element slice containing either userID ($1) or
	// accountID ($1), depending on the table's key column.
	args func(userID, accountID int64) []any

	// redact lists column names whose values must be redacted to "<redacted>"
	// before the row is included in the export.  Used for credential columns
	// that are personal-data-adjacent but should not be disclosed
	// (e.g. api_keys.key_hash -- a credential, not the user's data per se).
	redact []string
}

// exportTableSpecs is the ordered list of tables included in the export.
// Order is cosmetic only; the manifest is derived from this slice.
//
// IMPORTANT: when adding a new user-keyed table to the schema, update this
// slice AND knownUserKeyedTables in deletion_repo_test.go simultaneously.
// The TestExportCoverage_MirrorsFM3 fitness test will fail if the two diverge.
var exportTableSpecs = []tableSpec{
	// -- Via users(id) --------------------------------------------------------
	{
		name:  "accounts",
		query: `SELECT * FROM accounts WHERE user_id = $1`,
		args:  byUserID,
	},
	{
		name:   "api_keys",
		query:  `SELECT id, user_id, created_at, last_used_at, revoked FROM api_keys WHERE user_id = $1`,
		args:   byUserID,
		redact: []string{"key_hash"}, // key_hash excluded -- credential, not subject data
	},
	// -- Via accounts(id) -----------------------------------------------------
	{
		name:  "collection",
		query: `SELECT * FROM collection WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "collection_new",
		query: `SELECT * FROM collection_new WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "collection_history",
		query: `SELECT * FROM collection_history WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "matches",
		query: `SELECT * FROM matches WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "match_game_results",
		query: `SELECT * FROM match_game_results WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "player_stats",
		query: `SELECT * FROM player_stats WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "decks",
		query: `SELECT * FROM decks WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "rank_history",
		query: `SELECT * FROM rank_history WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "draft_events",
		query: `SELECT * FROM draft_events WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "draft_sessions",
		query: `SELECT * FROM draft_sessions WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "inventory",
		query: `SELECT * FROM inventory WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "inventory_history",
		query: `SELECT * FROM inventory_history WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "quests",
		query: `SELECT * FROM quests WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "user_settings",
		query: `SELECT * FROM user_settings WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "recommendation_feedback",
		query: `SELECT * FROM recommendation_feedback WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "card_inventory",
		query: `SELECT * FROM card_inventory WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "game_plays",
		query: `SELECT * FROM game_plays WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "draft_picks",
		query: `SELECT * FROM draft_picks WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "draft_packs",
		query: `SELECT * FROM draft_packs WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "draft_match_results",
		query: `SELECT * FROM draft_match_results WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "game_event_counters",
		query: `SELECT * FROM game_event_counters WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "life_change_tracking",
		query: `SELECT * FROM life_change_tracking WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "matchup_statistics",
		query: `SELECT * FROM matchup_statistics WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "deck_performance_history",
		query: `SELECT * FROM deck_performance_history WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "currency_history",
		query: `SELECT * FROM currency_history WHERE account_id = $1`,
		args:  byAccountID,
	},
	{
		name:  "quest_session_tracking",
		query: `SELECT * FROM quest_session_tracking WHERE account_id = $1`,
		args:  byAccountID,
	},
	// -- Via matches(id) ON DELETE CASCADE ------------------------------------
	// Join through matches to scope by account_id -- never expose cross-tenant rows.
	{
		name: "games",
		query: `SELECT g.* FROM games g
				JOIN matches m ON m.id = g.match_id
				WHERE m.account_id = $1`,
		args: byAccountID,
	},
	{
		name: "game_state_snapshots",
		query: `SELECT gs.* FROM game_state_snapshots gs
				JOIN games g ON g.id = gs.game_id
				JOIN matches m ON m.id = g.match_id
				WHERE m.account_id = $1`,
		args: byAccountID,
	},
	{
		name: "opponent_cards_observed",
		query: `SELECT oc.* FROM opponent_cards_observed oc
				JOIN matches m ON m.id = oc.match_id
				WHERE m.account_id = $1`,
		args: byAccountID,
	},
	{
		name: "opponent_deck_profiles",
		query: `SELECT od.* FROM opponent_deck_profiles od
				JOIN matches m ON m.id = od.match_id
				WHERE m.account_id = $1`,
		args: byAccountID,
	},
	// -- Via decks(id) ON DELETE CASCADE --------------------------------------
	{
		name: "deck_cards",
		query: `SELECT dc.* FROM deck_cards dc
				JOIN decks d ON d.id = dc.deck_id
				WHERE d.account_id = $1`,
		args: byAccountID,
	},
	{
		name: "deck_notes",
		query: `SELECT dn.* FROM deck_notes dn
				JOIN decks d ON d.id = dn.deck_id
				WHERE d.account_id = $1`,
		args: byAccountID,
	},
	{
		name: "deck_tags",
		query: `SELECT dt.* FROM deck_tags dt
				JOIN decks d ON d.id = dt.deck_id
				WHERE d.account_id = $1`,
		args: byAccountID,
	},
	{
		name: "ml_suggestions",
		query: `SELECT ml.* FROM ml_suggestions ml
				JOIN decks d ON d.id = ml.deck_id
				WHERE d.account_id = $1`,
		args: byAccountID,
	},
	{
		name: "deck_permutations",
		query: `SELECT dp.* FROM deck_permutations dp
				JOIN decks d ON d.id = dp.deck_id
				WHERE d.account_id = $1`,
		args: byAccountID,
	},
	// -- Explicit TEXT-keyed (client_id / account_id TEXT, no FK) -------------
	// These tables use the MTGA client_id string (TEXT) stored in accounts.client_id.
	// We join through accounts to scope by the authenticated accountID.
	{
		name: "daemon_events",
		query: `SELECT de.* FROM daemon_events de
				JOIN accounts a ON a.client_id = de.account_id
				WHERE a.id = $1`,
		args: byAccountID,
	},
	{
		name: "daemon_api_keys",
		query: `SELECT dak.account_id, dak.key_prefix, dak.device_id, dak.platform,
				       dak.daemon_ver, dak.created_at, dak.last_used_at, dak.revoked
				FROM daemon_api_keys dak
				JOIN accounts a ON a.client_id = dak.account_id
				WHERE a.id = $1`,
		args: byAccountID,
		// key_hash excluded by not selecting it in the query above.
	},
	{
		name: "user_play_patterns",
		query: `SELECT upp.* FROM user_play_patterns upp
				JOIN accounts a ON a.client_id = upp.account_id
				WHERE a.id = $1`,
		args: byAccountID,
	},
	{
		name: "projection_errors",
		query: `SELECT pe.* FROM projection_errors pe
				JOIN accounts a ON a.client_id = pe.account_id
				WHERE a.id = $1`,
		args: byAccountID,
	},
	// -- Anonymize in-place (consent_log) -------------------------------------
	// Include event_type, tos_version, privacy_policy_version, occurred_at.
	// Exclude ip_address_hash (already a hash, low disclosure value -- Ray Q5).
	{
		name: "consent_log",
		query: `SELECT id, account_id, event_type, tos_version, privacy_policy_version,
				       occurred_at
				FROM consent_log
				WHERE account_id = $1`,
		args: byAccountID,
	},
}

// byUserID returns query args with userID as the single $1 positional arg.
func byUserID(userID, _ int64) []any {
	return []any{userID}
}

// byAccountID returns query args with accountID as the single $1 positional arg.
// All account-scoped spec queries use $1 as the WHERE placeholder.
func byAccountID(_, accountID int64) []any {
	return []any{accountID}
}

// TableNames returns the canonical list of table names included in the export.
// This is the only method safe to call with a nil db.
func (r *DataExportRepository) TableNames() []string {
	names := make([]string, len(exportTableSpecs))
	for i, spec := range exportTableSpecs {
		names[i] = spec.name
	}

	return names
}

// GatherForUser collects all user-keyed personal data for the given userID and
// accountID and returns a UserExport ready for JSON serialisation.
//
// IDOR posture: userID and accountID are the authenticated principal's IDs
// resolved from the Clerk JWT by the middleware chain -- never from the request
// body or query string.  Every gather query filters by these values.
func (r *DataExportRepository) GatherForUser(ctx context.Context, userID, accountID int64) (*UserExport, error) {
	export := &UserExport{
		ExportID:      uuid.New().String(),
		ExportedAt:    time.Now().UTC(),
		AccountIDHash: hashForExport(accountID),
		SchemaVersion: "1.0",
		Format:        "access",
		Manifest:      make([]ExportManifestEntry, 0, len(exportTableSpecs)),
		Data:          make(map[string]any, len(exportTableSpecs)),
	}

	// Gather Clerk profile (email, name, created_at) -- Art.15 primary identifier.
	// Non-fatal: if the Clerk API call fails the DB tables are still included.
	if r.clerkFetcher != nil {
		clerkUserID, err := r.clerkUserIDForUser(ctx, userID)
		if err != nil {
			log.Printf("[DataExportRepository] GatherForUser: clerkUserIDForUser userID=%d: %v (non-fatal, clerk_profile omitted)", userID, err)
		} else if clerkUserID != "" {
			profile, err := r.clerkFetcher.FetchClerkProfile(ctx, clerkUserID)
			if err != nil {
				log.Printf("[DataExportRepository] GatherForUser: FetchClerkProfile clerkUserID=%s: %v (non-fatal, clerk_profile omitted)", clerkUserID, err)
			} else {
				export.ClerkProfile = profile
			}
		}
	}

	for _, spec := range exportTableSpecs {
		// All spec queries use $1 as the single positional arg.
		// byUserID returns []any{userID}; byAccountID returns []any{accountID}.
		queryArgs := spec.args(userID, accountID)
		rows, err := r.db.QueryContext(ctx, spec.query, queryArgs...)
		if err != nil {
			// Individual table failures are non-fatal: the table may not exist
			// in older schemas.  Log at WARN so incomplete exports are visible
			// in ops traces rather than silently zeroing the table.
			log.Printf("[DataExportRepository] GatherForUser: QueryContext table=%s userID=%d: %v", spec.name, userID, err)
			export.Manifest = append(export.Manifest, ExportManifestEntry{Source: spec.name, RowCount: 0})
			export.Data[spec.name] = []any{}

			continue
		}

		tableRows, err := scanRows(rows, spec.redact)
		_ = rows.Close()
		if err != nil {
			export.Manifest = append(export.Manifest, ExportManifestEntry{Source: spec.name, RowCount: 0})
			export.Data[spec.name] = []any{}

			continue
		}

		export.Manifest = append(export.Manifest, ExportManifestEntry{Source: spec.name, RowCount: len(tableRows)})
		export.Data[spec.name] = tableRows
	}

	return export, nil
}

// clerkUserIDForUser looks up the clerk_user_id for the given internal userID.
// Returns ("", nil) when the users row exists but has no clerk_user_id set.
func (r *DataExportRepository) clerkUserIDForUser(ctx context.Context, userID int64) (string, error) {
	const q = `SELECT clerk_user_id FROM users WHERE id = $1`

	var clerkUserID *string
	if err := r.db.QueryRowContext(ctx, q, userID).Scan(&clerkUserID); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}

		return "", fmt.Errorf("clerkUserIDForUser: %w", err)
	}

	if clerkUserID == nil {
		return "", nil
	}

	return *clerkUserID, nil
}

// scanRows reads all rows from a *sql.Rows into []map[string]any.
// Columns named in redact are replaced with "<redacted>".
func scanRows(rows *sql.Rows, redact []string) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("scanRows columns: %w", err)
	}

	redactSet := make(map[string]bool, len(redact))
	for _, r := range redact {
		redactSet[r] = true
	}

	var result []map[string]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}

		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scanRows scan: %w", err)
		}

		m := make(map[string]any, len(cols))
		for i, col := range cols {
			if redactSet[col] {
				m[col] = "<redacted>"
				continue
			}
			// Convert []byte to string so json.Marshal produces a readable string
			// rather than a base64-encoded blob for text columns.
			switch v := vals[i].(type) {
			case []byte:
				// Check if it looks like JSON (JSONB columns).
				if len(v) > 0 && (v[0] == '{' || v[0] == '[') {
					var raw json.RawMessage
					if json.Unmarshal(v, &raw) == nil {
						m[col] = raw
						continue
					}
				}
				m[col] = string(v)
			default:
				m[col] = v
			}
		}

		result = append(result, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanRows rows.Err: %w", err)
	}

	if result == nil {
		result = []map[string]any{}
	}

	return result, nil
}

// hashForExport returns SHA-256 hex[:16] of the string form of accountID.
// Mirrors identityhash.HashAccountID -- kept local to avoid an import cycle.
func hashForExport(accountID int64) string {
	s := fmt.Sprintf("%d", accountID)
	sum := sha256.Sum256([]byte(s))

	return fmt.Sprintf("%x", sum)[:16]
}
