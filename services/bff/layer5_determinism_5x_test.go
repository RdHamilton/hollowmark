//go:build layer5

// Layer-5 AC9 5×-determinism gate — ADR-052 (ticket #1314).
//
// TestAC9Layer5FiveRunDeterminism proves that replaying the committed
// daemon-emit corpus through the full BFF pipeline five consecutive times
// produces byte-identical HTTP responses from all 7 BFF surfaces.
//
// What this test does that the existing determinism tests do NOT:
//
//   - Five full runs (not two): five independent schema-dropped replays.
//   - DROP SCHEMA public CASCADE between runs — each run starts from a truly
//     empty schema, not from the same accumulating DB. ON CONFLICT deduplication
//     cannot mask a non-deterministic upsert when the prior state is gone.
//   - Seven BFF HTTP surface responses captured per run and byte-compared
//     (post-sanitization) — not just row counts. An ordering instability or
//     non-deterministic ID in a response body passes the existing row-count
//     check but fails here.
//   - Diff printed on failure: the failing pair's surface JSON is shown.
//
// Schema-drop privilege note:
//
//	DROP SCHEMA public CASCADE requires the test DB user to be a superuser
//	(or to own the schema). In CI (bff-layer5.yml) the postgres service
//	container is pgvector/pgvector:pg16 with POSTGRES_USER: mtga — the
//	postgres-image bootstrap user is a SUPERUSER. For local runs, ensure
//	your DATABASE_URL user has superuser rights, e.g.:
//	  ALTER ROLE mtga SUPERUSER;
//
// Corpus scope: committed daemon-emit fixtures only (no LAYER5_CORPUS_SNAPSHOT_DIR
// needed). Satisfies "≥3 matches + ≥1 draft" per ADR-052 §AC9 because
// match-completed.json + match-completed-brawl.json + match-game-ended.json
// (≥3 match events) and draft-pack.json + draft-pick.json (≥1 draft) are included.
//
// Pinned date window: start_date=2026-01-01, end_date=2026-12-31 for all
// time-windowed surfaces (trends, rank-progression-timeline). This prevents
// wall-clock non-determinism: deterministicEpoch() = 2026-06-02T00:00:00Z
// falls inside the window on every run.
//
// Draft sessions: the committed daemon-emit corpus drafts (draft-pack.json,
// draft-pick.json) do not project into draft_sessions (historical fixtures
// predate session_id injection). The draft-surface endpoint returns an empty
// data array; this is still captured and byte-compared across 5 runs — an
// empty but deterministic response is a valid gate.
//
// Local run:
//
//	export DATABASE_URL="postgres://mtga:mtga@localhost:5432/mtga_test?sslmode=disable"
//	go test -v -race -tags layer5 -run TestAC9 -timeout 300s ./services/bff/
//
// NOTE: Do NOT rename this test to start with "TestLayer5" — bff-layer5.yml:157
// uses -run TestLayer5 for the hard-fail gate; this test runs in its own
// continue-on-error: true job (-run TestAC9) per RULE-INFRA-01 / ADR-052 AC4.
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
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	"github.com/go-chi/chi/v5"
)

// ─── TestAC9Layer5FiveRunDeterminism ─────────────────────────────────────────

// TestAC9Layer5FiveRunDeterminism is the AC9 5×-determinism pass gate.
//
// It runs the full daemon→BFF→projection pipeline 5 times, dropping and
// recreating the public schema between runs, and asserts that all 7 BFF surface
// responses are byte-identical (after timestamp/cursor normalization) across all
// 5 runs.
func TestAC9Layer5FiveRunDeterminism(t *testing.T) {
	const numRuns = 5

	// Pinned date window for time-windowed surfaces (trends, rank-progression).
	// deterministicEpoch() = 2026-06-02T00:00:00Z falls inside this window.
	const pinnedStartDate = "2026-01-01"
	const pinnedEndDate = "2026-12-31"

	// Corpus fixtures from committed daemon-emit/ (no LAYER5_CORPUS_SNAPSHOT_DIR needed).
	// Satisfies ≥3 matches + ≥1 draft per ADR-052 §AC9.
	// collection-updated.json and inventory-updated.json are intentionally omitted —
	// they project into tables not covered by the 7 BFF surfaces.
	corpusFixtures := []string{
		"match-completed.json",
		"match-completed-brawl.json",
		"match-game-ended.json",
		"quest-progress.json",
		"quest-progress-duplicate.json",
		"deck-updated.json",
		"draft-pack.json",
		"draft-pick.json",
	}

	// surface name → raw response bytes per run.
	type surfaceCapture = map[string][]byte
	runResults := make([]surfaceCapture, numRuns)
	for i := range runResults {
		runResults[i] = make(surfaceCapture)
	}

	corpusDir := ac9HermeticCorpusDir(t)

	for runIdx := 0; runIdx < numRuns; runIdx++ {
		t.Logf("[ac9/5x] === run %d/%d start ===", runIdx+1, numRuns)

		// ── 1. Drop public schema and re-run migrations ───────────────────────────
		// Requires SUPERUSER on the test DB role. See file header note.
		if err := ac9DropAndRecreateSchema(l5TestDBURL); err != nil {
			t.Fatalf("[ac9/5x] run %d: drop+recreate schema: %v", runIdx+1, err)
		}
		if err := storage.RunMigrations(l5TestDBURL); err != nil {
			t.Fatalf("[ac9/5x] run %d: RunMigrations after schema drop: %v", runIdx+1, err)
		}
		t.Logf("[ac9/5x] run %d: schema dropped + migrations applied", runIdx+1)

		// ── 2. Open DB, seed user + account ──────────────────────────────────────
		// l5OpenDB registers t.Cleanup(db.Close). The DB is closed after each run
		// since cleanup fires when the test completes; within each run iteration
		// we use the returned *sql.DB directly.
		db := l5OpenDB(t)
		clientID := fmt.Sprintf("ac9-det-run%d", runIdx+1)
		userID := l5SeedUser(t, db, clientID)
		accountID := l5ResolveAccountID(t, db, clientID, userID)

		// ── 3. Seed corpus events + drain projection queue ────────────────────────
		l5HermeticSeedFromCorpus(t, db, userID, clientID, corpusDir, corpusFixtures)
		t.Logf("[ac9/5x] run %d: corpus seeded (clientID=%s, accountID=%d)",
			runIdx+1, clientID, accountID)

		// ── 4. Resolve a matchID for the timeline surface ─────────────────────────
		// ORDER BY id ensures deterministic selection across runs.
		var matchID string
		if err := db.QueryRowContext(
			context.Background(),
			`SELECT id FROM matches WHERE account_id = $1 ORDER BY id LIMIT 1`,
			accountID,
		).Scan(&matchID); err != nil {
			// No match projected — timeline surface will capture the error response
			// (still deterministic: an empty matchID produces the same URL every run).
			t.Logf("[ac9/5x] run %d: no match projected (timeline surface captures empty response): %v",
				runIdx+1, err)
		}

		// ── 5. Build repos + handlers ─────────────────────────────────────────────
		matchesRepo := repository.NewMatchesRepository(db)
		accountRepo := repository.NewAccountRepository(db)
		questRepo := repository.NewQuestRepository(db)
		gamePlaysRepo := repository.NewGamePlaysRepository(db)
		draftSessionsRepo := repository.NewDraftSessionsRepository(db)
		cardsRepo := repository.NewCardsRepository(db)

		historyH := handlers.NewHistoryHandler(accountRepo, matchesRepo, draftSessionsRepo)
		gamePlaysH := handlers.NewGamePlaysHandler(gamePlaysRepo, accountRepo)
		questsH := handlers.NewQuestsHandler(questRepo, accountRepo)
		matchesH := handlers.NewMatchesHandler(matchesRepo, accountRepo)
		cardsH := handlers.NewCardsHandler(cardsRepo, accountRepo)

		// Auth injection middleware — injects userID without a real JWT.
		injectUser := func(uid int64) func(http.Handler) http.Handler {
			return func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					req = req.WithContext(bffmiddleware.WithUserID(req.Context(), uid))
					next.ServeHTTP(w, req)
				})
			}
		}

		// ── 6. Capture the 7 BFF surfaces ────────────────────────────────────────
		//
		// Surface endpoints are the authoritative bff_endpoint values declared in
		// services/daemon/testdata/corpus/layer5-expected/*.json manifests.

		// Surface 1: match-list — GET /api/v1/history/matches
		{
			r := chi.NewRouter()
			r.With(injectUser(userID)).Get("/api/v1/history/matches", historyH.GetMatches)
			ts := httptest.NewServer(r)
			_, body := ac9DoRaw(t, ts, http.MethodGet, "/api/v1/history/matches", nil)
			ts.Close()
			runResults[runIdx]["match-list"] = body
		}

		// Surface 2: match-detail-timeline
		// GET /api/v1/matches/{matchId}/plays/timeline
		{
			r := chi.NewRouter()
			r.With(injectUser(userID)).Get("/api/v1/matches/{matchId}/plays/timeline",
				gamePlaysH.MatchTimeline)
			ts := httptest.NewServer(r)
			path := "/api/v1/matches/" + matchID + "/plays/timeline"
			_, body := ac9DoRaw(t, ts, http.MethodGet, path, nil)
			ts.Close()
			runResults[runIdx]["match-detail-timeline"] = body
		}

		// Surface 3: quest-list — GET /api/v1/quests/active
		{
			r := chi.NewRouter()
			r.With(injectUser(userID)).Get("/api/v1/quests/active", questsH.Active)
			ts := httptest.NewServer(r)
			_, body := ac9DoRaw(t, ts, http.MethodGet, "/api/v1/quests/active", nil)
			ts.Close()
			runResults[runIdx]["quest-list"] = body
		}

		// Surface 4: win-rate-trend — POST /api/v1/matches/trends
		// Pinned start/end dates prevent parseTrendWindow() defaulting to time.Now().
		// periodType=week is the default in the SPA and produces deterministic bucketing
		// when data falls within the fixed 2026-01-01..2026-12-31 window.
		{
			r := chi.NewRouter()
			r.With(injectUser(userID)).Post("/api/v1/matches/trends", matchesH.Trends)
			ts := httptest.NewServer(r)
			reqBody, _ := json.Marshal(map[string]any{
				"periodType": "week",
				"startDate":  pinnedStartDate,
				"endDate":    pinnedEndDate,
			})
			_, body := ac9DoRaw(t, ts, http.MethodPost, "/api/v1/matches/trends", reqBody)
			ts.Close()
			runResults[runIdx]["win-rate-trend"] = body
		}

		// Surface 5: rank-progression
		// GET /api/v1/matches/rank-progression-timeline
		// Pinned start/end dates prevent wall-clock drift in the date filter.
		{
			r := chi.NewRouter()
			r.With(injectUser(userID)).Get("/api/v1/matches/rank-progression-timeline",
				matchesH.RankProgressionTimeline)
			ts := httptest.NewServer(r)
			path := fmt.Sprintf(
				"/api/v1/matches/rank-progression-timeline?format=QuickDraft_SOS_20260526&start_date=%s&end_date=%s",
				pinnedStartDate, pinnedEndDate,
			)
			_, body := ac9DoRaw(t, ts, http.MethodGet, path, nil)
			ts.Close()
			runResults[runIdx]["rank-progression"] = body
		}

		// Surface 6: draft-surface — GET /api/v1/history/drafts
		// The committed corpus does not project draft_sessions (historical fixtures
		// predate session_id injection per draft-surface.json corpus_promotion note).
		// An empty but deterministic response is a valid AC9 gate.
		{
			r := chi.NewRouter()
			r.With(injectUser(userID)).Get("/api/v1/history/drafts", historyH.GetDrafts)
			ts := httptest.NewServer(r)
			_, body := ac9DoRaw(t, ts, http.MethodGet, "/api/v1/history/drafts", nil)
			ts.Close()
			runResults[runIdx]["draft-surface"] = body
		}

		// Surface 7: deck-builder-resolution — GET /api/v1/cards/{arenaId}
		// The committed corpus does not populate set_cards (card catalog is seeded
		// by the sync Lambda in production). Requesting arena_id=90002 returns a
		// deterministic 404 on every run — still a valid byte-identical gate.
		// A future PR that seeds set_cards in CI will make this a non-404 assertion.
		{
			r := chi.NewRouter()
			r.With(injectUser(userID)).Get("/api/v1/cards/{arenaId}", cardsH.GetByArenaID)
			ts := httptest.NewServer(r)
			_, body := ac9DoRaw(t, ts, http.MethodGet, "/api/v1/cards/90002", nil)
			ts.Close()
			runResults[runIdx]["deck-builder-resolution"] = body
		}

		t.Logf("[ac9/5x] run %d: all 7 surfaces captured", runIdx+1)
	}

	// ── 7. Assert byte-identical across all 5 runs ────────────────────────────
	// Compare run[1..4] against run[0] (the baseline). Apply sanitization first
	// to normalize known non-deterministic fields (timestamps, cursors).
	surfaces := []string{
		"match-list",
		"match-detail-timeline",
		"quest-list",
		"win-rate-trend",
		"rank-progression",
		"draft-surface",
		"deck-builder-resolution",
	}

	t.Log("[ac9/5x] comparing 5 runs for byte-identity (post-sanitization)...")
	allPass := true
	for _, surface := range surfaces {
		baseline := ac9SanitizeForDiff(runResults[0][surface])
		for runIdx := 1; runIdx < numRuns; runIdx++ {
			candidate := ac9SanitizeForDiff(runResults[runIdx][surface])
			if !bytes.Equal(baseline, candidate) {
				allPass = false
				t.Errorf(
					"[ac9/5x] FAIL surface %q: run 0 vs run %d differ\n"+
						"--- run 0 (%d bytes):\n%s\n"+
						"--- run %d (%d bytes):\n%s\n"+
						"--- diff (first 2000 chars):\n%s",
					surface, runIdx,
					len(baseline), ac9Indent(baseline),
					runIdx, len(candidate), ac9Indent(candidate),
					ac9DiffLines(baseline, candidate, 2000),
				)
			}
		}
	}
	if allPass {
		t.Logf("[ac9/5x] PASS — all 7 surfaces byte-identical across %d runs (post-sanitization)", numRuns)
	}
}

// ─── Schema lifecycle ─────────────────────────────────────────────────────────

// ac9DropAndRecreateSchema drops the public schema (CASCADE) and recreates it
// with GRANT ALL ON SCHEMA public TO PUBLIC so subsequent migrations and test
// queries work without additional privilege grants.
//
// This is the between-run teardown per Ray's ruling R3 (#1314):
//   - DROP SCHEMA public CASCADE is blast-proof: future migrations cannot
//     accumulate stale state silently (unlike an explicit table list).
//   - schema_migrations is dropped too → storage.RunMigrations replays from
//     scratch on the next call.
//   - All extensions use CREATE EXTENSION IF NOT EXISTS so re-creation after
//     the CASCADE drop is clean (verified: citext/pgcrypto/vector/
//     pg_stat_statements in migrations 000051/54/64/86/109).
//
// Requires SUPERUSER. In CI: POSTGRES_USER: mtga is the postgres-image
// bootstrap superuser. For local runs: ALTER ROLE mtga SUPERUSER;
func ac9DropAndRecreateSchema(dbURL string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("open DB for schema drop: %w", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `DROP SCHEMA public CASCADE`); err != nil {
		return fmt.Errorf("DROP SCHEMA public CASCADE: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA public`); err != nil {
		return fmt.Errorf("CREATE SCHEMA public: %w", err)
	}
	// GRANT ALL so the test role and PUBLIC can use the fresh empty schema.
	if _, err := db.ExecContext(ctx, `GRANT ALL ON SCHEMA public TO PUBLIC`); err != nil {
		return fmt.Errorf("GRANT ALL ON SCHEMA public TO PUBLIC: %w", err)
	}
	return nil
}

// ─── Path helper ──────────────────────────────────────────────────────────────

// ac9HermeticCorpusDir returns the absolute path to
// services/daemon/testdata/corpus/daemon-emit/ resolved from this source file
// via runtime.Caller(0). Never CWD-relative (the #803 lesson).
func ac9HermeticCorpusDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ac9HermeticCorpusDir: runtime.Caller returned ok=false")
	}
	// thisFile: .../services/bff/layer5_determinism_5x_test.go
	// Corpus:   .../services/daemon/testdata/corpus/daemon-emit/
	dir := filepath.Clean(filepath.Join(
		filepath.Dir(thisFile), "..", "daemon", "testdata", "corpus", "daemon-emit",
	))
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatalf("ac9HermeticCorpusDir: daemon-emit corpus dir absent: %s", dir)
	}
	return dir
}

// ─── HTTP helper ──────────────────────────────────────────────────────────────

// ac9DoRaw executes an HTTP request against ts and returns the raw status code
// and body bytes. Does not fail the test on non-200 responses — a non-200 is
// still deterministic and is byte-compared across runs.
func ac9DoRaw(t *testing.T, ts *httptest.Server, method, path string, body []byte) (int, []byte) {
	t.Helper()
	var reqBodyReader *bytes.Reader
	if body != nil {
		reqBodyReader = bytes.NewReader(body)
	}
	var req *http.Request
	var err error
	if reqBodyReader != nil {
		req, err = http.NewRequestWithContext(context.Background(), method, ts.URL+path, reqBodyReader)
	} else {
		req, err = http.NewRequestWithContext(context.Background(), method, ts.URL+path, nil)
	}
	if err != nil {
		t.Fatalf("[ac9/5x] build request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("[ac9/5x] do request %s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("[ac9/5x] read response body %s %s: %v", method, path, err)
	}
	return resp.StatusCode, buf.Bytes()
}

// ─── Sanitization ─────────────────────────────────────────────────────────────

// ac9TimestampRe matches JSON-quoted RFC3339 and RFC3339Nano timestamp values.
//
// Covers:
//   - Second-precision:        "2026-06-02T00:00:00Z"
//   - Sub-second (RFC3339Nano): "2026-06-02T00:00:00.123456789Z"  ← next_cursor_ts
//   - With UTC offset:          "2026-06-02T00:00:00+00:00"
//
// The pattern matches the JSON-quoted value (e.g. "2026-06-02T00:00:00Z")
// and replaces the entire quoted string with "__TIMESTAMP__".
var ac9TimestampRe = regexp.MustCompile(
	`"(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2}))"`,
)

// ac9SanitizeForDiff normalizes fields that may vary between runs for reasons
// unrelated to pipeline correctness:
//
//   - RFC3339 and RFC3339Nano timestamp values — replaced with "__TIMESTAMP__".
//     Covers: created_at, updated_at, timestamp, first_seen_at, occurred_at,
//     start_date, end_date, StartDate, EndDate (trendAnalysisResponse),
//     next_cursor_ts (RFC3339Nano, history.go:206), BucketStart, BucketEnd.
//
// IDs (match_id, quest_id, deck_id, account_id) are NOT normalized — those
// must be stable across runs. A non-deterministic ID correctly fails the gate.
func ac9SanitizeForDiff(body []byte) []byte {
	return ac9TimestampRe.ReplaceAll(body, []byte(`"__TIMESTAMP__"`))
}

// ─── Diff helpers ─────────────────────────────────────────────────────────────

// ac9DiffLines returns a simple line-by-line diff of a and b, truncated to
// maxChars characters total. Uses "< " / "> " prefix notation. No external dep.
func ac9DiffLines(a, b []byte, maxChars int) string {
	aLines := strings.Split(string(a), "\n")
	bLines := strings.Split(string(b), "\n")

	var sb strings.Builder
	maxLines := len(aLines)
	if len(bLines) > maxLines {
		maxLines = len(bLines)
	}

	for i := 0; i < maxLines; i++ {
		if sb.Len() >= maxChars {
			sb.WriteString("...(truncated)")
			break
		}
		var aLine, bLine string
		if i < len(aLines) {
			aLine = aLines[i]
		}
		if i < len(bLines) {
			bLine = bLines[i]
		}
		if aLine != bLine {
			if i < len(aLines) {
				sb.WriteString("< ")
				sb.WriteString(aLine)
				sb.WriteString("\n")
			}
			if i < len(bLines) {
				sb.WriteString("> ")
				sb.WriteString(bLine)
				sb.WriteString("\n")
			}
		}
	}
	if sb.Len() == 0 {
		return "(no line-level diff — bytes differ but lines match; possible whitespace or encoding difference)"
	}
	return sb.String()
}

// ac9Indent pretty-prints JSON for display in failure messages.
// Falls back to the raw string on invalid JSON (e.g. a plain-text error body).
func ac9Indent(b []byte) string {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(out)
}
