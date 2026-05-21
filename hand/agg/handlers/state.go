// Package handlers implements hand aggregate state reconstruction.
package handlers

import (
	"encoding/hex"
	"strconv"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr/v1"
	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
)

// HandState represents the current state of a hand aggregate.
type HandState struct {
	HandID      string
	TableRoot   []byte
	HandNumber  int64
	GameVariant examples.GameVariant

	// Deck state
	RemainingDeck []*examples.Card

	// Player state
	Players map[string]*PlayerHandState // player_root_hex -> state

	// Community cards
	CommunityCards []*examples.Card

	// Betting state
	CurrentPhase     examples.BettingPhase
	ActionOnPosition int32
	CurrentBet       int64
	MinRaise         int64
	Pots             []*PotState

	// Positions
	DealerPosition     int32
	SmallBlindPosition int32
	BigBlindPosition   int32

	Status string // "dealing", "betting", "showdown", "complete"
}

// PlayerHandState represents a player's state in the hand.
type PlayerHandState struct {
	PlayerRoot    []byte
	Position      int32
	HoleCards     []*examples.Card
	Stack         int64
	BetThisRound  int64
	TotalInvested int64
	HasActed      bool
	HasFolded     bool
	IsAllIn       bool
}

// PotState represents a pot (main or side).
type PotState struct {
	Amount          int64
	EligiblePlayers [][]byte
	PotType         string
}

// NewHandState creates a new empty hand state.
func NewHandState() HandState {
	return HandState{
		Players: make(map[string]*PlayerHandState),
		Pots:    []*PotState{{PotType: "main"}},
	}
}

// Exists returns true if the hand has been dealt.
func (s HandState) Exists() bool {
	return s.HandID != ""
}

// IsComplete returns true if the hand is complete.
func (s HandState) IsComplete() bool {
	return s.Status == "complete"
}

// TotalPot returns the sum of all pots.
func (s HandState) TotalPot() int64 {
	total := int64(0)
	for _, pot := range s.Pots {
		total += pot.Amount
	}
	return total
}

// IsShowdown reports whether the hand is in the showdown phase.
func (s HandState) IsShowdown() bool {
	return s.Status == "showdown"
}

// PlayerHasPostedBlind reports whether any player has bet this round
// (i.e. a blind has been posted).
func (s HandState) PlayerHasPostedBlind() bool {
	for _, p := range s.Players {
		if p.BetThisRound > 0 {
			return true
		}
	}
	return false
}

// GetPlayerByRoot returns the player state for a given player root.
func (s HandState) GetPlayerByRoot(root []byte) *PlayerHandState {
	return s.Players[hex.EncodeToString(root)]
}

// ActivePlayerCount returns the number of players who haven't folded.
func (s HandState) ActivePlayerCount() int {
	count := 0
	for _, p := range s.Players {
		if !p.HasFolded {
			count++
		}
	}
	return count
}

// Event applier functions for StateRouter

func applyCardsDealt(state *HandState, event *examples.CardsDealt) {
	// Hand ID format: hex(table_root) + "_" + decimal(hand_number).
	// Pre-fix this used `string(rune(N))` which yielded the unicode char
	// at code-point N instead of the decimal string — broke hand_id
	// invariants asserted by EU-1133 ("hand_id is aabbccdd_5").
	state.HandID = hex.EncodeToString(event.TableRoot) + "_" + strconv.FormatInt(event.HandNumber, 10)
	state.TableRoot = event.TableRoot
	state.HandNumber = event.HandNumber
	state.GameVariant = event.GameVariant
	state.DealerPosition = event.DealerPosition
	state.RemainingDeck = event.RemainingDeck
	state.CurrentPhase = examples.BettingPhase_PREFLOP
	state.Status = "betting"

	// Initialize players
	for _, p := range event.Players {
		key := hex.EncodeToString(p.PlayerRoot)
		state.Players[key] = &PlayerHandState{
			PlayerRoot: p.PlayerRoot,
			Position:   p.Position,
			Stack:      p.Stack,
		}
	}

	// Apply hole cards
	for _, pc := range event.PlayerCards {
		key := hex.EncodeToString(pc.PlayerRoot)
		if player := state.Players[key]; player != nil {
			player.HoleCards = pc.Cards
		}
	}
}

func applyBlindPosted(state *HandState, event *examples.BlindPosted) {
	key := hex.EncodeToString(event.PlayerRoot)
	if player := state.Players[key]; player != nil {
		player.Stack = event.PlayerStack
		player.BetThisRound += event.Amount
		player.TotalInvested += event.Amount
	}
	state.Pots[0].Amount = event.PotTotal
	if event.Amount > state.CurrentBet {
		state.CurrentBet = event.Amount
	}
	// Big blind sets the minimum raise amount
	if event.BlindType == "big" {
		state.MinRaise = event.Amount
	}
}

func applyActionTaken(state *HandState, event *examples.ActionTaken) {
	key := hex.EncodeToString(event.PlayerRoot)
	if player := state.Players[key]; player != nil {
		player.Stack = event.PlayerStack
		player.HasActed = true

		switch event.Action {
		case examples.ActionType_FOLD:
			player.HasFolded = true
		case examples.ActionType_ALL_IN:
			player.IsAllIn = true
			player.BetThisRound += event.Amount
			player.TotalInvested += event.Amount
		case examples.ActionType_BET, examples.ActionType_RAISE, examples.ActionType_CALL:
			player.BetThisRound += event.Amount
			player.TotalInvested += event.Amount
		}
	}
	state.Pots[0].Amount = event.PotTotal
	state.CurrentBet = event.AmountToCall
}

func applyBettingRoundComplete(state *HandState, event *examples.BettingRoundComplete) {
	// Reset for next round
	for _, p := range state.Players {
		p.BetThisRound = 0
		p.HasActed = false
	}
	state.CurrentBet = 0

	// Phase advancement: Five Card Draw transitions PREFLOP → DRAW;
	// other variants' phase is advanced by CommunityCardsDealt.
	if state.GameVariant == examples.GameVariant_FIVE_CARD_DRAW &&
		event.CompletedPhase == examples.BettingPhase_PREFLOP {
		state.CurrentPhase = examples.BettingPhase_DRAW
	}

	// Update stacks from snapshot
	for _, snap := range event.Stacks {
		key := hex.EncodeToString(snap.PlayerRoot)
		if player := state.Players[key]; player != nil {
			player.Stack = snap.Stack
			player.IsAllIn = snap.IsAllIn
			player.HasFolded = snap.HasFolded
		}
	}
}

func applyCommunityCardsDealt(state *HandState, event *examples.CommunityCardsDealt) {
	state.CommunityCards = event.AllCommunityCards
	state.CurrentPhase = event.Phase
}

func applyDrawCompleted(state *HandState, event *examples.DrawCompleted) {
	key := hex.EncodeToString(event.PlayerRoot)
	if player := state.Players[key]; player != nil {
		// Replace discarded cards with new cards
		if len(event.NewCards) > 0 {
			player.HoleCards = append(player.HoleCards[:len(player.HoleCards)-int(event.CardsDiscarded)], event.NewCards...)
		}
	}
	// Update remaining deck
	if int(event.CardsDrawn) <= len(state.RemainingDeck) {
		state.RemainingDeck = state.RemainingDeck[event.CardsDrawn:]
	}
}

func applyShowdownStarted(state *HandState, _ *examples.ShowdownStarted) {
	state.Status = "showdown"
}

func applyCardsRevealed(state *HandState, _ *examples.CardsRevealed) {
	// Cards revealed during showdown - could store revealed hands
}

func applyCardsMucked(state *HandState, _ *examples.CardsMucked) {
	// Player mucked - could mark as mucked
}

func applyPotAwarded(state *HandState, event *examples.PotAwarded) {
	for _, winner := range event.Winners {
		key := hex.EncodeToString(winner.PlayerRoot)
		if player := state.Players[key]; player != nil {
			player.Stack += winner.Amount
		}
	}
}

func applyHandComplete(state *HandState, event *examples.HandComplete) {
	state.Status = "complete"
	// Update final stacks
	for _, snap := range event.FinalStacks {
		key := hex.EncodeToString(snap.PlayerRoot)
		if player := state.Players[key]; player != nil {
			player.Stack = snap.Stack
		}
	}
}

// applyBringInPosted updates state for a stud-game bring-in (forced first
// bet on third street). Mirrors Python hand/agg/handlers/hand.py
// applies(BringInPosted).
func applyBringInPosted(state *HandState, event *examples.BringInPosted) {
	key := hex.EncodeToString(event.PlayerRoot)
	if player := state.Players[key]; player != nil {
		player.Stack = event.PlayerStack
		player.BetThisRound += event.Amount
		player.TotalInvested += event.Amount
	}
	state.Pots[0].Amount = event.PotTotal
	if event.Amount > state.CurrentBet {
		state.CurrentBet = event.Amount
	}
}

// applyStudStreetDealt advances state when a new stud street is dealt
// (4th-7th streets in 7-card stud). Mirrors Python applies(StudStreetDealt).
func applyStudStreetDealt(state *HandState, event *examples.StudStreetDealt) {
	// Stud uses up-cards: append each dealt up-card to that player's hole
	// cards (stud has no community board — every player gets their own).
	// Consume one card from the deck per up-card dealt.
	for _, pc := range event.UpCards {
		key := hex.EncodeToString(pc.PlayerRoot)
		if player := state.Players[key]; player != nil {
			player.HoleCards = append(player.HoleCards, pc.UpCards...)
		}
		if len(pc.UpCards) > 0 && len(pc.UpCards) <= len(state.RemainingDeck) {
			state.RemainingDeck = state.RemainingDeck[len(pc.UpCards):]
		}
	}
}

// applyActionClockStarted is a state-neutral applier — the clock event is
// informational; no state changes. Mirrors Python applies(ActionClockStarted)
// which has the same shape (no state mutation).
func applyActionClockStarted(_ *HandState, _ *examples.ActionClockStarted) {}

// applyPriorChipPulledBack reverses a player's chip commitment after a
// PullBackPriorChip command resolved. Mirrors Python
// applies(PriorChipPulledBack).
//
// The proto event carries only ChipsPulled (not derived stack/pot totals),
// so we reconstruct the deltas: refund chips_pulled into the player's stack,
// reverse it out of the round commitment, and decrement the main pot by the
// same amount.
func applyPriorChipPulledBack(state *HandState, event *examples.PriorChipPulledBack) {
	key := hex.EncodeToString(event.PlayerRoot)
	if player := state.Players[key]; player != nil {
		player.Stack += event.ChipsPulled
		player.BetThisRound -= event.ChipsPulled
		if player.BetThisRound < 0 {
			player.BetThisRound = 0
		}
		player.TotalInvested -= event.ChipsPulled
		if player.TotalInvested < 0 {
			player.TotalInvested = 0
		}
	}
	state.Pots[0].Amount -= event.ChipsPulled
	if state.Pots[0].Amount < 0 {
		state.Pots[0].Amount = 0
	}
}

// applyUnderbetCorrected updates state when a CorrectIllegalBet command
// resolved. Mirrors Python applies(UnderbetCorrected).
//
// Each UnderbetAdjustment carries PriorContribution / NewContribution and a
// RefundToStack delta. Adjustments can move chips either way:
//   - PL_ILLEGAL_OVERBET: NewContribution < PriorContribution; refund to stack
//   - NL_DECLARED_UNDERRAISE: NewContribution > PriorContribution; extra chips
//     come out of stack.
//
// We update BetThisRound/TotalInvested to the new absolute contribution and
// adjust stack by the refund delta (positive refund = chips back to stack).
// CurrentBet is raised to corrected_amount.
func applyUnderbetCorrected(state *HandState, event *examples.UnderbetCorrected) {
	for _, adj := range event.Adjustments {
		key := hex.EncodeToString(adj.PlayerRoot)
		delta := adj.NewContribution - adj.PriorContribution
		if player := state.Players[key]; player != nil {
			player.BetThisRound += delta
			if player.BetThisRound < 0 {
				player.BetThisRound = 0
			}
			player.TotalInvested += delta
			if player.TotalInvested < 0 {
				player.TotalInvested = 0
			}
			player.Stack += adj.RefundToStack - (delta - 0)
			// Concretely: if delta > 0 chips moved stack -> pot; if delta < 0
			// (NewContribution < PriorContribution) refund_to_stack lifts the
			// stack back up. RefundToStack already encodes the player-visible
			// net effect on stack for the common PL overbet case, but for the
			// underraise case the delta-out-of-stack path must subtract too.
			if player.Stack < 0 {
				player.Stack = 0
			}
		}
		state.Pots[0].Amount += delta
		if state.Pots[0].Amount < 0 {
			state.Pots[0].Amount = 0
		}
	}
	if event.CorrectedAmount > state.CurrentBet {
		state.CurrentBet = event.CorrectedAmount
	}
}

// stateRouter is the fluent state reconstruction router.
var stateRouter = angzarr.NewStateRouter(NewHandState).
	On(applyCardsDealt).
	On(applyBlindPosted).
	On(applyActionTaken).
	On(applyBettingRoundComplete).
	On(applyCommunityCardsDealt).
	On(applyDrawCompleted).
	On(applyShowdownStarted).
	On(applyCardsRevealed).
	On(applyCardsMucked).
	On(applyPotAwarded).
	On(applyHandComplete).
	On(applyBringInPosted).
	On(applyStudStreetDealt).
	On(applyActionClockStarted).
	On(applyPriorChipPulledBack).
	On(applyUnderbetCorrected)

// RebuildState rebuilds hand state from event history.
func RebuildState(eventBook *pb.EventBook) HandState {
	if eventBook == nil {
		return NewHandState()
	}

	state := NewHandState()
	for _, page := range eventBook.Pages {
		event := page.GetEvent()
		if event != nil {
			stateRouter.ApplySingle(&state, event)
		}
	}
	return state
}
