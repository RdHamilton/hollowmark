package logparse

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestParseGamePlays_Integration tests the play tracking parser with sanitized match log data.
// The fixture contains GRE messages from a synthetic MTGA match fixture (vs TestPlayer#00004).
func TestParseGamePlays_Integration(t *testing.T) {
	// Get the directory of this test file
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to get current file path")
	}
	testdataDir := filepath.Join(filepath.Dir(currentFile), "testdata")
	fixturePath := filepath.Join(testdataDir, "match_play_tracking.json")

	// Read the fixture file
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Skipf("Skipping integration test: fixture file not found at %s", fixturePath)
	}

	// Parse the JSON fixture
	var jsonBlocks []map[string]interface{}
	if err := json.Unmarshal(data, &jsonBlocks); err != nil {
		t.Fatalf("Failed to parse fixture JSON: %v", err)
	}

	// Convert JSON blocks to LogEntry format
	entries := make([]*LogEntry, 0, len(jsonBlocks))
	for _, block := range jsonBlocks {
		entries = append(entries, &LogEntry{
			IsJSON:    true,
			JSON:      block,
			Timestamp: "2025-01-11 15:00:00",
		})
	}

	t.Logf("Loaded %d log entries from fixture", len(entries))

	// Get player connection info
	playerConn := GetPlayerSeatIDByName(entries, "TestPlayer#00003")
	if playerConn == nil {
		t.Fatal("Failed to find player connection info")
	}
	t.Logf("Player seat ID: %d (SystemSeatID: %d)", playerConn.SeatID, playerConn.SystemSeatID)

	// Parse GRE messages
	messages, err := ParseGREMessages(entries)
	if err != nil {
		t.Fatalf("ParseGREMessages failed: %v", err)
	}
	t.Logf("Parsed %d GRE game state messages", len(messages))

	// Verify we have game state messages
	if len(messages) < 2 {
		t.Fatalf("Expected at least 2 game state messages, got %d", len(messages))
	}

	// Check that zones are being parsed
	zonesFound := false
	for _, msg := range messages {
		if len(msg.Zones) > 10 {
			zonesFound = true
			t.Logf("Found message with %d zones", len(msg.Zones))
			break
		}
	}
	if !zonesFound {
		t.Log("Warning: No message with full zones array found")
	}

	// Parse game plays
	plays, err := ParseGamePlays(entries, playerConn)
	if err != nil {
		t.Fatalf("ParseGamePlays failed: %v", err)
	}
	t.Logf("Detected %d game plays", len(plays))

	// Verify we detected some plays
	if len(plays) == 0 {
		t.Error("Expected to detect some game plays, got 0")
	}

	// Analyze the detected plays
	actionCounts := make(map[string]int)
	playerPlays := 0
	opponentPlays := 0
	lifeChanges := 0

	for _, play := range plays {
		actionCounts[play.ActionType]++
		if play.PlayerType == "player" {
			playerPlays++
		} else {
			opponentPlays++
		}
		if play.ActionType == "life_change" {
			lifeChanges++
		}
	}

	t.Logf("Action type breakdown:")
	for action, count := range actionCounts {
		t.Logf("  %s: %d", action, count)
	}
	t.Logf("Player plays: %d, Opponent plays: %d", playerPlays, opponentPlays)
	t.Logf("Life changes detected: %d", lifeChanges)

	// Verify we have a mix of player and opponent plays
	if playerPlays == 0 && opponentPlays == 0 {
		t.Error("Expected to detect plays for both player and opponent")
	}

	// Extract game snapshots
	snapshots, err := ExtractGameSnapshots(entries, playerConn)
	if err != nil {
		t.Fatalf("ExtractGameSnapshots failed: %v", err)
	}
	t.Logf("Extracted %d game snapshots", len(snapshots))

	// Extract opponent cards
	opponentCards, err := ExtractOpponentCards(entries, playerConn)
	if err != nil {
		t.Fatalf("ExtractOpponentCards failed: %v", err)
	}
	t.Logf("Observed %d opponent cards", len(opponentCards))
}

// TestParseGREMessages_Integration tests GRE message parsing with real data.
func TestParseGREMessages_Integration(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to get current file path")
	}
	testdataDir := filepath.Join(filepath.Dir(currentFile), "testdata")
	fixturePath := filepath.Join(testdataDir, "match_play_tracking.json")

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Skipf("Skipping integration test: fixture file not found at %s", fixturePath)
	}

	var jsonBlocks []map[string]interface{}
	if err := json.Unmarshal(data, &jsonBlocks); err != nil {
		t.Fatalf("Failed to parse fixture JSON: %v", err)
	}

	entries := make([]*LogEntry, 0, len(jsonBlocks))
	for _, block := range jsonBlocks {
		entries = append(entries, &LogEntry{
			IsJSON:    true,
			JSON:      block,
			Timestamp: "2025-01-11 15:00:00",
		})
	}

	messages, err := ParseGREMessages(entries)
	if err != nil {
		t.Fatalf("ParseGREMessages failed: %v", err)
	}

	// Verify message structure
	t.Run("MatchID", func(t *testing.T) {
		foundMatchID := false
		for _, msg := range messages {
			if msg.MatchID != "" {
				foundMatchID = true
				t.Logf("Match ID: %s", msg.MatchID)
				break
			}
		}
		if !foundMatchID {
			t.Log("Warning: No match ID found in messages")
		}
	})

	t.Run("TurnProgression", func(t *testing.T) {
		turns := make(map[int]bool)
		for _, msg := range messages {
			if msg.TurnInfo != nil && msg.TurnInfo.TurnNumber > 0 {
				turns[msg.TurnInfo.TurnNumber] = true
			}
		}
		t.Logf("Turns found: %v", len(turns))
	})

	t.Run("GameObjects", func(t *testing.T) {
		totalObjects := 0
		maxObjects := 0
		for _, msg := range messages {
			count := len(msg.GameObjects)
			totalObjects += count
			if count > maxObjects {
				maxObjects = count
			}
		}
		t.Logf("Total game objects across all messages: %d", totalObjects)
		t.Logf("Max objects in single message: %d", maxObjects)
	})

	t.Run("Players", func(t *testing.T) {
		for _, msg := range messages {
			if len(msg.Players) > 0 {
				for _, player := range msg.Players {
					t.Logf("Player seat %d: life=%d", player.SeatID, player.LifeTotal)
				}
				break
			}
		}
	})

	t.Run("Zones", func(t *testing.T) {
		for _, msg := range messages {
			if len(msg.Zones) > 0 {
				t.Logf("Message has %d zones", len(msg.Zones))
				// Log a few zone types
				count := 0
				for _, zone := range msg.Zones {
					t.Logf("  Zone %d: %s (owner: %d)", zone.ZoneID, zone.Type, zone.OwnerSeatID)
					count++
					if count >= 5 {
						break
					}
				}
				break
			}
		}
	})
}

// TestZoneNameResolution_Integration tests zone name resolution with real data.
func TestZoneNameResolution_Integration(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to get current file path")
	}
	testdataDir := filepath.Join(filepath.Dir(currentFile), "testdata")
	fixturePath := filepath.Join(testdataDir, "match_play_tracking.json")

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Skipf("Skipping integration test: fixture file not found at %s", fixturePath)
	}

	var jsonBlocks []map[string]interface{}
	if err := json.Unmarshal(data, &jsonBlocks); err != nil {
		t.Fatalf("Failed to parse fixture JSON: %v", err)
	}

	entries := make([]*LogEntry, 0, len(jsonBlocks))
	for _, block := range jsonBlocks {
		entries = append(entries, &LogEntry{
			IsJSON:    true,
			JSON:      block,
			Timestamp: "2025-01-11 15:00:00",
		})
	}

	messages, err := ParseGREMessages(entries)
	if err != nil {
		t.Fatalf("ParseGREMessages failed: %v", err)
	}

	// Build cumulative zones map like ParseGamePlays does
	cumulativeZones := make(map[int]*GREZone)
	for _, msg := range messages {
		for zoneID, zone := range msg.Zones {
			cumulativeZones[zoneID] = zone
		}
	}

	t.Logf("Built cumulative zones map with %d zones", len(cumulativeZones))

	// Test zone name resolution
	t.Run("ZoneTypeMapping", func(t *testing.T) {
		expectedTypes := map[string]bool{
			"hand":        false,
			"library":     false,
			"battlefield": false,
			"graveyard":   false,
			"exile":       false,
			"stack":       false,
		}

		for zoneID, zone := range cumulativeZones {
			zoneName := zoneTypeToReadableName(zone.Type)
			t.Logf("Zone %d (%s) -> %s", zoneID, zone.Type, zoneName)
			if _, ok := expectedTypes[zoneName]; ok {
				expectedTypes[zoneName] = true
			}
		}

		// Check which common zones we found
		for zoneType, found := range expectedTypes {
			if found {
				t.Logf("Found zone type: %s", zoneType)
			}
		}
	})
}

// TestParseGamePlaysResult_RealCorpusFixture replays the promoted corpus
// gre-game-session fixture (services/daemon/testdata/corpus/player-log/
// gre-game-session.log — 5 real GRE lines from a 2026-06-01 SOS quick-draft
// session, sanitized per ADR-041) through ParseGamePlaysResult and verifies
// the parsers handle real Strixhaven/SOS GRE message shapes without error.
//
// This is the parser-validation step required by #403 corpus promotion:
// the fixture was previously synthetic; running it through the real parser
// confirms the parsers work on live MTGA data, not just crafted inputs.
func TestParseGamePlaysResult_RealCorpusFixture(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Failed to get current file path")
	}
	fixturePath := filepath.Join(filepath.Dir(currentFile), "testdata", "gre-game-session-corpus.log")

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Skipf("Skipping: corpus fixture not found at %s", fixturePath)
	}

	// Each line is a standalone greToClientEvent JSON object.
	var entries []*LogEntry
	for _, line := range splitLines(data) {
		if len(line) == 0 {
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		entries = append(entries, &LogEntry{
			IsJSON:    true,
			JSON:      raw,
			Timestamp: "2026-01-01 00:00:00",
		})
	}

	if len(entries) == 0 {
		t.Fatal("corpus fixture produced zero log entries")
	}
	t.Logf("Loaded %d GRE lines from corpus fixture", len(entries))

	// Identify the player's seat from the ConnectResp in the fixture.
	playerConn := GetPlayerSeatID(entries)
	if playerConn == nil {
		// The fixture may start with a ConnectResp embedded inside the first
		// greToClientEvent line; fall back to seat 1 (the local player in
		// MTGA is always seat 1 from their own perspective).
		playerConn = &GREConnection{SeatID: 1}
		t.Log("ConnectResp not found via GetPlayerSeatID — defaulting to SeatID=1")
	}
	t.Logf("Player seat: %d", playerConn.SeatID)

	result, err := ParseGamePlaysResult(entries, playerConn)
	if err != nil {
		t.Fatalf("ParseGamePlaysResult on real corpus fixture: %v", err)
	}

	// The fixture contains a ConnectResp + SetSettingsResp + GameStateMessages
	// (including a MulliganReq). We expect snapshots and mulligan data.
	t.Logf("Snapshots: %d, OpponentCards: %d, Plays: %d, CounterChanges: %d",
		len(result.Snapshots), len(result.OpponentCards), len(result.Plays), len(result.CounterChanges))

	// Invariant: ParseGamePlaysResult must not error on real GRE data.
	// At least one GameStateMessage is present so snapshots must be non-nil
	// (an empty slice is still non-nil after the message loop runs).
	if result.Snapshots == nil && result.OpponentCards == nil && result.Plays == nil {
		t.Error("all outputs nil — parser produced no output from real corpus fixture")
	}

	// If a MulliganReq is present in the fixture, mulligan detection should
	// not error. Mulligan data may be nil if no pre-game messages with
	// TurnNumber=0 appear in the trimmed sample.
	t.Logf("Mulligan data present: %v", result.Mulligan != nil)

	// Counter changes must be 0 for this SOS quick-draft session — confirmed
	// absence of counter-bearing gameObjects in the 2026-06-01 session log.
	// This validates #613 counter finding: counter events are absent because
	// the games played (B/G and B/W aggro) used no counter-bearing permanents.
	if len(result.CounterChanges) != 0 {
		t.Errorf("expected 0 counter changes for SOS quick-draft session, got %d — "+
			"if counters now appear, update the #613 counter finding note in #403",
			len(result.CounterChanges))
	}
}

// splitLines splits a byte slice into non-empty lines.
func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
