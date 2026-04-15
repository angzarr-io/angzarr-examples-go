package handlers

import (
	"time"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func guardCreateTournament(state TournamentState) error {
	if state.Exists() {
		return angzarr.NewCommandRejectedError("Tournament already exists")
	}
	return nil
}

func validateCreateTournament(cmd *examples.CreateTournament) error {
	if cmd.Name == "" {
		return angzarr.NewInvalidArgumentError("name is required")
	}
	if cmd.BuyIn <= 0 {
		return angzarr.NewInvalidArgumentError("buy_in must be positive")
	}
	if cmd.StartingStack <= 0 {
		return angzarr.NewInvalidArgumentError("starting_stack must be positive")
	}
	if cmd.MaxPlayers < 2 {
		return angzarr.NewInvalidArgumentError("max_players must be at least 2")
	}
	if cmd.MinPlayers > cmd.MaxPlayers {
		return angzarr.NewInvalidArgumentError("min_players must be <= max_players")
	}
	return nil
}

func computeTournamentCreated(cmd *examples.CreateTournament) *examples.TournamentCreated {
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
	}
}

// HandleCreateTournament handles the CreateTournament command.
func HandleCreateTournament(
	commandBook *pb.CommandBook,
	commandAny *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	var cmd examples.CreateTournament
	if err := proto.Unmarshal(commandAny.Value, &cmd); err != nil {
		return nil, err
	}

	if err := guardCreateTournament(state); err != nil {
		return nil, err
	}
	if err := validateCreateTournament(&cmd); err != nil {
		return nil, err
	}

	event := computeTournamentCreated(&cmd)
	eventAny, err := anypb.New(event)
	if err != nil {
		return nil, err
	}

	return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
}
