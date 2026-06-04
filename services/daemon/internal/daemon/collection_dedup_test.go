package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/daemon/internal/config"
	"github.com/RdHamilton/vault-mtg/services/daemon/internal/logreader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCollectionTestService builds a minimal Service pointed at the given BFF URL,
// suitable for handleEntry collection tests.
func newCollectionTestService(t *testing.T, bffURL string) *Service {
	t.Helper()
	cfg := &config.Config{
		CloudAPIURL: bffURL,
		IngestPath:  "/v1/ingest/events",
		APIKey:      "test-key",
		AccountID:   "test-acct-id",
	}
	return New(cfg)
}

// collectionEntryWith builds a PlayerInventoryGetPlayerCardsV3-shaped LogEntry
// from a map of arena_id -> count, optionally flagging it as a backlog entry.
func collectionEntryWith(cards map[int]int, fromBacklog bool) *logreader.LogEntry {
	jsonMap := make(map[string]interface{}, len(cards))
	for id, count := range cards {
		jsonMap[strconv.Itoa(id)] = float64(count)
	}
	return &logreader.LogEntry{
		IsJSON:      true,
		Raw:         "{}",
		JSON:        jsonMap,
		FromBacklog: fromBacklog,
	}
}

// countingIngest returns a test server that counts collection.updated dispatches.
// It handles both single-event and batch (ADR-053 array) payloads, counting
// each collection.updated event individually.
func countingIngest(t *testing.T, collectionCount *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// Try batch first.
		var batch []struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(body, &batch); err == nil {
			for _, e := range batch {
				if e.Type == "collection.updated" {
					atomic.AddInt32(collectionCount, 1)
				}
			}
		} else if strings.Contains(string(body), `"collection.updated"`) {
			// Legacy single-event fallback.
			atomic.AddInt32(collectionCount, 1)
		}
		w.WriteHeader(http.StatusOK)
	}))
}

// TestCollectionDedup_UnchangedSnapshotNotRedispatched verifies the dedup guard:
// dispatching the same collection snapshot N times results in exactly ONE
// dispatch. This reproduces the rc3 idle storm (Arena re-writing an identical
// GetPlayerCardsV3 line ~1-2/sec).
func TestCollectionDedup_UnchangedSnapshotNotRedispatched(t *testing.T) {
	var count int32
	srv := countingIngest(t, &count)
	defer srv.Close()

	svc := newCollectionTestService(t, srv.URL)

	cards := map[int]int{67108: 4, 73778: 4, 79426: 1}
	for i := 0; i < 50; i++ {
		entry := collectionEntryWith(cards, false)
		require.NoError(t, svc.handleEntry(context.Background(), entry))
	}

	// Flush the batch buffer so the single queued event reaches the BFF before
	// the assertion fires (event dispatch is now async via BatchBuffer, ADR-053).
	svc.batchBuffer.FlushNow()
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, int32(1), atomic.LoadInt32(&count),
		"50 identical collection snapshots must dispatch exactly once")
}

// TestCollectionDedup_RealChangeRedispatched verifies a genuine collection
// change (a new card / different count) DOES re-dispatch.
func TestCollectionDedup_RealChangeRedispatched(t *testing.T) {
	var count int32
	srv := countingIngest(t, &count)
	defer srv.Close()

	svc := newCollectionTestService(t, srv.URL)

	require.NoError(t, svc.handleEntry(context.Background(),
		collectionEntryWith(map[int]int{67108: 4, 73778: 4}, false)))
	// Identical — should be suppressed.
	require.NoError(t, svc.handleEntry(context.Background(),
		collectionEntryWith(map[int]int{67108: 4, 73778: 4}, false)))
	// Count change on an existing card — real change.
	require.NoError(t, svc.handleEntry(context.Background(),
		collectionEntryWith(map[int]int{67108: 4, 73778: 3}, false)))
	// New card added — real change.
	require.NoError(t, svc.handleEntry(context.Background(),
		collectionEntryWith(map[int]int{67108: 4, 73778: 3, 79426: 1}, false)))

	// Flush and wait for async batch dispatch to complete.
	svc.batchBuffer.FlushNow()
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, int32(3), atomic.LoadInt32(&count),
		"three distinct snapshots (1st + 2 real changes) must dispatch three times")
}

// TestCollectionStormReplay_BoundedBacklog reproduces the (re)install startup
// flood: the daemon replays the entire historical Player.log (ReadFromStart) and
// every historical GetPlayerCardsV3 snapshot plus every incidental {} line
// arrives as a backlog entry. The fix must bound this to at most ONE dispatch.
func TestCollectionStormReplay_BoundedBacklog(t *testing.T) {
	var count int32
	srv := countingIngest(t, &count)
	defer srv.Close()

	svc := newCollectionTestService(t, srv.URL)

	// 200 historical backlog snapshots, most identical with a few real changes
	// interleaved, exactly as a long-running player's log would contain.
	base := map[int]int{67108: 4, 73778: 4, 79426: 1}
	for i := 0; i < 200; i++ {
		cards := make(map[int]int, len(base))
		for k, v := range base {
			cards[k] = v
		}
		// Grow the collection a few times across the backlog.
		if i == 50 {
			base[80000] = 1
		}
		if i == 120 {
			base[80001] = 2
		}
		require.NoError(t, svc.handleEntry(context.Background(),
			collectionEntryWith(base, true)))
		_ = cards
	}

	assert.LessOrEqual(t, atomic.LoadInt32(&count), int32(1),
		"a (re)install backlog replay must dispatch at most one collection.updated")
}

// TestCollectionStartupCoalesce_DispatchesLatestBacklogThenLive verifies that
// after the bounded backlog flushes its single (latest) snapshot, a subsequent
// LIVE change still dispatches normally.
func TestCollectionStartupCoalesce_DispatchesLatestBacklogThenLive(t *testing.T) {
	var count int32
	srv := countingIngest(t, &count)
	defer srv.Close()

	svc := newCollectionTestService(t, srv.URL)

	// Backlog: two snapshots, the latest is the "real" current collection.
	require.NoError(t, svc.handleEntry(context.Background(),
		collectionEntryWith(map[int]int{67108: 4}, true)))
	require.NoError(t, svc.handleEntry(context.Background(),
		collectionEntryWith(map[int]int{67108: 4, 73778: 2}, true)))

	// First live entry that is IDENTICAL to the last backlog snapshot must NOT
	// re-dispatch (dedup carries the coalesced backlog hash forward).
	require.NoError(t, svc.handleEntry(context.Background(),
		collectionEntryWith(map[int]int{67108: 4, 73778: 2}, false)))

	// A genuine live change after startup must dispatch.
	require.NoError(t, svc.handleEntry(context.Background(),
		collectionEntryWith(map[int]int{67108: 4, 73778: 2, 79426: 1}, false)))

	// Flush and wait for async batch dispatch to complete.
	svc.batchBuffer.FlushNow()
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, int32(2), atomic.LoadInt32(&count),
		"expect one coalesced-backlog dispatch + one live-change dispatch")
}
