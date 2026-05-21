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

// HandleProcessRebuy handles the ProcessRebuy command (sent by Rebuy PM).
// Returns RebuyProcessed on success, RebuyDenied (event-shape) on
// per-config denials. Tournament-not-running / not-exists raise
// command-level errors.
//
// Mirrors Py `handle_process_rebuy` + `_rebuy_denial_reason`.
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
		return nil, angzarr.NewCommandRejectedError("Tournament is not running")
	}

	playerRootHex := hex.EncodeToString(cmd.PlayerRoot)
	emitDenied := func(reason string) (*pb.EventBook, error) {
		event := &examples.RebuyDenied{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			Reason:        reason,
			DeniedAt:      timestamppb.New(time.Now()),
		}
		eventAny, err := anypb.New(event)
		if err != nil {
			return nil, err
		}
		return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
	}

	if !state.IsPlayerRegistered(playerRootHex) {
		// Unregistered player → event-shape per EU-0818 (PM compensates).
		return emitDenied("Player is not registered in this tournament")
	}

	if state.RebuyConfig == nil || !state.RebuyConfig.Enabled {
		return emitDenied("Rebuys are not enabled for this tournament")
	}

	// Cutoff-disabled when RebuyLevelCutoff == 0.
	if state.RebuyConfig.RebuyLevelCutoff > 0 &&
		state.CurrentLevel > state.RebuyConfig.RebuyLevelCutoff {
		return emitDenied("Rebuy window is closed (past cutoff level)")
	}

	rebuysUsed := state.PlayerRebuyCount(playerRootHex)
	maxRebuys := state.RebuyConfig.MaxRebuys
	if maxRebuys > 0 && rebuysUsed >= maxRebuys {
		return emitDenied("Maximum rebuys reached for this player")
	}

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
