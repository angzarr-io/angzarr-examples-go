// Tournament command handler - rich domain model using OO pattern.
//
// This command handler uses the OO-style pattern with embedded CommandHandlerBase,
// method-based handlers, and fluent registration. Manages tournament lifecycle:
// creation, registration, blind levels, rebuys, player elimination, and pause/resume.
package main

import (
	"encoding/hex"
	"time"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TournamentState represents the current state of a tournament aggregate.
type TournamentState struct {
	Name              string
	GameVariant       examples.GameVariant
	Status            examples.TournamentStatus
	BuyIn             int64
	StartingStack     int64
	MaxPlayers        int32
	MinPlayers        int32
	RebuyConfig       *examples.RebuyConfig
	BlindStructure    []*examples.BlindLevel
	CurrentLevel      int32
	RegisteredPlayers map[string]*examples.PlayerRegistration // player_root_hex -> registration
	PlayersRemaining  int32
	TotalPrizePool    int64
}

// Tournament command handler with event sourcing using OO pattern.
type Tournament struct {
	angzarr.CommandHandlerBase[TournamentState]
}

// NewTournament creates a new Tournament command handler with prior events for state reconstruction.
func NewTournament(eventBook *pb.EventBook) *Tournament {
	t := &Tournament{}
	t.Init(eventBook, func() TournamentState {
		return TournamentState{
			RegisteredPlayers: make(map[string]*examples.PlayerRegistration),
		}
	})
	t.SetDomain("tournament")

	// Register event appliers
	t.Applies(t.applyCreated)
	t.Applies(t.applyRegistrationOpened)
	t.Applies(t.applyRegistrationClosed)
	t.Applies(t.applyPlayerEnrolled)
	t.Applies(t.applyEnrollmentRejected)
	t.Applies(t.applyTournamentStarted)
	t.Applies(t.applyRebuyProcessed)
	t.Applies(t.applyRebuyDenied)
	t.Applies(t.applyBlindAdvanced)
	t.Applies(t.applyPlayerEliminated)
	t.Applies(t.applyPaused)
	t.Applies(t.applyResumed)
	t.Applies(t.applyCompleted)

	// Register command handlers
	t.Handles(t.HandleCreateTournament)
	t.Handles(t.HandleOpenRegistration)
	t.Handles(t.HandleCloseRegistration)
	t.Handles(t.HandleEnrollPlayer)
	t.Handles(t.HandleProcessRebuy)
	t.Handles(t.HandleAdvanceBlindLevel)
	t.Handles(t.HandleEliminatePlayer)
	t.Handles(t.HandlePauseTournament)
	t.Handles(t.HandleResumeTournament)

	return t
}

// --- Event Appliers ---

func (t *Tournament) applyCreated(state *TournamentState, event *examples.TournamentCreated) {
	state.Name = event.Name
	state.GameVariant = event.GameVariant
	state.Status = examples.TournamentStatus_TOURNAMENT_CREATED
	state.BuyIn = event.BuyIn
	state.StartingStack = event.StartingStack
	state.MaxPlayers = event.MaxPlayers
	state.MinPlayers = event.MinPlayers
	state.RebuyConfig = event.RebuyConfig
	state.BlindStructure = event.BlindStructure
	state.CurrentLevel = 0
	state.PlayersRemaining = 0
	state.TotalPrizePool = 0
}

func (t *Tournament) applyRegistrationOpened(state *TournamentState, _ *examples.RegistrationOpened) {
	state.Status = examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN
}

func (t *Tournament) applyRegistrationClosed(state *TournamentState, _ *examples.RegistrationClosed) {
	state.Status = examples.TournamentStatus_TOURNAMENT_CREATED
}

func (t *Tournament) applyPlayerEnrolled(state *TournamentState, event *examples.TournamentPlayerEnrolled) {
	rootHex := hex.EncodeToString(event.PlayerRoot)
	state.RegisteredPlayers[rootHex] = &examples.PlayerRegistration{
		PlayerRoot:    event.PlayerRoot,
		FeePaid:       event.FeePaid,
		StartingStack: event.StartingStack,
	}
	state.PlayersRemaining++
	state.TotalPrizePool += event.FeePaid
}

func (t *Tournament) applyEnrollmentRejected(_ *TournamentState, _ *examples.TournamentEnrollmentRejected) {
	// No state change on rejection
}

func (t *Tournament) applyTournamentStarted(state *TournamentState, _ *examples.TournamentStarted) {
	state.Status = examples.TournamentStatus_TOURNAMENT_RUNNING
}

func (t *Tournament) applyRebuyProcessed(state *TournamentState, event *examples.RebuyProcessed) {
	rootHex := hex.EncodeToString(event.PlayerRoot)
	if reg, exists := state.RegisteredPlayers[rootHex]; exists {
		reg.RebuysUsed = event.RebuyCount
	}
	state.TotalPrizePool += event.RebuyCost
}

func (t *Tournament) applyRebuyDenied(_ *TournamentState, _ *examples.RebuyDenied) {
	// No state change on denial
}

func (t *Tournament) applyBlindAdvanced(state *TournamentState, event *examples.BlindLevelAdvanced) {
	state.CurrentLevel = event.Level
}

func (t *Tournament) applyPlayerEliminated(state *TournamentState, event *examples.PlayerEliminated) {
	rootHex := hex.EncodeToString(event.PlayerRoot)
	delete(state.RegisteredPlayers, rootHex)
	state.PlayersRemaining--
}

func (t *Tournament) applyPaused(state *TournamentState, _ *examples.TournamentPaused) {
	state.Status = examples.TournamentStatus_TOURNAMENT_PAUSED
}

func (t *Tournament) applyResumed(state *TournamentState, _ *examples.TournamentResumed) {
	state.Status = examples.TournamentStatus_TOURNAMENT_RUNNING
}

func (t *Tournament) applyCompleted(state *TournamentState, _ *examples.TournamentCompleted) {
	state.Status = examples.TournamentStatus_TOURNAMENT_COMPLETED
}

// --- State Accessors ---

func (t *Tournament) exists() bool {
	return t.State().Name != ""
}

func (t *Tournament) isRegistrationOpen() bool {
	return t.State().Status == examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN
}

func (t *Tournament) isRunning() bool {
	return t.State().Status == examples.TournamentStatus_TOURNAMENT_RUNNING
}

func (t *Tournament) isFull() bool {
	return int32(len(t.State().RegisteredPlayers)) >= t.State().MaxPlayers
}

func (t *Tournament) isPlayerRegistered(playerRootHex string) bool {
	_, exists := t.State().RegisteredPlayers[playerRootHex]
	return exists
}

func (t *Tournament) playerRebuyCount(playerRootHex string) int32 {
	reg, exists := t.State().RegisteredPlayers[playerRootHex]
	if !exists {
		return 0
	}
	return reg.RebuysUsed
}

// --- Command Handlers ---

// HandleCreateTournament handles the CreateTournament command.
func (t *Tournament) HandleCreateTournament(cmd *examples.CreateTournament) (*examples.TournamentCreated, error) {
	// Guard
	if t.exists() {
		return nil, angzarr.NewCommandRejectedError("Tournament already exists")
	}

	// Validate
	if cmd.Name == "" {
		return nil, angzarr.NewInvalidArgumentError("name is required")
	}
	if cmd.BuyIn <= 0 {
		return nil, angzarr.NewInvalidArgumentError("buy_in must be positive")
	}
	if cmd.StartingStack <= 0 {
		return nil, angzarr.NewInvalidArgumentError("starting_stack must be positive")
	}
	if cmd.MaxPlayers < 2 {
		return nil, angzarr.NewInvalidArgumentError("max_players must be at least 2")
	}
	if cmd.MinPlayers > cmd.MaxPlayers {
		return nil, angzarr.NewInvalidArgumentError("min_players must be <= max_players")
	}

	// Compute
	return &examples.TournamentCreated{
		Name:           cmd.Name,
		GameVariant:    cmd.GameVariant,
		BuyIn:          cmd.BuyIn,
		StartingStack:  cmd.StartingStack,
		MaxPlayers:     cmd.MaxPlayers,
		MinPlayers:     cmd.MinPlayers,
		ScheduledStart: cmd.ScheduledStart,
		RebuyConfig:    cmd.RebuyConfig,
		AddonConfig:    cmd.AddonConfig,
		BlindStructure: cmd.BlindStructure,
		CreatedAt:      timestamppb.New(time.Now()),
	}, nil
}

// HandleOpenRegistration handles the OpenRegistration command.
func (t *Tournament) HandleOpenRegistration(cmd *examples.OpenRegistration) (*examples.RegistrationOpened, error) {
	if !t.exists() {
		return nil, angzarr.NewCommandRejectedError("Tournament does not exist")
	}
	if t.isRegistrationOpen() {
		return nil, angzarr.NewCommandRejectedError("Registration already open")
	}
	if t.isRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament is running")
	}

	return &examples.RegistrationOpened{
		OpenedAt: timestamppb.New(time.Now()),
	}, nil
}

// HandleCloseRegistration handles the CloseRegistration command.
func (t *Tournament) HandleCloseRegistration(cmd *examples.CloseRegistration) (*examples.RegistrationClosed, error) {
	if !t.isRegistrationOpen() {
		return nil, angzarr.NewCommandRejectedError("Registration not open")
	}

	return &examples.RegistrationClosed{
		TotalRegistrations: int32(len(t.State().RegisteredPlayers)),
		ClosedAt:           timestamppb.New(time.Now()),
	}, nil
}

// HandleEnrollPlayer handles the EnrollPlayer command (sent by Registration PM).
// Returns TournamentPlayerEnrolled on success, TournamentEnrollmentRejected on failure.
// Note: enrollment rejections are events (not errors) because the PM needs to
// react to them for compensation. Returns proto.Message to allow alternate event types.
func (t *Tournament) HandleEnrollPlayer(cmd *examples.EnrollPlayer) (proto.Message, error) {
	playerRootHex := hex.EncodeToString(cmd.PlayerRoot)

	// Rejection cases produce events, not errors
	if !t.isRegistrationOpen() {
		return &examples.TournamentEnrollmentRejected{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "closed",
			RejectedAt:    timestamppb.New(time.Now()),
		}, nil
	}

	if t.isFull() {
		return &examples.TournamentEnrollmentRejected{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "full",
			RejectedAt:    timestamppb.New(time.Now()),
		}, nil
	}

	if t.isPlayerRegistered(playerRootHex) {
		return &examples.TournamentEnrollmentRejected{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "already_registered",
			RejectedAt:    timestamppb.New(time.Now()),
		}, nil
	}

	// Success
	return &examples.TournamentPlayerEnrolled{
		PlayerRoot:         cmd.PlayerRoot,
		ReservationId:      cmd.ReservationId,
		FeePaid:            t.State().BuyIn,
		StartingStack:      t.State().StartingStack,
		RegistrationNumber: int32(len(t.State().RegisteredPlayers)) + 1,
		EnrolledAt:         timestamppb.New(time.Now()),
	}, nil
}

// HandleProcessRebuy handles the ProcessRebuy command (sent by Rebuy PM).
// Returns RebuyProcessed on success, RebuyDenied on failure.
// Returns proto.Message to allow alternate event types.
func (t *Tournament) HandleProcessRebuy(cmd *examples.ProcessRebuy) (proto.Message, error) {
	state := t.State()

	if !t.exists() {
		return nil, angzarr.NewCommandRejectedError("Tournament does not exist")
	}
	if !t.isRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament not running")
	}

	playerRootHex := hex.EncodeToString(cmd.PlayerRoot)
	if !t.isPlayerRegistered(playerRootHex) {
		return nil, angzarr.NewCommandRejectedError("Player not registered")
	}

	// Check rebuy eligibility
	if state.RebuyConfig == nil || !state.RebuyConfig.Enabled {
		return &examples.RebuyDenied{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "rebuys_disabled",
			DeniedAt:      timestamppb.New(time.Now()),
		}, nil
	}

	// Check rebuy window (current level must be <= cutoff)
	if state.CurrentLevel > state.RebuyConfig.RebuyLevelCutoff {
		return &examples.RebuyDenied{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "window_closed",
			DeniedAt:      timestamppb.New(time.Now()),
		}, nil
	}

	// Check max rebuys
	rebuysUsed := t.playerRebuyCount(playerRootHex)
	maxRebuys := state.RebuyConfig.MaxRebuys
	if maxRebuys > 0 && rebuysUsed >= maxRebuys {
		return &examples.RebuyDenied{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "max_reached",
			DeniedAt:      timestamppb.New(time.Now()),
		}, nil
	}

	// Success
	return &examples.RebuyProcessed{
		PlayerRoot:    cmd.PlayerRoot,
		ReservationId: cmd.ReservationId,
		RebuyCost:     state.RebuyConfig.RebuyCost,
		ChipsAdded:    state.RebuyConfig.RebuyChips,
		RebuyCount:    rebuysUsed + 1,
		ProcessedAt:   timestamppb.New(time.Now()),
	}, nil
}

// HandleAdvanceBlindLevel handles the AdvanceBlindLevel command.
func (t *Tournament) HandleAdvanceBlindLevel(cmd *examples.AdvanceBlindLevel) (*examples.BlindLevelAdvanced, error) {
	state := t.State()

	if !t.isRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament not running")
	}

	nextLevel := state.CurrentLevel + 1
	var smallBlind, bigBlind, ante int64

	// Look up blind values from structure, use last level if exceeded
	if len(state.BlindStructure) > 0 {
		idx := int(nextLevel) - 1
		if idx >= len(state.BlindStructure) {
			idx = len(state.BlindStructure) - 1
		}
		if idx >= 0 {
			level := state.BlindStructure[idx]
			smallBlind = level.SmallBlind
			bigBlind = level.BigBlind
			ante = level.Ante
		}
	}

	return &examples.BlindLevelAdvanced{
		Level:      nextLevel,
		SmallBlind: smallBlind,
		BigBlind:   bigBlind,
		Ante:       ante,
		AdvancedAt: timestamppb.New(time.Now()),
	}, nil
}

// HandleEliminatePlayer handles the EliminatePlayer command.
func (t *Tournament) HandleEliminatePlayer(cmd *examples.EliminatePlayer) (*examples.PlayerEliminated, error) {
	state := t.State()

	if !t.isRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament not running")
	}

	playerRootHex := hex.EncodeToString(cmd.PlayerRoot)
	if !t.isPlayerRegistered(playerRootHex) {
		return nil, angzarr.NewCommandRejectedError("Player not registered")
	}

	finishPosition := state.PlayersRemaining

	return &examples.PlayerEliminated{
		PlayerRoot:     cmd.PlayerRoot,
		FinishPosition: finishPosition,
		HandRoot:       cmd.HandRoot,
		Payout:         0, // Payout calculation would be domain-specific
		EliminatedAt:   timestamppb.New(time.Now()),
	}, nil
}

// HandlePauseTournament handles the PauseTournament command.
func (t *Tournament) HandlePauseTournament(cmd *examples.PauseTournament) (*examples.TournamentPaused, error) {
	if !t.isRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament not running")
	}

	return &examples.TournamentPaused{
		Reason:   cmd.Reason,
		PausedAt: timestamppb.New(time.Now()),
	}, nil
}

// HandleResumeTournament handles the ResumeTournament command.
func (t *Tournament) HandleResumeTournament(cmd *examples.ResumeTournament) (*examples.TournamentResumed, error) {
	if t.State().Status != examples.TournamentStatus_TOURNAMENT_PAUSED {
		return nil, angzarr.NewCommandRejectedError("Tournament not paused")
	}

	return &examples.TournamentResumed{
		ResumedAt: timestamppb.New(time.Now()),
	}, nil
}
