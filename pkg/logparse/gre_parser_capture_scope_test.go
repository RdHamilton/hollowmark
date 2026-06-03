package logparse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// This file tests the capture-scope exclusions added for ticket #814
// (ADR-046): (1) unresolvable-instance plays (grpID==0 / hidden-zone dest) and
// (2) end-of-game library mill (category=="Mill" sourced from a Library zone).
//
// The synthetic fixtures below carry NO player PII — all grpIds, instanceIds,
// and zone ids are fabricated to exercise the parser code paths. The real-log
// regression (TestCaptureScope_RealPlayerLog) reads the gitignored, PII-bearing
// repo-root Player.log only when present and is skipped in CI.

// buildGREEntry wraps a gameStateMessage in the greToClientEvent envelope the
// parser expects and returns it as a single JSON LogEntry.
func buildGREEntry(t *testing.T, gameStateMessage map[string]interface{}) *LogEntry {
	t.Helper()
	envelope := map[string]interface{}{
		"greToClientEvent": map[string]interface{}{
			"greToClientMessages": []interface{}{
				map[string]interface{}{
					"type":             "GREMessageType_GameStateMessage",
					"gameStateMessage": gameStateMessage,
				},
			},
		},
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal synthetic GRE entry: %v", err)
	}
	return ParseLine(string(raw))
}

// libraryZones / battlefieldZones / graveyardZones are the verified MTGA
// zone-type ids used in the real Player.log (see ADR-046 capture-scope notes).
func captureScopeZones() []interface{} {
	return []interface{}{
		map[string]interface{}{"zoneId": float64(28), "type": "ZoneType_Battlefield"},
		map[string]interface{}{"zoneId": float64(31), "type": "ZoneType_Hand"},
		map[string]interface{}{"zoneId": float64(32), "type": "ZoneType_Library"},
		map[string]interface{}{"zoneId": float64(33), "type": "ZoneType_Graveyard"},
		map[string]interface{}{"zoneId": float64(35), "type": "ZoneType_Hand"},
		map[string]interface{}{"zoneId": float64(36), "type": "ZoneType_Library"},
		map[string]interface{}{"zoneId": float64(37), "type": "ZoneType_Graveyard"},
	}
}

// zoneTransferAnnotation builds an AnnotationType_ZoneTransfer annotation with a
// category, zone_src, and zone_dest detail set.
func zoneTransferAnnotation(id, affected, zoneSrc, zoneDest int, category string) map[string]interface{} {
	return map[string]interface{}{
		"id":          float64(id),
		"affectorId":  float64(1),
		"affectedIds": []interface{}{float64(affected)},
		"type":        []interface{}{"AnnotationType_ZoneTransfer"},
		"details": []interface{}{
			map[string]interface{}{"key": "zone_src", "valueInt32": []interface{}{float64(zoneSrc)}},
			map[string]interface{}{"key": "zone_dest", "valueInt32": []interface{}{float64(zoneDest)}},
			map[string]interface{}{"key": "category", "valueString": []interface{}{category}},
		},
	}
}

// gameObject builds a minimal gameObject entry.
func gameObject(instanceID, grpID, zoneID, controllerSeatID int) map[string]interface{} {
	return map[string]interface{}{
		"instanceId":       float64(instanceID),
		"grpId":            float64(grpID),
		"zoneId":           float64(zoneID),
		"controllerSeatId": float64(controllerSeatID),
	}
}

// TestCaptureScope_MillFromLibrarySuppressed verifies that a flood of
// category=="Mill" ZoneTransfer plays sourced from a Library zone (the engine
// dumping a dead player's deck into the graveyard) is dropped from the parsed
// plays — both the annotation-pass output AND the snapshot-diff re-derivation.
func TestCaptureScope_MillFromLibrarySuppressed(t *testing.T) {
	// Message 1: baseline snapshot — three opponent cards sitting in their
	// library (zone 36), each with a resolvable grpId.
	msg1 := map[string]interface{}{
		"gameInfo": map[string]interface{}{"matchID": "synthetic", "gameNumber": float64(1), "stage": "GameStage_Play"},
		"turnInfo": map[string]interface{}{"turnNumber": float64(10)},
		"zones":    captureScopeZones(),
		"gameObjects": []interface{}{
			gameObject(501, 70001, 36, 2),
			gameObject(502, 70002, 36, 2),
			gameObject(503, 70003, 36, 2),
		},
	}
	// Message 2: those three cards are milled — they now appear in the graveyard
	// (zone 37) AND carry Mill ZoneTransfer annotations sourced from Library.
	msg2 := map[string]interface{}{
		"gameInfo": map[string]interface{}{"matchID": "synthetic", "gameNumber": float64(1), "stage": "GameStage_Play"},
		"turnInfo": map[string]interface{}{"turnNumber": float64(10)},
		"zones":    captureScopeZones(),
		"gameObjects": []interface{}{
			gameObject(501, 70001, 37, 2),
			gameObject(502, 70002, 37, 2),
			gameObject(503, 70003, 37, 2),
		},
		"annotations": []interface{}{
			zoneTransferAnnotation(901, 501, 36, 37, "Mill"),
			zoneTransferAnnotation(902, 502, 36, 37, "Mill"),
			zoneTransferAnnotation(903, 503, 36, 37, "Mill"),
		},
	}

	entries := []*LogEntry{buildGREEntry(t, msg1), buildGREEntry(t, msg2)}
	res, err := ParseGamePlaysResult(entries, &GREConnection{SeatID: 1})
	if err != nil {
		t.Fatalf("ParseGamePlaysResult: %v", err)
	}

	for _, p := range res.Plays {
		if p.ActionType == "to_graveyard" {
			t.Errorf("Mill-from-Library play leaked into output: instance=%d card=%d turn=%d — "+
				"end-of-game library mill must be suppressed (#814)", p.InstanceID, p.CardID, p.TurnNumber)
		}
	}
}

// TestCaptureScope_RealDeathsPreserved verifies the Mill suppression is narrow:
// non-Mill graveyard moves (Destroy / Sacrifice / Discard — real card deaths)
// and a graveyard move NOT sourced from a Library zone are still emitted.
func TestCaptureScope_RealDeathsPreserved(t *testing.T) {
	msg1 := map[string]interface{}{
		"gameInfo": map[string]interface{}{"matchID": "synthetic", "gameNumber": float64(1), "stage": "GameStage_Play"},
		"turnInfo": map[string]interface{}{"turnNumber": float64(8)},
		"zones":    captureScopeZones(),
		"gameObjects": []interface{}{
			gameObject(601, 80001, 28, 1), // a creature on the battlefield
		},
	}
	// The creature is destroyed: battlefield (28) -> graveyard (33), category Destroy.
	msg2 := map[string]interface{}{
		"gameInfo": map[string]interface{}{"matchID": "synthetic", "gameNumber": float64(1), "stage": "GameStage_Play"},
		"turnInfo": map[string]interface{}{"turnNumber": float64(8)},
		"zones":    captureScopeZones(),
		"gameObjects": []interface{}{
			gameObject(601, 80001, 33, 1),
		},
		"annotations": []interface{}{
			zoneTransferAnnotation(910, 601, 28, 33, "Destroy"),
		},
	}

	entries := []*LogEntry{buildGREEntry(t, msg1), buildGREEntry(t, msg2)}
	res, err := ParseGamePlaysResult(entries, &GREConnection{SeatID: 1})
	if err != nil {
		t.Fatalf("ParseGamePlaysResult: %v", err)
	}

	found := false
	for _, p := range res.Plays {
		if p.ActionType == "to_graveyard" && p.InstanceID == 601 && p.CardID == 80001 {
			found = true
		}
	}
	if !found {
		t.Error("real death (Destroy from battlefield) was suppressed — only Mill-from-Library may be dropped (#814)")
	}
}

// TestCaptureScope_UnresolvableGrpIDSuppressed verifies that a ZoneTransfer
// annotation whose affected instance has no resolvable gameObject (grpID==0)
// and lands in a hidden zone is dropped rather than emitted as an arena=0 /
// zone_0 blank play (prod bug #195 failure mode).
func TestCaptureScope_UnresolvableGrpIDSuppressed(t *testing.T) {
	// A single message carrying a Put-into-hidden-zone annotation for an
	// instance that is NOT present in gameObjects -> grpID resolves to 0.
	msg := map[string]interface{}{
		"gameInfo":    map[string]interface{}{"matchID": "synthetic", "gameNumber": float64(1), "stage": "GameStage_Play"},
		"turnInfo":    map[string]interface{}{"turnNumber": float64(5)},
		"zones":       captureScopeZones(),
		"gameObjects": []interface{}{}, // empty — instance 777 is unresolvable
		"annotations": []interface{}{
			// Put into opponent Library (zone 36) — hidden, unresolvable.
			zoneTransferAnnotation(920, 777, 28, 36, "Put"),
			// Return into opponent Hand (zone 35) — hidden, unresolvable.
			zoneTransferAnnotation(921, 778, 28, 35, "Return"),
		},
	}
	// Need two messages for ParseGamePlaysResult's diff loop; duplicate it.
	entries := []*LogEntry{buildGREEntry(t, msg), buildGREEntry(t, msg)}
	res, err := ParseGamePlaysResult(entries, &GREConnection{SeatID: 1})
	if err != nil {
		t.Fatalf("ParseGamePlaysResult: %v", err)
	}

	for _, p := range res.Plays {
		if p.CardID == 0 && p.ActionType != "life_change" {
			t.Errorf("unresolvable grpID==0 play leaked into output: action=%s to=%q — "+
				"must be suppressed (#814 / #195)", p.ActionType, p.ZoneTo)
		}
		if p.ZoneTo == "zone_0" || p.ZoneFrom == "zone_0" {
			t.Errorf("zone_0 play leaked into output: action=%s — must be suppressed (#814)", p.ActionType)
		}
	}
}

// TestCaptureScope_HiddenZoneIDs documents the SECONDARY confirming signal for
// the grpID==0 suppression: every unresolvable grpID==0 ZoneTransfer in the real
// Player.log lands in a hidden zone (Hand 31/35 or Library 32/36). This pins the
// verified zone-type map and proves the helper classifies the zones Ray audited.
func TestCaptureScope_HiddenZoneIDs(t *testing.T) {
	hidden := []int{31, 35, 32, 36} // Hand (ours/opp), Library (ours/opp)
	for _, z := range hidden {
		if !isHiddenZoneID(z) {
			t.Errorf("zone %d should be classified hidden (Hand/Library)", z)
		}
	}
	public := []int{28, 33, 37} // Battlefield, Graveyard (ours/opp)
	for _, z := range public {
		if isHiddenZoneID(z) {
			t.Errorf("zone %d is public (Battlefield/Graveyard) and must not be classified hidden", z)
		}
	}
}

// TestCaptureScope_ResolvableGrpIDStillEmitted is the anti-#2937 guard: a play
// whose instance DOES resolve to a real grpId must still be emitted. This proves
// the grpID==0 suppression is narrow and will not mask a future real
// capture-drop recovery (the #2937 cumulative-map path).
func TestCaptureScope_ResolvableGrpIDStillEmitted(t *testing.T) {
	msg1 := map[string]interface{}{
		"gameInfo": map[string]interface{}{"matchID": "synthetic", "gameNumber": float64(1), "stage": "GameStage_Play"},
		"turnInfo": map[string]interface{}{"turnNumber": float64(3)},
		"zones":    captureScopeZones(),
		"gameObjects": []interface{}{
			gameObject(701, 90010, 31, 2), // opponent card in hand, resolvable grpId
		},
	}
	// Opponent casts the card: a CastSpell ZoneTransfer for instance 701, which
	// IS resolvable (grpId 90010). It must survive the capture-scope filters.
	msg2 := map[string]interface{}{
		"gameInfo": map[string]interface{}{"matchID": "synthetic", "gameNumber": float64(1), "stage": "GameStage_Play"},
		"turnInfo": map[string]interface{}{"turnNumber": float64(3)},
		"zones":    captureScopeZones(),
		"gameObjects": []interface{}{
			gameObject(701, 90010, 31, 2),
		},
		"annotations": []interface{}{
			zoneTransferAnnotation(930, 701, 31, 28, "CastSpell"),
		},
	}

	entries := []*LogEntry{buildGREEntry(t, msg1), buildGREEntry(t, msg2)}
	res, err := ParseGamePlaysResult(entries, &GREConnection{SeatID: 1})
	if err != nil {
		t.Fatalf("ParseGamePlaysResult: %v", err)
	}

	found := false
	for _, p := range res.Plays {
		if p.ActionType == "cast_spell" && p.CardID == 90010 {
			found = true
		}
	}
	if !found {
		t.Error("resolvable-grpId CastSpell was suppressed — the capture-scope filter must NOT " +
			"mask recoverable plays (anti-#2937 regression guard, #814)")
	}
}

// TestCaptureScope_RealPlayerLog is the binding real-log regression for #814.
// It replays the gitignored, PII-bearing repo-root Player.log through the real
// parser and asserts the three release-gating conditions from Ray's plan
// verdict. The Player.log is never committed (ADR-041); this test is skipped
// when the file is absent (CI) and runs only on a developer machine that has
// the real capture, where it produces the Local Verification transcript.
func TestCaptureScope_RealPlayerLog(t *testing.T) {
	// Repo-root Player.log: pkg/logparse -> repo root is two levels up.
	candidates := []string{
		"../../Player.log",
		"Player.log",
	}
	var path string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			path = c
			break
		}
	}
	if path == "" {
		t.Skip("repo-root Player.log not present (expected in CI) — skipping real-log regression")
	}
	abs, _ := filepath.Abs(path)
	t.Logf("replaying real Player.log at %s", abs)

	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("open Player.log: %v", err)
	}
	defer r.Close()
	entries, err := r.ReadAllJSON()
	if err != nil {
		t.Fatalf("read Player.log: %v", err)
	}

	conn := GetPlayerSeatID(entries)
	if conn == nil {
		conn = &GREConnection{SeatID: 1}
	}
	res, err := ParseGamePlaysResult(entries, conn)
	if err != nil {
		t.Fatalf("ParseGamePlaysResult on real Player.log: %v", err)
	}

	// (1) ZERO arena=0 / zone_0 plays remain.
	zeroCard := 0
	zoneZero := 0
	for _, p := range res.Plays {
		if p.CardID == 0 && p.ActionType != "life_change" {
			zeroCard++
		}
		if p.ZoneTo == "zone_0" || p.ZoneFrom == "zone_0" {
			zoneZero++
		}
	}
	if zeroCard != 0 {
		t.Errorf("AC1: expected 0 arena=0 non-life plays, got %d", zeroCard)
	}
	if zoneZero != 0 {
		t.Errorf("AC1: expected 0 zone_0 plays, got %d", zoneZero)
	}

	// (2) The end-of-game graveyard flood is gone. On main this log produces 106
	// to_graveyard plays (70 of them the dead-deck library mill); after the fix
	// only the 36 real deaths remain. Pin the exact post-fix count.
	const expectedGraveyard = 36
	graveyard := 0
	for _, p := range res.Plays {
		if p.ActionType == "to_graveyard" {
			graveyard++
		}
	}
	if graveyard != expectedGraveyard {
		t.Errorf("AC2: expected %d to_graveyard plays after mill suppression, got %d", expectedGraveyard, graveyard)
	}

	// (3) The real lethal sequence at gsId 412/417 survives. These are the
	// player's game-ending casts (grpId 204333, 204453), the lethal attacker
	// (grpId 102639), and the resolved spell (grpId 102792) — none are
	// category Mill or sourced from Library, so they must be preserved.
	var lethalCasts, lethalAttacks, lethalResolves int
	for _, p := range res.Plays {
		if p.TurnNumber < 15 {
			continue
		}
		switch {
		case p.ActionType == "cast_spell" && (p.CardID == 204333 || p.CardID == 204453):
			lethalCasts++
		case p.ActionType == "attack" && p.CardID == 102639:
			lethalAttacks++
		case p.ActionType == "resolve_spell" && p.CardID == 102792:
			lethalResolves++
		}
	}
	if lethalCasts == 0 || lethalAttacks == 0 || lethalResolves == 0 {
		t.Errorf("AC3 (lethal must survive): casts=%d attacks=%d resolves=%d — "+
			"the game-ending play was swallowed by the filter", lethalCasts, lethalAttacks, lethalResolves)
	}

	t.Logf("AC1 arena=0 plays: %d (want 0) | zone_0 plays: %d (want 0)", zeroCard, zoneZero)
	t.Logf("AC2 to_graveyard plays: %d (want %d) | total plays: %d", graveyard, expectedGraveyard, len(res.Plays))
	t.Logf("AC3 lethal survives: casts=%d attacks=%d resolves=%d", lethalCasts, lethalAttacks, lethalResolves)
}
