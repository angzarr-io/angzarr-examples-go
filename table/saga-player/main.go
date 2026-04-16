// Saga: Table → Player (OO Pattern)
//
// Reacts to HandEnded events from Table domain.
// Sends ReleaseFunds commands to Player domain.
//
// Uses the OO-style implementation with SagaBase and method-based
// handlers with fluent registration.
package main

import (
	"encoding/hex"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"google.golang.org/protobuf/types/known/anypb"
)

// TablePlayerSaga translates HandEnded events to ReleaseFunds commands.
type TablePlayerSaga struct {
	angzarr.SagaBase
}

// NewTablePlayerSaga creates a new TablePlayerSaga with registered handlers.
func NewTablePlayerSaga() *TablePlayerSaga {
	s := &TablePlayerSaga{}
	s.Init("saga-table-player", "table", "player")

	// Register event handler (multi because we emit one command per player)
	s.HandlesMulti(s.HandleHandEnded)

	return s
}

// HandleHandEnded translates HandEnded → ReleaseFunds for each player.
// Destinations are now config-driven; uses AngzarrDeferred for sequence stamping.
func (s *TablePlayerSaga) HandleHandEnded(
	event *examples.HandEnded,
	dests []*pb.EventBook,
) ([]*pb.CommandBook, error) {
	var commands []*pb.CommandBook

	// Create ReleaseFunds commands for all players
	for playerHex := range event.StackChanges {
		playerRoot, err := hex.DecodeString(playerHex)
		if err != nil {
			continue
		}

		releaseFunds := &examples.ReleaseFunds{
			TableRoot: event.HandRoot,
		}

		cmdAny, err := anypb.New(releaseFunds)
		if err != nil {
			return nil, err
		}

		commands = append(commands, &pb.CommandBook{
			Cover: &pb.Cover{
				Domain: "player",
				Root:   &pb.UUID{Value: playerRoot},
			},
			Pages: []*pb.CommandPage{
				{
					Header:  &pb.PageHeader{SequenceType: &pb.PageHeader_AngzarrDeferred{AngzarrDeferred: &pb.AngzarrDeferredSequence{}}},
					Payload: &pb.CommandPage_Command{Command: cmdAny},
				},
			},
		})
	}

	return commands, nil
}

// Handle satisfies the OOSaga interface (destinations parameter added in 0.5.0).
func (s *TablePlayerSaga) Handle(source *pb.EventBook, _ *angzarr.Destinations) (*angzarr.SagaHandlerResponse, error) {
	return s.SagaBase.Handle(source)
}

func main() {
	saga := NewTablePlayerSaga()
	angzarr.RunOOSagaServer("saga-table-player", "50213", saga)
}
