package repository_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// seedTestUser inserts a minimal users row and returns its auto-generated id.
// The row is deleted via t.Cleanup so each test is fully self-contained.
// uniqueSuffix must be unique across concurrent test runs; using a per-test
// suffix (e.g. derived from t.Name()) avoids clerk_user_id UNIQUE conflicts
// when tests run in parallel.
func seedTestUser(t *testing.T, db *sql.DB, uniqueSuffix string) int64 {
	t.Helper()

	var userID int64
	if err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO users (email, clerk_user_id) VALUES ($1, $2) RETURNING id`,
		"de-test-"+uniqueSuffix+"@test.local",
		"de-clerk-"+uniqueSuffix,
	).Scan(&userID); err != nil {
		t.Fatalf("seedTestUser(%q): %v", uniqueSuffix, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	return userID
}

func TestDaemonEventsRepository_Insert_NoError(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)

	userID := seedTestUser(t, db, "insert-noerror")

	payload := json.RawMessage(`{"key":"value"}`)
	occurredAt := time.Now().UTC().Truncate(time.Second)

	err := repo.Insert(context.Background(), userID, "test-account-1", "match.game_started", payload, occurredAt, "", 0)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// Cleanup the inserted row.
	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM daemon_events WHERE user_id = $1 AND account_id = 'test-account-1' AND event_type = 'match.game_started'`,
			userID,
		)
	})
}

func TestDaemonEventsRepository_Insert_WithEventID(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)

	userID := seedTestUser(t, db, "insert-eventid")

	payload := json.RawMessage(`{"key":"value"}`)
	occurredAt := time.Now().UTC().Truncate(time.Second)
	eventID := "evt_test_001"

	err := repo.Insert(context.Background(), userID, "test-account-eventid", "match.completed", payload, occurredAt, eventID, 0)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM daemon_events WHERE account_id = 'test-account-eventid'`,
		)
	})

	// Verify idempotency: second insert with same event_id must be a no-op.
	err = repo.Insert(context.Background(), userID, "test-account-eventid", "match.completed", payload, occurredAt, eventID, 0)
	if err != nil {
		t.Fatalf("idempotent Insert: %v", err)
	}

	rows, err := repo.ListByUserID(context.Background(), userID, 100)
	if err != nil {
		t.Fatalf("ListByUserID: %v", err)
	}

	count := 0
	for _, r := range rows {
		if r.AccountID == "test-account-eventid" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 row for idempotent insert, got %d", count)
	}
}

func TestDaemonEventsRepository_ListByUserID_OrderedNewestFirst(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)

	userID := seedTestUser(t, db, "list-ordered")
	const accountID = "test-account-ordered"

	older := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	newer := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Second)

	payload := json.RawMessage(`{"seq":1}`)

	if err := repo.Insert(context.Background(), userID, accountID, "event.a", payload, older, "", 1); err != nil {
		t.Fatalf("Insert older: %v", err)
	}

	payload2 := json.RawMessage(`{"seq":2}`)

	if err := repo.Insert(context.Background(), userID, accountID, "event.b", payload2, newer, "", 2); err != nil {
		t.Fatalf("Insert newer: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM daemon_events WHERE user_id = $1 AND account_id = $2`,
			userID, accountID,
		)
	})

	events, err := repo.ListByUserID(context.Background(), userID, 10)
	if err != nil {
		t.Fatalf("ListByUserID: %v", err)
	}

	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}

	// Newest first — first element should have the newer occurred_at.
	if !events[0].OccurredAt.Equal(newer) {
		t.Errorf("expected first event occurred_at=%v, got %v", newer, events[0].OccurredAt)
	}

	if !events[1].OccurredAt.Equal(older) {
		t.Errorf("expected second event occurred_at=%v, got %v", older, events[1].OccurredAt)
	}
}

func TestDaemonEventsRepository_ListByUserID_ScopedToUser(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)

	userA := seedTestUser(t, db, "scope-usera")
	userB := seedTestUser(t, db, "scope-userb")
	const accountA = "test-account-a"
	const accountB = "test-account-b"

	occurredAt := time.Now().UTC().Truncate(time.Second)
	payload := json.RawMessage(`{}`)

	if err := repo.Insert(context.Background(), userA, accountA, "event.x", payload, occurredAt, "", 0); err != nil {
		t.Fatalf("Insert userA: %v", err)
	}

	if err := repo.Insert(context.Background(), userB, accountB, "event.y", payload, occurredAt, "", 0); err != nil {
		t.Fatalf("Insert userB: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM daemon_events WHERE user_id IN ($1, $2)`,
			userA, userB,
		)
	})

	eventsA, err := repo.ListByUserID(context.Background(), userA, 10)
	if err != nil {
		t.Fatalf("ListByUserID userA: %v", err)
	}

	for _, e := range eventsA {
		if e.UserID != userA {
			t.Errorf("expected only userA events, got user_id=%d", e.UserID)
		}
	}

	eventsB, err := repo.ListByUserID(context.Background(), userB, 10)
	if err != nil {
		t.Fatalf("ListByUserID userB: %v", err)
	}

	for _, e := range eventsB {
		if e.UserID != userB {
			t.Errorf("expected only userB events, got user_id=%d", e.UserID)
		}
	}
}

func TestDaemonEventsRepository_HasRecentEvent_Connected(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)

	userID := seedTestUser(t, db, "health-connected")
	const accountID = "test-account-health-connected"

	payload := json.RawMessage(`{}`)
	occurredAt := time.Now().UTC().Truncate(time.Second)

	if err := repo.Insert(context.Background(), userID, accountID, "heartbeat", payload, occurredAt, "", 0); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM daemon_events WHERE user_id = $1 AND account_id = $2`,
			userID, accountID,
		)
	})

	connected, err := repo.HasRecentEventByUserID(context.Background(), userID, 60*time.Second)
	if err != nil {
		t.Fatalf("HasRecentEventByUserID: %v", err)
	}

	if !connected {
		t.Error("expected connected=true for a row inserted just now")
	}
}

func TestDaemonEventsRepository_HasRecentEvent_Disconnected_NoRows(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)

	// Use a user ID that has no rows in the test database.
	// HasRecentEventByUserID is a read-only query — it does not INSERT into
	// daemon_events, so the FK constraint does not apply here.  The constant
	// 9995 is kept to preserve the intent of the original test (query for a
	// user_id with zero rows and assert connected=false).
	const userID int64 = 9995

	connected, err := repo.HasRecentEventByUserID(context.Background(), userID, 60*time.Second)
	if err != nil {
		t.Fatalf("HasRecentEventByUserID: %v", err)
	}

	if connected {
		t.Error("expected connected=false for a user with no daemon_events rows")
	}
}

func TestDaemonEventsRepository_HasRecentEvent_Disconnected_OldRow(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)

	userID := seedTestUser(t, db, "health-old")
	const accountID = "test-account-health-old"

	payload := json.RawMessage(`{}`)
	// occurred_at is fine being in the past; we need received_at to be old.
	// We insert directly with an explicit old received_at to simulate a stale row.
	occurredAt := time.Now().UTC().Add(-5 * time.Minute).Truncate(time.Second)

	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO daemon_events (user_id, account_id, event_type, payload, occurred_at, received_at)
		 VALUES ($1, $2, $3, $4, $5, NOW() - INTERVAL '5 minutes')`,
		userID, accountID, "heartbeat", payload, occurredAt,
	)
	if err != nil {
		t.Fatalf("direct insert: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM daemon_events WHERE user_id = $1 AND account_id = $2`,
			userID, accountID,
		)
	})

	connected, err := repo.HasRecentEventByUserID(context.Background(), userID, 60*time.Second)
	if err != nil {
		t.Fatalf("HasRecentEventByUserID: %v", err)
	}

	if connected {
		t.Error("expected connected=false for a row older than the window")
	}
}

func TestDaemonEventsRepository_HasRecentEvent_ScopedToUser(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)

	// User A has a recent row; user B must not see it as connected.
	// Both are seeded as real users rows so their IDs satisfy the FK on any
	// future INSERT; userB's check is read-only here but seeding is consistent.
	userA := seedTestUser(t, db, "health-scope-a")
	userB := seedTestUser(t, db, "health-scope-b")
	const accountA = "test-account-health-a"

	payload := json.RawMessage(`{}`)
	occurredAt := time.Now().UTC().Truncate(time.Second)

	if err := repo.Insert(context.Background(), userA, accountA, "heartbeat", payload, occurredAt, "", 0); err != nil {
		t.Fatalf("Insert userA: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM daemon_events WHERE user_id IN ($1, $2)`,
			userA, userB,
		)
	})

	connectedA, err := repo.HasRecentEventByUserID(context.Background(), userA, 60*time.Second)
	if err != nil {
		t.Fatalf("HasRecentEventByUserID userA: %v", err)
	}

	if !connectedA {
		t.Error("expected userA to be connected")
	}

	connectedB, err := repo.HasRecentEventByUserID(context.Background(), userB, 60*time.Second)
	if err != nil {
		t.Fatalf("HasRecentEventByUserID userB: %v", err)
	}

	if connectedB {
		t.Error("expected userB to be disconnected — must not see userA's events")
	}
}

func TestDaemonEventsRepository_Interface(t *testing.T) {
	// Compile-time check: NewDaemonEventsRepository accepts a repository.DB.
	var db repository.DB = &fakeDB{}
	repo := repository.NewDaemonEventsRepository(db)

	if repo == nil {
		t.Fatal("NewDaemonEventsRepository returned nil")
	}
}

// TestDaemonEventsRepository_Insert_SequencePersisted verifies that the sequence
// value is written to the daemon_events.sequence column (ADR-013, ticket #1521).
func TestDaemonEventsRepository_Insert_SequencePersisted(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)

	userID := seedTestUser(t, db, "seq-persisted")
	const accountID = "test-account-seq"
	const wantSequence uint64 = 42

	payload := json.RawMessage(`{"key":"value"}`)
	occurredAt := time.Now().UTC().Truncate(time.Second)

	err := repo.Insert(context.Background(), userID, accountID, "match.completed", payload, occurredAt, "evt_seq_repo_01", wantSequence)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM daemon_events WHERE user_id = $1 AND account_id = $2`,
			userID, accountID,
		)
	})

	// Read back the raw sequence value to confirm it was persisted.
	var gotSequence uint64

	row := db.QueryRowContext(
		context.Background(),
		`SELECT sequence FROM daemon_events WHERE user_id = $1 AND account_id = $2`,
		userID, accountID,
	)
	if err := row.Scan(&gotSequence); err != nil {
		t.Fatalf("Scan sequence: %v", err)
	}

	if gotSequence != wantSequence {
		t.Errorf("sequence=%d, want %d", gotSequence, wantSequence)
	}
}

// ---------------------------------------------------------------------------
// GetLatestHeartbeatAuthStatus integration tests (#144)
// ---------------------------------------------------------------------------

// insertHeartbeat inserts a daemon.heartbeat row for the given userID with
// the provided payload JSON string. Cleans up via t.Cleanup.
func insertHeartbeat(t *testing.T, db *sql.DB, userID int64, payloadJSON string) {
	t.Helper()
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO daemon_events (user_id, account_id, event_type, payload, occurred_at)
		 VALUES ($1, $2, 'daemon.heartbeat', $3::jsonb, NOW())`,
		userID, "test-account-hb", payloadJSON,
	)
	if err != nil {
		t.Fatalf("insertHeartbeat: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM daemon_events WHERE user_id = $1 AND event_type = 'daemon.heartbeat' AND account_id = 'test-account-hb'`,
			userID,
		)
	})
}

// TestGetLatestHeartbeatAuthStatus_Authenticated verifies that a heartbeat row
// with auth_status="authenticated" is returned correctly.
func TestGetLatestHeartbeatAuthStatus_Authenticated(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)
	userID := seedTestUser(t, db, "hb-auth-authenticated")

	insertHeartbeat(t, db, userID, `{"auth_status":"authenticated","parse_failure_count":0}`)

	got, err := repo.GetLatestHeartbeatAuthStatus(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetLatestHeartbeatAuthStatus: %v", err)
	}
	if got != "authenticated" {
		t.Errorf("want authenticated, got %q", got)
	}
}

// TestGetLatestHeartbeatAuthStatus_SetupRequired verifies the setup_required value.
func TestGetLatestHeartbeatAuthStatus_SetupRequired(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)
	userID := seedTestUser(t, db, "hb-auth-setup")

	insertHeartbeat(t, db, userID, `{"auth_status":"setup_required","parse_failure_count":0}`)

	got, err := repo.GetLatestHeartbeatAuthStatus(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetLatestHeartbeatAuthStatus: %v", err)
	}
	if got != "setup_required" {
		t.Errorf("want setup_required, got %q", got)
	}
}

// TestGetLatestHeartbeatAuthStatus_KeychainError verifies the keychain_error value.
func TestGetLatestHeartbeatAuthStatus_KeychainError(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)
	userID := seedTestUser(t, db, "hb-auth-keychain")

	insertHeartbeat(t, db, userID, `{"auth_status":"keychain_error","parse_failure_count":0}`)

	got, err := repo.GetLatestHeartbeatAuthStatus(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetLatestHeartbeatAuthStatus: %v", err)
	}
	if got != "keychain_error" {
		t.Errorf("want keychain_error, got %q", got)
	}
}

// TestGetLatestHeartbeatAuthStatus_AuthPaused verifies the auth_paused value.
func TestGetLatestHeartbeatAuthStatus_AuthPaused(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)
	userID := seedTestUser(t, db, "hb-auth-paused")

	insertHeartbeat(t, db, userID, `{"auth_status":"auth_paused","parse_failure_count":0}`)

	got, err := repo.GetLatestHeartbeatAuthStatus(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetLatestHeartbeatAuthStatus: %v", err)
	}
	if got != "auth_paused" {
		t.Errorf("want auth_paused, got %q", got)
	}
}

// TestGetLatestHeartbeatAuthStatus_NoRows verifies that when there are no
// daemon.heartbeat rows for the user, the result is ("unknown", nil).
func TestGetLatestHeartbeatAuthStatus_NoRows(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)
	// Use a user with no daemon_events rows at all. Read-only query, no FK risk.
	const userID int64 = 9994

	got, err := repo.GetLatestHeartbeatAuthStatus(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected nil error for no-rows, got: %v", err)
	}
	if got != "unknown" {
		t.Errorf("want unknown on no-rows, got %q", got)
	}
}

// TestGetLatestHeartbeatAuthStatus_OldDaemon_FieldAbsent verifies that when
// the most recent heartbeat payload lacks the auth_status field (old daemon,
// pre-#144), the result is ("unknown", nil).
func TestGetLatestHeartbeatAuthStatus_OldDaemon_FieldAbsent(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)
	userID := seedTestUser(t, db, "hb-auth-oldfield")

	// Old daemon payload — no auth_status field.
	insertHeartbeat(t, db, userID, `{"parse_failure_count":0,"consecutive_bff_failures":0}`)

	got, err := repo.GetLatestHeartbeatAuthStatus(context.Background(), userID)
	if err != nil {
		t.Fatalf("expected nil error for missing field, got: %v", err)
	}
	if got != "unknown" {
		t.Errorf("want unknown when auth_status field absent, got %q", got)
	}
}

// TestGetLatestHeartbeatAuthStatus_MultipleRows_ReturnsNewest verifies that
// when multiple heartbeat rows exist, the newest received_at row is used.
func TestGetLatestHeartbeatAuthStatus_MultipleRows_ReturnsNewest(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)
	userID := seedTestUser(t, db, "hb-auth-multi")

	// Insert old row (setup_required) then newer row (authenticated).
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO daemon_events (user_id, account_id, event_type, payload, occurred_at, received_at)
		 VALUES ($1, 'test-account-multi', 'daemon.heartbeat', '{"auth_status":"setup_required"}'::jsonb,
		         NOW() - INTERVAL '2 minutes', NOW() - INTERVAL '2 minutes')`,
		userID,
	)
	if err != nil {
		t.Fatalf("insert old row: %v", err)
	}
	_, err = db.ExecContext(
		context.Background(),
		`INSERT INTO daemon_events (user_id, account_id, event_type, payload, occurred_at, received_at)
		 VALUES ($1, 'test-account-multi', 'daemon.heartbeat', '{"auth_status":"authenticated"}'::jsonb,
		         NOW() - INTERVAL '1 minute', NOW() - INTERVAL '1 minute')`,
		userID,
	)
	if err != nil {
		t.Fatalf("insert newer row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM daemon_events WHERE user_id = $1 AND account_id = 'test-account-multi'`,
			userID,
		)
	})

	got, err := repo.GetLatestHeartbeatAuthStatus(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetLatestHeartbeatAuthStatus: %v", err)
	}
	if got != "authenticated" {
		t.Errorf("want authenticated (newest row), got %q", got)
	}
}

// TestGetLatestHeartbeatAuthStatus_CrossTenantIsolation verifies that user B's
// heartbeat rows are not visible to user A's query.
func TestGetLatestHeartbeatAuthStatus_CrossTenantIsolation(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDaemonEventsRepository(db)
	userA := seedTestUser(t, db, "hb-auth-tenant-a")
	userB := seedTestUser(t, db, "hb-auth-tenant-b")

	// Only user B has a heartbeat with an auth_status.
	insertHeartbeat(t, db, userB, `{"auth_status":"authenticated"}`)

	// User A must get "unknown" (no rows for A).
	got, err := repo.GetLatestHeartbeatAuthStatus(context.Background(), userA)
	if err != nil {
		t.Fatalf("GetLatestHeartbeatAuthStatus userA: %v", err)
	}
	if got != "unknown" {
		t.Errorf("cross-tenant leak: userA must get unknown, got %q", got)
	}

	// User B must get "authenticated" from their own row.
	got, err = repo.GetLatestHeartbeatAuthStatus(context.Background(), userB)
	if err != nil {
		t.Fatalf("GetLatestHeartbeatAuthStatus userB: %v", err)
	}
	if got != "authenticated" {
		t.Errorf("userB: want authenticated, got %q", got)
	}
}
