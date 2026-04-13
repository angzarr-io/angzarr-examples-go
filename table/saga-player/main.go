// Saga: Table → Player
//
// Reacts to HandEnded events from Table domain.
// Sends ReleaseFunds commands to Player domain.
package main

import (
	"encoding/hex"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// handleHandEnded translates HandEnded → ReleaseFunds for each player.
// Destinations are now config-driven; uses AngzarrDeferred for sequence stamping.
func handleHandEnded(source *pb.EventBook, event *anypb.Any, _ *angzarr.Destinations) ([]*pb.CommandBook, error) {
	var handEnded examples.HandEnded
	if err := proto.Unmarshal(event.Value, &handEnded); err != nil {
		return nil, err
	}

	// Get correlation ID from source
	var correlationID string
	if source.Cover != nil {
		correlationID = source.Cover.CorrelationId
	}

	var commands []*pb.CommandBook

	// Create ReleaseFunds commands for all players
	for playerHex := range handEnded.StackChanges {
		playerRoot, err := hex.DecodeString(playerHex)
		if err != nil {
			continue
		}

		releaseFunds := &examples.ReleaseFunds{
			TableRoot: handEnded.HandRoot,
		}

		cmdAny, err := anypb.New(releaseFunds)
		if err != nil {
			return nil, err
		}

		commands = append(commands, &pb.CommandBook{
			Cover: &pb.Cover{
				Domain:        "player",
				Root:          &pb.UUID{Value: playerRoot},
				CorrelationId: correlationID,
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

func main() {
	router := angzarr.NewEventRouter("saga-table-player").
		Domain("table").
		On("HandEnded", handleHandEnded)

	angzarr.RunSagaServer("saga-table-player", "50213", router)
}
