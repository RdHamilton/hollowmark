package daemon

// handleentry_race_test.go — regression guard for #732.
//
// Verifies that handleEntry is safe to call concurrently from two goroutines
// (the Run event-loop goroutine and an HTTP-spawned Replay goroutine).  Before
// the fix, both callers accessed s.lastDeckID and s.lastCollectionHash with no
// synchronization; the race detector would fire when these tests are run with
// -race.
//
// Design: each test calls handleEntry from exactly two goroutines simultaneously
// and relies on -race to catch unsynchronized access.  The tests do NOT assert
// any specific output value — race-freedom is the sole correctness property
// being verified here.  Functional correctness (correct deck-ID attribution,
// correct dedup behaviour) is covered in match_result_wiring_test.go and
// collection_dedup_test.go respectively.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/config"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/logreader"
)

// raceSrv returns a minimal httptest.Server that accepts any POST and returns
// 200 OK.  It is intentionally simple — we only need a reachable BFF so the
// dispatcher does not fail on every call.
func raceSrv(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// raceTestService returns a minimal Service pointed at srv, suitable for
// concurrent handleEntry calls.
func raceTestService(t *testing.T, bffURL string) *Service {
	t.Helper()
	return New(&config.Config{
		CloudAPIURL: bffURL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "race-test-key",
		AccountID:   "race-test-acct",
	})
}

// raceCourseDeckEntry returns a LogEntry that classifies as course.deck_submitted
// and carries the given deckID.  Reuses the same structure as the synthetic
// entry in TestHandleEntry_CourseDeck_AttachesDeckIDToMatchCompleted.
func raceCourseDeckEntry(deckID string) *logreader.LogEntry {
	return &logreader.LogEntry{
		IsJSON: true,
		Raw:    "{}",
		JSON: map[string]interface{}{
			"CourseId":          "bd46df66-ba9d-4dbf-81a5-861ecc483c61",
			"InternalEventName": "Play",
			"CourseDeckSummary": map[string]interface{}{
				"DeckId": deckID,
				"Name":   "Race Test Deck",
			},
			"CourseDeck": map[string]interface{}{
				"MainDeck":  []interface{}{},
				"Sideboard": []interface{}{},
			},
		},
	}
}

// raceMatchCompletedEntry returns a LogEntry that classifies as match.completed
// using the real fixture file, loaded once per test.
func raceMatchCompletedEntry(t *testing.T) *logreader.LogEntry {
	t.Helper()
	r, err := logreader.NewReader(
		realFixtureDir(t) + "/match_completed_win_2026.59.20.log",
	)
	if err != nil {
		t.Fatalf("open match fixture: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	entry, err := r.ReadEntry()
	if err != nil && err != io.EOF {
		t.Fatalf("read match fixture: %v", err)
	}
	return entry
}

// TestHandleEntry_ConcurrentReplayAndRunLoop_LastDeckID verifies that
// concurrent calls to handleEntry from two goroutines (simulating the Run
// event-loop goroutine and an HTTP-spawned Replay goroutine) do not race on
// s.lastDeckID.  Run with -race; without the fix the race detector fires.
func TestHandleEntry_ConcurrentReplayAndRunLoop_LastDeckID(t *testing.T) {
	srv := raceSrv(t)
	svc := raceTestService(t, srv.URL)
	ctx := context.Background()

	deck := raceCourseDeckEntry("aaaaaaaa-0000-0000-0000-000000000001")
	match := raceMatchCompletedEntry(t)

	const iters = 200
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: simulates the Run event-loop goroutine.
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = svc.handleEntry(ctx, deck)
			_ = svc.handleEntry(ctx, match)
		}
	}()

	// Goroutine 2: simulates the HTTP-spawned Replay goroutine.
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = svc.handleEntry(ctx, deck)
			_ = svc.handleEntry(ctx, match)
		}
	}()

	wg.Wait()
}

// TestHandleEntry_ConcurrentReplayAndRunLoop_LastCollectionHash verifies that
// concurrent calls to handleEntry do not race on s.lastCollectionHash.
// Run with -race; without the fix the race detector fires.
func TestHandleEntry_ConcurrentReplayAndRunLoop_LastCollectionHash(t *testing.T) {
	srv := raceSrv(t)
	svc := raceTestService(t, srv.URL)
	ctx := context.Background()

	entry1 := collectionEntryWith(map[int]int{67108: 4, 73778: 4}, false)
	entry2 := collectionEntryWith(map[int]int{67108: 4, 73778: 3}, false)

	const iters = 200
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: simulates the Run event-loop goroutine.
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = svc.handleEntry(ctx, entry1)
			_ = svc.handleEntry(ctx, entry2)
		}
	}()

	// Goroutine 2: simulates the HTTP-spawned Replay goroutine.
	go func() {
		defer wg.Done()
		for i := 0; i < iters; i++ {
			_ = svc.handleEntry(ctx, entry1)
			_ = svc.handleEntry(ctx, entry2)
		}
	}()

	wg.Wait()
}
