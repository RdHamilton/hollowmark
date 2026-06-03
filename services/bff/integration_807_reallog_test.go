//go:build integration

package bff_integration_test

// End-to-end Local Verification gate for #807.
//
// This test ingests the EXACT non-partial match.game_ended payloads the daemon
// produces when replaying the REAL repo-root Player.log (captured to a file by
// the daemon test TestReplay_RealPlayerLog_NonEmptyAnchoredTimeline via
// VAULTMTG_REAL_LOG_OUT), runs them through the real BFF projection worker, and
// asserts ListGamePlaysByMatch + PlaysByMatch return a NON-EMPTY per-turn
// timeline anchored to the REAL match_id.
//
// This is the chain #2943 shipped past without proving:
//   daemon (real log) → match.game_ended → projection → game_plays →
//   ListGamePlaysByMatch / PlaysByMatch → non-empty timeline.
//
// Gated behind VAULTMTG_REAL_LOG_PAYLOAD (a JSON array of daemon-emitted
// payloads). The real Player.log is gitignored PII; only the projection result
// is asserted. Run via the Local Verification harness, not in CI.

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/bff/internal/storage/repository"
)

func TestIntegration_807_RealLogTimeline_EndToEnd(t *testing.T) {
	payloadFile := os.Getenv("VAULTMTG_REAL_LOG_PAYLOAD")
	if payloadFile == "" {
		t.Skip("VAULTMTG_REAL_LOG_PAYLOAD not set — real-log end-to-end runs in Local Verification only")
	}

	blob, err := os.ReadFile(payloadFile)
	if err != nil {
		t.Fatalf("read payload file: %v", err)
	}
	var payloads []json.RawMessage
	if err := json.Unmarshal(blob, &payloads); err != nil {
		t.Fatalf("decode captured daemon payloads: %v", err)
	}
	if len(payloads) == 0 {
		t.Fatal("captured payload file is empty — daemon produced no non-partial flush")
	}

	db := openIntegrationDB(t)
	userID := seedUser(t, db, "807-reallog")
	const clientID = "mtga-807-reallog"
	accountID := resolveAccountID(t, db, clientID, userID)

	// Decode each captured payload to learn its real match_id, seed the matches
	// FK anchor (normally written by the match.completed projector), then ingest
	// the daemon event verbatim.
	seen := map[string]bool{}
	for i, p := range payloads {
		var gp struct {
			MatchID    string `json:"match_id"`
			GameNumber int    `json:"game_number"`
			Partial    bool   `json:"partial"`
		}
		if err := json.Unmarshal(p, &gp); err != nil {
			t.Fatalf("decode payload %d: %v", i, err)
		}
		if gp.Partial {
			t.Fatalf("payload %d is partial — only non-partial game-end flushes are expected (#807)", i)
		}
		if gp.MatchID == "" {
			t.Fatalf("payload %d has empty match_id — the #807 fallback did not anchor it", i)
		}

		if !seen[gp.MatchID] {
			seen[gp.MatchID] = true
			mid := gp.MatchID
			_, err := db.ExecContext(context.Background(),
				`INSERT INTO matches
					(id, account_id, event_id, event_name, timestamp, player_wins, opponent_wins,
					 player_team_id, format, result)
				 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
				 ON CONFLICT (id) DO NOTHING`,
				mid, accountID, "807-evt", "Standard_BO3",
				time.Now().UTC(), 2, 1, 1, "Standard", "win")
			if err != nil {
				t.Fatalf("seed matches row for %s: %v", mid, err)
			}
			t.Cleanup(func() {
				_, _ = db.ExecContext(context.Background(), `DELETE FROM game_plays WHERE game_id IN (SELECT id FROM games WHERE match_id = $1)`, mid)
				_, _ = db.ExecContext(context.Background(), `DELETE FROM games WHERE match_id = $1`, mid)
				_, _ = db.ExecContext(context.Background(), `DELETE FROM match_game_results WHERE match_id = $1`, mid)
				_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, mid)
			})
		}

		insertDaemonEvent(t, db, repository.DaemonEventRow{
			UserID:     userID,
			AccountID:  clientID,
			EventType:  "match.game_ended",
			Payload:    p,
			OccurredAt: time.Now().UTC().Truncate(time.Microsecond),
			Sequence:   uint64(i + 1),
		})
	}

	// Project all pending events through the real worker.
	worker := buildWorker(db)
	worker.RunOnce(context.Background())

	// Assert each real match now renders a NON-EMPTY per-turn timeline.
	gpRepo := repository.NewGamePlayRepository(db)
	playsRepo := repository.NewGamePlaysRepository(db)

	for mid := range seen {
		// Game-level rows (ListGamePlaysByMatch — partial=false filtered).
		gameRows, err := gpRepo.ListGamePlaysByMatch(context.Background(), accountID, mid)
		if err != nil {
			t.Fatalf("ListGamePlaysByMatch %s: %v", mid, err)
		}
		if len(gameRows) == 0 {
			t.Fatalf("ListGamePlaysByMatch returned EMPTY for real match %s — false-empty trap NOT closed", mid)
		}

		// Per-turn timeline rows (PlaysByMatch — the rendered Match-Detail timeline).
		plays, err := playsRepo.PlaysByMatch(context.Background(), accountID, mid)
		if err != nil {
			t.Fatalf("PlaysByMatch %s: %v", mid, err)
		}
		if len(plays) == 0 {
			t.Fatalf("PlaysByMatch returned EMPTY per-turn timeline for real match %s — #807 not fixed", mid)
		}

		turns := map[int]int{}
		for _, p := range plays {
			turns[p.TurnNumber]++
		}
		t.Logf("[807-e2e] match %s: game_rows=%d card_plays=%d distinct_turns=%d",
			mid, len(gameRows), len(plays), len(turns))
		if len(turns) == 0 {
			t.Errorf("match %s: per-turn timeline has no turn grouping", mid)
		}
	}
}
