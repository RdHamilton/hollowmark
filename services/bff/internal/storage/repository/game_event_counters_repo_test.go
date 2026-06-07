package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

func TestGameEventCountersRepository_InsertAndCount(t *testing.T) {
	db := openTestDB(t)
	gpRepo := repository.NewGamePlayRepository(db)
	ctrRepo := repository.NewGameEventCountersRepository(db)
	ctx := context.Background()

	var accountID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts (name) VALUES ($1) RETURNING id`, "ctr-account-insert").Scan(&accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, accountID) })

	matchGameResultID, err := gpRepo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID:  accountID,
		MatchID:    "match-ctr-001",
		GameNumber: 1,
		Sequence:   1,
		OccurredAt: time.Now().UTC().Truncate(time.Microsecond),
	})
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM match_game_results WHERE id = $1`, matchGameResultID) })

	inserts := []repository.GameEventCounterInsert{
		{MatchGameResultID: matchGameResultID, AccountID: accountID, InstanceID: 101, ArenaID: 80001, CounterType: "loyalty", Count: 4, Delta: -1, Controller: "player", TurnNumber: 3},
		{MatchGameResultID: matchGameResultID, AccountID: accountID, InstanceID: 102, ArenaID: 80002, CounterType: "+1/+1", Count: 2, Delta: 1, Controller: "opponent", TurnNumber: 5},
	}

	if err := ctrRepo.InsertCounters(ctx, inserts); err != nil {
		t.Fatalf("InsertCounters: %v", err)
	}

	n, err := ctrRepo.CountByMatchGameResult(ctx, matchGameResultID)
	if err != nil {
		t.Fatalf("CountByMatchGameResult: %v", err)
	}
	if n != 2 {
		t.Errorf("count: want 2, got %d", n)
	}
}

func TestGameEventCountersRepository_OnConflictDoNothing(t *testing.T) {
	db := openTestDB(t)
	gpRepo := repository.NewGamePlayRepository(db)
	ctrRepo := repository.NewGameEventCountersRepository(db)
	ctx := context.Background()

	var accountID int64
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts (name) VALUES ($1) RETURNING id`, "ctr-account-idempotent").Scan(&accountID); err != nil {
		t.Fatalf("insert account: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, accountID) })

	matchGameResultID, err := gpRepo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID:  accountID,
		MatchID:    "match-ctr-idem",
		GameNumber: 1,
		Sequence:   1,
		OccurredAt: time.Now().UTC().Truncate(time.Microsecond),
	})
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM match_game_results WHERE id = $1`, matchGameResultID) })

	ins := repository.GameEventCounterInsert{
		MatchGameResultID: matchGameResultID, AccountID: accountID,
		InstanceID: 201, ArenaID: 90001, CounterType: "poison",
		Count: 3, Delta: 1, Controller: "opponent", TurnNumber: 7,
	}

	// First insert.
	if err := ctrRepo.InsertCounters(ctx, []repository.GameEventCounterInsert{ins}); err != nil {
		t.Fatalf("InsertCounters first: %v", err)
	}

	// Replay the same row — ON CONFLICT DO NOTHING must not error.
	if err := ctrRepo.InsertCounters(ctx, []repository.GameEventCounterInsert{ins}); err != nil {
		t.Fatalf("InsertCounters replay: %v", err)
	}

	// Still exactly one row.
	n, err := ctrRepo.CountByMatchGameResult(ctx, matchGameResultID)
	if err != nil {
		t.Fatalf("CountByMatchGameResult: %v", err)
	}
	if n != 1 {
		t.Errorf("count after replay: want 1, got %d", n)
	}
}

func TestGameEventCountersRepository_AccountIsolation(t *testing.T) {
	db := openTestDB(t)
	gpRepo := repository.NewGamePlayRepository(db)
	ctrRepo := repository.NewGameEventCountersRepository(db)
	ctx := context.Background()

	var acctA, acctB int64
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts (name) VALUES ($1) RETURNING id`, "ctr-acct-iso-a").Scan(&acctA); err != nil {
		t.Fatalf("insert account A: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, acctA) })
	if err := db.QueryRowContext(ctx, `INSERT INTO accounts (name) VALUES ($1) RETURNING id`, "ctr-acct-iso-b").Scan(&acctB); err != nil {
		t.Fatalf("insert account B: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM accounts WHERE id = $1`, acctB) })

	mgrA, err := gpRepo.InsertGamePlay(ctx, repository.GamePlayInsert{AccountID: acctA, MatchID: "match-iso-ctr-a", GameNumber: 1, Sequence: 1, OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("InsertGamePlay A: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM match_game_results WHERE id = $1`, mgrA) })

	mgrB, err := gpRepo.InsertGamePlay(ctx, repository.GamePlayInsert{AccountID: acctB, MatchID: "match-iso-ctr-b", GameNumber: 1, Sequence: 1, OccurredAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("InsertGamePlay B: %v", err)
	}
	t.Cleanup(func() { _, _ = db.ExecContext(ctx, `DELETE FROM match_game_results WHERE id = $1`, mgrB) })

	if err := ctrRepo.InsertCounters(ctx, []repository.GameEventCounterInsert{
		{MatchGameResultID: mgrA, AccountID: acctA, InstanceID: 301, ArenaID: 70001, CounterType: "loyalty", Count: 3, Delta: -2, Controller: "player", TurnNumber: 2},
	}); err != nil {
		t.Fatalf("InsertCounters A: %v", err)
	}

	nA, err := ctrRepo.CountByMatchGameResult(ctx, mgrA)
	if err != nil {
		t.Fatalf("CountByMatchGameResult A: %v", err)
	}
	nB, err := ctrRepo.CountByMatchGameResult(ctx, mgrB)
	if err != nil {
		t.Fatalf("CountByMatchGameResult B: %v", err)
	}

	if nA != 1 {
		t.Errorf("account A counter count: want 1, got %d", nA)
	}
	if nB != 0 {
		t.Errorf("account B counter count: want 0, got %d (cross-tenant leak)", nB)
	}
}

func TestGameEventCountersRepository_EmptyInserts_NoError(t *testing.T) {
	db := openTestDB(t)
	ctrRepo := repository.NewGameEventCountersRepository(db)
	ctx := context.Background()

	if err := ctrRepo.InsertCounters(ctx, nil); err != nil {
		t.Errorf("InsertCounters(nil): want no error, got %v", err)
	}
	if err := ctrRepo.InsertCounters(ctx, []repository.GameEventCounterInsert{}); err != nil {
		t.Errorf("InsertCounters(empty): want no error, got %v", err)
	}
}
