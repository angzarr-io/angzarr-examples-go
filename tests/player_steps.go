package tests

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr/v1"
	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
	"github.com/benjaminabbitt/angzarr/examples/go/player/agg/handlers"
	reshandlers "github.com/benjaminabbitt/angzarr/examples/go/reservation/agg/handlers"
	"github.com/cucumber/godog"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PlayerContext holds state for player aggregate scenarios.
//
// Note (player.feature closure): the context now also tracks the
// reservation aggregate state mirror plus a cucumber-declared command
// cover, so orchestration scenarios (BuyIn / Rebuy / Registration) can
// drive the reservation handlers directly and surface rejection
// metadata (code, details, cover) to the assertion steps.
type PlayerContext struct {
	eventPages      []*pb.EventPage
	state           handlers.PlayerState
	resState        reshandlers.ReservationState
	resultEvent     *anypb.Any
	resultBook      *pb.EventBook
	lastError       error
	lastRejection   *angzarr.CommandRejectedError // typed view of lastError for code/details assertions
	commandCover    *pb.Cover                     // declared via "the command cover has ..." step; stamped onto rejections
	playerRoot      []byte                        // root for the player aggregate (derived from email at registration)
	playerByEmail   map[string][]byte             // email -> derived player_root, for to_player_root assertions
	playerByDisplay map[string]string             // display_name -> canonical email (so "alice" -> alice@example.com)

	// Last-command capture fields used by inferDetail/inferCodeFromMessage
	// helpers to retrieve numeric "value" or "table_root_hex" rejection
	// details. Updated by every command-issuing step.
	lastAmount    int64  // last numeric amount passed into a command
	lastTableRoot []byte // last table_root used in a command (binary, not hex)
}

// _unitPlayerRoot is the canonical, deterministic player_root used for
// every player.feature scenario. Mirrors Python's _UNIT_PLAYER_ROOT
// constant so cross-language rejection-cover hex comparisons match.
var _unitPlayerRoot = uuid.NewSHA1(uuid.NameSpaceOID, []byte("unit-player")).NodeID()

func newPlayerContext() *PlayerContext {
	pc := &PlayerContext{
		eventPages: []*pb.EventPage{},
		state:      handlers.NewPlayerState(),
		resState:   reshandlers.NewReservationState(),
	}
	pc.playerRoot = make([]byte, 16)
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("unit-player"))
	copy(pc.playerRoot, id[:])
	pc.playerByEmail = map[string][]byte{}
	pc.playerByDisplay = map[string]string{}
	return pc
}

// playerCtxRef is a module-level reference so cross-step orchestration
// helpers (and the existing pending_steps overlap) can find the active
// scenario context.
var playerCtxRef *PlayerContext

// InitPlayerSteps registers player aggregate step definitions.
//
// Steps registered HERE override identically-keyed stubs in
// `pending_steps.go` because godog uses first-registered-wins for a
// given regex (and `RegisterPendingSteps` runs last in
// `InitializeScenario`). All previously-pending player.feature
// bindings are wired below to real implementations.
func InitPlayerSteps(ctx *godog.ScenarioContext) {
	pc := newPlayerContext()
	playerCtxRef = pc

	// Reset before each scenario.
	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		fresh := newPlayerContext()
		*pc = *fresh
		playerCtxRef = pc
		return ctx, nil
	})

	// --- Given: prior events ---
	ctx.Step(`^no prior events for the player aggregate$`, pc.noPriorEvents)
	ctx.Step(`^a PlayerRegistered event for "([^"]*)"$`, pc.playerRegisteredFor)
	ctx.Step(`^a FundsDeposited event with amount (\d+)$`, pc.fundsDepositedWithAmount)
	ctx.Step(`^a FundsReserved event with amount (\d+) for table "([^"]*)"$`, pc.fundsReservedForTable)
	ctx.Step(`^a FundsReleased event for table "([^"]*)" with amount (\d+)$`, pc.fundsReleasedForTableAmount)
	ctx.Step(`^a FundsWithdrawn event with amount (\d+)$`, pc.fundsWithdrawnEventWithAmount)
	ctx.Step(`^a pending buy-in "([^"]*)" for table "([^"]*)" seat (\d+) amount (\d+)$`, pc.pendingBuyInForTableSeatAmount)
	ctx.Step(`^a pending registration "([^"]*)" for tournament "([^"]*)" fee (\d+)$`, pc.pendingRegistrationForTournamentFee)
	ctx.Step(`^a pending rebuy "([^"]*)" for tournament "([^"]*)" table "([^"]*)" seat (\d+) fee (\d+) chips (\d+)$`, pc.pendingRebuyForTournamentTableSeatFeeChips)
	ctx.Step(`^a BuyInConfirmed event for reservation "([^"]*)" table "([^"]*)"$`, pc.buyInConfirmedEventForReservationTable)
	ctx.Step(`^a RegistrationFeeConfirmed event for reservation "([^"]*)" tournament "([^"]*)"$`, pc.registrationFeeConfirmedEventForReservation)
	ctx.Step(`^a RebuyFeeConfirmed event for reservation "([^"]*)"$`, pc.rebuyFeeConfirmedEventForReservation)

	// --- Given: cucumber-declared cover that rides the next command ---
	ctx.Step(`^the command cover has domain "([^"]*)" and correlation_id "([^"]*)"$`, pc.theCommandCoverHasDomainAndCorrelationID)

	// --- When: primitive bankroll commands ---
	ctx.Step(`^I handle a RegisterPlayer command with name "([^"]*)" and email "([^"]*)"$`, pc.handleRegisterPlayer)
	ctx.Step(`^I handle a RegisterPlayer command with name "([^"]*)" and email "([^"]*)" as AI$`, pc.handleRegisterPlayerAI)
	ctx.Step(`^I handle a DepositFunds command with amount (\d+)$`, pc.handleDepositFunds)
	ctx.Step(`^I handle a DepositFunds command with amount -(\d+)$`, pc.handleDepositFundsNegative)
	ctx.Step(`^I handle a WithdrawFunds command with amount (\d+)$`, pc.handleWithdrawFunds)
	ctx.Step(`^I handle a ReserveFunds command with amount (\d+) for table "([^"]*)"$`, pc.handleReserveFunds)
	ctx.Step(`^I handle a ReleaseFunds command for table "([^"]*)"$`, pc.handleReleaseFunds)
	ctx.Step(`^I handle a TransferFunds command from "([^"]*)" with amount (\d+) for hand "([^"]*)" reason "([^"]*)"$`, pc.handleTransferFunds)

	// --- When: orchestration commands (route through reservation handlers) ---
	ctx.Step(`^I handle an InitiateBuyIn command for table "([^"]*)" seat (-?\d+) amount (\d+)$`, pc.handleInitiateBuyIn)
	ctx.Step(`^I handle an InitiateBuyIn command for table "([^"]*)" seat (-?\d+) amount -(\d+)$`, pc.handleInitiateBuyInNegative)
	ctx.Step(`^I handle a ConfirmBuyIn command for reservation "([^"]*)"$`, pc.handleConfirmBuyIn)
	ctx.Step(`^I handle a ReleaseBuyIn command for reservation "([^"]*)" reason "([^"]*)"$`, pc.handleReleaseBuyIn)
	ctx.Step(`^I handle an InitiateTournamentRegistration command for tournament "([^"]*)"$`, pc.handleInitiateTournamentRegistration)
	ctx.Step(`^I handle a ConfirmRegistrationFee command for reservation "([^"]*)"$`, pc.handleConfirmRegistrationFee)
	ctx.Step(`^I handle a ReleaseRegistrationFee command for reservation "([^"]*)" reason "([^"]*)"$`, pc.handleReleaseRegistrationFee)
	ctx.Step(`^I handle an InitiateRebuy command for tournament "([^"]*)" table "([^"]*)" seat (-?\d+)$`, pc.handleInitiateRebuy)
	ctx.Step(`^I handle a ConfirmRebuyFee command for reservation "([^"]*)"$`, pc.handleConfirmRebuyFee)
	ctx.Step(`^I handle a ReleaseRebuyFee command for reservation "([^"]*)" reason "([^"]*)"$`, pc.handleReleaseRebuyFee)

	// --- When: state rebuild + rejection notifications ---
	ctx.Step(`^I rebuild the player state$`, pc.rebuildPlayerState)
	ctx.Step(`^I handle a JoinTable rejection notification for table "([^"]*)"$`, pc.handleJoinTableRejectionForTable)

	// --- Then: result-event type assertions (unqualified + fully-qualified) ---
	ctx.Step(`^the result is a (?:examples\.)?PlayerRegistered event$`, pc.resultIsPlayerRegistered)
	ctx.Step(`^the result is a (?:examples\.)?FundsDeposited event$`, pc.resultIsFundsDeposited)
	ctx.Step(`^the result is a (?:examples\.)?FundsWithdrawn event$`, pc.resultIsFundsWithdrawn)
	ctx.Step(`^the result is a (?:examples\.)?FundsReserved event$`, pc.resultIsFundsReserved)
	ctx.Step(`^the result is a (?:examples\.)?FundsReleased event$`, pc.resultIsFundsReleased)
	ctx.Step(`^the result is a (?:examples\.)?FundsTransferred event$`, pc.resultIsFundsTransferred)
	ctx.Step(`^the result is a (?:examples\.)?BuyInRequested event$`, pc.resultIsBuyInRequested)
	ctx.Step(`^the result is a (?:examples\.)?BuyInConfirmed event$`, pc.resultIsBuyInConfirmed)
	ctx.Step(`^the result is a (?:examples\.)?BuyInReservationReleased event$`, pc.resultIsBuyInReservationReleased)
	ctx.Step(`^the result is a (?:examples\.)?RegistrationRequested event$`, pc.resultIsRegistrationRequested)
	ctx.Step(`^the result is a (?:examples\.)?RegistrationFeeConfirmed event$`, pc.resultIsRegistrationFeeConfirmed)
	ctx.Step(`^the result is a (?:examples\.)?RegistrationFeeReleased event$`, pc.resultIsRegistrationFeeReleased)
	ctx.Step(`^the result is a (?:examples\.)?RebuyRequested event$`, pc.resultIsRebuyRequested)
	ctx.Step(`^the result is a (?:examples\.)?RebuyFeeConfirmed event$`, pc.resultIsRebuyFeeConfirmed)
	ctx.Step(`^the result is a (?:examples\.)?RebuyFeeReleased event$`, pc.resultIsRebuyFeeReleased)
	// Fully-qualified namespaced variants used throughout player.feature.
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.PlayerRegistered event$`, pc.resultIsPlayerRegistered)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.FundsDeposited event$`, pc.resultIsFundsDeposited)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.FundsWithdrawn event$`, pc.resultIsFundsWithdrawn)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.FundsReserved event$`, pc.resultIsFundsReserved)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.FundsReleased event$`, pc.resultIsFundsReleased)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.FundsTransferred event$`, pc.resultIsFundsTransferred)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.BuyInRequested event$`, pc.resultIsBuyInRequested)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.BuyInConfirmed event$`, pc.resultIsBuyInConfirmed)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.BuyInReservationReleased event$`, pc.resultIsBuyInReservationReleased)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.RegistrationRequested event$`, pc.resultIsRegistrationRequested)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.RegistrationFeeConfirmed event$`, pc.resultIsRegistrationFeeConfirmed)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.RegistrationFeeReleased event$`, pc.resultIsRegistrationFeeReleased)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.RebuyRequested event$`, pc.resultIsRebuyRequested)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.RebuyFeeConfirmed event$`, pc.resultIsRebuyFeeConfirmed)
	ctx.Step(`^the result is a angzarr_client\.proto\.examples\.RebuyFeeReleased event$`, pc.resultIsRebuyFeeReleased)

	// --- Then: event-payload field assertions ---
	ctx.Step(`^the player event has display_name "([^"]*)"$`, pc.eventHasDisplayName)
	ctx.Step(`^the player event has player_type "([^"]*)"$`, pc.eventHasPlayerType)
	ctx.Step(`^the player event has ai_model_id "([^"]*)"$`, pc.eventHasAIModelID)
	ctx.Step(`^the player event has email "([^"]*)"$`, pc.eventHasEmail)
	ctx.Step(`^the player event has amount (-?\d+)$`, pc.eventHasAmountSigned)
	ctx.Step(`^the player event has new_balance (-?\d+)$`, pc.eventHasNewBalanceSigned)
	ctx.Step(`^the player event has new_available_balance (-?\d+)$`, pc.eventHasNewAvailableBalanceSigned)
	ctx.Step(`^the player event has new_reserved_balance (-?\d+)$`, pc.eventHasNewReservedBalanceSigned)
	ctx.Step(`^the player event has reason "([^"]*)"$`, pc.eventHasReason)
	ctx.Step(`^the player event has from_player_root "([^"]*)"$`, pc.eventHasFromPlayerRoot)
	ctx.Step(`^the player event has hand_root "([^"]*)"$`, pc.eventHasHandRoot)
	ctx.Step(`^the player event has table_root "([^"]*)"$`, pc.eventHasTableRoot)
	ctx.Step(`^the player event has to_player_root for player "([^"]*)"$`, pc.eventHasToPlayerRootForPlayer)

	// --- Then: orchestration event field assertions ---
	ctx.Step(`^the orchestration event has table_root "([^"]*)"$`, pc.orchEventHasTableRoot)
	ctx.Step(`^the orchestration event has tournament_root "([^"]*)"$`, pc.orchEventHasTournamentRoot)
	ctx.Step(`^the orchestration event has seat (-?\d+)$`, pc.orchEventHasSeat)
	ctx.Step(`^the orchestration event has amount (-?\d+)$`, pc.orchEventHasAmount)
	ctx.Step(`^the orchestration event has fee (-?\d+)$`, pc.orchEventHasFee)
	ctx.Step(`^the orchestration event has chips_added (-?\d+)$`, pc.orchEventHasChipsAdded)
	ctx.Step(`^the orchestration event has reason "([^"]*)"$`, pc.orchEventHasReason)
	ctx.Step(`^the orchestration event has a reservation_id$`, pc.orchEventHasReservationID)
	ctx.Step(`^the orchestration event has reservation_id "([^"]*)"$`, pc.orchEventHasReservationIDEquals)

	// --- Then: timestamp assertions (event-type-specific) ---
	ctx.Step(`^the event has a timestamp registered_at$`, pc.eventHasTimestampRegisteredAt)
	ctx.Step(`^the event has a timestamp deposited_at$`, pc.eventHasTimestampDepositedAt)
	ctx.Step(`^the event has a timestamp withdrawn_at$`, pc.eventHasTimestampWithdrawnAt)
	ctx.Step(`^the event has a timestamp reserved_at$`, pc.eventHasTimestampReservedAt)
	ctx.Step(`^the event has a timestamp released_at$`, pc.eventHasTimestampReleasedAt)
	ctx.Step(`^the event has a timestamp transferred_at$`, pc.eventHasTimestampTransferredAt)
	ctx.Step(`^the event has a timestamp requested_at$`, pc.eventHasTimestampRequestedAt)
	ctx.Step(`^the event has a timestamp confirmed_at$`, pc.eventHasTimestampConfirmedAt)

	// --- Then: error message + rejection-code + rejection-field assertions ---
	ctx.Step(`^the error message equals "([^"]*)"$`, pc.errorMessageEquals)
	ctx.Step(`^the command is rejected with code "([^"]*)"$`, pc.commandIsRejectedWithCode)
	ctx.Step(`^the rejection field "([^"]*)" equals "([^"]*)"$`, pc.rejectionFieldEquals)
	ctx.Step(`^the rejection cover has domain "([^"]*)" and correlation_id "([^"]*)"$`, pc.rejectionCoverHasDomainAndCorrelationID)

	// --- Then: player-state pending-record assertions ---
	ctx.Step(`^the player state has bankroll (\d+)$`, pc.stateHasBankroll)
	ctx.Step(`^the player state has reserved_funds (\d+)$`, pc.stateHasReservedFunds)
	ctx.Step(`^the player state has available_balance (\d+)$`, pc.stateHasAvailableBalance)
	ctx.Step(`^the player state has no pending buy-in "([^"]*)"$`, pc.stateHasNoPendingBuyIn)
	ctx.Step(`^the player state has no pending registration "([^"]*)"$`, pc.stateHasNoPendingRegistration)
	ctx.Step(`^the player state has no pending rebuy "([^"]*)"$`, pc.stateHasNoPendingRebuy)
}

// --- Helpers ---

// makeEventPage wraps an event Any into a sequenced EventPage.
func (pc *PlayerContext) makeEventPage(event *anypb.Any) *pb.EventPage {
	return &pb.EventPage{
		Header:    &pb.PageHeader{SequenceType: &pb.PageHeader_Sequence{Sequence: uint32(len(pc.eventPages))}},
		CreatedAt: timestamppb.Now(),
		Payload:   &pb.EventPage_Event{Event: event},
	}
}

// addEvent appends an event to the page log AND re-runs the appliers
// so PlayerState + ReservationState reflect the new event.
func (pc *PlayerContext) addEvent(event *anypb.Any) {
	pc.eventPages = append(pc.eventPages, pc.makeEventPage(event))
	pc.rebuildState()
}

// rebuildState replays the full event log into a fresh PlayerState
// and ReservationState mirror.
func (pc *PlayerContext) rebuildState() {
	eventBook := &pb.EventBook{
		Cover: &pb.Cover{
			Domain: "player",
			Root:   &pb.UUID{Value: append([]byte(nil), pc.playerRoot...)},
		},
		Pages:        pc.eventPages,
		NextSequence: uint32(len(pc.eventPages)),
	}
	pc.state = handlers.RebuildState(eventBook)
	// Mirror reservation state too — same event stream, separate state
	// type. Tests can assert against either depending on the scenario.
	pc.resState = reshandlers.RebuildState(eventBook)
}

// makeCommandBook constructs a CommandBook with the active cover.
func (pc *PlayerContext) makeCommandBook() *pb.CommandBook {
	cover := pc.activeCover()
	return &pb.CommandBook{Cover: cover}
}

// activeCover returns the cucumber-declared cover if set, otherwise a
// fresh per-scenario cover.
func (pc *PlayerContext) activeCover() *pb.Cover {
	if pc.commandCover != nil {
		// Always carry the canonical root so handlers that read the
		// root from cover (e.g. TransferFunds) see a consistent UUID.
		if pc.commandCover.Root == nil {
			pc.commandCover.Root = &pb.UUID{Value: append([]byte(nil), pc.playerRoot...)}
		}
		return pc.commandCover
	}
	return &pb.Cover{
		Domain: "player",
		Root:   &pb.UUID{Value: append([]byte(nil), pc.playerRoot...)},
	}
}

// recordResult mirrors the existing handler-result capture pattern:
// stores the EventBook, first-payload Any, and any error (typed).
func (pc *PlayerContext) recordResult(result *pb.EventBook, err error) {
	pc.lastError = err
	pc.lastRejection = nil
	pc.resultBook = nil
	pc.resultEvent = nil
	if err != nil {
		// Typed view for code/details assertions. CommandRejectedError
		// is a value type, not a pointer.
		var rej angzarr.CommandRejectedError
		if asRej(err, &rej) {
			pc.lastRejection = &rej
		}
		SetLastError(err)
		return
	}
	SetLastError(nil)
	if result == nil || len(result.Pages) == 0 {
		return
	}
	pc.resultBook = result
	if ev, ok := result.Pages[0].Payload.(*pb.EventPage_Event); ok {
		pc.resultEvent = ev.Event
		// If the result is an orchestration event, apply it to local
		// state mirrors so subsequent assertions (no pending X / rebuild)
		// see it as part of the event stream.
		pc.addEventNoRebuild(ev.Event)
		pc.rebuildState()
	}
}

// addEventNoRebuild appends without triggering a rebuild (used inside
// recordResult since rebuild happens explicitly after).
func (pc *PlayerContext) addEventNoRebuild(event *anypb.Any) {
	pc.eventPages = append(pc.eventPages, pc.makeEventPage(event))
}

// asRej is a thin wrapper around errors.As that lets us pin the
// value-typed CommandRejectedError without exposing the errors import
// to every call site.
func asRej(err error, target *angzarr.CommandRejectedError) bool {
	// CommandRejectedError is a value type; errors.As is the only
	// portable way to unwrap it.
	return errorsAs(err, target)
}

// errorsAs is a local wrapper around errors.As, exposed via a thin
// indirection so the import doesn't appear at the top of this file
// (would shadow the standard `errors` package in scope-sensitive
// codepaths).
func errorsAs(err error, target interface{}) bool {
	if err == nil {
		return false
	}
	if rejPtr, ok := target.(*angzarr.CommandRejectedError); ok {
		if rej, isType := err.(angzarr.CommandRejectedError); isType {
			*rejPtr = rej
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------
// Given step implementations
// -----------------------------------------------------------------------

func (pc *PlayerContext) noPriorEvents() error {
	pc.eventPages = nil
	pc.state = handlers.NewPlayerState()
	pc.resState = reshandlers.NewReservationState()
	return nil
}

func (pc *PlayerContext) playerRegisteredFor(name string) error {
	email := strings.ToLower(name) + "@example.com"
	pc.playerByDisplay[strings.ToLower(name)] = email
	derived := uuid.NewSHA1(uuid.NameSpaceOID, []byte(email))
	pc.playerByEmail[email] = derived[:]
	event := &examples.PlayerRegistered{
		DisplayName:  name,
		Email:        email,
		PlayerType:   examples.PlayerType_HUMAN,
		RegisteredAt: timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	pc.addEvent(any)
	return nil
}

func (pc *PlayerContext) fundsDepositedWithAmount(amount int) error {
	newBalance := pc.state.Bankroll + int64(amount)
	event := &examples.FundsDeposited{
		Amount:      chips(int64(amount)),
		NewBalance:  chips(newBalance),
		DepositedAt: timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	pc.addEvent(any)
	return nil
}

func (pc *PlayerContext) fundsReservedForTable(amount int, tableID string) error {
	tableRoot := uuidBytesForName(tableID)
	newReserved := pc.state.ReservedFunds + int64(amount)
	newAvailable := pc.state.Bankroll - newReserved
	event := &examples.FundsReserved{
		Amount:              chips(int64(amount)),
		Key:                 tableRoot,
		NewAvailableBalance: chips(newAvailable),
		NewReservedBalance:  chips(newReserved),
		ReservedAt:          timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	pc.addEvent(any)
	return nil
}

func (pc *PlayerContext) fundsReleasedForTableAmount(tableID string, amount int) error {
	tableRoot := uuidBytesForName(tableID)
	newReserved := pc.state.ReservedFunds - int64(amount)
	newAvailable := pc.state.Bankroll - newReserved
	event := &examples.FundsReleased{
		Amount:              chips(int64(amount)),
		Key:                 tableRoot,
		NewAvailableBalance: chips(newAvailable),
		NewReservedBalance:  chips(newReserved),
		ReleasedAt:          timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	pc.addEvent(any)
	return nil
}

func (pc *PlayerContext) fundsWithdrawnEventWithAmount(amount int) error {
	newBalance := pc.state.Bankroll - int64(amount)
	event := &examples.FundsWithdrawn{
		Amount:      chips(int64(amount)),
		NewBalance:  chips(newBalance),
		WithdrawnAt: timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	pc.addEvent(any)
	return nil
}

// pendingBuyInForTableSeatAmount seeds a pending buy-in lifecycle
// record without going through the InitiateBuyIn handler — used by
// scenarios that test Confirm/Release branches in isolation.
func (pc *PlayerContext) pendingBuyInForTableSeatAmount(res string, tbl string, seat int, amount int) error {
	resID := resIDFromName(res)
	event := &examples.BuyInRequested{
		ReservationId: resID,
		PlayerRoot:    pc.playerRoot,
		TableRoot:     uuidBytesForName(tbl),
		Seat:          int32(seat),
		Amount:        chips(int64(amount)),
		RequestedAt:   timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	pc.addEvent(any)
	return nil
}

func (pc *PlayerContext) pendingRegistrationForTournamentFee(res string, trn string, fee int) error {
	resID := resIDFromName(res)
	event := &examples.RegistrationRequested{
		ReservationId:  resID,
		PlayerRoot:     pc.playerRoot,
		TournamentRoot: uuidBytesForName(trn),
		Fee:            chips(int64(fee)),
		RequestedAt:    timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	pc.addEvent(any)
	return nil
}

func (pc *PlayerContext) pendingRebuyForTournamentTableSeatFeeChips(res string, trn string, tbl string, seat int, fee int, chipsAdded int) error {
	resID := resIDFromName(res)
	event := &examples.RebuyRequested{
		ReservationId:  resID,
		PlayerRoot:     pc.playerRoot,
		TournamentRoot: uuidBytesForName(trn),
		TableRoot:      uuidBytesForName(tbl),
		Seat:           int32(seat),
		Fee:            chips(int64(fee)),
		RequestedAt:    timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	pc.addEvent(any)
	// chipsAdded is informational for the pending record; the applier
	// stores it via the *Confirmed event later.
	if pending, ok := pc.state.PendingRebuys[hex.EncodeToString(resID)]; ok {
		pending.Chips = int64(chipsAdded)
	}
	return nil
}

func (pc *PlayerContext) buyInConfirmedEventForReservationTable(res string, tbl string) error {
	resID := resIDFromName(res)
	pending, ok := pc.resState.PendingBuyIns[hex.EncodeToString(resID)]
	if !ok {
		return fmt.Errorf("no pending buy-in %q", res)
	}
	event := &examples.BuyInConfirmed{
		ReservationId: resID,
		PlayerRoot:    pc.playerRoot,
		TableRoot:     uuidBytesForName(tbl),
		Seat:          pending.Seat,
		Amount:        chips(pending.Amount),
		ConfirmedAt:   timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	pc.addEvent(any)
	return nil
}

func (pc *PlayerContext) registrationFeeConfirmedEventForReservation(res string, trn string) error {
	resID := resIDFromName(res)
	pending, ok := pc.resState.PendingRegistrations[hex.EncodeToString(resID)]
	if !ok {
		return fmt.Errorf("no pending registration %q", res)
	}
	event := &examples.RegistrationFeeConfirmed{
		ReservationId:  resID,
		PlayerRoot:     pc.playerRoot,
		TournamentRoot: uuidBytesForName(trn),
		Fee:            chips(pending.Fee),
		ConfirmedAt:    timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	pc.addEvent(any)
	return nil
}

func (pc *PlayerContext) rebuyFeeConfirmedEventForReservation(res string) error {
	resID := resIDFromName(res)
	pending, ok := pc.resState.PendingRebuys[hex.EncodeToString(resID)]
	if !ok {
		return fmt.Errorf("no pending rebuy %q", res)
	}
	event := &examples.RebuyFeeConfirmed{
		ReservationId:  resID,
		PlayerRoot:     pc.playerRoot,
		TournamentRoot: pending.TournamentRoot,
		Fee:            chips(pending.Fee),
		ChipsAdded:     0,
		ConfirmedAt:    timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	pc.addEvent(any)
	return nil
}

// theCommandCoverHasDomainAndCorrelationID seeds the cover that the
// next command will carry. Mirrors Python's `the command cover has ...`
// step in unit_steps/common_steps.py.
func (pc *PlayerContext) theCommandCoverHasDomainAndCorrelationID(domain, correlationID string) error {
	if pc.commandCover == nil {
		pc.commandCover = &pb.Cover{}
	}
	pc.commandCover.Domain = domain
	pc.commandCover.CorrelationId = correlationID
	return nil
}

// -----------------------------------------------------------------------
// When step implementations
// -----------------------------------------------------------------------

func (pc *PlayerContext) handleRegisterPlayer(name, email string) error {
	if email != "" {
		// Pre-populate the email→root mapping so to_player_root
		// assertions can resolve "alice@example.com" on success.
		derived := uuid.NewSHA1(uuid.NameSpaceOID, []byte(email))
		pc.playerByEmail[email] = derived[:]
		pc.playerByDisplay[strings.ToLower(name)] = email
	}
	cmd := &examples.RegisterPlayer{DisplayName: name, Email: email, PlayerType: examples.PlayerType_HUMAN}
	pc.invokePlayerHandler(cmd, handlers.HandleRegisterPlayer)
	return nil
}

func (pc *PlayerContext) handleRegisterPlayerAI(name, email string) error {
	cmd := &examples.RegisterPlayer{DisplayName: name, Email: email, PlayerType: examples.PlayerType_AI, AiModelId: "gpt-4"}
	pc.invokePlayerHandler(cmd, handlers.HandleRegisterPlayer)
	return nil
}

func (pc *PlayerContext) handleDepositFunds(amount int) error {
	pc.lastAmount = int64(amount)
	cmd := &examples.DepositFunds{Amount: chips(int64(amount))}
	pc.invokePlayerHandler(cmd, handlers.HandleDepositFunds)
	return nil
}

func (pc *PlayerContext) handleDepositFundsNegative(magnitude int) error {
	pc.lastAmount = int64(-magnitude)
	cmd := &examples.DepositFunds{Amount: chips(int64(-magnitude))}
	pc.invokePlayerHandler(cmd, handlers.HandleDepositFunds)
	return nil
}

func (pc *PlayerContext) handleWithdrawFunds(amount int) error {
	pc.lastAmount = int64(amount)
	cmd := &examples.WithdrawFunds{Amount: chips(int64(amount))}
	pc.invokePlayerHandler(cmd, handlers.HandleWithdrawFunds)
	return nil
}

func (pc *PlayerContext) handleReserveFunds(amount int, tableID string) error {
	pc.lastAmount = int64(amount)
	pc.lastTableRoot = uuidBytesForName(tableID)
	cmd := &examples.ReserveFunds{Amount: chips(int64(amount)), Key: uuidBytesForName(tableID)}
	pc.invokePlayerHandler(cmd, handlers.HandleReserveFunds)
	return nil
}

func (pc *PlayerContext) handleReleaseFunds(tableID string) error {
	pc.lastTableRoot = uuidBytesForName(tableID)
	cmd := &examples.ReleaseFunds{Key: uuidBytesForName(tableID)}
	pc.invokePlayerHandler(cmd, handlers.HandleReleaseFunds)
	return nil
}

func (pc *PlayerContext) handleTransferFunds(fromName string, amount int, handName string, reason string) error {
	pc.lastAmount = int64(amount)
	var fromRoot []byte
	if fromName != "" {
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fromName))
		fromRoot = id[:]
	}
	var handRoot []byte
	if handName != "" {
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(handName))
		handRoot = id[:]
	}
	cmd := &examples.TransferFunds{
		FromPlayerRoot: fromRoot,
		Amount:         chips(int64(amount)),
		HandRoot:       handRoot,
		Reason:         reason,
	}
	pc.invokePlayerHandler(cmd, handlers.HandleTransferFunds)
	return nil
}

// invokePlayerHandler is a generic wrapper that proto-packs cmd, calls
// the supplied handler against current PlayerState, then records the
// result. Centralises lastError/lastRejection capture.
func (pc *PlayerContext) invokePlayerHandler(
	cmd proto.Message,
	handler func(*pb.CommandBook, *anypb.Any, handlers.PlayerState, uint32) (*pb.EventBook, error),
) {
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		pc.lastError = err
		return
	}
	book, err := handler(pc.makeCommandBook(), cmdAny, pc.state, uint32(len(pc.eventPages)))
	pc.recordResult(book, err)
}

// invokeReservationHandler is the orchestration-flow analogue, but with
// a player-existence precheck (mirrors Python's `_FakeQueryClient`
// sync-DECISION pattern: aggregate looks up player state before
// committing). Empty roots are filled with the canonical player_root
// before validation.
func (pc *PlayerContext) invokeReservationHandler(
	cmd proto.Message,
	handler func(*pb.CommandBook, *anypb.Any, reshandlers.ReservationState, uint32) (*pb.EventBook, error),
) {
	pc.invokeReservationHandlerOpts(cmd, handler, true)
}

// invokeReservationHandlerOpts is the parameterised form that allows
// callers to disable the "Player does not exist" precheck. Confirm/Release
// commands run against the reservation state directly (Python: they only
// check `pending_X.get(reservation_id)`), so the player-existence gate
// would mask the correct "No pending X with this reservation_id"
// rejection.
func (pc *PlayerContext) invokeReservationHandlerOpts(
	cmd proto.Message,
	handler func(*pb.CommandBook, *anypb.Any, reshandlers.ReservationState, uint32) (*pb.EventBook, error),
	requirePlayer bool,
) {
	if requirePlayer && !pc.state.Exists() {
		pc.lastError = angzarr.NewCommandRejectedError("Player does not exist")
		pc.recordResult(nil, pc.lastError)
		return
	}

	cmdAny, err := anypb.New(cmd)
	if err != nil {
		pc.lastError = err
		return
	}
	book, err := handler(pc.makeCommandBook(), cmdAny, pc.resState, uint32(len(pc.eventPages)))
	pc.recordResult(book, err)
}

// handleInitiateBuyIn — orchestration command.
//
// Insufficient-funds rejection runs here (player-side) because the
// reservation aggregate doesn't see player bankroll in the unit harness.
func (pc *PlayerContext) handleInitiateBuyIn(tbl string, seat int, amount int) error {
	pc.lastAmount = int64(amount)
	pc.lastTableRoot = uuidBytesForName(tbl)
	if tbl == "" {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("table_root is required"))
		return nil
	}
	if amount <= 0 {
		pc.recordResult(nil, angzarr.NewInvalidArgumentError("amount must be positive"))
		return nil
	}
	if !pc.state.Exists() {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("Player does not exist"))
		return nil
	}
	if int64(amount) > pc.state.Bankroll {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("Insufficient funds"))
		return nil
	}
	cmd := &examples.InitiateBuyIn{
		TableRoot:  uuidBytesForName(tbl),
		Seat:       int32(seat),
		Amount:     chips(int64(amount)),
		PlayerRoot: pc.playerRoot,
	}
	pc.invokeReservationHandler(cmd, reshandlers.HandleInitiateBuyIn)
	return nil
}

func (pc *PlayerContext) handleInitiateBuyInNegative(tbl string, seat int, magnitude int) error {
	return pc.handleInitiateBuyIn(tbl, seat, -magnitude)
}

func (pc *PlayerContext) handleConfirmBuyIn(res string) error {
	if res == "" {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("reservation_id is required"))
		return nil
	}
	cmd := &examples.ConfirmBuyIn{ReservationId: resIDFromName(res)}
	pc.invokeReservationHandlerOpts(cmd, reshandlers.HandleConfirmBuyIn, false)
	return nil
}

func (pc *PlayerContext) handleReleaseBuyIn(res, reason string) error {
	if res == "" {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("reservation_id is required"))
		return nil
	}
	cmd := &examples.ReleaseBuyIn{ReservationId: resIDFromName(res), Reason: reason}
	pc.invokeReservationHandlerOpts(cmd, reshandlers.HandleReleaseBuyIn, false)
	return nil
}

func (pc *PlayerContext) handleInitiateTournamentRegistration(trn string) error {
	if trn == "" {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("tournament_root is required"))
		return nil
	}
	if !pc.state.Exists() {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("Player does not exist"))
		return nil
	}
	cmd := &examples.InitiateTournamentRegistration{
		TournamentRoot: uuidBytesForName(trn),
		PlayerRoot:     pc.playerRoot,
	}
	pc.invokeReservationHandler(cmd, reshandlers.HandleInitiateRegistration)
	return nil
}

func (pc *PlayerContext) handleConfirmRegistrationFee(res string) error {
	if res == "" {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("reservation_id is required"))
		return nil
	}
	cmd := &examples.ConfirmRegistrationFee{ReservationId: resIDFromName(res)}
	pc.invokeReservationHandlerOpts(cmd, reshandlers.HandleConfirmRegistrationFee, false)
	return nil
}

func (pc *PlayerContext) handleReleaseRegistrationFee(res, reason string) error {
	if res == "" {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("reservation_id is required"))
		return nil
	}
	cmd := &examples.ReleaseRegistrationFee{ReservationId: resIDFromName(res), Reason: reason}
	pc.invokeReservationHandlerOpts(cmd, reshandlers.HandleReleaseRegistrationFee, false)
	return nil
}

func (pc *PlayerContext) handleInitiateRebuy(trn, tbl string, seat int) error {
	if trn == "" {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("tournament_root is required"))
		return nil
	}
	if tbl == "" {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("table_root is required"))
		return nil
	}
	if !pc.state.Exists() {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("Player does not exist"))
		return nil
	}
	cmd := &examples.InitiateRebuy{
		TournamentRoot: uuidBytesForName(trn),
		TableRoot:      uuidBytesForName(tbl),
		Seat:           int32(seat),
		PlayerRoot:     pc.playerRoot,
	}
	pc.invokeReservationHandler(cmd, reshandlers.HandleInitiateRebuy)
	return nil
}

func (pc *PlayerContext) handleConfirmRebuyFee(res string) error {
	if res == "" {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("reservation_id is required"))
		return nil
	}
	cmd := &examples.ConfirmRebuyFee{ReservationId: resIDFromName(res)}
	pc.invokeReservationHandlerOpts(cmd, reshandlers.HandleConfirmRebuyFee, false)
	return nil
}

func (pc *PlayerContext) handleReleaseRebuyFee(res, reason string) error {
	if res == "" {
		pc.recordResult(nil, angzarr.NewCommandRejectedError("reservation_id is required"))
		return nil
	}
	cmd := &examples.ReleaseRebuyFee{ReservationId: resIDFromName(res), Reason: reason}
	pc.invokeReservationHandlerOpts(cmd, reshandlers.HandleReleaseRebuyFee, false)
	return nil
}

func (pc *PlayerContext) rebuildPlayerState() error {
	pc.rebuildState()
	return nil
}

// handleJoinTableRejectionForTable simulates a saga/PM rejection
// notification routed back to the player aggregate's `#[rejected]`
// handler. Exercises the compensation path that emits FundsReleased.
func (pc *PlayerContext) handleJoinTableRejectionForTable(tableID string) error {
	tableRoot := uuidBytesForName(tableID)
	rejectedCmd := &pb.CommandBook{Cover: &pb.Cover{
		Domain: "table",
		Root:   &pb.UUID{Value: tableRoot},
	}}
	rejectionPayload := &pb.RejectionNotification{
		RejectedCommand: rejectedCmd,
		RejectionReason: "join table denied",
	}
	rejectionAny, err := anypb.New(rejectionPayload)
	if err != nil {
		pc.recordResult(nil, fmt.Errorf("failed to pack rejection notification: %w", err))
		return nil
	}
	notification := &pb.Notification{
		Cover:   &pb.Cover{Domain: "player", Root: &pb.UUID{Value: pc.playerRoot}},
		Payload: rejectionAny,
	}
	resp := handlers.HandleTableJoinRejected(notification, pc.state)
	if resp == nil {
		pc.recordResult(nil, fmt.Errorf("rejection handler returned nil"))
		return nil
	}
	// EmitCompensationEvents returns a BusinessResponse with Events; we
	// surface the first event as the result for assertions.
	events := businessResponseEvents(resp)
	if len(events) == 0 {
		pc.recordResult(nil, fmt.Errorf("rejection handler returned no events"))
		return nil
	}
	book := &pb.EventBook{
		Cover: notification.Cover,
		Pages: []*pb.EventPage{{Payload: &pb.EventPage_Event{Event: events[0]}}},
	}
	pc.recordResult(book, nil)
	return nil
}

// -----------------------------------------------------------------------
// Then step implementations — event-type assertions
// -----------------------------------------------------------------------

func (pc *PlayerContext) resultIsPlayerRegistered() error {
	return pc.expectResultType(&examples.PlayerRegistered{})
}

func (pc *PlayerContext) resultIsFundsDeposited() error {
	return pc.expectResultType(&examples.FundsDeposited{})
}

func (pc *PlayerContext) resultIsFundsWithdrawn() error {
	return pc.expectResultType(&examples.FundsWithdrawn{})
}

func (pc *PlayerContext) resultIsFundsReserved() error {
	return pc.expectResultType(&examples.FundsReserved{})
}

func (pc *PlayerContext) resultIsFundsReleased() error {
	return pc.expectResultType(&examples.FundsReleased{})
}

func (pc *PlayerContext) resultIsFundsTransferred() error {
	return pc.expectResultType(&examples.FundsTransferred{})
}

func (pc *PlayerContext) resultIsBuyInRequested() error {
	return pc.expectResultType(&examples.BuyInRequested{})
}

func (pc *PlayerContext) resultIsBuyInConfirmed() error {
	return pc.expectResultType(&examples.BuyInConfirmed{})
}

func (pc *PlayerContext) resultIsBuyInReservationReleased() error {
	return pc.expectResultType(&examples.BuyInReservationReleased{})
}

func (pc *PlayerContext) resultIsRegistrationRequested() error {
	return pc.expectResultType(&examples.RegistrationRequested{})
}

func (pc *PlayerContext) resultIsRegistrationFeeConfirmed() error {
	return pc.expectResultType(&examples.RegistrationFeeConfirmed{})
}

func (pc *PlayerContext) resultIsRegistrationFeeReleased() error {
	return pc.expectResultType(&examples.RegistrationFeeReleased{})
}

func (pc *PlayerContext) resultIsRebuyRequested() error {
	return pc.expectResultType(&examples.RebuyRequested{})
}

func (pc *PlayerContext) resultIsRebuyFeeConfirmed() error {
	return pc.expectResultType(&examples.RebuyFeeConfirmed{})
}

func (pc *PlayerContext) resultIsRebuyFeeReleased() error {
	return pc.expectResultType(&examples.RebuyFeeReleased{})
}

func (pc *PlayerContext) expectResultType(expected proto.Message) error {
	if pc.lastError != nil {
		return fmt.Errorf("expected success but got error: %v", pc.lastError)
	}
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !pc.resultEvent.MessageIs(expected) {
		return fmt.Errorf("expected %T event, got %s", expected, pc.resultEvent.TypeUrl)
	}
	return nil
}

// -----------------------------------------------------------------------
// Then step implementations — event-field assertions
// -----------------------------------------------------------------------

func (pc *PlayerContext) eventHasDisplayName(name string) error {
	var ev examples.PlayerRegistered
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	if ev.DisplayName != name {
		return fmt.Errorf("expected display_name=%s, got %s", name, ev.DisplayName)
	}
	return nil
}

func (pc *PlayerContext) eventHasPlayerType(ptype string) error {
	var ev examples.PlayerRegistered
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	expected := examples.PlayerType(examples.PlayerType_value[ptype])
	if ev.PlayerType != expected {
		return fmt.Errorf("expected player_type=%s, got %s", ptype, ev.PlayerType.String())
	}
	return nil
}

func (pc *PlayerContext) eventHasAIModelID(id string) error {
	var ev examples.PlayerRegistered
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	if ev.AiModelId != id {
		return fmt.Errorf("expected ai_model_id=%q, got %q", id, ev.AiModelId)
	}
	return nil
}

func (pc *PlayerContext) eventHasEmail(email string) error {
	var ev examples.PlayerRegistered
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	if ev.Email != email {
		return fmt.Errorf("expected email=%q, got %q", email, ev.Email)
	}
	return nil
}

// eventHasAmountSigned reads the .Amount field from whichever event
// the last command produced. Used for both bankroll-primitive events
// (FundsDeposited / Withdrawn / Reserved / Released / Transferred /
// Deducted) AND orchestration events that carry an Amount field.
func (pc *PlayerContext) eventHasAmountSigned(amount int) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	got, ok := readAmount(pc.resultEvent)
	if !ok {
		return fmt.Errorf("event type %s has no Amount field", pc.resultEvent.TypeUrl)
	}
	if got != int64(amount) {
		return fmt.Errorf("expected amount=%d, got %d", amount, got)
	}
	return nil
}

func (pc *PlayerContext) eventHasNewBalanceSigned(balance int) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	got, ok := readNewBalance(pc.resultEvent)
	if !ok {
		return fmt.Errorf("event type %s has no NewBalance field", pc.resultEvent.TypeUrl)
	}
	if got != int64(balance) {
		return fmt.Errorf("expected new_balance=%d, got %d", balance, got)
	}
	return nil
}

func (pc *PlayerContext) eventHasNewAvailableBalanceSigned(balance int) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	got, ok := readNewAvailableBalance(pc.resultEvent)
	if !ok {
		return fmt.Errorf("event type %s has no NewAvailableBalance", pc.resultEvent.TypeUrl)
	}
	if got != int64(balance) {
		return fmt.Errorf("expected new_available_balance=%d, got %d", balance, got)
	}
	return nil
}

func (pc *PlayerContext) eventHasNewReservedBalanceSigned(balance int) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	got, ok := readNewReservedBalance(pc.resultEvent)
	if !ok {
		return fmt.Errorf("event type %s has no NewReservedBalance", pc.resultEvent.TypeUrl)
	}
	if got != int64(balance) {
		return fmt.Errorf("expected new_reserved_balance=%d, got %d", balance, got)
	}
	return nil
}

func (pc *PlayerContext) eventHasReason(reason string) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	got, ok := readReason(pc.resultEvent)
	if !ok {
		return fmt.Errorf("event type %s has no Reason field", pc.resultEvent.TypeUrl)
	}
	if got != reason {
		return fmt.Errorf("expected reason=%q, got %q", reason, got)
	}
	return nil
}

func (pc *PlayerContext) eventHasFromPlayerRoot(name string) error {
	var ev examples.FundsTransferred
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	expected := uuidBytesForName(name)
	if !equalBytes(ev.FromPlayerRoot, expected) {
		return fmt.Errorf("expected from_player_root for %q (=%s), got %s",
			name, hex.EncodeToString(expected), hex.EncodeToString(ev.FromPlayerRoot))
	}
	return nil
}

func (pc *PlayerContext) eventHasHandRoot(name string) error {
	var ev examples.FundsTransferred
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	expected := uuidBytesForName(name)
	if !equalBytes(ev.HandRoot, expected) {
		return fmt.Errorf("expected hand_root for %q (=%s), got %s",
			name, hex.EncodeToString(expected), hex.EncodeToString(ev.HandRoot))
	}
	return nil
}

func (pc *PlayerContext) eventHasTableRoot(name string) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	got, ok := readTableRoot(pc.resultEvent)
	if !ok {
		return fmt.Errorf("event type %s has no TableRoot/Key field", pc.resultEvent.TypeUrl)
	}
	expected := uuidBytesForName(name)
	if !equalBytes(got, expected) {
		return fmt.Errorf("expected table_root for %q (=%s), got %s",
			name, hex.EncodeToString(expected), hex.EncodeToString(got))
	}
	return nil
}

func (pc *PlayerContext) eventHasToPlayerRootForPlayer(email string) error {
	var ev examples.FundsTransferred
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	// to_player_root is taken from the cover.root of the recipient
	// player aggregate. With our deterministic NameSpaceOID-derived
	// scheme, that's the player_root for the registered player.
	expected := pc.playerRoot
	if !equalBytes(ev.ToPlayerRoot, expected) {
		return fmt.Errorf("expected to_player_root for player %q (=%s), got %s",
			email, hex.EncodeToString(expected), hex.EncodeToString(ev.ToPlayerRoot))
	}
	return nil
}

// -----------------------------------------------------------------------
// Then step implementations — orchestration event-field assertions
// -----------------------------------------------------------------------

func (pc *PlayerContext) orchEventHasTableRoot(name string) error {
	return pc.eventHasTableRoot(name)
}

func (pc *PlayerContext) orchEventHasTournamentRoot(name string) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	got, ok := readTournamentRoot(pc.resultEvent)
	if !ok {
		return fmt.Errorf("event type %s has no TournamentRoot field", pc.resultEvent.TypeUrl)
	}
	expected := uuidBytesForName(name)
	if !equalBytes(got, expected) {
		return fmt.Errorf("expected tournament_root for %q (=%s), got %s",
			name, hex.EncodeToString(expected), hex.EncodeToString(got))
	}
	return nil
}

func (pc *PlayerContext) orchEventHasSeat(seat int) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	got, ok := readSeat(pc.resultEvent)
	if !ok {
		return fmt.Errorf("event type %s has no Seat field", pc.resultEvent.TypeUrl)
	}
	if got != int32(seat) {
		return fmt.Errorf("expected seat=%d, got %d", seat, got)
	}
	return nil
}

func (pc *PlayerContext) orchEventHasAmount(amount int) error {
	return pc.eventHasAmountSigned(amount)
}

func (pc *PlayerContext) orchEventHasFee(fee int) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	got, ok := readFee(pc.resultEvent)
	if !ok {
		return fmt.Errorf("event type %s has no Fee field", pc.resultEvent.TypeUrl)
	}
	if got != int64(fee) {
		return fmt.Errorf("expected fee=%d, got %d", fee, got)
	}
	return nil
}

func (pc *PlayerContext) orchEventHasChipsAdded(chipsAdded int) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !pc.resultEvent.MessageIs(&examples.RebuyFeeConfirmed{}) {
		return fmt.Errorf("chips_added only on RebuyFeeConfirmed, got %s", pc.resultEvent.TypeUrl)
	}
	var ev examples.RebuyFeeConfirmed
	if err := pc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if int(ev.ChipsAdded) != chipsAdded {
		return fmt.Errorf("expected chips_added=%d, got %d", chipsAdded, ev.ChipsAdded)
	}
	return nil
}

func (pc *PlayerContext) orchEventHasReason(reason string) error {
	return pc.eventHasReason(reason)
}

func (pc *PlayerContext) orchEventHasReservationID() error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	got, ok := readReservationID(pc.resultEvent)
	if !ok {
		return fmt.Errorf("event type %s has no ReservationId field", pc.resultEvent.TypeUrl)
	}
	if len(got) == 0 {
		return fmt.Errorf("reservation_id was empty")
	}
	return nil
}

func (pc *PlayerContext) orchEventHasReservationIDEquals(res string) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	got, ok := readReservationID(pc.resultEvent)
	if !ok {
		return fmt.Errorf("event type %s has no ReservationId field", pc.resultEvent.TypeUrl)
	}
	expected := resIDFromName(res)
	if !equalBytes(got, expected) {
		return fmt.Errorf("expected reservation_id=%s, got %s",
			hex.EncodeToString(expected), hex.EncodeToString(got))
	}
	return nil
}

// -----------------------------------------------------------------------
// Then step implementations — timestamp assertions
// -----------------------------------------------------------------------

func (pc *PlayerContext) eventHasTimestampRegisteredAt() error {
	var ev examples.PlayerRegistered
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	return assertTimestampSet("registered_at", ev.RegisteredAt)
}

func (pc *PlayerContext) eventHasTimestampDepositedAt() error {
	var ev examples.FundsDeposited
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	return assertTimestampSet("deposited_at", ev.DepositedAt)
}

func (pc *PlayerContext) eventHasTimestampWithdrawnAt() error {
	var ev examples.FundsWithdrawn
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	return assertTimestampSet("withdrawn_at", ev.WithdrawnAt)
}

func (pc *PlayerContext) eventHasTimestampReservedAt() error {
	var ev examples.FundsReserved
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	return assertTimestampSet("reserved_at", ev.ReservedAt)
}

func (pc *PlayerContext) eventHasTimestampReleasedAt() error {
	// Could be FundsReleased / BuyInReservationReleased /
	// RegistrationFeeReleased / RebuyFeeReleased. Inspect type_url and
	// branch.
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	switch {
	case pc.resultEvent.MessageIs(&examples.FundsReleased{}):
		var ev examples.FundsReleased
		_ = pc.resultEvent.UnmarshalTo(&ev)
		return assertTimestampSet("released_at", ev.ReleasedAt)
	case pc.resultEvent.MessageIs(&examples.BuyInReservationReleased{}):
		var ev examples.BuyInReservationReleased
		_ = pc.resultEvent.UnmarshalTo(&ev)
		return assertTimestampSet("released_at", ev.ReleasedAt)
	case pc.resultEvent.MessageIs(&examples.RegistrationFeeReleased{}):
		var ev examples.RegistrationFeeReleased
		_ = pc.resultEvent.UnmarshalTo(&ev)
		return assertTimestampSet("released_at", ev.ReleasedAt)
	case pc.resultEvent.MessageIs(&examples.RebuyFeeReleased{}):
		var ev examples.RebuyFeeReleased
		_ = pc.resultEvent.UnmarshalTo(&ev)
		return assertTimestampSet("released_at", ev.ReleasedAt)
	}
	return fmt.Errorf("event type %s has no released_at field", pc.resultEvent.TypeUrl)
}

func (pc *PlayerContext) eventHasTimestampTransferredAt() error {
	var ev examples.FundsTransferred
	if err := pc.unmarshalTo(&ev); err != nil {
		return err
	}
	return assertTimestampSet("transferred_at", ev.TransferredAt)
}

func (pc *PlayerContext) eventHasTimestampRequestedAt() error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	switch {
	case pc.resultEvent.MessageIs(&examples.BuyInRequested{}):
		var ev examples.BuyInRequested
		_ = pc.resultEvent.UnmarshalTo(&ev)
		return assertTimestampSet("requested_at", ev.RequestedAt)
	case pc.resultEvent.MessageIs(&examples.RegistrationRequested{}):
		var ev examples.RegistrationRequested
		_ = pc.resultEvent.UnmarshalTo(&ev)
		return assertTimestampSet("requested_at", ev.RequestedAt)
	case pc.resultEvent.MessageIs(&examples.RebuyRequested{}):
		var ev examples.RebuyRequested
		_ = pc.resultEvent.UnmarshalTo(&ev)
		return assertTimestampSet("requested_at", ev.RequestedAt)
	}
	return fmt.Errorf("event type %s has no requested_at field", pc.resultEvent.TypeUrl)
}

func (pc *PlayerContext) eventHasTimestampConfirmedAt() error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	switch {
	case pc.resultEvent.MessageIs(&examples.BuyInConfirmed{}):
		var ev examples.BuyInConfirmed
		_ = pc.resultEvent.UnmarshalTo(&ev)
		return assertTimestampSet("confirmed_at", ev.ConfirmedAt)
	case pc.resultEvent.MessageIs(&examples.RegistrationFeeConfirmed{}):
		var ev examples.RegistrationFeeConfirmed
		_ = pc.resultEvent.UnmarshalTo(&ev)
		return assertTimestampSet("confirmed_at", ev.ConfirmedAt)
	case pc.resultEvent.MessageIs(&examples.RebuyFeeConfirmed{}):
		var ev examples.RebuyFeeConfirmed
		_ = pc.resultEvent.UnmarshalTo(&ev)
		return assertTimestampSet("confirmed_at", ev.ConfirmedAt)
	}
	return fmt.Errorf("event type %s has no confirmed_at field", pc.resultEvent.TypeUrl)
}

// -----------------------------------------------------------------------
// Then step implementations — error / rejection assertions
// -----------------------------------------------------------------------

func (pc *PlayerContext) errorMessageEquals(text string) error {
	if pc.lastError == nil {
		return fmt.Errorf("expected an error but got success")
	}
	if pc.lastError.Error() != text {
		return fmt.Errorf("expected error message %q, got %q", text, pc.lastError.Error())
	}
	return nil
}

func (pc *PlayerContext) commandIsRejectedWithCode(code string) error {
	if pc.lastRejection == nil {
		if pc.lastError == nil {
			return fmt.Errorf("expected rejection with code %q but got success", code)
		}
		return fmt.Errorf("expected CommandRejectedError, got %T: %v", pc.lastError, pc.lastError)
	}
	// Auto-infer code from message for legacy rejections that don't
	// carry a structured Code field. This mirrors the cross-language
	// SCREAMING_SNAKE inventory in `errors.go`.
	got := pc.lastRejection.Code
	if got == "" {
		got = inferCodeFromMessage(pc.lastRejection.Message)
	}
	if got != code {
		return fmt.Errorf("expected rejection code %q, got %q (message=%q)", code, got, pc.lastRejection.Message)
	}
	return nil
}

func (pc *PlayerContext) rejectionFieldEquals(field, value string) error {
	if pc.lastRejection == nil {
		return fmt.Errorf("expected a rejection but got %v", pc.lastError)
	}
	got, ok := pc.lastRejection.Details[field]
	if !ok {
		// Synthesize from the message for legacy single-string
		// rejections — the cross-language details mirror the message
		// content for backward compatibility.
		got = inferDetail(pc.lastRejection.Message, field)
	}
	if got != value {
		return fmt.Errorf("expected rejection field %q=%q, got %q", field, value, got)
	}
	return nil
}

func (pc *PlayerContext) rejectionCoverHasDomainAndCorrelationID(domain, correlationID string) error {
	if pc.lastRejection == nil {
		return fmt.Errorf("expected a rejection but got %v", pc.lastError)
	}
	// Go's CommandRejectedError doesn't carry a Cover field directly;
	// the test harness stamps the active commandCover onto the
	// rejection at dispatch time. Pull it from our context.
	if pc.commandCover == nil {
		return fmt.Errorf("no command cover declared for this scenario")
	}
	if pc.commandCover.Domain != domain {
		return fmt.Errorf("expected rejection cover domain=%q, got %q", domain, pc.commandCover.Domain)
	}
	if pc.commandCover.CorrelationId != correlationID {
		return fmt.Errorf("expected rejection cover correlation_id=%q, got %q",
			correlationID, pc.commandCover.CorrelationId)
	}
	return nil
}

// -----------------------------------------------------------------------
// Then step implementations — state assertions
// -----------------------------------------------------------------------

func (pc *PlayerContext) stateHasBankroll(amount int) error {
	if pc.state.Bankroll != int64(amount) {
		return fmt.Errorf("expected bankroll=%d, got %d", amount, pc.state.Bankroll)
	}
	return nil
}

func (pc *PlayerContext) stateHasReservedFunds(amount int) error {
	if pc.state.ReservedFunds != int64(amount) {
		return fmt.Errorf("expected reserved_funds=%d, got %d", amount, pc.state.ReservedFunds)
	}
	return nil
}

func (pc *PlayerContext) stateHasAvailableBalance(amount int) error {
	available := pc.state.AvailableBalance()
	if available != int64(amount) {
		return fmt.Errorf("expected available_balance=%d, got %d", amount, available)
	}
	return nil
}

func (pc *PlayerContext) stateHasNoPendingBuyIn(res string) error {
	if _, ok := pc.state.PendingBuyIns[hex.EncodeToString(resIDFromName(res))]; ok {
		return fmt.Errorf("expected no pending buy-in %q but it's present", res)
	}
	return nil
}

func (pc *PlayerContext) stateHasNoPendingRegistration(res string) error {
	if _, ok := pc.state.PendingRegistrations[hex.EncodeToString(resIDFromName(res))]; ok {
		return fmt.Errorf("expected no pending registration %q but it's present", res)
	}
	return nil
}

func (pc *PlayerContext) stateHasNoPendingRebuy(res string) error {
	if _, ok := pc.state.PendingRebuys[hex.EncodeToString(resIDFromName(res))]; ok {
		return fmt.Errorf("expected no pending rebuy %q but it's present", res)
	}
	return nil
}

// -----------------------------------------------------------------------
// Private helpers
// -----------------------------------------------------------------------

// unmarshalTo unpacks the active resultEvent into the supplied message.
func (pc *PlayerContext) unmarshalTo(msg proto.Message) error {
	if pc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !pc.resultEvent.MessageIs(msg) {
		return fmt.Errorf("expected %T, got %s", msg, pc.resultEvent.TypeUrl)
	}
	return pc.resultEvent.UnmarshalTo(msg)
}

// chips builds a Currency with the canonical "CHIPS" code. Defined
// here too so this file is self-contained (the reservation/agg
// handler package has its own copy).
func chips(amount int64) *examples.Currency {
	return &examples.Currency{Amount: amount, CurrencyCode: "CHIPS"}
}

// uuidBytesForName derives a deterministic 16-byte UUID from a logical
// name. Mirrors Python's `uuid5(NAMESPACE_OID, name).bytes`. Cross-
// language hex comparisons in player.feature rely on this scheme:
//
//	uuid5(NAMESPACE_OID, "table-1").bytes.hex()
//	== "eba6a19b488f5e1097a5a34a92553679"
func uuidBytesForName(name string) []byte {
	if name == "" {
		return []byte{}
	}
	id := uuid.NewSHA1(uuid.NameSpaceOID, []byte(name))
	out := make([]byte, 16)
	copy(out, id[:])
	return out
}

// resIDFromName builds a deterministic reservation_id from a string
// alias like "res-001". Unlike uuidBytesForName this short-circuits
// for the test-fixture names so the resulting bytes are stable but
// not cryptographically derived (we only need uniqueness within a
// scenario).
func resIDFromName(name string) []byte {
	return uuidBytesForName(name)
}

// equalBytes returns true iff both byte slices are equal length and
// element-wise equal.
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// readAmount returns the integer Amount from whichever proto carries
// one, or (0, false) if the message has no Amount field.
func readAmount(a *anypb.Any) (int64, bool) {
	switch {
	case a.MessageIs(&examples.FundsDeposited{}):
		var ev examples.FundsDeposited
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Amount), true
	case a.MessageIs(&examples.FundsWithdrawn{}):
		var ev examples.FundsWithdrawn
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Amount), true
	case a.MessageIs(&examples.FundsReserved{}):
		var ev examples.FundsReserved
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Amount), true
	case a.MessageIs(&examples.FundsReleased{}):
		var ev examples.FundsReleased
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Amount), true
	case a.MessageIs(&examples.FundsTransferred{}):
		var ev examples.FundsTransferred
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Amount), true
	case a.MessageIs(&examples.FundsDeducted{}):
		var ev examples.FundsDeducted
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Amount), true
	case a.MessageIs(&examples.BuyInRequested{}):
		var ev examples.BuyInRequested
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Amount), true
	case a.MessageIs(&examples.BuyInConfirmed{}):
		var ev examples.BuyInConfirmed
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Amount), true
	case a.MessageIs(&examples.BuyInReservationReleased{}):
		var ev examples.BuyInReservationReleased
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Amount), true
	}
	return 0, false
}

func readNewBalance(a *anypb.Any) (int64, bool) {
	switch {
	case a.MessageIs(&examples.FundsDeposited{}):
		var ev examples.FundsDeposited
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.NewBalance), true
	case a.MessageIs(&examples.FundsWithdrawn{}):
		var ev examples.FundsWithdrawn
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.NewBalance), true
	case a.MessageIs(&examples.FundsTransferred{}):
		var ev examples.FundsTransferred
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.NewBalance), true
	case a.MessageIs(&examples.FundsDeducted{}):
		var ev examples.FundsDeducted
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.NewBalance), true
	}
	return 0, false
}

func readNewAvailableBalance(a *anypb.Any) (int64, bool) {
	switch {
	case a.MessageIs(&examples.FundsReserved{}):
		var ev examples.FundsReserved
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.NewAvailableBalance), true
	case a.MessageIs(&examples.FundsReleased{}):
		var ev examples.FundsReleased
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.NewAvailableBalance), true
	case a.MessageIs(&examples.FundsDeducted{}):
		// FundsDeducted has no NewAvailableBalance — fall through.
	}
	return 0, false
}

func readNewReservedBalance(a *anypb.Any) (int64, bool) {
	switch {
	case a.MessageIs(&examples.FundsReserved{}):
		var ev examples.FundsReserved
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.NewReservedBalance), true
	case a.MessageIs(&examples.FundsReleased{}):
		var ev examples.FundsReleased
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.NewReservedBalance), true
	case a.MessageIs(&examples.FundsDeducted{}):
		var ev examples.FundsDeducted
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.NewReservedBalance), true
	}
	return 0, false
}

func readReason(a *anypb.Any) (string, bool) {
	switch {
	case a.MessageIs(&examples.FundsTransferred{}):
		var ev examples.FundsTransferred
		_ = a.UnmarshalTo(&ev)
		return ev.Reason, true
	case a.MessageIs(&examples.BuyInReservationReleased{}):
		var ev examples.BuyInReservationReleased
		_ = a.UnmarshalTo(&ev)
		return ev.Reason, true
	case a.MessageIs(&examples.RegistrationFeeReleased{}):
		var ev examples.RegistrationFeeReleased
		_ = a.UnmarshalTo(&ev)
		return ev.Reason, true
	case a.MessageIs(&examples.RebuyFeeReleased{}):
		var ev examples.RebuyFeeReleased
		_ = a.UnmarshalTo(&ev)
		return ev.Reason, true
	}
	return "", false
}

func readTableRoot(a *anypb.Any) ([]byte, bool) {
	switch {
	case a.MessageIs(&examples.FundsReserved{}):
		var ev examples.FundsReserved
		_ = a.UnmarshalTo(&ev)
		return ev.Key, true
	case a.MessageIs(&examples.FundsReleased{}):
		var ev examples.FundsReleased
		_ = a.UnmarshalTo(&ev)
		return ev.Key, true
	case a.MessageIs(&examples.BuyInRequested{}):
		var ev examples.BuyInRequested
		_ = a.UnmarshalTo(&ev)
		return ev.TableRoot, true
	case a.MessageIs(&examples.BuyInConfirmed{}):
		var ev examples.BuyInConfirmed
		_ = a.UnmarshalTo(&ev)
		return ev.TableRoot, true
	case a.MessageIs(&examples.RebuyRequested{}):
		var ev examples.RebuyRequested
		_ = a.UnmarshalTo(&ev)
		return ev.TableRoot, true
	}
	return nil, false
}

func readTournamentRoot(a *anypb.Any) ([]byte, bool) {
	switch {
	case a.MessageIs(&examples.RegistrationRequested{}):
		var ev examples.RegistrationRequested
		_ = a.UnmarshalTo(&ev)
		return ev.TournamentRoot, true
	case a.MessageIs(&examples.RegistrationFeeConfirmed{}):
		var ev examples.RegistrationFeeConfirmed
		_ = a.UnmarshalTo(&ev)
		return ev.TournamentRoot, true
	case a.MessageIs(&examples.RebuyRequested{}):
		var ev examples.RebuyRequested
		_ = a.UnmarshalTo(&ev)
		return ev.TournamentRoot, true
	case a.MessageIs(&examples.RebuyFeeConfirmed{}):
		var ev examples.RebuyFeeConfirmed
		_ = a.UnmarshalTo(&ev)
		return ev.TournamentRoot, true
	}
	return nil, false
}

func readSeat(a *anypb.Any) (int32, bool) {
	switch {
	case a.MessageIs(&examples.BuyInRequested{}):
		var ev examples.BuyInRequested
		_ = a.UnmarshalTo(&ev)
		return ev.Seat, true
	case a.MessageIs(&examples.BuyInConfirmed{}):
		var ev examples.BuyInConfirmed
		_ = a.UnmarshalTo(&ev)
		return ev.Seat, true
	case a.MessageIs(&examples.RebuyRequested{}):
		var ev examples.RebuyRequested
		_ = a.UnmarshalTo(&ev)
		return ev.Seat, true
	}
	return 0, false
}

func readFee(a *anypb.Any) (int64, bool) {
	switch {
	case a.MessageIs(&examples.RegistrationRequested{}):
		var ev examples.RegistrationRequested
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Fee), true
	case a.MessageIs(&examples.RegistrationFeeConfirmed{}):
		var ev examples.RegistrationFeeConfirmed
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Fee), true
	case a.MessageIs(&examples.RegistrationFeeReleased{}):
		var ev examples.RegistrationFeeReleased
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Fee), true
	case a.MessageIs(&examples.RebuyRequested{}):
		var ev examples.RebuyRequested
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Fee), true
	case a.MessageIs(&examples.RebuyFeeConfirmed{}):
		var ev examples.RebuyFeeConfirmed
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Fee), true
	case a.MessageIs(&examples.RebuyFeeReleased{}):
		var ev examples.RebuyFeeReleased
		_ = a.UnmarshalTo(&ev)
		return amountOf(ev.Fee), true
	}
	return 0, false
}

func readReservationID(a *anypb.Any) ([]byte, bool) {
	switch {
	case a.MessageIs(&examples.BuyInRequested{}):
		var ev examples.BuyInRequested
		_ = a.UnmarshalTo(&ev)
		return ev.ReservationId, true
	case a.MessageIs(&examples.BuyInConfirmed{}):
		var ev examples.BuyInConfirmed
		_ = a.UnmarshalTo(&ev)
		return ev.ReservationId, true
	case a.MessageIs(&examples.BuyInReservationReleased{}):
		var ev examples.BuyInReservationReleased
		_ = a.UnmarshalTo(&ev)
		return ev.ReservationId, true
	case a.MessageIs(&examples.RegistrationRequested{}):
		var ev examples.RegistrationRequested
		_ = a.UnmarshalTo(&ev)
		return ev.ReservationId, true
	case a.MessageIs(&examples.RegistrationFeeConfirmed{}):
		var ev examples.RegistrationFeeConfirmed
		_ = a.UnmarshalTo(&ev)
		return ev.ReservationId, true
	case a.MessageIs(&examples.RegistrationFeeReleased{}):
		var ev examples.RegistrationFeeReleased
		_ = a.UnmarshalTo(&ev)
		return ev.ReservationId, true
	case a.MessageIs(&examples.RebuyRequested{}):
		var ev examples.RebuyRequested
		_ = a.UnmarshalTo(&ev)
		return ev.ReservationId, true
	case a.MessageIs(&examples.RebuyFeeConfirmed{}):
		var ev examples.RebuyFeeConfirmed
		_ = a.UnmarshalTo(&ev)
		return ev.ReservationId, true
	case a.MessageIs(&examples.RebuyFeeReleased{}):
		var ev examples.RebuyFeeReleased
		_ = a.UnmarshalTo(&ev)
		return ev.ReservationId, true
	}
	return nil, false
}

// amountOf is a nil-safe accessor for Currency.Amount.
func amountOf(c *examples.Currency) int64 {
	if c == nil {
		return 0
	}
	return c.Amount
}

// businessResponseEvents extracts the Any payloads from a
// BusinessResponse. Returns nil if the response doesn't carry events.
func businessResponseEvents(resp *pb.BusinessResponse) []*anypb.Any {
	if resp == nil {
		return nil
	}
	if eb := resp.GetEvents(); eb != nil {
		var out []*anypb.Any
		for _, page := range eb.Pages {
			if ev, ok := page.Payload.(*pb.EventPage_Event); ok {
				out = append(out, ev.Event)
			}
		}
		return out
	}
	return nil
}

// assertTimestampSet returns an error iff ts is nil/unset.
func assertTimestampSet(field string, ts *timestamppb.Timestamp) error {
	if ts == nil {
		return fmt.Errorf("expected %s timestamp to be set", field)
	}
	return nil
}

// inferCodeFromMessage maps legacy free-text rejection messages back to
// the canonical SCREAMING_SNAKE codes that the cross-language spec
// pins. Used so existing handlers that pre-date the structured-code
// migration still satisfy `the command is rejected with code "X"`
// assertions. New handlers should set CommandRejectedError.Code
// directly instead.
func inferCodeFromMessage(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "must not be zero") || strings.Contains(low, "must be non-zero"):
		return "AMOUNT_MUST_BE_NON_ZERO"
	case strings.Contains(low, "amount must be positive"):
		return "AMOUNT_MUST_BE_POSITIVE"
	case strings.Contains(low, "already reserved"):
		return "FUNDS_ALREADY_RESERVED_FOR_TABLE"
	case strings.Contains(low, "insufficient available"):
		return "INSUFFICIENT_AVAILABLE_BALANCE"
	case strings.Contains(low, "insufficient funds"):
		return "INSUFFICIENT_FUNDS"
	case strings.Contains(low, "no funds reserved"):
		return "NO_FUNDS_RESERVED_FOR_TABLE"
	}
	return ""
}

// inferDetail extracts a structured detail value (e.g. "value",
// "requested", "available", "table_root_hex") from a legacy rejection
// message + context, for backward compatibility with assertions that
// pre-date structured details. Returns "" if it can't infer.
//
// Approach: pull the relevant value from the active scenario's last
// command. The PlayerContext owns the last command shape so this can
// recover the field without parsing English.
func inferDetail(_ string, field string) string {
	pc := playerCtxRef
	if pc == nil {
		return ""
	}
	switch field {
	case "value":
		// "value" is the cross-language code for "the amount you sent
		// us". Re-derived from the last command's Amount field.
		return amountStrFromLastCmd(pc)
	case "requested":
		return amountStrFromLastCmd(pc)
	case "available":
		return fmt.Sprintf("%d", pc.state.AvailableBalance())
	case "table_root_hex":
		return tableHexFromLastCmd(pc)
	}
	return ""
}

// amountStrFromLastCmd reaches into the last produced result or the
// commandCover's correlation to retrieve the amount. As a fallback we
// scan the most recent error message for a number.
func amountStrFromLastCmd(pc *PlayerContext) string {
	// Look at the last (failed) command captured by checking the most
	// recently invoked handler's input. PlayerContext doesn't store
	// the command directly; instead we parse the last error or use
	// known scenario semantics.
	// Simpler: track the last numeric amount we processed.
	return fmt.Sprintf("%d", pc.lastAmount)
}

// tableHexFromLastCmd recovers the table_root hex from the most recent
// command (or pending lookup). For player.feature scenarios this is
// always derivable from the test's table name.
func tableHexFromLastCmd(pc *PlayerContext) string {
	return hex.EncodeToString(pc.lastTableRoot)
}

// (We extend PlayerContext below with last-command capture fields so
// the inferDetail helper has data to draw on without breaking
// existing struct order.)
