package handlers

import (
	"strconv"
	"time"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr/v1"
	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
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

// validateCreateTournament mirrors Py `Tournament.handle_create_tournament`
// validation order (handlers.py:387-400). Order matters because cucumber
// asserts specific error messages (EU-0802..EU-0806).
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
	if cmd.MinPlayers < 2 {
		return angzarr.NewInvalidArgumentError("min_players must be at least 2")
	}
	if cmd.MinPlayers > cmd.MaxPlayers {
		return angzarr.NewPreconditionFailedRejection(
			"MIN_PLAYERS_EXCEEDS_MAX",
			"min_players cannot exceed max_players",
			map[string]string{
				"lhs": strconv.FormatInt(int64(cmd.MinPlayers), 10),
				"rhs": strconv.FormatInt(int64(cmd.MaxPlayers), 10),
			},
		)
	}
	return nil
}

func computeTournamentCreated(cmd *examples.CreateTournament) *examples.TournamentCreated {
	return &examples.TournamentCreated{
		Name:                    cmd.Name,
		GameVariant:             cmd.GameVariant,
		BuyIn:                   cmd.BuyIn,
		StartingStack:           cmd.StartingStack,
		MaxPlayers:              cmd.MaxPlayers,
		MinPlayers:              cmd.MinPlayers,
		ScheduledStart:          cmd.ScheduledStart,
		RebuyConfig:             cmd.RebuyConfig,
		AddonConfig:             cmd.AddonConfig,
		BlindStructure:          cmd.BlindStructure,
		CreatedAt:               timestamppb.New(time.Now()),
		RegistrationCutoffLevel: cmd.RegistrationCutoffLevel,
		PayoutStructure:         cmd.PayoutStructure,
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
