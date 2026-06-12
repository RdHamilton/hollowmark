// Package projection provides a background worker that fans daemon_events rows
// into destination tables (matches, draft_sessions, card_inventory, inventory,
// quests, quest_session_tracking, decks, game_plays).
package projection

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/RdHamilton/hollowmark/services/contract"

	"github.com/RdHamilton/hollowmark/services/bff/internal/analytics"
	"github.com/RdHamilton/hollowmark/services/bff/internal/identityhash"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
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

// matchStore writes to the matches table and provides read-access for
// fields needed by dependent projections.
type matchStore interface {
	UpsertMatch(ctx context.Context, m repository.MatchUpsert) error
	// GetPlayerTeamIDForMatch returns the player_team_id stored on the matches
	// row for (accountID, matchID). Returns (0, nil) when the match row does
	// not exist yet — the match.completed event may not have been projected
	// before match.game_ended arrives. The caller must treat 0 as indeterminate.
	GetPlayerTeamIDForMatch(ctx context.Context, accountID int64, matchID string) (int, error)
	// GetResultForMatch returns the match-level result ("win", "loss", "draw")
	// stored on the matches row for (accountID, matchID). Returns ("", nil) when
	// the match row does not yet exist — the caller must treat "" as indeterminate
	// and fall back gracefully. Used by deriveGameResult when the GRE payload
	// carries WinningTeamID=0 (no final-win signal from GRE).
	GetResultForMatch(ctx context.Context, accountID int64, matchID string) (string, error)
}

// draftStore writes to the draft_sessions and draft_match_results tables.
type draftStore interface {
	UpsertDraftSession(ctx context.Context, s repository.DraftSessionUpsert) error
	// SessionExists returns true if a draft_sessions row with the given ID is
	// owned by accountID. Used to validate daemon-supplied DraftSessionID.
	SessionExists(ctx context.Context, accountID int64, sessionID string) (bool, error)
	// InferSessionForMatch returns the draft_sessions.id for the single
	// completed session matching eventName within 48 hours before matchTime.
	// Returns ("", nil) when zero or multiple sessions match.
	InferSessionForMatch(ctx context.Context, accountID int64, eventName string, matchTime time.Time) (string, error)
	// GetWinsForSession returns the number of 'win' rows in draft_match_results
	// for the given sessionID. Used to compute is_trophy on draft.completed.
	GetWinsForSession(ctx context.Context, sessionID string) (int, error)
	// InsertDraftMatchResult writes one row to draft_match_results.
	// ON CONFLICT (session_id, match_id) DO NOTHING — idempotent.
	InsertDraftMatchResult(ctx context.Context, r repository.DraftMatchResultInsert) error
	// InsertDraftPick writes one row to draft_picks.
	// ON CONFLICT (session_id, pack_number, pick_number) DO NOTHING — idempotent.
	InsertDraftPick(ctx context.Context, p repository.DraftPickInsert) error
}

// collectionStore writes card counts to the card_inventory table.
type collectionStore interface {
	UpsertDelta(ctx context.Context, u repository.CardInventoryUpsert) error
}

// inventoryStore writes player inventory snapshots to the inventory table.
type inventoryStore interface {
	UpsertInventory(ctx context.Context, u repository.InventoryUpsert) error
}

// questStore writes quest progress and completion records to the quests and
// quest_session_tracking tables.
type questStore interface {
	UpsertQuestProgress(ctx context.Context, u repository.QuestProgressUpsert) error
	InsertQuestCompleted(ctx context.Context, ins repository.QuestCompletedInsert) error
}

// deckStore writes deck snapshots to the decks and deck_cards tables.
type deckStore interface {
	UpsertDeck(ctx context.Context, u repository.DeckUpsert) error
}

// deckSummaryStore writes deck header rows from DeckSummaries login-blob entries.
// It deliberately omits any deck_cards write — see UpsertDeckSummary docs and
// Ray's amendment 1 on #1337.
type deckSummaryStore interface {
	UpsertDeckSummary(ctx context.Context, u repository.DeckSummaryUpsert) error
}

// draftDeckCreator creates a deck row from a completed draft session's picks.
// It is implemented by *repository.DecksRepository.
type draftDeckCreator interface {
	CreateDraftDeck(ctx context.Context, in repository.CreateDraftDeckInput) (*repository.DeckDetailRow, error)
}

// draftPickReader reads the picked card IDs for a session from draft_picks.
// It is implemented by *repository.DraftSessionsRepository.
type draftPickReader interface {
	PickCardIDsForSession(ctx context.Context, sessionID string) ([]string, error)
}

// gamePlayStore writes per-game result records and life-change rows.
// After ADR-050: InsertGamePlay writes to match_game_results (per-game);
// InsertLifeChanges references match_game_result_id.
type gamePlayStore interface {
	InsertGamePlay(ctx context.Context, ins repository.GamePlayInsert) (int64, error)
	InsertLifeChanges(ctx context.Context, changes []repository.LifeChangeInsert) error
}

// cardPlayStore writes per-turn card play rows to game_plays.
// After ADR-050: InsertCardPlays writes the turn-by-turn action log.
// accountID is required so game_plays.account_id is populated on every insert
// (defense-in-depth multi-tenancy hygiene, ticket #820).
type cardPlayStore interface {
	InsertCardPlays(ctx context.Context, accountID int64, gameID int64, matchID string, entries []contract.CardPlayEntry, occurredAt time.Time) error
}

// gameRowWriter creates the games anchor row required by game_plays.game_id FK.
// UpsertGameRow is idempotent: ON CONFLICT (match_id, game_number) returns the
// existing id so replaying the same event never produces duplicate rows.
// result must be "win" or "loss" — the projection worker derives this from the
// event payload before calling.
type gameRowWriter interface {
	UpsertGameRow(ctx context.Context, matchID string, gameNumber int, result string) (int64, error)
}

// counterStore writes counter-change rows to game_event_counters.
type counterStore interface {
	InsertCounters(ctx context.Context, inserts []repository.GameEventCounterInsert) error
}

// dlqStore writes permanently-failed projection rows to the dead-letter table.
type dlqStore interface {
	Insert(ctx context.Context, ins repository.ProjectionErrorInsert) error
}

// permanentErr wraps an error to signal that the failure is not transient —
// it will not be resolved by retrying.  Projection rows whose projector returns
// a permanent error are written to the projection_errors DLQ.
type permanentErr struct {
	cause error
}

func (e *permanentErr) Error() string { return e.cause.Error() }
func (e *permanentErr) Unwrap() error { return e.cause }

// permanent wraps err in permanentErr so the worker identifies it as a DLQ
// candidate rather than a transient retry.
func permanent(err error) error {
	if err == nil {
		return nil
	}
	return &permanentErr{cause: err}
}

// isPermanent reports whether err (or any error in its chain) is a permanentErr.
func isPermanent(err error) bool {
	var p *permanentErr
	return errors.As(err, &p)
}

// Worker projects pending daemon_events rows into their destination tables.
type Worker struct {
	events        daemonEventStore
	accounts      accountStore
	matches       matchStore
	drafts        draftStore
	draftDecks    draftDeckCreator
	draftPicks    draftPickReader
	collection    collectionStore
	inventory     inventoryStore
	quests        questStore
	decks         deckStore
	deckSummaries deckSummaryStore
	gamePlays     gamePlayStore
	cardPlays     cardPlayStore
	gameRows      gameRowWriter
	counters      counterStore
	dlq           dlqStore
	analytics     *analytics.Client
}

// NewWorker returns a Worker wired with the provided stores.
func NewWorker(
	events daemonEventStore,
	accounts accountStore,
	matches matchStore,
	drafts draftStore,
	collection collectionStore,
	inventory inventoryStore,
	quests questStore,
	decks deckStore,
	gamePlays gamePlayStore,
) *Worker {
	return &Worker{
		events:     events,
		accounts:   accounts,
		matches:    matches,
		drafts:     drafts,
		collection: collection,
		inventory:  inventory,
		quests:     quests,
		decks:      decks,
		gamePlays:  gamePlays,
		cardPlays:  nil, // optional; wired via WithCardPlayStore
		counters:   nil, // optional; wired via WithCounterStore
		dlq:        nil, // optional; wired via WithDLQ
		analytics:  analytics.NewClient(analytics.NoopEnqueuer{}, analytics.NewNoopHaltChecker()),
	}
}

// WithCardPlayStore wires the per-turn card play store into w and returns w.
func (w *Worker) WithCardPlayStore(store cardPlayStore) *Worker {
	w.cardPlays = store
	return w
}

// WithGameRowWriter wires the games-row upsert writer into w and returns w.
// When wired, projectGamePlayEvent calls UpsertGameRow to ensure the games FK
// anchor row exists before InsertCardPlays writes per-turn game_plays rows.
func (w *Worker) WithGameRowWriter(writer gameRowWriter) *Worker {
	w.gameRows = writer
	return w
}

// WithCounterStore wires the game_event_counters store into w and returns w.
func (w *Worker) WithCounterStore(store counterStore) *Worker {
	w.counters = store
	return w
}

// WithDLQ wires the dead-letter store into w and returns w.
func (w *Worker) WithDLQ(store dlqStore) *Worker {
	w.dlq = store
	return w
}

// WithDraftDeckCreator wires the draft-deck creation store into w and returns w.
// When wired, a draft.completed event automatically creates a deck row from the
// session's draft_picks (the draft → deck linkage per ADR-051).
func (w *Worker) WithDraftDeckCreator(creator draftDeckCreator) *Worker {
	w.draftDecks = creator
	return w
}

// WithDraftPickReader wires the draft-picks reader into w and returns w.
// Required alongside WithDraftDeckCreator — it supplies the card IDs for the
// new deck from draft_picks.
func (w *Worker) WithDraftPickReader(reader draftPickReader) *Worker {
	w.draftPicks = reader
	return w
}

// WithDeckSummaryStore wires the deck-summary (header-only) store into w and
// returns w.  When wired, projectInventoryUpdated fans out to UpsertDeckSummary
// for each entry in the payload's Decks slice without touching deck_cards.
func (w *Worker) WithDeckSummaryStore(store deckSummaryStore) *Worker {
	w.deckSummaries = store
	return w
}

// WithPostHogClient is deprecated. Use WithAnalyticsClient instead.
func (w *Worker) WithPostHogClient(client analytics.PostHogEnqueuer) *Worker {
	w.analytics = analytics.NewClient(client, analytics.NewNoopHaltChecker())
	return w
}

// WithAnalyticsClient wires an analytics.Client into the worker.
func (w *Worker) WithAnalyticsClient(c *analytics.Client) *Worker {
	w.analytics = c
	return w
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

	var projected, skippedUnknown, skippedMalformed, errored, deadLettered int

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[projection] runOnce PANIC recovered: %v", r)
		}

		log.Printf(
			"[projection] runOnce completed pending=%d projected=%d skipped_unknown=%d skipped_malformed=%d errored=%d dead_lettered=%d duration_ms=%d",
			projected+skippedUnknown+skippedMalformed+errored+deadLettered,
			projected, skippedUnknown, skippedMalformed, errored, deadLettered,
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
		case outcomeDeadLettered:
			deadLettered++
		}
	}
}

type projectionOutcome int

const (
	outcomeProjected projectionOutcome = iota
	outcomeSkippedUnknown
	outcomeSkippedMalformed
	outcomeErrored
	outcomeDeadLettered
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

	case "draft.started":
		writeErr = w.projectDraftSession(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectDraftSession id=%d type=%s: %v", row.ID, row.EventType, writeErr)
			outcome = outcomeSkippedMalformed
		}

	case "draft.completed":
		writeErr = w.projectDraftSession(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectDraftSession id=%d type=%s: %v", row.ID, row.EventType, writeErr)
			outcome = outcomeSkippedMalformed
			break
		}
		// Mint a draft deck from the session's picks once the draft completes.
		// Soft failure: if deck creation fails, the session is still projected.
		if w.draftDecks != nil && w.draftPicks != nil {
			if deckErr := w.projectDraftDeck(ctx, row); deckErr != nil {
				log.Printf("[projection] projectDraftDeck id=%d: %v — draft session projected without deck", row.ID, deckErr)
			}
		}

	case "draft.pick":
		// v0.2.0: increment total_picks on the session.
		writeErr = w.projectDraftPick(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectDraftPick id=%d: %v", row.ID, writeErr)
			outcome = outcomeSkippedMalformed
		}

	case "collection.updated":
		writeErr = w.projectCollectionUpdated(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectCollectionUpdated id=%d: %v", row.ID, writeErr)
			outcome = outcomeSkippedMalformed
		}

	case "inventory.updated":
		writeErr = w.projectInventoryUpdated(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectInventoryUpdated id=%d: %v", row.ID, writeErr)
			outcome = outcomeSkippedMalformed
		}

	case "quest.progress":
		writeErr = w.projectQuestProgress(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectQuestProgress id=%d: %v", row.ID, writeErr)
			outcome = outcomeSkippedMalformed
		}

	case "quest.completed":
		writeErr = w.projectQuestCompleted(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectQuestCompleted id=%d: %v", row.ID, writeErr)
			outcome = outcomeSkippedMalformed
		}

	case "deck.updated":
		writeErr = w.projectDeckUpdated(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectDeckUpdated id=%d: %v", row.ID, writeErr)
			outcome = outcomeSkippedMalformed
		}

	case "match.game_ended":
		writeErr = w.projectGamePlayEvent(ctx, row)
		if writeErr != nil {
			log.Printf("[projection] projectGamePlayEvent id=%d: %v", row.ID, writeErr)
			outcome = outcomeSkippedMalformed
		}

	default:
		log.Printf("[projection] unknown event_type=%q id=%d — marking projected", row.EventType, row.ID)
		outcome = outcomeSkippedUnknown
	}

	// If the projector returned a permanent error, write to the DLQ and emit
	// the projection.dead_letter PostHog metric.
	if writeErr != nil && isPermanent(writeErr) {
		outcome = w.writeToDLQ(ctx, row, writeErr)
	}

	// Always mark projected so we don't re-scan this row.
	if err := w.events.MarkProjected(ctx, row.ID); err != nil {
		log.Printf("[projection] MarkProjected id=%d: %v", row.ID, err)
		return outcomeErrored
	}

	return outcome
}

// writeToDLQ inserts a dead-letter row for a permanently-failed projection
// and emits the projection.dead_letter PostHog metric.
// Returns outcomeDeadLettered on success, outcomeSkippedMalformed on DLQ failure.
func (w *Worker) writeToDLQ(ctx context.Context, row *repository.DaemonEventRow, projErr error) projectionOutcome {
	if w.dlq == nil {
		// DLQ not wired — fall back to existing malformed behaviour.
		log.Printf("[projection] DLQ not wired, cannot dead-letter id=%d: %v", row.ID, projErr)
		return outcomeSkippedMalformed
	}

	rawPayload := string(row.Payload)

	dlqErr := w.dlq.Insert(ctx, repository.ProjectionErrorInsert{
		DaemonEventID: row.ID,
		AccountID:     row.AccountID,
		EventType:     row.EventType,
		RawPayload:    rawPayload,
		ErrorMessage:  projErr.Error(),
	})
	if dlqErr != nil {
		log.Printf("[projection] DLQ insert failed id=%d: %v", row.ID, dlqErr)
		return outcomeSkippedMalformed
	}

	log.Printf("[projection] dead-lettered id=%d event_type=%s: %v", row.ID, row.EventType, projErr)

	// Emit projection.dead_letter analytics metric.
	// account_id_hash uses SHA-256 (first 16 hex chars) per I-10 — never raw account_id.
	// Operational:true — system health telemetry, GDPR §6(1)(f) carve-out.
	acctHash := hashAccountIDProjection(row.AccountID)
	_ = w.analytics.Capture(ctx, acctHash, "projection.dead_letter", map[string]any{
		"account_id_hash": acctHash,
		"event_type":      row.EventType,
		"error_message":   projErr.Error(),
	}, analytics.CaptureOptions{Operational: true})

	return outcomeDeadLettered
}

// hashAccountIDProjection returns a privacy-safe representation of accountID
// for PostHog: SHA-256 hex, first 16 characters.  No raw PII is ever sent.
//
// Delegates to identityhash.HashAccountID per the FM-2 one-implementation rule.
func hashAccountIDProjection(accountID string) string {
	return identityhash.HashAccountID(accountID)
}

// emitMissingField emits a projection.missing_field analytics metric for an
// enrichment field that was absent from the event payload.  The account_id is
// hashed per I-10 — no raw PII is sent.
// Operational:true — system health telemetry, GDPR §6(1)(f) carve-out.
func emitMissingField(c *analytics.Client, accountID, field, eventType string) {
	acctHash := hashAccountIDProjection(accountID)
	_ = c.Capture(context.Background(), acctHash, "projection.missing_field", map[string]any{
		"account_id_hash": acctHash,
		"field":           field,
		"type":            eventType,
	}, analytics.CaptureOptions{Operational: true})
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
	// WinningTeamID is included so the projection can derive Result when the
	// daemon did not pre-compute it (e.g. player.authenticated not yet seen).
	WinningTeamID int `json:"winning_team_id"`
	// DraftSessionID is set by the daemon when the match was played using a
	// deck from an active draft session. Nil for non-draft matches.
	DraftSessionID *string `json:"draft_session_id"`
}

type draftPayload struct {
	SessionID string `json:"session_id"`
	EventName string `json:"event_name"`
	SetCode   string `json:"set_code"`
	DraftType string `json:"draft_type"`
	Status    string `json:"status"`
}

func (w *Worker) projectMatch(ctx context.Context, row *repository.DaemonEventRow) error {
	// Correction 2 (Ray): guard on empty account_id before the DB call.
	// An empty account_id is a structural payload defect — permanent error.
	if row.AccountID == "" {
		return permanent(fmt.Errorf("match.completed payload missing account_id"))
	}

	var p matchPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return permanent(fmt.Errorf("unmarshal match payload: %w", err))
	}

	if p.MatchID == "" {
		return permanent(fmt.Errorf("match payload missing match_id"))
	}

	if p.Format == "" {
		// Enrichment miss: format is not a PK/FK — default-fill and project.
		// Emit projection.missing_field metric so missing-format events are
		// observable without dropping the record.
		p.Format = "Unknown"
		log.Printf("[projection] projectMatch id=%d: format empty, defaulting to %q", row.ID, p.Format)
		emitMissingField(w.analytics, row.AccountID, "format", "match")
	}

	result := normaliseResult(p.Result)
	// Fallback: derive result from winning_team_id + player_team_id when the
	// daemon did not pre-compute the result string (player.authenticated not
	// yet observed in that daemon session).
	if result == "" && p.PlayerTeamID > 0 && p.WinningTeamID > 0 {
		if p.WinningTeamID == p.PlayerTeamID {
			result = "win"
		} else {
			result = "loss"
		}
	}
	// Q2 (Ray): result indeterminate is an enrichment miss — default-fill to
	// "unknown" so the row is projected rather than dropped.  The DB constraint
	// on matches.result is widened to ('win','loss','unknown') by migration
	// 000095.  Emit projection.missing_field so indeterminate-result events are
	// observable.
	if result == "" {
		result = "unknown"
		log.Printf("[projection] projectMatch id=%d: result indeterminate (result=%q winning_team_id=%d player_team_id=%d), defaulting to %q",
			row.ID, p.Result, p.WinningTeamID, p.PlayerTeamID, result)
		emitMissingField(w.analytics, row.AccountID, "result", "match")
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		// GetOrCreateByClientID failure is transient (DB call) — do NOT wrap in permanent().
		return fmt.Errorf("resolve account: %w", err)
	}

	eventID := p.EventID
	if eventID == "" && row.EventID != nil {
		eventID = *row.EventID
	}

	// Resolve the draft session ID for this match (ADR-051).
	// Path 1: daemon supplied it directly — validate ownership.
	// Path 2: attempt time-window inference when the event_name looks like a draft.
	// Path 3: leave nil (non-draft match or ambiguous inference).
	var draftSessionID *string
	if p.DraftSessionID != nil {
		exists, existsErr := w.drafts.SessionExists(ctx, accountID, *p.DraftSessionID)
		if existsErr != nil {
			log.Printf("[projection] projectMatch id=%d: SessionExists: %v — ignoring DraftSessionID", row.ID, existsErr)
		} else if !exists {
			log.Printf("[projection] projectMatch id=%d: DraftSessionID %q not found for account — ignoring", row.ID, *p.DraftSessionID)
		} else {
			draftSessionID = p.DraftSessionID
		}
	} else if isDraftEventName(p.EventName) {
		inferred, inferErr := w.drafts.InferSessionForMatch(ctx, accountID, p.EventName, row.OccurredAt)
		if inferErr != nil {
			log.Printf("[projection] projectMatch id=%d: InferSessionForMatch: %v", row.ID, inferErr)
		} else if inferred != "" {
			draftSessionID = &inferred
		}
	}

	if err := w.matches.UpsertMatch(ctx, repository.MatchUpsert{
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
		DraftSessionID:  draftSessionID,
	}); err != nil {
		return err
	}

	// Write draft_match_results when we have a resolved session ID.
	if draftSessionID != nil {
		if dmrErr := w.drafts.InsertDraftMatchResult(ctx, repository.DraftMatchResultInsert{
			SessionID:      *draftSessionID,
			MatchID:        p.MatchID,
			Result:         result,
			OpponentColors: nil, // v0.3.7: not derived; future enhancement
			GameWins:       p.PlayerWins,
			GameLosses:     p.OpponentWins,
			MatchTimestamp: row.OccurredAt,
		}); dmrErr != nil {
			// Soft failure — the match itself was projected; log and continue.
			log.Printf("[projection] projectMatch id=%d: InsertDraftMatchResult: %v — match projected without draft result", row.ID, dmrErr)
		}
	}

	return nil
}

// isDraftEventName returns true when the event_name heuristically identifies a
// draft event (contains "Draft" or "draft"). Used by the BFF inference fallback
// to avoid unnecessary DB queries on constructed/ladder matches.
func isDraftEventName(eventName string) bool {
	return strings.Contains(eventName, "Draft") || strings.Contains(eventName, "draft")
}

// deriveDraftFormatType maps an MTGA CourseName / event_name to the canonical
// format_type stored in draft_sessions. The rules follow the MTGA event naming
// convention used by the daemon and by the 17Lands primary filter.
//
//   - contains "QuickDraft"     → "quick_draft"
//   - contains "PremierDraft"   → "premier_draft"
//   - contains "TradDraft"      → "traditional_draft"
//   - contains "ContenderDraft" → "contender_draft"
//   - unknown                   → "quick_draft" (safe default matches column DEFAULT)
func deriveDraftFormatType(eventName string) string {
	switch {
	case strings.Contains(eventName, "QuickDraft"):
		return "quick_draft"
	case strings.Contains(eventName, "PremierDraft"):
		return "premier_draft"
	case strings.Contains(eventName, "TradDraft"):
		return "traditional_draft"
	case strings.Contains(eventName, "ContenderDraft"):
		return "contender_draft"
	default:
		return "quick_draft"
	}
}

func (w *Worker) projectDraftSession(ctx context.Context, row *repository.DaemonEventRow) error {
	var p draftPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal draft payload: %w", err)
	}

	if p.SessionID == "" {
		return permanent(fmt.Errorf("draft payload missing session_id"))
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

	// Derive format_type from the MTGA CourseName (stored as event_name).
	// This is only available when the event carries EventName — draft.pick
	// partial upserts may not, and the repo COALESCE guard retains existing.
	formatType := deriveDraftFormatType(p.EventName)

	// Compute is_trophy when the session is completing (draft.completed).
	// Query the current win count from draft_match_results — the match results
	// are projected before draft.completed in normal ordering, so this count
	// reflects the actual final record. Soft-fail: if the query fails, log and
	// leave is_trophy nil (DB default FALSE remains; backfill script corrects).
	var isTrophy *bool
	if row.EventType == "draft.completed" {
		wins, winsErr := w.drafts.GetWinsForSession(ctx, p.SessionID)
		if winsErr != nil {
			log.Printf("[projection] projectDraftSession id=%d: GetWinsForSession: %v — is_trophy not set", row.ID, winsErr)
		} else if wins >= 7 {
			t := true
			isTrophy = &t
		}
	}

	return w.drafts.UpsertDraftSession(ctx, repository.DraftSessionUpsert{
		ID:         p.SessionID,
		AccountID:  accountID,
		EventName:  p.EventName,
		SetCode:    p.SetCode,
		DraftType:  p.DraftType,
		StartTime:  row.OccurredAt,
		EndTime:    endTime,
		Status:     status,
		FormatType: formatType,
		IsTrophy:   isTrophy,
	})
}

type draftPickPayload struct {
	SessionID  string `json:"session_id"`
	PackNumber int    `json:"PackNumber"`
	PickNumber int    `json:"PickNumber"`
	// PickedCards is the raw JSON array of arena IDs. We take index 0 as the
	// single picked card for draft_picks.card_id (TEXT).
	PickedCards []int `json:"pickedCards"`
}

func (w *Worker) projectDraftPick(ctx context.Context, row *repository.DaemonEventRow) error {
	var p draftPickPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal draft pick payload: %w", err)
	}

	// Soft skip when session_id is absent — the daemon may have restarted
	// mid-draft. Do NOT dead-letter; ADR-051 / ADR-039 soft-skip pattern.
	if p.SessionID == "" {
		log.Printf("[projection] projectDraftPick id=%d: missing session_id — skipping (daemon restart?)", row.ID)
		return nil
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		return fmt.Errorf("resolve account: %w", err)
	}

	// Upsert the session with a bumped total_picks counter via GREATEST.
	if err := w.drafts.UpsertDraftSession(ctx, repository.DraftSessionUpsert{
		ID:         p.SessionID,
		AccountID:  accountID,
		StartTime:  row.OccurredAt,
		Status:     "in_progress",
		TotalPicks: 1, // GREATEST(1, current) effectively increments
	}); err != nil {
		return err
	}

	// Write the individual pick row when card data is present.
	if len(p.PickedCards) > 0 {
		cardID := fmt.Sprintf("%d", p.PickedCards[0])
		if pickErr := w.drafts.InsertDraftPick(ctx, repository.DraftPickInsert{
			SessionID:  p.SessionID,
			PackNumber: p.PackNumber,
			PickNumber: p.PickNumber,
			CardID:     cardID,
			Timestamp:  row.OccurredAt,
		}); pickErr != nil {
			// Soft failure — total_picks was bumped; individual pick row is
			// best-effort. Log and continue.
			log.Printf("[projection] projectDraftPick id=%d: InsertDraftPick: %v", row.ID, pickErr)
		}
	}

	return nil
}

// projectDraftDeck creates a decks row from the picks accumulated in
// draft_picks for the session that just completed. It is called as a soft
// follow-on after projectDraftSession succeeds on a draft.completed event.
//
// Idempotency: CreateDraftDeck checks whether a deck already exists for the
// (account_id, draft_session_id) pair and returns it if so — replay is safe.
//
// Card IDs: draft_picks.card_id is stored as TEXT (arena ID string). We parse
// each value to int before passing to CreateDraftDeck; unparseable rows are
// skipped with a log warning so a single bad pick does not abort deck creation.
func (w *Worker) projectDraftDeck(ctx context.Context, row *repository.DaemonEventRow) error {
	var p draftPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal draft payload for deck creation: %w", err)
	}
	if p.SessionID == "" {
		return fmt.Errorf("draft.completed payload missing session_id — cannot create deck")
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		return fmt.Errorf("resolve account: %w", err)
	}

	cardIDStrs, err := w.draftPicks.PickCardIDsForSession(ctx, p.SessionID)
	if err != nil {
		return fmt.Errorf("PickCardIDsForSession: %w", err)
	}
	if len(cardIDStrs) == 0 {
		log.Printf("[projection] projectDraftDeck: no picks found for session %s — skipping deck creation", p.SessionID)
		return nil
	}

	cardIDs := make([]int, 0, len(cardIDStrs))
	for _, s := range cardIDStrs {
		n, convErr := strconv.Atoi(strings.TrimSpace(s))
		if convErr != nil || n <= 0 {
			log.Printf("[projection] projectDraftDeck: skip unparseable card_id %q: %v", s, convErr)
			continue
		}
		cardIDs = append(cardIDs, n)
	}
	if len(cardIDs) == 0 {
		log.Printf("[projection] projectDraftDeck: all card_ids unparseable for session %s — skipping", p.SessionID)
		return nil
	}

	// Derive a human-readable deck name from the event name.
	deckName := deckNameForDraft(p.EventName, p.SetCode)

	_, createErr := w.draftDecks.CreateDraftDeck(ctx, repository.CreateDraftDeckInput{
		AccountID:      accountID,
		DraftSessionID: p.SessionID,
		Name:           deckName,
		Format:         "Limited",
		CardIDs:        cardIDs,
	})
	if createErr != nil {
		return fmt.Errorf("CreateDraftDeck session=%s: %w", p.SessionID, createErr)
	}

	log.Printf("[projection] projectDraftDeck: created deck for session %s (%d cards)", p.SessionID, len(cardIDs))
	return nil
}

// deckNameForDraft returns a human-readable deck name for a draft deck.
// Examples: "QuickDraft SOS" → "QuickDraft SOS Draft Deck",
//
//	"PremierDraft_BLB" → "PremierDraft BLB Draft Deck".
func deckNameForDraft(eventName, setCode string) string {
	if setCode != "" {
		return setCode + " Draft Deck"
	}
	if eventName != "" {
		// Replace underscores for display.
		display := strings.ReplaceAll(eventName, "_", " ")
		return display + " Draft Deck"
	}
	return "Draft Deck"
}

// projectCollectionUpdated applies the delta from a collection.updated event
// to card_inventory.  Each card entry is upserted independently so a partial
// delta (IsDelta=true) only touches the cards that changed.
//
// Idempotency: the snapshot_hash is derived from the raw payload bytes so
// replaying the exact same event produces no new writes.
func (w *Worker) projectCollectionUpdated(ctx context.Context, row *repository.DaemonEventRow) error {
	var p contract.CollectionUpdatedPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal collection.updated payload: %w", err)
	}

	if len(p.Cards) == 0 {
		// Empty delta is a no-op; not an error.
		return nil
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		return fmt.Errorf("resolve account: %w", err)
	}

	// Snapshot hash is computed from the raw payload bytes so it is stable
	// across re-sends of the same event.
	h := sha256.Sum256(row.Payload)
	snapshotHash := fmt.Sprintf("%x", h)

	for _, card := range p.Cards {
		if err := w.collection.UpsertDelta(ctx, repository.CardInventoryUpsert{
			AccountID:    accountID,
			CardID:       card.ArenaID,
			Count:        card.Count,
			SnapshotHash: snapshotHash,
		}); err != nil {
			return fmt.Errorf("UpsertDelta card_id=%d: %w", card.ArenaID, err)
		}
	}

	return nil
}

// --- inventory.updated projector ---

func (w *Worker) projectInventoryUpdated(ctx context.Context, row *repository.DaemonEventRow) error {
	var p contract.InventoryUpdatedPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal inventory.updated payload: %w", err)
	}

	if row.AccountID == "" {
		return permanent(fmt.Errorf("inventory.updated payload missing account_id"))
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		return fmt.Errorf("resolve account: %w", err)
	}

	if err := w.inventory.UpsertInventory(ctx, repository.InventoryUpsert{
		AccountID:          accountID,
		Gems:               p.Gems,
		Gold:               p.Gold,
		TotalVaultProgress: p.TotalVaultProgress,
		WildCardCommons:    p.WildCardCommons,
		WildCardUncommons:  p.WildCardUncommons,
		WildCardRares:      p.WildCardRares,
		WildCardMythics:    p.WildCardMythics,
		UpdatedAt:          row.OccurredAt,
	}); err != nil {
		return err
	}

	// Fan out to UpsertDeckSummary for each deck in the login-blob DeckSummaries
	// array (#1337). This uses the header-only path that never touches deck_cards
	// (Ray amendment 1). Only fires when the deckSummaries store is wired and the
	// payload carries at least one deck.
	if w.deckSummaries != nil && len(p.Decks) > 0 {
		for _, d := range p.Decks {
			if upsertErr := w.deckSummaries.UpsertDeckSummary(ctx, repository.DeckSummaryUpsert{
				DeckID:    d.DeckID,
				AccountID: accountID,
				Name:      d.Name,
				Format:    d.Format,
				UpdatedAt: row.OccurredAt,
			}); upsertErr != nil {
				// Soft failure: log and continue so a single bad deck row does not
				// prevent the rest of the fan-out or the inventory upsert from
				// being projected. The raw payload is preserved in daemon_events.
				log.Printf("[projection] projectInventoryUpdated id=%d: UpsertDeckSummary deck_id=%s: %v", row.ID, d.DeckID, upsertErr)
			}
		}
	}

	return nil
}

// --- quest.progress projector ---

func (w *Worker) projectQuestProgress(ctx context.Context, row *repository.DaemonEventRow) error {
	var p contract.QuestProgressPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal quest.progress payload: %w", err)
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		return fmt.Errorf("projectQuestProgress resolve account client_id=%s: %w", row.AccountID, err)
	}

	for _, q := range p.Quests {
		if q.QuestID == "" {
			continue
		}

		if err := w.quests.UpsertQuestProgress(ctx, repository.QuestProgressUpsert{
			AccountID: accountID,
			QuestID:   q.QuestID,
			QuestName: q.QuestName,
			Progress:  q.Progress,
			Goal:      q.Goal,
			CanSwap:   q.CanSwap,
			SeenAt:    row.OccurredAt,
		}); err != nil {
			return fmt.Errorf("UpsertQuestProgress quest_id=%s: %w", q.QuestID, err)
		}
	}

	return nil
}

// --- quest.completed projector ---

func (w *Worker) projectQuestCompleted(ctx context.Context, row *repository.DaemonEventRow) error {
	var p contract.QuestCompletedPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal quest.completed payload: %w", err)
	}

	if p.QuestID == "" {
		return permanent(fmt.Errorf("quest.completed payload missing quest_id"))
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		return fmt.Errorf("resolve account: %w", err)
	}

	return w.quests.InsertQuestCompleted(ctx, repository.QuestCompletedInsert{
		AccountID:        accountID,
		QuestID:          p.QuestID,
		QuestName:        p.QuestName,
		Progress:         p.Progress,
		Goal:             p.Goal,
		XPReward:         p.XPReward,
		CompletionSource: p.CompletionSource,
		OccurredAt:       row.OccurredAt,
	})
}

// --- deck.updated projector ---

func (w *Worker) projectDeckUpdated(ctx context.Context, row *repository.DaemonEventRow) error {
	var p contract.DeckUpdatedPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal deck.updated payload: %w", err)
	}

	if p.DeckID == "" {
		return permanent(fmt.Errorf("deck.updated payload missing deck_id"))
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		return fmt.Errorf("resolve account: %w", err)
	}

	cards := make([]repository.DeckCard, 0, len(p.Cards))
	for _, c := range p.Cards {
		cards = append(cards, repository.DeckCard{
			ArenaID:  c.ArenaID,
			Quantity: c.Quantity,
		})
	}

	return w.decks.UpsertDeck(ctx, repository.DeckUpsert{
		DeckID:    p.DeckID,
		AccountID: accountID,
		Name:      p.Name,
		Format:    p.Format,
		Cards:     cards,
		UpdatedAt: row.OccurredAt,
	})
}

// --- match.game_ended projector ---

// projectGamePlayEvent projects a match.game_ended event into
// match_game_results (per-game), game_plays (per-turn card plays),
// life_change_tracking, and game_event_counters.
//
// After ADR-050: InsertGamePlay writes to match_game_results (the new per-game
// results table). Per-turn card plays from p.CardPlays are written to
// game_plays via InsertCardPlays. The game_plays table schema (per-turn, from
// migration 000030/000054) is preserved and the two tables serve distinct
// purposes.
//
// Ordering guarantee: the Sequence field from the DaemonEvent envelope is
// written to match_game_results.sequence.  InsertGamePlay enforces a WHERE
// match_game_results.sequence < EXCLUDED.sequence guard on conflict, ensuring
// that out-of-order retransmissions of the same (match_id, game_number) do not
// regress the stored state.
//
// Card plays (ADR-050): after inserting the per-game row, resolve games.id
// from (match_id, game_number) and write each CardPlayEntry to game_plays.
// If games.id cannot be resolved (match.completed not yet projected), log at
// WARN and skip — the raw payload is preserved in daemon_events.payload for
// retroactive projection (v0.3.8 follow-on, not a v0.3.7 gate).
//
// Counter projection (ADR-046 A2.1, vmt-t#613): if the payload carries
// CounterChanges and the counterStore is wired, each entry is written to
// game_event_counters with ON CONFLICT DO NOTHING so replay is idempotent.
//
// Mulligan deferral (ADR-046 A2.2, vmt-t#614, deferred to v0.3.8): the
// Mulligan field is already stored verbatim in daemon_events.payload (JSONB)
// by the existing ingest path.  game_summaries.mulligan_json does not exist
// until v0.3.8 (its FK to mtgzone_archetypes is a Tier 1 gate).  This method
// logs at DEBUG and takes no write action for mulligan data.
func (w *Worker) projectGamePlayEvent(ctx context.Context, row *repository.DaemonEventRow) error {
	var p contract.GamePlayPayload
	if err := json.Unmarshal(row.Payload, &p); err != nil {
		return fmt.Errorf("unmarshal match.game_ended payload: %w", err)
	}

	// Partial events are GRE buffer flushes emitted before a game completes.
	// They may not yet carry a final match_id or game_number, so skip those
	// guards.  A follow-on ticket will add GRE entry parsing to populate these
	// fields once the GRE log schema is mapped.
	if !p.Partial {
		if p.MatchID == "" {
			return fmt.Errorf("match.game_ended payload missing match_id")
		}

		if p.GameNumber < 1 {
			return fmt.Errorf("match.game_ended payload invalid game_number %d", p.GameNumber)
		}
	}

	accountID, err := w.accounts.GetOrCreateByClientID(ctx, row.AccountID, row.UserID)
	if err != nil {
		return fmt.Errorf("resolve account: %w", err)
	}

	// InsertGamePlay writes to match_game_results (ADR-050).
	matchGameResultID, err := w.gamePlays.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID:     accountID,
		MatchID:       p.MatchID,
		GameNumber:    p.GameNumber,
		WinningTeamID: p.WinningTeamID,
		TurnCount:     p.TurnCount,
		DurationSecs:  p.DurationSecs,
		Sequence:      row.Sequence,
		OccurredAt:    row.OccurredAt,
		Partial:       p.Partial,
		PlayerOnPlay:  p.PlayerOnPlay,
	})
	if err != nil {
		return fmt.Errorf("InsertGamePlay: %w", err)
	}

	if len(p.LifeChanges) > 0 {
		changes := make([]repository.LifeChangeInsert, 0, len(p.LifeChanges))
		for _, lc := range p.LifeChanges {
			changes = append(changes, repository.LifeChangeInsert{
				AccountID:         accountID,
				MatchGameResultID: matchGameResultID,
				TeamID:            lc.TeamID,
				LifeTotal:         lc.LifeTotal,
				Delta:             lc.Delta,
				TurnNumber:        lc.TurnNumber,
			})
		}
		if err := w.gamePlays.InsertLifeChanges(ctx, changes); err != nil {
			return fmt.Errorf("InsertLifeChanges: %w", err)
		}
	}

	// Per-turn card play writes to game_plays (ADR-050).
	//
	// Ensure the games anchor row exists first — game_plays.game_id is a FK
	// into games. Before this fix the worker relied on match.completed having
	// already projected a games row, which never happened (the match.completed
	// projector only writes matches, not games). UpsertGameRow is idempotent so
	// replaying the event is safe.
	//
	// Non-partial events with no match_id or game_number are guarded above, so
	// we only reach here when both are valid. Partial events (no match_id) skip
	// the card-play path entirely via the len(p.CardPlays) guard.
	if len(p.CardPlays) > 0 && w.cardPlays != nil && !p.Partial {
		var gameID int64

		if w.gameRows != nil {
			// Derive the per-game result from the event payload.
			// p.WinningTeamID is the team that won this game (0 when indeterminate).
			// matches.player_team_id identifies the local player's team.
			// When match.completed has not been projected yet, player_team_id is 0
			// and we fall back to 'win' with a log warning — the same value the
			// previous placeholder wrote, so there is no regression; a subsequent
			// replay when match.completed has projected will correct it via the
			// ON CONFLICT UPDATE path.
			gameResult := deriveGameResult(ctx, w.matches, accountID, p.MatchID, p.WinningTeamID, row.ID)
			var upsertErr error
			gameID, upsertErr = w.gameRows.UpsertGameRow(ctx, p.MatchID, p.GameNumber, gameResult)
			if upsertErr != nil {
				return fmt.Errorf("UpsertGameRow: %w", upsertErr)
			}
		}

		if gameID > 0 {
			if err := w.cardPlays.InsertCardPlays(ctx, accountID, gameID, p.MatchID, p.CardPlays, row.OccurredAt); err != nil {
				return fmt.Errorf("InsertCardPlays: %w", err)
			}
		}
	}

	// Counter projection (ADR-046 A2.1, vmt-t#613).
	if len(p.CounterChanges) > 0 && w.counters != nil {
		cInserts := make([]repository.GameEventCounterInsert, 0, len(p.CounterChanges))
		for _, cc := range p.CounterChanges {
			cInserts = append(cInserts, repository.GameEventCounterInsert{
				MatchGameResultID: matchGameResultID,
				AccountID:         accountID,
				InstanceID:        cc.InstanceID,
				ArenaID:           cc.ArenaID,
				CounterType:       cc.CounterType,
				Count:             cc.Count,
				Delta:             cc.Delta,
				Controller:        cc.Controller,
				TurnNumber:        cc.TurnNumber,
			})
		}
		if err := w.counters.InsertCounters(ctx, cInserts); err != nil {
			return fmt.Errorf("InsertCounters: %w", err)
		}
	}

	// Mulligan deferral (ADR-046 A2.2, vmt-t#614, deferred to v0.3.8).
	// game_summaries.mulligan_json does not exist yet — storing verbatim in
	// daemon_events.payload (JSONB) is the current storage path.
	if p.Mulligan != nil {
		log.Printf("[projection] projectGamePlayEvent id=%d: mulligan data present (count=%d) — stored verbatim in daemon_events.payload; game_summaries.mulligan_json write deferred to v0.3.8 (ADR-046 A2.2)",
			row.ID, p.Mulligan.MulliganCount)
	}

	return nil
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

// deriveGameResult returns the per-game result ("win" or "loss") for a
// match.game_ended event.
//
// GRE payloads always carry WinningTeamID=0 (no final-win signal from GRE).
// The function therefore always calls GetPlayerTeamIDForMatch first.
//
// When winningTeamID is non-zero (future-proofing / non-GRE sources): compare
// winningTeamID directly against player_team_id to derive the result.
//
// When winningTeamID is zero (the common GRE case): call GetResultForMatch to
// obtain the match-level result stored by the match.completed projector, and
// return it directly. This cross-reference is the correct derivation path —
// the GRE payload alone is insufficient.
//
// If the match row does not yet exist (match.completed not yet projected),
// both lookups return zero/empty; the function falls back to "win" and logs a
// warning. A subsequent replay after match.completed is projected will
// overwrite the stored value via the ON CONFLICT UPDATE path.
func deriveGameResult(ctx context.Context, matches matchStore, accountID int64, matchID string, winningTeamID int, eventID int64) string {
	playerTeamID, err := matches.GetPlayerTeamIDForMatch(ctx, accountID, matchID)
	if err != nil {
		log.Printf("[projection] projectGamePlayEvent id=%d: GetPlayerTeamIDForMatch: %v — defaulting to 'win'", eventID, err)
		return "win"
	}

	if winningTeamID > 0 {
		// Non-zero winning team from payload: compare directly.
		if playerTeamID <= 0 {
			log.Printf("[projection] projectGamePlayEvent id=%d: match row not yet projected for match_id=%q — defaulting to 'win'", eventID, matchID)
			return "win"
		}
		if winningTeamID == playerTeamID {
			return "win"
		}
		return "loss"
	}

	// winningTeamID==0: GRE has no final-win signal. Cross-reference the
	// match-level result recorded by the match.completed projector.
	matchResult, err := matches.GetResultForMatch(ctx, accountID, matchID)
	if err != nil {
		log.Printf("[projection] projectGamePlayEvent id=%d: GetResultForMatch: %v — defaulting to 'win'", eventID, err)
		return "win"
	}
	if matchResult == "win" || matchResult == "loss" {
		return matchResult
	}

	log.Printf("[projection] projectGamePlayEvent id=%d: match row not yet projected for match_id=%q (winning_team_id=0) — defaulting to 'win'", eventID, matchID)
	return "win"
}
