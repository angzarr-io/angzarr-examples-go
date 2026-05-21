// Process Manager: Hand Flow (OO Pattern)
//
// Orchestrates the flow of poker hands by:
// 1. Subscribing to table and hand domain events
// 2. Managing hand process state machines (9-phase machine per Py/Rs)
// 3. Sending commands to drive hands forward
//
// Phase I-Go: HIGH-EX-4.1 (PMState now tracks HandPhase) + HIGH-EX-4.2
// (added 3 missing handlers: BettingRoundComplete, ShowdownStarted,
// HandComplete) per cross-lang spec. Mirrors:
//
//   - examples-python/main/hand-flow/hand_process.py  (HandPhase enum, 9
//     phases)
//   - examples-rust/main/pmg-hand-flow/src/state_machine.rs  (Phase enum,
//     9 handlers)
//
// This example demonstrates the OO pattern using:
// - ProcessManagerBase with generic state
// - Handles() for event processing
// - Applies() for state reconstruction (optional)

// docs:start:pm_handler_oo
package main

import (
	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples"
)

// HandPhase mirrors Py/Rs 9-phase enum. The PM tracks which phase the
// hand is in so phase-aware orchestration (variant-specific transitions,
// draw vs showdown distinction) works.
type HandPhase int32

const (
	PhaseWaitingForStart HandPhase = iota
	PhaseDealing
	PhasePostingBlinds
	PhaseBetting
	PhaseDealingCommunity
	PhaseDraw
	PhaseShowdown
	PhaseAwardingPot
	PhaseComplete
)

// String returns a human-readable name for the phase (useful for logs +
// step assertions).
func (p HandPhase) String() string {
	switch p {
	case PhaseWaitingForStart:
		return "WAITING_FOR_START"
	case PhaseDealing:
		return "DEALING"
	case PhasePostingBlinds:
		return "POSTING_BLINDS"
	case PhaseBetting:
		return "BETTING"
	case PhaseDealingCommunity:
		return "DEALING_COMMUNITY"
	case PhaseDraw:
		return "DRAW"
	case PhaseShowdown:
		return "SHOWDOWN"
	case PhaseAwardingPot:
		return "AWARDING_POT"
	case PhaseComplete:
		return "COMPLETE"
	default:
		return "UNKNOWN"
	}
}

// docs:start:pm_state_oo
// PMState is the PM's aggregate state (rebuilt from its own events).
// Phase I-Go: expanded from {HandRoot, HandInProgress} to track the full
// 9-phase HandPhase enum so cross-lang scenarios that drive phase
// transitions pass against the Py/Rs canonical model.
type PMState struct {
	HandRoot       []byte
	HandInProgress bool
	Phase          HandPhase
	GameVariant    examples.GameVariant
}

// docs:end:pm_state_oo

// HandFlowPM is the OO-style process manager for hand flow orchestration.
type HandFlowPM struct {
	angzarr.ProcessManagerBase[*PMState]
}

// NewHandFlowPM creates a new HandFlowPM with all handlers registered.
func NewHandFlowPM() *HandFlowPM {
	pm := &HandFlowPM{}
	pm.Init("pmg-hand-flow", "pmg-hand-flow", []string{"table", "hand"})
	pm.WithStateFactory(func() *PMState { return &PMState{Phase: PhaseWaitingForStart} })

	// Register event handlers (Phase I-Go: 9 total per Py/Rs canonical).
	pm.Handles(pm.HandleHandStarted)
	pm.Handles(pm.HandleCardsDealt)
	pm.Handles(pm.HandleBlindPosted)
	pm.Handles(pm.HandleActionTaken)
	pm.Handles(pm.HandleBettingRoundComplete)
	pm.Handles(pm.HandleCommunityCardsDealt)
	pm.Handles(pm.HandleShowdownStarted)
	pm.Handles(pm.HandlePotAwarded)
	pm.Handles(pm.HandleHandComplete)

	return pm
}

// HandleHandStarted processes the HandStarted event. Initializes hand
// process; transitions Phase to DEALING.
func (pm *HandFlowPM) HandleHandStarted(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.HandStarted,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	state.HandInProgress = true
	state.HandRoot = event.HandRoot
	state.Phase = PhaseDealing
	return nil, nil, nil
}

// HandleCardsDealt processes the CardsDealt event. Phase moves to
// POSTING_BLINDS (in Texas Hold'em) or BETTING (in Stud variants).
func (pm *HandFlowPM) HandleCardsDealt(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.CardsDealt,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	state.GameVariant = event.GameVariant
	state.Phase = nextPhaseAfterDeal(event.GameVariant)
	return nil, nil, nil
}

// nextPhaseAfterDeal returns the phase to transition to after CardsDealt.
// Mirrors Py `_advance_after_deal` table.
func nextPhaseAfterDeal(variant examples.GameVariant) HandPhase {
	switch variant {
	case examples.GameVariant_SEVEN_CARD_STUD,
		examples.GameVariant_RAZZ,
		examples.GameVariant_STUD_HI_LO_8B:
		return PhaseBetting // Stud-family skips blinds; goes straight to betting after the bring-in.
	default:
		return PhasePostingBlinds
	}
}

// HandleBlindPosted processes the BlindPosted event. Phase moves to
// BETTING once both blinds are posted.
func (pm *HandFlowPM) HandleBlindPosted(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.BlindPosted,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	state.Phase = PhaseBetting
	return nil, nil, nil
}

// HandleActionTaken processes the ActionTaken event. Phase stays at
// BETTING unless a BettingRoundComplete arrives.
func (pm *HandFlowPM) HandleActionTaken(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.ActionTaken,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	state.Phase = PhaseBetting
	return nil, nil, nil
}

// HandleBettingRoundComplete advances out of BETTING. Phase moves to
// DEALING_COMMUNITY for board-based variants, or to DRAW for FCD, or to
// SHOWDOWN when the final street has been completed.
func (pm *HandFlowPM) HandleBettingRoundComplete(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.BettingRoundComplete,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	state.Phase = nextPhaseAfterBettingRound(state.GameVariant, event.CompletedPhase)
	return nil, nil, nil
}

// nextPhaseAfterBettingRound returns the phase to transition to after a
// betting round ends. Mirrors Py `_advance_after_betting_round` table.
func nextPhaseAfterBettingRound(variant examples.GameVariant, phase examples.BettingPhase) HandPhase {
	// River or showdown phase → showdown.
	if phase == examples.BettingPhase_RIVER || phase == examples.BettingPhase_SHOWDOWN {
		return PhaseShowdown
	}
	// Pre-draw round in Five-Card Draw → draw phase.
	if variant == examples.GameVariant_FIVE_CARD_DRAW && phase != examples.BettingPhase_DRAW {
		return PhaseDraw
	}
	// Board-based variants (Hold'em, Omaha) → community cards next.
	if variant == examples.GameVariant_TEXAS_HOLDEM || variant == examples.GameVariant_OMAHA || variant == examples.GameVariant_OMAHA_HI_LO_8B {
		return PhaseDealingCommunity
	}
	// Stud variants — return to betting (next street is dealt elsewhere).
	return PhaseBetting
}

// HandleCommunityCardsDealt processes the CommunityCardsDealt event.
// Phase returns to BETTING for the next street.
func (pm *HandFlowPM) HandleCommunityCardsDealt(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.CommunityCardsDealt,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	state.Phase = PhaseBetting
	return nil, nil, nil
}

// HandleShowdownStarted processes the ShowdownStarted event.
func (pm *HandFlowPM) HandleShowdownStarted(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.ShowdownStarted,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	state.Phase = PhaseShowdown
	return nil, nil, nil
}

// HandlePotAwarded processes the PotAwarded event. Phase moves to
// AWARDING_POT (transient) then COMPLETE on HandComplete.
func (pm *HandFlowPM) HandlePotAwarded(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.PotAwarded,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	state.Phase = PhaseAwardingPot
	return nil, nil, nil
}

// HandleHandComplete finalizes the hand process; Phase → COMPLETE.
func (pm *HandFlowPM) HandleHandComplete(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.HandComplete,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	state.HandInProgress = false
	state.Phase = PhaseComplete
	return nil, nil, nil
}

// docs:end:pm_handler_oo

func main() {
	pm := NewHandFlowPM()
	angzarr.RunOOProcessManagerServer("pmg-hand-flow", "50291", pm)
}
