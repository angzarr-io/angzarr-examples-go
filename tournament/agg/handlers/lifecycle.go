package handlers

import (
	"encoding/hex"
	"strconv"
	"time"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr/v1"
	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// HandleAdvanceBlindLevel handles the AdvanceBlindLevel command.
//
// Rejection BLIND_STRUCTURE_EXHAUSTED carries the current level and the
// max defined level so operators can extend the structure or end the
// tournament deliberately (EU-0855, EU-0856).
func HandleAdvanceBlindLevel(
	commandBook *pb.CommandBook,
	_ *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	if !state.IsRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament is not running")
	}

	maxLevel := state.MaxBlindLevel()
	// Reject when no more defined levels remain. Per EU-0855 the current
	// level matches max defined level; per EU-0856 the structure is empty
	// (max == 0).
	if state.CurrentLevel >= maxLevel {
		return nil, angzarr.NewPreconditionFailedRejection(
			"BLIND_STRUCTURE_EXHAUSTED",
			"Blind structure exhausted",
			map[string]string{
				"current":   strconv.FormatInt(int64(state.CurrentLevel), 10),
				"max_value": strconv.FormatInt(int64(maxLevel), 10),
			},
		)
	}

	nextLevel := state.CurrentLevel + 1
	idx := int(nextLevel) - 1
	level := state.BlindStructure[idx]

	event := &examples.BlindLevelAdvanced{
		Level:      nextLevel,
		SmallBlind: level.SmallBlind,
		BigBlind:   level.BigBlind,
		Ante:       level.Ante,
		AdvancedAt: timestamppb.New(time.Now()),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return nil, err
	}
	return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
}

// HandleEliminatePlayer handles the EliminatePlayer command.
func HandleEliminatePlayer(
	commandBook *pb.CommandBook,
	commandAny *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	var cmd examples.EliminatePlayer
	if err := proto.Unmarshal(commandAny.Value, &cmd); err != nil {
		return nil, err
	}

	if !state.IsRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament is not running")
	}

	playerRootHex := hex.EncodeToString(cmd.PlayerRoot)
	if !state.IsPlayerRegistered(playerRootHex) {
		return nil, angzarr.NewCommandRejectedError("Player is not registered in this tournament")
	}

	finishPosition := state.PlayersRemaining

	event := &examples.PlayerEliminated{
		PlayerRoot:     cmd.PlayerRoot,
		FinishPosition: finishPosition,
		HandRoot:       cmd.HandRoot,
		Payout:         0,
		EliminatedAt:   timestamppb.New(time.Now()),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return nil, err
	}
	return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
}

// HandlePauseTournament handles the PauseTournament command.
func HandlePauseTournament(
	commandBook *pb.CommandBook,
	commandAny *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	var cmd examples.PauseTournament
	if err := proto.Unmarshal(commandAny.Value, &cmd); err != nil {
		return nil, err
	}

	if state.IsPaused() {
		return nil, angzarr.NewCommandRejectedError("Tournament is already paused")
	}
	if !state.IsRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament is not running")
	}

	event := &examples.TournamentPaused{
		Reason:   cmd.Reason,
		PausedAt: timestamppb.New(time.Now()),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return nil, err
	}
	return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
}

// HandleResumeTournament handles the ResumeTournament command.
func HandleResumeTournament(
	commandBook *pb.CommandBook,
	_ *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	if !state.IsPaused() {
		return nil, angzarr.NewCommandRejectedError("Tournament is not paused")
	}

	event := &examples.TournamentResumed{
		ResumedAt: timestamppb.New(time.Now()),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return nil, err
	}
	return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
}

// HandleStartTournament handles the StartTournament command.
// Requires registration open + at least MinPlayers enrolled (EU-0822 /
// EU-0823 / WSOP §IX).
func HandleStartTournament(
	commandBook *pb.CommandBook,
	_ *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Tournament does not exist")
	}
	if state.IsRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament is already running")
	}
	if int32(len(state.RegisteredPlayers)) < state.MinPlayers {
		return nil, angzarr.NewCommandRejectedError("Not enough players to start tournament")
	}

	event := &examples.TournamentStarted{
		TotalPlayers:   state.PlayersRemaining,
		TablesCreated:  0,
		TotalPrizePool: state.TotalPrizePool,
		StartedAt:      timestamppb.New(time.Now()),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return nil, err
	}
	return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
}
