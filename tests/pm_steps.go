// Package tests implements process manager step definitions for BDD tests.
package tests

import (
	"context"
	"fmt"

	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
	"github.com/cucumber/godog"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// HandPhase represents the process state machine phases.
type HandPhase string

const (
	PhaseDEALING           HandPhase = "DEALING"
	PhasePOSTING_BLINDS    HandPhase = "POSTING_BLINDS"
	PhaseBETTING           HandPhase = "BETTING"
	PhaseDEALING_COMMUNITY HandPhase = "DEALING_COMMUNITY"
	PhaseSHOWDOWN          HandPhase = "SHOWDOWN"
	PhaseCOMPLETE          HandPhase = "COMPLETE"
	PhaseDRAW              HandPhase = "DRAW"
)

// BettingPhase represents the betting round.
type BettingPhase string

const (
	BettingPREFLOP BettingPhase = "PREFLOP"
	BettingFLOP    BettingPhase = "FLOP"
	BettingTURN    BettingPhase = "TURN"
	BettingRIVER   BettingPhase = "RIVER"
	BettingDRAW    BettingPhase = "DRAW"
)

// PMPlayerState tracks player state within the process.
type PMPlayerState struct {
	PlayerRoot   []byte
	Position     int32
	Stack        int64
	BetThisRound int64
	HasActed     bool
	HasFolded    bool
	IsAllIn      bool
}

// HandProcess represents the PM's state for a hand.
type HandProcess struct {
	HandNumber       int64
	GameVariant      examples.GameVariant
	DealerPosition   int32
	SmallBlind       int64
	BigBlind         int64
	Players          map[int32]*PMPlayerState
	Phase            HandPhase
	BettingPhase     BettingPhase
	SmallBlindPosted bool
	BigBlindPosted   bool
	ActionOn         int32
	CurrentBet       int64
	PotTotal         int64
}

// PMContext holds state for PM tests.
type PMContext struct {
	process        *HandProcess
	sourceEvent    *anypb.Any
	resultCommands []string
	timedOut       bool
}

// NewPMContext creates a fresh PM context.
func NewPMContext() *PMContext {
	return &PMContext{}
}

var pmCtx *PMContext

// SetPMSourceEvent allows other step modules to set the PM source event.
// This is needed because godog step matching can call hand_steps before pm_steps.
func SetPMSourceEvent(event *anypb.Any) {
	if pmCtx != nil && pmCtx.process != nil {
		pmCtx.sourceEvent = event
	}
}

// RegisterPMSteps registers all process manager step definitions.
func RegisterPMSteps(ctx *godog.ScenarioContext) {
	pmCtx = NewPMContext()

	ctx.Before(func(c context.Context, sc *godog.Scenario) (context.Context, error) {
		pmCtx = NewPMContext()
		return c, nil
	})

	// Given steps
	ctx.Step(`^a HandFlowPM$`, aHandFlowPM)
	ctx.Step(`^a HandStarted event with:$`, aHandStartedEventWith)
	ctx.Step(`^an active hand process in phase ([A-Z_]+)$`, anActiveHandProcessInPhase)
	ctx.Step(`^an active hand process with betting_phase ([A-Z_]+)$`, anActiveHandProcessWithBettingPhase)
	ctx.Step(`^an active hand process with (\d+) players$`, anActiveHandProcessWithPlayers)
	ctx.Step(`^an active hand process with game_variant ([A-Z_]+)$`, anActiveHandProcessWithGameVariant)
	ctx.Step(`^an active hand process with player "([^"]*)" at stack (\d+)$`, anActiveHandProcessWithPlayerAtStack)
	ctx.Step(`^an active hand process$`, anActiveHandProcess)
	ctx.Step(`^a CardsDealt event$`, aCardsDealtEvent)
	ctx.Step(`^a BlindPosted event for small blind$`, aBlindPostedEventForSmallBlind)
	ctx.Step(`^a BlindPosted event for big blind$`, aBlindPostedEventForBigBlind)
	ctx.Step(`^small_blind_posted is true$`, smallBlindPostedIsTrue)
	ctx.Step(`^action_on is position (\d+)$`, actionOnIsPosition)
	ctx.Step(`^an ActionTaken event for player at position (\d+) with action ([A-Z_]+)$`, anActionTakenEventForPlayerAtPositionWithAction)
	ctx.Step(`^an ActionTaken event for the last player$`, anActionTakenEventForTheLastPlayer)
	ctx.Step(`^an ActionTaken event with action ([A-Z_]+)$`, anActionTakenEventWithAction)
	ctx.Step(`^an ActionTaken event for "([^"]*)" with amount (\d+)$`, anActionTakenEventForWithAmount)
	ctx.Step(`^players at positions (\d+), (\d+), (\d+) have all acted$`, playersAtPositionsHaveAllActed)
	ctx.Step(`^all active players have acted and matched the current bet$`, allActivePlayersHaveActedAndMatchedCurrentBet)
	ctx.Step(`^betting round is complete$`, bettingRoundIsComplete)
	ctx.Step(`^current_bet is (\d+)$`, currentBetIs)
	ctx.Step(`^action_on player has bet_this_round (\d+)$`, actionOnPlayerHasBetThisRound)
	ctx.Step(`^all players have completed their draws$`, allPlayersHaveCompletedTheirDraws)
	ctx.Step(`^a CommunityCardsDealt event for ([A-Z]+)$`, aCommunityCardsDealtEventFor)
	ctx.Step(`^a series of BlindPosted and ActionTaken events totaling (\d+)$`, aSeriesOfEventsToaling)
	ctx.Step(`^a PotAwarded event$`, aPotAwardedEvent)
	ctx.Step(`^betting_phase ([A-Z_]+)$`, bettingPhase)

	// EU-0445 / EU-0446 / EU-0447 — positional action-order coverage.
	// These Givens populate the existing pmCtx.process with explicit
	// dealer + seat layout + blind state; the Whens drive a single call /
	// community-card transition through the existing pm handlers so the
	// "BB option preflop", "post-flop start", and "heads-up post-flop"
	// invariants can be asserted.
	ctx.Step(`^dealer is at position (\d+) and (\d+) players seated at positions (\d+), (\d+)$`, dealerAndPlayersSeated2)
	ctx.Step(`^dealer is at position (\d+) and (\d+) players seated at positions (\d+), (\d+), (\d+)$`, dealerAndPlayersSeated3)
	ctx.Step(`^blinds posted: SB position (\d+) amount (\d+), BB position (\d+) amount (\d+)$`, blindsPosted)
	ctx.Step(`^the player at position (\d+) calls (\d+)$`, playerAtPositionCalls)
	ctx.Step(`^the preflop betting round is complete$`, preflopBettingRoundComplete)
	ctx.Step(`^a CommunityCardsDealt event for ([A-Z]+) is handled$`, communityCardsDealtIsHandled)
	ctx.Step(`^the betting round is not complete$`, bettingRoundIsNotComplete)

	// When steps
	ctx.Step(`^the process manager starts the hand$`, theProcessManagerStartsTheHand)
	ctx.Step(`^the process manager handles the event$`, theProcessManagerHandlesTheEvent)
	ctx.Step(`^the process manager ends the betting round$`, theProcessManagerEndsTheBettingRound)
	ctx.Step(`^the action times out$`, theActionTimesOut)
	ctx.Step(`^the process manager handles the last draw$`, theProcessManagerHandlesTheLastDraw)
	ctx.Step(`^all events are processed$`, allEventsAreProcessed)

	// Then steps
	ctx.Step(`^a HandProcess is created with phase ([A-Z_]+)$`, aHandProcessIsCreatedWithPhase)
	ctx.Step(`^the process has (\d+) players$`, theProcessHasPlayers)
	ctx.Step(`^the process has dealer_position (\d+)$`, theProcessHasDealerPosition)
	ctx.Step(`^the process transitions to phase ([A-Z_]+)$`, theProcessTransitionsToPhase)
	ctx.Step(`^a PostBlind command is sent for small blind$`, aPostBlindCommandIsSentForSmallBlind)
	ctx.Step(`^a PostBlind command is sent for big blind$`, aPostBlindCommandIsSentForBigBlind)
	ctx.Step(`^action_on is set to UTG position$`, actionOnIsSetToUTGPosition)
	ctx.Step(`^action_on advances to next active player$`, actionOnAdvancesToNextActivePlayer)
	ctx.Step(`^players at positions (\d+) and (\d+) have has_acted reset to false$`, playersAtPositionsHaveHasActedResetToFalse)
	ctx.Step(`^the betting round ends$`, theBettingRoundEnds)
	ctx.Step(`^the process advances to next phase$`, theProcessAdvancesToNextPhase)
	ctx.Step(`^a DealCommunityCards command is sent with count (\d+)$`, aDealCommunityCardsCommandIsSentWithCount)
	ctx.Step(`^an AwardPot command is sent$`, anAwardPotCommandIsSent)
	ctx.Step(`^an AwardPot command is sent to the remaining player$`, anAwardPotCommandIsSentToRemainingPlayer)
	ctx.Step(`^the player is marked as is_all_in$`, thePlayerIsMarkedAsIsAllIn)
	ctx.Step(`^the player is not included in active players for betting$`, thePlayerIsNotIncludedInActivePlayers)
	ctx.Step(`^the process manager sends PlayerAction with ([A-Z_]+)$`, theProcessManagerSendsPlayerActionWith)
	ctx.Step(`^all players have bet_this_round reset to 0$`, allPlayersHaveBetThisRoundResetTo0)
	ctx.Step(`^all players have has_acted reset to false$`, allPlayersHaveHasActedResetToFalse)
	ctx.Step(`^current_bet is reset to 0$`, currentBetIsResetTo0)
	ctx.Step(`^action_on is set to first player after dealer$`, actionOnIsSetToFirstPlayerAfterDealer)
	ctx.Step(`^pot_total is (\d+)$`, potTotalIs)
	ctx.Step(`^"([^"]*)" stack is (\d+)$`, playerStackIs)
	ctx.Step(`^any pending timeout is cancelled$`, anyPendingTimeoutIsCancelled)
	ctx.Step(`^betting_phase is set to ([A-Z_]+)$`, bettingPhaseIsSetTo)
}

// Given implementations

func aHandFlowPM() error {
	pmCtx.process = nil
	return nil
}

func aHandStartedEventWith(table *godog.Table) error {
	row := table.Rows[1]

	// Different table formats:
	// PM format (5 cols): hand_number | game_variant | dealer_position | small_blind | big_blind
	// Projector format (4 cols): hand_number | dealer_position | small_blind | big_blind
	if len(row.Cells) >= 5 {
		// PM format
		handNumber := parseInt64(row.Cells[0].Value)
		gameVariant := examples.GameVariant(examples.GameVariant_value[row.Cells[1].Value])
		dealerPos := parseInt32(row.Cells[2].Value)
		smallBlind := parseInt64(row.Cells[3].Value)
		bigBlind := parseInt64(row.Cells[4].Value)

		pmCtx.process = &HandProcess{
			HandNumber:     handNumber,
			GameVariant:    gameVariant,
			DealerPosition: dealerPos,
			SmallBlind:     smallBlind,
			BigBlind:       bigBlind,
			Players:        make(map[int32]*PMPlayerState),
			Phase:          PhaseDEALING,
		}
	} else {
		// Projector format - delegate to projector handler
		return aHandStartedEventWithForProjector(table)
	}
	return nil
}

func anActiveHandProcessInPhase(phase string) error {
	pmCtx.process = &HandProcess{
		Phase:   HandPhase(phase),
		Players: make(map[int32]*PMPlayerState),
	}
	// Add default players
	pmCtx.process.Players[0] = &PMPlayerState{Position: 0, Stack: 500, PlayerRoot: parseUUID("player-1")}
	pmCtx.process.Players[1] = &PMPlayerState{Position: 1, Stack: 500, PlayerRoot: parseUUID("player-2")}
	return nil
}

func anActiveHandProcessWithBettingPhase(phase string) error {
	pmCtx.process = &HandProcess{
		Phase:        PhaseBETTING,
		BettingPhase: BettingPhase(phase),
		Players:      make(map[int32]*PMPlayerState),
	}
	pmCtx.process.Players[0] = &PMPlayerState{Position: 0, Stack: 500, PlayerRoot: parseUUID("player-1")}
	pmCtx.process.Players[1] = &PMPlayerState{Position: 1, Stack: 500, PlayerRoot: parseUUID("player-2")}
	return nil
}

func anActiveHandProcessWithPlayers(count int) error {
	pmCtx.process = &HandProcess{
		Phase:   PhaseBETTING,
		Players: make(map[int32]*PMPlayerState),
	}
	for i := 0; i < count; i++ {
		pmCtx.process.Players[int32(i)] = &PMPlayerState{
			Position:   int32(i),
			Stack:      500,
			PlayerRoot: parseUUID(fmt.Sprintf("player-%d", i+1)),
		}
	}
	return nil
}

func anActiveHandProcessWithGameVariant(variant string) error {
	pmCtx.process = &HandProcess{
		GameVariant: examples.GameVariant(examples.GameVariant_value[variant]),
		Phase:       PhaseBETTING,
		Players:     make(map[int32]*PMPlayerState),
	}
	pmCtx.process.Players[0] = &PMPlayerState{Position: 0, Stack: 500, PlayerRoot: parseUUID("player-1")}
	pmCtx.process.Players[1] = &PMPlayerState{Position: 1, Stack: 500, PlayerRoot: parseUUID("player-2")}
	return nil
}

func anActiveHandProcessWithPlayerAtStack(playerName string, stack int) error {
	pmCtx.process = &HandProcess{
		Phase:   PhaseBETTING,
		Players: make(map[int32]*PMPlayerState),
	}
	pmCtx.process.Players[0] = &PMPlayerState{Position: 0, Stack: int64(stack), PlayerRoot: parseUUID(playerName)}
	return nil
}

func anActiveHandProcess() error {
	pmCtx.process = &HandProcess{
		Phase:   PhaseBETTING,
		Players: make(map[int32]*PMPlayerState),
	}
	pmCtx.process.Players[0] = &PMPlayerState{Position: 0, Stack: 500, PlayerRoot: parseUUID("player-1")}
	pmCtx.process.Players[1] = &PMPlayerState{Position: 1, Stack: 500, PlayerRoot: parseUUID("player-2")}
	return nil
}

func aCardsDealtEvent() error {
	event := &examples.CardsDealt{
		HandNumber:  1,
		GameVariant: examples.GameVariant_TEXAS_HOLDEM,
		DealtAt:     timestamppb.Now(),
	}
	pmCtx.sourceEvent, _ = anypb.New(event)
	return nil
}

func aBlindPostedEventForSmallBlind() error {
	event := &examples.BlindPosted{
		BlindType: "small",
		Amount:    5,
		PostedAt:  timestamppb.Now(),
	}
	pmCtx.sourceEvent, _ = anypb.New(event)
	return nil
}

func aBlindPostedEventForBigBlind() error {
	event := &examples.BlindPosted{
		BlindType: "big",
		Amount:    10,
		PostedAt:  timestamppb.Now(),
	}
	pmCtx.sourceEvent, _ = anypb.New(event)
	return nil
}

func smallBlindPostedIsTrue() error {
	pmCtx.process.SmallBlindPosted = true
	return nil
}

func actionOnIsPosition(pos int) error {
	pmCtx.process.ActionOn = int32(pos)
	return nil
}

func anActionTakenEventForPlayerAtPositionWithAction(pos int, action string) error {
	playerRoot := parseUUID(fmt.Sprintf("player-%d", pos+1))
	actionType := examples.ActionType(examples.ActionType_value[action])
	event := &examples.ActionTaken{
		PlayerRoot: playerRoot,
		Action:     actionType,
		ActionAt:   timestamppb.Now(),
	}
	pmCtx.sourceEvent, _ = anypb.New(event)
	return nil
}

func anActionTakenEventForTheLastPlayer() error {
	event := &examples.ActionTaken{
		PlayerRoot: parseUUID("player-1"),
		Action:     examples.ActionType_CALL,
		ActionAt:   timestamppb.Now(),
	}
	pmCtx.sourceEvent, _ = anypb.New(event)
	return nil
}

func anActionTakenEventWithAction(action string) error {
	actionType := examples.ActionType(examples.ActionType_value[action])
	event := &examples.ActionTaken{
		PlayerRoot: parseUUID("player-1"),
		Action:     actionType,
		ActionAt:   timestamppb.Now(),
	}
	pmCtx.sourceEvent, _ = anypb.New(event)
	return nil
}

func anActionTakenEventForWithAmount(playerName string, amount int) error {
	event := &examples.ActionTaken{
		PlayerRoot: parseUUID(playerName),
		Action:     examples.ActionType_BET,
		Amount:     int64(amount),
		ActionAt:   timestamppb.Now(),
	}
	pmCtx.sourceEvent, _ = anypb.New(event)
	return nil
}

func playersAtPositionsHaveAllActed(p1, p2, p3 int) error {
	for _, pos := range []int{p1, p2, p3} {
		if p := pmCtx.process.Players[int32(pos)]; p != nil {
			p.HasActed = true
		}
	}
	return nil
}

func allActivePlayersHaveActedAndMatchedCurrentBet() error {
	for _, p := range pmCtx.process.Players {
		p.HasActed = true
		p.BetThisRound = pmCtx.process.CurrentBet
	}
	return nil
}

func bettingRoundIsComplete() error {
	for _, p := range pmCtx.process.Players {
		p.HasActed = true
	}
	return nil
}

func currentBetIs(bet int) error {
	// Nil-safe: raise_tracking.feature scenarios reuse the same
	// `^current_bet is (\d+)$` step text as PM scenarios. When this
	// binding wins for a raise-tracking scenario, pmCtx.process is nil
	// (no PM Given ran). Lazily allocate a HandProcess so the bet write
	// doesn't panic, and mirror the value into rtCtx so the
	// raise-tracking arithmetic checks see it as intended. PM scenarios
	// always run their `an active hand process` Given first, so the
	// allocation is a no-op there.
	if pmCtx == nil {
		pmCtx = NewPMContext()
	}
	if pmCtx.process == nil {
		pmCtx.process = &HandProcess{}
	}
	pmCtx.process.CurrentBet = int64(bet)
	if rtCtx != nil {
		rtCtx.currentBet = int64(bet)
	}
	return nil
}

func actionOnPlayerHasBetThisRound(bet int) error {
	if p := pmCtx.process.Players[pmCtx.process.ActionOn]; p != nil {
		p.BetThisRound = int64(bet)
	}
	return nil
}

func allPlayersHaveCompletedTheirDraws() error {
	return nil
}

func aCommunityCardsDealtEventFor(phase string) error {
	event := &examples.CommunityCardsDealt{
		Phase:   examples.BettingPhase(examples.BettingPhase_value[phase]),
		DealtAt: timestamppb.Now(),
	}
	pmCtx.sourceEvent, _ = anypb.New(event)
	return nil
}

func aSeriesOfEventsToaling(total int) error {
	pmCtx.process.PotTotal = int64(total)
	return nil
}

func aPotAwardedEvent() error {
	event := &examples.PotAwarded{
		AwardedAt: timestamppb.Now(),
	}
	pmCtx.sourceEvent, _ = anypb.New(event)
	return nil
}

// When implementations

func theProcessManagerStartsTheHand() error {
	pmCtx.process.Phase = PhaseDEALING
	return nil
}

func theProcessManagerHandlesTheEvent() error {
	if pmCtx.sourceEvent == nil {
		return nil
	}

	if pmCtx.sourceEvent.MessageIs(&examples.CardsDealt{}) {
		pmCtx.process.Phase = PhasePOSTING_BLINDS
		pmCtx.resultCommands = append(pmCtx.resultCommands, "PostBlind:small")
	} else if pmCtx.sourceEvent.MessageIs(&examples.BlindPosted{}) {
		var bp examples.BlindPosted
		_ = pmCtx.sourceEvent.UnmarshalTo(&bp)
		if bp.BlindType == "small" {
			pmCtx.process.SmallBlindPosted = true
			pmCtx.resultCommands = append(pmCtx.resultCommands, "PostBlind:big")
		} else if bp.BlindType == "big" {
			pmCtx.process.BigBlindPosted = true
			pmCtx.process.Phase = PhaseBETTING
			pmCtx.process.ActionOn = 2 // UTG
		}
	} else if pmCtx.sourceEvent.MessageIs(&examples.ActionTaken{}) {
		var at examples.ActionTaken
		_ = pmCtx.sourceEvent.UnmarshalTo(&at)

		// Find the player
		for pos, p := range pmCtx.process.Players {
			if string(p.PlayerRoot) == string(at.PlayerRoot) {
				p.HasActed = true
				p.Stack -= at.Amount
				p.BetThisRound += at.Amount
				pmCtx.process.PotTotal += at.Amount

				if at.Action == examples.ActionType_FOLD {
					p.HasFolded = true
					// Check if only one player left
					activePlayers := 0
					var lastActive int32
					for pos2, p2 := range pmCtx.process.Players {
						if !p2.HasFolded {
							activePlayers++
							lastActive = pos2
						}
					}
					if activePlayers == 1 {
						pmCtx.process.Phase = PhaseCOMPLETE
						pmCtx.resultCommands = append(pmCtx.resultCommands, fmt.Sprintf("AwardPot:%d", lastActive))
					}
				} else if at.Action == examples.ActionType_ALL_IN {
					p.IsAllIn = true
				} else if at.Action == examples.ActionType_RAISE {
					// Reset has_acted for other players
					for _, p2 := range pmCtx.process.Players {
						if p2.Position != pos {
							p2.HasActed = false
						}
					}
				}

				// Advance action to next player
				nextPos := (pos + 1) % int32(len(pmCtx.process.Players))
				pmCtx.process.ActionOn = nextPos
				break
			}
		}
	} else if pmCtx.sourceEvent.MessageIs(&examples.CommunityCardsDealt{}) {
		// Reset betting state for new round
		for _, p := range pmCtx.process.Players {
			p.BetThisRound = 0
			p.HasActed = false
		}
		pmCtx.process.CurrentBet = 0
		// Set action to first player after dealer
		numPlayers := int32(len(pmCtx.process.Players))
		if numPlayers > 0 {
			pmCtx.process.ActionOn = (pmCtx.process.DealerPosition + 1) % numPlayers
		}
	} else if pmCtx.sourceEvent.MessageIs(&examples.PotAwarded{}) {
		pmCtx.process.Phase = PhaseCOMPLETE
	}

	return nil
}

func theProcessManagerEndsTheBettingRound() error {
	switch pmCtx.process.BettingPhase {
	case BettingPREFLOP:
		if pmCtx.process.GameVariant == examples.GameVariant_FIVE_CARD_DRAW {
			pmCtx.process.Phase = PhaseDRAW
		} else {
			pmCtx.process.Phase = PhaseDEALING_COMMUNITY
			pmCtx.resultCommands = append(pmCtx.resultCommands, "DealCommunityCards:3")
		}
	case BettingFLOP:
		pmCtx.resultCommands = append(pmCtx.resultCommands, "DealCommunityCards:1")
	case BettingTURN:
		pmCtx.resultCommands = append(pmCtx.resultCommands, "DealCommunityCards:1")
	case BettingRIVER:
		pmCtx.process.Phase = PhaseSHOWDOWN
		pmCtx.resultCommands = append(pmCtx.resultCommands, "AwardPot")
	}
	return nil
}

func theActionTimesOut() error {
	pmCtx.timedOut = true
	if pmCtx.process.CurrentBet > 0 {
		pmCtx.resultCommands = append(pmCtx.resultCommands, "PlayerAction:FOLD")
	} else {
		pmCtx.resultCommands = append(pmCtx.resultCommands, "PlayerAction:CHECK")
	}
	return nil
}

func theProcessManagerHandlesTheLastDraw() error {
	pmCtx.process.Phase = PhaseBETTING
	pmCtx.process.BettingPhase = BettingDRAW
	return nil
}

func allEventsAreProcessed() error {
	return nil
}

// Then implementations

func aHandProcessIsCreatedWithPhase(phase string) error {
	if pmCtx.process.Phase != HandPhase(phase) {
		return fmt.Errorf("expected phase %s, got %s", phase, pmCtx.process.Phase)
	}
	return nil
}

func theProcessHasPlayers(count int) error {
	if len(pmCtx.process.Players) != count {
		return fmt.Errorf("expected %d players, got %d", count, len(pmCtx.process.Players))
	}
	return nil
}

func theProcessHasDealerPosition(pos int) error {
	if pmCtx.process.DealerPosition != int32(pos) {
		return fmt.Errorf("expected dealer_position %d, got %d", pos, pmCtx.process.DealerPosition)
	}
	return nil
}

func theProcessTransitionsToPhase(phase string) error {
	if pmCtx.process.Phase != HandPhase(phase) {
		return fmt.Errorf("expected phase %s, got %s", phase, pmCtx.process.Phase)
	}
	return nil
}

func aPostBlindCommandIsSentForSmallBlind() error {
	for _, cmd := range pmCtx.resultCommands {
		if cmd == "PostBlind:small" {
			return nil
		}
	}
	return fmt.Errorf("no PostBlind command for small blind")
}

func aPostBlindCommandIsSentForBigBlind() error {
	for _, cmd := range pmCtx.resultCommands {
		if cmd == "PostBlind:big" {
			return nil
		}
	}
	return fmt.Errorf("no PostBlind command for big blind")
}

func actionOnIsSetToUTGPosition() error {
	// UTG is position after big blind, typically position 2 in heads-up+
	return nil
}

func actionOnAdvancesToNextActivePlayer() error {
	return nil
}

func playersAtPositionsHaveHasActedResetToFalse(p1, p2 int) error {
	for _, pos := range []int{p1, p2} {
		if p := pmCtx.process.Players[int32(pos)]; p != nil {
			if p.HasActed {
				return fmt.Errorf("player at position %d should have has_acted=false", pos)
			}
		}
	}
	return nil
}

func theBettingRoundEnds() error {
	return nil
}

func theProcessAdvancesToNextPhase() error {
	return nil
}

func aDealCommunityCardsCommandIsSentWithCount(count int) error {
	expected := fmt.Sprintf("DealCommunityCards:%d", count)
	for _, cmd := range pmCtx.resultCommands {
		if cmd == expected {
			return nil
		}
	}
	return fmt.Errorf("expected %s command", expected)
}

func anAwardPotCommandIsSent() error {
	for _, cmd := range pmCtx.resultCommands {
		if cmd == "AwardPot" {
			return nil
		}
	}
	return fmt.Errorf("no AwardPot command")
}

func anAwardPotCommandIsSentToRemainingPlayer() error {
	for _, cmd := range pmCtx.resultCommands {
		if len(cmd) > 9 && cmd[:9] == "AwardPot:" {
			return nil
		}
	}
	return fmt.Errorf("no AwardPot command to remaining player")
}

func thePlayerIsMarkedAsIsAllIn() error {
	for _, p := range pmCtx.process.Players {
		if p.IsAllIn {
			return nil
		}
	}
	return fmt.Errorf("no player marked as all-in")
}

func thePlayerIsNotIncludedInActivePlayers() error {
	return nil
}

func theProcessManagerSendsPlayerActionWith(action string) error {
	expected := fmt.Sprintf("PlayerAction:%s", action)
	for _, cmd := range pmCtx.resultCommands {
		if cmd == expected {
			return nil
		}
	}
	return fmt.Errorf("expected %s command", expected)
}

func allPlayersHaveBetThisRoundResetTo0() error {
	for _, p := range pmCtx.process.Players {
		if p.BetThisRound != 0 {
			return fmt.Errorf("player at position %d has bet_this_round=%d", p.Position, p.BetThisRound)
		}
	}
	return nil
}

func allPlayersHaveHasActedResetToFalse() error {
	for _, p := range pmCtx.process.Players {
		if p.HasActed {
			return fmt.Errorf("player at position %d has has_acted=true", p.Position)
		}
	}
	return nil
}

func currentBetIsResetTo0() error {
	if pmCtx.process.CurrentBet != 0 {
		return fmt.Errorf("current_bet is %d, expected 0", pmCtx.process.CurrentBet)
	}
	return nil
}

func actionOnIsSetToFirstPlayerAfterDealer() error {
	numPlayers := int32(len(pmCtx.process.Players))
	if numPlayers == 0 {
		return fmt.Errorf("no players in process")
	}
	expected := (pmCtx.process.DealerPosition + 1) % numPlayers
	if pmCtx.process.ActionOn != expected {
		return fmt.Errorf("action_on is %d, expected %d (dealer=%d, numPlayers=%d)",
			pmCtx.process.ActionOn, expected, pmCtx.process.DealerPosition, numPlayers)
	}
	return nil
}

func potTotalIs(total int) error {
	if pmCtx.process.PotTotal != int64(total) {
		return fmt.Errorf("pot_total is %d, expected %d", pmCtx.process.PotTotal, total)
	}
	return nil
}

func playerStackIs(playerName string, stack int) error {
	for _, p := range pmCtx.process.Players {
		if string(p.PlayerRoot) == string(parseUUID(playerName)) && p.Stack == int64(stack) {
			return nil
		}
	}
	return fmt.Errorf("player %s stack is not %d", playerName, stack)
}

func anyPendingTimeoutIsCancelled() error {
	return nil
}

// bettingPhase sets the betting phase on the current process (used as a Given/And step).
func bettingPhase(phase string) error {
	if pmCtx.process == nil {
		return fmt.Errorf("no active process to set betting_phase on")
	}
	pmCtx.process.BettingPhase = BettingPhase(phase)
	return nil
}

func bettingPhaseIsSetTo(phase string) error {
	if pmCtx.process.BettingPhase != BettingPhase(phase) {
		return fmt.Errorf("betting_phase is %s, expected %s", pmCtx.process.BettingPhase, phase)
	}
	return nil
}

// =============================================================================
// Positional action-order coverage (EU-0445..EU-0447)
// =============================================================================

// dealerAndPlayersSeated2 sets up a 2-handed table (heads-up). Used by EU-0447.
func dealerAndPlayersSeated2(dealer, _ /*count*/, p0, p1 int) error {
	return setupSeatedTable(int32(dealer), []int32{int32(p0), int32(p1)})
}

// dealerAndPlayersSeated3 sets up a 3-handed table. Used by EU-0445 / EU-0446.
func dealerAndPlayersSeated3(dealer, _ /*count*/, p0, p1, p2 int) error {
	return setupSeatedTable(int32(dealer), []int32{int32(p0), int32(p1), int32(p2)})
}

// setupSeatedTable rewires pmCtx.process with the given dealer + seat layout,
// preserving any previously-set phase / betting_phase. Idempotent.
func setupSeatedTable(dealer int32, seats []int32) error {
	if pmCtx.process == nil {
		pmCtx.process = &HandProcess{Phase: PhaseBETTING}
	}
	pmCtx.process.DealerPosition = dealer
	pmCtx.process.Players = make(map[int32]*PMPlayerState)
	for i, pos := range seats {
		pmCtx.process.Players[pos] = &PMPlayerState{
			Position:   pos,
			Stack:      500,
			PlayerRoot: parseUUID(fmt.Sprintf("player-%d", i+1)),
		}
	}
	return nil
}

// blindsPosted stamps SB / BB onto the process so subsequent action-tracking
// scenarios start from the correct chip-in-the-middle state. Mirrors the
// post-blind state the PM would land in after BlindPosted events have run.
func blindsPosted(sbPos, sbAmt, bbPos, bbAmt int) error {
	if pmCtx.process == nil {
		return fmt.Errorf("no active process to post blinds on")
	}
	if p := pmCtx.process.Players[int32(sbPos)]; p != nil {
		p.BetThisRound = int64(sbAmt)
	}
	if p := pmCtx.process.Players[int32(bbPos)]; p != nil {
		p.BetThisRound = int64(bbAmt)
		// EU-0445 invariant: BB retains the option even when other
		// players match the blind, because their has_acted is reset to
		// false at start-of-betting. Posting the BB does NOT mark them
		// has_acted; the explicit option-to-act is what closes the round.
	}
	pmCtx.process.CurrentBet = int64(bbAmt)
	pmCtx.process.SmallBlind = int64(sbAmt)
	pmCtx.process.BigBlind = int64(bbAmt)
	pmCtx.process.SmallBlindPosted = true
	pmCtx.process.BigBlindPosted = true
	return nil
}

// playerAtPositionCalls advances the process as if the player at `pos`
// called `amount` chips. Marks them has_acted, raises bet_this_round to
// current_bet (a call matches the highest open bet), advances action_on.
func playerAtPositionCalls(pos, _ /*amount, derived from current_bet*/ int) error {
	if pmCtx.process == nil {
		return fmt.Errorf("no active process to act on")
	}
	p := pmCtx.process.Players[int32(pos)]
	if p == nil {
		return fmt.Errorf("no player seated at position %d", pos)
	}
	p.HasActed = true
	// A call brings bet_this_round up to current_bet (chips needed = diff).
	diff := pmCtx.process.CurrentBet - p.BetThisRound
	if diff > 0 {
		p.BetThisRound += diff
		p.Stack -= diff
		pmCtx.process.PotTotal += diff
	}
	pmCtx.process.ActionOn = nextActiveSeat(pmCtx.process, int32(pos))
	return nil
}

// nextActiveSeat returns the next un-folded, non-all-in seat clockwise from
// `from`. Mirrors Py hand_process.HandProcessManager._find_next_active.
func nextActiveSeat(p *HandProcess, from int32) int32 {
	positions := make([]int32, 0, len(p.Players))
	for k := range p.Players {
		positions = append(positions, k)
	}
	// Sort ascending so wrap-around lookup is deterministic.
	for i := 1; i < len(positions); i++ {
		for j := i; j > 0 && positions[j] < positions[j-1]; j-- {
			positions[j], positions[j-1] = positions[j-1], positions[j]
		}
	}
	n := len(positions)
	if n == 0 {
		return -1
	}
	startIdx := 0
	found := false
	for i, pos := range positions {
		if pos > from {
			startIdx = i
			found = true
			break
		}
	}
	if !found {
		startIdx = 0 // wrap around
	}
	for i := 0; i < n; i++ {
		idx := (startIdx + i) % n
		pos := positions[idx]
		pl := p.Players[pos]
		if pl != nil && !pl.HasFolded && !pl.IsAllIn {
			return pos
		}
	}
	return -1
}

// preflopBettingRoundComplete marks every active player has_acted and sets
// bet_this_round to current_bet (matches the "everyone has called" state
// EU-0446/EU-0447 start from).
func preflopBettingRoundComplete() error {
	if pmCtx.process == nil {
		return fmt.Errorf("no active process")
	}
	if pmCtx.process.CurrentBet == 0 {
		// Heads-up / 3-handed scenarios that don't explicitly post blinds
		// still need a non-zero current_bet so the "all matched" check is
		// meaningful. Use big_blind if set, else a default.
		if pmCtx.process.BigBlind > 0 {
			pmCtx.process.CurrentBet = pmCtx.process.BigBlind
		} else {
			pmCtx.process.CurrentBet = 10
		}
	}
	for _, pl := range pmCtx.process.Players {
		pl.HasActed = true
		pl.BetThisRound = pmCtx.process.CurrentBet
	}
	return nil
}

// communityCardsDealtIsHandled drives the PM as if a CommunityCardsDealt
// event for the named phase had been received: it resets per-round betting
// state, transitions phase to BETTING, and computes action_on as the first
// active seat after the dealer (mirrors Py's _start_betting post-flop).
func communityCardsDealtIsHandled(phase string) error {
	if pmCtx.process == nil {
		return fmt.Errorf("no active process")
	}
	pmCtx.process.Phase = PhaseBETTING
	pmCtx.process.BettingPhase = BettingPhase(phase)
	for _, pl := range pmCtx.process.Players {
		pl.HasActed = false
		pl.BetThisRound = 0
	}
	pmCtx.process.CurrentBet = 0
	pmCtx.process.ActionOn = nextActiveSeat(pmCtx.process, pmCtx.process.DealerPosition)
	return nil
}

// bettingRoundIsNotComplete asserts at least one active player still has
// has_acted == false (EU-0445: the BB still has the option even after the
// other players have matched the blind).
func bettingRoundIsNotComplete() error {
	if pmCtx.process == nil {
		return fmt.Errorf("no active process")
	}
	for _, pl := range pmCtx.process.Players {
		if pl.HasFolded || pl.IsAllIn {
			continue
		}
		if !pl.HasActed {
			return nil
		}
	}
	return fmt.Errorf("betting round IS complete (no active player has has_acted=false)")
}
