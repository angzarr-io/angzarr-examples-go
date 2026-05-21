// Package handlers implements table aggregate command handlers for testing.
//
// These functional handlers mirror the OO handlers in the main package,
// enabling unit testing without importing the main package. The contract
// follows the Python reference at
// `examples-python/main/table/agg/handlers/table.py`.
package handlers

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sort"
	"time"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr/v1"
	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Cross-language rejection codes mirroring Python's table.agg.errors.
const (
	CodeMinBuyInMustBePositive        = "MIN_BUY_IN_MUST_BE_POSITIVE"
	CodeMaxBuyInMustExceedMinBuyIn    = "MAX_BUY_IN_MUST_EXCEED_MIN_BUY_IN"
	CodeSmallBlindMustBePositive      = "SMALL_BLIND_MUST_BE_POSITIVE"
	CodeBigBlindMustExceedSmallBlind  = "BIG_BLIND_MUST_EXCEED_SMALL_BLIND"
	CodeMaxPlayersOutOfRange          = "MAX_PLAYERS_OUT_OF_RANGE"
	CodeBuyInBelowMin                 = "BUY_IN_BELOW_MIN"
	CodeBuyInAboveMax                 = "BUY_IN_ABOVE_MAX"
	CodeSeatOccupied                  = "SEAT_OCCUPIED"
	CodeAmountMustBePositive          = "AMOUNT_MUST_BE_POSITIVE"
	CodeSeatPositionMismatch          = "SEAT_POSITION_MISMATCH"
	CodeTableHandForHandRoundComplete = "TABLE_HAND_FOR_HAND_ROUND_COMPLETE"
	CodeTableNotInHandForHandWaiting  = "TABLE_NOT_IN_HAND_FOR_HAND_WAITING"
	CodeNotEnoughPlayersToStartHand   = "NOT_ENOUGH_PLAYERS_TO_START_HAND"
)

// HandleCreateTable handles the CreateTable command.
//
// Cross-field semantic failures (big_blind < small_blind, max_buy_in <
// min_buy_in) raise FAILED_PRECONDITION; pure shape failures
// (zero/negative inputs, range violations) raise INVALID_ARGUMENT.
// Matches Python's table.py:520-565.
func HandleCreateTable(_ *pb.EventBook, cmdAny *anypb.Any, state TableState) (*anypb.Any, error) {
	var cmd examples.CreateTable
	if err := cmdAny.UnmarshalTo(&cmd); err != nil {
		return nil, err
	}

	// Guard
	if state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Table already exists")
	}

	// Validate
	if cmd.TableName == "" {
		return nil, angzarr.NewInvalidArgumentError("table_name is required")
	}
	if cmd.SmallBlind <= 0 {
		return nil, angzarr.NewInvalidArgumentRejectionWithCode(
			CodeSmallBlindMustBePositive,
			"small_blind must be positive",
			map[string]string{"value": fmt.Sprintf("%d", cmd.SmallBlind)},
		)
	}
	if cmd.BigBlind <= 0 || cmd.BigBlind < cmd.SmallBlind {
		return nil, angzarr.NewPreconditionFailedRejection(
			CodeBigBlindMustExceedSmallBlind,
			"big_blind must be >= small_blind",
			map[string]string{
				"lhs": fmt.Sprintf("%d", cmd.BigBlind),
				"rhs": fmt.Sprintf("%d", cmd.SmallBlind),
			},
		)
	}
	if cmd.MinBuyIn <= 0 {
		return nil, angzarr.NewInvalidArgumentRejectionWithCode(
			CodeMinBuyInMustBePositive,
			"min_buy_in must be positive",
			map[string]string{"value": fmt.Sprintf("%d", cmd.MinBuyIn)},
		)
	}
	if cmd.MaxBuyIn < cmd.MinBuyIn {
		return nil, angzarr.NewPreconditionFailedRejection(
			CodeMaxBuyInMustExceedMinBuyIn,
			"max_buy_in must be >= min_buy_in",
			map[string]string{
				"lhs": fmt.Sprintf("%d", cmd.MaxBuyIn),
				"rhs": fmt.Sprintf("%d", cmd.MinBuyIn),
			},
		)
	}
	if cmd.MaxPlayers < 2 || cmd.MaxPlayers > 10 {
		return nil, angzarr.NewInvalidArgumentRejectionWithCode(
			CodeMaxPlayersOutOfRange,
			"max_players must be 2-10",
			map[string]string{"got": fmt.Sprintf("%d", cmd.MaxPlayers)},
		)
	}

	actionTimeout := cmd.ActionTimeoutSeconds
	if actionTimeout == 0 {
		actionTimeout = 30
	}

	// Compute
	event := &examples.TableCreated{
		TableName:            cmd.TableName,
		GameVariant:          cmd.GameVariant,
		SmallBlind:           cmd.SmallBlind,
		BigBlind:             cmd.BigBlind,
		MinBuyIn:             cmd.MinBuyIn,
		MaxBuyIn:             cmd.MaxBuyIn,
		MaxPlayers:           cmd.MaxPlayers,
		ActionTimeoutSeconds: actionTimeout,
		CreatedAt:            timestamppb.New(time.Now()),
	}

	return anypb.New(event)
}

// HandleJoinTable handles the JoinTable command.
func HandleJoinTable(_ *pb.EventBook, cmdAny *anypb.Any, state TableState) (*anypb.Any, error) {
	var cmd examples.JoinTable
	if err := cmdAny.UnmarshalTo(&cmd); err != nil {
		return nil, err
	}

	// Guard
	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Table does not exist")
	}

	// Validate
	if len(cmd.PlayerRoot) == 0 {
		return nil, angzarr.NewInvalidArgumentError("player_root is required")
	}
	if findSeatByPlayer(state, cmd.PlayerRoot) >= 0 {
		return nil, angzarr.NewCommandRejectedError("Player already seated at table")
	}
	if state.PlayerCount() >= int(state.MaxPlayers) {
		return nil, angzarr.NewCommandRejectedError("Table is full")
	}
	if cmd.BuyInAmount < state.MinBuyIn {
		return nil, angzarr.NewPreconditionFailedRejection(
			CodeBuyInBelowMin,
			fmt.Sprintf("Buy-in must be at least %d", state.MinBuyIn),
			map[string]string{
				"got":   fmt.Sprintf("%d", cmd.BuyInAmount),
				"bound": fmt.Sprintf("%d", state.MinBuyIn),
			},
		)
	}
	if cmd.BuyInAmount > state.MaxBuyIn {
		return nil, angzarr.NewPreconditionFailedRejection(
			CodeBuyInAboveMax,
			fmt.Sprintf("Buy-in cannot exceed %d", state.MaxBuyIn),
			map[string]string{
				"got":   fmt.Sprintf("%d", cmd.BuyInAmount),
				"bound": fmt.Sprintf("%d", state.MaxBuyIn),
			},
		)
	}

	// Determine seat position
	var seatPos int32
	if cmd.PreferredSeat >= 0 && cmd.PreferredSeat < state.MaxPlayers {
		if _, occupied := state.Seats[cmd.PreferredSeat]; occupied {
			return nil, angzarr.NewPreconditionFailedRejection(
				CodeSeatOccupied,
				"Seat is occupied",
				map[string]string{"seat": fmt.Sprintf("%d", cmd.PreferredSeat)},
			)
		}
		seatPos = cmd.PreferredSeat
	} else {
		seatPos = nextAvailableSeat(state)
	}

	// Compute
	event := &examples.PlayerJoined{
		PlayerRoot:   cmd.PlayerRoot,
		SeatPosition: seatPos,
		BuyInAmount:  cmd.BuyInAmount,
		Stack:        cmd.BuyInAmount,
		JoinedAt:     timestamppb.New(time.Now()),
	}

	return anypb.New(event)
}

// HandleLeaveTable handles the LeaveTable command.
func HandleLeaveTable(_ *pb.EventBook, cmdAny *anypb.Any, state TableState) (*anypb.Any, error) {
	var cmd examples.LeaveTable
	if err := cmdAny.UnmarshalTo(&cmd); err != nil {
		return nil, err
	}

	// Guard
	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Table does not exist")
	}

	// Validate
	if len(cmd.PlayerRoot) == 0 {
		return nil, angzarr.NewInvalidArgumentError("player_root is required")
	}

	pos := findSeatByPlayer(state, cmd.PlayerRoot)
	if pos < 0 {
		return nil, angzarr.NewCommandRejectedError("Player is not seated at table")
	}
	if state.Status == "in_hand" {
		return nil, angzarr.NewCommandRejectedError("Cannot leave table during a hand")
	}

	seat := state.Seats[pos]

	// Compute
	event := &examples.PlayerLeft{
		PlayerRoot:     cmd.PlayerRoot,
		SeatPosition:   pos,
		ChipsCashedOut: seat.Stack,
		LeftAt:         timestamppb.New(time.Now()),
	}

	return anypb.New(event)
}

// HandleStartHand handles the StartHand command. Mirrors Python's
// table.py:647-754 — applies the TDA dead-button blind advancement, also
// guards against starting while parked at H4H COMPLETE.
func HandleStartHand(_ *pb.EventBook, cmdAny *anypb.Any, state TableState) (*anypb.Any, error) {
	// Guard
	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Table does not exist")
	}
	if state.Status == "in_hand" {
		return nil, angzarr.NewCommandRejectedError("Hand already in progress")
	}
	if state.HandForHandStatus == "COMPLETE" {
		return nil, angzarr.NewPreconditionFailedRejection(
			CodeTableHandForHandRoundComplete,
			"Hand-for-hand round complete; waiting for next-round signal",
			nil,
		)
	}
	if state.ActivePlayerCount() < 2 {
		return nil, angzarr.NewPreconditionFailedRejection(
			CodeNotEnoughPlayersToStartHand,
			"Not enough players to start hand",
			map[string]string{
				"requested": "2",
				"available": fmt.Sprintf("%d", state.ActivePlayerCount()),
			},
		)
	}

	// Optionally parse StartHand cmd to read PlayersOnPenalty / BlindLevel.
	var startCmd examples.StartHand
	if cmdAny != nil {
		_ = cmdAny.UnmarshalTo(&startCmd)
	}

	// Generate hand root (deterministic based on table + hand number)
	handNumber := state.HandCount + 1
	handRootInput := fmt.Sprintf("angzarr.poker.hand.%s.%d", state.TableID, handNumber)
	hash := sha256.Sum256([]byte(handRootInput))
	handRoot := hash[:16]

	dealerPosition, sbPosition, bbPosition := advanceBlindsWithDeadButton(state)

	// Build active players list (sorted ascending by seat).
	activePositions := sortedActivePositions(state)
	var activePlayers []*examples.SeatSnapshot
	for _, pos := range activePositions {
		seat := state.Seats[pos]
		activePlayers = append(activePlayers, &examples.SeatSnapshot{
			Position:   pos,
			PlayerRoot: seat.PlayerRoot,
			Stack:      seat.Stack,
		})
	}

	// Compute
	event := &examples.HandStarted{
		HandRoot:           handRoot,
		HandNumber:         handNumber,
		DealerPosition:     dealerPosition,
		SmallBlindPosition: sbPosition,
		BigBlindPosition:   bbPosition,
		GameVariant:        state.GameVariant,
		SmallBlind:         state.SmallBlind,
		BigBlind:           state.BigBlind,
		BlindLevel:         startCmd.BlindLevel,
		ActivePlayers:      activePlayers,
		StartedAt:          timestamppb.New(time.Now()),
	}

	return anypb.New(event)
}

// HandleEndHand handles the EndHand command.
func HandleEndHand(_ *pb.EventBook, cmdAny *anypb.Any, state TableState) (*anypb.Any, error) {
	var cmd examples.EndHand
	if err := cmdAny.UnmarshalTo(&cmd); err != nil {
		return nil, err
	}

	// Guard
	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Table does not exist")
	}
	if state.Status != "in_hand" {
		return nil, angzarr.NewCommandRejectedError("No hand in progress")
	}
	if hex.EncodeToString(cmd.HandRoot) != hex.EncodeToString(state.CurrentHandRoot) {
		return nil, angzarr.NewCommandRejectedError("Hand root mismatch")
	}

	// Calculate stack changes from results
	stackChanges := make(map[string]int64)
	for _, result := range cmd.Results {
		playerHex := hex.EncodeToString(result.WinnerRoot)
		stackChanges[playerHex] += result.Amount
	}

	// Compute
	event := &examples.HandEnded{
		HandRoot:     cmd.HandRoot,
		StackChanges: stackChanges,
		Results:      cmd.Results,
		EndedAt:      timestamppb.New(time.Now()),
	}

	return anypb.New(event)
}

// HandleSeatPlayer handles the PM-orchestrated SeatPlayer command.
//
// Unlike JoinTable, validation failures become SeatingRejected events
// (not errors) so the buy-in PM can compensate by releasing the
// reservation. The only error path is the table-not-found guard, which
// indicates a routing bug rather than a recoverable rejection. Mirrors
// Python table.py:794-882.
func HandleSeatPlayer(_ *pb.EventBook, cmdAny *anypb.Any, state TableState) (*anypb.Any, error) {
	var cmd examples.SeatPlayer
	if err := cmdAny.UnmarshalTo(&cmd); err != nil {
		return nil, err
	}

	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Table does not exist")
	}

	var reason string
	seatPos := cmd.Seat
	var rngSeedUsed []byte

	switch {
	case len(cmd.PlayerRoot) == 0:
		reason = "player_root is required"
	case findSeatByPlayer(state, cmd.PlayerRoot) >= 0:
		reason = "Player already seated"
	case cmd.Amount < state.MinBuyIn:
		reason = fmt.Sprintf("Buy-in must be at least %d", state.MinBuyIn)
	case cmd.Amount > state.MaxBuyIn:
		reason = "Buy-in above maximum"
	case cmd.Seat >= 0 && cmd.Seat < state.MaxPlayers:
		if _, occupied := state.Seats[cmd.Seat]; occupied {
			reason = "Seat is occupied"
		}
	case cmd.Seat == -1:
		if cmd.TournamentMode {
			openSeats := openSeatsList(state)
			if len(openSeats) == 0 {
				reason = "Table is full"
			} else {
				seed := cmd.RngSeed
				if len(seed) == 0 {
					seed = append(append([]byte{}, cmd.PlayerRoot...), []byte(state.TableID)...)
				}
				h := sha256.Sum256(seed)
				rngSeedUsed = h[:16]
				seedInt := int64(binary.BigEndian.Uint64(rngSeedUsed[:8]))
				r := rand.New(rand.NewSource(seedInt))
				seatPos = openSeats[r.Intn(len(openSeats))]
			}
		} else {
			next := nextAvailableSeat(state)
			if next < 0 {
				reason = "Table is full"
			} else {
				seatPos = next
			}
		}
	default:
		reason = "Invalid seat position"
	}

	if reason != "" {
		rejected := &examples.SeatingRejected{
			PlayerRoot:    cmd.PlayerRoot,
			ReservationId: cmd.ReservationId,
			RequestedSeat: cmd.Seat,
			Reason:        reason,
			RejectedAt:    timestamppb.New(time.Now()),
		}
		return anypb.New(rejected)
	}

	seated := &examples.PlayerSeated{
		PlayerRoot:    cmd.PlayerRoot,
		ReservationId: cmd.ReservationId,
		SeatPosition:  seatPos,
		Stack:         cmd.Amount,
		RngSeed:       rngSeedUsed,
		SeatedAt:      timestamppb.New(time.Now()),
	}
	return anypb.New(seated)
}

// HandleAddRebuyChips handles the PM-orchestrated AddRebuyChips command.
// Unlike SeatPlayer, this always returns an error on rejection — the
// rebuy PM has already reserved the funds, so any precondition failure
// is a bug in the upstream flow, not something to compensate for.
// Mirrors Python table.py:884-920.
func HandleAddRebuyChips(_ *pb.EventBook, cmdAny *anypb.Any, state TableState) (*anypb.Any, error) {
	var cmd examples.AddRebuyChips
	if err := cmdAny.UnmarshalTo(&cmd); err != nil {
		return nil, err
	}

	if !state.Exists() {
		return nil, angzarr.NewCommandRejectedError("Table does not exist")
	}
	if len(cmd.PlayerRoot) == 0 {
		return nil, angzarr.NewInvalidArgumentError("player_root is required")
	}
	if cmd.Amount <= 0 {
		return nil, angzarr.NewInvalidArgumentRejectionWithCode(
			CodeAmountMustBePositive,
			"amount must be positive",
			map[string]string{"value": fmt.Sprintf("%d", cmd.Amount)},
		)
	}
	pos := findSeatByPlayer(state, cmd.PlayerRoot)
	if pos < 0 {
		return nil, angzarr.NewCommandRejectedError("Player is not seated at table")
	}
	if pos != cmd.Seat {
		return nil, angzarr.NewPreconditionFailedRejection(
			CodeSeatPositionMismatch,
			fmt.Sprintf("Seat position mismatch (expected %d, got %d)", pos, cmd.Seat),
			map[string]string{
				"expected": fmt.Sprintf("%d", pos),
				"got":      fmt.Sprintf("%d", cmd.Seat),
			},
		)
	}

	currentStack := state.Seats[pos].Stack
	event := &examples.RebuyChipsAdded{
		PlayerRoot:    cmd.PlayerRoot,
		ReservationId: cmd.ReservationId,
		Seat:          cmd.Seat,
		Amount:        cmd.Amount,
		NewStack:      currentStack + cmd.Amount,
		AddedAt:       timestamppb.New(time.Now()),
	}
	return anypb.New(event)
}

// Helper functions

func findSeatByPlayer(state TableState, playerRoot []byte) int32 {
	playerHex := hex.EncodeToString(playerRoot)
	for pos, seat := range state.Seats {
		if hex.EncodeToString(seat.PlayerRoot) == playerHex {
			return pos
		}
	}
	return -1
}

func nextAvailableSeat(state TableState) int32 {
	for i := int32(0); i < state.MaxPlayers; i++ {
		if _, exists := state.Seats[i]; !exists {
			return i
		}
	}
	return -1
}

func openSeatsList(state TableState) []int32 {
	var open []int32
	for i := int32(0); i < state.MaxPlayers; i++ {
		if _, exists := state.Seats[i]; !exists {
			open = append(open, i)
		}
	}
	sort.Slice(open, func(i, j int) bool { return open[i] < open[j] })
	return open
}

func sortedActivePositions(state TableState) []int32 {
	var positions []int32
	for pos, seat := range state.Seats {
		if !seat.IsSittingOut {
			positions = append(positions, pos)
		}
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i] < positions[j] })
	return positions
}

// advanceBlindsWithDeadButton computes (dealer, sb, bb) positions for
// the next hand using the TDA dead-button rule. Mirrors Python's
// _advance_blinds_with_dead_button in table.py:402-484.
//
// Invariant: every player must pay the BB exactly once per orbit.
// Behaviours:
//   - First hand (no prior BB): WSOP Rule 85 — dealer is the highest-
//     numbered active seat (first chip stack to the operator dealer's
//     right).
//   - Heads-up (2 active): button alternates; dealer is the next active
//     seat clockwise from prior dealer and is also SB; other is BB.
//   - 3+ players, prior BB still seated: BB advances to next active CW
//     from prior BB; SB and dealer derived clockwise-backward.
//   - 3+ players, prior BB busted: dead-button override — button freezes
//     at prior dealer (or next active); SB is next active CW from
//     vacated BB; BB follows.
func advanceBlindsWithDeadButton(state TableState) (dealer, sb, bb int32) {
	active := sortedActivePositions(state)
	if len(active) < 2 {
		return state.DealerPosition, state.DealerPosition, state.DealerPosition
	}

	prevBB := state.LastBigBlindPosition
	prevDealer := state.DealerPosition

	// First hand: WSOP Rule 85.
	if prevBB < 0 {
		dealer := active[len(active)-1] // highest seat
		return deriveBlindPositions(active, dealer)
	}

	// Heads-up: button alternates.
	if len(active) == 2 {
		newDealer := nextActiveAfterSeat(active, prevDealer)
		// In headsup, dealer == SB; the other player is BB.
		var newBB int32
		for _, s := range active {
			if s != newDealer {
				newBB = s
				break
			}
		}
		return newDealer, newDealer, newBB
	}

	// 3+ players. If prev BB busted, apply dead-button override.
	if !contains(active, prevBB) {
		newDealer := prevDealer
		if !contains(active, prevDealer) {
			newDealer = nextActiveAfterSeat(active, prevDealer)
		}
		newSB := nextActiveAfterSeat(active, prevBB)
		newBB := nextActiveAfterSeat(active, newSB)
		return newDealer, newSB, newBB
	}

	// Standard advance: BB → next active CW from prev BB.
	newBB := nextActiveAfterSeat(active, prevBB)
	bbIdx := indexOf(active, newBB)
	newSB := active[(bbIdx-1+len(active))%len(active)]
	sbIdx := indexOf(active, newSB)
	newDealer := active[(sbIdx-1+len(active))%len(active)]
	return newDealer, newSB, newBB
}

// deriveBlindPositions returns (dealer, sb, bb) given the dealer and the
// sorted active-seats list. Used on the first hand only.
func deriveBlindPositions(active []int32, dealer int32) (int32, int32, int32) {
	dIdx := indexOf(active, dealer)
	if dIdx < 0 {
		dIdx = 0
	}
	if len(active) == 2 {
		return dealer, active[dIdx], active[(dIdx+1)%2]
	}
	sb := active[(dIdx+1)%len(active)]
	bb := active[(dIdx+2)%len(active)]
	return dealer, sb, bb
}

// nextActiveAfterSeat returns the next active seat clockwise after the
// given (possibly vacant) seat. If no active seat is strictly greater,
// wraps to active[0].
func nextActiveAfterSeat(active []int32, seat int32) int32 {
	for _, s := range active {
		if s > seat {
			return s
		}
	}
	return active[0]
}

func contains(xs []int32, x int32) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func indexOf(xs []int32, x int32) int {
	for i, v := range xs {
		if v == x {
			return i
		}
	}
	return -1
}
