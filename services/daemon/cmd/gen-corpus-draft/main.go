//go:build record

// gen-corpus-draft generates the daemon-emit/draft-completed.json golden-corpus
// fixture by running the real classifier + enricher (buildCoursesCompletedPayload)
// against the committed player-log/draft-complete.log fixture and printing the
// resulting contract.DaemonEvent JSON to stdout.
//
// Run to regenerate (from the repo root):
//
//	GOPRIVATE=github.com/RdHamilton/hollowmark \
//	  go run -tags record ./services/daemon/cmd/gen-corpus-draft/ \
//	  > services/daemon/testdata/corpus/daemon-emit/draft-completed.json
//
// The -tags record guard ensures this binary is NEVER compiled in normal CI or
// go test ./... sweeps. Ray ruling (#1427 plan approval): committed, build-tagged,
// reproducible recorder is strictly better than ad-hoc script provenance.
//
// Provenance: guards #3285 (EventGetCoursesV2 CurrentModule=Complete →
// draft.completed emit). The payload values are REAL-DERIVED — they are the
// actual output of classify.go + buildCoursesCompletedPayload on the committed
// draft-complete.log. Do NOT hand-edit the output file.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/RdHamilton/hollowmark/services/contract"
)

// draftScenePayload mirrors daemon/internal/daemon.draftScenePayload.
// Duplicated here (instead of importing the internal package) so the recorder
// can live in cmd/ without pulling in the full daemon dependency graph.
type draftScenePayload struct {
	SessionID string `json:"session_id"`
	EventName string `json:"event_name"`
	SetCode   string `json:"set_code"`
	DraftType string `json:"draft_type"`
}

// deriveDraftFormatAndSet mirrors daemon/internal/daemon.deriveDraftFormatAndSet.
func deriveDraftFormatAndSet(courseName string) (format, setCode string) {
	parts := strings.Split(courseName, "_")
	if len(parts) < 3 {
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
		return courseName, ""
	}
	setCode = parts[len(parts)-2]
	format = strings.Join(parts[:len(parts)-2], "_")
	return format, setCode
}

// buildCoursesCompletedPayload mirrors daemon/internal/daemon.buildCoursesCompletedPayload
// (no draftstate.Store needed — it is nil in this recorder; we only care about
// the completion payload, not the CourseId registration side-effect).
func buildCoursesCompletedPayload(courses []interface{}) *draftScenePayload {
	for _, item := range courses {
		c, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := c["InternalEventName"].(string)
		courseID, _ := c["CourseId"].(string)
		mod, _ := c["CurrentModule"].(string)

		if !strings.Contains(name, "Draft") {
			continue
		}

		if mod == "Complete" {
			format, setCode := deriveDraftFormatAndSet(name)
			id := courseID
			if id == "" {
				id = name
			}
			return &draftScenePayload{
				SessionID: id,
				EventName: name,
				SetCode:   setCode,
				DraftType: format,
			}
		}
	}
	return nil
}

func main() {
	// Read the committed player-log fixture.
	logPath := "services/daemon/testdata/corpus/player-log/draft-complete.log"
	raw, err := os.ReadFile(logPath)
	if err != nil {
		log.Fatalf("read %s: %v", logPath, err)
	}

	// Parse the log entry JSON.
	var entry map[string]interface{}
	if err := json.Unmarshal(raw, &entry); err != nil {
		log.Fatalf("parse log entry: %v", err)
	}

	// Classify: confirm this entry triggers draft.completed via Path A.
	courses, ok := entry["Courses"].([]interface{})
	if !ok {
		log.Fatalf("log entry has no Courses[] field — this fixture does not trigger Path A (EventGetCoursesV2 draft.completed)")
	}

	// Enrich: derive draftScenePayload.
	payload := buildCoursesCompletedPayload(courses)
	if payload == nil {
		log.Fatalf("buildCoursesCompletedPayload returned nil — no Draft course with CurrentModule=Complete in Courses[]")
	}

	fmt.Fprintf(os.Stderr, "[gen-corpus-draft] REAL-DERIVED values:\n")
	fmt.Fprintf(os.Stderr, "  session_id  = %s\n", payload.SessionID)
	fmt.Fprintf(os.Stderr, "  event_name  = %s\n", payload.EventName)
	fmt.Fprintf(os.Stderr, "  set_code    = %s\n", payload.SetCode)
	fmt.Fprintf(os.Stderr, "  draft_type  = %s\n", payload.DraftType)
	fmt.Fprintf(os.Stderr, "[gen-corpus-draft] Classifier path: Path A (EventGetCoursesV2 CurrentModule=Complete) — authoritative per classify.go:88-100\n")

	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("marshal payload: %v", err)
	}

	// Build the contract.DaemonEvent envelope.
	// Use a deterministic OccurredAt so the fixture is stable across re-runs.
	// Sequence 8 follows pack=6 and pick=7 in the SOS QuickDraftEmblem session.
	occurredAt, _ := time.Parse(time.RFC3339, "2026-06-12T00:05:00Z")
	evt := contract.DaemonEvent{
		Type:       "draft.completed",
		AccountID:  "test-account-001",
		EventID:    "11111111-0000-0000-0000-000000000008",
		SessionID:  "22222222-0000-0000-0000-000000000001",
		Sequence:   8,
		OccurredAt: occurredAt,
		Payload:    json.RawMessage(payloadRaw),
	}

	out, err := json.MarshalIndent(evt, "", "  ")
	if err != nil {
		log.Fatalf("marshal event: %v", err)
	}
	fmt.Println(string(out))
}
