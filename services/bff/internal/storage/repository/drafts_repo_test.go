package repository_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/bff/internal/storage/repository"
)

// TestDraftsRepository_ListSessions_WinsLosses verifies that ListSessions
// aggregates wins and losses from draft_match_results via the LEFT JOIN.
// A draft with N wins → Wins=N, Losses=M in the returned row.
func TestDraftsRepository_ListSessions_WinsLosses(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftsRepository(db)

	accountID := insertTestAccount(t, db, "drafts-repo-wl-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("dr-list-wl-%d", accountID)

	insertTestDraftSession(t, db, sessionID, accountID, "MKM", now)

	// 3 wins, 2 losses.
	insertTestDraftMatchResult(t, db, sessionID, "dr-mw1", "win", now.Add(time.Minute))
	insertTestDraftMatchResult(t, db, sessionID, "dr-mw2", "win", now.Add(2*time.Minute))
	insertTestDraftMatchResult(t, db, sessionID, "dr-mw3", "win", now.Add(3*time.Minute))
	insertTestDraftMatchResult(t, db, sessionID, "dr-ml1", "loss", now.Add(4*time.Minute))
	insertTestDraftMatchResult(t, db, sessionID, "dr-ml2", "loss", now.Add(5*time.Minute))

	rows, err := repo.ListSessions(context.Background(), accountID, repository.DraftFilter{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	var found *repository.DraftSessionDetailRow
	for i := range rows {
		if rows[i].ID == sessionID {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("seeded session %q not found in ListSessions results", sessionID)
	}
	if found.Wins != 3 {
		t.Errorf("Wins: want 3, got %d", found.Wins)
	}
	if found.Losses != 2 {
		t.Errorf("Losses: want 2, got %d", found.Losses)
	}
}

// TestDraftsRepository_ListSessions_NoMatches verifies that a session with no
// draft_match_results rows returns Wins=0, Losses=0 (LEFT JOIN + COALESCE).
func TestDraftsRepository_ListSessions_NoMatches(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftsRepository(db)

	accountID := insertTestAccount(t, db, "drafts-repo-zero-wl-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("dr-list-zero-%d", accountID)

	insertTestDraftSession(t, db, sessionID, accountID, "WOE", now)

	rows, err := repo.ListSessions(context.Background(), accountID, repository.DraftFilter{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	var found *repository.DraftSessionDetailRow
	for i := range rows {
		if rows[i].ID == sessionID {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("seeded session %q not found in ListSessions results", sessionID)
	}
	if found.Wins != 0 {
		t.Errorf("Wins: want 0 for no match results, got %d", found.Wins)
	}
	if found.Losses != 0 {
		t.Errorf("Losses: want 0 for no match results, got %d", found.Losses)
	}
}

// TestDraftsRepository_ListSessions_IsTrophyAndFormatType verifies that
// is_trophy=true and format_type are returned correctly by ListSessions.
// is_trophy is set when a session completes with wins >= 7.
func TestDraftsRepository_ListSessions_IsTrophyAndFormatType(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftsRepository(db)

	accountID := insertTestAccount(t, db, "drafts-repo-trophy-ft-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("dr-list-trophy-%d", accountID)

	// Insert with format_type=premier_draft, is_trophy=true directly.
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO draft_sessions
			(id, account_id, event_name, set_code, draft_type, start_time, status, format_type, is_trophy)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		sessionID, accountID, "PremierDraft_MKM", "MKM", "PremierDraft", now, "completed",
		"premier_draft", true,
	)
	if err != nil {
		t.Fatalf("insert draft_sessions with trophy: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, sessionID)
	})

	rows, err := repo.ListSessions(context.Background(), accountID, repository.DraftFilter{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	var found *repository.DraftSessionDetailRow
	for i := range rows {
		if rows[i].ID == sessionID {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("seeded session %q not found in ListSessions results", sessionID)
	}
	if !found.IsTrophy {
		t.Error("IsTrophy: want true, got false")
	}
	if found.FormatType != "premier_draft" {
		t.Errorf("FormatType: want premier_draft, got %q", found.FormatType)
	}
}

// TestDraftsRepository_ListSessions_TrophyAt7Wins verifies that a session with
// exactly 7 wins has IsTrophy=true (the is_trophy column is set by the
// projection worker, but this test confirms the read path surfaces it).
func TestDraftsRepository_ListSessions_TrophyAt7Wins(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftsRepository(db)

	accountID := insertTestAccount(t, db, "drafts-repo-7wins-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("dr-list-7w-%d", accountID)

	// Insert session with is_trophy=true (set by the projection worker).
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO draft_sessions
			(id, account_id, event_name, set_code, draft_type, start_time, status, is_trophy)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		sessionID, accountID, "QuickDraft_MKM", "MKM", "QuickDraft", now, "completed", true,
	)
	if err != nil {
		t.Fatalf("insert draft_sessions: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, sessionID)
	})

	// Seed 7 wins.
	for i := 0; i < 7; i++ {
		insertTestDraftMatchResult(t, db, sessionID, fmt.Sprintf("dr-7w-mw%d", i), "win", now.Add(time.Duration(i+1)*time.Minute))
	}
	insertTestDraftMatchResult(t, db, sessionID, "dr-7w-ml1", "loss", now.Add(8*time.Minute))

	rows, err := repo.ListSessions(context.Background(), accountID, repository.DraftFilter{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	var found *repository.DraftSessionDetailRow
	for i := range rows {
		if rows[i].ID == sessionID {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("seeded session %q not found in ListSessions results", sessionID)
	}
	if found.Wins != 7 {
		t.Errorf("Wins: want 7, got %d", found.Wins)
	}
	if found.Losses != 1 {
		t.Errorf("Losses: want 1, got %d", found.Losses)
	}
	if !found.IsTrophy {
		t.Error("IsTrophy: want true for 7-win session, got false")
	}
}

// TestDraftsRepository_ListSessions_CrossAccountIsolation verifies that
// ListSessions only returns sessions belonging to the queried account.
func TestDraftsRepository_ListSessions_CrossAccountIsolation(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftsRepository(db)

	accountA := insertTestAccount(t, db, "drafts-repo-iso-a-acct")
	accountB := insertTestAccount(t, db, "drafts-repo-iso-b-acct")
	now := time.Now().UTC().Truncate(time.Second)

	idA := fmt.Sprintf("dr-iso-a-%d", accountA)
	idB := fmt.Sprintf("dr-iso-b-%d", accountB)

	insertTestDraftSession(t, db, idA, accountA, "MKM", now)
	insertTestDraftSession(t, db, idB, accountB, "MKM", now.Add(-time.Second))

	rows, err := repo.ListSessions(context.Background(), accountA, repository.DraftFilter{})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}

	for _, r := range rows {
		if r.ID == idB {
			t.Errorf("cross-account leak: accountA query returned accountB session %q", r.ID)
		}
	}
}
