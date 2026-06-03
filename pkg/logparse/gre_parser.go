package logparse

import (
	"fmt"
	"time"
)

// GREGameStateMessage represents a parsed game state message from the GRE.
type GREGameStateMessage struct {
	MatchID       string
	GameNumber    int
	Stage         string // gameInfo.stage, e.g. "GameStage_Play", "GameStage_GameOver"
	TurnInfo      *GRETurnInfo
	Players       []GREPlayerState
	GameObjects   []GREGameObject
	Zones         map[int]*GREZone     // Zone ID to zone info mapping
	Annotations   []GREAnnotation      // Per-event annotations; populated by parseGameStateMessage
	PrevGameState *GREGameStateMessage // For comparing state changes
	Timestamp     time.Time
}

// GREAnnotation is one annotation entry from a GRE GameStateMessage.
// Annotations carry explicit action semantics (e.g. ZoneTransfer.category =
// "CastSpell" / "PlayLand" / "Resolve") and complement the object-snapshot
// diff used by detectZoneChangesWithZones.
type GREAnnotation struct {
	ID          int
	AffectorID  int
	AffectedIDs []int
	Types       []string
	// Details is a flat key→value map derived from the annotation's details
	// array. Integer values are stored as int; string values as string.
	Details map[string]interface{}
}

// GREZone represents a zone in the game.
type GREZone struct {
	ZoneID      int
	Type        string // e.g., "ZoneType_Hand", "ZoneType_Battlefield"
	Visibility  string
	OwnerSeatID int // 0 if shared zone (battlefield, stack, etc.)
}

// GRETurnInfo contains information about the current turn.
type GRETurnInfo struct {
	TurnNumber          int
	Phase               string // "Phase_Main1", "Phase_Combat", etc.
	Step                string // "Step_BeginCombat", "Step_DeclareAttackers", etc.
	ActivePlayer        int    // Seat ID of the active player
	PriorityPlayer      int    // Seat ID of player with priority
	DecisionPlayer      int    // Seat ID of player making a decision
	NextPhase           string
	NextStep            string
	StormCount          int
	ManaSpent           int
	PhasePaymentOptions []interface{}
}

// GREPlayerState represents a player's state in the game.
type GREPlayerState struct {
	SeatID          int
	LifeTotal       int
	TeamID          int
	MaxHandSize     int
	PendingMessages int
	TimerState      string
	TimeRemaining   int
	SystemSeatID    int // For identifying player vs opponent
}

// GREGameObject represents an object in the game (card, token, etc.).
type GREGameObject struct {
	InstanceID           int
	GRPId                int    // Arena card ID
	OwnerSeatID          int    // Who owns this object
	ControllerSeatID     int    // Who controls this object
	ZoneID               int    // Current zone
	ZoneName             string // Derived from ZoneID
	CardTypes            []string
	Subtypes             []string
	SuperTypes           []string
	Power                int
	Toughness            int
	IsTapped             bool
	IsAttacking          bool
	IsBlocking           bool
	HasSummoningSickness bool
	Counters             map[string]int
	Abilities            []int
}

// GamePlayEvent represents a detected game play/action.
type GamePlayEvent struct {
	MatchID        string
	GameNumber     int
	TurnNumber     int
	Phase          string
	Step           string
	PlayerType     string // "player" or "opponent"
	TeamID         int    // MTGA teamId from GREPlayerState; populated for life_change events
	ActionType     string // "play_card", "attack", "block", "land_drop", "life_change", etc.
	CardID         int    // Arena card ID (GRPId)
	InstanceID     int    // GRE instanceId; used for cross-pass deduplication
	CardName       string // Will be populated later from card database
	ZoneFrom       string
	ZoneTo         string
	LifeFrom       int // Previous life total (for life_change events)
	LifeTo         int // New life total (for life_change events)
	Timestamp      time.Time
	SequenceNumber int
}

// GREConnection stores the player's connection info for seat identification.
type GREConnection struct {
	SeatID       int
	SystemSeatID int
	TeamID       int
}

// GetPlayerSeatID extracts the player's seat ID from connectResp messages.
// This is used to identify which objects belong to the player vs opponent.
// If playerScreenName is provided, it matches by name; otherwise falls back to connectResp.
func GetPlayerSeatID(entries []*LogEntry) *GREConnection {
	return GetPlayerSeatIDByName(entries, "")
}

// GetPlayerSeatIDByName extracts the player's seat ID, matching by screen name if provided.
// This ensures correct player identification even when player order varies in reservedPlayers.
func GetPlayerSeatIDByName(entries []*LogEntry, playerScreenName string) *GREConnection {
	for _, entry := range entries {
		if !entry.IsJSON {
			continue
		}

		// Primary: look for GREMessageType_ConnectResp inside the greToClientEvent wrapper.
		// Real MTGA Player.log lines carry connectResp as a nested GRE message:
		//   { "greToClientEvent": { "greToClientMessages": [
		//       { "type": "GREMessageType_ConnectResp", "systemSeatIds": [N], "connectResp": {...} }
		//   ]}}
		// The systemSeatIds field at the message level (not inside connectResp) is the
		// authoritative player seat number.
		if greEvt, ok := entry.JSON["greToClientEvent"].(map[string]interface{}); ok {
			if msgs, ok := greEvt["greToClientMessages"].([]interface{}); ok {
				for _, m := range msgs {
					msg, ok := m.(map[string]interface{})
					if !ok {
						continue
					}
					if msg["type"] != "GREMessageType_ConnectResp" {
						continue
					}
					conn := &GREConnection{}
					// systemSeatIds is at the message level (not inside connectResp).
					if seatIDs, ok := msg["systemSeatIds"].([]interface{}); ok && len(seatIDs) > 0 {
						if seatID, ok := seatIDs[0].(float64); ok {
							conn.SystemSeatID = int(seatID)
							conn.SeatID = int(seatID)
						}
					}
					// teamId may be inside connectResp.
					if cr, ok := msg["connectResp"].(map[string]interface{}); ok {
						if teamID, ok := cr["teamId"].(float64); ok {
							conn.TeamID = int(teamID)
						}
					}
					if conn.SeatID != 0 || conn.SystemSeatID != 0 {
						return conn
					}
				}
			}
		}

		// Legacy / test-fixture path: top-level connectResp key.
		// Synthetic test entries may place connectResp at the top level of the JSON
		// object for brevity. Real MTGA logs do not use this shape.
		if connectResp, ok := entry.JSON["connectResp"]; ok {
			connMap, ok := connectResp.(map[string]interface{})
			if !ok {
				continue
			}

			conn := &GREConnection{}

			// Get system seat IDs from connectResp - this is always the player's seat
			if seatIDs, ok := connMap["systemSeatIds"].([]interface{}); ok && len(seatIDs) > 0 {
				if seatID, ok := seatIDs[0].(float64); ok {
					conn.SystemSeatID = int(seatID)
					conn.SeatID = int(seatID)
				}
			}

			// Try to get teamId
			if teamID, ok := connMap["teamId"].(float64); ok {
				conn.TeamID = int(teamID)
			}

			if conn.SeatID != 0 || conn.SystemSeatID != 0 {
				return conn
			}
		}

		// Also look for matchGameRoomStateChangedEvent for seat info
		if matchEvent, ok := entry.JSON["matchGameRoomStateChangedEvent"]; ok {
			eventMap, ok := matchEvent.(map[string]interface{})
			if !ok {
				continue
			}

			gameRoomInfo, ok := eventMap["gameRoomInfo"].(map[string]interface{})
			if !ok {
				continue
			}

			gameRoomConfig, ok := gameRoomInfo["gameRoomConfig"].(map[string]interface{})
			if !ok {
				continue
			}

			reservedPlayers, ok := gameRoomConfig["reservedPlayers"].([]interface{})
			if !ok {
				continue
			}

			// Search through all players to find the matching one
			for _, p := range reservedPlayers {
				player, ok := p.(map[string]interface{})
				if !ok {
					continue
				}

				// If we have a screen name, match by playerName
				if playerScreenName != "" {
					pName, hasName := player["playerName"].(string)
					if !hasName || pName != playerScreenName {
						continue // Not a match, try next player
					}
				}

				// Extract connection info
				conn := &GREConnection{}
				if seatID, ok := player["systemSeatId"].(float64); ok {
					conn.SystemSeatID = int(seatID)
					conn.SeatID = int(seatID)
				}
				if teamID, ok := player["teamId"].(float64); ok {
					conn.TeamID = int(teamID)
				}

				// If matching by name, return this player's seat
				if playerScreenName != "" && (conn.SeatID != 0 || conn.SystemSeatID != 0) {
					return conn
				}

				// Without screen name, we can't reliably pick - skip matchGameRoomStateChangedEvent
				// and hope connectResp is found instead
				break
			}
		}
	}

	return nil
}

// ParseGREMessages extracts game state messages from log entries.
func ParseGREMessages(entries []*LogEntry) ([]*GREGameStateMessage, error) {
	var messages []*GREGameStateMessage

	for _, entry := range entries {
		if !entry.IsJSON {
			continue
		}

		// Parse entry timestamp
		entryTime := time.Now()
		if entry.Timestamp != "" {
			if t, err := parseLogTimestamp(entry.Timestamp); err == nil {
				entryTime = t
			}
		}

		// Look for greToClientEvent messages
		if greEvent, ok := entry.JSON["greToClientEvent"]; ok {
			eventMap, ok := greEvent.(map[string]interface{})
			if !ok {
				continue
			}

			greToClientMsgs, ok := eventMap["greToClientMessages"].([]interface{})
			if !ok {
				continue
			}

			for _, msgData := range greToClientMsgs {
				msgMap, ok := msgData.(map[string]interface{})
				if !ok {
					continue
				}

				// Check message type
				msgType, _ := msgMap["type"].(string)
				if msgType != "GREMessageType_GameStateMessage" {
					continue
				}

				msg := parseGameStateMessage(msgMap, entryTime)
				if msg != nil {
					messages = append(messages, msg)
				}
			}
		}
	}

	return messages, nil
}

// parseGameStateMessage parses a single game state message.
func parseGameStateMessage(msgMap map[string]interface{}, timestamp time.Time) *GREGameStateMessage {
	msg := &GREGameStateMessage{
		Timestamp: timestamp,
		Zones:     make(map[int]*GREZone),
	}

	// Get game state info
	gameStateMsg, ok := msgMap["gameStateMessage"].(map[string]interface{})
	if !ok {
		return nil
	}

	// Parse zones first so we can use them when parsing game objects
	if zones, ok := gameStateMsg["zones"].([]interface{}); ok {
		for _, zoneData := range zones {
			zoneMap, ok := zoneData.(map[string]interface{})
			if !ok {
				continue
			}
			zone := parseZone(zoneMap)
			if zone != nil {
				msg.Zones[zone.ZoneID] = zone
			}
		}
	}

	// Parse turn info
	if turnInfo, ok := gameStateMsg["turnInfo"].(map[string]interface{}); ok {
		msg.TurnInfo = parseTurnInfo(turnInfo)
	}

	// Parse players
	if players, ok := gameStateMsg["players"].([]interface{}); ok {
		for _, playerData := range players {
			playerMap, ok := playerData.(map[string]interface{})
			if !ok {
				continue
			}
			player := parsePlayerState(playerMap)
			msg.Players = append(msg.Players, player)
		}
	}

	// Parse game objects (using zones map for name resolution)
	if gameObjects, ok := gameStateMsg["gameObjects"].([]interface{}); ok {
		for _, objData := range gameObjects {
			objMap, ok := objData.(map[string]interface{})
			if !ok {
				continue
			}
			obj := parseGameObjectWithZones(objMap, msg.Zones)
			msg.GameObjects = append(msg.GameObjects, obj)
		}
	}

	// Get game info if available
	if gameInfo, ok := gameStateMsg["gameInfo"].(map[string]interface{}); ok {
		if matchID, ok := gameInfo["matchID"].(string); ok {
			msg.MatchID = matchID
		}
		if gameNumber, ok := gameInfo["gameNumber"].(float64); ok {
			msg.GameNumber = int(gameNumber)
		}
		if stage, ok := gameInfo["stage"].(string); ok {
			msg.Stage = stage
		}
	}

	// Parse annotations — carry explicit action semantics (ZoneTransfer.category,
	// ResolutionStart.grpid, etc.) that the object-snapshot diff can miss when
	// only partial-state messages are present.
	if annotations, ok := gameStateMsg["annotations"].([]interface{}); ok {
		for _, annData := range annotations {
			annMap, ok := annData.(map[string]interface{})
			if !ok {
				continue
			}
			ann := parseAnnotation(annMap)
			msg.Annotations = append(msg.Annotations, ann)
		}
	}

	return msg
}

// parseAnnotation parses a single annotation entry from the GRE game state.
func parseAnnotation(annMap map[string]interface{}) GREAnnotation {
	ann := GREAnnotation{
		Details: make(map[string]interface{}),
	}
	if id, ok := annMap["id"].(float64); ok {
		ann.ID = int(id)
	}
	if affectorID, ok := annMap["affectorId"].(float64); ok {
		ann.AffectorID = int(affectorID)
	}
	if affectedIDs, ok := annMap["affectedIds"].([]interface{}); ok {
		for _, aid := range affectedIDs {
			if id, ok := aid.(float64); ok {
				ann.AffectedIDs = append(ann.AffectedIDs, int(id))
			}
		}
	}
	if types, ok := annMap["type"].([]interface{}); ok {
		for _, t := range types {
			if ts, ok := t.(string); ok {
				ann.Types = append(ann.Types, ts)
			}
		}
	}
	if details, ok := annMap["details"].([]interface{}); ok {
		for _, detData := range details {
			detMap, ok := detData.(map[string]interface{})
			if !ok {
				continue
			}
			key, _ := detMap["key"].(string)
			if key == "" {
				continue
			}
			if vs, ok := detMap["valueInt32"].([]interface{}); ok && len(vs) > 0 {
				if v, ok := vs[0].(float64); ok {
					ann.Details[key] = int(v)
				}
			} else if vs, ok := detMap["valueString"].([]interface{}); ok && len(vs) > 0 {
				if v, ok := vs[0].(string); ok {
					ann.Details[key] = v
				}
			}
		}
	}
	return ann
}

// parseZone parses a zone from the game state.
func parseZone(zoneMap map[string]interface{}) *GREZone {
	zone := &GREZone{}

	if zoneID, ok := zoneMap["zoneId"].(float64); ok {
		zone.ZoneID = int(zoneID)
	} else {
		return nil // Zone ID is required
	}

	if zoneType, ok := zoneMap["type"].(string); ok {
		zone.Type = zoneType
	}
	if visibility, ok := zoneMap["visibility"].(string); ok {
		zone.Visibility = visibility
	}
	if ownerSeatID, ok := zoneMap["ownerSeatId"].(float64); ok {
		zone.OwnerSeatID = int(ownerSeatID)
	}

	return zone
}

// parseTurnInfo parses turn information from the game state.
func parseTurnInfo(turnInfo map[string]interface{}) *GRETurnInfo {
	ti := &GRETurnInfo{}

	if turnNumber, ok := turnInfo["turnNumber"].(float64); ok {
		ti.TurnNumber = int(turnNumber)
	}
	if phase, ok := turnInfo["phase"].(string); ok {
		ti.Phase = phase
	}
	if step, ok := turnInfo["step"].(string); ok {
		ti.Step = step
	}
	if activePlayer, ok := turnInfo["activePlayer"].(float64); ok {
		ti.ActivePlayer = int(activePlayer)
	}
	if priorityPlayer, ok := turnInfo["priorityPlayer"].(float64); ok {
		ti.PriorityPlayer = int(priorityPlayer)
	}
	if decisionPlayer, ok := turnInfo["decisionPlayer"].(float64); ok {
		ti.DecisionPlayer = int(decisionPlayer)
	}
	if nextPhase, ok := turnInfo["nextPhase"].(string); ok {
		ti.NextPhase = nextPhase
	}
	if nextStep, ok := turnInfo["nextStep"].(string); ok {
		ti.NextStep = nextStep
	}

	return ti
}

// parsePlayerState parses a player's state from the game state.
func parsePlayerState(playerMap map[string]interface{}) GREPlayerState {
	ps := GREPlayerState{}

	if seatID, ok := playerMap["seatId"].(float64); ok {
		ps.SeatID = int(seatID)
	}
	if lifeTotal, ok := playerMap["lifeTotal"].(float64); ok {
		ps.LifeTotal = int(lifeTotal)
	}
	if teamID, ok := playerMap["teamId"].(float64); ok {
		ps.TeamID = int(teamID)
	}
	if maxHandSize, ok := playerMap["maxHandSize"].(float64); ok {
		ps.MaxHandSize = int(maxHandSize)
	}
	if systemSeatID, ok := playerMap["systemSeatId"].(float64); ok {
		ps.SystemSeatID = int(systemSeatID)
	}
	if timerState, ok := playerMap["timerState"].(string); ok {
		ps.TimerState = timerState
	}
	if timeRemaining, ok := playerMap["timeRemaining"].(float64); ok {
		ps.TimeRemaining = int(timeRemaining)
	}

	return ps
}

// parseGameObject parses a game object from the game state (legacy, no zones map).
func parseGameObject(objMap map[string]interface{}) GREGameObject {
	return parseGameObjectWithZones(objMap, nil)
}

// parseGameObjectWithZones parses a game object using the zones map for name resolution.
func parseGameObjectWithZones(objMap map[string]interface{}, zones map[int]*GREZone) GREGameObject {
	obj := GREGameObject{
		Counters: make(map[string]int),
	}

	if instanceID, ok := objMap["instanceId"].(float64); ok {
		obj.InstanceID = int(instanceID)
	}
	if grpID, ok := objMap["grpId"].(float64); ok {
		obj.GRPId = int(grpID)
	}
	if ownerSeatID, ok := objMap["ownerSeatId"].(float64); ok {
		obj.OwnerSeatID = int(ownerSeatID)
	}
	if controllerSeatID, ok := objMap["controllerSeatId"].(float64); ok {
		obj.ControllerSeatID = int(controllerSeatID)
	}
	if zoneID, ok := objMap["zoneId"].(float64); ok {
		obj.ZoneID = int(zoneID)
		obj.ZoneName = zoneIDToNameWithMap(int(zoneID), zones)
	}

	// Parse card types
	if cardTypes, ok := objMap["cardTypes"].([]interface{}); ok {
		for _, ct := range cardTypes {
			if ctStr, ok := ct.(string); ok {
				obj.CardTypes = append(obj.CardTypes, ctStr)
			}
		}
	}

	// Parse other attributes
	if power, ok := objMap["power"].(map[string]interface{}); ok {
		if val, ok := power["value"].(float64); ok {
			obj.Power = int(val)
		}
	}
	if toughness, ok := objMap["toughness"].(map[string]interface{}); ok {
		if val, ok := toughness["value"].(float64); ok {
			obj.Toughness = int(val)
		}
	}
	if isTapped, ok := objMap["isTapped"].(bool); ok {
		obj.IsTapped = isTapped
	}
	if attacking, ok := objMap["attackState"].(string); ok {
		obj.IsAttacking = attacking == "AttackState_Attacking"
	}
	if blocking, ok := objMap["blockState"].(string); ok {
		obj.IsBlocking = blocking != "" && blocking != "BlockState_None"
	}

	// Parse counters
	if counters, ok := objMap["counters"].([]interface{}); ok {
		for _, counterData := range counters {
			counterMap, ok := counterData.(map[string]interface{})
			if !ok {
				continue
			}
			if counterType, ok := counterMap["type"].(string); ok {
				if count, ok := counterMap["count"].(float64); ok {
					obj.Counters[counterType] = int(count)
				}
			}
		}
	}

	return obj
}

// zoneIDToNameWithMap maps zone IDs to readable zone names using the zones map.
func zoneIDToNameWithMap(zoneID int, zones map[int]*GREZone) string {
	if zones != nil {
		if zone, ok := zones[zoneID]; ok {
			return zoneTypeToReadableName(zone.Type)
		}
	}
	// Fallback to legacy method if zones map is not available
	return zoneIDToName(zoneID)
}

// zoneTypeToReadableName converts MTGA zone type to readable name.
func zoneTypeToReadableName(zoneType string) string {
	switch zoneType {
	case "ZoneType_Hand":
		return "hand"
	case "ZoneType_Library":
		return "library"
	case "ZoneType_Battlefield":
		return "battlefield"
	case "ZoneType_Graveyard":
		return "graveyard"
	case "ZoneType_Exile":
		return "exile"
	case "ZoneType_Stack":
		return "stack"
	case "ZoneType_Command":
		return "command"
	case "ZoneType_Sideboard":
		return "sideboard"
	case "ZoneType_Revealed":
		return "revealed"
	case "ZoneType_Limbo":
		return "limbo"
	case "ZoneType_Pending":
		return "pending"
	case "ZoneType_Suppressed":
		return "suppressed"
	default:
		// Strip "ZoneType_" prefix if present
		if len(zoneType) > 9 && zoneType[:9] == "ZoneType_" {
			return zoneType[9:]
		}
		return zoneType
	}
}

// zoneIDToName maps zone IDs to readable zone names using legacy modulo method.
// This is a fallback when the zones array is not available.
func zoneIDToName(zoneID int) string {
	// This legacy method uses modulo patterns that don't work reliably
	// for all MTGA zone IDs. Use zoneIDToNameWithMap when zones are available.
	zoneType := zoneID % 10
	switch zoneType {
	case 1:
		return "hand"
	case 2:
		return "library"
	case 3:
		return "battlefield"
	case 4:
		return "graveyard"
	case 5:
		return "exile"
	case 6:
		return "stack"
	case 7:
		return "command"
	default:
		return fmt.Sprintf("zone_%d", zoneID)
	}
}

// ParseGamePlays extracts game plays by comparing consecutive game states.
func ParseGamePlays(entries []*LogEntry, playerConn *GREConnection) ([]*GamePlayEvent, error) {
	messages, err := ParseGREMessages(entries)
	if err != nil {
		return nil, err
	}

	if len(messages) < 2 {
		return nil, nil
	}

	// Build a cumulative zones map from all messages
	// The zones are typically defined in the first full game state message
	cumulativeZones := make(map[int]*GREZone)
	for _, msg := range messages {
		for zoneID, zone := range msg.Zones {
			cumulativeZones[zoneID] = zone
		}
	}

	// Track all game objects across all messages for proper zone transition detection
	allObjects := make(map[int]*trackedObject)

	// Track life totals for life change detection
	lifeTotals := make(map[int]int) // seatID -> life total

	var plays []*GamePlayEvent
	sequenceNum := 0

	// Track the current game number from messages that include it.
	// Many GRE messages don't include gameNumber in gameInfo, so we need to
	// track it ourselves and apply it to plays from messages missing the field.
	currentGameNumber := 1 // Default to game 1

	// Track the last valid turn number from messages that include it.
	// Some GRE messages (especially zone transfers for lands) report turnNumber=0
	// even though they happen during a valid turn. We carry forward the last valid
	// turn number to properly attribute these events.
	lastValidTurnNumber := 1 // Default to turn 1

	// Compare consecutive states to detect changes
	for i := 1; i < len(messages); i++ {
		prev := messages[i-1]
		curr := messages[i]

		// Update tracked game number if this message has a valid one
		if curr.GameNumber > 0 {
			currentGameNumber = curr.GameNumber
		}

		// Update tracked turn number if this message has a valid one
		if curr.TurnInfo != nil && curr.TurnInfo.TurnNumber > 0 {
			lastValidTurnNumber = curr.TurnInfo.TurnNumber
		}

		// Detect life changes
		lifeChanges := detectLifeChanges(prev, curr, playerConn, lifeTotals)
		for _, change := range lifeChanges {
			change.SequenceNumber = sequenceNum
			sequenceNum++
			// Apply tracked game number if the message didn't have one
			if change.GameNumber == 0 {
				change.GameNumber = currentGameNumber
			}
			// Apply tracked turn number if the message had turnNumber=0
			if change.TurnNumber == 0 {
				change.TurnNumber = lastValidTurnNumber
			}
			plays = append(plays, change)
		}

		// Detect zone changes using cumulative zones map
		zoneChanges := detectZoneChangesWithZones(prev, curr, playerConn, cumulativeZones, allObjects)
		for _, change := range zoneChanges {
			change.SequenceNumber = sequenceNum
			sequenceNum++
			if change.GameNumber == 0 {
				change.GameNumber = currentGameNumber
			}
			// Apply tracked turn number if the message had turnNumber=0
			// This is especially important for land drops which often have turnNumber=0
			if change.TurnNumber == 0 {
				change.TurnNumber = lastValidTurnNumber
			}
			plays = append(plays, change)
		}

		// Detect attacks
		attacks := detectAttacks(prev, curr, playerConn)
		for _, attack := range attacks {
			attack.SequenceNumber = sequenceNum
			sequenceNum++
			if attack.GameNumber == 0 {
				attack.GameNumber = currentGameNumber
			}
			// Apply tracked turn number if the message had turnNumber=0
			if attack.TurnNumber == 0 {
				attack.TurnNumber = lastValidTurnNumber
			}
			plays = append(plays, attack)
		}

		// Detect blocks
		blocks := detectBlocks(prev, curr, playerConn)
		for _, block := range blocks {
			block.SequenceNumber = sequenceNum
			sequenceNum++
			if block.GameNumber == 0 {
				block.GameNumber = currentGameNumber
			}
			// Apply tracked turn number if the message had turnNumber=0
			if block.TurnNumber == 0 {
				block.TurnNumber = lastValidTurnNumber
			}
			plays = append(plays, block)
		}

		// Update tracked objects from current state
		for _, obj := range curr.GameObjects {
			allObjects[obj.InstanceID] = &trackedObject{
				instanceID:   obj.InstanceID,
				grpID:        obj.GRPId,
				zoneID:       obj.ZoneID,
				controllerID: obj.ControllerSeatID,
			}
		}

		// Update life totals from current state
		for _, player := range curr.Players {
			lifeTotals[player.SeatID] = player.LifeTotal
		}
	}

	return plays, nil
}

// detectLifeChanges finds changes in player life totals.
func detectLifeChanges(prev, curr *GREGameStateMessage, playerConn *GREConnection, lifeTotals map[int]int) []*GamePlayEvent {
	var events []*GamePlayEvent

	// Get turn number safely (may be nil for some game states)
	turnNumber := 0
	if curr.TurnInfo != nil {
		turnNumber = curr.TurnInfo.TurnNumber
	}

	for _, player := range curr.Players {
		prevLife, existed := lifeTotals[player.SeatID]
		if !existed {
			// First time seeing this player, check prev message
			for _, prevPlayer := range prev.Players {
				if prevPlayer.SeatID == player.SeatID {
					prevLife = prevPlayer.LifeTotal
					existed = true
					break
				}
			}
		}

		if existed && prevLife != player.LifeTotal {
			// Skip mulligan-related life changes (turn 0 or very early)
			// The caller will apply lastValidTurnNumber to turn 0 events
			if turnNumber < 1 && curr.TurnInfo != nil {
				continue
			}

			playerType := "opponent"
			if playerConn != nil && player.SeatID == playerConn.SeatID {
				playerType = "player"
			}

			// Extract phase/step safely (TurnInfo may be nil)
			phase := ""
			step := ""
			if curr.TurnInfo != nil {
				phase = normalizePhase(curr.TurnInfo.Phase)
				step = normalizeStep(curr.TurnInfo.Step)
			}

			event := &GamePlayEvent{
				MatchID:    curr.MatchID,
				GameNumber: curr.GameNumber,
				TurnNumber: turnNumber,
				Phase:      phase,
				Step:       step,
				PlayerType: playerType,
				TeamID:     player.TeamID, // populated from GREPlayerState.TeamID
				ActionType: "life_change",
				LifeFrom:   prevLife,
				LifeTo:     player.LifeTotal,
				Timestamp:  curr.Timestamp,
			}
			events = append(events, event)
		}
	}

	return events
}

// trackedObject tracks an object's state across game state messages.
type trackedObject struct {
	instanceID   int
	grpID        int
	zoneID       int
	controllerID int
}

// detectZoneChangesWithZones finds objects that moved between zones using the cumulative zones map.
func detectZoneChangesWithZones(prev, curr *GREGameStateMessage, playerConn *GREConnection, zones map[int]*GREZone, trackedObjs map[int]*trackedObject) []*GamePlayEvent {
	var events []*GamePlayEvent

	// Note: We no longer return early when TurnInfo is nil.
	// Zone changes (especially land drops) often occur in game states without TurnInfo.
	// The caller (ParseGamePlays) will apply the last valid turn number to events with TurnNumber=0.

	// Build map of previous objects from both the prev message and our tracked state
	prevObjects := make(map[int]*trackedObject)

	// First, use previously tracked objects
	for id, obj := range trackedObjs {
		prevObjects[id] = obj
	}

	// Then overlay with objects from the previous message (more recent state)
	for _, obj := range prev.GameObjects {
		prevObjects[obj.InstanceID] = &trackedObject{
			instanceID:   obj.InstanceID,
			grpID:        obj.GRPId,
			zoneID:       obj.ZoneID,
			controllerID: obj.ControllerSeatID,
		}
	}

	// Check current objects for zone changes
	for _, currObj := range curr.GameObjects {
		prevObj, existed := prevObjects[currObj.InstanceID]

		// Get zone names using the cumulative zones map
		currZoneName := getZoneName(currObj.ZoneID, zones)
		prevZoneName := ""
		if existed {
			prevZoneName = getZoneName(prevObj.zoneID, zones)
		}

		// Skip if zone hasn't changed
		if existed && prevObj.zoneID == currObj.ZoneID {
			continue
		}

		// Determine player type
		playerType := "opponent"
		if playerConn != nil && currObj.ControllerSeatID == playerConn.SeatID {
			playerType = "player"
		}

		// Determine action type based on zone transition
		actionType := determineActionType(prevZoneName, currZoneName, currObj.CardTypes)

		// Skip draw events (library to hand) - not interesting plays
		if actionType == "draw" {
			continue
		}

		// Skip mulligan/game start events
		if prevZoneName == "" && currZoneName == "hand" {
			continue
		}

		// Extract turn info safely (may be nil for some game states)
		turnNumber := 0
		phase := ""
		step := ""
		if curr.TurnInfo != nil {
			turnNumber = curr.TurnInfo.TurnNumber
			phase = normalizePhase(curr.TurnInfo.Phase)
			step = normalizeStep(curr.TurnInfo.Step)
		}

		event := &GamePlayEvent{
			MatchID:    curr.MatchID,
			GameNumber: curr.GameNumber,
			TurnNumber: turnNumber,
			Phase:      phase,
			Step:       step,
			PlayerType: playerType,
			ActionType: actionType,
			CardID:     currObj.GRPId,
			InstanceID: currObj.InstanceID,
			ZoneFrom:   prevZoneName,
			ZoneTo:     currZoneName,
			Timestamp:  curr.Timestamp,
		}
		events = append(events, event)
	}

	return events
}

// getZoneName returns the readable zone name for a zone ID using the zones map.
// Falls back to legacy modulo-based mapping if zones map doesn't have the zone.
func getZoneName(zoneID int, zones map[int]*GREZone) string {
	if zones != nil {
		if zone, ok := zones[zoneID]; ok {
			return zoneTypeToReadableName(zone.Type)
		}
	}
	// Fallback to legacy method for test compatibility and edge cases
	return zoneIDToName(zoneID)
}

// determineActionType determines the action type based on zone transition.
func determineActionType(fromZone, toZone string, cardTypes []string) string {
	// Check if it's a land
	isLand := false
	for _, ct := range cardTypes {
		if ct == "CardType_Land" {
			isLand = true
			break
		}
	}

	switch {
	case fromZone == "hand" && toZone == "battlefield":
		if isLand {
			return "land_drop"
		}
		return "play_card"
	case fromZone == "hand" && toZone == "stack":
		return "cast_spell"
	case fromZone == "stack" && toZone == "battlefield":
		return "resolve_spell"
	case fromZone == "library" && toZone == "hand":
		return "draw"
	case toZone == "graveyard":
		return "to_graveyard"
	case toZone == "exile":
		return "exile"
	case toZone == "battlefield":
		return "enter_battlefield"
	case toZone == "stack":
		return "cast_spell"
	default:
		return "zone_change"
	}
}

// detectAttacks finds creatures that started attacking.
func detectAttacks(prev, curr *GREGameStateMessage, playerConn *GREConnection) []*GamePlayEvent {
	var events []*GamePlayEvent

	// Note: We no longer return early when TurnInfo is nil.
	// The caller (ParseGamePlays) will apply the last valid turn number to events with TurnNumber=0.

	// Extract turn info safely (may be nil for some game states)
	turnNumber := 0
	phase := ""
	step := ""
	if curr.TurnInfo != nil {
		turnNumber = curr.TurnInfo.TurnNumber
		phase = normalizePhase(curr.TurnInfo.Phase)
		step = normalizeStep(curr.TurnInfo.Step)
	}

	// Build map of previous attacking state
	prevAttacking := make(map[int]bool)
	for _, obj := range prev.GameObjects {
		prevAttacking[obj.InstanceID] = obj.IsAttacking
	}

	// Check for new attackers
	for _, obj := range curr.GameObjects {
		wasAttacking := prevAttacking[obj.InstanceID]
		if obj.IsAttacking && !wasAttacking {
			playerType := "opponent"
			if playerConn != nil && obj.ControllerSeatID == playerConn.SeatID {
				playerType = "player"
			}

			event := &GamePlayEvent{
				MatchID:    curr.MatchID,
				GameNumber: curr.GameNumber,
				TurnNumber: turnNumber,
				Phase:      phase,
				Step:       step,
				PlayerType: playerType,
				ActionType: "attack",
				CardID:     obj.GRPId,
				ZoneFrom:   "battlefield",
				ZoneTo:     "battlefield",
				Timestamp:  curr.Timestamp,
			}
			events = append(events, event)
		}
	}

	return events
}

// detectBlocks finds creatures that started blocking.
func detectBlocks(prev, curr *GREGameStateMessage, playerConn *GREConnection) []*GamePlayEvent {
	var events []*GamePlayEvent

	// Note: We no longer return early when TurnInfo is nil.
	// The caller (ParseGamePlays) will apply the last valid turn number to events with TurnNumber=0.

	// Extract turn info safely (may be nil for some game states)
	turnNumber := 0
	phase := ""
	step := ""
	if curr.TurnInfo != nil {
		turnNumber = curr.TurnInfo.TurnNumber
		phase = normalizePhase(curr.TurnInfo.Phase)
		step = normalizeStep(curr.TurnInfo.Step)
	}

	// Build map of previous blocking state
	prevBlocking := make(map[int]bool)
	for _, obj := range prev.GameObjects {
		prevBlocking[obj.InstanceID] = obj.IsBlocking
	}

	// Check for new blockers
	for _, obj := range curr.GameObjects {
		wasBlocking := prevBlocking[obj.InstanceID]
		if obj.IsBlocking && !wasBlocking {
			playerType := "opponent"
			if playerConn != nil && obj.ControllerSeatID == playerConn.SeatID {
				playerType = "player"
			}

			event := &GamePlayEvent{
				MatchID:    curr.MatchID,
				GameNumber: curr.GameNumber,
				TurnNumber: turnNumber,
				Phase:      phase,
				Step:       step,
				PlayerType: playerType,
				ActionType: "block",
				CardID:     obj.GRPId,
				ZoneFrom:   "battlefield",
				ZoneTo:     "battlefield",
				Timestamp:  curr.Timestamp,
			}
			events = append(events, event)
		}
	}

	return events
}

// annotationActionType maps a ZoneTransfer.category string from the GRE
// annotations array to the canonical ActionType values used by GamePlayEvent.
// Categories not listed here are skipped (e.g. "Draw" is intentionally
// filtered out to match detectZoneChangesWithZones behaviour).
var annotationActionType = map[string]string{
	"CastSpell": "cast_spell",
	"PlayLand":  "land_drop",
	"Resolve":   "resolve_spell",
	"Put":       "enter_battlefield",
	"Exile":     "exile",
	"Discard":   "to_graveyard",
	"Destroy":   "to_graveyard",
	"Mill":      "to_graveyard",
	"Sacrifice": "to_graveyard",
	"Return":    "zone_change",
	"Surveil":   "zone_change",
}

// extractAnnotationPlays extracts GamePlayEvents from the annotations array of
// a single GameStateMessage.  This complements detectZoneChangesWithZones,
// which compares consecutive object-snapshot pairs and misses plays in
// partial-state messages that carry annotations but omit a full gameObjects
// snapshot (the common case in corpus logs — only 413 of 5671 consecutive
// GameStateMessage pairs both carry gameObjects).
//
// The function reads every AnnotationType_ZoneTransfer annotation, resolves
// the instance's grpId from the message's gameObjects (when present), and maps
// the annotation category to the canonical ActionType string.  "Draw" events
// are intentionally skipped to match the existing filter in
// detectZoneChangesWithZones.
//
// seenKey is a deduplication set shared with the zone-diff pass.  The key is
// (instanceID, turnNumber, actionType); when the zone-diff already produced a
// play for the same (instance, turn, action), the annotation play is skipped.
func extractAnnotationPlays(
	msg *GREGameStateMessage,
	playerConn *GREConnection,
	cumulativeZones map[int]*GREZone,
	lastValidTurnNumber int,
	currentGameNumber int,
	seenKey map[string]bool,
) []*GamePlayEvent {
	if len(msg.Annotations) == 0 {
		return nil
	}

	// Build instanceID → grpId lookup from this message's gameObjects.
	instanceGRP := make(map[int]int, len(msg.GameObjects))
	instanceController := make(map[int]int, len(msg.GameObjects))
	instanceZone := make(map[int]int, len(msg.GameObjects))
	for _, obj := range msg.GameObjects {
		instanceGRP[obj.InstanceID] = obj.GRPId
		instanceController[obj.InstanceID] = obj.ControllerSeatID
		instanceZone[obj.InstanceID] = obj.ZoneID
	}

	turnNumber := lastValidTurnNumber
	if msg.TurnInfo != nil && msg.TurnInfo.TurnNumber > 0 {
		turnNumber = msg.TurnInfo.TurnNumber
	}

	phase := ""
	step := ""
	if msg.TurnInfo != nil {
		phase = normalizePhase(msg.TurnInfo.Phase)
		step = normalizeStep(msg.TurnInfo.Step)
	}

	gameNumber := currentGameNumber
	if msg.GameNumber > 0 {
		gameNumber = msg.GameNumber
	}

	var events []*GamePlayEvent
	for _, ann := range msg.Annotations {
		hasZoneTransfer := false
		for _, t := range ann.Types {
			if t == "AnnotationType_ZoneTransfer" {
				hasZoneTransfer = true
				break
			}
		}
		if !hasZoneTransfer {
			continue
		}

		cat, _ := ann.Details["category"].(string)
		actionType, ok := annotationActionType[cat]
		if !ok {
			// Skip Draw and any unknown categories.
			continue
		}

		for _, instanceID := range ann.AffectedIDs {
			grpID := instanceGRP[instanceID]
			controllerID := instanceController[instanceID]
			zoneID := instanceZone[instanceID]

			// Dedup: skip if the zone-diff pass already produced this event.
			key := fmt.Sprintf("%d:%d:%s", instanceID, turnNumber, actionType)
			if seenKey[key] {
				continue
			}
			seenKey[key] = true

			playerType := "opponent"
			if playerConn != nil && controllerID == playerConn.SeatID {
				playerType = "player"
			}

			// Resolve zone name for the destination zone.
			zoneTo := getZoneName(zoneID, cumulativeZones)
			// ZoneFrom is not directly available from this annotation; derive from
			// the category so the action is self-describing even without the pair.
			zoneFrom := ""
			switch cat {
			case "CastSpell":
				zoneFrom = "hand"
				zoneTo = "stack"
			case "PlayLand":
				zoneFrom = "hand"
				zoneTo = "battlefield"
			case "Resolve":
				zoneFrom = "stack"
			case "Put":
				zoneFrom = ""
			}

			events = append(events, &GamePlayEvent{
				MatchID:    msg.MatchID,
				GameNumber: gameNumber,
				TurnNumber: turnNumber,
				Phase:      phase,
				Step:       step,
				PlayerType: playerType,
				ActionType: actionType,
				CardID:     grpID,
				ZoneFrom:   zoneFrom,
				ZoneTo:     zoneTo,
				Timestamp:  msg.Timestamp,
			})
		}
	}
	return events
}

// normalizePhase converts MTGA phase names to readable names.
func normalizePhase(phase string) string {
	switch phase {
	case "Phase_Beginning":
		return "Beginning"
	case "Phase_Main1":
		return "Main1"
	case "Phase_Combat":
		return "Combat"
	case "Phase_Main2":
		return "Main2"
	case "Phase_Ending":
		return "Ending"
	default:
		return phase
	}
}

// normalizeStep converts MTGA step names to readable names.
func normalizeStep(step string) string {
	switch step {
	case "Step_Upkeep":
		return "Upkeep"
	case "Step_Draw":
		return "Draw"
	case "Step_BeginCombat":
		return "BeginCombat"
	case "Step_DeclareAttack":
		return "DeclareAttackers"
	case "Step_DeclareBlock":
		return "DeclareBlockers"
	case "Step_CombatDamage":
		return "CombatDamage"
	case "Step_FirstStrikeDamage":
		return "FirstStrikeDamage"
	case "Step_EndCombat":
		return "EndCombat"
	case "Step_End":
		return "EndStep"
	case "Step_Cleanup":
		return "Cleanup"
	default:
		return step
	}
}

// ExtractOpponentCards extracts all cards observed from the opponent.
func ExtractOpponentCards(entries []*LogEntry, playerConn *GREConnection) ([]OpponentCard, error) {
	messages, err := ParseGREMessages(entries)
	if err != nil {
		return nil, err
	}

	// Track seen cards with the turn they were first seen
	seenCards := make(map[int]*OpponentCard)

	for _, msg := range messages {
		turnNumber := 0
		if msg.TurnInfo != nil {
			turnNumber = msg.TurnInfo.TurnNumber
		}

		for _, obj := range msg.GameObjects {
			// Skip player's own cards
			if playerConn != nil && obj.ControllerSeatID == playerConn.SeatID {
				continue
			}

			// Skip cards without a valid GRPId
			if obj.GRPId == 0 {
				continue
			}

			// Track the card
			if existing, exists := seenCards[obj.GRPId]; exists {
				existing.TimesSeen++
				// Update zone if moved to a more revealing zone
				if obj.ZoneName == "battlefield" || obj.ZoneName == "hand" || obj.ZoneName == "graveyard" {
					existing.ZoneObserved = obj.ZoneName
				}
			} else {
				seenCards[obj.GRPId] = &OpponentCard{
					CardID:        obj.GRPId,
					ZoneObserved:  obj.ZoneName,
					TurnFirstSeen: turnNumber,
					TimesSeen:     1,
				}
			}
		}
	}

	// Convert map to slice
	var cards []OpponentCard
	for _, card := range seenCards {
		cards = append(cards, *card)
	}

	return cards, nil
}

// OpponentCard represents an opponent's card that was observed.
type OpponentCard struct {
	CardID        int
	CardName      string // Will be populated later from card database
	ZoneObserved  string
	TurnFirstSeen int
	TimesSeen     int
}

// ExtractGameSnapshots extracts turn-by-turn snapshots from game state messages.
func ExtractGameSnapshots(entries []*LogEntry, playerConn *GREConnection) ([]*GameSnapshot, error) {
	messages, err := ParseGREMessages(entries)
	if err != nil {
		return nil, err
	}

	// Group by turn number and take the last state for each turn
	turnSnapshots := make(map[int]*GREGameStateMessage)
	for _, msg := range messages {
		if msg.TurnInfo == nil {
			continue
		}
		turnSnapshots[msg.TurnInfo.TurnNumber] = msg
	}

	var snapshots []*GameSnapshot
	for turnNumber, msg := range turnSnapshots {
		snapshot := &GameSnapshot{
			MatchID:    msg.MatchID,
			GameNumber: msg.GameNumber,
			TurnNumber: turnNumber,
			Timestamp:  msg.Timestamp,
		}

		if msg.TurnInfo != nil {
			activePlayer := "opponent"
			if playerConn != nil && msg.TurnInfo.ActivePlayer == playerConn.SeatID {
				activePlayer = "player"
			}
			snapshot.ActivePlayer = activePlayer
		}

		// Extract player and opponent states
		for _, player := range msg.Players {
			if playerConn != nil && player.SeatID == playerConn.SeatID {
				snapshot.PlayerLife = player.LifeTotal
			} else {
				snapshot.OpponentLife = player.LifeTotal
			}
		}

		// Count cards and lands by controller
		playerCardsInHand := 0
		opponentCardsInHand := 0
		playerLands := 0
		opponentLands := 0

		for _, obj := range msg.GameObjects {
			isPlayer := playerConn != nil && obj.ControllerSeatID == playerConn.SeatID

			if obj.ZoneName == "hand" {
				if isPlayer {
					playerCardsInHand++
				} else {
					opponentCardsInHand++
				}
			}

			if obj.ZoneName == "battlefield" {
				for _, cardType := range obj.CardTypes {
					if cardType == "CardType_Land" {
						if isPlayer {
							playerLands++
						} else {
							opponentLands++
						}
						break
					}
				}
			}
		}

		snapshot.PlayerCardsInHand = playerCardsInHand
		snapshot.OpponentCardsInHand = opponentCardsInHand
		snapshot.PlayerLandsInPlay = playerLands
		snapshot.OpponentLandsInPlay = opponentLands

		snapshots = append(snapshots, snapshot)
	}

	return snapshots, nil
}

// GameSnapshot represents the game state at a specific turn.
type GameSnapshot struct {
	MatchID             string
	GameNumber          int
	TurnNumber          int
	ActivePlayer        string
	PlayerLife          int
	OpponentLife        int
	PlayerCardsInHand   int
	OpponentCardsInHand int
	PlayerLandsInPlay   int
	OpponentLandsInPlay int
	BoardStateJSON      string
	Timestamp           time.Time
}

// CounterChangeEvent records a single counter mutation observed on a permanent
// between two consecutive GRE game state messages.
type CounterChangeEvent struct {
	InstanceID  int
	ArenaID     int
	CounterType string
	Count       int    // new total after the change
	Delta       int    // Count minus previous Count (negative for decrements)
	Controller  string // "player" or "opponent"
	TurnNumber  int
}

// MulliganData records the opening hand decision for a single game, inferred
// from pre-game (turn < 1) GRE game state messages.
type MulliganData struct {
	OpeningHandSize int
	MulliganCount   int
	KeptCardIDs     []int
	BottomedCardIDs []int
}

// GamePlaysResult contains all parsed GRE outputs from a single
// ParseGamePlaysResult call, replacing the previous three-function pattern of
// ParseGamePlays + ExtractGameSnapshots + ExtractOpponentCards.
type GamePlaysResult struct {
	Plays          []*GamePlayEvent
	Snapshots      []*GameSnapshot
	OpponentCards  []OpponentCard
	CounterChanges []CounterChangeEvent
	Mulligan       *MulliganData
	// FirstTurnActivePlayerSeatID is the systemSeatNumber of the player whose
	// turn it is at the start of the first turn of game play (turnNumber == 1,
	// stage == "GameStage_Play"). Zero means no such message was observed in
	// this buffer window.
	FirstTurnActivePlayerSeatID int
}

// ParseGamePlaysResult parses a GRE session buffer and returns all detected
// game events in a single pass: plays, snapshots, opponent cards, counter
// changes, and mulligan data.
//
// The caller is the daemon's flushGREBuffer. This replaces the three
// separate calls to ParseGamePlays / ExtractGameSnapshots /
// ExtractOpponentCards without changing their individual semantics.
func ParseGamePlaysResult(entries []*LogEntry, playerConn *GREConnection) (GamePlaysResult, error) {
	messages, err := ParseGREMessages(entries)
	if err != nil {
		return GamePlaysResult{}, err
	}

	// Mulligan: scan pre-game messages before any per-state iteration.
	mulligan := detectMulligan(messages, playerConn)

	// extractOpponentCardsFromMessages and extractSnapshotsFromMessages operate
	// on individual messages — no diff pair is required. Run them before the
	// early-return so a single-message buffer still produces opponent cards and
	// snapshots.
	snapshots, err := extractSnapshotsFromMessages(messages, playerConn)
	if err != nil {
		return GamePlaysResult{}, err
	}
	opponentCards := extractOpponentCardsFromMessages(messages, playerConn)

	// Detect which player seat went first (on-the-play). The first
	// GameStateMessage with stage "GameStage_Play" and turnNumber == 1 carries
	// turnInfo.activePlayer set to the seat playing first.
	firstTurnActivePlayer := detectFirstTurnActivePlayer(messages)

	if len(messages) < 2 {
		return GamePlaysResult{
			Snapshots:                   snapshots,
			OpponentCards:               opponentCards,
			Mulligan:                    mulligan,
			FirstTurnActivePlayerSeatID: firstTurnActivePlayer,
		}, nil
	}

	// Build a cumulative zones map from all messages.
	cumulativeZones := make(map[int]*GREZone)
	for _, msg := range messages {
		for zoneID, zone := range msg.Zones {
			cumulativeZones[zoneID] = zone
		}
	}

	allTrackedObjects := make(map[int]*trackedObject)
	lifeTotals := make(map[int]int)
	// prevCounters tracks the last-observed counter state per instanceID so we
	// can compute deltas without re-reading the previous GRE message on each
	// iteration.
	prevCounters := make(map[int]map[string]int)

	var plays []*GamePlayEvent
	var counterChanges []CounterChangeEvent
	seqNum := 0
	currentGameNumber := 1
	lastValidTurnNumber := 1

	// seenKey prevents double-counting between the zone-diff pass and the
	// annotation-based pass.  Key: "instanceID:turnNumber:actionType".
	seenKey := make(map[string]bool)

	// Seed prevCounters from the first message so the first comparison has a
	// baseline to diff against.
	for _, obj := range messages[0].GameObjects {
		if len(obj.Counters) > 0 {
			snapshot := make(map[string]int, len(obj.Counters))
			for k, v := range obj.Counters {
				snapshot[k] = v
			}
			prevCounters[obj.InstanceID] = snapshot
		}
	}

	for i := 1; i < len(messages); i++ {
		prev := messages[i-1]
		curr := messages[i]

		if curr.GameNumber > 0 {
			currentGameNumber = curr.GameNumber
		}
		if curr.TurnInfo != nil && curr.TurnInfo.TurnNumber > 0 {
			lastValidTurnNumber = curr.TurnInfo.TurnNumber
		}

		// Life changes.
		for _, lc := range detectLifeChanges(prev, curr, playerConn, lifeTotals) {
			lc.SequenceNumber = seqNum
			seqNum++
			if lc.GameNumber == 0 {
				lc.GameNumber = currentGameNumber
			}
			if lc.TurnNumber == 0 {
				lc.TurnNumber = lastValidTurnNumber
			}
			plays = append(plays, lc)
		}

		// Zone changes (snapshot-diff path).
		for _, zc := range detectZoneChangesWithZones(prev, curr, playerConn, cumulativeZones, allTrackedObjects) {
			zc.SequenceNumber = seqNum
			seqNum++
			if zc.GameNumber == 0 {
				zc.GameNumber = currentGameNumber
			}
			if zc.TurnNumber == 0 {
				zc.TurnNumber = lastValidTurnNumber
			}
			plays = append(plays, zc)
			// Register in seenKey so the annotation pass skips this event.
			// Use the real InstanceID so the key namespace matches the annotation
			// pass (which also keys on instanceID from affectedIDs).
			seenKey[fmt.Sprintf("%d:%d:%s", zc.InstanceID, zc.TurnNumber, zc.ActionType)] = true
		}

		// Attacks.
		for _, a := range detectAttacks(prev, curr, playerConn) {
			a.SequenceNumber = seqNum
			seqNum++
			if a.GameNumber == 0 {
				a.GameNumber = currentGameNumber
			}
			if a.TurnNumber == 0 {
				a.TurnNumber = lastValidTurnNumber
			}
			plays = append(plays, a)
		}

		// Blocks.
		for _, b := range detectBlocks(prev, curr, playerConn) {
			b.SequenceNumber = seqNum
			seqNum++
			if b.GameNumber == 0 {
				b.GameNumber = currentGameNumber
			}
			if b.TurnNumber == 0 {
				b.TurnNumber = lastValidTurnNumber
			}
			plays = append(plays, b)
		}

		// Counter changes (only during in-game turns, not pre-game).
		if curr.TurnInfo != nil && curr.TurnInfo.TurnNumber > 0 {
			for _, cc := range detectCounterChanges(prev, curr, playerConn) {
				counterChanges = append(counterChanges, cc)
			}
		}

		// Update tracking state.
		for _, obj := range curr.GameObjects {
			allTrackedObjects[obj.InstanceID] = &trackedObject{
				instanceID:   obj.InstanceID,
				grpID:        obj.GRPId,
				zoneID:       obj.ZoneID,
				controllerID: obj.ControllerSeatID,
			}
			if len(obj.Counters) > 0 {
				snapshot := make(map[string]int, len(obj.Counters))
				for k, v := range obj.Counters {
					snapshot[k] = v
				}
				prevCounters[obj.InstanceID] = snapshot
			}
		}
		for _, player := range curr.Players {
			lifeTotals[player.SeatID] = player.LifeTotal
		}

		// Annotation-based plays (per-message pass).
		// This complements the snapshot-diff pass above — it captures card
		// plays in partial-state messages where gameObjects are absent from
		// one or both consecutive messages, which is the typical shape of
		// real MTGA corpus logs.  seenKey prevents double-counting events
		// already detected by the snapshot-diff pass.
		for _, ap := range extractAnnotationPlays(curr, playerConn, cumulativeZones, lastValidTurnNumber, currentGameNumber, seenKey) {
			ap.SequenceNumber = seqNum
			seqNum++
			plays = append(plays, ap)
		}
	}

	// Also run the annotation pass on the first message, which is not visited
	// as curr in the diff loop.
	if len(messages) >= 1 {
		first := messages[0]
		if first.GameNumber > 0 {
			currentGameNumber = first.GameNumber
		}
		firstTurn := lastValidTurnNumber
		if first.TurnInfo != nil && first.TurnInfo.TurnNumber > 0 {
			firstTurn = first.TurnInfo.TurnNumber
		}
		for _, ap := range extractAnnotationPlays(first, playerConn, cumulativeZones, firstTurn, currentGameNumber, seenKey) {
			ap.SequenceNumber = seqNum
			seqNum++
			plays = append(plays, ap)
		}
	}

	return GamePlaysResult{
		Plays:                       plays,
		Snapshots:                   snapshots,
		OpponentCards:               opponentCards,
		CounterChanges:              counterChanges,
		Mulligan:                    mulligan,
		FirstTurnActivePlayerSeatID: firstTurnActivePlayer,
	}, nil
}

// detectFirstTurnActivePlayer scans messages for the first GameStateMessage
// with stage "GameStage_Play" and turnNumber == 1 that carries
// turnInfo.activePlayer. Returns the seat ID of the active player (the player
// on the play), or 0 when no such message is found.
func detectFirstTurnActivePlayer(messages []*GREGameStateMessage) int {
	for _, msg := range messages {
		if msg.Stage != "GameStage_Play" {
			continue
		}
		if msg.TurnInfo == nil {
			continue
		}
		if msg.TurnInfo.TurnNumber != 1 {
			continue
		}
		if msg.TurnInfo.ActivePlayer > 0 {
			return msg.TurnInfo.ActivePlayer
		}
	}
	return 0
}

// detectCounterChanges compares the counter maps of game objects between two
// consecutive game state messages and emits one CounterChangeEvent per
// (instance, counter_type) pair whose count changed.
func detectCounterChanges(prev, curr *GREGameStateMessage, playerConn *GREConnection) []CounterChangeEvent {
	// Build a map of previous counter state keyed by instanceID.
	prevState := make(map[int]map[string]int, len(prev.GameObjects))
	for _, obj := range prev.GameObjects {
		if len(obj.Counters) == 0 {
			continue
		}
		snap := make(map[string]int, len(obj.Counters))
		for k, v := range obj.Counters {
			snap[k] = v
		}
		prevState[obj.InstanceID] = snap
	}

	turnNumber := 0
	if curr.TurnInfo != nil {
		turnNumber = curr.TurnInfo.TurnNumber
	}

	var events []CounterChangeEvent
	for _, obj := range curr.GameObjects {
		if len(obj.Counters) == 0 {
			continue
		}
		controller := "opponent"
		if playerConn != nil && obj.ControllerSeatID == playerConn.SeatID {
			controller = "player"
		}
		prevObj := prevState[obj.InstanceID]
		for cType, newCount := range obj.Counters {
			oldCount := 0
			if prevObj != nil {
				oldCount = prevObj[cType]
			}
			if newCount == oldCount {
				continue
			}
			events = append(events, CounterChangeEvent{
				InstanceID:  obj.InstanceID,
				ArenaID:     obj.GRPId,
				CounterType: cType,
				Count:       newCount,
				Delta:       newCount - oldCount,
				Controller:  controller,
				TurnNumber:  turnNumber,
			})
		}
		// Also detect counter types that existed in prev but are absent in curr
		// (counter removed entirely — count dropped to 0).
		for cType, oldCount := range prevObj {
			if _, stillPresent := obj.Counters[cType]; !stillPresent && oldCount > 0 {
				events = append(events, CounterChangeEvent{
					InstanceID:  obj.InstanceID,
					ArenaID:     obj.GRPId,
					CounterType: cType,
					Count:       0,
					Delta:       -oldCount,
					Controller:  controller,
					TurnNumber:  turnNumber,
				})
			}
		}
	}
	return events
}

// detectMulligan scans pre-game GRE game state messages (those with nil
// TurnInfo or TurnNumber == 0) and infers the mulligan count from decrements
// in the player's maxHandSize field (London mulligan rules).
//
// Returns nil when no pre-game messages are found.
func detectMulligan(messages []*GREGameStateMessage, playerConn *GREConnection) *MulliganData {
	// Collect only pre-game messages (nil TurnInfo or TurnNumber == 0).
	var preGameMsgs []*GREGameStateMessage
	for _, msg := range messages {
		if msg.TurnInfo == nil || msg.TurnInfo.TurnNumber == 0 {
			preGameMsgs = append(preGameMsgs, msg)
		}
	}
	if len(preGameMsgs) == 0 {
		return nil
	}

	// Scan pre-game messages for the local player's maxHandSize.
	// The starting value is 7 (or whatever value appears first for the player).
	// Each time maxHandSize decrements by 1, a mulligan was taken.
	maxHandSizeHistory := make([]int, 0, len(preGameMsgs))
	for _, msg := range preGameMsgs {
		for _, player := range msg.Players {
			if playerConn != nil && player.SeatID != playerConn.SeatID {
				continue
			}
			if player.MaxHandSize > 0 {
				maxHandSizeHistory = append(maxHandSizeHistory, player.MaxHandSize)
			}
		}
	}

	if len(maxHandSizeHistory) == 0 {
		return nil
	}

	initialMaxHand := maxHandSizeHistory[0]
	finalMaxHand := maxHandSizeHistory[len(maxHandSizeHistory)-1]
	mulliganCount := initialMaxHand - finalMaxHand
	if mulliganCount < 0 {
		mulliganCount = 0
	}

	// Use the last pre-game message to determine the final kept hand.
	lastPreGame := preGameMsgs[len(preGameMsgs)-1]
	var keptCardIDs []int
	for _, obj := range lastPreGame.GameObjects {
		if playerConn != nil && obj.OwnerSeatID != playerConn.SeatID {
			continue
		}
		if obj.ZoneName == "hand" {
			keptCardIDs = append(keptCardIDs, obj.GRPId)
		} else if obj.ZoneID > 0 {
			// Resolve via zones map when ZoneName is not pre-populated.
			if lastPreGame.Zones != nil {
				if zone, ok := lastPreGame.Zones[obj.ZoneID]; ok {
					if zone.Type == "ZoneType_Hand" && zone.OwnerSeatID == obj.OwnerSeatID {
						keptCardIDs = append(keptCardIDs, obj.GRPId)
					}
				}
			}
		}
	}

	bottomed := make([]int, 0)
	return &MulliganData{
		OpeningHandSize: finalMaxHand,
		MulliganCount:   mulliganCount,
		KeptCardIDs:     keptCardIDs,
		BottomedCardIDs: bottomed,
	}
}

// extractSnapshotsFromMessages is the internal implementation of
// ExtractGameSnapshots operating on already-parsed messages.
func extractSnapshotsFromMessages(messages []*GREGameStateMessage, playerConn *GREConnection) ([]*GameSnapshot, error) {
	turnSnapshots := make(map[int]*GREGameStateMessage)
	for _, msg := range messages {
		if msg.TurnInfo == nil {
			continue
		}
		turnSnapshots[msg.TurnInfo.TurnNumber] = msg
	}

	var snapshots []*GameSnapshot
	for turnNumber, msg := range turnSnapshots {
		snapshot := &GameSnapshot{
			MatchID:    msg.MatchID,
			GameNumber: msg.GameNumber,
			TurnNumber: turnNumber,
			Timestamp:  msg.Timestamp,
		}
		if msg.TurnInfo != nil {
			activePlayer := "opponent"
			if playerConn != nil && msg.TurnInfo.ActivePlayer == playerConn.SeatID {
				activePlayer = "player"
			}
			snapshot.ActivePlayer = activePlayer
		}
		for _, player := range msg.Players {
			if playerConn != nil && player.SeatID == playerConn.SeatID {
				snapshot.PlayerLife = player.LifeTotal
			} else {
				snapshot.OpponentLife = player.LifeTotal
			}
		}
		playerCardsInHand := 0
		opponentCardsInHand := 0
		playerLands := 0
		opponentLands := 0
		for _, obj := range msg.GameObjects {
			isPlayer := playerConn != nil && obj.ControllerSeatID == playerConn.SeatID
			if obj.ZoneName == "hand" {
				if isPlayer {
					playerCardsInHand++
				} else {
					opponentCardsInHand++
				}
			}
			if obj.ZoneName == "battlefield" {
				for _, ct := range obj.CardTypes {
					if ct == "CardType_Land" {
						if isPlayer {
							playerLands++
						} else {
							opponentLands++
						}
						break
					}
				}
			}
		}
		snapshot.PlayerCardsInHand = playerCardsInHand
		snapshot.OpponentCardsInHand = opponentCardsInHand
		snapshot.PlayerLandsInPlay = playerLands
		snapshot.OpponentLandsInPlay = opponentLands
		snapshots = append(snapshots, snapshot)
	}
	return snapshots, nil
}

// extractOpponentCardsFromMessages is the internal implementation of
// ExtractOpponentCards operating on already-parsed messages.
func extractOpponentCardsFromMessages(messages []*GREGameStateMessage, playerConn *GREConnection) []OpponentCard {
	seenCards := make(map[int]*OpponentCard)
	for _, msg := range messages {
		turnNumber := 0
		if msg.TurnInfo != nil {
			turnNumber = msg.TurnInfo.TurnNumber
		}
		for _, obj := range msg.GameObjects {
			if playerConn != nil && obj.ControllerSeatID == playerConn.SeatID {
				continue
			}
			if obj.GRPId == 0 {
				continue
			}
			if existing, exists := seenCards[obj.GRPId]; exists {
				existing.TimesSeen++
				if obj.ZoneName == "battlefield" || obj.ZoneName == "hand" || obj.ZoneName == "graveyard" {
					existing.ZoneObserved = obj.ZoneName
				}
			} else {
				seenCards[obj.GRPId] = &OpponentCard{
					CardID:        obj.GRPId,
					ZoneObserved:  obj.ZoneName,
					TurnFirstSeen: turnNumber,
					TimesSeen:     1,
				}
			}
		}
	}
	var cards []OpponentCard
	for _, card := range seenCards {
		cards = append(cards, *card)
	}
	return cards
}

// parseLogTimestamp is defined in draft_picks.go
