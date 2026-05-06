// Package projection provides a background worker that fans daemon_events rows
// into destination tables (matches, draft_sessions).
package projection

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ramonehamilton/mtga-bff/internal/storage/repository"
)

const (
	batchSize    = 100
	tickInterval = 30 * time.Second
)

// daemonEventStore is the subset of DaemonEventsRepository the worker uses.
type daemonEventStore interface {
	ListPendingProjection(ctx context.Context, limit int) ([]repository.DaemonEventRow, error)
	MarkProjected(ctx context.Context, id int64) error
}

// accountStore resolves accounts.id from accounts.client_id (the raw MTGA string).
type accountStore interface {
	GetOrCreateByClientID(ctx context.Context, clientID string, userID int64) (int64, error)
}

// matchStore writes to the matches table.
type matchStore interface {
	UpsertMatch(ctx context.Context, m repository.MatchUpsert) error
}

// draftStore writes to the draft_sessions table.
type draftStore interface {
	UpsertDraftSession(ctx context.Context, s repository.DraftSessionUpsert) error
}

// Worker projects pending daemon_events rows into their destination tables.
type Worker struct {
	events   daemonEventStore
	accounts accountStore
	matches  matchStore
	drafts   draftStore
}

// NewWorker returns a Worker wired with the provided stores.
func NewWorker(
	events daemonEventStore,
	accounts accountStore,
	matches matchStore,
	drafts draftStore,
) *Worker {
	return &Worker{
		events:   events,
		accounts: accounts,
		matches:  matches,
		drafts:   drafts,
	}
}

// Run starts the projection loop.  It performs an immediate drain on startup,
// then ticks every 30 seconds.  The loop exits when ctx is cancelled.
func (w *Worker) Run(ctx context.Context) {
	log.Println("[projection] worker started")

	w.runOnce(ctx)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[projection] worker stopped")
			return
		case <-ticker.C:
			w.runOnce(ctx)
		}
	}
}

// RunOnce is exported for integration tests.
func (w *Worker) RunOnce(ctx context.Context) {
	w.runOnce(ctx)
}

// runOnce fetches up to batchSize pending events and projects each one.
func (w *Worker) runOnce(ctx context.Context) {
	start := time.Now()

	var projected, skippedUnknown, skippedMalformed, errored int

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[projection] runOnce PANIC recovered: %v", r)
		}

		log.Printf(
			"[projection] runOnce completed pending=%d projected=%d skipped_unknown=%d skipped_malformed=%d errored=%d duration_ms=%d",
			projected+skippedUnknown+skippedMalformed+errored,
			projected, skippedUnknown, skippedMalformed, errored,
			time.Since(start).Milliseconds(),
		)
	}()

	rows, err := w.events.ListPendingProjection(ctx, batchSize)
	if err != nil {
		log.Printf("[projection] ListPendingProjection: %v", err)
		errored++
		return
	}

	for i := range rows {
		row := rows[i]

		outcome := w.projectRow(ctx, &row)

		switch outcome {
		case outcomeProjected:
			projected++
		case outcomeSkippedUnknown:
			skippedUnknown++
		case outcomeSkippedMalformed:
			skippedMalformed++
		case outcomeErrored:
			errored++
		}
	}
}

type projectionOutcome int

const (
	outcomeProjected projectionOutcome = iota
	outcomeSkippedUnknown
	outcomeSkippedMalformed
	outcomeErrored
)

// projectRow processes a single daemon_events row.
// It always attempts to mark the row as projected (even on skip/error) so
// malformed rows don't block the queue.
func (w *Worker) projectRow(ctx context.Context, row *repository.DaemonEventRow) projectionOutcome {
	var writeErr error

	outcome := outcomeProjected

	switch row.EventType {
	case "match.completed":
		writeErr = w.projectMatch(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectMatch id=%d: %v", row.ID, writeErr)
			outcome = outcomeSkippedMalformed
		}

	case "draft.started", "draft.completed":
		writeErr = w.projectDraftSession(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectDraftSession id=%d type=%s: %v", row.ID, row.EventType, writeErr)
			outcome = outcomeSkippedMalformed
		}

	case "draft.pick":
		// v0.2.0: increment total_picks on the session.
		writeErr = w.projectDraftPick(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectDraftPick id=%d: %v", row.ID, writeErr)
			outcome = outcomeSkippedMalformed
		}

	default:
		log.Printf("[projection] unknown event_type=%q id=%d — marking projected", row.EventType, row.ID)
		outcome = outcomeSkippedUnknown
	}

	// Always mark projected so we don't re-scan this row.
	if err := w.events.MarkProjected(ctx, row.ID); err != nil {
		log.Printf("[projection] MarkProjected id=%d: %v", row.ID, err)
		return outcomeErrored
	}

	return outcome
}

// --- payload shapes ---

type matchPayload struct {
	MatchID         string  `json:"match_id"`
	EventID         string  `json:"event_id"`
	EventName       string  `json:"event_name"`
	Format          string  `json:"format"`
	Result          string  `json:"result"`
	ResultReason    *string `json:"result_reason"`
	PlayerWins      int     `json:"player_wins"`
	OpponentWins    int     `json:"opponent_wins"`
	PlayerTeamID    int     `json:"player_team_id"`
	DeckID          *string `json:"deck_id"`
	RankBefore      *string `json:"rank_before"`
	RankAfter       *string `json:"rank_after"`
	DurationSeconds *int    `json:"duration_seconds"`
	OpponentName    *string `json:"opponent_name"`
	OpponentID      *string `json:"opponent_id"`
}

type draftPayload struct {
	SessionID string `json:"session_id"`
	EventName string `json:"event_name"`
	SetCode   string `json:"set_code"`
	DraftType string `json:"draft_type"`
	Status    string `json:"status"`
}

func (w *Worker) projectMatch(ctx context.Context, row *repository.DaemonEventRow) error {
	var p matchPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal match payload: %w", err)
	}

	if p.MatchID == "" {
		return fmt.Errorf("match payload missing match_id")
	}

	if p.Format == "" {
		return fmt.Errorf("match payload missing format")
	}

	result := normaliseResult(p.Result)
	if result == "" {
		return fmt.Errorf("match payload invalid result %q", p.Result)
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		return fmt.Errorf("resolve account: %w", err)
	}

	eventID := p.EventID
	if eventID == "" && row.EventID != nil {
		eventID = *row.EventID
	}

	return w.matches.UpsertMatch(ctx, repository.MatchUpsert{
		ID:              p.MatchID,
		AccountID:       accountID,
		EventID:         eventID,
		EventName:       p.EventName,
		Timestamp:       row.OccurredAt,
		DurationSeconds: p.DurationSeconds,
		PlayerWins:      p.PlayerWins,
		OpponentWins:    p.OpponentWins,
		PlayerTeamID:    p.PlayerTeamID,
		DeckID:          p.DeckID,
		RankBefore:      p.RankBefore,
		RankAfter:       p.RankAfter,
		Format:          p.Format,
		Result:          result,
		ResultReason:    p.ResultReason,
		OpponentName:    p.OpponentName,
		OpponentID:      p.OpponentID,
	})
}

func (w *Worker) projectDraftSession(ctx context.Context, row *repository.DaemonEventRow) error {
	var p draftPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal draft payload: %w", err)
	}

	if p.SessionID == "" {
		return fmt.Errorf("draft payload missing session_id")
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		return fmt.Errorf("resolve account: %w", err)
	}

	status := p.Status
	if status == "" {
		if row.EventType == "draft.completed" {
			status = "completed"
		} else {
			status = "in_progress"
		}
	}

	var endTime *time.Time
	if row.EventType == "draft.completed" {
		t := row.OccurredAt
		endTime = &t
	}

	return w.drafts.UpsertDraftSession(ctx, repository.DraftSessionUpsert{
		ID:        p.SessionID,
		AccountID: accountID,
		EventName: p.EventName,
		SetCode:   p.SetCode,
		DraftType: p.DraftType,
		StartTime: row.OccurredAt,
		EndTime:   endTime,
		Status:    status,
	})
}

type draftPickPayload struct {
	SessionID string `json:"session_id"`
}

func (w *Worker) projectDraftPick(ctx context.Context, row *repository.DaemonEventRow) error {
	var p draftPickPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal draft pick payload: %w", err)
	}

	if p.SessionID == "" {
		return fmt.Errorf("draft.pick payload missing session_id")
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		return fmt.Errorf("resolve account: %w", err)
	}

	// Upsert the session with a bumped total_picks counter via GREATEST.
	return w.drafts.UpsertDraftSession(ctx, repository.DraftSessionUpsert{
		ID:         p.SessionID,
		AccountID:  accountID,
		StartTime:  row.OccurredAt,
		Status:     "in_progress",
		TotalPicks: 1, // GREATEST(1, current) effectively increments when used in the ON CONFLICT clause
	})
}

// normaliseResult maps win/loss variants to the canonical DB value.
func normaliseResult(s string) string {
	switch s {
	case "win", "Win", "WIN":
		return "win"
	case "loss", "Loss", "LOSS":
		return "loss"
	default:
		return ""
	}
}
