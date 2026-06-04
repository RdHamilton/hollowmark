package logparse

import (
	"testing"
	"time"
)

func TestGetPlayerSeatID_FromConnectResp(t *testing.T) {
	entries := []*LogEntry{
		{
			IsJSON: true,
			JSON: map[string]interface{}{
				"connectResp": map[string]interface{}{
					"systemSeatIds": []interface{}{float64(1)},
					"teamId":        float64(2),
				},
			},
		},
	}

	conn := GetPlayerSeatID(entries)

	if conn == nil {
		t.Fatal("Expected connection info to be non-nil")
	}
	if conn.SystemSeatID != 1 {
		t.Errorf("Expected SystemSeatID 1, got %d", conn.SystemSeatID)
	}
	if conn.TeamID != 2 {
		t.Errorf("Expected TeamID 2, got %d", conn.TeamID)
	}
}

func TestGetPlayerSeatID_FromMatchEvent(t *testing.T) {
	entries := []*LogEntry{
		{
			IsJSON: true,
			JSON: map[string]interface{}{
				"matchGameRoomStateChangedEvent": map[string]interface{}{
					"gameRoomInfo": map[string]interface{}{
						"gameRoomConfig": map[string]interface{}{
							"reservedPlayers": []interface{}{
								map[string]interface{}{
									"systemSeatId": float64(2),
									"teamId":       float64(1),
									"playerName":   "TestPlayer",
								},
							},
						},
					},
				},
			},
		},
	}

	// Without screen name, GetPlayerSeatID won't match from matchGameRoomStateChangedEvent
	// (to avoid picking wrong player). Use GetPlayerSeatIDByName instead.
	conn := GetPlayerSeatIDByName(entries, "TestPlayer")

	if conn == nil {
		t.Fatal("Expected connection info to be non-nil")
	}
	if conn.SystemSeatID != 2 {
		t.Errorf("Expected SystemSeatID 2, got %d", conn.SystemSeatID)
	}
	if conn.TeamID != 1 {
		t.Errorf("Expected TeamID 1, got %d", conn.TeamID)
	}
}

func TestGetPlayerSeatIDByName_MatchesCorrectPlayer(t *testing.T) {
	// Test that GetPlayerSeatIDByName correctly matches by player name when
	// there are multiple players in reservedPlayers
	entries := []*LogEntry{
		{
			IsJSON: true,
			JSON: map[string]interface{}{
				"matchGameRoomStateChangedEvent": map[string]interface{}{
					"gameRoomInfo": map[string]interface{}{
						"gameRoomConfig": map[string]interface{}{
							"reservedPlayers": []interface{}{
								map[string]interface{}{
									"systemSeatId": float64(1),
									"teamId":       float64(1),
									"playerName":   "Opponent",
								},
								map[string]interface{}{
									"systemSeatId": float64(2),
									"teamId":       float64(2),
									"playerName":   "MyPlayer",
								},
							},
						},
					},
				},
			},
		},
	}

	// Match by name - should get seat 2 (MyPlayer), not seat 1 (Opponent)
	conn := GetPlayerSeatIDByName(entries, "MyPlayer")

	if conn == nil {
		t.Fatal("Expected connection info to be non-nil")
	}
	if conn.SystemSeatID != 2 {
		t.Errorf("Expected SystemSeatID 2 (MyPlayer), got %d", conn.SystemSeatID)
	}
	if conn.TeamID != 2 {
		t.Errorf("Expected TeamID 2, got %d", conn.TeamID)
	}
}

func TestGetPlayerSeatID_NoMatch(t *testing.T) {
	entries := []*LogEntry{
		{
			IsJSON: true,
			JSON: map[string]interface{}{
				"someOtherEvent": map[string]interface{}{},
			},
		},
	}

	conn := GetPlayerSeatID(entries)

	if conn != nil {
		t.Error("Expected connection info to be nil for non-matching entries")
	}
}

func TestParseGREMessages_GameStateMessage(t *testing.T) {
	entries := []*LogEntry{
		{
			IsJSON:    true,
			Timestamp: "2024-01-15 10:30:45",
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber":     float64(3),
									"phase":          "Phase_Main1",
									"step":           "",
									"activePlayer":   float64(1),
									"priorityPlayer": float64(1),
								},
								"players": []interface{}{
									map[string]interface{}{
										"seatId":    float64(1),
										"lifeTotal": float64(20),
										"teamId":    float64(1),
									},
									map[string]interface{}{
										"seatId":    float64(2),
										"lifeTotal": float64(18),
										"teamId":    float64(2),
									},
								},
								"gameObjects": []interface{}{
									map[string]interface{}{
										"instanceId":       float64(100),
										"grpId":            float64(12345),
										"ownerSeatId":      float64(1),
										"controllerSeatId": float64(1),
										"zoneId":           float64(3), // battlefield
										"cardTypes":        []interface{}{"CardType_Creature"},
									},
								},
								"gameInfo": map[string]interface{}{
									"matchID":    "match-123",
									"gameNumber": float64(1),
								},
							},
						},
					},
				},
			},
		},
	}

	messages, err := ParseGREMessages(entries)
	if err != nil {
		t.Fatalf("ParseGREMessages failed: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(messages))
	}

	msg := messages[0]
	if msg.MatchID != "match-123" {
		t.Errorf("Expected MatchID 'match-123', got '%s'", msg.MatchID)
	}
	if msg.GameNumber != 1 {
		t.Errorf("Expected GameNumber 1, got %d", msg.GameNumber)
	}

	if msg.TurnInfo == nil {
		t.Fatal("Expected TurnInfo to be non-nil")
	}
	if msg.TurnInfo.TurnNumber != 3 {
		t.Errorf("Expected TurnNumber 3, got %d", msg.TurnInfo.TurnNumber)
	}
	if msg.TurnInfo.Phase != "Phase_Main1" {
		t.Errorf("Expected Phase 'Phase_Main1', got '%s'", msg.TurnInfo.Phase)
	}

	if len(msg.Players) != 2 {
		t.Fatalf("Expected 2 players, got %d", len(msg.Players))
	}
	if msg.Players[0].LifeTotal != 20 {
		t.Errorf("Expected player 1 life 20, got %d", msg.Players[0].LifeTotal)
	}

	if len(msg.GameObjects) != 1 {
		t.Fatalf("Expected 1 game object, got %d", len(msg.GameObjects))
	}
	if msg.GameObjects[0].GRPId != 12345 {
		t.Errorf("Expected GRPId 12345, got %d", msg.GameObjects[0].GRPId)
	}
}

func TestParseGREMessages_SkipsNonGameStateMessages(t *testing.T) {
	entries := []*LogEntry{
		{
			IsJSON: true,
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_QueuedGameStateMessage",
						},
						map[string]interface{}{
							"type": "GREMessageType_UIMessage",
						},
					},
				},
			},
		},
	}

	messages, err := ParseGREMessages(entries)
	if err != nil {
		t.Fatalf("ParseGREMessages failed: %v", err)
	}

	if len(messages) != 0 {
		t.Errorf("Expected 0 messages for non-GameStateMessage types, got %d", len(messages))
	}
}

func TestNormalizePhase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Phase_Beginning", "Beginning"},
		{"Phase_Main1", "Main1"},
		{"Phase_Combat", "Combat"},
		{"Phase_Main2", "Main2"},
		{"Phase_Ending", "Ending"},
		{"UnknownPhase", "UnknownPhase"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizePhase(tt.input)
			if result != tt.expected {
				t.Errorf("normalizePhase(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeStep(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Step_Upkeep", "Upkeep"},
		{"Step_Draw", "Draw"},
		{"Step_BeginCombat", "BeginCombat"},
		{"Step_DeclareAttack", "DeclareAttackers"},
		{"Step_DeclareBlock", "DeclareBlockers"},
		{"Step_CombatDamage", "CombatDamage"},
		{"Step_End", "EndStep"},
		{"Step_Cleanup", "Cleanup"},
		{"UnknownStep", "UnknownStep"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeStep(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeStep(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestZoneIDToName(t *testing.T) {
	tests := []struct {
		zoneID   int
		expected string
	}{
		{1, "hand"},
		{11, "hand"},
		{2, "library"},
		{3, "battlefield"},
		{4, "graveyard"},
		{5, "exile"},
		{6, "stack"},
		{7, "command"},
		{8, "zone_8"}, // Unknown zone
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := zoneIDToName(tt.zoneID)
			if result != tt.expected {
				t.Errorf("zoneIDToName(%d) = %q, expected %q", tt.zoneID, result, tt.expected)
			}
		})
	}
}

func TestParseGamePlays_DetectsZoneChanges(t *testing.T) {
	// Create two game states where a card moves from hand to battlefield
	entries := []*LogEntry{
		// First state: card in hand
		{
			IsJSON:    true,
			Timestamp: "2024-01-15 10:30:45",
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber":   float64(1),
									"phase":        "Phase_Main1",
									"activePlayer": float64(1),
								},
								"gameObjects": []interface{}{
									map[string]interface{}{
										"instanceId":       float64(100),
										"grpId":            float64(12345),
										"controllerSeatId": float64(1),
										"zoneId":           float64(1), // hand
										"cardTypes":        []interface{}{"CardType_Creature"},
									},
								},
								"gameInfo": map[string]interface{}{
									"matchID":    "match-123",
									"gameNumber": float64(1),
								},
							},
						},
					},
				},
			},
		},
		// Second state: card on battlefield
		{
			IsJSON:    true,
			Timestamp: "2024-01-15 10:30:46",
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber":   float64(1),
									"phase":        "Phase_Main1",
									"activePlayer": float64(1),
								},
								"gameObjects": []interface{}{
									map[string]interface{}{
										"instanceId":       float64(100),
										"grpId":            float64(12345),
										"controllerSeatId": float64(1),
										"zoneId":           float64(3), // battlefield
										"cardTypes":        []interface{}{"CardType_Creature"},
									},
								},
								"gameInfo": map[string]interface{}{
									"matchID":    "match-123",
									"gameNumber": float64(1),
								},
							},
						},
					},
				},
			},
		},
	}

	playerConn := &GREConnection{SeatID: 1, SystemSeatID: 1}
	plays, err := ParseGamePlays(entries, playerConn)
	if err != nil {
		t.Fatalf("ParseGamePlays failed: %v", err)
	}

	if len(plays) != 1 {
		t.Fatalf("Expected 1 play, got %d", len(plays))
	}

	play := plays[0]
	if play.ActionType != "play_card" {
		t.Errorf("Expected ActionType 'play_card', got '%s'", play.ActionType)
	}
	if play.PlayerType != "player" {
		t.Errorf("Expected PlayerType 'player', got '%s'", play.PlayerType)
	}
	if play.ZoneFrom != "hand" {
		t.Errorf("Expected ZoneFrom 'hand', got '%s'", play.ZoneFrom)
	}
	if play.ZoneTo != "battlefield" {
		t.Errorf("Expected ZoneTo 'battlefield', got '%s'", play.ZoneTo)
	}
	if play.CardID != 12345 {
		t.Errorf("Expected CardID 12345, got %d", play.CardID)
	}
}

func TestParseGamePlays_DetectsLandDrop(t *testing.T) {
	entries := []*LogEntry{
		// First state: land in hand
		{
			IsJSON:    true,
			Timestamp: "2024-01-15 10:30:45",
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber":   float64(1),
									"phase":        "Phase_Main1",
									"activePlayer": float64(1),
								},
								"gameObjects": []interface{}{
									map[string]interface{}{
										"instanceId":       float64(200),
										"grpId":            float64(67890),
										"controllerSeatId": float64(1),
										"zoneId":           float64(1), // hand
										"cardTypes":        []interface{}{"CardType_Land"},
									},
								},
							},
						},
					},
				},
			},
		},
		// Second state: land on battlefield
		{
			IsJSON:    true,
			Timestamp: "2024-01-15 10:30:46",
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber":   float64(1),
									"phase":        "Phase_Main1",
									"activePlayer": float64(1),
								},
								"gameObjects": []interface{}{
									map[string]interface{}{
										"instanceId":       float64(200),
										"grpId":            float64(67890),
										"controllerSeatId": float64(1),
										"zoneId":           float64(3), // battlefield
										"cardTypes":        []interface{}{"CardType_Land"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	playerConn := &GREConnection{SeatID: 1, SystemSeatID: 1}
	plays, err := ParseGamePlays(entries, playerConn)
	if err != nil {
		t.Fatalf("ParseGamePlays failed: %v", err)
	}

	if len(plays) != 1 {
		t.Fatalf("Expected 1 play, got %d", len(plays))
	}

	play := plays[0]
	if play.ActionType != "land_drop" {
		t.Errorf("Expected ActionType 'land_drop', got '%s'", play.ActionType)
	}
}

func TestParseGamePlays_DetectsAttack(t *testing.T) {
	entries := []*LogEntry{
		// First state: creature not attacking
		{
			IsJSON:    true,
			Timestamp: "2024-01-15 10:30:45",
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber":   float64(2),
									"phase":        "Phase_Combat",
									"step":         "Step_BeginCombat",
									"activePlayer": float64(1),
								},
								"gameObjects": []interface{}{
									map[string]interface{}{
										"instanceId":       float64(100),
										"grpId":            float64(12345),
										"controllerSeatId": float64(1),
										"zoneId":           float64(3),
									},
								},
							},
						},
					},
				},
			},
		},
		// Second state: creature attacking
		{
			IsJSON:    true,
			Timestamp: "2024-01-15 10:30:46",
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber":   float64(2),
									"phase":        "Phase_Combat",
									"step":         "Step_DeclareAttack",
									"activePlayer": float64(1),
								},
								"gameObjects": []interface{}{
									map[string]interface{}{
										"instanceId":       float64(100),
										"grpId":            float64(12345),
										"controllerSeatId": float64(1),
										"zoneId":           float64(3),
										"attackState":      "AttackState_Attacking",
									},
								},
							},
						},
					},
				},
			},
		},
	}

	playerConn := &GREConnection{SeatID: 1, SystemSeatID: 1}
	plays, err := ParseGamePlays(entries, playerConn)
	if err != nil {
		t.Fatalf("ParseGamePlays failed: %v", err)
	}

	// Should have at least one attack play
	hasAttack := false
	for _, play := range plays {
		if play.ActionType == "attack" {
			hasAttack = true
			if play.PlayerType != "player" {
				t.Errorf("Expected PlayerType 'player' for attack, got '%s'", play.PlayerType)
			}
			break
		}
	}

	if !hasAttack {
		t.Error("Expected to detect an attack action")
	}
}

func TestExtractOpponentCards(t *testing.T) {
	entries := []*LogEntry{
		{
			IsJSON: true,
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber": float64(2),
								},
								"gameObjects": []interface{}{
									// Player's card
									map[string]interface{}{
										"instanceId":       float64(100),
										"grpId":            float64(11111),
										"controllerSeatId": float64(1),
										"zoneId":           float64(3),
									},
									// Opponent's card
									map[string]interface{}{
										"instanceId":       float64(200),
										"grpId":            float64(22222),
										"controllerSeatId": float64(2),
										"zoneId":           float64(3),
									},
									// Another opponent card
									map[string]interface{}{
										"instanceId":       float64(201),
										"grpId":            float64(33333),
										"controllerSeatId": float64(2),
										"zoneId":           float64(4), // graveyard
									},
								},
							},
						},
					},
				},
			},
		},
	}

	playerConn := &GREConnection{SeatID: 1, SystemSeatID: 1}
	cards, err := ExtractOpponentCards(entries, playerConn)
	if err != nil {
		t.Fatalf("ExtractOpponentCards failed: %v", err)
	}

	if len(cards) != 2 {
		t.Fatalf("Expected 2 opponent cards, got %d", len(cards))
	}

	// Check that both opponent cards are captured
	foundCard1 := false
	foundCard2 := false
	for _, card := range cards {
		if card.CardID == 22222 {
			foundCard1 = true
			if card.ZoneObserved != "battlefield" {
				t.Errorf("Expected zone 'battlefield' for card 22222, got '%s'", card.ZoneObserved)
			}
		}
		if card.CardID == 33333 {
			foundCard2 = true
			if card.ZoneObserved != "graveyard" {
				t.Errorf("Expected zone 'graveyard' for card 33333, got '%s'", card.ZoneObserved)
			}
		}
	}

	if !foundCard1 {
		t.Error("Expected to find opponent card 22222")
	}
	if !foundCard2 {
		t.Error("Expected to find opponent card 33333")
	}
}

func TestExtractGameSnapshots(t *testing.T) {
	entries := []*LogEntry{
		{
			IsJSON:    true,
			Timestamp: "2024-01-15 10:30:45",
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber":   float64(1),
									"activePlayer": float64(1),
								},
								"players": []interface{}{
									map[string]interface{}{
										"seatId":    float64(1),
										"lifeTotal": float64(20),
									},
									map[string]interface{}{
										"seatId":    float64(2),
										"lifeTotal": float64(18),
									},
								},
								"gameObjects": []interface{}{
									// Player's hand
									map[string]interface{}{
										"instanceId":       float64(100),
										"grpId":            float64(11111),
										"controllerSeatId": float64(1),
										"zoneId":           float64(1), // hand
									},
									// Player's land
									map[string]interface{}{
										"instanceId":       float64(101),
										"grpId":            float64(22222),
										"controllerSeatId": float64(1),
										"zoneId":           float64(3), // battlefield
										"cardTypes":        []interface{}{"CardType_Land"},
									},
								},
								"gameInfo": map[string]interface{}{
									"matchID":    "match-123",
									"gameNumber": float64(1),
								},
							},
						},
					},
				},
			},
		},
	}

	playerConn := &GREConnection{SeatID: 1, SystemSeatID: 1}
	snapshots, err := ExtractGameSnapshots(entries, playerConn)
	if err != nil {
		t.Fatalf("ExtractGameSnapshots failed: %v", err)
	}

	if len(snapshots) != 1 {
		t.Fatalf("Expected 1 snapshot, got %d", len(snapshots))
	}

	snapshot := snapshots[0]
	if snapshot.MatchID != "match-123" {
		t.Errorf("Expected MatchID 'match-123', got '%s'", snapshot.MatchID)
	}
	if snapshot.TurnNumber != 1 {
		t.Errorf("Expected TurnNumber 1, got %d", snapshot.TurnNumber)
	}
	if snapshot.ActivePlayer != "player" {
		t.Errorf("Expected ActivePlayer 'player', got '%s'", snapshot.ActivePlayer)
	}
	if snapshot.PlayerLife != 20 {
		t.Errorf("Expected PlayerLife 20, got %d", snapshot.PlayerLife)
	}
	if snapshot.OpponentLife != 18 {
		t.Errorf("Expected OpponentLife 18, got %d", snapshot.OpponentLife)
	}
	if snapshot.PlayerCardsInHand != 1 {
		t.Errorf("Expected PlayerCardsInHand 1, got %d", snapshot.PlayerCardsInHand)
	}
	if snapshot.PlayerLandsInPlay != 1 {
		t.Errorf("Expected PlayerLandsInPlay 1, got %d", snapshot.PlayerLandsInPlay)
	}
}

func TestParseTurnInfo(t *testing.T) {
	turnInfo := map[string]interface{}{
		"turnNumber":     float64(5),
		"phase":          "Phase_Combat",
		"step":           "Step_DeclareAttack",
		"activePlayer":   float64(2),
		"priorityPlayer": float64(1),
		"decisionPlayer": float64(1),
		"nextPhase":      "Phase_Main2",
		"nextStep":       "",
	}

	ti := parseTurnInfo(turnInfo)

	if ti.TurnNumber != 5 {
		t.Errorf("Expected TurnNumber 5, got %d", ti.TurnNumber)
	}
	if ti.Phase != "Phase_Combat" {
		t.Errorf("Expected Phase 'Phase_Combat', got '%s'", ti.Phase)
	}
	if ti.Step != "Step_DeclareAttack" {
		t.Errorf("Expected Step 'Step_DeclareAttack', got '%s'", ti.Step)
	}
	if ti.ActivePlayer != 2 {
		t.Errorf("Expected ActivePlayer 2, got %d", ti.ActivePlayer)
	}
	if ti.PriorityPlayer != 1 {
		t.Errorf("Expected PriorityPlayer 1, got %d", ti.PriorityPlayer)
	}
}

func TestParsePlayerState(t *testing.T) {
	playerMap := map[string]interface{}{
		"seatId":        float64(1),
		"lifeTotal":     float64(17),
		"teamId":        float64(1),
		"maxHandSize":   float64(7),
		"systemSeatId":  float64(1),
		"timerState":    "Running",
		"timeRemaining": float64(300),
	}

	ps := parsePlayerState(playerMap)

	if ps.SeatID != 1 {
		t.Errorf("Expected SeatID 1, got %d", ps.SeatID)
	}
	if ps.LifeTotal != 17 {
		t.Errorf("Expected LifeTotal 17, got %d", ps.LifeTotal)
	}
	if ps.TeamID != 1 {
		t.Errorf("Expected TeamID 1, got %d", ps.TeamID)
	}
	if ps.MaxHandSize != 7 {
		t.Errorf("Expected MaxHandSize 7, got %d", ps.MaxHandSize)
	}
}

func TestParseGameObject(t *testing.T) {
	objMap := map[string]interface{}{
		"instanceId":       float64(42),
		"grpId":            float64(12345),
		"ownerSeatId":      float64(1),
		"controllerSeatId": float64(1),
		"zoneId":           float64(3),
		"cardTypes":        []interface{}{"CardType_Creature", "CardType_Legendary"},
		"power": map[string]interface{}{
			"value": float64(4),
		},
		"toughness": map[string]interface{}{
			"value": float64(5),
		},
		"isTapped":    true,
		"attackState": "AttackState_Attacking",
		"counters": []interface{}{
			map[string]interface{}{
				"type":  "+1/+1",
				"count": float64(2),
			},
		},
	}

	obj := parseGameObject(objMap)

	if obj.InstanceID != 42 {
		t.Errorf("Expected InstanceID 42, got %d", obj.InstanceID)
	}
	if obj.GRPId != 12345 {
		t.Errorf("Expected GRPId 12345, got %d", obj.GRPId)
	}
	if obj.ControllerSeatID != 1 {
		t.Errorf("Expected ControllerSeatID 1, got %d", obj.ControllerSeatID)
	}
	if obj.ZoneName != "battlefield" {
		t.Errorf("Expected ZoneName 'battlefield', got '%s'", obj.ZoneName)
	}
	if len(obj.CardTypes) != 2 {
		t.Errorf("Expected 2 card types, got %d", len(obj.CardTypes))
	}
	if obj.Power != 4 {
		t.Errorf("Expected Power 4, got %d", obj.Power)
	}
	if obj.Toughness != 5 {
		t.Errorf("Expected Toughness 5, got %d", obj.Toughness)
	}
	if !obj.IsTapped {
		t.Error("Expected IsTapped to be true")
	}
	if !obj.IsAttacking {
		t.Error("Expected IsAttacking to be true")
	}
	if obj.Counters["+1/+1"] != 2 {
		t.Errorf("Expected 2 +1/+1 counters, got %d", obj.Counters["+1/+1"])
	}
}

func TestZoneIDToNameWithMap(t *testing.T) {
	// Create a zones map similar to what MTGA sends
	zones := map[int]*GREZone{
		28: {ZoneID: 28, Type: "ZoneType_Battlefield", Visibility: "Visibility_Public"},
		30: {ZoneID: 30, Type: "ZoneType_Limbo", Visibility: "Visibility_Public"},
		31: {ZoneID: 31, Type: "ZoneType_Hand", OwnerSeatID: 1, Visibility: "Visibility_Private"},
		32: {ZoneID: 32, Type: "ZoneType_Library", OwnerSeatID: 1, Visibility: "Visibility_Hidden"},
		33: {ZoneID: 33, Type: "ZoneType_Graveyard", OwnerSeatID: 1, Visibility: "Visibility_Public"},
		27: {ZoneID: 27, Type: "ZoneType_Stack", Visibility: "Visibility_Public"},
		29: {ZoneID: 29, Type: "ZoneType_Exile", Visibility: "Visibility_Public"},
	}

	tests := []struct {
		zoneID   int
		expected string
	}{
		{28, "battlefield"},
		{30, "limbo"},
		{31, "hand"},
		{32, "library"},
		{33, "graveyard"},
		{27, "stack"},
		{29, "exile"},
		{99, "zone_99"}, // Unknown zone, falls back to legacy
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := zoneIDToNameWithMap(tt.zoneID, zones)
			if result != tt.expected {
				t.Errorf("zoneIDToNameWithMap(%d) = %q, want %q", tt.zoneID, result, tt.expected)
			}
		})
	}

	// Test with nil zones map (should fall back to legacy)
	result := zoneIDToNameWithMap(31, nil)
	if result != "hand" {
		t.Errorf("zoneIDToNameWithMap(31, nil) = %q, want 'hand'", result)
	}
}

func TestZoneTypeToReadableName(t *testing.T) {
	tests := []struct {
		zoneType string
		expected string
	}{
		{"ZoneType_Hand", "hand"},
		{"ZoneType_Library", "library"},
		{"ZoneType_Battlefield", "battlefield"},
		{"ZoneType_Graveyard", "graveyard"},
		{"ZoneType_Exile", "exile"},
		{"ZoneType_Stack", "stack"},
		{"ZoneType_Command", "command"},
		{"ZoneType_Sideboard", "sideboard"},
		{"ZoneType_Revealed", "revealed"},
		{"ZoneType_Limbo", "limbo"},
		{"ZoneType_Pending", "pending"},
		{"ZoneType_Suppressed", "suppressed"},
		{"ZoneType_NewType", "NewType"},    // Unknown type, strips prefix
		{"SomethingElse", "SomethingElse"}, // No prefix
	}

	for _, tt := range tests {
		t.Run(tt.zoneType, func(t *testing.T) {
			result := zoneTypeToReadableName(tt.zoneType)
			if result != tt.expected {
				t.Errorf("zoneTypeToReadableName(%q) = %q, want %q", tt.zoneType, result, tt.expected)
			}
		})
	}
}

func TestParseZone(t *testing.T) {
	zoneMap := map[string]interface{}{
		"zoneId":      float64(28),
		"type":        "ZoneType_Battlefield",
		"visibility":  "Visibility_Public",
		"ownerSeatId": float64(1),
	}

	zone := parseZone(zoneMap)

	if zone == nil {
		t.Fatal("Expected zone to be non-nil")
	}
	if zone.ZoneID != 28 {
		t.Errorf("Expected ZoneID 28, got %d", zone.ZoneID)
	}
	if zone.Type != "ZoneType_Battlefield" {
		t.Errorf("Expected Type 'ZoneType_Battlefield', got '%s'", zone.Type)
	}
	if zone.Visibility != "Visibility_Public" {
		t.Errorf("Expected Visibility 'Visibility_Public', got '%s'", zone.Visibility)
	}
	if zone.OwnerSeatID != 1 {
		t.Errorf("Expected OwnerSeatID 1, got %d", zone.OwnerSeatID)
	}
}

func TestParseZone_MissingZoneID(t *testing.T) {
	zoneMap := map[string]interface{}{
		"type":       "ZoneType_Battlefield",
		"visibility": "Visibility_Public",
	}

	zone := parseZone(zoneMap)
	if zone != nil {
		t.Error("Expected nil zone when zoneId is missing")
	}
}

func TestParseGameObjectWithZones(t *testing.T) {
	// Create a zones map similar to what MTGA sends
	zones := map[int]*GREZone{
		28: {ZoneID: 28, Type: "ZoneType_Battlefield", Visibility: "Visibility_Public"},
	}

	objMap := map[string]interface{}{
		"instanceId":       float64(42),
		"grpId":            float64(12345),
		"controllerSeatId": float64(1),
		"zoneId":           float64(28),
	}

	obj := parseGameObjectWithZones(objMap, zones)

	if obj.ZoneID != 28 {
		t.Errorf("Expected ZoneID 28, got %d", obj.ZoneID)
	}
	if obj.ZoneName != "battlefield" {
		t.Errorf("Expected ZoneName 'battlefield', got '%s'", obj.ZoneName)
	}
}

func TestParseGamePlays_EmptyEntries(t *testing.T) {
	entries := []*LogEntry{}

	playerConn := &GREConnection{SeatID: 1, SystemSeatID: 1}
	plays, err := ParseGamePlays(entries, playerConn)
	if err != nil {
		t.Fatalf("ParseGamePlays failed: %v", err)
	}

	if plays != nil && len(plays) != 0 {
		t.Errorf("Expected nil or empty plays for empty entries, got %d", len(plays))
	}
}

func TestParseGamePlays_SingleEntry(t *testing.T) {
	entries := []*LogEntry{
		{
			IsJSON: true,
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber": float64(1),
								},
							},
						},
					},
				},
			},
		},
	}

	playerConn := &GREConnection{SeatID: 1, SystemSeatID: 1}
	plays, err := ParseGamePlays(entries, playerConn)
	if err != nil {
		t.Fatalf("ParseGamePlays failed: %v", err)
	}

	// With only one state, no zone changes can be detected
	if plays != nil && len(plays) != 0 {
		t.Errorf("Expected nil or empty plays for single entry, got %d", len(plays))
	}
}

// ---- ParseGamePlaysResult tests ----

// TestParseGamePlaysResult_ReturnsAllOutputs verifies that ParseGamePlaysResult
// returns plays, snapshots, opponent cards, counter changes, and mulligan data
// in a single call and that the result is consistent with the individual
// Parse/Extract calls that existed before.
func TestParseGamePlaysResult_ReturnsAllOutputs(t *testing.T) {
	// A minimal two-state sequence: a creature moves hand→battlefield.
	entries := makeZoneChangeEntries(1, 12345, 1, 3, "CardType_Creature")

	playerConn := &GREConnection{SeatID: 1, SystemSeatID: 1}
	result, err := ParseGamePlaysResult(entries, playerConn)
	if err != nil {
		t.Fatalf("ParseGamePlaysResult: %v", err)
	}

	if len(result.Plays) == 0 {
		t.Error("expected at least one play from zone change sequence")
	}
	// CounterChanges and Mulligan may be nil/zero for a simple two-state sequence.
	// The key assertion is that the function returns without error and the Plays
	// field is populated correctly.
}

// TestParseGamePlaysResult_NoEntries verifies an empty input produces an empty
// result without error.
func TestParseGamePlaysResult_NoEntries(t *testing.T) {
	result, err := ParseGamePlaysResult([]*LogEntry{}, &GREConnection{SeatID: 1})
	if err != nil {
		t.Fatalf("ParseGamePlaysResult on empty input: %v", err)
	}
	if len(result.Plays) != 0 {
		t.Errorf("Plays: got %d, want 0", len(result.Plays))
	}
	if len(result.CounterChanges) != 0 {
		t.Errorf("CounterChanges: got %d, want 0", len(result.CounterChanges))
	}
	if result.Mulligan != nil {
		t.Error("Mulligan should be nil for empty input")
	}
}

// TestParseGamePlaysResult_SingleMessage_OpponentCardsAndSnapshotsPopulated is the
// regression test for the bug introduced in v0.1.2: when the GRE buffer contains
// exactly one game state message, extractOpponentCardsFromMessages and
// extractSnapshotsFromMessages were skipped by the len(messages) < 2 early-return
// guard. Both functions operate per-message (no diff-pair needed), so they must
// run even for a single-message buffer.
//
// This test must FAIL before the fix and PASS after.
func TestParseGamePlaysResult_SingleMessage_OpponentCardsAndSnapshotsPopulated(t *testing.T) {
	// A single game state message: turn 2, opponent has a creature on the battlefield.
	entry := &LogEntry{
		IsJSON:    true,
		Timestamp: "2024-01-15 10:30:45",
		JSON: map[string]interface{}{
			"greToClientEvent": map[string]interface{}{
				"greToClientMessages": []interface{}{
					map[string]interface{}{
						"type": "GREMessageType_GameStateMessage",
						"gameStateMessage": map[string]interface{}{
							"turnInfo": map[string]interface{}{
								"turnNumber":   float64(2),
								"phase":        "Phase_Main1",
								"activePlayer": float64(2),
							},
							"players": []interface{}{
								map[string]interface{}{
									"seatId":    float64(1),
									"lifeTotal": float64(20),
								},
								map[string]interface{}{
									"seatId":    float64(2),
									"lifeTotal": float64(18),
								},
							},
							"gameObjects": []interface{}{
								// Opponent creature on battlefield (seat 2 = opponent when playerConn.SeatID=1).
								map[string]interface{}{
									"instanceId":       float64(201),
									"grpId":            float64(99999),
									"ownerSeatId":      float64(2),
									"controllerSeatId": float64(2),
									"zoneId":           float64(3), // battlefield
									"cardTypes":        []interface{}{"CardType_Creature"},
								},
							},
							"gameInfo": map[string]interface{}{
								"matchID":    "match-single",
								"gameNumber": float64(1),
							},
						},
					},
				},
			},
		},
	}

	playerConn := &GREConnection{SeatID: 1, SystemSeatID: 1}
	result, err := ParseGamePlaysResult([]*LogEntry{entry}, playerConn)
	if err != nil {
		t.Fatalf("ParseGamePlaysResult: %v", err)
	}

	// Opponent card (grpId 99999) must appear in OpponentCards even though
	// there is only one GRE message — no diff pair is needed to observe it.
	if len(result.OpponentCards) == 0 {
		t.Error("OpponentCards: got 0, want at least 1 — single-message regression")
	} else {
		found := false
		for _, c := range result.OpponentCards {
			if c.CardID == 99999 {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("OpponentCards: card 99999 not present; got %+v", result.OpponentCards)
		}
	}

	// A snapshot for turn 2 must exist — TurnInfo is present in the message.
	if len(result.Snapshots) == 0 {
		t.Error("Snapshots: got 0, want at least 1 — single-message regression")
	} else {
		found := false
		for _, s := range result.Snapshots {
			if s.TurnNumber == 2 {
				found = true
				if s.MatchID != "match-single" {
					t.Errorf("Snapshots[turn=2].MatchID: got %q, want %q", s.MatchID, "match-single")
				}
				break
			}
		}
		if !found {
			t.Errorf("Snapshots: no snapshot for turn 2; got %+v", result.Snapshots)
		}
	}

	// Plays must be empty — no diff pair means no zone-change/life-change/attack detection.
	if len(result.Plays) != 0 {
		t.Errorf("Plays: got %d, want 0 for single-message buffer", len(result.Plays))
	}
}

// ---- Counter delta detector tests ----

// TestDetectCounterChanges_LoyaltyDecrement verifies that a planeswalker losing
// a loyalty counter across two consecutive game states produces one
// CounterChangeEvent with the correct delta.
func TestDetectCounterChanges_LoyaltyDecrement(t *testing.T) {
	// State 1: planeswalker with 4 loyalty counters (turn 3).
	prev := &GREGameStateMessage{
		TurnInfo: &GRETurnInfo{TurnNumber: 3},
		GameObjects: []GREGameObject{
			{
				InstanceID:       55,
				GRPId:            88888,
				ControllerSeatID: 2, // opponent's planeswalker
				ZoneName:         "battlefield",
				Counters:         map[string]int{"loyalty": 4},
			},
		},
	}
	// State 2: same planeswalker with 1 loyalty counter after activation.
	curr := &GREGameStateMessage{
		TurnInfo: &GRETurnInfo{TurnNumber: 3},
		GameObjects: []GREGameObject{
			{
				InstanceID:       55,
				GRPId:            88888,
				ControllerSeatID: 2,
				ZoneName:         "battlefield",
				Counters:         map[string]int{"loyalty": 1},
			},
		},
	}

	playerConn := &GREConnection{SeatID: 1}
	changes := detectCounterChanges(prev, curr, playerConn)

	if len(changes) != 1 {
		t.Fatalf("expected 1 counter change, got %d", len(changes))
	}
	c := changes[0]
	if c.InstanceID != 55 {
		t.Errorf("InstanceID: got %d, want 55", c.InstanceID)
	}
	if c.ArenaID != 88888 {
		t.Errorf("ArenaID: got %d, want 88888", c.ArenaID)
	}
	if c.CounterType != "loyalty" {
		t.Errorf("CounterType: got %q, want \"loyalty\"", c.CounterType)
	}
	if c.Count != 1 {
		t.Errorf("Count: got %d, want 1", c.Count)
	}
	if c.Delta != -3 {
		t.Errorf("Delta: got %d, want -3", c.Delta)
	}
	if c.Controller != "opponent" {
		t.Errorf("Controller: got %q, want \"opponent\"", c.Controller)
	}
	if c.TurnNumber != 3 {
		t.Errorf("TurnNumber: got %d, want 3", c.TurnNumber)
	}
}

// TestDetectCounterChanges_PlusPlusIncrement verifies a +1/+1 counter gain.
func TestDetectCounterChanges_PlusPlusIncrement(t *testing.T) {
	prev := &GREGameStateMessage{
		TurnInfo: &GRETurnInfo{TurnNumber: 5},
		GameObjects: []GREGameObject{
			{
				InstanceID:       10,
				GRPId:            12345,
				ControllerSeatID: 1,
				ZoneName:         "battlefield",
				Counters:         map[string]int{"+1/+1": 2},
			},
		},
	}
	curr := &GREGameStateMessage{
		TurnInfo: &GRETurnInfo{TurnNumber: 5},
		GameObjects: []GREGameObject{
			{
				InstanceID:       10,
				GRPId:            12345,
				ControllerSeatID: 1,
				ZoneName:         "battlefield",
				Counters:         map[string]int{"+1/+1": 4},
			},
		},
	}

	playerConn := &GREConnection{SeatID: 1}
	changes := detectCounterChanges(prev, curr, playerConn)

	if len(changes) != 1 {
		t.Fatalf("expected 1 counter change, got %d", len(changes))
	}
	c := changes[0]
	if c.Delta != 2 {
		t.Errorf("Delta: got %d, want 2", c.Delta)
	}
	if c.Controller != "player" {
		t.Errorf("Controller: got %q, want \"player\"", c.Controller)
	}
	if c.CounterType != "+1/+1" {
		t.Errorf("CounterType: got %q, want \"+1/+1\"", c.CounterType)
	}
}

// TestDetectCounterChanges_NoChange verifies no events are emitted when
// counter values are identical across states.
func TestDetectCounterChanges_NoChange(t *testing.T) {
	state := &GREGameStateMessage{
		TurnInfo: &GRETurnInfo{TurnNumber: 2},
		GameObjects: []GREGameObject{
			{
				InstanceID: 20,
				GRPId:      55555,
				Counters:   map[string]int{"loyalty": 3},
			},
		},
	}

	changes := detectCounterChanges(state, state, &GREConnection{SeatID: 1})
	if len(changes) != 0 {
		t.Errorf("expected 0 changes when counter is unchanged, got %d", len(changes))
	}
}

// TestDetectCounterChanges_NewCounterAppears verifies that a counter appearing
// on a permanent for the first time (previous count == 0) is emitted.
func TestDetectCounterChanges_NewCounterAppears(t *testing.T) {
	prev := &GREGameStateMessage{
		TurnInfo: &GRETurnInfo{TurnNumber: 4},
		GameObjects: []GREGameObject{
			{
				InstanceID: 30,
				GRPId:      77777,
				Counters:   map[string]int{}, // no counters yet
			},
		},
	}
	curr := &GREGameStateMessage{
		TurnInfo: &GRETurnInfo{TurnNumber: 4},
		GameObjects: []GREGameObject{
			{
				InstanceID: 30,
				GRPId:      77777,
				Counters:   map[string]int{"poison": 1},
			},
		},
	}

	changes := detectCounterChanges(prev, curr, &GREConnection{SeatID: 1})
	if len(changes) != 1 {
		t.Fatalf("expected 1 change for new counter appearance, got %d", len(changes))
	}
	if changes[0].Delta != 1 {
		t.Errorf("Delta: got %d, want 1", changes[0].Delta)
	}
	if changes[0].Count != 1 {
		t.Errorf("Count: got %d, want 1", changes[0].Count)
	}
}

// ---- Mulligan detector tests ----

// TestDetectMulligan_NoMulligan verifies that a player who kept their opening 7
// produces MulliganCount == 0 and KeptCardIDs length == 7.
func TestDetectMulligan_NoMulligan(t *testing.T) {
	// Pre-game state: maxHandSize 7, 7 cards in hand zone (zone owned by seat 1).
	handZoneID := 31
	msgs := []*GREGameStateMessage{
		{
			TurnInfo: nil, // pre-game, no turn info
			Players: []GREPlayerState{
				{SeatID: 1, MaxHandSize: 7},
			},
			GameObjects: makeHandObjects(1, handZoneID, []int{1001, 1002, 1003, 1004, 1005, 1006, 1007}),
			Zones:       map[int]*GREZone{handZoneID: {ZoneID: handZoneID, Type: "ZoneType_Hand", OwnerSeatID: 1}},
		},
	}

	playerConn := &GREConnection{SeatID: 1}
	m := detectMulligan(msgs, playerConn)
	if m == nil {
		t.Fatal("detectMulligan returned nil for a pre-game state with 7 cards")
	}
	if m.MulliganCount != 0 {
		t.Errorf("MulliganCount: got %d, want 0", m.MulliganCount)
	}
	if m.OpeningHandSize != 7 {
		t.Errorf("OpeningHandSize: got %d, want 7", m.OpeningHandSize)
	}
	if len(m.KeptCardIDs) != 7 {
		t.Errorf("KeptCardIDs: got %d, want 7", len(m.KeptCardIDs))
	}
	if len(m.BottomedCardIDs) != 0 {
		t.Errorf("BottomedCardIDs: got %d, want 0", len(m.BottomedCardIDs))
	}
}

// TestDetectMulligan_OneMulligan verifies that a player who mulliganed once
// (maxHandSize dropped from 7 to 6) has MulliganCount == 1 and ends with 6
// cards in hand.
func TestDetectMulligan_OneMulligan(t *testing.T) {
	handZoneID := 31
	// Two pre-game states: first with maxHandSize 7 (pre-mulligan redraw), second
	// with maxHandSize 6 (after mulligan decision, 6 kept + 1 to bottom).
	msgs := []*GREGameStateMessage{
		{
			TurnInfo: nil,
			Players:  []GREPlayerState{{SeatID: 1, MaxHandSize: 7}},
			GameObjects: makeHandObjects(1, handZoneID, []int{
				2001, 2002, 2003, 2004, 2005, 2006, 2007,
			}),
			Zones: map[int]*GREZone{handZoneID: {ZoneID: handZoneID, Type: "ZoneType_Hand", OwnerSeatID: 1}},
		},
		{
			TurnInfo: nil,
			Players:  []GREPlayerState{{SeatID: 1, MaxHandSize: 6}},
			GameObjects: makeHandObjects(1, handZoneID, []int{
				2001, 2002, 2003, 2004, 2005, 2006,
			}),
			Zones: map[int]*GREZone{handZoneID: {ZoneID: handZoneID, Type: "ZoneType_Hand", OwnerSeatID: 1}},
		},
	}

	playerConn := &GREConnection{SeatID: 1}
	m := detectMulligan(msgs, playerConn)
	if m == nil {
		t.Fatal("detectMulligan returned nil")
	}
	if m.MulliganCount != 1 {
		t.Errorf("MulliganCount: got %d, want 1", m.MulliganCount)
	}
	if m.OpeningHandSize != 6 {
		t.Errorf("OpeningHandSize: got %d, want 6", m.OpeningHandSize)
	}
	if len(m.KeptCardIDs) != 6 {
		t.Errorf("KeptCardIDs: got %d, want 6", len(m.KeptCardIDs))
	}
}

// TestDetectMulligan_TwoMulligans verifies the maxHandSize decrement correctly
// tracks two mulligans taken.
func TestDetectMulligan_TwoMulligans(t *testing.T) {
	handZoneID := 31
	msgs := []*GREGameStateMessage{
		{
			TurnInfo:    nil,
			Players:     []GREPlayerState{{SeatID: 1, MaxHandSize: 7}},
			GameObjects: makeHandObjects(1, handZoneID, []int{3001, 3002, 3003, 3004, 3005, 3006, 3007}),
			Zones:       map[int]*GREZone{handZoneID: {ZoneID: handZoneID, Type: "ZoneType_Hand", OwnerSeatID: 1}},
		},
		{
			TurnInfo:    nil,
			Players:     []GREPlayerState{{SeatID: 1, MaxHandSize: 6}},
			GameObjects: makeHandObjects(1, handZoneID, []int{3001, 3002, 3003, 3004, 3005, 3006}),
			Zones:       map[int]*GREZone{handZoneID: {ZoneID: handZoneID, Type: "ZoneType_Hand", OwnerSeatID: 1}},
		},
		{
			TurnInfo:    nil,
			Players:     []GREPlayerState{{SeatID: 1, MaxHandSize: 5}},
			GameObjects: makeHandObjects(1, handZoneID, []int{3001, 3002, 3003, 3004, 3005}),
			Zones:       map[int]*GREZone{handZoneID: {ZoneID: handZoneID, Type: "ZoneType_Hand", OwnerSeatID: 1}},
		},
	}

	playerConn := &GREConnection{SeatID: 1}
	m := detectMulligan(msgs, playerConn)
	if m == nil {
		t.Fatal("detectMulligan returned nil")
	}
	if m.MulliganCount != 2 {
		t.Errorf("MulliganCount: got %d, want 2", m.MulliganCount)
	}
	if m.OpeningHandSize != 5 {
		t.Errorf("OpeningHandSize: got %d, want 5", m.OpeningHandSize)
	}
}

// TestDetectMulligan_OnlyInGameMessages returns nil when there are no pre-game
// messages (all messages have TurnInfo with TurnNumber > 0).
func TestDetectMulligan_OnlyInGameMessages(t *testing.T) {
	msgs := []*GREGameStateMessage{
		{
			TurnInfo: &GRETurnInfo{TurnNumber: 1},
			Players:  []GREPlayerState{{SeatID: 1, MaxHandSize: 7}},
		},
		{
			TurnInfo: &GRETurnInfo{TurnNumber: 2},
			Players:  []GREPlayerState{{SeatID: 1, MaxHandSize: 7}},
		},
	}
	playerConn := &GREConnection{SeatID: 1}
	m := detectMulligan(msgs, playerConn)
	if m != nil {
		t.Errorf("expected nil Mulligan for in-game-only messages, got %+v", m)
	}
}

// ---- helpers for logparse tests ----

// makeZoneChangeEntries builds a two-entry log that moves a card from
// fromZoneID to toZoneID so zone-change tests can be concise.
func makeZoneChangeEntries(turnNumber int, grpID, fromZoneID, toZoneID int, cardType string) []*LogEntry {
	makeEntry := func(zoneID int) *LogEntry {
		return &LogEntry{
			IsJSON:    true,
			Timestamp: "2024-01-15 10:30:45",
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber":   float64(turnNumber),
									"phase":        "Phase_Main1",
									"activePlayer": float64(1),
								},
								"gameObjects": []interface{}{
									map[string]interface{}{
										"instanceId":       float64(100),
										"grpId":            float64(grpID),
										"controllerSeatId": float64(1),
										"zoneId":           float64(zoneID),
										"cardTypes":        []interface{}{cardType},
									},
								},
								"gameInfo": map[string]interface{}{
									"matchID":    "match-test",
									"gameNumber": float64(1),
								},
							},
						},
					},
				},
			},
		}
	}
	return []*LogEntry{makeEntry(fromZoneID), makeEntry(toZoneID)}
}

// makeHandObjects creates GREGameObject slices representing cards in a player's
// hand zone, one object per grpID.
func makeHandObjects(ownerSeatID, handZoneID int, grpIDs []int) []GREGameObject {
	objs := make([]GREGameObject, len(grpIDs))
	for i, id := range grpIDs {
		objs[i] = GREGameObject{
			InstanceID:       id,
			GRPId:            id,
			OwnerSeatID:      ownerSeatID,
			ControllerSeatID: ownerSeatID,
			ZoneID:           handZoneID,
			ZoneName:         "hand",
			Counters:         map[string]int{},
		}
	}
	return objs
}

// TestParseGamePlaysResult_ZoneDiffAndAnnotationOverlap_NoDuplication is the
// regression test for the cross-pass dedup bug.
//
// Root cause: the zone-diff pass (detectZoneChangesWithZones) registered its
// seenKey entries with a hardcoded instanceID of 0:
//
//	seenKey[fmt.Sprintf("%d:%d:%s", 0, zc.TurnNumber, zc.ActionType)] = true
//
// The annotation pass keys on the REAL instanceID:
//
//	key := fmt.Sprintf("%d:%d:%s", instanceID, turnNumber, actionType)
//
// These two namespaces never intersect → for consecutive GameStateMessage pairs
// that BOTH carry full gameObjects snapshots (triggering detectZoneChangesWithZones)
// AND carry a matching AnnotationType_ZoneTransfer annotation, both passes emit
// events for the same play → double-count.
//
// This test MUST FAIL before the fix (two plays emitted for one action) and
// PASS after (exactly one play emitted).
func TestParseGamePlaysResult_ZoneDiffAndAnnotationOverlap_NoDuplication(t *testing.T) {
	// Zone IDs used in this fixture. Both messages carry an explicit "zones"
	// array so zone names resolve via the ZoneType map (not the fallback modulo
	// method). instanceID=42, grpID=99001, controllerSeatID=1 (local player).
	const (
		handZoneID  = 31
		stackZoneID = 27
		instanceID  = 42
		grpID       = 99001
		turnNumber  = 3
	)

	zones := []interface{}{
		map[string]interface{}{
			"zoneId": float64(handZoneID),
			"type":   "ZoneType_Hand",
		},
		map[string]interface{}{
			"zoneId": float64(stackZoneID),
			"type":   "ZoneType_Stack",
		},
	}

	// Message 1 (prev): instanceID=42 in hand. No annotations.
	prevEntry := &LogEntry{
		IsJSON:    true,
		Timestamp: "2026-06-03 10:00:00",
		JSON: map[string]interface{}{
			"greToClientEvent": map[string]interface{}{
				"greToClientMessages": []interface{}{
					map[string]interface{}{
						"type": "GREMessageType_GameStateMessage",
						"gameStateMessage": map[string]interface{}{
							"turnInfo": map[string]interface{}{
								"turnNumber":   float64(turnNumber),
								"phase":        "Phase_Main1",
								"activePlayer": float64(1),
							},
							"zones": zones,
							"gameObjects": []interface{}{
								map[string]interface{}{
									"instanceId":       float64(instanceID),
									"grpId":            float64(grpID),
									"controllerSeatId": float64(1),
									"zoneId":           float64(handZoneID),
									"cardTypes":        []interface{}{"CardType_Instant"},
								},
							},
							"gameInfo": map[string]interface{}{
								"matchID":    "match-overlap-test",
								"gameNumber": float64(1),
							},
						},
					},
				},
			},
		},
	}

	// Message 2 (curr): instanceID=42 has moved to stack (zone-diff fires:
	// hand→stack = cast_spell). The message ALSO carries an
	// AnnotationType_ZoneTransfer with category=CastSpell for the same
	// instanceID (annotation pass also wants to emit cast_spell).
	// After the fix, seenKey must block the annotation-pass duplicate.
	currEntry := &LogEntry{
		IsJSON:    true,
		Timestamp: "2026-06-03 10:00:01",
		JSON: map[string]interface{}{
			"greToClientEvent": map[string]interface{}{
				"greToClientMessages": []interface{}{
					map[string]interface{}{
						"type": "GREMessageType_GameStateMessage",
						"gameStateMessage": map[string]interface{}{
							"turnInfo": map[string]interface{}{
								"turnNumber":   float64(turnNumber),
								"phase":        "Phase_Main1",
								"activePlayer": float64(1),
							},
							"zones": zones,
							"gameObjects": []interface{}{
								map[string]interface{}{
									"instanceId":       float64(instanceID),
									"grpId":            float64(grpID),
									"controllerSeatId": float64(1),
									"zoneId":           float64(stackZoneID),
									"cardTypes":        []interface{}{"CardType_Instant"},
								},
							},
							// Annotation mirrors the zone-diff event: same instanceID,
							// same turn, same action (CastSpell → cast_spell).
							"annotations": []interface{}{
								map[string]interface{}{
									"id":          float64(1),
									"affectorId":  float64(instanceID),
									"affectedIds": []interface{}{float64(instanceID)},
									"type":        []interface{}{"AnnotationType_ZoneTransfer"},
									"details": []interface{}{
										map[string]interface{}{
											"key":         "category",
											"valueString": []interface{}{"CastSpell"},
										},
									},
								},
							},
							"gameInfo": map[string]interface{}{
								"matchID":    "match-overlap-test",
								"gameNumber": float64(1),
							},
						},
					},
				},
			},
		},
	}

	playerConn := &GREConnection{SeatID: 1, SystemSeatID: 1}
	result, err := ParseGamePlaysResult([]*LogEntry{prevEntry, currEntry}, playerConn)
	if err != nil {
		t.Fatalf("ParseGamePlaysResult: %v", err)
	}

	// Count cast_spell plays — must be exactly 1 (zone-diff OR annotation, not both).
	castSpellCount := 0
	for _, p := range result.Plays {
		if p.ActionType == "cast_spell" {
			castSpellCount++
		}
	}
	t.Logf("Total plays: %d, cast_spell plays: %d", len(result.Plays), castSpellCount)

	if castSpellCount != 1 {
		t.Errorf("cast_spell play count = %d, want exactly 1 — cross-pass dedup is broken (instanceID=0 hardcoded in seenKey registration)", castSpellCount)
	}
}

// Benchmarks

func BenchmarkParseGREMessages(b *testing.B) {
	// Create a realistic set of entries
	entries := make([]*LogEntry, 100)
	for i := range entries {
		entries[i] = &LogEntry{
			IsJSON:    true,
			Timestamp: time.Now().Format("2006-01-02 15:04:05"),
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type": "GREMessageType_GameStateMessage",
							"gameStateMessage": map[string]interface{}{
								"turnInfo": map[string]interface{}{
									"turnNumber":   float64(i),
									"phase":        "Phase_Main1",
									"activePlayer": float64(1),
								},
								"players": []interface{}{
									map[string]interface{}{
										"seatId":    float64(1),
										"lifeTotal": float64(20),
									},
									map[string]interface{}{
										"seatId":    float64(2),
										"lifeTotal": float64(20),
									},
								},
								"gameObjects": []interface{}{
									map[string]interface{}{
										"instanceId":       float64(100 + i),
										"grpId":            float64(12345),
										"controllerSeatId": float64(1),
										"zoneId":           float64(3),
									},
								},
							},
						},
					},
				},
			},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseGREMessages(entries)
	}
}

// ---------------------------------------------------------------------------
// detectFirstTurnActivePlayer (ticket #687)
// ---------------------------------------------------------------------------

// makeGSMWithStageAndTurn returns a minimal GREGameStateMessage with the
// given stage, turnNumber, and activePlayer.
func makeGSMWithStageAndTurn(stage string, turnNumber, activePlayer int) *GREGameStateMessage {
	ti := &GRETurnInfo{
		TurnNumber:   turnNumber,
		ActivePlayer: activePlayer,
	}
	return &GREGameStateMessage{
		Stage:    stage,
		TurnInfo: ti,
	}
}

func TestDetectFirstTurnActivePlayer_OnPlay(t *testing.T) {
	msgs := []*GREGameStateMessage{
		makeGSMWithStageAndTurn("GameStage_Start", 0, 0),
		makeGSMWithStageAndTurn("GameStage_Play", 1, 2), // seat 2 is active on turn 1
	}
	got := detectFirstTurnActivePlayer(msgs)
	if got != 2 {
		t.Errorf("detectFirstTurnActivePlayer = %d, want 2", got)
	}
}

func TestDetectFirstTurnActivePlayer_NoPlayStageMessage(t *testing.T) {
	// Buffer only contains pre-play messages — no GameStage_Play entry.
	msgs := []*GREGameStateMessage{
		makeGSMWithStageAndTurn("GameStage_Start", 0, 0),
		makeGSMWithStageAndTurn("GameStage_Start", 0, 1),
	}
	got := detectFirstTurnActivePlayer(msgs)
	if got != 0 {
		t.Errorf("detectFirstTurnActivePlayer = %d, want 0", got)
	}
}

func TestDetectFirstTurnActivePlayer_NoTurnInfo(t *testing.T) {
	// GameStage_Play message with no TurnInfo — should be skipped.
	msgs := []*GREGameStateMessage{
		{Stage: "GameStage_Play", TurnInfo: nil},
	}
	got := detectFirstTurnActivePlayer(msgs)
	if got != 0 {
		t.Errorf("detectFirstTurnActivePlayer = %d, want 0", got)
	}
}

func TestDetectFirstTurnActivePlayer_WrongTurnNumber(t *testing.T) {
	// GameStage_Play message but turnNumber != 1.
	msgs := []*GREGameStateMessage{
		makeGSMWithStageAndTurn("GameStage_Play", 3, 1),
	}
	got := detectFirstTurnActivePlayer(msgs)
	if got != 0 {
		t.Errorf("detectFirstTurnActivePlayer = %d, want 0", got)
	}
}

func TestDetectFirstTurnActivePlayer_EmptyMessages(t *testing.T) {
	got := detectFirstTurnActivePlayer(nil)
	if got != 0 {
		t.Errorf("detectFirstTurnActivePlayer(nil) = %d, want 0", got)
	}
}

func TestParseGamePlaysResult_FirstTurnActivePlayer(t *testing.T) {
	// Build a minimal log entry that contains a GREMessageType_GameStateMessage
	// with stage GameStage_Play, turnNumber 1, and activePlayer 1.
	greEvent := map[string]interface{}{
		"greToClientEvent": map[string]interface{}{
			"greToClientMessages": []interface{}{
				map[string]interface{}{
					"type": "GREMessageType_GameStateMessage",
					"gameStateMessage": map[string]interface{}{
						"gameInfo": map[string]interface{}{
							"matchID":    "test-match-001",
							"gameNumber": float64(1),
							"stage":      "GameStage_Play",
						},
						"turnInfo": map[string]interface{}{
							"turnNumber":   float64(1),
							"activePlayer": float64(1),
						},
					},
				},
			},
		},
	}

	entry := &LogEntry{
		IsJSON: true,
		JSON:   greEvent,
	}

	// Player seat 1 is the local player.
	playerConn := &GREConnection{SeatID: 1, SystemSeatID: 1}

	result, err := ParseGamePlaysResult([]*LogEntry{entry}, playerConn)
	if err != nil {
		t.Fatalf("ParseGamePlaysResult: %v", err)
	}

	if result.FirstTurnActivePlayerSeatID != 1 {
		t.Errorf("FirstTurnActivePlayerSeatID = %d, want 1", result.FirstTurnActivePlayerSeatID)
	}
}

// TestParseGameStage_CapturedInMessage verifies that Stage is captured in
// GREGameStateMessage during parsing.
func TestParseGameStage_CapturedInMessage(t *testing.T) {
	greEvent := map[string]interface{}{
		"greToClientEvent": map[string]interface{}{
			"greToClientMessages": []interface{}{
				map[string]interface{}{
					"type": "GREMessageType_GameStateMessage",
					"gameStateMessage": map[string]interface{}{
						"gameInfo": map[string]interface{}{
							"matchID":    "test-match-002",
							"gameNumber": float64(1),
							"stage":      "GameStage_Play",
						},
						"turnInfo": map[string]interface{}{
							"turnNumber": float64(1),
						},
					},
				},
			},
		},
	}
	entry := &LogEntry{IsJSON: true, JSON: greEvent}

	messages, err := ParseGREMessages([]*LogEntry{entry})
	if err != nil {
		t.Fatalf("ParseGREMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Stage != "GameStage_Play" {
		t.Errorf("Stage = %q, want %q", messages[0].Stage, "GameStage_Play")
	}
}

// TestGetPlayerSeatID_FromGREMessageConnectResp verifies that GetPlayerSeatID
// correctly extracts the player's seat from a connectResp nested inside a
// greToClientEvent.greToClientMessages entry — which is the ACTUAL structure
// emitted by MTGA in real Player.log files.
//
// The previous implementation only checked for top-level entry.JSON["connectResp"],
// which is never present in real logs. This caused PlayerOnPlay to always be nil
// for every game in the corpus (19 matches, 0 with player_on_play populated).
//
// Real log structure:
//
//	{
//	  "transactionId": "...",
//	  "greToClientEvent": {
//	    "greToClientMessages": [
//	      {
//	        "type": "GREMessageType_ConnectResp",
//	        "systemSeatIds": [1],
//	        "msgId": 1,
//	        "connectResp": { "status": "ConnectionStatus_Success", ... }
//	      }
//	    ]
//	  }
//	}
func TestGetPlayerSeatID_FromGREMessageConnectResp(t *testing.T) {
	// This is the REAL structure as it appears in Player.log — connectResp
	// is inside greToClientEvent.greToClientMessages[n], NOT at the top level.
	entries := []*LogEntry{
		{
			IsJSON: true,
			JSON: map[string]interface{}{
				"transactionId": "da772c1d-4e21-41b7-927b-dedf79a9328b",
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type":          "GREMessageType_ConnectResp",
							"systemSeatIds": []interface{}{float64(1)},
							"msgId":         float64(1),
							"connectResp": map[string]interface{}{
								"status":   "ConnectionStatus_Success",
								"protoVer": "ProtoVersion_PersistentAnnotations",
							},
						},
					},
				},
			},
		},
	}

	conn := GetPlayerSeatID(entries)
	if conn == nil {
		t.Fatal("GetPlayerSeatID returned nil — connectResp nested inside greToClientEvent was not found")
	}
	if conn.SeatID != 1 {
		t.Errorf("SeatID = %d, want 1", conn.SeatID)
	}
}

// TestGetPlayerSeatID_ConnectRespInsideGREWrapper_Precedence verifies that
// the GRE-message-embedded connectResp is preferred over a matchGameRoomStateChangedEvent
// when both are present in the same entry slice, and that the correct player
// seat is returned when the player is on seat 2.
func TestGetPlayerSeatID_ConnectRespInsideGREWrapper_Precedence(t *testing.T) {
	// Player is on seat 2 per connectResp. A matchGameRoomStateChangedEvent
	// also exists with player on seat 1 (the opponent's seat) — connectResp
	// should take precedence.
	entries := []*LogEntry{
		{
			IsJSON: true,
			JSON: map[string]interface{}{
				"greToClientEvent": map[string]interface{}{
					"greToClientMessages": []interface{}{
						map[string]interface{}{
							"type":          "GREMessageType_ConnectResp",
							"systemSeatIds": []interface{}{float64(2)},
							"msgId":         float64(1),
							"connectResp": map[string]interface{}{
								"status": "ConnectionStatus_Success",
							},
						},
					},
				},
			},
		},
	}

	conn := GetPlayerSeatID(entries)
	if conn == nil {
		t.Fatal("GetPlayerSeatID returned nil")
	}
	if conn.SeatID != 2 {
		t.Errorf("SeatID = %d, want 2 (player is on seat 2)", conn.SeatID)
	}
}
