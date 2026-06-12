package sse_test

// Tests for the readmodel.updated SSE notification path (ADR-084, #1368).
//
// Fitness functions covered:
//   - Frame format: event name is "readmodel.updated", id: field present
//   - Monotonic-id (AC8): sequential publishes carry strictly increasing id: values
//   - Cross-tenant isolation: readmodel.updated is scoped to the target userID

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/sse"
)

// collectLinesUntil reads from scanner until timeout, returning all non-empty
// lines seen. Must be called after subscribing the scanner goroutine.
func collectLinesUntil(scanner interface {
	Scan() bool
	Text() string
}, timeout time.Duration,
) []string {
	lines := make(chan string, 200)
	go func() {
		for scanner.Scan() {
			l := scanner.Text()
			if l != "" {
				lines <- l
			}
		}
		close(lines)
	}()

	var result []string
	deadline := time.After(timeout)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				return result
			}
			result = append(result, l)
		case <-deadline:
			return result
		}
	}
}

func idFromLines(lines []string) int64 {
	for _, l := range lines {
		if strings.HasPrefix(l, "id: ") {
			v, err := strconv.ParseInt(strings.TrimPrefix(l, "id: "), 10, 64)
			if err == nil {
				return v
			}
		}
	}
	return -1
}

// TestBroker_PublishReadModelUpdated_FrameFormat verifies that
// PublishReadModelUpdated emits an SSE frame with:
//   - "event: readmodel.updated"
//   - "id: <eventID>"
//   - "data: ..." containing the supplied domains
func TestBroker_PublishReadModelUpdated_FrameFormat(t *testing.T) {
	b := sse.New()
	_, scanner, cancel := connectSSE(t, b, stubExtractor(1))
	defer cancel()

	time.Sleep(30 * time.Millisecond)

	b.PublishReadModelUpdated(1, []string{"matches", "decks"}, 42)

	lines := collectLinesUntil(scanner, 2*time.Second)

	foundEvent := false
	for _, l := range lines {
		if l == "event: readmodel.updated" {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Errorf("expected 'event: readmodel.updated' line; got: %v", lines)
	}

	gotID := idFromLines(lines)
	if gotID != 42 {
		t.Errorf("expected id: 42, got %d (lines: %v)", gotID, lines)
	}

	foundData := false
	for _, l := range lines {
		if strings.HasPrefix(l, "data: ") && strings.Contains(l, "matches") {
			foundData = true
		}
	}
	if !foundData {
		t.Errorf("expected data line containing domains; got: %v", lines)
	}
}

// TestBroker_PublishReadModelUpdated_MonotonicID verifies that sequential
// PublishReadModelUpdated calls emit strictly increasing id: values (AC8).
func TestBroker_PublishReadModelUpdated_MonotonicID(t *testing.T) {
	b := sse.New()
	_, scanner, cancel := connectSSE(t, b, stubExtractor(10))
	defer cancel()

	time.Sleep(30 * time.Millisecond)

	b.PublishReadModelUpdated(10, []string{"matches"}, 100)
	b.PublishReadModelUpdated(10, []string{"drafts"}, 200)

	lines := collectLinesUntil(scanner, 2*time.Second)

	var ids []int64
	for _, l := range lines {
		if strings.HasPrefix(l, "id: ") {
			v, err := strconv.ParseInt(strings.TrimPrefix(l, "id: "), 10, 64)
			if err == nil {
				ids = append(ids, v)
			}
		}
	}

	if len(ids) < 2 {
		t.Fatalf("expected at least 2 id: lines, got %d (lines: %v)", len(ids), lines)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Errorf("id: values not strictly increasing at positions %d,%d: %v", i-1, i, ids)
		}
	}
}

// TestBroker_PublishReadModelUpdated_CrossTenantIsolation verifies that
// readmodel.updated frames are NOT delivered to a different user.
func TestBroker_PublishReadModelUpdated_CrossTenantIsolation(t *testing.T) {
	b := sse.New()

	_, _, cancelA := connectSSE(t, b, stubExtractor(1))
	defer cancelA()
	_, scannerB, cancelB := connectSSE(t, b, stubExtractor(2))
	defer cancelB()

	time.Sleep(30 * time.Millisecond)

	// Publish only for user 1.
	b.PublishReadModelUpdated(1, []string{"matches"}, 7)

	lines := collectLinesUntil(scannerB, 300*time.Millisecond)
	for _, l := range lines {
		if l == "event: readmodel.updated" {
			t.Error("user 2 received a readmodel.updated frame scoped to user 1 (cross-tenant leak)")
		}
	}
}
