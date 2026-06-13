package draftstate_test

import (
	"sync"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/draftstate"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/logreader"
)

// fixedClock returns a deterministic time.Now so synthetic session IDs
// in tests are predictable.
func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestHandlePack_CreatesNewSessionOnFirstPack(t *testing.T) {
	s := draftstate.New()
	s.SetClock(fixedClock(time.Date(2026, 5, 12, 1, 2, 3, 0, time.UTC)))

	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "PremierDraft_BLB",
		DraftPack: logreader.DraftPackDetail{
			PackCards: []int{100, 200, 300},
			SelfPick:  1, // 1-based; first pick of pack 1
		},
	})

	sess, ok := s.Get("current")
	if !ok {
		t.Fatal("expected a current session after HandlePack")
	}
	if sess.CourseName != "PremierDraft_BLB" {
		t.Errorf("CourseName = %q", sess.CourseName)
	}
	if sess.SetCode != "BLB" {
		t.Errorf("SetCode = %q, want BLB", sess.SetCode)
	}
	if sess.Format != "PremierDraft" {
		t.Errorf("Format = %q, want PremierDraft", sess.Format)
	}
	if sess.CurrentPack != 0 || sess.CurrentPick != 0 {
		t.Errorf("CurrentPack/Pick = %d/%d, want 0/0", sess.CurrentPack, sess.CurrentPick)
	}
	if len(sess.CurrentCards) != 3 || sess.CurrentCards[0] != 100 {
		t.Errorf("CurrentCards = %v", sess.CurrentCards)
	}
}

func TestHandlePack_UpdatesExistingSessionOnSubsequentPicks(t *testing.T) {
	s := draftstate.New()
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "PremierDraft_BLB",
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{1, 2, 3}, SelfPick: 1},
	})
	firstSession, _ := s.Get("current")
	firstID := firstSession.ID

	// Pick 5 in pack 1 — should NOT mint a new session.
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "PremierDraft_BLB",
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{4, 5, 6}, SelfPick: 5},
	})
	sess, _ := s.Get("current")
	if sess.ID != firstID {
		t.Errorf("session ID changed: %q -> %q", firstID, sess.ID)
	}
	if sess.CurrentPick != 4 { // SelfPick 5 (1-based) → CurrentPick 4 within pack 0
		t.Errorf("CurrentPick = %d, want 4", sess.CurrentPick)
	}
}

func TestHandlePack_PackNumberDerivedFromCumulativePick(t *testing.T) {
	s := draftstate.New()
	// SelfPick 16 (1-based) is the first pick of pack 2 (15 picks per pack).
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "PremierDraft_BLB",
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{1}, SelfPick: 16},
	})
	sess, _ := s.Get("current")
	if sess.CurrentPack != 1 {
		t.Errorf("CurrentPack = %d, want 1 (pack 2)", sess.CurrentPack)
	}
	if sess.CurrentPick != 0 {
		t.Errorf("CurrentPick = %d, want 0", sess.CurrentPick)
	}
}

func TestHandlePick_AttachesPackCardsWhenAligned(t *testing.T) {
	s := draftstate.New()
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "PremierDraft_BLB",
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{100, 200, 300}, SelfPick: 1},
	})
	s.HandlePick(&logreader.DraftPickPayload{
		CourseName:  "PremierDraft_BLB",
		PickedCards: []int{200},
		PackNumber:  0,
		PickNumber:  0,
	})

	sess, _ := s.Get("current")
	if len(sess.Picks) != 1 {
		t.Fatalf("Picks len = %d, want 1", len(sess.Picks))
	}
	got := sess.Picks[0]
	if got.Picked != 200 {
		t.Errorf("Picked = %d, want 200", got.Picked)
	}
	if len(got.PackCards) != 3 {
		t.Errorf("PackCards not attached: %v", got.PackCards)
	}
	// CurrentCards cleared after pick lands.
	if len(sess.CurrentCards) != 0 {
		t.Errorf("CurrentCards = %v, want cleared", sess.CurrentCards)
	}
}

// TestPremierSessionKeyedByDraftID verifies that when CourseName is empty
// (the Premier case — Draft.Notify carries no CourseName), a pack and a pick
// sharing the same DraftID correlate to ONE session keyed by that DraftID.
// Without the sessionKey() fallback the pick would not find the pack's session.
func TestPremierSessionKeyedByDraftID(t *testing.T) {
	s := draftstate.New()
	const draftID = "62a14a91-bb89-470a-a7c0-6ad8d7ddf227"

	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "",
		DraftID:    draftID,
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{100, 200, 300}, SelfPick: 1},
	})
	s.HandlePick(&logreader.DraftPickPayload{
		CourseName:  "",
		DraftID:     draftID,
		PickedCards: []int{200},
		PackNumber:  0,
		PickNumber:  0,
	})

	if got := len(s.Sessions()); got != 1 {
		t.Fatalf("expected exactly 1 session keyed by draftId, got %d", got)
	}
	sess, ok := s.Get("current")
	if !ok {
		t.Fatal("expected a current session")
	}
	if sess.CourseName != draftID {
		t.Errorf("session key = %q, want draftId %q", sess.CourseName, draftID)
	}
	if len(sess.Picks) != 1 || sess.Picks[0].Picked != 200 {
		t.Errorf("pick not attached to draftId-keyed session: %+v", sess.Picks)
	}
	// PackCards attached because pick aligns with the in-flight pack.
	if len(sess.Picks[0].PackCards) != 3 {
		t.Errorf("PackCards not attached: %v", sess.Picks[0].PackCards)
	}
}

func TestHandlePick_RecordsEvenWithoutPrecedingPack(t *testing.T) {
	s := draftstate.New()
	s.HandlePick(&logreader.DraftPickPayload{
		CourseName:  "PremierDraft_BLB",
		PickedCards: []int{42},
		PackNumber:  0,
		PickNumber:  0,
	})
	sess, ok := s.Get("current")
	if !ok {
		t.Fatal("expected a session even when pick arrives without a pack")
	}
	if len(sess.Picks) != 1 || sess.Picks[0].Picked != 42 {
		t.Errorf("pick not recorded: %+v", sess.Picks)
	}
}

func TestHandlePick_NilPayloadIsNoOp(t *testing.T) {
	s := draftstate.New()
	s.HandlePick(nil)
	s.HandlePick(&logreader.DraftPickPayload{CourseName: "X", PickedCards: nil})
	if len(s.Sessions()) != 0 {
		t.Errorf("expected no sessions, got %v", s.Sessions())
	}
}

func TestGet_FallsBackToCurrentForUnknownID(t *testing.T) {
	s := draftstate.New()
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "PremierDraft_BLB",
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{1}, SelfPick: 1},
	})
	// Unknown ID — should fall back to the most-recently-touched session
	// so a SPA passing a BFF-issued sessionID still sees live state.
	sess, ok := s.Get("bff-issued-id-the-daemon-doesnt-know")
	if !ok {
		t.Fatal("expected fallback to current session for unknown ID")
	}
	if sess.CourseName != "PremierDraft_BLB" {
		t.Errorf("CourseName = %q", sess.CourseName)
	}
}

func TestGet_ReturnsFalseWhenNoSessions(t *testing.T) {
	s := draftstate.New()
	if _, ok := s.Get("current"); ok {
		t.Error("expected false for empty store")
	}
	if _, ok := s.Get("anything"); ok {
		t.Error("expected false for empty store")
	}
}

func TestGet_DeepCopiesSessionToProtectInternalState(t *testing.T) {
	s := draftstate.New()
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "PremierDraft_BLB",
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{1, 2, 3}, SelfPick: 1},
	})
	sess, _ := s.Get("current")
	// Mutate the returned copy — original must not change.
	sess.CurrentCards[0] = 9999
	again, _ := s.Get("current")
	if again.CurrentCards[0] == 9999 {
		t.Error("returned session shares slice memory with Store internal state")
	}
}

func TestSetCodeFallback_CourseWithoutUnderscore(t *testing.T) {
	s := draftstate.New()
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "Sealed",
		DraftPack:  logreader.DraftPackDetail{SelfPick: 1},
	})
	sess, _ := s.Get("current")
	if sess.SetCode != "" || sess.Format != "Sealed" {
		t.Errorf("unexpected split: Format=%q SetCode=%q", sess.Format, sess.SetCode)
	}
}

// ---------------------------------------------------------------------------
// Emblem draft-type family — splitCourse robustness (#1418 Defect A)
// ---------------------------------------------------------------------------
//
// MTGA 2026.61+ introduced Emblem-variant drafts. The CourseName has THREE
// underscore-separated segments: <FormatPrefix>_<SetCode>_<YYYYMMDD>.
//
//   Example: "QuickDraftEmblem_SOS_20260611"
//
// The existing splitCourse implementation does strings.LastIndex(course,"_")
// which extracts the DATE segment ("20260611") as SetCode instead of the
// actual 2–4 letter set code ("SOS"). This causes every Emblem draft session
// to carry a garbage SetCode and an incorrect Format, breaking the draft
// advisor, set-code-scoped ratings lookup, and SPA display.
//
// The fix: scan segments right-to-left; the first ALL-ALPHA segment is the
// set code; everything to its left is the format prefix.

// TestSplitCourse_QuickDraftEmblem_ThreeSegment is the headline regression
// test from the incident. A QuickDraftEmblem CourseName with the shape
// <Format>_<SetCode>_<YYYYMMDD> must parse to Format="QuickDraftEmblem" and
// SetCode="SOS" (not "20260611").
func TestSplitCourse_QuickDraftEmblem_ThreeSegment(t *testing.T) {
	s := draftstate.New()
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "QuickDraftEmblem_SOS_20260611",
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{102470}, SelfPick: 1},
	})
	sess, ok := s.Get("current")
	if !ok {
		t.Fatal("expected a current session after HandlePack")
	}
	if sess.Format != "QuickDraftEmblem" {
		t.Errorf("Format = %q, want QuickDraftEmblem", sess.Format)
	}
	if sess.SetCode != "SOS" {
		t.Errorf("SetCode = %q, want SOS (not the date segment)", sess.SetCode)
	}
}

// TestSplitCourse_PremierDraftEmblem_ThreeSegment verifies the broader Emblem
// family: PremierDraftEmblem also has the three-segment shape.
func TestSplitCourse_PremierDraftEmblem_ThreeSegment(t *testing.T) {
	s := draftstate.New()
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "PremierDraftEmblem_SOS_20260611",
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{100}, SelfPick: 1},
	})
	sess, ok := s.Get("current")
	if !ok {
		t.Fatal("expected a current session after HandlePack")
	}
	if sess.Format != "PremierDraftEmblem" {
		t.Errorf("Format = %q, want PremierDraftEmblem", sess.Format)
	}
	if sess.SetCode != "SOS" {
		t.Errorf("SetCode = %q, want SOS", sess.SetCode)
	}
}

// TestSplitCourse_QuickDraft_TwoSegment_NotRegressed verifies the standard
// two-segment QuickDraft form still parses correctly after the Emblem fix.
// "QuickDraft_SOS_20260526" is also three-segment — it must yield Format="QuickDraft"
// and SetCode="SOS".
func TestSplitCourse_QuickDraft_TwoSegment_NotRegressed(t *testing.T) {
	s := draftstate.New()
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "QuickDraft_SOS_20260526",
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{100}, SelfPick: 1},
	})
	sess, ok := s.Get("current")
	if !ok {
		t.Fatal("expected a current session after HandlePack")
	}
	if sess.Format != "QuickDraft" {
		t.Errorf("Format = %q, want QuickDraft", sess.Format)
	}
	if sess.SetCode != "SOS" {
		t.Errorf("SetCode = %q, want SOS", sess.SetCode)
	}
}

// TestSplitCourse_PremierDraft_TwoSegment_NotRegressed verifies the canonical
// two-segment PremierDraft form is unaffected.
func TestSplitCourse_PremierDraft_TwoSegment_NotRegressed(t *testing.T) {
	s := draftstate.New()
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "PremierDraft_BLB",
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{100}, SelfPick: 1},
	})
	sess, ok := s.Get("current")
	if !ok {
		t.Fatal("expected a current session after HandlePack")
	}
	if sess.Format != "PremierDraft" {
		t.Errorf("Format = %q, want PremierDraft", sess.Format)
	}
	if sess.SetCode != "BLB" {
		t.Errorf("SetCode = %q, want BLB", sess.SetCode)
	}
}

// TestSplitCourse_FutureEmblemVariant_ArbitrarySetCode verifies that the
// fix is not SOS-specific; any all-alpha set code in position N-2 is
// recognised correctly.
func TestSplitCourse_FutureEmblemVariant_ArbitrarySetCode(t *testing.T) {
	s := draftstate.New()
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: "QuickDraftEmblem_FDN_20261201",
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{100}, SelfPick: 1},
	})
	sess, ok := s.Get("current")
	if !ok {
		t.Fatal("expected a current session after HandlePack")
	}
	if sess.Format != "QuickDraftEmblem" {
		t.Errorf("Format = %q, want QuickDraftEmblem", sess.Format)
	}
	if sess.SetCode != "FDN" {
		t.Errorf("SetCode = %q, want FDN", sess.SetCode)
	}
}

func TestConcurrentReadsAndWritesAreSafe(t *testing.T) {
	s := draftstate.New()
	var wg sync.WaitGroup

	// Two writers + two readers contending. The -race detector will
	// surface any unsynchronised access.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				s.HandlePack(&logreader.DraftPackPayload{
					CourseName: "PremierDraft_BLB",
					DraftPack:  logreader.DraftPackDetail{PackCards: []int{i, j}, SelfPick: j + 1},
				})
			}
		}(i)
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, _ = s.Get("current")
				_ = s.Sessions()
			}
		}()
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// HandleCourseId — stable session identity (#1422)
// ---------------------------------------------------------------------------

// TestHandleCourseId_UsedBySubsequentHandlePack verifies that when
// HandleCourseId is called before the first draft.pack, the new session's ID
// is the stable CourseId GUID rather than a regenerating timestamp.
//
// Pre-fix behaviour: ID = "QuickDraftEmblem_SOS_20260611:<timestamp>"
// Post-fix behaviour: ID = "56c6eed8-bec8-4f4c-a8b5-b8beeb94ea1e"
func TestHandleCourseId_UsedBySubsequentHandlePack(t *testing.T) {
	const courseID = "56c6eed8-bec8-4f4c-a8b5-b8beeb94ea1e"
	const courseName = "QuickDraftEmblem_SOS_20260611"

	s := draftstate.New()
	s.SetClock(fixedClock(time.Date(2026, 6, 11, 1, 9, 50, 0, time.UTC)))

	// Register the stable GUID before any pack event (simulates EventGetCoursesV2
	// firing before the player sees their first pack).
	s.HandleCourseId(courseID, courseName)

	// First pack event.
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: courseName,
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{1001, 1002}, SelfPick: 1},
	})

	sess, ok := s.Get("current")
	if !ok {
		t.Fatal("expected a current session")
	}
	if sess.ID != courseID {
		t.Errorf("session ID = %q, want stable GUID %q", sess.ID, courseID)
	}
}

// TestHandleCourseId_RekeyExistingSession verifies that when HandleCourseId
// is called AFTER a HandlePack (i.e. the pack arrived before the Courses
// response), the existing session is rekeyed to the stable GUID.
func TestHandleCourseId_RekeyExistingSession(t *testing.T) {
	const courseID = "56c6eed8-bec8-4f4c-a8b5-b8beeb94ea1e"
	const courseName = "QuickDraftEmblem_SOS_20260611"

	s := draftstate.New()
	s.SetClock(fixedClock(time.Date(2026, 6, 11, 1, 9, 50, 0, time.UTC)))

	// Pack arrives first — session is minted with a timestamp ID.
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: courseName,
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{1001, 1002}, SelfPick: 1},
	})
	before, _ := s.Get("current")
	if before.ID == courseID {
		t.Skip("ID was already the GUID before rekeying — precondition not met")
	}

	// Courses response arrives — rekeying should happen.
	s.HandleCourseId(courseID, courseName)

	after, ok := s.Get("current")
	if !ok {
		t.Fatal("expected a current session after rekeying")
	}
	if after.ID != courseID {
		t.Errorf("session ID after rekey = %q, want stable GUID %q", after.ID, courseID)
	}
}

// TestHandleCourseId_TwiceDraftStartedExactlyOneSession is the #1423 cut-criteria
// test: a draft.started event re-emitted after daemon restart (simulated by
// calling HandleCourseId + HandlePack twice) must result in exactly ONE
// session in the store, not two duplicate rows.
//
// This is the exact scenario that caused three `draft_sessions` rows in prod
// for one real SOS draft (account_id=7, ids differing only by timestamp).
func TestHandleCourseId_TwiceDraftStartedExactlyOneSession(t *testing.T) {
	const courseID = "56c6eed8-bec8-4f4c-a8b5-b8beeb94ea1e"
	const courseName = "QuickDraftEmblem_SOS_20260611"

	s := draftstate.New()
	tick := time.Date(2026, 6, 11, 1, 9, 50, 0, time.UTC)
	s.SetClock(func() time.Time {
		tick = tick.Add(time.Minute) // advance time so naive timestamps differ
		return tick
	})

	// First daemon session: Courses response registers the GUID, then pack 1.
	s.HandleCourseId(courseID, courseName)
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: courseName,
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{1}, SelfPick: 1},
	})

	// Daemon restart: Courses response fires again, then same pack 1 replayed.
	s.HandleCourseId(courseID, courseName)
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: courseName,
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{1}, SelfPick: 1},
	})

	sessions := s.Sessions()
	if len(sessions) != 1 {
		t.Errorf("#1423 restart-dedup: want exactly 1 session, got %d (phantom duplicate)", len(sessions))
	}
	if len(sessions) >= 1 && sessions[0].ID != courseID {
		t.Errorf("surviving session ID = %q, want GUID %q", sessions[0].ID, courseID)
	}
}

// TestHandleCourseId_RestartDedup verifies the restart-duplicate scenario:
// two back-to-back HandlePack calls with the same CourseName (simulating a
// daemon restart) map to the SAME session when HandleCourseId has been called
// with a stable GUID.  Before the fix, each call produced a distinct
// timestamp-based ID.
func TestHandleCourseId_RestartDedup(t *testing.T) {
	const courseID = "56c6eed8-bec8-4f4c-a8b5-b8beeb94ea1e"
	const courseName = "QuickDraftEmblem_SOS_20260611"

	s := draftstate.New()
	t0 := time.Date(2026, 6, 11, 1, 9, 50, 0, time.UTC)
	tick := t0
	s.SetClock(func() time.Time {
		tick = tick.Add(time.Second)
		return tick
	})

	// Register stable GUID.
	s.HandleCourseId(courseID, courseName)

	// First "session open" (e.g. log replay from before restart).
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: courseName,
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{1}, SelfPick: 1},
	})
	first, _ := s.Get("current")
	if first.ID != courseID {
		t.Fatalf("first session ID = %q, want %q", first.ID, courseID)
	}

	// Simulate daemon restart: HandleCourseId fires again for the same course.
	s.HandleCourseId(courseID, courseName)

	// Another pack event — must return the SAME session, not a duplicate.
	s.HandlePack(&logreader.DraftPackPayload{
		CourseName: courseName,
		DraftPack:  logreader.DraftPackDetail{PackCards: []int{2}, SelfPick: 2},
	})
	second, _ := s.Get("current")
	if second.ID != courseID {
		t.Fatalf("second session ID = %q, want %q", second.ID, courseID)
	}

	// Only ONE session should exist in the store.
	sessions := s.Sessions()
	if len(sessions) != 1 {
		t.Errorf("want 1 session after restart-dedup, got %d", len(sessions))
	}
}
