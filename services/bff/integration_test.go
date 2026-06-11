//go:build integration

package bff_integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/projection"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// corpusDir returns the absolute path to services/daemon/testdata/corpus/
// resolved relative to this source file so it works across the go.work
// boundary without embedding or copying fixtures.
func corpusDir(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("corpusDir: runtime.Caller returned ok=false")
	}

	// thisFile: .../services/bff/integration_test.go
	// Corpus:   .../services/daemon/testdata/corpus/
	dir := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "daemon", "testdata", "corpus"))

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		// Corpus absent = hard fail per Correction 2 (Ray): a silent skip
		// would defeat Layer 3a entirely.
		t.Fatalf("[integration] corpus dir absent: %s — Layer 3a requires #243 to be merged first", dir)
	}

	return dir
}

// mustReadCorpus reads a JSON file from the corpus directory and calls
// t.Fatal (never t.Skip) when the file is missing.  A silent skip would
// defeat Layer 3a entirely.
func mustReadCorpus(t *testing.T, corpus string, parts ...string) json.RawMessage {
	t.Helper()

	p := filepath.Join(append([]string{corpus}, parts...)...)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("[integration] corpus file missing or unreadable (%s): %v", p, err)
	}

	return json.RawMessage(b)
}

// ─── test database helpers ──────────────────────────────────────────────────

var testDBURL string

// TestMain runs migrations once, then runs all subtests.
func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "[integration] DATABASE_URL not set — skipping")
		os.Exit(0) // clean skip, not failure
	}

	if err := storage.RunMigrations(dbURL); err != nil {
		fmt.Fprintf(os.Stderr, "[integration] RunMigrations: %v\n", err)
		os.Exit(1)
	}

	testDBURL = dbURL
	os.Exit(m.Run())
}

// openIntegrationDB opens a *sql.DB for the test DATABASE_URL.
// The connection is closed via t.Cleanup.
func openIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("pgx", testDBURL)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("db.Ping: %v", err)
	}

	return db
}

// seedUser inserts a minimal users row and returns its id.
// Cleaned up by t.Cleanup.
func seedUser(t *testing.T, db *sql.DB, tag string) int64 {
	t.Helper()

	email := "integration-test-" + tag + "@vault-test.local"
	var id int64

	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO users (email) VALUES ($1) RETURNING id`,
		email,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedUser %q: %v", tag, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})

	return id
}

// corpusDaemonEventRow decodes a daemon-emit corpus fixture into a
// DaemonEventRow.  The corpus wire format wraps the inner payload in an
// envelope (type, account_id, event_id, sequence, occurred_at, payload).
func corpusDaemonEventRow(t *testing.T, raw json.RawMessage, userID int64, clientID string) repository.DaemonEventRow {
	t.Helper()

	var env struct {
		Type       string          `json:"type"`
		EventID    string          `json:"event_id"`
		Sequence   uint64          `json:"sequence"`
		OccurredAt time.Time       `json:"occurred_at"`
		Payload    json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode daemon-emit fixture: %v", err)
	}

	eventID := env.EventID
	return repository.DaemonEventRow{
		UserID:     userID,
		AccountID:  clientID, // use per-test unique client_id, not the fixture's
		EventType:  env.Type,
		Payload:    env.Payload,
		OccurredAt: env.OccurredAt,
		EventID:    &eventID,
		Sequence:   env.Sequence,
	}
}

// insertDaemonEvent writes a DaemonEventRow directly to daemon_events and
// returns the row's auto-assigned id.  Cleaned up via t.Cleanup.
func insertDaemonEvent(t *testing.T, db *sql.DB, row repository.DaemonEventRow) int64 {
	t.Helper()

	var nullableEventID *string
	if row.EventID != nil && *row.EventID != "" {
		nullableEventID = row.EventID
	}

	var id int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO daemon_events
		  (user_id, account_id, event_type, payload, occurred_at, event_id, sequence)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 RETURNING id`,
		row.UserID, row.AccountID, row.EventType, row.Payload,
		row.OccurredAt, nullableEventID, row.Sequence,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertDaemonEvent: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM daemon_events WHERE id = $1`, id)
	})

	return id
}

// buildWorker wires the real repositories into a projection Worker.
func buildWorker(db *sql.DB) *projection.Worker {
	events := repository.NewDaemonEventsRepository(db)
	accounts := repository.NewAccountRepository(db)
	matches := repository.NewMatchesRepository(db)
	drafts := repository.NewDraftSessionsRepository(db)
	collection := repository.NewCardInventoryRepository(db)
	inventory := repository.NewInventoryRepository(db)
	quests := repository.NewQuestRepository(db)
	decks := repository.NewDeckProjectorRepository(db)
	gamePlays := repository.NewGamePlayRepository(db)
	dlq := repository.NewProjectionErrorsRepository(db)

	return projection.NewWorker(
		events, accounts, matches, drafts, collection, inventory, quests, decks, gamePlays,
	).WithDLQ(dlq).
		WithCardPlayStore(gamePlays).
		WithGameRowWriter(gamePlays).
		WithGameIDResolver(gamePlays)
}

// resolveAccountID calls GetOrCreateByClientID to materialise an accounts row
// for clientID under userID and registers a t.Cleanup to remove it.
// Also cleans projection output rows (matches, quests, projection_errors) on
// teardown using both the numeric accountID and the string clientID, since
// projection_errors.account_id is TEXT while the other tables use BIGINT.
func resolveAccountID(t *testing.T, db *sql.DB, clientID string, userID int64) int64 {
	t.Helper()

	repo := repository.NewAccountRepository(db)
	accountID, err := repo.GetOrCreateByClientID(context.Background(), clientID, userID)
	if err != nil {
		t.Fatalf("resolveAccountID clientID=%s: %v", clientID, err)
	}

	t.Cleanup(func() {
		// matches and quests use BIGINT account_id FK.
		for _, tbl := range []string{"matches", "quests"} {
			_, _ = db.ExecContext(
				context.Background(),
				fmt.Sprintf(`DELETE FROM %s WHERE account_id = $1`, tbl),
				accountID,
			)
		}
		// projection_errors.account_id is TEXT (the raw client_id).
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM projection_errors WHERE account_id = $1`,
			clientID,
		)
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM accounts WHERE id = $1`,
			accountID,
		)
	})

	return accountID
}

// ─── Layer 3a integration tests ─────────────────────────────────────────────

// TestProjectionIntegration is the entry point for the Layer 3a corpus-driven
// smoke suite.  Each subtest is independent: it seeds its own user + account,
// inserts daemon_events rows from the corpus, runs worker.RunOnce, then asserts
// the projected DB state against the db-expected fixtures.
//
// Anti-flake invariants:
//   - No time.Now() in test bodies; all timestamps come from corpus fixtures.
//   - No uuid.New(); all IDs come from corpus fixtures.
//   - Each subtest uses a unique client_id (tag-scoped) to prevent
//     cross-contamination between subtests that share the same DB session.
//   - t.Cleanup handles all row removal.
//   - noopPostHogClient is the default (WithPostHogClient is never called).
func TestProjectionIntegration(t *testing.T) {
	corpus := corpusDir(t)

	t.Run("FullValidMatch", func(t *testing.T) {
		db := openIntegrationDB(t)

		raw := mustReadCorpus(t, corpus, "daemon-emit", "match-completed.json")
		expected := mustReadCorpus(t, corpus, "db-expected", "match-completed.json")

		var exp struct {
			ID           string `json:"ID"`
			EventID      string `json:"EventID"`
			Format       string `json:"Format"`
			Result       string `json:"Result"`
			PlayerWins   int    `json:"PlayerWins"`
			OpponentWins int    `json:"OpponentWins"`
			PlayerTeamID int    `json:"PlayerTeamID"`
		}
		if err := json.Unmarshal(expected, &exp); err != nil {
			t.Fatalf("decode db-expected: %v", err)
		}

		clientID := "test-l3a-match-full"
		userID := seedUser(t, db, clientID)
		accountID := resolveAccountID(t, db, clientID, userID)

		row := corpusDaemonEventRow(t, raw, userID, clientID)
		insertDaemonEvent(t, db, row)

		w := buildWorker(db)
		w.RunOnce(context.Background())

		// Assert: exactly one matches row with the corpus fixture values.
		var got struct {
			ID           string
			Format       string
			Result       string
			PlayerWins   int
			OpponentWins int
		}
		err := db.QueryRowContext(
			context.Background(),
			`SELECT id, format, result, player_wins, opponent_wins
			 FROM matches WHERE account_id = $1`,
			accountID,
		).Scan(&got.ID, &got.Format, &got.Result, &got.PlayerWins, &got.OpponentWins)
		if err != nil {
			t.Fatalf("SELECT matches: %v", err)
		}

		if got.ID != exp.ID {
			t.Errorf("match id: want %q, got %q", exp.ID, got.ID)
		}
		if got.Format != exp.Format {
			t.Errorf("format: want %q, got %q", exp.Format, got.Format)
		}
		if got.Result != exp.Result {
			t.Errorf("result: want %q, got %q", exp.Result, got.Result)
		}
		if got.PlayerWins != exp.PlayerWins {
			t.Errorf("player_wins: want %d, got %d", exp.PlayerWins, got.PlayerWins)
		}
		if got.OpponentWins != exp.OpponentWins {
			t.Errorf("opponent_wins: want %d, got %d", exp.OpponentWins, got.OpponentWins)
		}

		// AC7: zero projection_errors for a well-formed event.
		var errCount int
		_ = db.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM projection_errors WHERE account_id = $1`,
			clientID,
		).Scan(&errCount)
		if errCount != 0 {
			t.Errorf("projection_errors: want 0, got %d", errCount)
		}
	})

	t.Run("EmptyFormatFieldDefaultsToUnknown", func(t *testing.T) {
		db := openIntegrationDB(t)

		raw := mustReadCorpus(t, corpus, "daemon-emit", "match-completed-empty-format.json")
		expected := mustReadCorpus(t, corpus, "db-expected", "match-completed-empty-format.json")

		var exp struct {
			ID     string `json:"ID"`
			Format string `json:"Format"`
			Result string `json:"Result"`
		}
		if err := json.Unmarshal(expected, &exp); err != nil {
			t.Fatalf("decode db-expected: %v", err)
		}

		clientID := "test-l3a-match-emptyfmt"
		userID := seedUser(t, db, clientID)
		accountID := resolveAccountID(t, db, clientID, userID)

		row := corpusDaemonEventRow(t, raw, userID, clientID)
		insertDaemonEvent(t, db, row)

		w := buildWorker(db)
		w.RunOnce(context.Background())

		// AC4: format must be "Unknown" when the corpus event carries an empty format.
		var gotFormat string
		err := db.QueryRowContext(
			context.Background(),
			`SELECT format FROM matches WHERE account_id = $1 AND id = $2`,
			accountID, exp.ID,
		).Scan(&gotFormat)
		if err != nil {
			t.Fatalf("SELECT matches format: %v", err)
		}
		if gotFormat != "Unknown" {
			t.Errorf("format: want %q (AC4), got %q", "Unknown", gotFormat)
		}

		// AC4: zero projection_errors.
		var errCount int
		_ = db.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM projection_errors WHERE account_id = $1`,
			clientID,
		).Scan(&errCount)
		if errCount != 0 {
			t.Errorf("projection_errors: want 0 for empty-format event (AC4), got %d", errCount)
		}
	})

	t.Run("MissingMatchIDGoesToDLQ", func(t *testing.T) {
		db := openIntegrationDB(t)

		raw := mustReadCorpus(t, corpus, "daemon-emit", "match-completed-missing-id.json")

		clientID := "test-l3a-match-missingid"
		userID := seedUser(t, db, clientID)
		accountID := resolveAccountID(t, db, clientID, userID)

		row := corpusDaemonEventRow(t, raw, userID, clientID)
		insertDaemonEvent(t, db, row)

		w := buildWorker(db)
		w.RunOnce(context.Background())

		// AC5 Correction 1 (Ray): assert COUNT(*) FROM matches = 0, not WHERE id = ''.
		var matchCount int
		err := db.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM matches WHERE account_id = $1`,
			accountID,
		).Scan(&matchCount)
		if err != nil {
			t.Fatalf("COUNT matches: %v", err)
		}
		if matchCount != 0 {
			t.Errorf("matches COUNT: want 0 (AC5), got %d", matchCount)
		}

		// AC5: exactly one projection_errors row.
		// projection_errors.account_id is TEXT (raw client_id).
		var errCount int
		err = db.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM projection_errors WHERE account_id = $1`,
			clientID,
		).Scan(&errCount)
		if err != nil {
			t.Fatalf("COUNT projection_errors: %v", err)
		}
		if errCount != 1 {
			t.Errorf("projection_errors COUNT: want 1 (AC5), got %d", errCount)
		}
	})

	t.Run("QuestProgressDedup", func(t *testing.T) {
		db := openIntegrationDB(t)

		// Corpus: two quest.progress events carrying the same quest_id with
		// progressions 3→5.  After both rows are projected in a single RunOnce
		// pass, exactly one quests row must exist with ending_progress = 5
		// (the second upsert wins via ON CONFLICT DO UPDATE).
		raw1 := mustReadCorpus(t, corpus, "daemon-emit", "quest-progress.json")
		raw2 := mustReadCorpus(t, corpus, "daemon-emit", "quest-progress-duplicate.json")
		expected := mustReadCorpus(t, corpus, "db-expected", "quest-upsert-result.json")

		var exp struct {
			QuestID  string `json:"QuestID"`
			Progress int    `json:"Progress"`
			Goal     int    `json:"Goal"`
			CanSwap  bool   `json:"CanSwap"`
		}
		if err := json.Unmarshal(expected, &exp); err != nil {
			t.Fatalf("decode db-expected: %v", err)
		}

		clientID := "test-l3a-quest-dedup"
		userID := seedUser(t, db, clientID)
		accountID := resolveAccountID(t, db, clientID, userID)

		row1 := corpusDaemonEventRow(t, raw1, userID, clientID)
		row2 := corpusDaemonEventRow(t, raw2, userID, clientID)

		insertDaemonEvent(t, db, row1)
		insertDaemonEvent(t, db, row2)

		w := buildWorker(db)
		w.RunOnce(context.Background())

		// AC6: exactly one quests row for the deduplicated quest_id.
		var rowCount int
		err := db.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM quests WHERE account_id = $1 AND quest_id = $2`,
			accountID, exp.QuestID,
		).Scan(&rowCount)
		if err != nil {
			t.Fatalf("COUNT quests: %v", err)
		}
		if rowCount != 1 {
			t.Errorf("quests COUNT for quest_id=%s: want 1 (AC6), got %d", exp.QuestID, rowCount)
		}

		// AC6: ending_progress = 5 (the duplicate upsert value).
		var gotProgress int
		err = db.QueryRowContext(
			context.Background(),
			`SELECT ending_progress FROM quests WHERE account_id = $1 AND quest_id = $2`,
			accountID, exp.QuestID,
		).Scan(&gotProgress)
		if err != nil {
			t.Fatalf("SELECT ending_progress: %v", err)
		}
		if gotProgress != exp.Progress {
			t.Errorf("ending_progress: want %d (AC6), got %d", exp.Progress, gotProgress)
		}

		// AC6: zero projection_errors.
		var errCount int
		_ = db.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM projection_errors WHERE account_id = $1`,
			clientID,
		).Scan(&errCount)
		if errCount != 0 {
			t.Errorf("projection_errors: want 0 for valid quest events, got %d", errCount)
		}
	})

	// TestProjectionIntegration_APIResponseShape (AC7, 5th subtest):
	// Seeds a full valid match event, runs the worker, then hits a minimal Chi
	// router with the real MatchesHandler to verify the API response shape
	// matches api-expected/match-history-response.json.
	// Auth bypass: injects userID directly via bffmiddleware.WithUserID, the
	// same mechanism used in the handlers unit test suite.
	t.Run("APIResponseShape", func(t *testing.T) {
		db := openIntegrationDB(t)

		raw := mustReadCorpus(t, corpus, "daemon-emit", "match-completed.json")
		apiExpected := mustReadCorpus(t, corpus, "api-expected", "match-history-response.json")

		clientID := "test-l3a-match-api"
		userID := seedUser(t, db, clientID)
		resolveAccountID(t, db, clientID, userID)

		row := corpusDaemonEventRow(t, raw, userID, clientID)
		insertDaemonEvent(t, db, row)

		w := buildWorker(db)
		w.RunOnce(context.Background())

		// Build minimal Chi router with the real MatchesHandler.
		// Auth bypass: bffmiddleware.WithUserID injects the test userID into
		// the request context — no Clerk JWT required.
		matchesRepo := repository.NewMatchesRepository(db)
		accountRepo := repository.NewAccountRepository(db)
		h := handlers.NewMatchesHandler(matchesRepo, accountRepo)

		r := chi.NewRouter()
		r.Post("/api/v1/matches", func(w http.ResponseWriter, req *http.Request) {
			req = req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
			h.List(w, req)
		})

		ts := httptest.NewServer(r)
		t.Cleanup(ts.Close)

		// POST with limit=20 and no cursor (first page).
		body, _ := json.Marshal(map[string]any{"limit": 20})
		resp, err := http.Post(ts.URL+"/api/v1/matches", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatalf("POST /api/v1/matches: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status: want 200, got %d", resp.StatusCode)
		}

		// Decode the {"data": ...} envelope the MatchesHandler writes.
		var envelope struct {
			Data struct {
				Matches []struct {
					ID           string `json:"ID"`
					Format       string `json:"Format"`
					Result       string `json:"Result"`
					PlayerWins   int    `json:"PlayerWins"`
					OpponentWins int    `json:"OpponentWins"`
				} `json:"Matches"`
				HasMore bool `json:"HasMore"`
				Limit   int  `json:"Limit"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("decode response: %v", err)
		}

		// Decode the api-expected fixture.
		var exp struct {
			Matches []struct {
				ID           string `json:"ID"`
				Format       string `json:"Format"`
				Result       string `json:"Result"`
				PlayerWins   int    `json:"PlayerWins"`
				OpponentWins int    `json:"OpponentWins"`
			} `json:"Matches"`
			HasMore bool `json:"HasMore"`
			Limit   int  `json:"Limit"`
		}
		if err := json.Unmarshal(apiExpected, &exp); err != nil {
			t.Fatalf("decode api-expected: %v", err)
		}

		got := envelope.Data

		if len(got.Matches) != len(exp.Matches) {
			t.Fatalf("Matches count: want %d, got %d", len(exp.Matches), len(got.Matches))
		}
		if len(exp.Matches) > 0 {
			gotM := got.Matches[0]
			expM := exp.Matches[0]

			if gotM.ID != expM.ID {
				t.Errorf("Matches[0].ID: want %q, got %q", expM.ID, gotM.ID)
			}
			if gotM.Format != expM.Format {
				t.Errorf("Matches[0].Format: want %q, got %q", expM.Format, gotM.Format)
			}
			if gotM.Result != expM.Result {
				t.Errorf("Matches[0].Result: want %q, got %q", expM.Result, gotM.Result)
			}
			if gotM.PlayerWins != expM.PlayerWins {
				t.Errorf("Matches[0].PlayerWins: want %d, got %d", expM.PlayerWins, gotM.PlayerWins)
			}
			if gotM.OpponentWins != expM.OpponentWins {
				t.Errorf("Matches[0].OpponentWins: want %d, got %d", expM.OpponentWins, gotM.OpponentWins)
			}
		}
		if got.HasMore != exp.HasMore {
			t.Errorf("HasMore: want %v, got %v", exp.HasMore, got.HasMore)
		}
		if got.Limit != exp.Limit {
			t.Errorf("Limit: want %d, got %d", exp.Limit, got.Limit)
		}
	})
}

// ─── Layer 3a: player_on_play pipeline smoke (ticket #712) ─────────────────

// TestProjectionIntegration_PlayerOnPlay verifies the full player_on_play
// pipeline:
//
//	match.completed corpus event  → worker.RunOnce → matches row
//	match.game_ended corpus event → worker.RunOnce → match_game_results row (player_on_play=true)
//	GET /api/v1/history/matches   → response includes player_on_play: true for that match
//
// Root-cause context (#712): the ci-smoke seed only inserts match.completed
// events, which write to matches but NOT to match_game_results. The
// ListByAccountIDCursorFiltered LEFT JOIN on match_game_results finds no
// game-1 row, so player_on_play is NULL on every history row regardless of
// the column's migration state. This test gates the missing corpus fixture
// and proves the full chain in one shot.
func TestProjectionIntegration_PlayerOnPlay(t *testing.T) {
	db := openIntegrationDB(t)
	corpus := corpusDir(t)

	// Corpus fixtures — match.completed must project first so the match row
	// exists when match.game_ended arrives and resolves account_id.
	rawMatchCompleted := mustReadCorpus(t, corpus, "daemon-emit", "match-completed.json")
	rawGameEnded := mustReadCorpus(t, corpus, "daemon-emit", "match-game-ended.json")

	// Expected DB values after projection.
	expectedGameEnded := mustReadCorpus(t, corpus, "db-expected", "match-game-ended.json")

	var expGame struct {
		MatchID      string `json:"MatchID"`
		GameNumber   int    `json:"GameNumber"`
		PlayerOnPlay bool   `json:"PlayerOnPlay"`
		Partial      bool   `json:"Partial"`
	}
	if err := json.Unmarshal(expectedGameEnded, &expGame); err != nil {
		t.Fatalf("decode db-expected/match-game-ended.json: %v", err)
	}

	clientID := "test-l3a-player-on-play"
	userID := seedUser(t, db, clientID)
	accountID := resolveAccountID(t, db, clientID, userID)

	// Insert both events. Order matters: match.completed assigns the account row
	// used by match.game_ended's InsertGamePlay call.
	rowCompleted := corpusDaemonEventRow(t, rawMatchCompleted, userID, clientID)
	rowGameEnded := corpusDaemonEventRow(t, rawGameEnded, userID, clientID)

	insertDaemonEvent(t, db, rowCompleted)
	insertDaemonEvent(t, db, rowGameEnded)

	w := buildWorker(db)
	w.RunOnce(context.Background())

	// --- AC1: match_game_results row exists with correct player_on_play ---

	var gotGameNumber int
	var gotPlayerOnPlay *bool
	var gotPartial bool

	err := db.QueryRowContext(
		context.Background(),
		`SELECT game_number, player_on_play, partial
		 FROM match_game_results
		 WHERE account_id = $1 AND match_id = $2 AND game_number = $3`,
		accountID, expGame.MatchID, expGame.GameNumber,
	).Scan(&gotGameNumber, &gotPlayerOnPlay, &gotPartial)
	if err != nil {
		t.Fatalf("[PlayerOnPlay] SELECT match_game_results: %v — match.game_ended event was not projected", err)
	}

	if gotGameNumber != expGame.GameNumber {
		t.Errorf("[PlayerOnPlay] game_number: want %d, got %d", expGame.GameNumber, gotGameNumber)
	}
	if gotPlayerOnPlay == nil {
		t.Error("[PlayerOnPlay] player_on_play is NULL in match_game_results — corpus fixture not carrying the field?")
	} else if *gotPlayerOnPlay != expGame.PlayerOnPlay {
		t.Errorf("[PlayerOnPlay] player_on_play: want %v, got %v", expGame.PlayerOnPlay, *gotPlayerOnPlay)
	}
	if gotPartial != expGame.Partial {
		t.Errorf("[PlayerOnPlay] partial: want %v, got %v", expGame.Partial, gotPartial)
	}

	// --- AC2: history API response includes player_on_play for this match ---

	matchesRepo := repository.NewMatchesRepository(db)
	accountRepo := repository.NewAccountRepository(db)
	h := handlers.NewHistoryHandler(accountRepo, matchesRepo, nil)

	r := chi.NewRouter()
	r.Get("/api/v1/history/matches", func(w http.ResponseWriter, req *http.Request) {
		req = req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
		h.GetMatches(w, req)
	})

	ts := httptest.NewServer(r)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/api/v1/history/matches?limit=20")
	if err != nil {
		t.Fatalf("[PlayerOnPlay] GET /history/matches: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("[PlayerOnPlay] status: want 200, got %d", resp.StatusCode)
	}

	// history.go writes cursorPaginatedMatchResponse with top-level "data" array.
	var envelope struct {
		Data []struct {
			ID           string `json:"id"`
			PlayerOnPlay *bool  `json:"player_on_play"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("[PlayerOnPlay] decode history response: %v", err)
	}

	if len(envelope.Data) == 0 {
		t.Fatal("[PlayerOnPlay] history response data is empty — match.completed not projected?")
	}

	// Find the match we projected. There may be other matches from earlier subtests
	// if the DB is shared; look up by ID.
	var foundMatch *struct {
		ID           string `json:"id"`
		PlayerOnPlay *bool  `json:"player_on_play"`
	}
	for i := range envelope.Data {
		if envelope.Data[i].ID == expGame.MatchID {
			foundMatch = &envelope.Data[i]
			break
		}
	}
	if foundMatch == nil {
		t.Fatalf("[PlayerOnPlay] match ID %q not found in history response", expGame.MatchID)
	}

	if foundMatch.PlayerOnPlay == nil {
		t.Error("[PlayerOnPlay] history response player_on_play is null/absent — LEFT JOIN on match_game_results returned no row (ADR-050 gap: match.game_ended not in seed)")
	} else if *foundMatch.PlayerOnPlay != expGame.PlayerOnPlay {
		t.Errorf("[PlayerOnPlay] history response player_on_play: want %v, got %v", expGame.PlayerOnPlay, *foundMatch.PlayerOnPlay)
	}

	// AC3: zero projection_errors.
	var errCount int
	_ = db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM projection_errors WHERE account_id = $1`,
		clientID,
	).Scan(&errCount)
	if errCount != 0 {
		t.Errorf("[PlayerOnPlay] projection_errors: want 0, got %d", errCount)
	}
}

// ─── Layer 3b: BFF sync integration smoke (ADR-042 §Layer3b, closes #185) ───

// TestSyncIntegration is the entry point for the Layer 3b BFF sync
// integration smoke suite. It verifies the full pipeline:
//
//	stub Scryfall HTTP server → two-hop card fetch → inlined UpsertSetCards
//	→ set_cards non-empty (AC4) → /cards/sets/{setCode}/cards matches
//	api-expected corpus fixture (AC5).
//
// mtgzone_archetypes is seeded directly rather than via the Scryfall sync
// path because the Scryfall Lambda does NOT write to that table — it is
// owned by the MTGGoldfish / MTGTop8 scrape pipeline. The seed verifies
// that the BFF /meta/archetypes handler reads whatever is in the table.
//
// The services/sync/internal/datasets package cannot be imported from
// services/bff (Go's internal package rule applies across module
// boundaries even inside a go.work workspace). The UpsertSetCards SQL
// is therefore inlined in upsertSetCardsInlined below.
//
// Anti-flake invariants mirror Layer 3a: no time.Now() / uuid.New() in
// test bodies; unique client_id per subtest; t.Cleanup on all rows.
func TestSyncIntegration(t *testing.T) {
	corpus := corpusDir(t)

	// ── read fixtures ──────────────────────────────────────────────────────
	setCardsFixture := mustReadCorpus(t, corpus, "api-expected", "set-cards-response.json")
	metaFixture := mustReadCorpus(t, corpus, "api-expected", "meta-archetypes-response.json")

	// Decode the api-expected set-cards fixture: { "cards": [...] }
	var expSetCards struct {
		Cards []struct {
			ArenaID int    `json:"arena_id"`
			Name    string `json:"name"`
			SetCode string `json:"set_code"`
			Rarity  string `json:"rarity"`
		} `json:"cards"`
	}
	if err := json.Unmarshal(setCardsFixture, &expSetCards); err != nil {
		t.Fatalf("[Layer3b] decode set-cards-response.json: %v", err)
	}
	if len(expSetCards.Cards) == 0 {
		t.Fatal("[Layer3b] set-cards-response.json carries no cards — fixture may be empty or malformed")
	}

	// Decode the api-expected meta fixture: { "archetypes": [...] }
	var expMeta struct {
		Archetypes []struct {
			Name   string `json:"name"`
			Format string `json:"format"`
		} `json:"archetypes"`
	}
	if err := json.Unmarshal(metaFixture, &expMeta); err != nil {
		t.Fatalf("[Layer3b] decode meta-archetypes-response.json: %v", err)
	}
	if len(expMeta.Archetypes) == 0 {
		t.Fatal("[Layer3b] meta-archetypes-response.json carries no archetypes — fixture may be empty or malformed")
	}

	// Set code and format are driven by the corpus fixtures.
	setCode := expSetCards.Cards[0].SetCode
	metaFormat := expMeta.Archetypes[0].Format

	// ── Scryfall two-hop stub ──────────────────────────────────────────────
	//
	// Scryfall's bulk-data flow is a two-hop redirect:
	//   Hop 1: GET /bulk-data/default-cards
	//          → JSON { "download_uri": "<server>/bulk-cards" }
	//   Hop 2: GET <download_uri>
	//          → JSON array of card objects
	//
	// The stub is shared across all subtests; each subtest uses its own DB
	// connection opened via openIntegrationDB so rows are isolated.
	bulkCards := make([]map[string]any, 0, len(expSetCards.Cards))
	for _, c := range expSetCards.Cards {
		arenaID := c.ArenaID
		bulkCards = append(bulkCards, map[string]any{
			"id":               fmt.Sprintf("scryfall-fake-%d", arenaID),
			"arena_id":         arenaID,
			"name":             c.Name,
			"set":              c.SetCode,
			"rarity":           c.Rarity,
			"mana_cost":        "",
			"cmc":              0,
			"type_line":        "Creature",
			"oracle_text":      "",
			"colors":           []string{},
			"color_identity":   []string{},
			"collector_number": strconv.Itoa(arenaID),
			"power":            "",
			"toughness":        "",
			"loyalty":          "",
			"layout":           "normal",
			"released_at":      "2024-11-15",
			"image_uris":       nil,
			"card_faces":       nil,
			"legalities":       map[string]string{},
		})
	}

	bulkJSON, err := json.Marshal(bulkCards)
	if err != nil {
		t.Fatalf("[Layer3b] marshal bulkCards: %v", err)
	}

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bulk-data/default-cards":
			// Hop 1: return metadata pointing to the bulk download
			// path on this same stub server.
			meta := map[string]any{
				"object":       "bulk_data",
				"download_uri": "http://" + r.Host + "/bulk-cards",
			}
			w.Header().Set("Content-Type", "application/json")
			if encErr := json.NewEncoder(w).Encode(meta); encErr != nil {
				t.Logf("[Layer3b] stub /bulk-data/default-cards encode: %v", encErr)
			}
		case "/bulk-cards":
			// Hop 2: raw JSON array of card objects.
			w.Header().Set("Content-Type", "application/json")
			if _, writeErr := w.Write(bulkJSON); writeErr != nil {
				t.Logf("[Layer3b] stub /bulk-cards write: %v", writeErr)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(stub.Close)

	// ── subtest: SetCardsNonEmpty (AC4) ────────────────────────────────────
	t.Run("SetCardsNonEmpty", func(t *testing.T) {
		db := openIntegrationDB(t)

		cards, fetchErr := fetchBulkCardsFromStub(t, stub.URL)
		if fetchErr != nil {
			t.Fatalf("[Layer3b/SetCardsNonEmpty] fetchBulkCardsFromStub: %v", fetchErr)
		}
		upsertSetCardsInlined(t, db, cards)

		var count int
		if err := db.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM set_cards WHERE lower(set_code) = lower($1)`,
			setCode,
		).Scan(&count); err != nil {
			t.Fatalf("[Layer3b/SetCardsNonEmpty] COUNT set_cards: %v", err)
		}
		if count == 0 {
			t.Errorf("[Layer3b/SetCardsNonEmpty] set_cards COUNT for set=%s: want >0 (AC4)", setCode)
		}
	})

	// ── subtest: ArchetypesNonEmpty (AC4) ──────────────────────────────────
	t.Run("ArchetypesNonEmpty", func(t *testing.T) {
		db := openIntegrationDB(t)

		// Scryfall sync does NOT populate mtgzone_archetypes — see the
		// function-level comment above for the full rationale.
		seedMtgzoneArchetypes(t, db, expMeta.Archetypes[0].Name, metaFormat)

		var count int
		if err := db.QueryRowContext(
			context.Background(),
			`SELECT COUNT(*) FROM mtgzone_archetypes WHERE lower(format) = lower($1)`,
			metaFormat,
		).Scan(&count); err != nil {
			t.Fatalf("[Layer3b/ArchetypesNonEmpty] COUNT mtgzone_archetypes: %v", err)
		}
		if count == 0 {
			t.Errorf("[Layer3b/ArchetypesNonEmpty] mtgzone_archetypes COUNT for format=%s: want >0 (AC4)", metaFormat)
		}
	})

	// ── subtest: SetCardsAPIResponse (AC5) ─────────────────────────────────
	//
	// Fetches cards via stub, upserts to set_cards, then hits the real
	// CardsHandler.SetCards and validates the response against
	// api-expected/set-cards-response.json.
	t.Run("SetCardsAPIResponse", func(t *testing.T) {
		db := openIntegrationDB(t)

		cards, fetchErr := fetchBulkCardsFromStub(t, stub.URL)
		if fetchErr != nil {
			t.Fatalf("[Layer3b/SetCardsAPIResponse] fetchBulkCardsFromStub: %v", fetchErr)
		}
		upsertSetCardsInlined(t, db, cards)

		userID := seedUser(t, db, "test-l3b-cards-api")

		cardsRepo := repository.NewCardsRepository(db)
		accountRepo := repository.NewAccountRepository(db)
		h := handlers.NewCardsHandler(cardsRepo, accountRepo)

		r := chi.NewRouter()
		r.Get("/api/v1/cards/sets/{setCode}/cards", func(w http.ResponseWriter, req *http.Request) {
			req = req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
			h.SetCards(w, req)
		})

		ts := httptest.NewServer(r)
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + "/api/v1/cards/sets/" + setCode + "/cards")
		if err != nil {
			t.Fatalf("[Layer3b/SetCardsAPIResponse] GET /cards/sets/.../cards: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("[Layer3b/SetCardsAPIResponse] status: want 200, got %d", resp.StatusCode)
		}

		// CardsHandler wraps the slice in {"data": [...]}.
		var envelope struct {
			Data []struct {
				ArenaID string `json:"ArenaID"`
				Name    string `json:"Name"`
				SetCode string `json:"SetCode"`
				Rarity  string `json:"Rarity"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("[Layer3b/SetCardsAPIResponse] decode response: %v", err)
		}

		if len(envelope.Data) != len(expSetCards.Cards) {
			t.Fatalf("[Layer3b/SetCardsAPIResponse] card count: want %d (AC5), got %d",
				len(expSetCards.Cards), len(envelope.Data))
		}

		// Order-independent comparison: build name→{arenaID,rarity} map.
		gotByName := make(map[string]struct {
			ArenaID string
			Rarity  string
		}, len(envelope.Data))
		for _, c := range envelope.Data {
			gotByName[c.Name] = struct {
				ArenaID string
				Rarity  string
			}{ArenaID: c.ArenaID, Rarity: c.Rarity}
		}
		for _, exp := range expSetCards.Cards {
			got, ok := gotByName[exp.Name]
			if !ok {
				t.Errorf("[Layer3b/SetCardsAPIResponse] missing card %q in response (AC5)", exp.Name)
				continue
			}
			wantArenaID := strconv.Itoa(exp.ArenaID)
			if got.ArenaID != wantArenaID {
				t.Errorf("[Layer3b/SetCardsAPIResponse] %q ArenaID: want %s, got %s (AC5)",
					exp.Name, wantArenaID, got.ArenaID)
			}
			if got.Rarity != exp.Rarity {
				t.Errorf("[Layer3b/SetCardsAPIResponse] %q Rarity: want %s, got %s (AC5)",
					exp.Name, exp.Rarity, got.Rarity)
			}
		}
	})

	// ── subtest: MetaArchetypesAPIResponse (AC5) ───────────────────────────
	//
	// Seeds mtgzone_archetypes directly, then hits MetaHandler.Archetypes
	// and validates against api-expected/meta-archetypes-response.json.
	t.Run("MetaArchetypesAPIResponse", func(t *testing.T) {
		db := openIntegrationDB(t)

		// Scryfall sync does NOT populate mtgzone_archetypes — see the
		// function-level comment above for the full rationale.
		seedMtgzoneArchetypes(t, db, expMeta.Archetypes[0].Name, metaFormat)

		userID := seedUser(t, db, "test-l3b-meta-api")

		metaRepo := repository.NewMetaRepository(db)
		h := handlers.NewMetaHandler(metaRepo)

		r := chi.NewRouter()
		r.Get("/api/v1/meta/archetypes", func(w http.ResponseWriter, req *http.Request) {
			req = req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
			h.Archetypes(w, req)
		})

		ts := httptest.NewServer(r)
		t.Cleanup(ts.Close)

		resp, err := http.Get(ts.URL + "/api/v1/meta/archetypes?format=" + metaFormat)
		if err != nil {
			t.Fatalf("[Layer3b/MetaArchetypesAPIResponse] GET /meta/archetypes: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("[Layer3b/MetaArchetypesAPIResponse] status: want 200, got %d", resp.StatusCode)
		}

		// MetaHandler wraps in {"data": [...]}.
		var envelope struct {
			Data []struct {
				Name string `json:"name"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			t.Fatalf("[Layer3b/MetaArchetypesAPIResponse] decode response: %v", err)
		}

		if len(envelope.Data) == 0 {
			t.Fatalf("[Layer3b/MetaArchetypesAPIResponse] archetypes: want >0 (AC5), got 0")
		}

		expName := expMeta.Archetypes[0].Name
		found := false
		for _, a := range envelope.Data {
			if a.Name == expName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("[Layer3b/MetaArchetypesAPIResponse] archetype %q not found in response (AC5)", expName)
		}
	})
}

// ─── Layer 3b helpers ────────────────────────────────────────────────────────

// scryfallCardStub is a minimal card representation used by
// fetchBulkCardsFromStub and upsertSetCardsInlined. It mirrors only the
// fields that the inlined UpsertSetCards SQL actually writes.
type scryfallCardStub struct {
	ArenaID    *int
	ScryfallID string
	Name       string
	SetCode    string
	ManaCost   string
	CMC        int
	TypeLine   string
	Colors     []string
	Rarity     string
	OracleText string
	Power      string
	Toughness  string
}

// fetchBulkCardsFromStub performs the two-hop Scryfall bulk-data HTTP fetch
// against the provided stub server URL. This replicates the HTTP-level
// behaviour of scryfall.Client.FetchBulkDefaultCards using only stdlib
// HTTP so that services/sync/internal stays out of the import graph.
//
// Two-hop flow:
//
//	Hop 1: GET <baseURL>/bulk-data/default-cards → { "download_uri": "<url>" }
//	Hop 2: GET <download_uri>                    → JSON array of card objects
func fetchBulkCardsFromStub(t *testing.T, baseURL string) ([]scryfallCardStub, error) {
	t.Helper()

	// Hop 1.
	metaResp, err := http.Get(baseURL + "/bulk-data/default-cards")
	if err != nil {
		return nil, fmt.Errorf("hop1 GET /bulk-data/default-cards: %w", err)
	}
	defer metaResp.Body.Close()

	if metaResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hop1 status %d", metaResp.StatusCode)
	}

	var meta struct {
		DownloadURI string `json:"download_uri"`
	}
	if err := json.NewDecoder(metaResp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("hop1 decode: %w", err)
	}
	if meta.DownloadURI == "" {
		return nil, fmt.Errorf("hop1 response missing download_uri")
	}

	// Hop 2.
	dlResp, err := http.Get(meta.DownloadURI)
	if err != nil {
		return nil, fmt.Errorf("hop2 GET %s: %w", meta.DownloadURI, err)
	}
	defer dlResp.Body.Close()

	if dlResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hop2 status %d", dlResp.StatusCode)
	}

	// Decode as generic maps to avoid depending on services/sync types.
	var raw []map[string]any
	if err := json.NewDecoder(dlResp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("hop2 decode cards array: %w", err)
	}

	cards := make([]scryfallCardStub, 0, len(raw))
	for _, m := range raw {
		c := scryfallCardStub{
			ScryfallID: stringField(m, "id"),
			Name:       stringField(m, "name"),
			SetCode:    stringField(m, "set"),
			ManaCost:   stringField(m, "mana_cost"),
			TypeLine:   stringField(m, "type_line"),
			OracleText: stringField(m, "oracle_text"),
			Rarity:     stringField(m, "rarity"),
			Power:      stringField(m, "power"),
			Toughness:  stringField(m, "toughness"),
		}
		if v, ok := m["cmc"]; ok {
			if f, ok := v.(float64); ok {
				c.CMC = int(f)
			}
		}
		if v, ok := m["arena_id"]; ok {
			if f, ok := v.(float64); ok {
				id := int(f)
				c.ArenaID = &id
			}
		}
		if v, ok := m["colors"]; ok {
			if arr, ok := v.([]any); ok {
				for _, s := range arr {
					if str, ok := s.(string); ok {
						c.Colors = append(c.Colors, str)
					}
				}
			}
		}
		// Skip paper-only cards (no arena_id), matching the sync service.
		if c.ArenaID != nil {
			cards = append(cards, c)
		}
	}

	return cards, nil
}

// stringField extracts a string value from a map[string]any, returning ""
// when the key is absent or not a string.
func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

// upsertSetCardsInlined writes scryfallCardStub rows into set_cards using
// the same ON CONFLICT (set_code, arena_id) logic as
// services/sync/internal/datasets.PostgresStore.UpsertSetCards.
//
// This function is inlined because services/sync/internal/datasets is an
// internal package: Go's visibility rule prevents cross-module import even
// inside a go.work workspace. The SQL is a faithful reproduction of the
// production UpsertSetCards query, exercising the actual schema.
//
// t.Cleanup removes all inserted rows for the touched set codes.
func upsertSetCardsInlined(t *testing.T, db *sql.DB, cards []scryfallCardStub) {
	t.Helper()

	const q = `
		INSERT INTO set_cards (
			set_code, arena_id, scryfall_id, name, mana_cost, cmc, types,
			colors, rarity, text, power, toughness, image_url, image_url_small,
			image_url_art, legalities, fetched_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14,
			$15, $16, NOW()
		)
		ON CONFLICT (set_code, arena_id) DO UPDATE SET
			scryfall_id     = EXCLUDED.scryfall_id,
			name            = EXCLUDED.name,
			mana_cost       = EXCLUDED.mana_cost,
			cmc             = EXCLUDED.cmc,
			types           = EXCLUDED.types,
			colors          = EXCLUDED.colors,
			rarity          = EXCLUDED.rarity,
			text            = EXCLUDED.text,
			power           = EXCLUDED.power,
			toughness       = EXCLUDED.toughness,
			image_url       = EXCLUDED.image_url,
			image_url_small = EXCLUDED.image_url_small,
			image_url_art   = EXCLUDED.image_url_art,
			legalities      = EXCLUDED.legalities,
			fetched_at      = NOW()
	`

	touchedSetCodes := map[string]struct{}{}
	for i := range cards {
		c := &cards[i]
		if c.ArenaID == nil {
			continue
		}

		arenaIDText := strconv.Itoa(*c.ArenaID)
		colorsJSON, err := json.Marshal(c.Colors)
		if err != nil {
			t.Fatalf("upsertSetCardsInlined: marshal colors for %q: %v", c.Name, err)
		}

		if _, err := db.ExecContext(
			context.Background(), q,
			c.SetCode,
			arenaIDText,
			c.ScryfallID,
			c.Name,
			c.ManaCost,
			c.CMC,
			c.TypeLine,
			string(colorsJSON),
			c.Rarity,
			c.OracleText,
			c.Power,
			c.Toughness,
			"",   // image_url
			"",   // image_url_small
			"",   // image_url_art
			`{}`, // legalities (empty JSON object)
		); err != nil {
			t.Fatalf("upsertSetCardsInlined: upsert %q (set=%s arena_id=%s): %v",
				c.Name, c.SetCode, arenaIDText, err)
		}
		touchedSetCodes[c.SetCode] = struct{}{}
	}

	t.Cleanup(func() {
		for sc := range touchedSetCodes {
			_, _ = db.ExecContext(
				context.Background(),
				`DELETE FROM set_cards WHERE lower(set_code) = lower($1)`,
				sc,
			)
		}
	})
}

// seedMtgzoneArchetypes inserts one mtgzone_archetypes row for (name, format)
// and registers a t.Cleanup to remove it. Returns the new row id.
//
// Scryfall sync does NOT write to mtgzone_archetypes — that table is owned
// by the MTGGoldfish / MTGTop8 scrape pipeline. Seeding it directly here
// verifies that the /meta endpoint reads whatever is in the table,
// independent of which pipeline wrote it.
func seedMtgzoneArchetypes(t *testing.T, db *sql.DB, name, format string) int64 {
	t.Helper()

	var id int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO mtgzone_archetypes (name, format, last_updated)
		 VALUES ($1, $2, NOW())
		 ON CONFLICT (name, format) DO UPDATE SET last_updated = NOW()
		 RETURNING id`,
		name, format,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seedMtgzoneArchetypes name=%q format=%q: %v", name, format, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(
			context.Background(),
			`DELETE FROM mtgzone_archetypes WHERE id = $1`,
			id,
		)
	})

	return id
}

// ─── Defect A integration test: card plays persist end-to-end ────────────────

// TestIntegration_GamePlayEvent_CardPlays_PersistEndToEnd verifies Defect A:
// projecting a match.game_ended event carrying card_plays populates game_plays
// rows via the real path (games row created → InsertCardPlays writes).
//
// Before the fix, GameIDByMatchAndNumber always returned sql.ErrNoRows because
// the projection worker never populated the games table — InsertCardPlays was
// silently skipped for every live match. This test fails against the unfixed
// code and passes after WithGameRowWriter is wired.
//
// The test also verifies the timeline endpoint returns the card plays by
// querying GamePlaysRepository.PlaysByMatch and counting the result.
func TestIntegration_GamePlayEvent_CardPlays_PersistEndToEnd(t *testing.T) {
	db := openIntegrationDB(t)

	userID := seedUser(t, db, "defect-a-cp")
	const clientID = "mtga-defect-a-cardplays"
	accountID := resolveAccountID(t, db, clientID, userID)

	const matchID = "integ-defect-a-match-001"

	// Step 1: seed the matches row (required FK for game_plays via games.match_id).
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO matches
			(id, account_id, event_id, event_name, timestamp, player_wins, opponent_wins,
			 player_team_id, format, result)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (id) DO NOTHING`,
		matchID, accountID, "integ-evt-001", "Standard_BO1",
		time.Now().UTC(), 2, 1, 1, "Standard", "win",
	)
	if err != nil {
		t.Fatalf("seed match row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM games WHERE match_id = $1`, matchID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM match_game_results WHERE match_id = $1`, matchID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, matchID)
	})

	// Step 2: build the match.game_ended daemon event row carrying card_plays.
	now := time.Now().UTC().Truncate(time.Microsecond)
	payload, _ := json.Marshal(map[string]interface{}{
		"match_id":        matchID,
		"game_number":     1,
		"winning_team_id": 1,
		"turn_count":      6,
		"duration_secs":   180,
		"partial":         false,
		"schema_version":  2,
		"life_changes":    []interface{}{},
		"card_plays": []map[string]interface{}{
			{"game_number": 1, "turn_number": 2, "phase": "main1", "arena_id": 80001, "player_type": "player", "action_type": "play_card", "zone_from": "hand", "zone_to": "battlefield"},
			{"game_number": 1, "turn_number": 3, "phase": "main1", "arena_id": 80002, "player_type": "opponent", "action_type": "cast_spell", "zone_from": "hand", "zone_to": "stack"},
			{"game_number": 1, "turn_number": 4, "phase": "combat", "arena_id": 80003, "player_type": "player", "action_type": "attack", "zone_from": "battlefield", "zone_to": "battlefield"},
		},
	})

	row := repository.DaemonEventRow{
		UserID:     userID,
		AccountID:  clientID,
		EventType:  "match.game_ended",
		Payload:    payload,
		OccurredAt: now,
		Sequence:   1,
	}
	insertDaemonEvent(t, db, row)

	// Step 3: run the projection worker (real path — WithGameRowWriter wired in buildWorker).
	worker := buildWorker(db)
	worker.RunOnce(context.Background())

	// Step 4: assert game_plays rows were written.
	// AC: game_plays must have 3 rows for the match (not silently skipped).
	gpRepo := repository.NewGamePlayRepository(db)
	var gameID int64
	err = db.QueryRowContext(
		context.Background(),
		`SELECT id FROM games WHERE match_id = $1 AND game_number = $2`,
		matchID, 1,
	).Scan(&gameID)
	if err != nil {
		t.Fatalf("games row not created by projection: %v — Defect A not fixed", err)
	}

	n, err := gpRepo.CountCardPlaysByGame(context.Background(), gameID)
	if err != nil {
		t.Fatalf("CountCardPlaysByGame: %v", err)
	}
	if n != 3 {
		t.Errorf("game_plays count: want 3, got %d — InsertCardPlays was silently skipped (Defect A)", n)
	}

	// Step 5: assert the timeline read path returns the card plays.
	// This is the "timeline endpoint returns them" AC.
	playsRepo := repository.NewGamePlaysRepository(db)
	plays, err := playsRepo.PlaysByMatch(context.Background(), accountID, matchID)
	if err != nil {
		t.Fatalf("PlaysByMatch: %v", err)
	}
	if len(plays) != 3 {
		t.Errorf("PlaysByMatch: want 3 rows, got %d — timeline endpoint would return empty", len(plays))
	}

	// Verify player/opponent attribution is preserved (Defect B assertion on real path).
	playerCount := 0
	opponentCount := 0
	for _, p := range plays {
		switch p.PlayerType {
		case "player":
			playerCount++
		case "opponent":
			opponentCount++
		}
	}
	if playerCount != 2 {
		t.Errorf("player plays in timeline: want 2, got %d", playerCount)
	}
	if opponentCount != 1 {
		t.Errorf("opponent plays in timeline: want 1, got %d", opponentCount)
	}
}

// ─── Waitlist integration tests ──────────────────────────────────────────────

// TestWaitlistRepo_InsertIfNew_Integration covers AC7: POST with a valid email
// returns 200 + {"position": 1} against a real test DB.
//
// Covers three scenarios end-to-end against a real Postgres instance:
//  1. First insert returns (id, position=1, created=true).
//  2. Second insert of a different email returns position=2.
//  3. Duplicate insert returns ("", 0, created=false) — the 409 signal.
func TestWaitlistRepo_InsertIfNew_Integration(t *testing.T) {
	db := openIntegrationDB(t)
	repo := repository.NewWaitlistRepository(db)
	ctx := context.Background()

	// Use unique emails scoped to this test run to avoid cross-test contamination.
	email1 := fmt.Sprintf("waitlist-integration-a-%d@vault-test.local", time.Now().UnixNano())
	email2 := fmt.Sprintf("waitlist-integration-b-%d@vault-test.local", time.Now().UnixNano())

	// Remove rows added by this test even on failure.
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM waitlist_entries WHERE email = $1 OR email = $2`, email1, email2)
	})

	t.Run("FirstInsert_ReturnsPosition1_Created", func(t *testing.T) {
		id, position, created, err := repo.InsertIfNew(ctx, email1, nil, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("InsertIfNew: %v", err)
		}
		if !created {
			t.Fatal("expected created=true for first insert")
		}
		if id == "" {
			t.Fatal("expected non-empty id")
		}
		if position < 1 {
			t.Errorf("expected position >= 1, got %d", position)
		}
	})

	t.Run("SecondInsert_ReturnsHigherPosition_Created", func(t *testing.T) {
		id, position, created, err := repo.InsertIfNew(ctx, email2, nil, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("InsertIfNew: %v", err)
		}
		if !created {
			t.Fatal("expected created=true for second distinct email")
		}
		if id == "" {
			t.Fatal("expected non-empty id")
		}
		if position < 2 {
			t.Errorf("expected position >= 2 (two rows now), got %d", position)
		}
	})

	t.Run("DuplicateInsert_ReturnsNotCreated", func(t *testing.T) {
		id, position, created, err := repo.InsertIfNew(ctx, email1, nil, nil, nil, nil, nil, nil)
		if err != nil {
			t.Fatalf("InsertIfNew: %v", err)
		}
		if created {
			t.Fatal("expected created=false for duplicate email")
		}
		if id != "" {
			t.Errorf("expected empty id on duplicate, got %q", id)
		}
		if position != 0 {
			t.Errorf("expected position=0 on duplicate, got %d", position)
		}
	})
}

// TestWaitlistHandler_Integration_200WithPosition covers AC7 via the full HTTP
// handler stack against a real test DB.  A POST with a valid email must return
// 200 OK and a {"position": N} body where N >= 1.
func TestWaitlistHandler_Integration_200WithPosition(t *testing.T) {
	db := openIntegrationDB(t)
	repo := repository.NewWaitlistRepository(db)
	email := fmt.Sprintf("waitlist-handler-integration-%d@vault-test.local", time.Now().UnixNano())

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM waitlist_entries WHERE email = $1`, email)
	})

	h := handlers.NewWaitlistHandler(repo, nil, "")

	body, _ := json.Marshal(map[string]string{"email": email})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:9999"

	rr := httptest.NewRecorder()
	h.Join(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for new signup, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	posF, ok := resp["position"].(float64)
	if !ok {
		t.Fatalf(`expected {"position": N} body, got %v`, resp)
	}
	if int64(posF) < 1 {
		t.Errorf("expected position >= 1, got %d", int64(posF))
	}
}

// TestWaitlistHandler_Integration_409OnDuplicate covers AC3 via the full HTTP
// handler stack: second POST with the same email must return 409 Conflict.
func TestWaitlistHandler_Integration_409OnDuplicate(t *testing.T) {
	db := openIntegrationDB(t)
	repo := repository.NewWaitlistRepository(db)
	email := fmt.Sprintf("waitlist-dup-integration-%d@vault-test.local", time.Now().UnixNano())

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM waitlist_entries WHERE email = $1`, email)
	})

	h := handlers.NewWaitlistHandler(repo, nil, "")

	makeReq := func() *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"email": email})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "127.0.0.1:9998"
		rr := httptest.NewRecorder()
		h.Join(rr, req)
		return rr
	}

	// First request — must succeed.
	if rr := makeReq(); rr.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Second request with the same email — must return 409.
	rr := makeReq()
	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicate request: expected 409, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode 409 response: %v", err)
	}
	errMsg, _ := resp["error"].(string)
	if errMsg != "This email is already registered." {
		t.Errorf("409 error body: want %q, got %q", "This email is already registered.", errMsg)
	}
}
