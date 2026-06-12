package projection

// Tests for the ADR-084 read-model notification additions (#1368).
//
// Fitness functions covered:
//   - Coalescing (AC7): N events in one pass → exactly 1 readmodel.updated
//     call per (userID, domain), not N calls.
//   - Read-after-notify (AC6): the notification fires AFTER MarkProjected, so
//     any listener that queries the DB on notification sees the projected row.
//   - Multi-domain fan-out (inventory.updated dirtying inventory+decks+mastery).
//   - Ingest nudge channel is non-blocking (AC4): a full channel never blocks
//     the caller.
//   - Nudge causes runOnce within the ticker interval (AC4): projection runs
//     within ms of ingest, not waiting up to 30 s.

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// --- fakes for notification testing ---

// notifyCall records a single PublishReadModelUpdated invocation.
type notifyCall struct {
	UserID  int64
	Domains []string
	MaxID   int64
}

// fakeNotifier captures calls to PublishReadModelUpdated.
type fakeNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
}

func (f *fakeNotifier) PublishReadModelUpdated(userID int64, domains []string, maxEventID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Copy the domains slice so callers can't mutate it post-call.
	d := make([]string, len(domains))
	copy(d, domains)
	f.calls = append(f.calls, notifyCall{UserID: userID, Domains: d, MaxID: maxEventID})
}

func (f *fakeNotifier) Calls() []notifyCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]notifyCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// --- helpers ---

func makeMatchPayload(t *testing.T) json.RawMessage {
	t.Helper()
	b, _ := json.Marshal(map[string]interface{}{
		"match_id":       "m1",
		"event_id":       "evt1",
		"event_name":     "Standard_BO1",
		"format":         "Standard",
		"result":         "win",
		"player_wins":    2,
		"opponent_wins":  1,
		"player_team_id": 0,
	})
	return b
}

func makeInventoryPayload(t *testing.T, withMastery bool) json.RawMessage {
	t.Helper()
	p := map[string]interface{}{
		"gems": 1000, "gold": 500,
		"total_vault_progress": 0,
		"wildcards_common":     0, "wildcards_uncommon": 0,
		"wildcards_rare": 0, "wildcards_mythic": 0,
	}
	if withMastery {
		p["mastery"] = map[string]interface{}{
			"level": 10, "pass_type": "paid", "max": 80,
		}
	}
	b, _ := json.Marshal(p)
	return b
}

// newWorkerWithNotifier returns a Worker wired with a fakeNotifier.
// It uses the minimal set of stores needed for the tested event types.
func newWorkerWithNotifier(events *fakeEventStore, accounts *fakeAccountStore, n *fakeNotifier) *Worker {
	w := NewWorker(
		events,
		accounts,
		&fakeMatchStore{},
		&fakeDraftStore{},
		&fakeCollectionStore{},
		&fakeInventoryStore{},
		&fakeQuestStore{},
		&fakeDeckStore{},
		&fakeGamePlayStore{},
	)
	w.WithNotifier(n)
	return w
}

// --- tests ---

// TestRunOnce_Coalescing_MatchEvents verifies that 3 match.completed events
// for the same user in one pass produce exactly 1 notification call for the
// "matches" domain (AC7).
func TestRunOnce_Coalescing_MatchEvents(t *testing.T) {
	payload := makeMatchPayload(t)
	now := time.Now()
	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 1, UserID: 7, AccountID: "acct-A", EventType: "match.completed", Payload: payload, OccurredAt: now, ReceivedAt: now},
			{ID: 2, UserID: 7, AccountID: "acct-A", EventType: "match.completed", Payload: payload, OccurredAt: now, ReceivedAt: now.Add(time.Millisecond)},
			{ID: 3, UserID: 7, AccountID: "acct-A", EventType: "match.completed", Payload: payload, OccurredAt: now, ReceivedAt: now.Add(2 * time.Millisecond)},
		},
	}
	accounts := &fakeAccountStore{accountID: 1}
	n := &fakeNotifier{}

	w := newWorkerWithNotifier(events, accounts, n)
	w.RunOnce(context.Background())

	calls := n.Calls()
	matchesCalls := 0
	for _, c := range calls {
		if c.UserID == 7 {
			for _, d := range c.Domains {
				if d == "matches" {
					matchesCalls++
				}
			}
		}
	}
	if matchesCalls != 1 {
		t.Errorf("expected exactly 1 notification for 'matches' domain, got %d (calls: %v)", matchesCalls, calls)
	}
}

// TestRunOnce_Coalescing_MaxID verifies that the notification carries the
// maximum daemon_events.id from the batch (AC3).
func TestRunOnce_Coalescing_MaxID(t *testing.T) {
	payload := makeMatchPayload(t)
	now := time.Now()
	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 10, UserID: 5, AccountID: "acct-B", EventType: "match.completed", Payload: payload, OccurredAt: now, ReceivedAt: now},
			{ID: 25, UserID: 5, AccountID: "acct-B", EventType: "match.completed", Payload: payload, OccurredAt: now, ReceivedAt: now.Add(time.Millisecond)},
		},
	}
	accounts := &fakeAccountStore{accountID: 1}
	n := &fakeNotifier{}

	w := newWorkerWithNotifier(events, accounts, n)
	w.RunOnce(context.Background())

	calls := n.Calls()
	var maxIDSeen int64 = -1
	for _, c := range calls {
		if c.UserID == 5 && c.MaxID > maxIDSeen {
			maxIDSeen = c.MaxID
		}
	}
	if maxIDSeen != 25 {
		t.Errorf("expected max event ID 25 in notification, got %d (calls: %v)", maxIDSeen, calls)
	}
}

// TestRunOnce_InventoryUpdated_MultiDomainFanOut verifies that a single
// inventory.updated event (with mastery data) triggers notifications for
// all three domains: inventory, decks, mastery (ADR-084 domain map).
func TestRunOnce_InventoryUpdated_MultiDomainFanOut(t *testing.T) {
	payload := makeInventoryPayload(t, true /* withMastery */)
	now := time.Now()
	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 99, UserID: 3, AccountID: "acct-C", EventType: "inventory.updated", Payload: payload, OccurredAt: now, ReceivedAt: now},
		},
	}
	accounts := &fakeAccountStore{accountID: 1}
	n := &fakeNotifier{}

	w := newWorkerWithNotifier(events, accounts, n)
	// Wire mastery store so the mastery fan-out fires. fakeMasteryStore is
	// declared in worker_mastery_test.go (same package).
	w.WithMasteryStore(&fakeMasteryStore{})
	w.RunOnce(context.Background())

	calls := n.Calls()

	domainsSeen := map[string]bool{}
	for _, c := range calls {
		if c.UserID == 3 {
			for _, d := range c.Domains {
				domainsSeen[d] = true
			}
		}
	}

	for _, want := range []string{"inventory", "decks", "mastery"} {
		if !domainsSeen[want] {
			t.Errorf("expected domain %q in notifications, not found (calls: %v)", want, calls)
		}
	}
}

// TestRunOnce_ReadAfterNotify_NotifyFiresAfterMarkProjected verifies that
// the notification is only sent AFTER MarkProjected succeeds. This is the
// read-after-notify contract that closes the race (AC6):
//
//	A listener that queries the DB on notification will see the projected row.
//
// We verify this by checking that MarkProjected has been called before the
// notifier fires, using an ordered-recording fake.
func TestRunOnce_ReadAfterNotify_NotifyFiresAfterMarkProjected(t *testing.T) {
	payload := makeMatchPayload(t)
	now := time.Now()

	// orderedEventStore records the sequence of calls: MarkProjected and the
	// notifier both append to the same event log.
	type callEvent struct{ kind string }
	var mu sync.Mutex
	var log []callEvent

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 1, UserID: 2, AccountID: "acct-D", EventType: "match.completed", Payload: payload, OccurredAt: now, ReceivedAt: now},
		},
	}
	// Wrap MarkProjected to record ordering.
	orderedEvents := &orderTrackingEventStore{
		fakeEventStore: events,
		onMarkProjected: func() {
			mu.Lock()
			log = append(log, callEvent{"mark_projected"})
			mu.Unlock()
		},
	}
	accounts := &fakeAccountStore{accountID: 1}

	// Wrap notifier to record ordering.
	orderN := &orderTrackingNotifier{
		onPublish: func() {
			mu.Lock()
			log = append(log, callEvent{"notify"})
			mu.Unlock()
		},
	}

	w := NewWorker(
		orderedEvents,
		accounts,
		&fakeMatchStore{},
		&fakeDraftStore{},
		&fakeCollectionStore{},
		&fakeInventoryStore{},
		&fakeQuestStore{},
		&fakeDeckStore{},
		&fakeGamePlayStore{},
	)
	w.WithNotifier(orderN)
	w.RunOnce(context.Background())

	mu.Lock()
	defer mu.Unlock()

	if len(log) < 2 {
		t.Fatalf("expected at least 2 events (mark_projected + notify), got %d: %v", len(log), log)
	}

	// Find the positions of mark_projected and notify in the log.
	markIdx, notifyIdx := -1, -1
	for i, e := range log {
		switch e.kind {
		case "mark_projected":
			if markIdx == -1 {
				markIdx = i
			}
		case "notify":
			if notifyIdx == -1 {
				notifyIdx = i
			}
		}
	}
	if markIdx == -1 {
		t.Fatal("mark_projected never called")
	}
	if notifyIdx == -1 {
		t.Fatal("notify never called")
	}
	if notifyIdx <= markIdx {
		t.Errorf("notify fired before or at same time as mark_projected: mark_projected@%d notify@%d log=%v", markIdx, notifyIdx, log)
	}
}

// TestIngestNudgeChannel_NonBlocking verifies that calling NudgeProjection
// on a full channel does not block (AC4).
func TestIngestNudgeChannel_NonBlocking(t *testing.T) {
	ch := make(chan struct{}, 1)
	// Fill the channel first.
	ch <- struct{}{}

	// sendNudge must return immediately even when ch is full.
	done := make(chan struct{})
	go func() {
		sendNudge(ch)
		close(done)
	}()

	select {
	case <-done:
		// pass — did not block
	case <-time.After(100 * time.Millisecond):
		t.Error("sendNudge blocked on a full channel")
	}
}

// TestIngestNudge_CausesRunOnceFasterThanTicker verifies that when the nudge
// channel fires, Run processes the pending row well before the 30 s ticker
// would fire (AC4). We use a test-only long ticker so the nudge is the only
// trigger.
func TestIngestNudge_CausesRunOnceFasterThanTicker(t *testing.T) {
	payload := makeMatchPayload(t)
	now := time.Now()
	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 1, UserID: 1, AccountID: "acct-E", EventType: "match.completed", Payload: payload, OccurredAt: now, ReceivedAt: now},
		},
	}
	accounts := &fakeAccountStore{accountID: 1}
	n := &fakeNotifier{}

	nudge := make(chan struct{}, 1)
	w := newWorkerWithNotifier(events, accounts, n)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Run with a very long ticker (30 s) so only the nudge can trigger runOnce.
	go w.RunWithNudge(ctx, nudge)

	// Wait briefly, then nudge.
	time.Sleep(20 * time.Millisecond)
	nudge <- struct{}{}

	// Projection should complete (notification fired) well within 1 second.
	deadline := time.After(1 * time.Second)
	for {
		select {
		case <-deadline:
			t.Error("projection did not run within 1s of nudge")
			return
		default:
			if len(n.Calls()) > 0 {
				return // pass
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// --- ordering test helpers ---

type orderTrackingEventStore struct {
	*fakeEventStore
	onMarkProjected func()
}

func (o *orderTrackingEventStore) MarkProjected(ctx context.Context, id int64) error {
	err := o.fakeEventStore.MarkProjected(ctx, id)
	if err == nil && o.onMarkProjected != nil {
		o.onMarkProjected()
	}
	return err
}

type orderTrackingNotifier struct {
	onPublish func()
}

func (o *orderTrackingNotifier) PublishReadModelUpdated(_ int64, _ []string, _ int64) {
	if o.onPublish != nil {
		o.onPublish()
	}
}

// readModelNotifier is the interface the Worker requires. Defined here so the
// compile-time check below catches interface drift without importing contract.
type readModelNotifier interface {
	PublishReadModelUpdated(userID int64, domains []string, maxEventID int64)
}

// Compile-time check: fakeNotifier must satisfy readModelNotifier.
var _ readModelNotifier = (*fakeNotifier)(nil)

// Compile-time check: orderTrackingNotifier must satisfy readModelNotifier.
var _ readModelNotifier = (*orderTrackingNotifier)(nil)
