package handlers

import (
	"testing"
)

func TestGapDetector_FirstEvent_NoGap(t *testing.T) {
	d := &GapDetector{}

	isGap, expected := d.Check("acct_1", "sess_1", 1)

	if isGap {
		t.Errorf("first event should not be a gap, got isGap=true expected=%d", expected)
	}
}

func TestGapDetector_Sequential_NoGap(t *testing.T) {
	d := &GapDetector{}

	// Seed the first event.
	d.Check("acct_1", "sess_1", 1)

	// Next sequential event.
	isGap, expected := d.Check("acct_1", "sess_1", 2)

	if isGap {
		t.Errorf("sequential event should not be a gap, got isGap=true expected=%d", expected)
	}

	// Continue the sequence.
	for seq := uint64(3); seq <= 10; seq++ {
		isGap, _ = d.Check("acct_1", "sess_1", seq)
		if isGap {
			t.Errorf("sequence %d: expected no gap for sequential events", seq)
		}
	}
}

func TestGapDetector_Gap_Detected(t *testing.T) {
	d := &GapDetector{}

	// Seed with seq=1.
	d.Check("acct_1", "sess_1", 1)

	// Jump to seq=5 — gap of 3 events (expected 2, 3, 4 missing).
	isGap, expected := d.Check("acct_1", "sess_1", 5)

	if !isGap {
		t.Fatalf("expected gap to be detected (seq 1→5), got isGap=false")
	}

	if expected != 2 {
		t.Errorf("expected expected_sequence=2, got %d", expected)
	}
}

func TestGapDetector_Gap_SingleStep(t *testing.T) {
	d := &GapDetector{}

	// Seed seq=3.
	d.Check("acct_1", "sess_1", 3)

	// Jump by exactly 2: expected=4, received=5.
	isGap, expected := d.Check("acct_1", "sess_1", 5)

	if !isGap {
		t.Fatal("expected gap (3→5), got isGap=false")
	}

	if expected != 4 {
		t.Errorf("expected expected_sequence=4, got %d", expected)
	}
}

func TestGapDetector_SequenceReset_NotAGap(t *testing.T) {
	d := &GapDetector{}

	// Build up to seq=50.
	d.Check("acct_reset", "sess_reset", 50)

	// Daemon restarted — seq resets to 1.
	isGap, _ := d.Check("acct_reset", "sess_reset", 1)

	if isGap {
		t.Error("sequence reset should not be treated as a gap")
	}
}

func TestGapDetector_SequenceReset_NewBaselineForSubsequentCheck(t *testing.T) {
	d := &GapDetector{}

	d.Check("acct_r2", "sess_r2", 100)
	// Reset.
	d.Check("acct_r2", "sess_r2", 1)
	// Sequential after reset.
	isGap, _ := d.Check("acct_r2", "sess_r2", 2)

	if isGap {
		t.Error("sequential event after reset should not be a gap")
	}
}

func TestGapDetector_DifferentSessions_Independent(t *testing.T) {
	d := &GapDetector{}

	d.Check("acct_1", "sess_A", 1)
	d.Check("acct_1", "sess_B", 1)

	// sess_A advances normally.
	isGap, _ := d.Check("acct_1", "sess_A", 2)
	if isGap {
		t.Error("sess_A sequential: unexpected gap")
	}

	// sess_B jumps — gap in sess_B only.
	isGap, expected := d.Check("acct_1", "sess_B", 10)
	if !isGap {
		t.Error("sess_B: expected gap detected")
	}

	if expected != 2 {
		t.Errorf("sess_B: expected expected_sequence=2, got %d", expected)
	}
}

func TestGapDetector_DifferentAccounts_Independent(t *testing.T) {
	d := &GapDetector{}

	d.Check("acct_X", "sess_1", 5)
	d.Check("acct_Y", "sess_1", 5)

	// acct_X gap.
	isGapX, expectedX := d.Check("acct_X", "sess_1", 10)
	// acct_Y sequential.
	isGapY, _ := d.Check("acct_Y", "sess_1", 6)

	if !isGapX {
		t.Error("acct_X: expected gap")
	}

	if expectedX != 6 {
		t.Errorf("acct_X: expected expected_sequence=6, got %d", expectedX)
	}

	if isGapY {
		t.Error("acct_Y: unexpected gap on sequential event")
	}
}

func TestGapDetector_Duplicate_NotAGap(t *testing.T) {
	d := &GapDetector{}

	d.Check("acct_dup", "sess_dup", 5)

	// Same sequence again — not a gap.
	isGap, _ := d.Check("acct_dup", "sess_dup", 5)
	if isGap {
		t.Error("duplicate sequence should not be treated as a gap")
	}
}
