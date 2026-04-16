// Projector: CloudEvents (OO Pattern)
//
// Transforms player domain events into CloudEvents format for external consumption.
// Filters sensitive fields (email, internal IDs) before publishing.
//
// Uses the OO-style implementation with ProjectorBase. Since CloudEvents requires
// accumulating events across an EventBook, the Handle method is overridden while
// individual event methods provide typed transformation logic.
package main

import (
	"strings"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// CloudEventsProjector transforms player domain events into CloudEvents format.
type CloudEventsProjector struct {
	angzarr.ProjectorBase
}

// NewCloudEventsProjector creates a new CloudEventsProjector with registered handlers.
func NewCloudEventsProjector() *CloudEventsProjector {
	p := &CloudEventsProjector{}
	p.Init("prj-player-cloudevents", []string{"player"})

	// Register projection handlers for typed deserialization
	p.Projects(p.ProjectPlayerRegistered)
	p.Projects(p.ProjectFundsDeposited)
	p.Projects(p.ProjectFundsWithdrawn)

	return p
}

// ProjectPlayerRegistered transforms PlayerRegistered into a public CloudEvent.
func (p *CloudEventsProjector) ProjectPlayerRegistered(event *examples.PlayerRegistered) *pb.Projection {
	// Handled by custom Handle method - this registration enables type matching
	return nil
}

// ProjectFundsDeposited transforms FundsDeposited into a public CloudEvent.
func (p *CloudEventsProjector) ProjectFundsDeposited(event *examples.FundsDeposited) *pb.Projection {
	return nil
}

// ProjectFundsWithdrawn transforms FundsWithdrawn into a public CloudEvent.
func (p *CloudEventsProjector) ProjectFundsWithdrawn(event *examples.FundsWithdrawn) *pb.Projection {
	return nil
}

// Handle overrides the base to accumulate CloudEvents across all pages in the EventBook.
func (p *CloudEventsProjector) Handle(events *pb.EventBook) (*pb.Projection, error) {
	if events == nil || events.Cover == nil {
		return &pb.Projection{}, nil
	}

	var cloudEvents []*pb.CloudEvent
	var lastSeq uint32

	for _, page := range events.Pages {
		event := page.GetEvent()
		if event == nil {
			continue
		}
		lastSeq = page.GetHeader().GetSequence()

		typeURL := event.TypeUrl
		typeName := typeURL[strings.LastIndex(typeURL, ".")+1:]

		cloudEvent := transformToCloudEvent(typeName, event.Value)
		if cloudEvent != nil {
			cloudEvents = append(cloudEvents, cloudEvent)
		}
	}

	// Pack CloudEventsResponse into Projection.Projection field
	ceResponse := &pb.CloudEventsResponse{Events: cloudEvents}
	projectionAny, _ := anypb.New(ceResponse)

	return &pb.Projection{
		Cover:      events.Cover,
		Projector:  "prj-player-cloudevents",
		Sequence:   lastSeq,
		Projection: projectionAny,
	}, nil
}

func transformToCloudEvent(typeName string, data []byte) *pb.CloudEvent {
	switch typeName {
	case "PlayerRegistered":
		var e examples.PlayerRegistered
		if err := proto.Unmarshal(data, &e); err != nil {
			return nil
		}
		// Create public version - filter sensitive fields
		publicData := &examples.PlayerRegistered{
			DisplayName: e.DisplayName,
			PlayerType:  e.PlayerType,
			// Omit: Email (PII), AiModelId (internal)
		}
		dataAny, _ := anypb.New(publicData)
		return &pb.CloudEvent{
			Type: "com.poker.player.registered",
			Data: dataAny,
		}

	case "FundsDeposited":
		var e examples.FundsDeposited
		if err := proto.Unmarshal(data, &e); err != nil {
			return nil
		}
		// Create public version
		publicData := &examples.FundsDeposited{
			Amount: e.Amount,
			// Omit: NewBalance (sensitive account info)
		}
		dataAny, _ := anypb.New(publicData)
		return &pb.CloudEvent{
			Type:       "com.poker.player.deposited",
			Data:       dataAny,
			Extensions: map[string]string{"priority": "normal"},
		}

	case "FundsWithdrawn":
		var e examples.FundsWithdrawn
		if err := proto.Unmarshal(data, &e); err != nil {
			return nil
		}
		publicData := &examples.FundsWithdrawn{
			Amount: e.Amount,
		}
		dataAny, _ := anypb.New(publicData)
		return &pb.CloudEvent{
			Type: "com.poker.player.withdrawn",
			Data: dataAny,
		}
	}

	return nil
}

func main() {
	projector := NewCloudEventsProjector()
	// Use custom Handle method via WithHandle
	handler := angzarr.NewProjectorHandler("prj-player-cloudevents", "player").
		WithHandle(projector.Handle)
	angzarr.RunProjectorServer("prj-player-cloudevents", "50291", handler)
}
