package logreader

// botdraft.go — BotDraft (QuickDraft / bot-draft) wire format parsers (#337, #1344).
//
// QuickDraft (and other bot drafts) emit a different wire format than Premier
// (#338). The format has two historical shapes:
//
// OLD format (MTGA ≤ 2026.59.x) — doubly-nested: a stringified inner JSON
// envelope with CAPITALIZED keys and STRINGIFIED grpIds, requiring a
// double-unmarshal:
//
//	Pack:  {"CurrentModule":"BotDraft","Payload":"{\"EventName\":\"QuickDraft_SOS_20260526\",
//	         \"PackNumber\":0,\"PickNumber\":0,\"DraftPack\":[\"102470\",...]}"}
//	Pick:  {"id":"<uuid>","request":"{\"EventName\":\"QuickDraft_SOS_20260526\",
//	         \"PickInfo\":{\"EventName\":\"...\",\"CardIds\":[\"102704\"],
//	         \"PackNumber\":0,\"PickNumber\":0}}"}
//
// NEW format (MTGA ≥ 2026.60) — Payload and request are native JSON objects
// (no string-escape wrapping), but grpIds in DraftPack and CardIds remain
// strings:
//
//	Pack:  {"CurrentModule":"BotDraft","Payload":{"EventName":"QuickDraft_SOS_20260526",
//	         "PackNumber":0,"PickNumber":0,"DraftPack":["102470",...]}}
//	Pick:  {"id":"<uuid>","request":{"EventName":"QuickDraft_SOS_20260526",
//	         "PickInfo":{"CardIds":["102704"],"PackNumber":0,"PickNumber":0}}}
//
// Both parsers accept both shapes. The classifiers in classify.go mirror the
// same tolerance. The BFF contract (DraftPackPayload / DraftPickPayload) is
// unchanged — callers are unaffected.

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// botDraftEnvelope is the outer wrapper for a BotDraft status/pack line.
// Payload uses json.RawMessage so it can hold either a JSON string (old format)
// or a JSON object (new format). The parser discriminates at runtime.
type botDraftEnvelope struct {
	CurrentModule string          `json:"CurrentModule"`
	Payload       json.RawMessage `json:"Payload"`
}

// botDraftStatus is the decoded inner Payload of a BotDraft pack line. grpIds
// in DraftPack are strings on the wire in both old and new formats.
type botDraftStatus struct {
	EventName  string   `json:"EventName"`
	PackNumber int      `json:"PackNumber"`
	PickNumber int      `json:"PickNumber"`
	DraftPack  []string `json:"DraftPack"`
}

// botDraftPickRequest is the decoded inner "request" of a BotDraftDraftPick
// line (old format: JSON string; new format: JSON object). The presence of
// PickInfo distinguishes BotDraft from Premier (which carries DraftId/GrpIds).
type botDraftPickRequest struct {
	EventName string            `json:"EventName"`
	PickInfo  *botDraftPickInfo `json:"PickInfo"`
}

// botDraftPickInfo holds the actual pick data. CardIds are strings on the wire.
type botDraftPickInfo struct {
	EventName  string   `json:"EventName"`
	CardIds    []string `json:"CardIds"`
	PackNumber int      `json:"PackNumber"`
	PickNumber int      `json:"PickNumber"`
}

// parseStringGrpIDs converts a slice of stringified grpIds into ints. An empty
// slice yields an empty (non-nil) slice.
func parseStringGrpIDs(ids []string) ([]int, error) {
	out := make([]int, 0, len(ids))
	for _, s := range ids {
		id, err := strconv.Atoi(s)
		if err != nil {
			return nil, fmt.Errorf("parse grpId %q: %w", s, err)
		}
		out = append(out, id)
	}
	return out, nil
}

// decodeBotDraftStatus extracts a botDraftStatus from a Payload RawMessage that
// is either a JSON string (old format, double-nested) or a JSON object (new
// format, single-nested).
func decodeBotDraftStatus(raw json.RawMessage) (botDraftStatus, error) {
	if len(raw) == 0 {
		return botDraftStatus{}, fmt.Errorf("BotDraft envelope missing Payload")
	}
	var status botDraftStatus
	if raw[0] == '"' {
		// Old format: Payload is a JSON-encoded string; unwrap and decode.
		var inner string
		if err := json.Unmarshal(raw, &inner); err != nil {
			return botDraftStatus{}, fmt.Errorf("unwrap BotDraft Payload string: %w", err)
		}
		if inner == "" {
			return botDraftStatus{}, fmt.Errorf("BotDraft envelope empty Payload string")
		}
		if err := json.Unmarshal([]byte(inner), &status); err != nil {
			return botDraftStatus{}, fmt.Errorf("unmarshal BotDraft Payload: %w", err)
		}
	} else {
		// New format: Payload is a JSON object; decode directly.
		if err := json.Unmarshal(raw, &status); err != nil {
			return botDraftStatus{}, fmt.Errorf("unmarshal BotDraft Payload: %w", err)
		}
	}
	return status, nil
}

// ParseBotDraftStatusPack parses a BotDraft pack line
// (CurrentModule=BotDraft + Payload) into the existing DraftPackPayload.
// Both old (Payload-as-string) and new (Payload-as-object) wire shapes are
// accepted. EventName becomes CourseName (the draftstate session key).
// PackNumber/PickNumber are 0-based on the wire; they are reconstructed into
// the cumulative 1-based index the draftstate Store expects:
//
//	cumulative_1based = PackNumber*15 + PickNumber + 1
//
// (Consistent with the Premier formula (SelfPack-1)*15 + SelfPick: BotDraft
// pack=0/pick=0 → 1; Premier SelfPack=1/SelfPick=1 → 1.)
func ParseBotDraftStatusPack(entry *LogEntry) (*DraftPackPayload, error) {
	if entry == nil || !entry.IsJSON {
		return nil, fmt.Errorf("entry is not JSON")
	}

	raw, err := json.Marshal(entry.JSON)
	if err != nil {
		return nil, fmt.Errorf("re-marshal entry JSON: %w", err)
	}

	var env botDraftEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("unmarshal botDraftEnvelope: %w", err)
	}
	if env.CurrentModule != "BotDraft" {
		return nil, fmt.Errorf("entry CurrentModule is %q, not BotDraft", env.CurrentModule)
	}

	status, err := decodeBotDraftStatus(env.Payload)
	if err != nil {
		return nil, err
	}

	cards, err := parseStringGrpIDs(status.DraftPack)
	if err != nil {
		return nil, err
	}

	cumulative := status.PackNumber*15 + status.PickNumber + 1

	return &DraftPackPayload{
		CourseName: status.EventName,
		DraftPack: DraftPackDetail{
			PackCards: cards,
			SelfPick:  cumulative,
		},
	}, nil
}

// decodeBotDraftPickRequest extracts a botDraftPickRequest from the "request"
// value in entry.JSON. Accepts both old format (string, double-nested) and new
// format (object, single-nested).
func decodeBotDraftPickRequest(entry *LogEntry) (botDraftPickRequest, error) {
	reqVal, ok := entry.JSON["request"]
	if !ok || reqVal == nil {
		return botDraftPickRequest{}, fmt.Errorf("entry missing request field")
	}

	var reqBytes []byte
	switch v := reqVal.(type) {
	case string:
		// Old format: request is a JSON-encoded string.
		if v == "" {
			return botDraftPickRequest{}, fmt.Errorf("entry has empty request string")
		}
		reqBytes = []byte(v)
	default:
		// New format: request is a JSON object (map[string]interface{} after
		// JSON unmarshal). Re-marshal it to get canonical bytes for decoding.
		b, err := json.Marshal(v)
		if err != nil {
			return botDraftPickRequest{}, fmt.Errorf("re-marshal request object: %w", err)
		}
		reqBytes = b
	}

	var req botDraftPickRequest
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		return botDraftPickRequest{}, fmt.Errorf("unmarshal BotDraftDraftPick request: %w", err)
	}
	return req, nil
}

// ParseBotDraftPick parses a BotDraftDraftPick request line into the existing
// DraftPickPayload. Both old (request-as-string) and new (request-as-object)
// wire shapes are accepted. PickInfo.CardIds are strings on the wire in both
// formats. PackNumber/PickNumber are 0-based and passed through unchanged.
// EventName becomes CourseName.
//
// The parser is strict: a request without a PickInfo block is rejected — that
// is the Premier (DraftId/GrpIds/Pack/Pick) shape, not BotDraft.
func ParseBotDraftPick(entry *LogEntry) (*DraftPickPayload, error) {
	if entry == nil || !entry.IsJSON {
		return nil, fmt.Errorf("entry is not JSON")
	}

	req, err := decodeBotDraftPickRequest(entry)
	if err != nil {
		return nil, err
	}
	if req.PickInfo == nil {
		return nil, fmt.Errorf("BotDraftDraftPick request missing PickInfo")
	}

	cards, err := parseStringGrpIDs(req.PickInfo.CardIds)
	if err != nil {
		return nil, err
	}

	courseName := req.PickInfo.EventName
	if courseName == "" {
		courseName = req.EventName
	}

	return &DraftPickPayload{
		CourseName:  courseName,
		PickedCards: cards,
		PackNumber:  req.PickInfo.PackNumber,
		PickNumber:  req.PickInfo.PickNumber,
	}, nil
}
