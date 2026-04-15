// Package handlers implements tournament aggregate command handlers.
//
// The tournament aggregate manages tournament lifecycle, player registrations,
// blind level progression, and player eliminations. Handlers follow the
// guard/validate/compute pattern for testability.
package handlers

import (
	"encoding/hex"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
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

// NewTournamentState creates a new empty tournament state.
func NewTournamentState() TournamentState {
	return TournamentState{
		RegisteredPlayers: make(map[string]*examples.PlayerRegistration),
	}
}

// Exists returns true if the tournament has been created.
func (s TournamentState) Exists() bool {
	return s.Name != ""
}

// IsRegistrationOpen returns true if registration is currently open.
func (s TournamentState) IsRegistrationOpen() bool {
	return s.Status == examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN
}

// IsRunning returns true if tournament is in progress.
func (s TournamentState) IsRunning() bool {
	return s.Status == examples.TournamentStatus_TOURNAMENT_RUNNING
}

// IsFull returns true if max player capacity is reached.
func (s TournamentState) IsFull() bool {
	return int32(len(s.RegisteredPlayers)) >= s.MaxPlayers
}

// IsPlayerRegistered checks if a player is already enrolled.
func (s TournamentState) IsPlayerRegistered(playerRootHex string) bool {
	_, exists := s.RegisteredPlayers[playerRootHex]
	return exists
}

// PlayerRebuyCount returns the number of rebuys a player has used.
func (s TournamentState) PlayerRebuyCount(playerRootHex string) int32 {
	reg, exists := s.RegisteredPlayers[playerRootHex]
	if !exists {
		return 0
	}
	return reg.RebuysUsed
}

// Event applier functions

func applyCreated(state *TournamentState, event *examples.TournamentCreated) {
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

func applyRegistrationOpened(state *TournamentState, _ *examples.RegistrationOpened) {
	state.Status = examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN
}

func applyRegistrationClosed(state *TournamentState, _ *examples.RegistrationClosed) {
	// Status transitions to CREATED (awaiting StartTournament) — or directly handled by start
	// For simplicity, keep as CREATED since StartTournament moves to RUNNING
	state.Status = examples.TournamentStatus_TOURNAMENT_CREATED
}

func applyPlayerEnrolled(state *TournamentState, event *examples.TournamentPlayerEnrolled) {
	rootHex := hex.EncodeToString(event.PlayerRoot)
	state.RegisteredPlayers[rootHex] = &examples.PlayerRegistration{
		PlayerRoot:    event.PlayerRoot,
		FeePaid:       event.FeePaid,
		StartingStack: event.StartingStack,
	}
	state.PlayersRemaining++
	state.TotalPrizePool += event.FeePaid
}

func applyEnrollmentRejected(_ *TournamentState, _ *examples.TournamentEnrollmentRejected) {
	// No state change on rejection
}

func applyTournamentStarted(state *TournamentState, _ *examples.TournamentStarted) {
	state.Status = examples.TournamentStatus_TOURNAMENT_RUNNING
}

func applyRebuyProcessed(state *TournamentState, event *examples.RebuyProcessed) {
	rootHex := hex.EncodeToString(event.PlayerRoot)
	if reg, exists := state.RegisteredPlayers[rootHex]; exists {
		reg.RebuysUsed = event.RebuyCount
	}
	state.TotalPrizePool += event.RebuyCost
}

func applyRebuyDenied(_ *TournamentState, _ *examples.RebuyDenied) {
	// No state change on denial
}

func applyBlindAdvanced(state *TournamentState, event *examples.BlindLevelAdvanced) {
	state.CurrentLevel = event.Level
}

func applyPlayerEliminated(state *TournamentState, event *examples.PlayerEliminated) {
	rootHex := hex.EncodeToString(event.PlayerRoot)
	delete(state.RegisteredPlayers, rootHex)
	state.PlayersRemaining--
}

func applyPaused(state *TournamentState, _ *examples.TournamentPaused) {
	state.Status = examples.TournamentStatus_TOURNAMENT_PAUSED
}

func applyResumed(state *TournamentState, _ *examples.TournamentResumed) {
	state.Status = examples.TournamentStatus_TOURNAMENT_RUNNING
}

func applyCompleted(state *TournamentState, _ *examples.TournamentCompleted) {
	state.Status = examples.TournamentStatus_TOURNAMENT_COMPLETED
}

// stateRouter is the fluent state reconstruction router.
var stateRouter = angzarr.NewStateRouter(NewTournamentState).
	On(applyCreated).
	On(applyRegistrationOpened).
	On(applyRegistrationClosed).
	On(applyPlayerEnrolled).
	On(applyEnrollmentRejected).
	On(applyTournamentStarted).
	On(applyRebuyProcessed).
	On(applyRebuyDenied).
	On(applyBlindAdvanced).
	On(applyPlayerEliminated).
	On(applyPaused).
	On(applyResumed).
	On(applyCompleted)

// RebuildState rebuilds tournament state from event history.
func RebuildState(eventBook *pb.EventBook) TournamentState {
	if eventBook == nil {
		return NewTournamentState()
	}

	state := NewTournamentState()
	for _, page := range eventBook.Pages {
		event := page.GetEvent()
		if event != nil {
			stateRouter.ApplySingle(&state, event)
		}
	}
	return state
}
