// Saga: Hand → Table (OO Pattern)
//
// Reacts to HandComplete events from Hand domain.
// Sends EndHand commands to Table domain.
//
// Uses the OO-style implementation with SagaBase and method-based
// handlers with fluent registration. The Handle method is overridden
// to pass the source hand root (from the EventBook cover) to the handler.
package main

import (
	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"google.golang.org/protobuf/types/known/anypb"
)

// HandTableSaga translates HandComplete events to EndHand commands.
type HandTableSaga struct {
	angzarr.SagaBase
	// handRoot is set by Handle before dispatching to the handler method.
	handRoot []byte
}

// NewHandTableSaga creates a new HandTableSaga with registered handlers.
func NewHandTableSaga() *HandTableSaga {
	s := &HandTableSaga{}
	s.Init("saga-hand-table", "hand", "table")

	// Register event handler
	s.Handles(s.HandleHandComplete)

	return s
}

// HandleHandComplete translates HandComplete → EndHand.
// Sagas are stateless translators - framework handles sequence stamping.
func (s *HandTableSaga) HandleHandComplete(
	event *examples.HandComplete,
) (*pb.CommandBook, error) {
	// Convert PotWinner to PotResult
	results := make([]*examples.PotResult, len(event.Winners))
	for i, winner := range event.Winners {
		results[i] = &examples.PotResult{
			WinnerRoot:  winner.PlayerRoot,
			Amount:      winner.Amount,
			PotType:     winner.PotType,
			WinningHand: winner.WinningHand,
		}
	}

	// Build EndHand command
	// HandRoot comes from the source aggregate's root (set by Handle)
	endHand := &examples.EndHand{
		HandRoot: s.handRoot,
		Results:  results,
	}

	cmdAny, err := anypb.New(endHand)
	if err != nil {
		return nil, err
	}

	// Use angzarr_deferred - framework stamps sequence on delivery
	return &pb.CommandBook{
		Cover: &pb.Cover{
			Domain: "table",
			Root:   &pb.UUID{Value: event.TableRoot},
		},
		Pages: []*pb.CommandPage{
			{
				Header:  &pb.PageHeader{SequenceType: &pb.PageHeader_AngzarrDeferred{AngzarrDeferred: &pb.AngzarrDeferredSequence{}}},
				Payload: &pb.CommandPage_Command{Command: cmdAny},
			},
		},
	}, nil
}

// Handle satisfies the OOSaga interface. Extracts the hand root from the source
// EventBook cover before delegating to the base handler.
func (s *HandTableSaga) Handle(source *pb.EventBook, _ *angzarr.Destinations) (*angzarr.SagaHandlerResponse, error) {
	// Extract hand root from source aggregate
	if source != nil && source.Cover != nil && source.Cover.Root != nil {
		s.handRoot = source.Cover.Root.Value
	}
	return s.SagaBase.Handle(source)
}

func main() {
	saga := NewHandTableSaga()
	angzarr.RunOOSagaServer("saga-hand-table", "50212", saga)
}
