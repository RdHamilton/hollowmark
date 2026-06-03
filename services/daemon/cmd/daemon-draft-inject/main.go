// Command daemon-draft-inject is the live-draft inject harness for the
// golden-corpus replay (ADR-052, ticket #53).
//
// It reads a corpus Player.log file, extracts draft.pack and draft.pick
// events (plus an opening draft.started), and POSTs them to a running BFF
// ingest endpoint one by one with a configurable delay between packs.
// The BFF broker fans each event over SSE to connected clients, putting the
// SPA's /draft/live view into an active-draft state populated with real pack
// and pick data from the corpus.
//
// This is the "active-draft slice" of the Layer-5 replay harness — it does
// NOT persist events (ingest writes to daemon_events, but the projection worker
// is intentionally not triggered because we want a live SSE state, not a
// completed session in the DB). The inject is purely for Tim / Prof to
// screenshot and review the live pick sequence.
//
// Usage:
//
//	daemon-draft-inject \
//	  -log    <path/to/Player_capture.log>                 \
//	  -bff    <BFF base URL, e.g. https://staging-api.vaultmtg.app> \
//	  -key    <daemon API key for the ci-smoke account>     \
//	  -account <ci-smoke MTGA account ID>                   \
//	  [-delay  <duration between packs, default 200ms>]     \
//	  [-packs  <number of draft.pack events to inject, default all>]
//
// Environment variable overrides (same names as flags, uppercased with
// VAULTMTG_INJECT_ prefix):
//
//	VAULTMTG_INJECT_LOG, VAULTMTG_INJECT_BFF,
//	VAULTMTG_INJECT_KEY, VAULTMTG_INJECT_ACCOUNT
//
// The harness does NOT require a running daemon process — it connects
// directly to the BFF ingest endpoint using a daemon API key.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/RdHamilton/vault-mtg/services/contract"
	"github.com/RdHamilton/vault-mtg/services/daemon/replay"
)

const ingestPath = "/api/v1/ingest/events"

func main() {
	logFlag := flag.String("log", envOrDefault("VAULTMTG_INJECT_LOG", ""), "path to corpus Player.log (required)")
	bffFlag := flag.String("bff", envOrDefault("VAULTMTG_INJECT_BFF", ""), "BFF base URL (required)")
	keyFlag := flag.String("key", envOrDefault("VAULTMTG_INJECT_KEY", ""), "daemon API key for the ci-smoke account (required)")
	accountFlag := flag.String("account", envOrDefault("VAULTMTG_INJECT_ACCOUNT", ""), "ci-smoke MTGA account ID (required)")
	delayFlag := flag.Duration("delay", 200*time.Millisecond, "delay between draft.pack events (governs how fast the pack grid updates)")
	packsFlag := flag.Int("packs", 0, "number of draft.pack events to inject (0 = all)")
	flag.Parse()

	if *logFlag == "" {
		log.Fatal("[draft-inject] -log / VAULTMTG_INJECT_LOG is required")
	}
	if *bffFlag == "" {
		log.Fatal("[draft-inject] -bff / VAULTMTG_INJECT_BFF is required")
	}
	if *keyFlag == "" {
		log.Fatal("[draft-inject] -key / VAULTMTG_INJECT_KEY is required")
	}
	if *accountFlag == "" {
		log.Fatal("[draft-inject] -account / VAULTMTG_INJECT_ACCOUNT is required")
	}

	result, err := replay.ParseLogFile(*logFlag)
	if err != nil {
		log.Fatalf("[draft-inject] parse log: %v", err)
	}

	log.Printf("[draft-inject] parsed %s: %d draft.pack + %d draft.pick events (%d parse errors)",
		*logFlag, result.DraftPackCount, result.DraftPickCount, len(result.ParseErrors))
	for _, pe := range result.ParseErrors {
		log.Printf("[draft-inject] parse error: %s", pe)
	}

	if result.DraftPackCount == 0 {
		log.Fatal("[draft-inject] no draft.pack events found — is this a draft log?")
	}

	// Synthetic session and sequence numbering for this inject run.
	sessionID := "inject-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	var seq uint64

	nextSeq := func() uint64 {
		seq++
		return seq
	}

	client := &http.Client{Timeout: 10 * time.Second}

	// Helper: POST a single event to the BFF ingest endpoint.
	post := func(eventType string, payload interface{}) error {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		ev := contract.DaemonEvent{
			Type:       eventType,
			AccountID:  *accountFlag,
			SessionID:  sessionID,
			OccurredAt: time.Now().UTC(),
			Sequence:   nextSeq(),
			Payload:    json.RawMessage(raw),
		}
		body, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
			*bffFlag+ingestPath, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+*keyFlag)

		resp, err := client.Do(req)
		if err != nil {
			return fmt.Errorf("post: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("BFF returned %d", resp.StatusCode)
		}
		return nil
	}

	// Emit draft.started so the SPA transitions into live-draft mode.
	type draftStartedPayload struct {
		SessionID string `json:"session_id"`
		EventName string `json:"event_name"`
		SetCode   string `json:"set_code"`
		DraftType string `json:"draft_type"`
		Status    string `json:"status"`
	}

	// Derive set_code and event_name from the first draft.pack payload if possible.
	// ParseLogFile does not return typed payloads, so we infer from the first
	// draft.pack event's raw JSON.
	setCode, eventName := inferDraftMeta(result.Events)

	if err := post("draft.started", draftStartedPayload{
		SessionID: sessionID,
		EventName: eventName,
		SetCode:   setCode,
		DraftType: "BotDraft",
		Status:    "in_progress",
	}); err != nil {
		log.Fatalf("[draft-inject] post draft.started: %v", err)
	}
	log.Printf("[draft-inject] emitted draft.started session_id=%s set_code=%s", sessionID, setCode)

	// Inject draft.pack events (and the interleaved draft.pick events that follow
	// each pack) in the order they appear in the log.
	packsSent := 0
	picksSent := 0
	maxPacks := *packsFlag

	// Walk the event list in parse order. For each draft.pack we inject the pack
	// then immediately inject the draft.pick that follows it (if present and its
	// PackNumber / PickNumber coordinates match the pack we just sent).
	for i, ev := range result.Events {
		switch ev.EventType {
		case "draft.pack":
			if maxPacks > 0 && packsSent >= maxPacks {
				goto done
			}
			if err := post("draft.pack", json.RawMessage(ev.Payload)); err != nil {
				log.Printf("[draft-inject] WARN post draft.pack #%d: %v", packsSent+1, err)
			} else {
				packsSent++
				log.Printf("[draft-inject] pack #%d/%d injected", packsSent, result.DraftPackCount)
			}
			// If the next event in the log is a draft.pick, inject it immediately
			// (before sleeping) to mirror how the daemon interleaves pack→pick.
			if i+1 < len(result.Events) && result.Events[i+1].EventType == "draft.pick" {
				if err := post("draft.pick", json.RawMessage(result.Events[i+1].Payload)); err != nil {
					log.Printf("[draft-inject] WARN post draft.pick after pack #%d: %v", packsSent, err)
				} else {
					picksSent++
				}
			}
			time.Sleep(*delayFlag)

		case "draft.pick":
			// Only inject standalone picks (those not immediately following a pack,
			// which were already handled above). A pick is "standalone" when the
			// previous event in the log was NOT a draft.pack.
			if i > 0 && result.Events[i-1].EventType == "draft.pack" {
				// Already injected above.
				continue
			}
			if err := post("draft.pick", json.RawMessage(ev.Payload)); err != nil {
				log.Printf("[draft-inject] WARN post standalone draft.pick: %v", err)
			} else {
				picksSent++
			}
		}
	}

done:
	log.Printf("[draft-inject] done: %d packs + %d picks injected (session_id=%s)",
		packsSent, picksSent, sessionID)
	log.Printf("[draft-inject] open https://<your-app-host>/draft/live to view the active draft")
}

// inferDraftMeta extracts the set_code and event_name from the first
// draft.pack event payload. The payload is a logreader.DraftPackPayload
// encoded as JSON; we unmarshal just the fields we need.
func inferDraftMeta(events []replay.ParsedEvent) (setCode, eventName string) {
	for _, ev := range events {
		if ev.EventType != "draft.pack" {
			continue
		}
		var p struct {
			CourseName string `json:"CourseName"`
			SetCode    string `json:"set_code"`
		}
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			break
		}
		if p.CourseName != "" {
			eventName = p.CourseName
			// CourseName is "QuickDraft_SOS_20260526" — set_code is the second segment.
			setCode = p.SetCode
			if setCode == "" {
				// Derive from CourseName suffix.
				for i := len(p.CourseName) - 1; i >= 0; i-- {
					if p.CourseName[i] == '_' {
						// Last segment before a trailing date (e.g. "_20260526").
						// The actual set code is the middle segment.
						mid := p.CourseName[:i]
						for j := len(mid) - 1; j >= 0; j-- {
							if mid[j] == '_' {
								setCode = mid[j+1:]
								break
							}
						}
						break
					}
				}
			}
		}
		break
	}
	if eventName == "" {
		eventName = "QuickDraft_UNKNOWN"
	}
	if setCode == "" {
		setCode = "???"
	}
	return setCode, eventName
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
