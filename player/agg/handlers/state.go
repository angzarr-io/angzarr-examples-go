// Package handlers implements player aggregate command handlers.
//
// DOC: This file is referenced in docs/docs/examples/aggregates.mdx
//
//	Update documentation when making changes to StateRouter patterns.
package handlers

import (
	"encoding/hex"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr/v1"
	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
)

// PendingBuyIn mirrors the reservation-aggregate record but is also
// tracked on PlayerState so unit tests (`player.feature` rebuild
// scenarios + "no pending buy-in"/registration/rebuy assertions) can
// observe orchestration lifecycle from the player side without rolling
// a separate aggregate replay.
type PendingBuyIn struct {
	TableRoot []byte
	Seat      int32
	Amount    int64
}

// PendingRegistration tracks a tournament registration that has been
// requested but not yet confirmed or released.
type PendingRegistration struct {
	TournamentRoot []byte
	Fee            int64
}

// PendingRebuy tracks a rebuy that has been requested but not yet
// confirmed or released.
type PendingRebuy struct {
	TournamentRoot []byte
	TableRoot      []byte
	Seat           int32
	Fee            int64
	Chips          int64
}

// PlayerState represents the current state of a player aggregate.
type PlayerState struct {
	PlayerID          string
	DisplayName       string
	Email             string
	PlayerType        examples.PlayerType
	AIModelID         string
	Bankroll          int64 // In smallest unit (chips)
	ReservedFunds     int64
	TableReservations map[string]int64 // table_root_hex -> amount
	Status            string

	// Pending-orchestration trackers — keyed by reservation_id_hex.
	// Maintained by appliers wired into stateRouter; written by
	// HandleInitiate* and consumed by HandleConfirm*/HandleRelease*.
	// Mirrors `state.pending_buy_ins` / `state.pending_registrations`
	// / `state.pending_rebuys` on Python's PlayerState.
	PendingBuyIns        map[string]*PendingBuyIn
	PendingRegistrations map[string]*PendingRegistration
	PendingRebuys        map[string]*PendingRebuy
}

// NewPlayerState creates a new empty player state.
func NewPlayerState() PlayerState {
	return PlayerState{
		TableReservations:    make(map[string]int64),
		PendingBuyIns:        make(map[string]*PendingBuyIn),
		PendingRegistrations: make(map[string]*PendingRegistration),
		PendingRebuys:        make(map[string]*PendingRebuy),
	}
}

// Exists returns true if the player has been registered.
func (s PlayerState) Exists() bool {
	return s.PlayerID != ""
}

// AvailableBalance returns the available balance (bankroll - reserved).
func (s PlayerState) AvailableBalance() int64 {
	return s.Bankroll - s.ReservedFunds
}

// IsAI returns true if this is an AI player.
func (s PlayerState) IsAI() bool {
	return s.PlayerType == examples.PlayerType_AI
}

// Event applier functions for StateRouter

// docs:start:state_router
func applyRegistered(state *PlayerState, event *examples.PlayerRegistered) {
	state.PlayerID = "player_" + event.Email
	state.DisplayName = event.DisplayName
	state.Email = event.Email
	state.PlayerType = event.PlayerType
	state.AIModelID = event.AiModelId
	state.Status = "active"
	state.Bankroll = 0
	state.ReservedFunds = 0
}

// applyDeposited applies a FundsDeposited event to state.
//
// # Why Events Carry Final State (Not Deltas)
//
// Events contain NewBalance (the result) rather than delta (amount deposited).
// This design choice provides:
// 1. **Idempotent replay**: Re-applying the event produces the same state
// 2. **Auditable**: Can verify the computation was correct at event time
// 3. **Simpler appliers**: Just assign the value, no arithmetic needed
//
// The trade-off: events are slightly larger, and you can't easily see the
// delta without comparing to previous state. For most use cases, the benefits
// of idempotent replay outweigh this.
func applyDeposited(state *PlayerState, event *examples.FundsDeposited) {
	if event.NewBalance != nil {
		state.Bankroll = event.NewBalance.Amount
	}
}

func applyWithdrawn(state *PlayerState, event *examples.FundsWithdrawn) {
	if event.NewBalance != nil {
		state.Bankroll = event.NewBalance.Amount
	}
}

func applyReserved(state *PlayerState, event *examples.FundsReserved) {
	if event.NewReservedBalance != nil {
		state.ReservedFunds = event.NewReservedBalance.Amount
	}
	if event.Key != nil && event.Amount != nil {
		tableKey := hex.EncodeToString(event.Key)
		state.TableReservations[tableKey] = event.Amount.Amount
	}
}

func applyReleased(state *PlayerState, event *examples.FundsReleased) {
	if event.NewReservedBalance != nil {
		state.ReservedFunds = event.NewReservedBalance.Amount
	}
	if event.Key != nil {
		tableKey := hex.EncodeToString(event.Key)
		delete(state.TableReservations, tableKey)
	}
}

func applyTransferred(state *PlayerState, event *examples.FundsTransferred) {
	if event.NewBalance != nil {
		state.Bankroll = event.NewBalance.Amount
	}
}

// --- Orchestration-lifecycle appliers (player.feature) ---
//
// The player aggregate observes orchestration events for the three
// flavours (buy-in / registration / rebuy) so its state mirror tracks
// pending lifecycle records. This is the player-side projection of
// reservation aggregate events — needed for the "no pending X"
// assertions in player.feature and for unit-tier full-lifecycle
// rebuild scenarios.
//
// Bankroll deductions: only the *Confirmed events actually move the
// bankroll. The corresponding `Funds*` events (FundsReserved /
// FundsDeducted) are still emitted by the reservation→player PM in
// production; the unit-tier player.feature scenarios use a pseudo
// `FundsDeducted` shortcut at *Confirmed time so the rebuilt state
// reflects the post-confirmation bankroll without requiring the PM
// to also fire.

func applyBuyInRequested(state *PlayerState, e *examples.BuyInRequested) {
	id := hex.EncodeToString(e.ReservationId)
	amount := int64(0)
	if e.Amount != nil {
		amount = e.Amount.Amount
	}
	state.PendingBuyIns[id] = &PendingBuyIn{
		TableRoot: e.TableRoot,
		Seat:      e.Seat,
		Amount:    amount,
	}
}

func applyBuyInConfirmedOnPlayer(state *PlayerState, e *examples.BuyInConfirmed) {
	id := hex.EncodeToString(e.ReservationId)
	pending, ok := state.PendingBuyIns[id]
	delete(state.PendingBuyIns, id)
	if !ok {
		return
	}
	// Atomically deduct the buy-in amount from the bankroll. Mirrors
	// Python `_apply_buy_in_confirmed_player_side` (state.py).
	state.Bankroll -= pending.Amount
}

func applyBuyInReleased(state *PlayerState, e *examples.BuyInReservationReleased) {
	delete(state.PendingBuyIns, hex.EncodeToString(e.ReservationId))
}

func applyRegistrationRequested(state *PlayerState, e *examples.RegistrationRequested) {
	id := hex.EncodeToString(e.ReservationId)
	fee := int64(0)
	if e.Fee != nil {
		fee = e.Fee.Amount
	}
	state.PendingRegistrations[id] = &PendingRegistration{
		TournamentRoot: e.TournamentRoot,
		Fee:            fee,
	}
}

func applyRegistrationConfirmed(state *PlayerState, e *examples.RegistrationFeeConfirmed) {
	id := hex.EncodeToString(e.ReservationId)
	pending, ok := state.PendingRegistrations[id]
	delete(state.PendingRegistrations, id)
	if !ok {
		return
	}
	state.Bankroll -= pending.Fee
}

func applyRegistrationReleased(state *PlayerState, e *examples.RegistrationFeeReleased) {
	delete(state.PendingRegistrations, hex.EncodeToString(e.ReservationId))
}

func applyRebuyRequested(state *PlayerState, e *examples.RebuyRequested) {
	id := hex.EncodeToString(e.ReservationId)
	fee := int64(0)
	if e.Fee != nil {
		fee = e.Fee.Amount
	}
	state.PendingRebuys[id] = &PendingRebuy{
		TournamentRoot: e.TournamentRoot,
		TableRoot:      e.TableRoot,
		Seat:           e.Seat,
		Fee:            fee,
	}
}

func applyRebuyConfirmed(state *PlayerState, e *examples.RebuyFeeConfirmed) {
	id := hex.EncodeToString(e.ReservationId)
	pending, ok := state.PendingRebuys[id]
	delete(state.PendingRebuys, id)
	if !ok {
		return
	}
	state.Bankroll -= pending.Fee
}

func applyRebuyReleased(state *PlayerState, e *examples.RebuyFeeReleased) {
	delete(state.PendingRebuys, hex.EncodeToString(e.ReservationId))
}

// stateRouter is the fluent state reconstruction router.
var stateRouter = angzarr.NewStateRouter(NewPlayerState).
	On(applyRegistered).
	On(applyDeposited).
	On(applyWithdrawn).
	On(applyReserved).
	On(applyReleased).
	On(applyTransferred).
	On(applyDeducted). // Phase I-Go: MED-EX-2.1.1 (DeductReservedFunds)
	On(applyBuyInRequested).
	On(applyBuyInConfirmedOnPlayer).
	On(applyBuyInReleased).
	On(applyRegistrationRequested).
	On(applyRegistrationConfirmed).
	On(applyRegistrationReleased).
	On(applyRebuyRequested).
	On(applyRebuyConfirmed).
	On(applyRebuyReleased)

// docs:end:state_router

// RebuildState rebuilds player state from event history.
func RebuildState(eventBook *pb.EventBook) PlayerState {
	if eventBook == nil {
		return NewPlayerState()
	}

	// Start from snapshot if available
	if eventBook.Snapshot != nil && eventBook.Snapshot.State != nil {
		if eventBook.Snapshot.State.MessageIs(&examples.PlayerState{}) {
			var snapshot examples.PlayerState
			if err := eventBook.Snapshot.State.UnmarshalTo(&snapshot); err == nil {
				state := applySnapshot(&snapshot)
				// Apply events since snapshot
				for _, page := range eventBook.Pages {
					event := page.GetEvent()
					if event != nil {
						stateRouter.ApplySingle(&state, event)
					}
				}
				return state
			}
		}
	}

	// Apply events using the state router
	state := NewPlayerState()
	for _, page := range eventBook.Pages {
		event := page.GetEvent()
		if event != nil {
			stateRouter.ApplySingle(&state, event)
		}
	}
	return state
}

func applySnapshot(snapshot *examples.PlayerState) PlayerState {
	bankroll := int64(0)
	if snapshot.Bankroll != nil {
		bankroll = snapshot.Bankroll.Amount
	}
	reservedFunds := int64(0)
	if snapshot.ReservedFunds != nil {
		reservedFunds = snapshot.ReservedFunds.Amount
	}

	reservations := make(map[string]int64)
	for k, v := range snapshot.TableReservations {
		reservations[k] = v
	}

	return PlayerState{
		PlayerID:             snapshot.PlayerId,
		DisplayName:          snapshot.DisplayName,
		Email:                snapshot.Email,
		PlayerType:           snapshot.PlayerType,
		AIModelID:            snapshot.AiModelId,
		Bankroll:             bankroll,
		ReservedFunds:        reservedFunds,
		TableReservations:    reservations,
		Status:               snapshot.Status,
		PendingBuyIns:        make(map[string]*PendingBuyIn),
		PendingRegistrations: make(map[string]*PendingRegistration),
		PendingRebuys:        make(map[string]*PendingRebuy),
	}
}
