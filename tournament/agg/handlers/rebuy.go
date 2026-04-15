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

// HandleProcessRebuy handles the ProcessRebuy command (sent by Rebuy PM).
// Returns RebuyProcessed on success, RebuyDenied on failure.
func HandleProcessRebuy(
	commandBook *pb.CommandBook,
	commandAny *anypb.Any,
	state TournamentState,
	seq uint32,
) (*pb.EventBook, error) {
	var cmd examples.ProcessRebuy
	if err := proto.Unmarshal(commandAny.Value, &cmd); err != nil {
		return nil, err
	}

	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Tournament does not exist")
	}
	if !state.IsRunning() {
		return nil, angzarr.NewCommandRejectedError("Tournament not running")
	}

	playerRootHex := hex.EncodeToString(cmd.PlayerRoot)
	if !state.IsPlayerRegistered(playerRootHex) {
		return nil, angzarr.NewCommandRejectedError("Player not registered")
	}

	// Check rebuy eligibility
	if state.RebuyConfig == nil || !state.RebuyConfig.Enabled {
		event := &examples.RebuyDenied{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "rebuys_disabled",
			DeniedAt:      timestamppb.New(time.Now()),
		}
		eventAny, _ := anypb.New(event)
		return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
	}

	// Check rebuy window (current level must be <= cutoff)
	if state.CurrentLevel > state.RebuyConfig.RebuyLevelCutoff {
		event := &examples.RebuyDenied{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "window_closed",
			DeniedAt:      timestamppb.New(time.Now()),
		}
		eventAny, _ := anypb.New(event)
		return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
	}

	// Check max rebuys
	rebuysUsed := state.PlayerRebuyCount(playerRootHex)
	maxRebuys := state.RebuyConfig.MaxRebuys
	if maxRebuys > 0 && rebuysUsed >= maxRebuys {
		event := &examples.RebuyDenied{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        "max_reached",
			DeniedAt:      timestamppb.New(time.Now()),
		}
		eventAny, _ := anypb.New(event)
		return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
	}

	// Success
	event := &examples.RebuyProcessed{
		PlayerRoot:    cmd.PlayerRoot,
		ReservationId: cmd.ReservationId,
		RebuyCost:     state.RebuyConfig.RebuyCost,
		ChipsAdded:    state.RebuyConfig.RebuyChips,
		RebuyCount:    rebuysUsed + 1,
		ProcessedAt:   timestamppb.New(time.Now()),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return nil, err
	}
	return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
}
