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

// HandleOpenRegistration handles the OpenRegistration command.
func HandleOpenRegistration(
	commandBook *pb.CommandBook,
	commandAny *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Tournament does not exist")
	}
	if state.IsRegistrationOpen() {
		return nil, angzarr.NewCommandRejectedError("Registration already open")
	}
	if state.IsRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament is running")
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
	commandAny *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	if !state.IsRegistrationOpen() {
		return nil, angzarr.NewCommandRejectedError("Registration not open")
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
// Returns TournamentPlayerEnrolled on success, TournamentEnrollmentRejected on failure.
// Note: enrollment rejections are events (not errors) because the PM needs to
// react to them for compensation.
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

	playerRootHex := hex.EncodeToString(cmd.PlayerRoot)

	// Rejection cases produce events, not errors
	if !state.IsRegistrationOpen() {
		event := &examples.TournamentEnrollmentRejected{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "closed",
			RejectedAt:    timestamppb.New(time.Now()),
		}
		eventAny, _ := anypb.New(event)
		return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
	}

	if state.IsFull() {
		event := &examples.TournamentEnrollmentRejected{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "full",
			RejectedAt:    timestamppb.New(time.Now()),
		}
		eventAny, _ := anypb.New(event)
		return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
	}

	if state.IsPlayerRegistered(playerRootHex) {
		event := &examples.TournamentEnrollmentRejected{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "already_registered",
			RejectedAt:    timestamppb.New(time.Now()),
		}
		eventAny, _ := anypb.New(event)
		return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
	}

	// Success
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
