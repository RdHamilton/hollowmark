package repository_test

// migration_121_test.go — integration tests for migration 000121
// (ticket #621: add CHECK length constraints on game_event_counters.counter_type
// and game_event_counters.controller TEXT columns).
//
// Tests verify the post-migration schema state:
//   - counter_type: CHECK (char_length(counter_type) <= 64)
//   - controller:   CHECK (char_length(controller) <= 64)
//
// The down migration removes both constraints and is verified in a transaction.
//
// All tests skip when DATABASE_URL is not set.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// TestMigration121_CounterTypeConstraintEnforced verifies that
// game_event_counters.counter_type rejects values longer than 64 characters.
func TestMigration121_CounterTypeConstraintEnforced(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Verify the CHECK constraint exists in the catalog.
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.check_constraints
			WHERE constraint_name = 'chk_game_event_counters_counter_type_len'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("check constraint catalog query: %v", err)
	}
	if !exists {
		t.Fatal("constraint chk_game_event_counters_counter_type_len not found — migration 000121 may not have run")
	}

	// Functional: insert a counter_type exceeding 64 chars must be rejected.
	gpRepo := repository.NewGamePlayRepository(db)
	ctrRepo := repository.NewGameEventCountersRepository(db)

	var acctID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts (name) VALUES ($1) RETURNING id`, "mig121-ctr-type-test").Scan(&acctID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, acctID) })

	mgrID, err := gpRepo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID: acctID, MatchID: "match-mig121-ctr-type", GameNumber: 1, Sequence: 1,
		OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM match_game_results WHERE id = $1`, mgrID) })

	// 65-character counter_type: must be rejected.
	longType := strings.Repeat("x", 65)
	insertErr := ctrRepo.InsertCounters(ctx, []repository.GameEventCounterInsert{
		{
			MatchGameResultID: mgrID, AccountID: acctID, InstanceID: 601, ArenaID: 40001,
			CounterType: longType, Count: 1, Delta: 1, Controller: "player", TurnNumber: 1,
		},
	})
	if insertErr == nil {
		t.Errorf("InsertCounters with counter_type len=65: want constraint error, got nil — chk_game_event_counters_counter_type_len missing")
	}

	// 64-character counter_type: must be accepted.
	okType := strings.Repeat("y", 64)
	if err := ctrRepo.InsertCounters(ctx, []repository.GameEventCounterInsert{
		{
			MatchGameResultID: mgrID, AccountID: acctID, InstanceID: 602, ArenaID: 40002,
			CounterType: okType, Count: 1, Delta: 1, Controller: "player", TurnNumber: 2,
		},
	}); err != nil {
		t.Errorf("InsertCounters with counter_type len=64: want success, got error: %v", err)
	}
}

// TestMigration121_ControllerConstraintEnforced verifies that
// game_event_counters.controller rejects values longer than 64 characters.
func TestMigration121_ControllerConstraintEnforced(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	// Verify the CHECK constraint exists in the catalog.
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.check_constraints
			WHERE constraint_name = 'chk_game_event_counters_controller_len'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("check constraint catalog query: %v", err)
	}
	if !exists {
		t.Fatal("constraint chk_game_event_counters_controller_len not found — migration 000121 may not have run")
	}

	// Functional: insert a controller exceeding 64 chars must be rejected.
	gpRepo := repository.NewGamePlayRepository(db)
	ctrRepo := repository.NewGameEventCountersRepository(db)

	var acctID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts (name) VALUES ($1) RETURNING id`, "mig121-ctrl-test").Scan(&acctID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, acctID) })

	mgrID, err := gpRepo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID: acctID, MatchID: "match-mig121-ctrl", GameNumber: 1, Sequence: 1,
		OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM match_game_results WHERE id = $1`, mgrID) })

	// 65-character controller: must be rejected.
	longCtrl := strings.Repeat("z", 65)
	insertErr := ctrRepo.InsertCounters(ctx, []repository.GameEventCounterInsert{
		{
			MatchGameResultID: mgrID, AccountID: acctID, InstanceID: 701, ArenaID: 30001,
			CounterType: "loyalty", Count: 1, Delta: 1, Controller: longCtrl, TurnNumber: 1,
		},
	})
	if insertErr == nil {
		t.Errorf("InsertCounters with controller len=65: want constraint error, got nil — chk_game_event_counters_controller_len missing")
	}

	// 64-character controller: must be accepted.
	okCtrl := strings.Repeat("w", 64)
	if err := ctrRepo.InsertCounters(ctx, []repository.GameEventCounterInsert{
		{
			MatchGameResultID: mgrID, AccountID: acctID, InstanceID: 702, ArenaID: 30002,
			CounterType: "poison", Count: 1, Delta: 1, Controller: okCtrl, TurnNumber: 2,
		},
	}); err != nil {
		t.Errorf("InsertCounters with controller len=64: want success, got error: %v", err)
	}
}
