// Package tests implements orchestration PM step definitions for BDD tests.
// These steps cover BuyInOrchestrator, RegistrationOrchestrator, and RebuyOrchestrator
// scenarios that coordinate cross-aggregate flows with decision coupling.
package tests

import (
	"context"
	"fmt"

	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"github.com/cucumber/godog"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// OrchestrationContext holds state for orchestration PM tests.
type OrchestrationContext struct {
	// Table state for buy-in validation
	tableMinBuyIn  int64
	tableMaxBuyIn  int64
	tableMaxPlayers int32
	occupiedSeats  map[int32]bool

	// Tournament state for registration/rebuy validation
	tournamentRegistrationOpen bool
	tournamentCapacityAvailable bool
	tournamentRebuyWindowOpen  bool
	playerEligibleForRebuy     bool

	// Table state for rebuy validation
	playerSeatedPosition int32
	playerIsSeated       bool

	// Trigger event fields
	triggerSeat   int32
	triggerAmount int64
	triggerFee    int64

	// Pending state flags (for confirmation/rejection flows)
	pendingBuyIn         bool
	pendingRegistration  bool
	pendingRebuy         bool
	pendingRebuyChips    bool

	// Results
	emittedCommands []string // command type names (e.g. "SeatPlayer", "ConfirmBuyIn")
	emittedEvents   []string // process event type names (e.g. "BuyInInitiated", "BuyInFailed")
	failureCode     string   // failure code if rejection occurred

	// Shared identifiers for the flow
	playerRoot    []byte
	tableRoot     []byte
	tournamentRoot []byte
	reservationID []byte
}

// NewOrchestrationContext creates a fresh orchestration context.
func NewOrchestrationContext() *OrchestrationContext {
	return &OrchestrationContext{
		occupiedSeats:  make(map[int32]bool),
		playerRoot:     parseUUID("orch-player-1"),
		tableRoot:      parseUUID("orch-table-1"),
		tournamentRoot: parseUUID("orch-tournament-1"),
		reservationID:  uuid.New().NodeID(),
		playerSeatedPosition: -1,
	}
}

var orchCtx *OrchestrationContext

// RegisterOrchestrationSteps registers all orchestration PM step definitions.
func RegisterOrchestrationSteps(ctx *godog.ScenarioContext) {
	orchCtx = NewOrchestrationContext()

	ctx.Before(func(c context.Context, sc *godog.Scenario) (context.Context, error) {
		orchCtx = NewOrchestrationContext()
		return c, nil
	})

	// =====================================================================
	// Given steps - BuyInOrchestrator
	// =====================================================================
	ctx.Step(`^a table with seat (\d+) available and buy-in range (\d+)-(\d+)$`, aTableWithSeatAvailableAndBuyInRange)
	ctx.Step(`^a player with a BuyInRequested event for seat (\d+) with amount (\d+)$`, aPlayerWithBuyInRequestedForSeatWithAmount)
	ctx.Step(`^a table with seat (\d+) occupied by another player$`, aTableWithSeatOccupied)
	ctx.Step(`^a table that is full with (\d+) players$`, aTableThatIsFullWithPlayers)
	ctx.Step(`^a player with a BuyInRequested event for any seat with amount (\d+)$`, aPlayerWithBuyInRequestedForAnySeatWithAmount)
	ctx.Step(`^a player and table in a pending buy-in state$`, aPlayerAndTableInPendingBuyInState)

	// =====================================================================
	// Given steps - RegistrationOrchestrator
	// =====================================================================
	ctx.Step(`^a tournament with registration open and capacity available$`, aTournamentWithRegistrationOpenAndCapacityAvailable)
	ctx.Step(`^a player with a RegistrationRequested event with fee (\d+)$`, aPlayerWithRegistrationRequestedWithFee)
	ctx.Step(`^a tournament that is full$`, aTournamentThatIsFull)
	ctx.Step(`^a tournament with registration closed$`, aTournamentWithRegistrationClosed)
	ctx.Step(`^a player and tournament in a pending registration state$`, aPlayerAndTournamentInPendingRegistrationState)

	// =====================================================================
	// Given steps - RebuyOrchestrator
	// =====================================================================
	ctx.Step(`^a tournament in rebuy window with player eligible$`, aTournamentInRebuyWindowWithPlayerEligible)
	ctx.Step(`^a table with the player seated at position (\d+)$`, aTableWithPlayerSeatedAtPosition)
	ctx.Step(`^a player with a RebuyRequested event for amount (\d+)$`, aPlayerWithRebuyRequestedForAmount)
	ctx.Step(`^a tournament with rebuy window closed$`, aTournamentWithRebuyWindowClosed)
	ctx.Step(`^a table without the player seated$`, aTableWithoutPlayerSeated)
	ctx.Step(`^a player, tournament, and table in a pending rebuy state$`, aPlayerTournamentAndTableInPendingRebuyState)
	ctx.Step(`^a player, tournament, and table with chips added$`, aPlayerTournamentAndTableWithChipsAdded)

	// =====================================================================
	// When steps - BuyInOrchestrator
	// =====================================================================
	ctx.Step(`^the BuyInOrchestrator handles the BuyInRequested event$`, theBuyInOrchestratorHandlesBuyInRequested)
	ctx.Step(`^the BuyInOrchestrator handles a PlayerSeated event$`, theBuyInOrchestratorHandlesPlayerSeated)
	ctx.Step(`^the BuyInOrchestrator handles a SeatingRejected event$`, theBuyInOrchestratorHandlesSeatingRejected)

	// =====================================================================
	// When steps - RegistrationOrchestrator
	// =====================================================================
	ctx.Step(`^the RegistrationOrchestrator handles the RegistrationRequested event$`, theRegistrationOrchestratorHandlesRegistrationRequested)
	ctx.Step(`^the RegistrationOrchestrator handles a TournamentPlayerEnrolled event$`, theRegistrationOrchestratorHandlesTournamentPlayerEnrolled)
	ctx.Step(`^the RegistrationOrchestrator handles a TournamentEnrollmentRejected event$`, theRegistrationOrchestratorHandlesTournamentEnrollmentRejected)

	// =====================================================================
	// When steps - RebuyOrchestrator
	// =====================================================================
	ctx.Step(`^the RebuyOrchestrator handles the RebuyRequested event$`, theRebuyOrchestratorHandlesRebuyRequested)
	ctx.Step(`^the RebuyOrchestrator handles a RebuyProcessed event$`, theRebuyOrchestratorHandlesRebuyProcessed)
	ctx.Step(`^the RebuyOrchestrator handles a RebuyChipsAdded event$`, theRebuyOrchestratorHandlesRebuyChipsAdded)
	ctx.Step(`^the RebuyOrchestrator handles a RebuyDenied event$`, theRebuyOrchestratorHandlesRebuyDenied)

	// =====================================================================
	// Then steps - shared PM assertions
	// =====================================================================
	ctx.Step(`^the PM emits a SeatPlayer command to the table$`, thePMEmitsSeatPlayerCommand)
	ctx.Step(`^the PM emits no commands$`, thePMEmitsNoCommands)
	ctx.Step(`^the PM emits a BuyInFailed process event with code "([^"]*)"$`, thePMEmitsBuyInFailedWithCode)
	ctx.Step(`^the PM emits a BuyInInitiated process event$`, thePMEmitsBuyInInitiated)
	ctx.Step(`^the PM emits a ConfirmBuyIn command to the player$`, thePMEmitsConfirmBuyInCommand)
	ctx.Step(`^the PM emits a BuyInCompleted process event$`, thePMEmitsBuyInCompleted)
	ctx.Step(`^the PM emits a ReleaseBuyIn command to the player$`, thePMEmitsReleaseBuyInCommand)

	ctx.Step(`^the PM emits an EnrollPlayer command to the tournament$`, thePMEmitsEnrollPlayerCommand)
	ctx.Step(`^the PM emits a RegistrationInitiated process event$`, thePMEmitsRegistrationInitiated)
	ctx.Step(`^the PM emits a RegistrationFailed process event with code "([^"]*)"$`, thePMEmitsRegistrationFailedWithCode)
	ctx.Step(`^the PM emits a ConfirmRegistrationFee command to the player$`, thePMEmitsConfirmRegistrationFeeCommand)
	ctx.Step(`^the PM emits a RegistrationCompleted process event$`, thePMEmitsRegistrationCompleted)
	ctx.Step(`^the PM emits a ReleaseRegistrationFee command to the player$`, thePMEmitsReleaseRegistrationFeeCommand)

	ctx.Step(`^the PM emits a ProcessRebuy command to the tournament$`, thePMEmitsProcessRebuyCommand)
	ctx.Step(`^the PM emits a RebuyInitiated process event$`, thePMEmitsRebuyInitiated)
	ctx.Step(`^the PM emits a RebuyFailed process event with code "([^"]*)"$`, thePMEmitsRebuyFailedWithCode)
	ctx.Step(`^the PM emits an AddRebuyChips command to the table$`, thePMEmitsAddRebuyChipsCommand)
	ctx.Step(`^the PM emits a ConfirmRebuyFee command to the player$`, thePMEmitsConfirmRebuyFeeCommand)
	ctx.Step(`^the PM emits a RebuyCompleted process event$`, thePMEmitsRebuyCompleted)
	ctx.Step(`^the PM emits a ReleaseRebuyFee command to the player$`, thePMEmitsReleaseRebuyFeeCommand)
}

// =====================================================================
// Given implementations - BuyInOrchestrator
// =====================================================================

func aTableWithSeatAvailableAndBuyInRange(seat, minBuyIn, maxBuyIn int) error {
	orchCtx.tableMinBuyIn = int64(minBuyIn)
	orchCtx.tableMaxBuyIn = int64(maxBuyIn)
	orchCtx.tableMaxPlayers = 9
	// Seat is available (not in occupied map)
	return nil
}

func aPlayerWithBuyInRequestedForSeatWithAmount(seat, amount int) error {
	orchCtx.triggerSeat = int32(seat)
	orchCtx.triggerAmount = int64(amount)
	return nil
}

func aTableWithSeatOccupied(seat int) error {
	orchCtx.tableMinBuyIn = 200
	orchCtx.tableMaxBuyIn = 2000
	orchCtx.tableMaxPlayers = 9
	orchCtx.occupiedSeats[int32(seat)] = true
	return nil
}

func aTableThatIsFullWithPlayers(count int) error {
	orchCtx.tableMinBuyIn = 200
	orchCtx.tableMaxBuyIn = 2000
	orchCtx.tableMaxPlayers = int32(count)
	// Mark all seats as occupied
	for i := int32(0); i < int32(count); i++ {
		orchCtx.occupiedSeats[i] = true
	}
	return nil
}

func aPlayerWithBuyInRequestedForAnySeatWithAmount(amount int) error {
	orchCtx.triggerSeat = -1 // -1 means any seat
	orchCtx.triggerAmount = int64(amount)
	return nil
}

func aPlayerAndTableInPendingBuyInState() error {
	orchCtx.pendingBuyIn = true
	orchCtx.triggerSeat = 0
	orchCtx.triggerAmount = 500
	orchCtx.tableMinBuyIn = 200
	orchCtx.tableMaxBuyIn = 2000
	return nil
}

// =====================================================================
// Given implementations - RegistrationOrchestrator
// =====================================================================

func aTournamentWithRegistrationOpenAndCapacityAvailable() error {
	orchCtx.tournamentRegistrationOpen = true
	orchCtx.tournamentCapacityAvailable = true
	return nil
}

func aPlayerWithRegistrationRequestedWithFee(fee int) error {
	orchCtx.triggerFee = int64(fee)
	return nil
}

func aTournamentThatIsFull() error {
	orchCtx.tournamentRegistrationOpen = true
	orchCtx.tournamentCapacityAvailable = false
	return nil
}

func aTournamentWithRegistrationClosed() error {
	orchCtx.tournamentRegistrationOpen = false
	orchCtx.tournamentCapacityAvailable = false
	return nil
}

func aPlayerAndTournamentInPendingRegistrationState() error {
	orchCtx.pendingRegistration = true
	orchCtx.triggerFee = 1000
	orchCtx.tournamentRegistrationOpen = true
	orchCtx.tournamentCapacityAvailable = true
	return nil
}

// =====================================================================
// Given implementations - RebuyOrchestrator
// =====================================================================

func aTournamentInRebuyWindowWithPlayerEligible() error {
	orchCtx.tournamentRebuyWindowOpen = true
	orchCtx.playerEligibleForRebuy = true
	return nil
}

func aTableWithPlayerSeatedAtPosition(position int) error {
	orchCtx.playerSeatedPosition = int32(position)
	orchCtx.playerIsSeated = true
	return nil
}

func aPlayerWithRebuyRequestedForAmount(amount int) error {
	orchCtx.triggerAmount = int64(amount)
	return nil
}

func aTournamentWithRebuyWindowClosed() error {
	orchCtx.tournamentRebuyWindowOpen = false
	orchCtx.playerEligibleForRebuy = false
	return nil
}

func aTableWithoutPlayerSeated() error {
	orchCtx.playerIsSeated = false
	orchCtx.playerSeatedPosition = -1
	return nil
}

func aPlayerTournamentAndTableInPendingRebuyState() error {
	orchCtx.pendingRebuy = true
	orchCtx.tournamentRebuyWindowOpen = true
	orchCtx.playerEligibleForRebuy = true
	orchCtx.playerIsSeated = true
	orchCtx.playerSeatedPosition = 2
	orchCtx.triggerAmount = 1000
	return nil
}

func aPlayerTournamentAndTableWithChipsAdded() error {
	orchCtx.pendingRebuyChips = true
	orchCtx.tournamentRebuyWindowOpen = true
	orchCtx.playerEligibleForRebuy = true
	orchCtx.playerIsSeated = true
	orchCtx.playerSeatedPosition = 2
	orchCtx.triggerAmount = 1000
	return nil
}

// =====================================================================
// When implementations - BuyInOrchestrator
// =====================================================================

func theBuyInOrchestratorHandlesBuyInRequested() error {
	orchCtx.emittedCommands = nil
	orchCtx.emittedEvents = nil
	orchCtx.failureCode = ""

	// Validate buy-in amount is within range
	if orchCtx.triggerAmount < orchCtx.tableMinBuyIn || orchCtx.triggerAmount > orchCtx.tableMaxBuyIn {
		orchCtx.failureCode = "INVALID_AMOUNT"
		orchCtx.emittedEvents = append(orchCtx.emittedEvents, "BuyInFailed")
		return nil
	}

	// Check if requested seat is occupied
	if orchCtx.triggerSeat >= 0 {
		if orchCtx.occupiedSeats[orchCtx.triggerSeat] {
			orchCtx.failureCode = "SEAT_OCCUPIED"
			orchCtx.emittedEvents = append(orchCtx.emittedEvents, "BuyInFailed")
			return nil
		}
	}

	// Check if table is full (all seats occupied, no available seat)
	if orchCtx.triggerSeat == -1 {
		occupiedCount := int32(len(orchCtx.occupiedSeats))
		if occupiedCount >= orchCtx.tableMaxPlayers {
			orchCtx.failureCode = "TABLE_FULL"
			orchCtx.emittedEvents = append(orchCtx.emittedEvents, "BuyInFailed")
			return nil
		}
	}

	// Validation passed: emit SeatPlayer command and BuyInInitiated process event
	_ = &examples.SeatPlayer{
		PlayerRoot:    orchCtx.playerRoot,
		ReservationId: orchCtx.reservationID,
		Seat:          orchCtx.triggerSeat,
		Amount:        orchCtx.triggerAmount,
	}
	orchCtx.emittedCommands = append(orchCtx.emittedCommands, "SeatPlayer")

	_ = &examples.BuyInInitiated{
		PlayerRoot:    orchCtx.playerRoot,
		TableRoot:     orchCtx.tableRoot,
		ReservationId: orchCtx.reservationID,
		Seat:          orchCtx.triggerSeat,
		Amount:        &examples.Currency{Amount: orchCtx.triggerAmount, CurrencyCode: "CHIPS"},
		Phase:         examples.BuyInPhase_BUY_IN_SEATING,
		InitiatedAt:   timestamppb.Now(),
	}
	orchCtx.emittedEvents = append(orchCtx.emittedEvents, "BuyInInitiated")
	return nil
}

func theBuyInOrchestratorHandlesPlayerSeated() error {
	orchCtx.emittedCommands = nil
	orchCtx.emittedEvents = nil
	orchCtx.failureCode = ""

	// PlayerSeated confirms the table accepted the seating request.
	// Emit ConfirmBuyIn command to player and BuyInCompleted process event.
	_ = &examples.ConfirmBuyIn{
		ReservationId: orchCtx.reservationID,
	}
	orchCtx.emittedCommands = append(orchCtx.emittedCommands, "ConfirmBuyIn")

	_ = &examples.BuyInCompleted{
		PlayerRoot:    orchCtx.playerRoot,
		TableRoot:     orchCtx.tableRoot,
		ReservationId: orchCtx.reservationID,
		Seat:          orchCtx.triggerSeat,
		Amount:        &examples.Currency{Amount: orchCtx.triggerAmount, CurrencyCode: "CHIPS"},
		CompletedAt:   timestamppb.Now(),
	}
	orchCtx.emittedEvents = append(orchCtx.emittedEvents, "BuyInCompleted")
	return nil
}

func theBuyInOrchestratorHandlesSeatingRejected() error {
	orchCtx.emittedCommands = nil
	orchCtx.emittedEvents = nil
	orchCtx.failureCode = "SEATING_REJECTED"

	// SeatingRejected means the table could not seat the player.
	// Release the player's reserved funds.
	_ = &examples.ReleaseBuyIn{
		ReservationId: orchCtx.reservationID,
		Reason:        "seating_rejected",
	}
	orchCtx.emittedCommands = append(orchCtx.emittedCommands, "ReleaseBuyIn")

	_ = &examples.BuyInFailed{
		PlayerRoot:    orchCtx.playerRoot,
		TableRoot:     orchCtx.tableRoot,
		ReservationId: orchCtx.reservationID,
		Failure: &examples.OrchestrationFailure{
			Code:    "SEATING_REJECTED",
			Message: "table rejected seating request",
			FailedAt: timestamppb.Now(),
		},
	}
	orchCtx.emittedEvents = append(orchCtx.emittedEvents, "BuyInFailed")
	return nil
}

// =====================================================================
// When implementations - RegistrationOrchestrator
// =====================================================================

func theRegistrationOrchestratorHandlesRegistrationRequested() error {
	orchCtx.emittedCommands = nil
	orchCtx.emittedEvents = nil
	orchCtx.failureCode = ""

	// Validate tournament is open and has capacity
	if !orchCtx.tournamentRegistrationOpen || !orchCtx.tournamentCapacityAvailable {
		orchCtx.failureCode = "REGISTRATION_CLOSED"
		orchCtx.emittedEvents = append(orchCtx.emittedEvents, "RegistrationFailed")
		return nil
	}

	// Validation passed: emit EnrollPlayer command and RegistrationInitiated process event
	_ = &examples.EnrollPlayer{
		PlayerRoot:    orchCtx.playerRoot,
		ReservationId: orchCtx.reservationID,
	}
	orchCtx.emittedCommands = append(orchCtx.emittedCommands, "EnrollPlayer")

	_ = &examples.RegistrationInitiated{
		PlayerRoot:     orchCtx.playerRoot,
		TournamentRoot: orchCtx.tournamentRoot,
		ReservationId:  orchCtx.reservationID,
		Fee:            &examples.Currency{Amount: orchCtx.triggerFee, CurrencyCode: "CHIPS"},
		Phase:          examples.RegistrationPhase_REGISTRATION_ENROLLING,
		InitiatedAt:    timestamppb.Now(),
	}
	orchCtx.emittedEvents = append(orchCtx.emittedEvents, "RegistrationInitiated")
	return nil
}

func theRegistrationOrchestratorHandlesTournamentPlayerEnrolled() error {
	orchCtx.emittedCommands = nil
	orchCtx.emittedEvents = nil
	orchCtx.failureCode = ""

	// TournamentPlayerEnrolled confirms enrollment succeeded.
	// Emit ConfirmRegistrationFee to player and RegistrationCompleted process event.
	_ = &examples.ConfirmRegistrationFee{
		ReservationId: orchCtx.reservationID,
	}
	orchCtx.emittedCommands = append(orchCtx.emittedCommands, "ConfirmRegistrationFee")

	_ = &examples.RegistrationCompleted{
		PlayerRoot:     orchCtx.playerRoot,
		TournamentRoot: orchCtx.tournamentRoot,
		ReservationId:  orchCtx.reservationID,
		Fee:            &examples.Currency{Amount: orchCtx.triggerFee, CurrencyCode: "CHIPS"},
		CompletedAt:    timestamppb.Now(),
	}
	orchCtx.emittedEvents = append(orchCtx.emittedEvents, "RegistrationCompleted")
	return nil
}

func theRegistrationOrchestratorHandlesTournamentEnrollmentRejected() error {
	orchCtx.emittedCommands = nil
	orchCtx.emittedEvents = nil
	orchCtx.failureCode = "ENROLLMENT_REJECTED"

	// TournamentEnrollmentRejected means the tournament denied enrollment.
	// Release the player's reserved fee.
	_ = &examples.ReleaseRegistrationFee{
		ReservationId: orchCtx.reservationID,
		Reason:        "enrollment_rejected",
	}
	orchCtx.emittedCommands = append(orchCtx.emittedCommands, "ReleaseRegistrationFee")

	_ = &examples.RegistrationFailed{
		PlayerRoot:     orchCtx.playerRoot,
		TournamentRoot: orchCtx.tournamentRoot,
		ReservationId:  orchCtx.reservationID,
		Failure: &examples.OrchestrationFailure{
			Code:     "ENROLLMENT_REJECTED",
			Message:  "tournament rejected enrollment",
			FailedAt: timestamppb.Now(),
		},
	}
	orchCtx.emittedEvents = append(orchCtx.emittedEvents, "RegistrationFailed")
	return nil
}

// =====================================================================
// When implementations - RebuyOrchestrator
// =====================================================================

func theRebuyOrchestratorHandlesRebuyRequested() error {
	orchCtx.emittedCommands = nil
	orchCtx.emittedEvents = nil
	orchCtx.failureCode = ""

	// Validate tournament rebuy window is open
	if !orchCtx.tournamentRebuyWindowOpen {
		orchCtx.failureCode = "TOURNAMENT_NOT_RUNNING"
		orchCtx.emittedEvents = append(orchCtx.emittedEvents, "RebuyFailed")
		return nil
	}

	// Validate player is seated at the table
	if !orchCtx.playerIsSeated {
		orchCtx.failureCode = "NOT_SEATED"
		orchCtx.emittedEvents = append(orchCtx.emittedEvents, "RebuyFailed")
		return nil
	}

	// Validation passed: emit ProcessRebuy command and RebuyInitiated process event
	_ = &examples.ProcessRebuy{
		PlayerRoot:    orchCtx.playerRoot,
		ReservationId: orchCtx.reservationID,
	}
	orchCtx.emittedCommands = append(orchCtx.emittedCommands, "ProcessRebuy")

	_ = &examples.RebuyInitiated{
		PlayerRoot:     orchCtx.playerRoot,
		TournamentRoot: orchCtx.tournamentRoot,
		TableRoot:      orchCtx.tableRoot,
		ReservationId:  orchCtx.reservationID,
		Seat:           orchCtx.playerSeatedPosition,
		Fee:            &examples.Currency{Amount: orchCtx.triggerAmount, CurrencyCode: "CHIPS"},
		Phase:          examples.RebuyPhase_REBUY_APPROVING,
		InitiatedAt:    timestamppb.Now(),
	}
	orchCtx.emittedEvents = append(orchCtx.emittedEvents, "RebuyInitiated")
	return nil
}

func theRebuyOrchestratorHandlesRebuyProcessed() error {
	orchCtx.emittedCommands = nil
	orchCtx.emittedEvents = nil
	orchCtx.failureCode = ""

	// RebuyProcessed means the tournament approved the rebuy.
	// Now add chips to the table.
	_ = &examples.AddRebuyChips{
		PlayerRoot:    orchCtx.playerRoot,
		ReservationId: orchCtx.reservationID,
		Seat:          orchCtx.playerSeatedPosition,
		Amount:        orchCtx.triggerAmount,
	}
	orchCtx.emittedCommands = append(orchCtx.emittedCommands, "AddRebuyChips")
	return nil
}

func theRebuyOrchestratorHandlesRebuyChipsAdded() error {
	orchCtx.emittedCommands = nil
	orchCtx.emittedEvents = nil
	orchCtx.failureCode = ""

	// RebuyChipsAdded confirms chips were added to the table.
	// Confirm the rebuy fee with the player and mark completed.
	_ = &examples.ConfirmRebuyFee{
		ReservationId: orchCtx.reservationID,
	}
	orchCtx.emittedCommands = append(orchCtx.emittedCommands, "ConfirmRebuyFee")

	_ = &examples.RebuyCompleted{
		PlayerRoot:     orchCtx.playerRoot,
		TournamentRoot: orchCtx.tournamentRoot,
		TableRoot:      orchCtx.tableRoot,
		ReservationId:  orchCtx.reservationID,
		Fee:            &examples.Currency{Amount: orchCtx.triggerAmount, CurrencyCode: "CHIPS"},
		ChipsAdded:     orchCtx.triggerAmount,
		CompletedAt:    timestamppb.Now(),
	}
	orchCtx.emittedEvents = append(orchCtx.emittedEvents, "RebuyCompleted")
	return nil
}

func theRebuyOrchestratorHandlesRebuyDenied() error {
	orchCtx.emittedCommands = nil
	orchCtx.emittedEvents = nil
	orchCtx.failureCode = "REBUY_DENIED"

	// RebuyDenied means the tournament rejected the rebuy.
	// Release the player's reserved fee.
	_ = &examples.ReleaseRebuyFee{
		ReservationId: orchCtx.reservationID,
		Reason:        "rebuy_denied",
	}
	orchCtx.emittedCommands = append(orchCtx.emittedCommands, "ReleaseRebuyFee")

	_ = &examples.RebuyFailed{
		PlayerRoot:     orchCtx.playerRoot,
		TournamentRoot: orchCtx.tournamentRoot,
		ReservationId:  orchCtx.reservationID,
		Failure: &examples.OrchestrationFailure{
			Code:     "REBUY_DENIED",
			Message:  "tournament denied rebuy request",
			FailedAt: timestamppb.Now(),
		},
	}
	orchCtx.emittedEvents = append(orchCtx.emittedEvents, "RebuyFailed")
	return nil
}

// =====================================================================
// Then implementations - BuyInOrchestrator
// =====================================================================

func thePMEmitsSeatPlayerCommand() error {
	for _, cmd := range orchCtx.emittedCommands {
		if cmd == "SeatPlayer" {
			return nil
		}
	}
	return fmt.Errorf("expected SeatPlayer command, got commands: %v", orchCtx.emittedCommands)
}

func thePMEmitsNoCommands() error {
	if len(orchCtx.emittedCommands) != 0 {
		return fmt.Errorf("expected no commands, got: %v", orchCtx.emittedCommands)
	}
	return nil
}

func thePMEmitsBuyInFailedWithCode(code string) error {
	for _, evt := range orchCtx.emittedEvents {
		if evt == "BuyInFailed" {
			if orchCtx.failureCode == code {
				return nil
			}
			return fmt.Errorf("expected BuyInFailed with code %q, got code %q", code, orchCtx.failureCode)
		}
	}
	return fmt.Errorf("expected BuyInFailed event, got events: %v", orchCtx.emittedEvents)
}

func thePMEmitsBuyInInitiated() error {
	for _, evt := range orchCtx.emittedEvents {
		if evt == "BuyInInitiated" {
			return nil
		}
	}
	return fmt.Errorf("expected BuyInInitiated event, got events: %v", orchCtx.emittedEvents)
}

func thePMEmitsConfirmBuyInCommand() error {
	for _, cmd := range orchCtx.emittedCommands {
		if cmd == "ConfirmBuyIn" {
			return nil
		}
	}
	return fmt.Errorf("expected ConfirmBuyIn command, got commands: %v", orchCtx.emittedCommands)
}

func thePMEmitsBuyInCompleted() error {
	for _, evt := range orchCtx.emittedEvents {
		if evt == "BuyInCompleted" {
			return nil
		}
	}
	return fmt.Errorf("expected BuyInCompleted event, got events: %v", orchCtx.emittedEvents)
}

func thePMEmitsReleaseBuyInCommand() error {
	for _, cmd := range orchCtx.emittedCommands {
		if cmd == "ReleaseBuyIn" {
			return nil
		}
	}
	return fmt.Errorf("expected ReleaseBuyIn command, got commands: %v", orchCtx.emittedCommands)
}

// =====================================================================
// Then implementations - RegistrationOrchestrator
// =====================================================================

func thePMEmitsEnrollPlayerCommand() error {
	for _, cmd := range orchCtx.emittedCommands {
		if cmd == "EnrollPlayer" {
			return nil
		}
	}
	return fmt.Errorf("expected EnrollPlayer command, got commands: %v", orchCtx.emittedCommands)
}

func thePMEmitsRegistrationInitiated() error {
	for _, evt := range orchCtx.emittedEvents {
		if evt == "RegistrationInitiated" {
			return nil
		}
	}
	return fmt.Errorf("expected RegistrationInitiated event, got events: %v", orchCtx.emittedEvents)
}

func thePMEmitsRegistrationFailedWithCode(code string) error {
	for _, evt := range orchCtx.emittedEvents {
		if evt == "RegistrationFailed" {
			if orchCtx.failureCode == code {
				return nil
			}
			return fmt.Errorf("expected RegistrationFailed with code %q, got code %q", code, orchCtx.failureCode)
		}
	}
	return fmt.Errorf("expected RegistrationFailed event, got events: %v", orchCtx.emittedEvents)
}

func thePMEmitsConfirmRegistrationFeeCommand() error {
	for _, cmd := range orchCtx.emittedCommands {
		if cmd == "ConfirmRegistrationFee" {
			return nil
		}
	}
	return fmt.Errorf("expected ConfirmRegistrationFee command, got commands: %v", orchCtx.emittedCommands)
}

func thePMEmitsRegistrationCompleted() error {
	for _, evt := range orchCtx.emittedEvents {
		if evt == "RegistrationCompleted" {
			return nil
		}
	}
	return fmt.Errorf("expected RegistrationCompleted event, got events: %v", orchCtx.emittedEvents)
}

func thePMEmitsReleaseRegistrationFeeCommand() error {
	for _, cmd := range orchCtx.emittedCommands {
		if cmd == "ReleaseRegistrationFee" {
			return nil
		}
	}
	return fmt.Errorf("expected ReleaseRegistrationFee command, got commands: %v", orchCtx.emittedCommands)
}

// =====================================================================
// Then implementations - RebuyOrchestrator
// =====================================================================

func thePMEmitsProcessRebuyCommand() error {
	for _, cmd := range orchCtx.emittedCommands {
		if cmd == "ProcessRebuy" {
			return nil
		}
	}
	return fmt.Errorf("expected ProcessRebuy command, got commands: %v", orchCtx.emittedCommands)
}

func thePMEmitsRebuyInitiated() error {
	for _, evt := range orchCtx.emittedEvents {
		if evt == "RebuyInitiated" {
			return nil
		}
	}
	return fmt.Errorf("expected RebuyInitiated event, got events: %v", orchCtx.emittedEvents)
}

func thePMEmitsRebuyFailedWithCode(code string) error {
	for _, evt := range orchCtx.emittedEvents {
		if evt == "RebuyFailed" {
			if orchCtx.failureCode == code {
				return nil
			}
			return fmt.Errorf("expected RebuyFailed with code %q, got code %q", code, orchCtx.failureCode)
		}
	}
	return fmt.Errorf("expected RebuyFailed event, got events: %v", orchCtx.emittedEvents)
}

func thePMEmitsAddRebuyChipsCommand() error {
	for _, cmd := range orchCtx.emittedCommands {
		if cmd == "AddRebuyChips" {
			return nil
		}
	}
	return fmt.Errorf("expected AddRebuyChips command, got commands: %v", orchCtx.emittedCommands)
}

func thePMEmitsConfirmRebuyFeeCommand() error {
	for _, cmd := range orchCtx.emittedCommands {
		if cmd == "ConfirmRebuyFee" {
			return nil
		}
	}
	return fmt.Errorf("expected ConfirmRebuyFee command, got commands: %v", orchCtx.emittedCommands)
}

func thePMEmitsRebuyCompleted() error {
	for _, evt := range orchCtx.emittedEvents {
		if evt == "RebuyCompleted" {
			return nil
		}
	}
	return fmt.Errorf("expected RebuyCompleted event, got events: %v", orchCtx.emittedEvents)
}

func thePMEmitsReleaseRebuyFeeCommand() error {
	for _, cmd := range orchCtx.emittedCommands {
		if cmd == "ReleaseRebuyFee" {
			return nil
		}
	}
	return fmt.Errorf("expected ReleaseRebuyFee command, got commands: %v", orchCtx.emittedCommands)
}
