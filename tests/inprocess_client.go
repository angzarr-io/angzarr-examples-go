package tests

import (
	"fmt"
	"sync"

	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	playerHandlers "github.com/benjaminabbitt/angzarr/examples/go/player/agg/handlers"
	tableHandlers "github.com/benjaminabbitt/angzarr/examples/go/table/agg/handlers"
	"google.golang.org/protobuf/types/known/anypb"
)

// aggregateState tracks the event history and rebuilt state for one aggregate root.
type aggregateState struct {
	eventPages []*pb.EventPage
	domain     string
	root       []byte
}

// inProcessClient dispatches commands directly to handler functions
// without any network call. It maintains per-root event history so
// that state can be rebuilt across multiple commands to the same root.
type inProcessClient struct {
	mu     sync.Mutex
	states map[string]*aggregateState // key = domain + hex(root)
}

// newInProcessClient creates a new in-process command client.
func newInProcessClient() *inProcessClient {
	return &inProcessClient{
		states: make(map[string]*aggregateState),
	}
}

func aggregateKey(domain string, root []byte) string {
	return fmt.Sprintf("%s:%x", domain, root)
}

func (c *inProcessClient) getState(domain string, root []byte) *aggregateState {
	key := aggregateKey(domain, root)
	if s, ok := c.states[key]; ok {
		return s
	}
	s := &aggregateState{
		eventPages: []*pb.EventPage{},
		domain:     domain,
		root:       root,
	}
	c.states[key] = s
	return s
}

func (c *inProcessClient) SendCommand(domain string, root []byte, command *anypb.Any, sequence uint32) (*pb.CommandResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	agg := c.getState(domain, root)

	switch domain {
	case "player":
		return c.handlePlayerCommand(agg, command, sequence)
	case "table":
		return c.handleTableCommand(agg, command, sequence)
	default:
		return nil, fmt.Errorf("unknown domain: %s", domain)
	}
}

func (c *inProcessClient) handlePlayerCommand(agg *aggregateState, command *anypb.Any, sequence uint32) (*pb.CommandResponse, error) {
	// Rebuild state from event history
	eventBook := c.makeEventBook(agg)
	state := playerHandlers.RebuildState(eventBook)

	commandBook := &pb.CommandBook{
		Cover: &pb.Cover{
			Domain: agg.domain,
			Root:   &pb.UUID{Value: agg.root},
		},
	}

	resultBook, err := c.dispatchPlayerCommand(commandBook, command, state, sequence)
	if err != nil {
		return nil, err
	}

	// Append result events to aggregate state
	if resultBook != nil {
		agg.eventPages = append(agg.eventPages, resultBook.Pages...)
	}

	return &pb.CommandResponse{
		Events: resultBook,
	}, nil
}

func (c *inProcessClient) dispatchPlayerCommand(commandBook *pb.CommandBook, command *anypb.Any, state playerHandlers.PlayerState, sequence uint32) (*pb.EventBook, error) {
	switch {
	case command.MessageIs(&examples.RegisterPlayer{}):
		return playerHandlers.HandleRegisterPlayer(commandBook, command, state, sequence)
	case command.MessageIs(&examples.DepositFunds{}):
		return playerHandlers.HandleDepositFunds(commandBook, command, state, sequence)
	case command.MessageIs(&examples.WithdrawFunds{}):
		return playerHandlers.HandleWithdrawFunds(commandBook, command, state, sequence)
	case command.MessageIs(&examples.ReserveFunds{}):
		return playerHandlers.HandleReserveFunds(commandBook, command, state, sequence)
	case command.MessageIs(&examples.ReleaseFunds{}):
		return playerHandlers.HandleReleaseFunds(commandBook, command, state, sequence)
	default:
		return nil, fmt.Errorf("unknown player command type: %s", command.TypeUrl)
	}
}

func (c *inProcessClient) handleTableCommand(agg *aggregateState, command *anypb.Any, sequence uint32) (*pb.CommandResponse, error) {
	eventBook := c.makeEventBook(agg)
	state := tableHandlers.RebuildState(eventBook)

	var resultEvent *anypb.Any
	var err error

	switch {
	case command.MessageIs(&examples.CreateTable{}):
		resultEvent, err = tableHandlers.HandleCreateTable(eventBook, command, state)
	case command.MessageIs(&examples.JoinTable{}):
		resultEvent, err = tableHandlers.HandleJoinTable(eventBook, command, state)
	case command.MessageIs(&examples.LeaveTable{}):
		resultEvent, err = tableHandlers.HandleLeaveTable(eventBook, command, state)
	case command.MessageIs(&examples.StartHand{}):
		resultEvent, err = tableHandlers.HandleStartHand(eventBook, command, state)
	case command.MessageIs(&examples.EndHand{}):
		resultEvent, err = tableHandlers.HandleEndHand(eventBook, command, state)
	default:
		return nil, fmt.Errorf("unknown table command type: %s", command.TypeUrl)
	}

	if err != nil {
		return nil, err
	}

	// Wrap single event into an EventBook
	cover := &pb.Cover{
		Domain: agg.domain,
		Root:   &pb.UUID{Value: agg.root},
	}
	resultBook := eventBookFromAny(cover, sequence, resultEvent)

	// Append to aggregate state
	if resultBook != nil {
		agg.eventPages = append(agg.eventPages, resultBook.Pages...)
	}

	return &pb.CommandResponse{
		Events: resultBook,
	}, nil
}

func (c *inProcessClient) makeEventBook(agg *aggregateState) *pb.EventBook {
	return &pb.EventBook{
		Cover: &pb.Cover{
			Domain: agg.domain,
			Root:   &pb.UUID{Value: agg.root},
		},
		Pages:        agg.eventPages,
		NextSequence: uint32(len(agg.eventPages)),
	}
}

func eventBookFromAny(cover *pb.Cover, seq uint32, event *anypb.Any) *pb.EventBook {
	if event == nil {
		return nil
	}
	return &pb.EventBook{
		Cover: cover,
		Pages: []*pb.EventPage{
			{
				Header: &pb.PageHeader{
					SequenceType: &pb.PageHeader_Sequence{Sequence: seq},
				},
				Payload: &pb.EventPage_Event{Event: event},
			},
		},
	}
}

func (c *inProcessClient) Close() {
	// No resources to release for in-process client.
}
