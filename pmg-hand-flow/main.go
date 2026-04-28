// Process Manager: Hand Flow (OO Pattern)
//
// Orchestrates the flow of poker hands by:
// 1. Subscribing to table and hand domain events
// 2. Managing hand process state machines
// 3. Sending commands to drive hands forward
//
// This example demonstrates the OO pattern using:
// - ProcessManagerBase with generic state
// - Handles() for event processing
// - Applies() for state reconstruction (optional)

// docs:start:pm_handler_oo
package main

import (
	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
)

// docs:start:pm_state_oo
// PMState is the PM's aggregate state (rebuilt from its own events).
// For simplicity in this example, we use a minimal state.
type PMState struct {
	HandRoot       []byte
	HandInProgress bool
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
	pm.WithStateFactory(func() *PMState { return &PMState{} })

	// Register event handlers
	pm.Handles(pm.HandleHandStarted)
	pm.Handles(pm.HandleCardsDealt)
	pm.Handles(pm.HandleBlindPosted)
	pm.Handles(pm.HandleActionTaken)
	pm.Handles(pm.HandleCommunityCardsDealt)
	pm.Handles(pm.HandlePotAwarded)

	return pm
}

// HandleHandStarted processes the HandStarted event.
func (pm *HandFlowPM) HandleHandStarted(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.HandStarted,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	// Initialize hand process (not persisted in this simplified version).
	// The saga-table-hand will send DealCards, so we don't emit commands here.
	return nil, nil, nil
}

// HandleCardsDealt processes the CardsDealt event.
func (pm *HandFlowPM) HandleCardsDealt(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.CardsDealt,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	// Post small blind command.
	// In a real implementation, we'd track state to know which blind to post.
	// For now, we assume the hand aggregate tracks this.
	return nil, nil, nil
}

// HandleBlindPosted processes the BlindPosted event.
func (pm *HandFlowPM) HandleBlindPosted(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.BlindPosted,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	// In a full implementation, we'd check if both blinds are posted
	// and then start the betting round.
	return nil, nil, nil
}

// HandleActionTaken processes the ActionTaken event.
func (pm *HandFlowPM) HandleActionTaken(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.ActionTaken,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	// In a full implementation, we'd check if betting is complete
	// and advance to the next phase.
	return nil, nil, nil
}

// HandleCommunityCardsDealt processes the CommunityCardsDealt event.
func (pm *HandFlowPM) HandleCommunityCardsDealt(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.CommunityCardsDealt,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	// Start new betting round after community cards.
	return nil, nil, nil
}

// HandlePotAwarded processes the PotAwarded event.
func (pm *HandFlowPM) HandlePotAwarded(
	trigger *pb.EventBook,
	state *PMState,
	event *examples.PotAwarded,
	dests *angzarr.Destinations,
) ([]*pb.CommandBook, *pb.EventBook, error) {
	// Hand is complete. Clean up.
	return nil, nil, nil
}

// docs:end:pm_handler_oo

func main() {
	pm := NewHandFlowPM()
	angzarr.RunOOProcessManagerServer("pmg-hand-flow", "50291", pm)
}
