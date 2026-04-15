package handlers

import (
	"encoding/hex"
	"time"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// HandleAdvanceBlindLevel handles the AdvanceBlindLevel command.
func HandleAdvanceBlindLevel(
	commandBook *pb.CommandBook,
	_ *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	if !state.IsRunning() {
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

	event := &examples.BlindLevelAdvanced{
		Level:      nextLevel,
		SmallBlind: smallBlind,
		BigBlind:   bigBlind,
		Ante:       ante,
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
		return nil, angzarr.NewCommandRejectedError("Tournament not running")
	}

	playerRootHex := hex.EncodeToString(cmd.PlayerRoot)
	if !state.IsPlayerRegistered(playerRootHex) {
		return nil, angzarr.NewCommandRejectedError("Player not registered")
	}

	finishPosition := state.PlayersRemaining

	event := &examples.PlayerEliminated{
		PlayerRoot:     cmd.PlayerRoot,
		FinishPosition: finishPosition,
		HandRoot:       cmd.HandRoot,
		Payout:         0, // Payout calculation would be domain-specific
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

	if !state.IsRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament not running")
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
	if state.Status != examples.TournamentStatus_TOURNAMENT_PAUSED {
		return nil, angzarr.NewCommandRejectedError("Tournament not paused")
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
