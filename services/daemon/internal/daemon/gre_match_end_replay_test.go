package daemon

// Regression tests for #807: the daemon must emit a NON-PARTIAL
// "match.game_ended" GRE flush anchored to the authoritative
// finalMatchResult.matchId when a match completes. Before the fix, the GRE
// session manager only ever flushed partial events with no anchoring match_id,
// so PR #2943's read-side projection had no live data to project and the
// Match-Detail timeline rendered empty for 18/27 matches.
//
// These tests replay real-shaped MTGA log lines through handleEntry (the same
// path the live monitor and ReadFromStart replay use), capture the dispatched
// DaemonEvents, and assert a non-empty per-turn timeline anchored to the real
// match_id.
//
// The committed CI fixture (gre-game-session-with-match-end.log) reproduces the
// false-empty trap: GRE game-state messages carry NO gameInfo.matchID (the
// sparse-matchID case observed in the real Player.log), so the only path to a
// non-empty, anchored timeline is the explicit matchID fallback threaded from
// the match.completed detector into the game-end flush.
//
// The real-log replay (TestReplay_RealPlayerLog_*) is GATED behind
// VAULTMTG_REAL_LOG and is the on-machine proof captured in the PR's
// ## Local Verification block. The real Player.log is gitignored (raw PII) and
// never committed, so it cannot run in CI.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/RdHamilton/vault-mtg/services/contract"
	"github.com/RdHamilton/vault-mtg/services/daemon/internal/config"
	"github.com/RdHamilton/vault-mtg/services/daemon/internal/logreader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturedEvents is a thread-safe sink of DaemonEvents dispatched to the BFF.
type capturedEvents struct {
	mu     sync.Mutex
	events []contract.DaemonEvent
}

func (c *capturedEvents) add(e contract.DaemonEvent) {
	c.mu.Lock()
	c.events = append(c.events, e)
	c.mu.Unlock()
}

func (c *capturedEvents) byType(t string) []contract.DaemonEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []contract.DaemonEvent
	for _, e := range c.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

// newCapturingService spins up an httptest.Server that records every dispatched
// DaemonEvent and returns a Service wired to it.
func newCapturingService(t *testing.T, accountID string) (*Service, *capturedEvents) {
	t.Helper()
	cap := &capturedEvents{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			var evt contract.DaemonEvent
			if json.Unmarshal(body, &evt) == nil && evt.Type != "" {
				cap.add(evt)
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	s := New(&config.Config{
		CloudAPIURL: srv.URL,
		IngestPath:  "/v1/ingest/events",
		AccountID:   accountID,
		// Production defaults (config.New): without these the threshold is 0 and
		// every Append flushes partial immediately, draining the buffer before
		// match.completed fires — which is NOT the live behavior we are testing.
		GRESessionFlushThreshold: 500,
		GRESessionStaleMinutes:   15,
	})
	return s, cap
}

// replayLog reads every entry from the given log file and feeds it through
// handleEntry, exactly as the live monitor / ReadFromStart replay does.
func replayLog(t *testing.T, s *Service, path string) {
	t.Helper()
	reader, err := logreader.NewReader(path)
	require.NoError(t, err, "open log %s", path)
	defer func() { _ = reader.Close() }()

	ctx := context.Background()
	for {
		entry, err := reader.ReadEntry()
		if err == io.EOF {
			break
		}
		require.NoError(t, err, "read entry")
		// handleEntry mirrors the live path; ignore per-entry errors the way
		// replay.go does (they are logged, not fatal).
		_ = s.handleEntry(ctx, entry)
	}
}

func corpusFixture(t *testing.T, name string) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	// thisFile: .../services/daemon/internal/daemon/gre_match_end_replay_test.go
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "corpus", "player-log", name)
}

// assertNonEmptyAnchoredTimeline finds the non-partial match.game_ended event
// and asserts it is anchored to wantMatchID with a non-empty per-turn timeline.
func assertNonEmptyAnchoredTimeline(t *testing.T, cap *capturedEvents, wantMatchID string) {
	t.Helper()
	ended := cap.byType("match.game_ended")
	require.NotEmpty(t, ended, "daemon must dispatch at least one match.game_ended event")

	var nonPartial *contract.GamePlayPayload
	for i := range ended {
		var p contract.GamePlayPayload
		require.NoError(t, json.Unmarshal(ended[i].Payload, &p))
		if !p.Partial {
			pCopy := p
			nonPartial = &pCopy
			break
		}
	}
	require.NotNil(t, nonPartial,
		"there must be a NON-PARTIAL match.game_ended flush — this is the #807 fix; "+
			"a partial-only flush is the false-empty trap")

	assert.Equal(t, wantMatchID, nonPartial.MatchID,
		"non-partial flush must be anchored to the authoritative finalMatchResult.matchId")
	require.NotEmpty(t, nonPartial.CardPlays,
		"the timeline must be NON-EMPTY: per-turn card plays must be present")

	// Per-turn structure: every card play must carry a positive turn number so
	// the rendered Match-Detail timeline groups by turn.
	for _, cp := range nonPartial.CardPlays {
		assert.Positive(t, cp.TurnNumber, "each card play must carry a positive turn number")
	}
	assert.Positive(t, nonPartial.TurnCount, "TurnCount must be positive for a non-empty timeline")
}

// TestReplay_CrossReferencedFixture_NonEmptyAnchoredTimeline is the CI gate for
// #807. The fixture's GRE game-state messages carry NO gameInfo.matchID, so the
// non-empty anchored timeline can only be produced by the explicit matchID
// fallback threaded from match.completed into the game-end flush.
func TestReplay_CrossReferencedFixture_NonEmptyAnchoredTimeline(t *testing.T) {
	s, cap := newCapturingService(t, "acct-807-fixture")
	replayLog(t, s, corpusFixture(t, "gre-game-session-with-match-end.log"))

	// The fixture's finalMatchResult.matchId.
	assertNonEmptyAnchoredTimeline(t, cap, "22222222-0000-4000-8000-000000000099")
}

// TestReplay_CrossReferencedFixture_HealOnReplayFromStart proves the ADR-054
// self-heal claim: re-sending the full history (the ReadFromStart=true path a
// tester hits on first run) re-projects the previously-stranded plays. We model
// "first run" by replaying the same log twice through the same service and
// asserting the second pass still produces a non-empty anchored non-partial
// flush (the daemon re-emits; the BFF's ON CONFLICT DO NOTHING makes the write
// idempotent — see game_play_repo.go:186).
func TestReplay_CrossReferencedFixture_HealOnReplayFromStart(t *testing.T) {
	s, cap := newCapturingService(t, "acct-807-heal")

	// First run.
	replayLog(t, s, corpusFixture(t, "gre-game-session-with-match-end.log"))
	first := len(cap.byType("match.game_ended"))
	require.Positive(t, first, "first run must emit a game-end flush")

	// Second run = ReadFromStart re-send drain on a fresh install / heal.
	replayLog(t, s, corpusFixture(t, "gre-game-session-with-match-end.log"))

	// Heal re-emits the non-partial anchored timeline. Back-to-back match-ends
	// in a single re-send drain must NOT leak match_id across matches (Decision
	// (b): explicit matchID arg, referentially transparent).
	assertNonEmptyAnchoredTimeline(t, cap, "22222222-0000-4000-8000-000000000099")
	require.Greater(t, len(cap.byType("match.game_ended")), first,
		"replay-from-start must re-emit the game-end flush (self-heal, ADR-054)")
}

// TestReplay_RealPlayerLog_NonEmptyAnchoredTimeline replays the REAL repo-root
// Player.log through the full daemon path and asserts a non-empty per-turn
// timeline anchored to a real match_id. This is the exact test that would have
// caught the false-empty trap that #2943 shipped past.
//
// Gated behind VAULTMTG_REAL_LOG (path to the real Player.log) because that log
// is gitignored raw PII and cannot live in CI. The PR's ## Local Verification
// block is the captured transcript of this test plus the BFF projection /
// ListGamePlaysByMatch leg.
func TestReplay_RealPlayerLog_NonEmptyAnchoredTimeline(t *testing.T) {
	logPath := os.Getenv("VAULTMTG_REAL_LOG")
	if logPath == "" {
		t.Skip("VAULTMTG_REAL_LOG not set — real-log replay runs in Local Verification only (raw PII, not in CI)")
	}

	s, cap := newCapturingService(t, "acct-807-reallog")
	replayLog(t, s, logPath)

	ended := cap.byType("match.game_ended")
	require.NotEmpty(t, ended, "real log must produce at least one match.game_ended")

	// Find any non-partial flush with a non-empty timeline and a real match_id.
	var found bool
	var capturedPayloads []json.RawMessage
	for i := range ended {
		var p contract.GamePlayPayload
		require.NoError(t, json.Unmarshal(ended[i].Payload, &p))
		if !p.Partial && p.MatchID != "" && len(p.CardPlays) > 0 {
			found = true
			capturedPayloads = append(capturedPayloads, ended[i].Payload)
			t.Logf("[real-log] non-partial match.game_ended match_id=%s card_plays=%d turn_count=%d game_number=%d",
				p.MatchID, len(p.CardPlays), p.TurnCount, p.GameNumber)
		}
	}
	require.True(t, found,
		"real Player.log must produce at least one NON-PARTIAL match.game_ended with a real match_id and a non-empty per-turn timeline (#807)")

	// Local Verification bridge: when VAULTMTG_REAL_LOG_OUT is set, write the
	// real daemon-produced non-partial payloads to a file so the BFF projection
	// integration test can ingest the EXACT bytes the daemon emits and prove the
	// full daemon→projection→ListGamePlaysByMatch chain on a real match (#807).
	if out := os.Getenv("VAULTMTG_REAL_LOG_OUT"); out != "" {
		blob, err := json.Marshal(capturedPayloads)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(out, blob, 0o600))
		t.Logf("[real-log] wrote %d non-partial payload(s) to %s", len(capturedPayloads), out)
	}
}
