package handlers

import (
	"encoding/hex"
	"time"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr/v1"
	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// HandleOpenRegistration handles the OpenRegistration command.
func HandleOpenRegistration(
	commandBook *pb.CommandBook,
	_ *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Tournament does not exist")
	}
	if state.IsRunning() {
		return nil, angzarr.NewCommandRejectedError("Cannot open registration on a running tournament")
	}
	if state.IsRegistrationOpen() {
		return nil, angzarr.NewCommandRejectedError("Registration is already open")
	}

	event := &examples.RegistrationOpened{
		OpenedAt: timestamppb.New(time.Now()),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return nil, err
	}
	return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
}

// HandleCloseRegistration handles the CloseRegistration command.
func HandleCloseRegistration(
	commandBook *pb.CommandBook,
	_ *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Tournament does not exist")
	}
	if !state.IsRegistrationOpen() {
		return nil, angzarr.NewCommandRejectedError("Registration is not open")
	}

	event := &examples.RegistrationClosed{
		TotalRegistrations: int32(len(state.RegisteredPlayers)),
		ClosedAt:           timestamppb.New(time.Now()),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return nil, err
	}
	return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
}

// HandleEnrollPlayer handles the EnrollPlayer command (sent by Registration PM).
//
// Mirrors `examples-python/main/tournament/agg/handlers.py::handle_enroll_player`:
//   - Tournament-not-exists → raises (command-level error).
//   - Empty player_root, closed registration, full, duplicate →
//     TournamentEnrollmentRejected (event-shape; PM compensates).
func HandleEnrollPlayer(
	commandBook *pb.CommandBook,
	commandAny *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	var cmd examples.EnrollPlayer
	if err := proto.Unmarshal(commandAny.Value, &cmd); err != nil {
		return nil, err
	}

	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Tournament does not exist")
	}

	emitRejected := func(reason string) (*pb.EventBook, error) {
		event := &examples.TournamentEnrollmentRejected{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        reason,
			RejectedAt:    timestamppb.New(time.Now()),
		}
		eventAny, err := anypb.New(event)
		if err != nil {
			return nil, err
		}
		return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
	}

	if len(cmd.PlayerRoot) == 0 {
		return emitRejected("player_root is required")
	}
	if !state.IsRegistrationOpen() {
		return emitRejected("Registration is not open")
	}
	if state.IsFull() {
		return emitRejected("Tournament is full")
	}
	playerRootHex := hex.EncodeToString(cmd.PlayerRoot)
	if state.IsPlayerRegistered(playerRootHex) {
		return emitRejected("Player is already registered")
	}

	event := &examples.TournamentPlayerEnrolled{
		PlayerRoot:         cmd.PlayerRoot,
		ReservationId:      cmd.ReservationId,
		FeePaid:            state.BuyIn,
		StartingStack:      state.StartingStack,
		RegistrationNumber: int32(len(state.RegisteredPlayers)) + 1,
		EnrolledAt:         timestamppb.New(time.Now()),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return nil, err
	}
	return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
}
