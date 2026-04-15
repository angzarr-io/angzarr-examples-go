package tests

import (
	"context"
	"encoding/hex"
	"fmt"

	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"github.com/benjaminabbitt/angzarr/examples/go/tournament/agg/handlers"
	"github.com/cucumber/godog"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TournamentContext holds state for tournament aggregate scenarios
type TournamentContext struct {
	eventPages  []*pb.EventPage
	state       handlers.TournamentState
	resultEvent *anypb.Any
	resultBook  *pb.EventBook
	lastError   error
}

func newTournamentContext() *TournamentContext {
	return &TournamentContext{
		eventPages: []*pb.EventPage{},
		state:      handlers.NewTournamentState(),
	}
}

// RegisterTournamentSteps registers tournament aggregate step definitions
func RegisterTournamentSteps(ctx *godog.ScenarioContext) {
	tc := newTournamentContext()

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		tc.eventPages = []*pb.EventPage{}
		tc.state = handlers.NewTournamentState()
		tc.resultEvent = nil
		tc.resultBook = nil
		tc.lastError = nil
		return ctx, nil
	})

	// Given steps
	ctx.Step(`^no prior events for the tournament aggregate$`, tc.noPriorEvents)
	ctx.Step(`^a TournamentCreated event for "([^"]*)"$`, tc.tournamentCreated)
	ctx.Step(`^a TournamentCreated event for "([^"]*)" with max-players (\d+)$`, tc.tournamentCreatedMaxPlayers)
	ctx.Step(`^a RegistrationOpened event$`, tc.registrationOpened)
	ctx.Step(`^a RegistrationClosed event$`, tc.registrationClosed)
	ctx.Step(`^a TournamentStarted event$`, tc.tournamentStarted)
	ctx.Step(`^a TournamentPaused event$`, tc.tournamentPaused)
	ctx.Step(`^(\d+) players enrolled$`, tc.nPlayersEnrolled)
	ctx.Step(`^player "([^"]*)" enrolled$`, tc.playerEnrolled)
	ctx.Step(`^player "([^"]*)" enrolled with (\d+) rebuys used$`, tc.playerEnrolledWithRebuys)
	ctx.Step(`^a running tournament$`, tc.runningTournament)
	ctx.Step(`^a running tournament with rebuys enabled max (\d+) cutoff level (\d+)$`, tc.runningTournamentWithRebuys)
	ctx.Step(`^a running tournament with (\d+)-level blind structure$`, tc.runningTournamentWithBlindStructure)
	ctx.Step(`^a running tournament with (\d+) players remaining$`, tc.runningTournamentWithPlayers)
	ctx.Step(`^the current blind level is (\d+)$`, tc.currentBlindLevel)
	ctx.Step(`^player at position (\d+) eliminated$`, tc.playerEliminated)

	// When steps
	ctx.Step(`^I handle a CreateTournament command with name "([^"]*)" buy-in (\d+) and starting-stack (\d+)$`, tc.handleCreateTournament)
	ctx.Step(`^I handle a CreateTournament command with name "([^"]*)" buy-in (\d+) starting-stack (\d+) and max-players (\d+)$`, tc.handleCreateTournamentMaxPlayers)
	ctx.Step(`^I handle an OpenRegistration command$`, tc.handleOpenRegistration)
	ctx.Step(`^I handle a CloseRegistration command$`, tc.handleCloseRegistration)
	ctx.Step(`^I handle an EnrollPlayer command for player "([^"]*)"$`, tc.handleEnrollPlayer)
	ctx.Step(`^I handle a ProcessRebuy command for player "([^"]*)"$`, tc.handleProcessRebuy)
	ctx.Step(`^I handle an AdvanceBlindLevel command$`, tc.handleAdvanceBlindLevel)
	ctx.Step(`^I handle an EliminatePlayer command for player "([^"]*)"$`, tc.handleEliminatePlayer)
	ctx.Step(`^I handle a PauseTournament command with reason "([^"]*)"$`, tc.handlePauseTournament)
	ctx.Step(`^I handle a ResumeTournament command$`, tc.handleResumeTournament)
	ctx.Step(`^I rebuild the tournament state$`, tc.rebuildTournamentState)

	// Then steps
	ctx.Step(`^the result is a (?:examples\.)?TournamentCreated event$`, tc.resultIsTournamentCreated)
	ctx.Step(`^the result is a (?:examples\.)?RegistrationOpened event$`, tc.resultIsRegistrationOpened)
	ctx.Step(`^the result is a (?:examples\.)?RegistrationClosed event$`, tc.resultIsRegistrationClosed)
	ctx.Step(`^the result is a (?:examples\.)?TournamentPlayerEnrolled event$`, tc.resultIsPlayerEnrolled)
	ctx.Step(`^the result is a (?:examples\.)?TournamentEnrollmentRejected event$`, tc.resultIsEnrollmentRejected)
	ctx.Step(`^the result is a (?:examples\.)?RebuyProcessed event$`, tc.resultIsRebuyProcessed)
	ctx.Step(`^the result is a (?:examples\.)?RebuyDenied event$`, tc.resultIsRebuyDenied)
	ctx.Step(`^the result is a (?:examples\.)?BlindLevelAdvanced event$`, tc.resultIsBlindLevelAdvanced)
	ctx.Step(`^the result is a (?:examples\.)?PlayerEliminated event$`, tc.resultIsPlayerEliminated)
	ctx.Step(`^the result is a (?:examples\.)?TournamentPaused event$`, tc.resultIsTournamentPaused)
	ctx.Step(`^the result is a (?:examples\.)?TournamentResumed event$`, tc.resultIsTournamentResumed)
	ctx.Step(`^the tournament event has name "([^"]*)"$`, tc.eventHasName)
	ctx.Step(`^the tournament event has buy_in (\d+)$`, tc.eventHasBuyIn)
	ctx.Step(`^the tournament event has starting_stack (\d+)$`, tc.eventHasStartingStack)
	ctx.Step(`^the tournament event has total_registrations (\d+)$`, tc.eventHasTotalRegistrations)
	ctx.Step(`^the tournament event has fee_paid (\d+)$`, tc.eventHasFeePaid)
	ctx.Step(`^the tournament event has starting_stack (\d+)$`, tc.eventHasStartingStack)
	ctx.Step(`^the tournament event has registration_number (\d+)$`, tc.eventHasRegistrationNumber)
	ctx.Step(`^the tournament event has reason "([^"]*)"$`, tc.eventHasReason)
	ctx.Step(`^the tournament event has rebuy_count (\d+)$`, tc.eventHasRebuyCount)
	ctx.Step(`^the tournament event has level (\d+)$`, tc.eventHasLevel)
	ctx.Step(`^the tournament event has finish_position (\d+)$`, tc.eventHasFinishPosition)
	ctx.Step(`^the tournament state has status "([^"]*)"$`, tc.stateHasStatus)
	ctx.Step(`^the tournament state has (\d+) registered players$`, tc.stateHasRegisteredPlayers)
	ctx.Step(`^the tournament state has (\d+) players remaining$`, tc.stateHasPlayersRemaining)
	ctx.Step(`^the tournament state has prize pool (\d+)$`, tc.stateHasPrizePool)
}

// Helpers

func (tc *TournamentContext) makeEventPage(event *anypb.Any) *pb.EventPage {
	return &pb.EventPage{
		Header:    &pb.PageHeader{SequenceType: &pb.PageHeader_Sequence{Sequence: uint32(len(tc.eventPages))}},
		CreatedAt: timestamppb.Now(),
		Payload:   &pb.EventPage_Event{Event: event},
	}
}

func (tc *TournamentContext) addEvent(event *anypb.Any) {
	tc.eventPages = append(tc.eventPages, tc.makeEventPage(event))
	tc.rebuildState()
}

func (tc *TournamentContext) rebuildState() {
	id := uuid.New()
	eventBook := &pb.EventBook{
		Cover:        &pb.Cover{Domain: "tournament", Root: &pb.UUID{Value: id[:]}},
		Pages:        tc.eventPages,
		NextSequence: uint32(len(tc.eventPages)),
	}
	tc.state = handlers.RebuildState(eventBook)
}

func (tc *TournamentContext) makeCommandBook() *pb.CommandBook {
	id := uuid.New()
	return &pb.CommandBook{Cover: &pb.Cover{Domain: "tournament", Root: &pb.UUID{Value: id[:]}}}
}

func (tc *TournamentContext) handleCommand(handler func(*pb.CommandBook, *anypb.Any, handlers.TournamentState, uint32) (*pb.EventBook, error), cmd proto.Message) {
	cmdAny, _ := anypb.New(cmd)
	result, err := handler(tc.makeCommandBook(), cmdAny, tc.state, uint32(len(tc.eventPages)))
	tc.lastError = err
	SetLastError(tc.lastError)
	tc.resultEvent = nil
	tc.resultBook = nil
	if err == nil && result != nil && len(result.Pages) > 0 {
		tc.resultBook = result
		if event, ok := result.Pages[0].Payload.(*pb.EventPage_Event); ok {
			tc.resultEvent = event.Event
		}
	}
}

func playerRootBytes(name string) []byte {
	// Deterministic root from name for test repeatability
	id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("player:"+name))
	return id[:]
}

// Given implementations

func (tc *TournamentContext) noPriorEvents() error {
	tc.eventPages = []*pb.EventPage{}
	tc.state = handlers.NewTournamentState()
	return nil
}

func (tc *TournamentContext) tournamentCreated(name string) error {
	return tc.tournamentCreatedFull(name, 1000, 10000, 100, 10, nil, nil)
}

func (tc *TournamentContext) tournamentCreatedMaxPlayers(name string, maxPlayers int) error {
	return tc.tournamentCreatedFull(name, 1000, 10000, int32(maxPlayers), 2, nil, nil)
}

func (tc *TournamentContext) tournamentCreatedFull(name string, buyIn, startingStack int64, maxPlayers, minPlayers int32, rebuyConfig *examples.RebuyConfig, blindStructure []*examples.BlindLevel) error {
	event := &examples.TournamentCreated{
		Name:           name,
		GameVariant:    examples.GameVariant_TEXAS_HOLDEM,
		BuyIn:          buyIn,
		StartingStack:  startingStack,
		MaxPlayers:     maxPlayers,
		MinPlayers:     minPlayers,
		RebuyConfig:    rebuyConfig,
		BlindStructure: blindStructure,
		CreatedAt:      timestamppb.Now(),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.addEvent(eventAny)
	return nil
}

func (tc *TournamentContext) registrationOpened() error {
	event := &examples.RegistrationOpened{OpenedAt: timestamppb.Now()}
	eventAny, _ := anypb.New(event)
	tc.addEvent(eventAny)
	return nil
}

func (tc *TournamentContext) registrationClosed() error {
	event := &examples.RegistrationClosed{
		TotalRegistrations: int32(len(tc.state.RegisteredPlayers)),
		ClosedAt:           timestamppb.Now(),
	}
	eventAny, _ := anypb.New(event)
	tc.addEvent(eventAny)
	return nil
}

func (tc *TournamentContext) tournamentStarted() error {
	event := &examples.TournamentStarted{
		TotalPlayers:   int32(len(tc.state.RegisteredPlayers)),
		TotalPrizePool: tc.state.TotalPrizePool,
		StartedAt:      timestamppb.Now(),
	}
	eventAny, _ := anypb.New(event)
	tc.addEvent(eventAny)
	return nil
}

func (tc *TournamentContext) tournamentPaused() error {
	event := &examples.TournamentPaused{Reason: "break", PausedAt: timestamppb.Now()}
	eventAny, _ := anypb.New(event)
	tc.addEvent(eventAny)
	return nil
}

func (tc *TournamentContext) nPlayersEnrolled(n int) error {
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("player-%d", i+1)
		if err := tc.enrollPlayerByName(name); err != nil {
			return err
		}
	}
	return nil
}

func (tc *TournamentContext) playerEnrolled(name string) error {
	return tc.enrollPlayerByName(name)
}

func (tc *TournamentContext) playerEnrolledWithRebuys(name string, rebuys int) error {
	if err := tc.enrollPlayerByName(name); err != nil {
		return err
	}
	// Apply rebuy events
	root := playerRootBytes(name)
	for i := 0; i < rebuys; i++ {
		event := &examples.RebuyProcessed{
			PlayerRoot:  root,
			RebuyCost:   tc.state.RebuyConfig.RebuyCost,
			ChipsAdded:  tc.state.RebuyConfig.RebuyChips,
			RebuyCount:  int32(i + 1),
			ProcessedAt: timestamppb.Now(),
		}
		eventAny, _ := anypb.New(event)
		tc.addEvent(eventAny)
	}
	return nil
}

func (tc *TournamentContext) enrollPlayerByName(name string) error {
	root := playerRootBytes(name)
	event := &examples.TournamentPlayerEnrolled{
		PlayerRoot:         root,
		FeePaid:            tc.state.BuyIn,
		StartingStack:      tc.state.StartingStack,
		RegistrationNumber: int32(len(tc.state.RegisteredPlayers)) + 1,
		EnrolledAt:         timestamppb.Now(),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.addEvent(eventAny)
	return nil
}

func (tc *TournamentContext) runningTournament() error {
	_ = tc.tournamentCreated("Test Tournament")
	_ = tc.registrationOpened()
	_ = tc.nPlayersEnrolled(3)
	_ = tc.registrationClosed()
	_ = tc.tournamentStarted()
	return nil
}

func (tc *TournamentContext) runningTournamentWithRebuys(maxRebuys, cutoffLevel int) error {
	rebuyConfig := &examples.RebuyConfig{
		Enabled:          true,
		MaxRebuys:        int32(maxRebuys),
		RebuyLevelCutoff: int32(cutoffLevel),
		RebuyCost:        1000,
		RebuyChips:       10000,
	}
	blindStructure := make([]*examples.BlindLevel, 10)
	for i := range blindStructure {
		blindStructure[i] = &examples.BlindLevel{
			Level:      int32(i + 1),
			SmallBlind: int64((i + 1) * 25),
			BigBlind:   int64((i + 1) * 50),
		}
	}
	_ = tc.tournamentCreatedFull("Rebuy Tournament", 1000, 10000, 100, 2, rebuyConfig, blindStructure)
	_ = tc.registrationOpened()
	_ = tc.nPlayersEnrolled(3)
	_ = tc.registrationClosed()
	_ = tc.tournamentStarted()
	return nil
}

func (tc *TournamentContext) runningTournamentWithBlindStructure(levels int) error {
	blindStructure := make([]*examples.BlindLevel, levels)
	for i := range blindStructure {
		blindStructure[i] = &examples.BlindLevel{
			Level:      int32(i + 1),
			SmallBlind: int64((i + 1) * 25),
			BigBlind:   int64((i + 1) * 50),
		}
	}
	_ = tc.tournamentCreatedFull("Blind Tournament", 1000, 10000, 100, 2, nil, blindStructure)
	_ = tc.registrationOpened()
	_ = tc.nPlayersEnrolled(3)
	_ = tc.registrationClosed()
	_ = tc.tournamentStarted()
	return nil
}

func (tc *TournamentContext) runningTournamentWithPlayers(n int) error {
	_ = tc.tournamentCreatedFull("Elimination Tournament", 1000, 10000, 100, 2, nil, nil)
	_ = tc.registrationOpened()
	_ = tc.nPlayersEnrolled(n)
	_ = tc.registrationClosed()
	_ = tc.tournamentStarted()
	return nil
}

func (tc *TournamentContext) currentBlindLevel(level int) error {
	for i := 0; i < level; i++ {
		event := &examples.BlindLevelAdvanced{
			Level:      int32(i + 1),
			SmallBlind: int64((i + 1) * 25),
			BigBlind:   int64((i + 1) * 50),
			AdvancedAt: timestamppb.Now(),
		}
		eventAny, _ := anypb.New(event)
		tc.addEvent(eventAny)
	}
	return nil
}

func (tc *TournamentContext) playerEliminated(position int) error {
	// Find first registered player
	for rootHex := range tc.state.RegisteredPlayers {
		rootBytes, _ := hex.DecodeString(rootHex)
		event := &examples.PlayerEliminated{
			PlayerRoot:     rootBytes,
			FinishPosition: tc.state.PlayersRemaining,
			EliminatedAt:   timestamppb.Now(),
		}
		eventAny, _ := anypb.New(event)
		tc.addEvent(eventAny)
		break
	}
	return nil
}

// When implementations

func (tc *TournamentContext) handleCreateTournament(name string, buyIn, startingStack int) error {
	cmd := &examples.CreateTournament{
		Name:          name,
		GameVariant:   examples.GameVariant_TEXAS_HOLDEM,
		BuyIn:         int64(buyIn),
		StartingStack: int64(startingStack),
		MaxPlayers:    100,
		MinPlayers:    2,
	}
	tc.handleCommand(handlers.HandleCreateTournament, cmd)
	return nil
}

func (tc *TournamentContext) handleCreateTournamentMaxPlayers(name string, buyIn, startingStack, maxPlayers int) error {
	cmd := &examples.CreateTournament{
		Name:          name,
		GameVariant:   examples.GameVariant_TEXAS_HOLDEM,
		BuyIn:         int64(buyIn),
		StartingStack: int64(startingStack),
		MaxPlayers:    int32(maxPlayers),
		MinPlayers:    2,
	}
	tc.handleCommand(handlers.HandleCreateTournament, cmd)
	return nil
}

func (tc *TournamentContext) handleOpenRegistration() error {
	cmd := &examples.OpenRegistration{}
	tc.handleCommand(handlers.HandleOpenRegistration, cmd)
	return nil
}

func (tc *TournamentContext) handleCloseRegistration() error {
	cmd := &examples.CloseRegistration{}
	tc.handleCommand(handlers.HandleCloseRegistration, cmd)
	return nil
}

func (tc *TournamentContext) handleEnrollPlayer(name string) error {
	cmd := &examples.EnrollPlayer{
		PlayerRoot:    playerRootBytes(name),
		ReservationId: uuid.New().NodeID(),
	}
	tc.handleCommand(handlers.HandleEnrollPlayer, cmd)
	return nil
}

func (tc *TournamentContext) handleProcessRebuy(name string) error {
	cmd := &examples.ProcessRebuy{
		PlayerRoot:    playerRootBytes(name),
		ReservationId: uuid.New().NodeID(),
	}
	tc.handleCommand(handlers.HandleProcessRebuy, cmd)
	return nil
}

func (tc *TournamentContext) handleAdvanceBlindLevel() error {
	cmd := &examples.AdvanceBlindLevel{}
	tc.handleCommand(handlers.HandleAdvanceBlindLevel, cmd)
	return nil
}

func (tc *TournamentContext) handleEliminatePlayer(name string) error {
	cmd := &examples.EliminatePlayer{
		PlayerRoot: playerRootBytes(name),
	}
	tc.handleCommand(handlers.HandleEliminatePlayer, cmd)
	return nil
}

func (tc *TournamentContext) handlePauseTournament(reason string) error {
	cmd := &examples.PauseTournament{Reason: reason}
	tc.handleCommand(handlers.HandlePauseTournament, cmd)
	return nil
}

func (tc *TournamentContext) handleResumeTournament() error {
	cmd := &examples.ResumeTournament{}
	tc.handleCommand(handlers.HandleResumeTournament, cmd)
	return nil
}

func (tc *TournamentContext) rebuildTournamentState() error {
	tc.rebuildState()
	return nil
}

// Then implementations — real assertions

func (tc *TournamentContext) resultIsTournamentCreated() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(&examples.TournamentCreated{}) {
		return fmt.Errorf("expected TournamentCreated, got %s", tc.resultEvent.TypeUrl)
	}
	return nil
}

func (tc *TournamentContext) resultIsRegistrationOpened() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(&examples.RegistrationOpened{}) {
		return fmt.Errorf("expected RegistrationOpened, got %s", tc.resultEvent.TypeUrl)
	}
	return nil
}

func (tc *TournamentContext) resultIsRegistrationClosed() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(&examples.RegistrationClosed{}) {
		return fmt.Errorf("expected RegistrationClosed, got %s", tc.resultEvent.TypeUrl)
	}
	return nil
}

func (tc *TournamentContext) resultIsPlayerEnrolled() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(&examples.TournamentPlayerEnrolled{}) {
		return fmt.Errorf("expected TournamentPlayerEnrolled, got %s", tc.resultEvent.TypeUrl)
	}
	return nil
}

func (tc *TournamentContext) resultIsEnrollmentRejected() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(&examples.TournamentEnrollmentRejected{}) {
		return fmt.Errorf("expected TournamentEnrollmentRejected, got %s", tc.resultEvent.TypeUrl)
	}
	return nil
}

func (tc *TournamentContext) resultIsRebuyProcessed() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(&examples.RebuyProcessed{}) {
		return fmt.Errorf("expected RebuyProcessed, got %s", tc.resultEvent.TypeUrl)
	}
	return nil
}

func (tc *TournamentContext) resultIsRebuyDenied() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(&examples.RebuyDenied{}) {
		return fmt.Errorf("expected RebuyDenied, got %s", tc.resultEvent.TypeUrl)
	}
	return nil
}

func (tc *TournamentContext) resultIsBlindLevelAdvanced() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(&examples.BlindLevelAdvanced{}) {
		return fmt.Errorf("expected BlindLevelAdvanced, got %s", tc.resultEvent.TypeUrl)
	}
	return nil
}

func (tc *TournamentContext) resultIsPlayerEliminated() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(&examples.PlayerEliminated{}) {
		return fmt.Errorf("expected PlayerEliminated, got %s", tc.resultEvent.TypeUrl)
	}
	return nil
}

func (tc *TournamentContext) resultIsTournamentPaused() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(&examples.TournamentPaused{}) {
		return fmt.Errorf("expected TournamentPaused, got %s", tc.resultEvent.TypeUrl)
	}
	return nil
}

func (tc *TournamentContext) resultIsTournamentResumed() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(&examples.TournamentResumed{}) {
		return fmt.Errorf("expected TournamentResumed, got %s", tc.resultEvent.TypeUrl)
	}
	return nil
}

// Field assertion helpers

func (tc *TournamentContext) eventHasName(expected string) error {
	var event examples.TournamentCreated
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return fmt.Errorf("failed to unmarshal TournamentCreated: %v", err)
	}
	if event.Name != expected {
		return fmt.Errorf("expected name %q, got %q", expected, event.Name)
	}
	return nil
}

func (tc *TournamentContext) eventHasBuyIn(expected int) error {
	var event examples.TournamentCreated
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return fmt.Errorf("failed to unmarshal: %v", err)
	}
	if event.BuyIn != int64(expected) {
		return fmt.Errorf("expected buy_in %d, got %d", expected, event.BuyIn)
	}
	return nil
}

func (tc *TournamentContext) eventHasStartingStack(expected int) error {
	// Try TournamentCreated first
	var created examples.TournamentCreated
	if err := tc.resultEvent.UnmarshalTo(&created); err == nil {
		if created.StartingStack != int64(expected) {
			return fmt.Errorf("expected starting_stack %d, got %d", expected, created.StartingStack)
		}
		return nil
	}
	// Try TournamentPlayerEnrolled
	var enrolled examples.TournamentPlayerEnrolled
	if err := tc.resultEvent.UnmarshalTo(&enrolled); err == nil {
		if enrolled.StartingStack != int64(expected) {
			return fmt.Errorf("expected starting_stack %d, got %d", expected, enrolled.StartingStack)
		}
		return nil
	}
	return fmt.Errorf("event does not have starting_stack field")
}

func (tc *TournamentContext) eventHasTotalRegistrations(expected int) error {
	var event examples.RegistrationClosed
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return fmt.Errorf("failed to unmarshal RegistrationClosed: %v", err)
	}
	if event.TotalRegistrations != int32(expected) {
		return fmt.Errorf("expected total_registrations %d, got %d", expected, event.TotalRegistrations)
	}
	return nil
}

func (tc *TournamentContext) eventHasFeePaid(expected int) error {
	var event examples.TournamentPlayerEnrolled
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return fmt.Errorf("failed to unmarshal: %v", err)
	}
	if event.FeePaid != int64(expected) {
		return fmt.Errorf("expected fee_paid %d, got %d", expected, event.FeePaid)
	}
	return nil
}

func (tc *TournamentContext) eventHasRegistrationNumber(expected int) error {
	var event examples.TournamentPlayerEnrolled
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return fmt.Errorf("failed to unmarshal: %v", err)
	}
	if event.RegistrationNumber != int32(expected) {
		return fmt.Errorf("expected registration_number %d, got %d", expected, event.RegistrationNumber)
	}
	return nil
}

func (tc *TournamentContext) eventHasReason(expected string) error {
	// Try multiple event types that have a reason field
	var rejected examples.TournamentEnrollmentRejected
	if tc.resultEvent.MessageIs(&rejected) {
		_ = tc.resultEvent.UnmarshalTo(&rejected)
		if rejected.Reason != expected {
			return fmt.Errorf("expected reason %q, got %q", expected, rejected.Reason)
		}
		return nil
	}
	var denied examples.RebuyDenied
	if tc.resultEvent.MessageIs(&denied) {
		_ = tc.resultEvent.UnmarshalTo(&denied)
		if denied.Reason != expected {
			return fmt.Errorf("expected reason %q, got %q", expected, denied.Reason)
		}
		return nil
	}
	var paused examples.TournamentPaused
	if tc.resultEvent.MessageIs(&paused) {
		_ = tc.resultEvent.UnmarshalTo(&paused)
		if paused.Reason != expected {
			return fmt.Errorf("expected reason %q, got %q", expected, paused.Reason)
		}
		return nil
	}
	return fmt.Errorf("event does not have reason field")
}

func (tc *TournamentContext) eventHasRebuyCount(expected int) error {
	var event examples.RebuyProcessed
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return fmt.Errorf("failed to unmarshal: %v", err)
	}
	if event.RebuyCount != int32(expected) {
		return fmt.Errorf("expected rebuy_count %d, got %d", expected, event.RebuyCount)
	}
	return nil
}

func (tc *TournamentContext) eventHasLevel(expected int) error {
	var event examples.BlindLevelAdvanced
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return fmt.Errorf("failed to unmarshal: %v", err)
	}
	if event.Level != int32(expected) {
		return fmt.Errorf("expected level %d, got %d", expected, event.Level)
	}
	return nil
}

func (tc *TournamentContext) eventHasFinishPosition(expected int) error {
	var event examples.PlayerEliminated
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return fmt.Errorf("failed to unmarshal: %v", err)
	}
	if event.FinishPosition != int32(expected) {
		return fmt.Errorf("expected finish_position %d, got %d", expected, event.FinishPosition)
	}
	return nil
}

// State assertions

func (tc *TournamentContext) stateHasStatus(expected string) error {
	statusMap := map[string]examples.TournamentStatus{
		"CREATED":           examples.TournamentStatus_TOURNAMENT_CREATED,
		"REGISTRATION_OPEN": examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN,
		"RUNNING":           examples.TournamentStatus_TOURNAMENT_RUNNING,
		"PAUSED":            examples.TournamentStatus_TOURNAMENT_PAUSED,
		"COMPLETED":         examples.TournamentStatus_TOURNAMENT_COMPLETED,
	}
	expectedStatus, ok := statusMap[expected]
	if !ok {
		return fmt.Errorf("unknown status %q", expected)
	}
	if tc.state.Status != expectedStatus {
		return fmt.Errorf("expected status %q, got %v", expected, tc.state.Status)
	}
	return nil
}

func (tc *TournamentContext) stateHasRegisteredPlayers(expected int) error {
	actual := len(tc.state.RegisteredPlayers)
	if actual != expected {
		return fmt.Errorf("expected %d registered players, got %d", expected, actual)
	}
	return nil
}

func (tc *TournamentContext) stateHasPlayersRemaining(expected int) error {
	if tc.state.PlayersRemaining != int32(expected) {
		return fmt.Errorf("expected %d players remaining, got %d", expected, tc.state.PlayersRemaining)
	}
	return nil
}

func (tc *TournamentContext) stateHasPrizePool(expected int) error {
	if tc.state.TotalPrizePool != int64(expected) {
		return fmt.Errorf("expected prize pool %d, got %d", expected, tc.state.TotalPrizePool)
	}
	return nil
}
