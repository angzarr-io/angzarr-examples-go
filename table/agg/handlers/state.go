// Package handlers implements table aggregate state reconstruction.
package handlers

import (
	"encoding/hex"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr/v1"
	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
)

// TableState represents the current state of a table aggregate.
type TableState struct {
	TableID              string
	TableName            string
	GameVariant          examples.GameVariant
	SmallBlind           int64
	BigBlind             int64
	MinBuyIn             int64
	MaxBuyIn             int64
	MaxPlayers           int32
	ActionTimeoutSeconds int32
	Seats                map[int32]*SeatState // position -> seat
	DealerPosition       int32
	HandCount            int64
	CurrentHandRoot      []byte
	Status               string // "waiting", "in_hand", "paused"
	// Last hand's blind positions, retained between hands so the
	// dead-button advancement rule (TDA Rule 32 / 34B) can compute the
	// next BB without re-deriving from the seat layout alone. Default
	// to -1 (no prior hand played).
	LastBigBlindPosition   int32
	LastSmallBlindPosition int32
	// HandForHandStatus tracks tournament hand-for-hand bubble sync per
	// Phase I-Go-v2a HIGH-EX-2.2.3. Values: "" (not in H4H), "WAITING"
	// (Enter received; round in progress), "COMPLETE" (MarkComplete
	// received; tournament can round up). See hand_for_hand.go.
	HandForHandStatus string
	// HandForHandTournamentRoot is captured from EnterTableHandForHand so
	// later events can be routed back to the owning tournament aggregate
	// without a separate registry. Cleared on TableHandForHandEnded.
	HandForHandTournamentRoot []byte
}

// SeatState represents a player seat at the table.
type SeatState struct {
	Position     int32
	PlayerRoot   []byte
	Stack        int64
	IsActive     bool
	IsSittingOut bool
}

// NewTableState creates a new empty table state.
func NewTableState() TableState {
	return TableState{
		Seats:                  make(map[int32]*SeatState),
		LastBigBlindPosition:   -1,
		LastSmallBlindPosition: -1,
	}
}

// Exists returns true if the table has been created.
func (s TableState) Exists() bool {
	return s.TableID != ""
}

// PlayerCount returns the number of seated players.
func (s TableState) PlayerCount() int {
	return len(s.Seats)
}

// ActivePlayerCount returns the number of active (not sitting out) players.
func (s TableState) ActivePlayerCount() int {
	count := 0
	for _, seat := range s.Seats {
		if !seat.IsSittingOut {
			count++
		}
	}
	return count
}

// GetSeatOccupant returns the player root hex string at the given seat, or empty string.
func (s TableState) GetSeatOccupant(position int32) string {
	if seat, ok := s.Seats[position]; ok {
		return hex.EncodeToString(seat.PlayerRoot)
	}
	return ""
}

// Event applier functions for StateRouter

func applyTableCreated(state *TableState, event *examples.TableCreated) {
	state.TableID = "table_" + event.TableName
	state.TableName = event.TableName
	state.GameVariant = event.GameVariant
	state.SmallBlind = event.SmallBlind
	state.BigBlind = event.BigBlind
	state.MinBuyIn = event.MinBuyIn
	state.MaxBuyIn = event.MaxBuyIn
	state.MaxPlayers = event.MaxPlayers
	state.ActionTimeoutSeconds = event.ActionTimeoutSeconds
	state.DealerPosition = 0
	state.HandCount = 0
	state.Status = "waiting"
}

func applyPlayerJoined(state *TableState, event *examples.PlayerJoined) {
	state.Seats[event.SeatPosition] = &SeatState{
		Position:     event.SeatPosition,
		PlayerRoot:   event.PlayerRoot,
		Stack:        event.Stack,
		IsActive:     true,
		IsSittingOut: false,
	}
}

func applyPlayerLeft(state *TableState, event *examples.PlayerLeft) {
	delete(state.Seats, event.SeatPosition)
}

func applyPlayerSatOut(state *TableState, event *examples.PlayerSatOut) {
	playerHex := hex.EncodeToString(event.PlayerRoot)
	for pos, seat := range state.Seats {
		if hex.EncodeToString(seat.PlayerRoot) == playerHex {
			state.Seats[pos].IsSittingOut = true
			break
		}
	}
}

func applyPlayerSatIn(state *TableState, event *examples.PlayerSatIn) {
	playerHex := hex.EncodeToString(event.PlayerRoot)
	for pos, seat := range state.Seats {
		if hex.EncodeToString(seat.PlayerRoot) == playerHex {
			state.Seats[pos].IsSittingOut = false
			break
		}
	}
}

func applyHandStarted(state *TableState, event *examples.HandStarted) {
	state.CurrentHandRoot = event.HandRoot
	state.HandCount = event.HandNumber
	state.DealerPosition = event.DealerPosition
	state.LastSmallBlindPosition = event.SmallBlindPosition
	state.LastBigBlindPosition = event.BigBlindPosition
	state.Status = "in_hand"
}

func applyHandEnded(state *TableState, event *examples.HandEnded) {
	state.CurrentHandRoot = nil
	state.Status = "waiting"
	// Apply stack changes
	for playerHex, delta := range event.StackChanges {
		for _, seat := range state.Seats {
			if hex.EncodeToString(seat.PlayerRoot) == playerHex {
				seat.Stack += delta
				break
			}
		}
	}
}

func applyChipsAdded(state *TableState, event *examples.ChipsAdded) {
	playerHex := hex.EncodeToString(event.PlayerRoot)
	for pos, seat := range state.Seats {
		if hex.EncodeToString(seat.PlayerRoot) == playerHex {
			state.Seats[pos].Stack = event.NewStack
			break
		}
	}
}

// applyPlayerSeated seats a PM-orchestrated PlayerSeated event onto
// state. Mirrors apply_chips_added shape since the table records the
// seat snapshot for both flows. See Python table.py:273-281.
func applyPlayerSeated(state *TableState, event *examples.PlayerSeated) {
	state.Seats[event.SeatPosition] = &SeatState{
		Position:     event.SeatPosition,
		PlayerRoot:   event.PlayerRoot,
		Stack:        event.Stack,
		IsActive:     true,
		IsSittingOut: false,
	}
}

// applyRebuyChipsAdded updates the seated stack when the PM-orchestrated
// rebuy flow completes. Same shape as ChipsAdded but a different proto
// type (rebuy_proto.RebuyChipsAdded). Python table.py:291-298.
func applyRebuyChipsAdded(state *TableState, event *examples.RebuyChipsAdded) {
	playerHex := hex.EncodeToString(event.PlayerRoot)
	for pos, seat := range state.Seats {
		if hex.EncodeToString(seat.PlayerRoot) == playerHex {
			state.Seats[pos].Stack = event.NewStack
			break
		}
	}
}

// applyPlayerHandKilledByPenalty debits a killed seat's stack by the
// posted blinds; the seat itself remains. TDA Rule 71C. Python
// table.py:261-271.
func applyPlayerHandKilledByPenalty(state *TableState, event *examples.PlayerHandKilledByPenalty) {
	seat, ok := state.Seats[event.SeatPosition]
	if !ok || event.StackCharged == 0 {
		return
	}
	if seat.Stack > event.StackCharged {
		seat.Stack -= event.StackCharged
	} else {
		seat.Stack = 0
	}
}

// applyTableHandForHandWaiting transitions H4H status "" → "WAITING" and
// stores the tournament_root for routing later events. Phase I-Go-v2a.
func applyTableHandForHandWaiting(state *TableState, event *examples.TableHandForHandWaiting) {
	state.HandForHandStatus = "WAITING"
	state.HandForHandTournamentRoot = event.TournamentRoot
}

// applyTableHandForHandRoundComplete transitions H4H status "WAITING" →
// "COMPLETE". The tournament_root stays in state so a subsequent End event
// can be authored without consulting external context. Phase I-Go-v2a.
func applyTableHandForHandRoundComplete(state *TableState, _ *examples.TableHandForHandRoundComplete) {
	state.HandForHandStatus = "COMPLETE"
}

// applyTableHandForHandEnded clears H4H state ("WAITING"/"COMPLETE" → "")
// and forgets the tournament_root. Phase I-Go-v2a.
func applyTableHandForHandEnded(state *TableState, _ *examples.TableHandForHandEnded) {
	state.HandForHandStatus = ""
	state.HandForHandTournamentRoot = nil
}

// stateRouter is the fluent state reconstruction router.
var stateRouter = angzarr.NewStateRouter(NewTableState).
	On(applyTableCreated).
	On(applyPlayerJoined).
	On(applyPlayerLeft).
	On(applyPlayerSatOut).
	On(applyPlayerSatIn).
	On(applyHandStarted).
	On(applyHandEnded).
	On(applyChipsAdded).
	On(applyPlayerSeated).
	On(applyRebuyChipsAdded).
	On(applyPlayerHandKilledByPenalty).
	On(applyTableHandForHandWaiting).
	On(applyTableHandForHandRoundComplete).
	On(applyTableHandForHandEnded)

// RebuildState rebuilds table state from event history.
func RebuildState(eventBook *pb.EventBook) TableState {
	if eventBook == nil {
		return NewTableState()
	}

	state := NewTableState()
	for _, page := range eventBook.Pages {
		event := page.GetEvent()
		if event != nil {
			stateRouter.ApplySingle(&state, event)
		}
	}
	return state
}
