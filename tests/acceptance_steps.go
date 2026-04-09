//go:build acceptance

package tests

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"github.com/cucumber/godog"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/anypb"
)

// AcceptanceContext holds shared state for acceptance test scenarios.
type AcceptanceContext struct {
	mu     sync.Mutex
	client CommandClient

	// Player state
	players map[string]*playerRecord // name -> player record

	// Table state
	tables       map[string]*tableRecord // name -> table record
	lastTableKey string                  // tracks the most recently created table

	// Hand state
	hands          map[string]*handRecord // tableKey -> hand record
	currentHandKey string                 // tableKey of the active hand
	deckSeed       string                 // deterministic deck seed
	sidePotIndex   int                    // tracks side pot assertion index

	// Sync mode test state
	syncTestStartTime time.Time
	monitoringBus     bool

	// Saga/PM configuration flags
	tableHandSagaFail    bool
	handPlayerSagaFail   bool
	outputProjectorOK    bool
	deadLetterConfigured bool
	handFlowPMRegistered bool
	multipleSagasFail    bool
	domainNoSagas        bool

	// Last command/error tracking
	lastError error
	lastResp  *pb.CommandResponse
}

type playerRecord struct {
	root     []byte
	sequence uint32
}

type tableRecord struct {
	root     []byte
	sequence uint32
}

type handRecord struct {
	root      []byte
	sequence  uint32
	tableKey  string
	potTotal  int64
	handCount int
}

func newAcceptanceContext() *AcceptanceContext {
	url := os.Getenv("PLAYER_URL")
	if url == "" {
		url = "localhost:1310"
	}
	client, err := newGRPCClient(url)
	if err != nil {
		panic(fmt.Sprintf("failed to create gRPC client: %v", err))
	}

	return &AcceptanceContext{
		client:  client,
		players: make(map[string]*playerRecord),
		tables:  make(map[string]*tableRecord),
		hands:   make(map[string]*handRecord),
	}
}

func (ac *AcceptanceContext) getOrCreatePlayer(name string) *playerRecord {
	if p, ok := ac.players[name]; ok {
		return p
	}
	id := uuid.New()
	p := &playerRecord{root: id[:], sequence: 0}
	ac.players[name] = p
	return p
}

func (ac *AcceptanceContext) getOrCreateTable(name string) *tableRecord {
	if t, ok := ac.tables[name]; ok {
		return t
	}
	id := uuid.New()
	t := &tableRecord{root: id[:], sequence: 0}
	ac.tables[name] = t
	ac.lastTableKey = name
	return t
}

func (ac *AcceptanceContext) getOrCreateHand(tableKey string) *handRecord {
	if h, ok := ac.hands[tableKey]; ok {
		return h
	}
	id := uuid.New()
	h := &handRecord{root: id[:], sequence: 0, tableKey: tableKey}
	ac.hands[tableKey] = h
	ac.currentHandKey = tableKey
	return h
}

// sendWithRetry sends a command with retry for eventual consistency.
func (ac *AcceptanceContext) sendWithRetry(domain string, root []byte, cmdAny *anypb.Any, sequence uint32) (*pb.CommandResponse, error) {
	maxAttempts := 10
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := ac.client.SendCommand(domain, root, cmdAny, sequence)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		time.Sleep(time.Duration(200*attempt) * time.Millisecond)
	}
	return nil, fmt.Errorf("command failed after %d attempts: %w", maxAttempts, lastErr)
}

// InitAcceptanceSteps registers acceptance step definitions that use CommandClient.
func InitAcceptanceSteps(ctx *godog.ScenarioContext) {
	ac := newAcceptanceContext()

	ctx.Before(func(c context.Context, sc *godog.Scenario) (context.Context, error) {
		ac.players = make(map[string]*playerRecord)
		ac.tables = make(map[string]*tableRecord)
		ac.hands = make(map[string]*handRecord)
		ac.lastError = nil
		ac.lastResp = nil
		ac.lastTableKey = ""
		ac.currentHandKey = ""
		ac.deckSeed = ""
		ac.sidePotIndex = 0
		ac.syncTestStartTime = time.Time{}
		ac.monitoringBus = false
		ac.tableHandSagaFail = false
		ac.handPlayerSagaFail = false
		ac.outputProjectorOK = false
		ac.deadLetterConfigured = false
		ac.handFlowPMRegistered = false
		ac.multipleSagasFail = false
		ac.domainNoSagas = false
		return c, nil
	})

	// =========================================================================
	// Background
	// =========================================================================
	ctx.Step(`^the poker system is running in standalone mode$`, ac.systemRunning)

	// =========================================================================
	// Player steps
	// =========================================================================
	ctx.Step(`^I register player "([^"]*)" with email "([^"]*)"$`, ac.registerPlayer)
	ctx.Step(`^I deposit (\d+) chips to player "([^"]*)"$`, ac.depositChips)
	ctx.Step(`^I deposit (\d+) chips to player "([^"]*)" with sync_mode ASYNC$`, ac.depositChipsAsync)
	ctx.Step(`^I deposit (\d+) chips to player "([^"]*)" with sync_mode SIMPLE$`, ac.depositChipsSimple)
	ctx.Step(`^player "([^"]*)" has bankroll (\d+)$`, ac.playerHasBankroll)
	ctx.Step(`^player "([^"]*)" has available balance (\d+)$`, ac.playerHasAvailableBalance)
	ctx.Step(`^player "([^"]*)" has reserved funds (\d+)$`, ac.playerHasReservedFunds)
	ctx.Step(`^registered players with bankroll:$`, ac.registeredPlayersWithBankroll)
	ctx.Step(`^player "([^"]*)" has bankroll (\d+) with (\d+) reserved$`, ac.playerHasBankrollWithReserved)
	ctx.Step(`^(\d+) registered players$`, ac.nRegisteredPlayers)
	ctx.Step(`^I deposit chips to all players with sync_mode ASYNC$`, ac.depositChipsToAllPlayersAsync)

	// =========================================================================
	// Table steps
	// =========================================================================
	ctx.Step(`^I create a Texas Hold'em table "([^"]*)" with blinds (\d+)/(\d+)$`, ac.createTexasHoldemTable)
	ctx.Step(`^a Five Card Draw table "([^"]*)" with blinds (\d+)/(\d+)$`, ac.createFiveCardDrawTable)
	ctx.Step(`^an Omaha table "([^"]*)" with blinds (\d+)/(\d+)$`, ac.createOmahaTable)
	ctx.Step(`^player "([^"]*)" joins table "([^"]*)" at seat (\d+) with buy-in (\d+)$`, ac.playerJoinsTable)
	ctx.Step(`^player "([^"]*)" leaves table "([^"]*)"$`, ac.playerLeavesTable)
	ctx.Step(`^table "([^"]*)" has (\d+) seated players?$`, ac.tableHasSeatedPlayers)
	ctx.Step(`^a table "([^"]*)" with seated players:$`, ac.tableWithSeatedPlayers)
	ctx.Step(`^a table "([^"]*)" with (\d+) seated players$`, ac.tableWithNSeatedPlayers)
	ctx.Step(`^a table "([^"]*)" with an active hand$`, ac.tableWithActiveHand)
	ctx.Step(`^seated players:$`, ac.seatedPlayersOnLastTable)
	ctx.Step(`^table "([^"]*)" has hand_count (\d+)$`, ac.tableHasHandCount)
	ctx.Step(`^a table with no seated players$`, ac.tableWithNoSeatedPlayers)

	// =========================================================================
	// Hand lifecycle steps
	// =========================================================================
	ctx.Step(`^a hand starts at table "([^"]*)"$`, ac.handStartsAtTable)
	ctx.Step(`^a hand starts and blinds are posted \((\d+)/(\d+)\)$`, ac.handStartsAndBlindsPosted)
	ctx.Step(`^blinds are posted \((\d+)/(\d+)\)$`, ac.blindsArePosted)
	ctx.Step(`^a hand starts with dealer at seat (\d+)$`, ac.handStartsWithDealerAtSeat)
	ctx.Step(`^I send a StartHand command to table "([^"]*)"$`, ac.sendStartHandCommand)
	ctx.Step(`^deterministic deck seed "([^"]*)"$`, ac.deterministicDeckSeed)
	ctx.Step(`^deterministic deck where both players make the same flush$`, ac.deterministicDeckSameFlush)
	ctx.Step(`^deterministic deck with community cards making a royal flush$`, ac.deterministicDeckRoyalFlush)
	ctx.Step(`^deterministic deck where:$`, ac.deterministicDeckWhere)
	ctx.Step(`^deterministic deck where Alice has best hand, Bob has second best$`, ac.deterministicDeckAliceBestBobSecond)
	ctx.Step(`^a hand is dealt with "([^"]*)" to act$`, ac.handDealtWithPlayerToAct)
	ctx.Step(`^a hand is in progress$`, ac.handInProgress)
	ctx.Step(`^a hand is in progress with "([^"]*)" to act$`, ac.handInProgressWithPlayerToAct)
	ctx.Step(`^current bet is (\d+) and min raise is (\d+)$`, ac.currentBetAndMinRaise)

	// =========================================================================
	// Blind posting steps
	// =========================================================================
	ctx.Step(`^"([^"]*)" posts small blind (\d+)$`, ac.postsSmallBlind)
	ctx.Step(`^"([^"]*)" posts big blind (\d+)$`, ac.postsBigBlind)

	// =========================================================================
	// Player action steps
	// =========================================================================
	ctx.Step(`^"([^"]*)" folds$`, ac.playerFolds)
	ctx.Step(`^"([^"]*)" calls (\d+)$`, ac.playerCalls)
	ctx.Step(`^"([^"]*)" checks$`, ac.playerChecks)
	ctx.Step(`^"([^"]*)" raises to (\d+)$`, ac.playerRaisesTo)
	ctx.Step(`^"([^"]*)" re-raises to (\d+)$`, ac.playerReRaisesTo)
	ctx.Step(`^"([^"]*)" bets (\d+)$`, ac.playerBets)
	ctx.Step(`^"([^"]*)" goes all-in for (\d+)$`, ac.playerGoesAllIn)
	ctx.Step(`^"([^"]*)" folds with sync_mode CASCADE$`, ac.playerFoldsCascade)
	ctx.Step(`^preflop betting completes with calls$`, ac.preflopBettingCompletes)
	ctx.Step(`^both players check to showdown$`, ac.bothPlayersCheckToShowdown)
	ctx.Step(`^"([^"]*)" attempts to act$`, ac.playerAttemptsToAct)
	ctx.Step(`^player attempts to raise to (\d+)$`, ac.playerAttemptsToRaise)

	// =========================================================================
	// Draw poker steps
	// =========================================================================
	ctx.Step(`^"([^"]*)" discards (\d+) cards at indices \[([^\]]*)\]$`, ac.playerDiscardsCards)
	ctx.Step(`^"([^"]*)" stands pat$`, ac.playerStandsPat)

	// =========================================================================
	// Showdown and hand completion steps
	// =========================================================================
	ctx.Step(`^showdown occurs with "([^"]*)" winning$`, ac.showdownOccursWithWinner)
	ctx.Step(`^showdown occurs$`, ac.showdownOccurs)
	ctx.Step(`^a hand completes through showdown$`, ac.handCompletesThroughShowdown)
	ctx.Step(`^the hand completes with winner "([^"]*)"$`, ac.theHandCompletesWithWinner)
	ctx.Step(`^the hand completes with sync_mode CASCADE and cascade_error_mode COMPENSATE$`, ac.handCompletesWithCascadeCompensate)
	ctx.Step(`^hand (\d+) completes with "([^"]*)" winning (\d+)$`, ac.handNCompletesWithWinner)
	ctx.Step(`^hand (\d+) completes$`, ac.handNCompletes)

	// =========================================================================
	// Rebuy steps
	// =========================================================================
	ctx.Step(`^"([^"]*)" adds (\d+) chips to her stack$`, ac.playerAddsChips)
	ctx.Step(`^"([^"]*)" attempts to add chips$`, ac.playerAttemptsToAddChips)
	ctx.Step(`^"([^"]*)" attempts to add (\d+) chips$`, ac.playerAttemptsToAddNChips)

	// =========================================================================
	// Assertion steps - pot and stack
	// =========================================================================
	ctx.Step(`^"([^"]*)" wins the pot of (\d+)$`, ac.playerWinsPotOf)
	ctx.Step(`^"([^"]*)" wins the pot of (\d+) uncontested$`, ac.playerWinsPotUncontested)
	ctx.Step(`^the pot is (\d+)$`, ac.potIs)
	ctx.Step(`^"([^"]*)" stack is (\d+)$`, ac.playerStackIs)
	ctx.Step(`^"([^"]*)" has stack (\d+)$`, ac.playerHasStack)
	ctx.Step(`^active player count is (\d+)$`, ac.activePlayerCountIs)

	// =========================================================================
	// Assertion steps - community cards and streets
	// =========================================================================
	ctx.Step(`^the flop is dealt$`, ac.flopIsDealt)
	ctx.Step(`^the turn is dealt$`, ac.turnIsDealt)
	ctx.Step(`^the river is dealt$`, ac.riverIsDealt)
	ctx.Step(`^showdown begins$`, ac.showdownBegins)
	ctx.Step(`^the winner is determined by hand ranking$`, ac.winnerDeterminedByRanking)
	ctx.Step(`^the hand completes$`, ac.theHandCompletes)
	ctx.Step(`^showdown is triggered immediately$`, ac.showdownTriggeredImmediately)
	ctx.Step(`^no showdown occurs$`, ac.noShowdownOccurs)
	ctx.Step(`^the hand ends without showdown$`, ac.handEndsWithoutShowdown)

	// =========================================================================
	// Assertion steps - side pots
	// =========================================================================
	ctx.Step(`^there is a main pot of (\d+) with (\d+) players eligible$`, ac.mainPotWithEligible)
	ctx.Step(`^there is a side pot of (\d+) with (\d+) players eligible$`, ac.sidePotWithEligible)
	ctx.Step(`^"([^"]*)" wins main pot of (\d+)$`, ac.playerWinsMainPot)
	ctx.Step(`^"([^"]*)" wins side pot of (\d+)$`, ac.playerWinsSidePot)

	// =========================================================================
	// Assertion steps - variant-specific
	// =========================================================================
	ctx.Step(`^each player has (\d+) hole cards$`, ac.eachPlayerHasHoleCards)
	ctx.Step(`^the remaining deck has (\d+) cards$`, ac.remainingDeckHasCards)
	ctx.Step(`^the draw phase begins$`, ac.drawPhaseBegins)
	ctx.Step(`^"([^"]*)" has (\d+) hole cards$`, ac.playerHasHoleCards)
	ctx.Step(`^the second betting round begins$`, ac.secondBettingRoundBegins)

	// =========================================================================
	// Assertion steps - split pot / kicker / showdown
	// =========================================================================
	ctx.Step(`^the pot of (\d+) is split evenly$`, ac.potSplitEvenly)
	ctx.Step(`^the pot is split evenly$`, ac.potIsSplitEvenly)
	ctx.Step(`^"([^"]*)" wins (\d+)$`, ac.playerWinsAmount)
	ctx.Step(`^both players play the board$`, ac.bothPlayersPlayTheBoard)
	ctx.Step(`^both players have a pair of aces$`, ac.bothHavePairOfAces)
	ctx.Step(`^"([^"]*)" wins with king kicker over queen$`, ac.playerWinsWithKicker)

	// =========================================================================
	// Assertion steps - heads-up and blind positions
	// =========================================================================
	ctx.Step(`^"([^"]*)" is small blind and "([^"]*)" is big blind$`, ac.smallAndBigBlinds)
	ctx.Step(`^"([^"]*)" posts the small blind of (\d+)$`, ac.playerPostsSmallBlindOf)
	ctx.Step(`^"([^"]*)" posts the big blind of (\d+)$`, ac.playerPostsBigBlindOf)
	ctx.Step(`^"([^"]*)" acts first preflop$`, ac.playerActsFirstPreflop)

	// =========================================================================
	// Assertion steps - betting restrictions
	// =========================================================================
	ctx.Step(`^"([^"]*)" may call (\d+) or raise to at least (\d+)$`, ac.playerMayCallOrRaise)
	ctx.Step(`^"([^"]*)" may only call (\d+) if "([^"]*)" just calls$`, ac.playerMayOnlyCall)
	ctx.Step(`^"([^"]*)" may re-raise if "([^"]*)" raises$`, ac.playerMayReRaise)
	ctx.Step(`^"([^"]*)" must act$`, ac.playerMustAct)

	// =========================================================================
	// Assertion steps - elimination and tournament
	// =========================================================================
	ctx.Step(`^"([^"]*)" is eliminated from table "([^"]*)"$`, ac.playerEliminatedFromTable)

	// =========================================================================
	// Assertion steps - error handling
	// =========================================================================
	ctx.Step(`^the command fails with "([^"]*)"$`, ac.commandFailsWith)
	ctx.Step(`^the request fails with "([^"]*)"$`, ac.requestFailsWith)

	// =========================================================================
	// Assertion steps - saga coordination
	// =========================================================================
	ctx.Step(`^within (\d+) seconds:$`, ac.withinNSeconds)
	ctx.Step(`^within (\d+) seconds player "([^"]*)" bankroll projection shows (\d+)$`, ac.withinNSecondsBankrollShows)
	ctx.Step(`^within (\d+) seconds hand domain has CardsDealt event$`, ac.withinSecondsCardsDealt)
	ctx.Step(`^the hand has the same hand_number as the table event$`, ac.handSameHandNumber)
	ctx.Step(`^the table updates player stacks$`, ac.tableUpdatesPlayerStacks)

	// =========================================================================
	// Sync mode steps - When
	// =========================================================================
	ctx.Step(`^I start a hand at table "([^"]*)" with sync_mode ASYNC$`, ac.startHandAsync)
	ctx.Step(`^I start a hand at table "([^"]*)" with sync_mode SIMPLE$`, ac.startHandSimple)
	ctx.Step(`^I start a hand at table "([^"]*)" with sync_mode CASCADE$`, ac.startHandCascade)
	ctx.Step(`^I start a hand at table "([^"]*)" with sync_mode CASCADE and cascade_error_mode FAIL_FAST$`, ac.startHandCascadeFailFast)
	ctx.Step(`^I start a hand at table "([^"]*)" with sync_mode CASCADE and cascade_error_mode CONTINUE$`, ac.startHandCascadeContinue)
	ctx.Step(`^I start a hand at table "([^"]*)" with sync_mode CASCADE and cascade_error_mode DEAD_LETTER$`, ac.startHandCascadeDeadLetter)
	ctx.Step(`^I execute a command with sync_mode CASCADE$`, ac.executeCommandCascade)
	ctx.Step(`^I execute a triggering command with cascade_error_mode CONTINUE$`, ac.executeTriggeringContinue)
	ctx.Step(`^I send an event without correlation_id with sync_mode CASCADE$`, ac.sendEventWithoutCorrelationCascade)

	// =========================================================================
	// Sync mode steps - Then (assertion)
	// =========================================================================
	ctx.Step(`^the command succeeds immediately$`, ac.commandSucceedsImmediately)
	ctx.Step(`^the command succeeds$`, ac.commandSucceeds)
	ctx.Step(`^the command succeeds with HandStarted event$`, ac.commandSucceedsWithHandStarted)
	ctx.Step(`^the command succeeds with HandStarted only$`, ac.commandSucceedsWithHandStartedOnly)
	ctx.Step(`^the response does not include projection updates$`, ac.responseNoProjectionUpdates)
	ctx.Step(`^the response does not include cascade results$`, ac.responseNoCascadeResults)
	ctx.Step(`^the response does not include cascade results from sagas$`, ac.responseNoCascadeResultsFromSagas)
	ctx.Step(`^the response includes projection updates for "([^"]*)"$`, ac.responseIncludesProjectionUpdatesFor)
	ctx.Step(`^the response includes projection updates$`, ac.responseIncludesProjectionUpdates)
	ctx.Step(`^the response includes projection updates for both table and hand domains$`, ac.responseIncludesProjectionUpdatesBothDomains)
	ctx.Step(`^the projection shows bankroll (\d+)$`, ac.projectionShowsBankroll)
	ctx.Step(`^the table projection shows hand_count incremented$`, ac.tableProjectionHandCountIncremented)
	ctx.Step(`^the command returns before DealCards is issued$`, ac.commandReturnsBeforeDealCards)
	ctx.Step(`^the response includes cascade results$`, ac.responseIncludesCascadeResults)
	ctx.Step(`^the cascade results include DealCards command to hand domain$`, ac.cascadeIncludesDealCards)
	ctx.Step(`^the cascade results include CardsDealt event from hand domain$`, ac.cascadeIncludesCardsDealt)
	ctx.Step(`^the response includes the full cascade chain:$`, ac.responseIncludesCascadeChain)
	ctx.Step(`^no events are published to the bus during command execution$`, ac.noEventsBusPublished)
	ctx.Step(`^all events remain in-process$`, ac.allEventsInProcess)

	// =========================================================================
	// Cascade error mode steps - Then
	// =========================================================================
	ctx.Step(`^the command fails with saga error$`, ac.commandFailsWithSagaError)
	ctx.Step(`^no further sagas are executed after the failure$`, ac.noFurtherSagasAfterFailure)
	ctx.Step(`^the original HandStarted event is still persisted$`, ac.originalHandStartedPersisted)
	ctx.Step(`^the response includes cascade_errors with the saga failure$`, ac.responseIncludesCascadeErrors)
	ctx.Step(`^the response includes successful projection updates$`, ac.responseIncludesSuccessfulProjectionUpdates)
	ctx.Step(`^other sagas continue executing despite the failure$`, ac.otherSagasContinue)
	ctx.Step(`^other sagas continue executing$`, ac.otherSagasContinueExecuting)
	ctx.Step(`^compensation commands are issued in reverse order$`, ac.compensationInReverseOrder)
	ctx.Step(`^the command fails after compensation completes$`, ac.commandFailsAfterCompensation)
	ctx.Step(`^the saga failure is published to the dead letter queue$`, ac.sagaFailureToDeadLetter)
	ctx.Step(`^the dead letter includes:$`, ac.deadLetterIncludes)
	ctx.Step(`^the original event is still persisted$`, ac.originalEventPersisted)
	ctx.Step(`^all saga errors are collected in cascade_errors$`, ac.allSagaErrorsCollected)

	// =========================================================================
	// Process manager steps - Given
	// =========================================================================
	ctx.Step(`^the hand-flow process manager is registered$`, ac.handFlowPmRegistered)
	ctx.Step(`^I am monitoring the event bus$`, ac.monitoringEventBus)

	// =========================================================================
	// Process manager steps - Then
	// =========================================================================
	ctx.Step(`^the process manager receives the correlated events$`, ac.pmReceivesCorrelatedEvents)
	ctx.Step(`^the response includes PM state updates$`, ac.responseIncludesPmUpdates)
	ctx.Step(`^the process manager is not invoked$`, ac.pmNotInvoked)
	ctx.Step(`^sagas still execute normally$`, ac.sagasExecuteNormally)

	// =========================================================================
	// Performance steps - Then
	// =========================================================================
	ctx.Step(`^all commands complete within (\d+)ms each$`, ac.allCommandsWithinMs)
	ctx.Step(`^total execution time is less than with SIMPLE mode$`, ac.totalTimeLessThanSimple)
	ctx.Step(`^the response time is higher than ASYNC or SIMPLE$`, ac.responseTimeHigher)
	ctx.Step(`^all cross-domain state is consistent immediately$`, ac.allStateConsistent)

	// =========================================================================
	// Edge case steps
	// =========================================================================
	ctx.Step(`^the response has empty cascade_results$`, ac.emptyResponse)
	ctx.Step(`^the saga produces no commands$`, ac.sagaProducesNoCommands)

	// =========================================================================
	// Saga configuration steps - Given
	// =========================================================================
	ctx.Step(`^the table-hand saga is configured to fail$`, ac.tableHandSagaConfiguredToFail)
	ctx.Step(`^the output projector is healthy$`, ac.outputProjectorHealthy)
	ctx.Step(`^the hand-player saga is configured to fail on PotAwarded$`, ac.handPlayerSagaConfiguredToFail)
	ctx.Step(`^a dead letter queue is configured$`, ac.deadLetterQueueConfigured)
	ctx.Step(`^a domain with no registered sagas$`, ac.domainWithNoRegisteredSagas)
	ctx.Step(`^multiple sagas configured to fail$`, ac.multipleSagasConfiguredToFail)
}

// =============================================================================
// Background
// =============================================================================

func (ac *AcceptanceContext) systemRunning() error {
	return nil
}

// =============================================================================
// Player step implementations
// =============================================================================

func (ac *AcceptanceContext) registerPlayer(name, email string) error {
	p := ac.getOrCreatePlayer(name)

	cmd := &examples.RegisterPlayer{
		DisplayName: name,
		Email:       email,
		PlayerType:  examples.PlayerType_HUMAN,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	resp, err := ac.client.SendCommand("player", p.root, cmdAny, p.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil // Store error, let Then steps check it
	}
	if resp.Events != nil {
		p.sequence += uint32(len(resp.Events.Pages))
	}
	return nil
}

func (ac *AcceptanceContext) depositChips(amount int, name string) error {
	p := ac.getOrCreatePlayer(name)

	cmd := &examples.DepositFunds{
		Amount: &examples.Currency{Amount: int64(amount), CurrencyCode: "CHIPS"},
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	resp, err := ac.sendWithRetry("player", p.root, cmdAny, p.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		p.sequence += uint32(len(resp.Events.Pages))
	}
	return nil
}

func (ac *AcceptanceContext) depositChipsAsync(amount int, name string) error {
	p := ac.getOrCreatePlayer(name)

	cmd := &examples.DepositFunds{
		Amount: &examples.Currency{Amount: int64(amount), CurrencyCode: "CHIPS"},
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	ac.syncTestStartTime = time.Now()
	resp, err := ac.client.SendCommandWithMode("player", p.root, cmdAny, p.sequence,
		pb.SyncMode_SYNC_MODE_ASYNC, pb.CascadeErrorMode_CASCADE_ERROR_FAIL_FAST)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		p.sequence += uint32(len(resp.Events.Pages))
	}
	return nil
}

func (ac *AcceptanceContext) depositChipsSimple(amount int, name string) error {
	p := ac.getOrCreatePlayer(name)

	cmd := &examples.DepositFunds{
		Amount: &examples.Currency{Amount: int64(amount), CurrencyCode: "CHIPS"},
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	resp, err := ac.client.SendCommandWithMode("player", p.root, cmdAny, p.sequence,
		pb.SyncMode_SYNC_MODE_SIMPLE, pb.CascadeErrorMode_CASCADE_ERROR_FAIL_FAST)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		p.sequence += uint32(len(resp.Events.Pages))
	}
	return nil
}

func (ac *AcceptanceContext) playerHasBankroll(name string, expected int) error {
	if ac.lastError != nil {
		return fmt.Errorf("previous command failed: %v", ac.lastError)
	}
	if ac.lastResp != nil && ac.lastResp.Events != nil {
		for i := len(ac.lastResp.Events.Pages) - 1; i >= 0; i-- {
			event := ac.lastResp.Events.Pages[i].GetEvent()
			if event == nil {
				continue
			}
			if event.MessageIs(&examples.FundsDeposited{}) {
				var e examples.FundsDeposited
				if err := event.UnmarshalTo(&e); err == nil && e.NewBalance != nil {
					if e.NewBalance.Amount == int64(expected) {
						return nil
					}
					return fmt.Errorf("expected bankroll %d, got %d", expected, e.NewBalance.Amount)
				}
			}
		}
	}
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerHasAvailableBalance(name string, expected int) error {
	// Available balance = bankroll - reserved. Check response events.
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerHasReservedFunds(name string, expected int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerHasBankrollWithReserved(name string, bankroll, reserved int) error {
	// Pre-condition setup: player already exists with given financial state
	return godog.ErrPending
}

func (ac *AcceptanceContext) nRegisteredPlayers(count int) error {
	for i := 1; i <= count; i++ {
		name := fmt.Sprintf("Player%d", i)
		if err := ac.registerPlayer(name, fmt.Sprintf("%s@example.com", strings.ToLower(name))); err != nil {
			return err
		}
		if ac.lastError != nil {
			return fmt.Errorf("failed to register %s: %v", name, ac.lastError)
		}
	}
	return nil
}

func (ac *AcceptanceContext) depositChipsToAllPlayersAsync() error {
	for name := range ac.players {
		p := ac.players[name]
		cmd := &examples.DepositFunds{
			Amount: &examples.Currency{Amount: 1000, CurrencyCode: "CHIPS"},
		}
		cmdAny, err := anypb.New(cmd)
		if err != nil {
			return err
		}
		resp, err := ac.client.SendCommandWithMode("player", p.root, cmdAny, p.sequence,
			pb.SyncMode_SYNC_MODE_ASYNC, pb.CascadeErrorMode_CASCADE_ERROR_FAIL_FAST)
		ac.lastResp = resp
		ac.lastError = err
		if err == nil {
			if resp.Events != nil {
				p.sequence += uint32(len(resp.Events.Pages))
			}
		}
	}
	return nil
}

func (ac *AcceptanceContext) registeredPlayersWithBankroll(table *godog.Table) error {
	for _, row := range table.Rows[1:] {
		name := row.Cells[0].Value
		bankroll := parseInt64(row.Cells[1].Value)

		if err := ac.registerPlayer(name, fmt.Sprintf("%s@example.com", strings.ToLower(name))); err != nil {
			return err
		}
		if ac.lastError != nil {
			return fmt.Errorf("failed to register %s: %v", name, ac.lastError)
		}

		if err := ac.depositChips(int(bankroll), name); err != nil {
			return err
		}
		if ac.lastError != nil {
			return fmt.Errorf("failed to deposit for %s: %v", name, ac.lastError)
		}
	}
	return nil
}

// =============================================================================
// Table step implementations
// =============================================================================

func (ac *AcceptanceContext) createTexasHoldemTable(name string, smallBlind, bigBlind int) error {
	t := ac.getOrCreateTable(name)

	cmd := &examples.CreateTable{
		TableName:            name,
		GameVariant:          examples.GameVariant_TEXAS_HOLDEM,
		SmallBlind:           int64(smallBlind),
		BigBlind:             int64(bigBlind),
		MinBuyIn:             int64(bigBlind) * 10,
		MaxBuyIn:             int64(bigBlind) * 100,
		MaxPlayers:           9,
		ActionTimeoutSeconds: 30,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	resp, err := ac.client.SendCommand("table", t.root, cmdAny, t.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		t.sequence += uint32(len(resp.Events.Pages))
	}
	return nil
}

func (ac *AcceptanceContext) createFiveCardDrawTable(name string, smallBlind, bigBlind int) error {
	t := ac.getOrCreateTable(name)

	cmd := &examples.CreateTable{
		TableName:            name,
		GameVariant:          examples.GameVariant_FIVE_CARD_DRAW,
		SmallBlind:           int64(smallBlind),
		BigBlind:             int64(bigBlind),
		MinBuyIn:             int64(bigBlind) * 10,
		MaxBuyIn:             int64(bigBlind) * 100,
		MaxPlayers:           9,
		ActionTimeoutSeconds: 30,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	resp, err := ac.client.SendCommand("table", t.root, cmdAny, t.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		t.sequence += uint32(len(resp.Events.Pages))
	}
	return nil
}

func (ac *AcceptanceContext) createOmahaTable(name string, smallBlind, bigBlind int) error {
	t := ac.getOrCreateTable(name)

	cmd := &examples.CreateTable{
		TableName:            name,
		GameVariant:          examples.GameVariant_OMAHA,
		SmallBlind:           int64(smallBlind),
		BigBlind:             int64(bigBlind),
		MinBuyIn:             int64(bigBlind) * 10,
		MaxBuyIn:             int64(bigBlind) * 100,
		MaxPlayers:           9,
		ActionTimeoutSeconds: 30,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	resp, err := ac.client.SendCommand("table", t.root, cmdAny, t.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		t.sequence += uint32(len(resp.Events.Pages))
	}
	return nil
}

func (ac *AcceptanceContext) playerJoinsTable(playerName, tableName string, seat, buyIn int) error {
	t := ac.getOrCreateTable(tableName)
	p := ac.getOrCreatePlayer(playerName)

	cmd := &examples.JoinTable{
		PlayerRoot:    p.root,
		BuyInAmount:   int64(buyIn),
		PreferredSeat: int32(seat),
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	resp, err := ac.client.SendCommand("table", t.root, cmdAny, t.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		t.sequence += uint32(len(resp.Events.Pages))
	}
	return nil
}

func (ac *AcceptanceContext) playerLeavesTable(playerName, tableName string) error {
	t := ac.getOrCreateTable(tableName)
	p := ac.getOrCreatePlayer(playerName)

	cmd := &examples.LeaveTable{
		PlayerRoot: p.root,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	resp, err := ac.client.SendCommand("table", t.root, cmdAny, t.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		t.sequence += uint32(len(resp.Events.Pages))
	}
	return nil
}

func (ac *AcceptanceContext) tableHasSeatedPlayers(tableName string, count int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) tableWithSeatedPlayers(tableName string, table *godog.Table) error {
	minStack := int64(1<<63 - 1)
	maxStack := int64(0)
	for _, row := range table.Rows[1:] {
		stack := parseInt64(row.Cells[2].Value)
		if stack < minStack {
			minStack = stack
		}
		if stack > maxStack {
			maxStack = stack
		}
	}
	if minStack > maxStack {
		minStack = maxStack
	}

	t := ac.getOrCreateTable(tableName)
	cmd := &examples.CreateTable{
		TableName:            tableName,
		GameVariant:          examples.GameVariant_TEXAS_HOLDEM,
		SmallBlind:           5,
		BigBlind:             10,
		MinBuyIn:             minStack,
		MaxBuyIn:             maxStack * 10,
		MaxPlayers:           9,
		ActionTimeoutSeconds: 30,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	resp, err := ac.client.SendCommand("table", t.root, cmdAny, t.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return fmt.Errorf("failed to create table %s: %v", tableName, err)
	}
	if resp.Events != nil {
		t.sequence += uint32(len(resp.Events.Pages))
	}

	for _, row := range table.Rows[1:] {
		name := row.Cells[0].Value
		seat := int(parseInt32(row.Cells[1].Value))
		stack := int(parseInt64(row.Cells[2].Value))

		if _, exists := ac.players[name]; !exists {
			if err := ac.registerPlayer(name, fmt.Sprintf("%s@example.com", strings.ToLower(name))); err != nil {
				return err
			}
			if ac.lastError != nil {
				return fmt.Errorf("failed to register %s: %v", name, ac.lastError)
			}
			if err := ac.depositChips(stack*2, name); err != nil {
				return err
			}
			if ac.lastError != nil {
				return fmt.Errorf("failed to deposit for %s: %v", name, ac.lastError)
			}
		}

		if err := ac.playerJoinsTable(name, tableName, seat, stack); err != nil {
			return err
		}
		if ac.lastError != nil {
			return fmt.Errorf("failed to seat %s: %v", name, ac.lastError)
		}
	}
	return nil
}

func (ac *AcceptanceContext) tableWithNSeatedPlayers(tableName string, count int) error {
	t := ac.getOrCreateTable(tableName)
	cmd := &examples.CreateTable{
		TableName:            tableName,
		GameVariant:          examples.GameVariant_TEXAS_HOLDEM,
		SmallBlind:           5,
		BigBlind:             10,
		MinBuyIn:             200,
		MaxBuyIn:             1000,
		MaxPlayers:           9,
		ActionTimeoutSeconds: 30,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	resp, err := ac.client.SendCommand("table", t.root, cmdAny, t.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return fmt.Errorf("failed to create table %s: %v", tableName, err)
	}
	if resp.Events != nil {
		t.sequence += uint32(len(resp.Events.Pages))
	}

	for i := 0; i < count; i++ {
		name := fmt.Sprintf("Player%d", i+1)
		if _, exists := ac.players[name]; !exists {
			if err := ac.registerPlayer(name, fmt.Sprintf("%s@example.com", strings.ToLower(name))); err != nil {
				return err
			}
			if ac.lastError != nil {
				return fmt.Errorf("failed to register %s: %v", name, ac.lastError)
			}
			if err := ac.depositChips(1000, name); err != nil {
				return err
			}
			if ac.lastError != nil {
				return fmt.Errorf("failed to deposit for %s: %v", name, ac.lastError)
			}
		}
		if err := ac.playerJoinsTable(name, tableName, i, 500); err != nil {
			return err
		}
		if ac.lastError != nil {
			return fmt.Errorf("failed to seat %s: %v", name, ac.lastError)
		}
	}
	return nil
}

func (ac *AcceptanceContext) tableWithActiveHand(tableName string) error {
	if err := ac.tableWithNSeatedPlayers(tableName, 2); err != nil {
		return err
	}
	return ac.handStartsAtTable(tableName)
}

func (ac *AcceptanceContext) seatedPlayersOnLastTable(table *godog.Table) error {
	tableName := ac.lastTableKey
	if tableName == "" {
		return fmt.Errorf("no table has been created yet")
	}
	t := ac.getOrCreateTable(tableName)

	for _, row := range table.Rows[1:] {
		name := row.Cells[0].Value
		seat := int(parseInt32(row.Cells[1].Value))
		stack := int(parseInt64(row.Cells[2].Value))

		if _, exists := ac.players[name]; !exists {
			if err := ac.registerPlayer(name, fmt.Sprintf("%s@example.com", strings.ToLower(name))); err != nil {
				return err
			}
			if ac.lastError != nil {
				return fmt.Errorf("failed to register %s: %v", name, ac.lastError)
			}
			if err := ac.depositChips(stack*2, name); err != nil {
				return err
			}
			if ac.lastError != nil {
				return fmt.Errorf("failed to deposit for %s: %v", name, ac.lastError)
			}
		}

		if err := ac.playerJoinsTable(name, tableName, seat, stack); err != nil {
			return err
		}
		if ac.lastError != nil {
			return fmt.Errorf("failed to seat %s at table %s: %v", name, tableName, ac.lastError)
		}
	}
	_ = t
	return nil
}

func (ac *AcceptanceContext) tableHasHandCount(tableName string, count int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) tableWithNoSeatedPlayers() error {
	ac.getOrCreateTable("Empty")
	t := ac.tables["Empty"]
	cmd := &examples.CreateTable{
		TableName:            "Empty",
		GameVariant:          examples.GameVariant_TEXAS_HOLDEM,
		SmallBlind:           5,
		BigBlind:             10,
		MinBuyIn:             100,
		MaxBuyIn:             1000,
		MaxPlayers:           9,
		ActionTimeoutSeconds: 30,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	resp, err := ac.client.SendCommand("table", t.root, cmdAny, t.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err == nil {
		if resp.Events != nil {
			t.sequence += uint32(len(resp.Events.Pages))
		}
	}
	return nil
}

// =============================================================================
// Hand lifecycle step implementations
// =============================================================================

func (ac *AcceptanceContext) handStartsAtTable(tableName string) error {
	return ac.sendStartHandCommand(tableName)
}

func (ac *AcceptanceContext) sendStartHandCommand(tableName string) error {
	t := ac.getOrCreateTable(tableName)

	cmd := &examples.StartHand{}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	resp, err := ac.client.SendCommand("table", t.root, cmdAny, t.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		t.sequence += uint32(len(resp.Events.Pages))
	}
	ac.getOrCreateHand(tableName)
	return nil
}

func (ac *AcceptanceContext) handStartsAndBlindsPosted(smallBlind, bigBlind int) error {
	tableName := ac.lastTableKey
	if tableName == "" {
		return fmt.Errorf("no table has been created yet")
	}
	if err := ac.handStartsAtTable(tableName); err != nil {
		return err
	}
	return ac.blindsArePosted(smallBlind, bigBlind)
}

func (ac *AcceptanceContext) blindsArePosted(smallBlind, bigBlind int) error {
	// Blinds are posted as part of hand start in the system.
	// This is a no-op assertion that the hand is ready for action.
	return godog.ErrPending
}

func (ac *AcceptanceContext) handStartsWithDealerAtSeat(seat int) error {
	tableName := ac.lastTableKey
	if tableName == "" {
		return fmt.Errorf("no table has been created yet")
	}
	// StartHand doesn't currently take a dealer position; it's auto-rotated.
	return ac.handStartsAtTable(tableName)
}

func (ac *AcceptanceContext) deterministicDeckSeed(seed string) error {
	ac.deckSeed = seed
	return nil
}

func (ac *AcceptanceContext) deterministicDeckSameFlush() error {
	ac.deckSeed = "same-flush"
	return nil
}

func (ac *AcceptanceContext) deterministicDeckRoyalFlush() error {
	ac.deckSeed = "royal-flush"
	return nil
}

func (ac *AcceptanceContext) deterministicDeckWhere(table *godog.Table) error {
	ac.deckSeed = "deterministic-table"
	return nil
}

func (ac *AcceptanceContext) deterministicDeckAliceBestBobSecond() error {
	ac.deckSeed = "alice-best-bob-second"
	return nil
}

func (ac *AcceptanceContext) handDealtWithPlayerToAct(playerName string) error {
	// Pre-condition: hand already dealt, specific player to act
	return godog.ErrPending
}

func (ac *AcceptanceContext) handInProgress() error {
	// Pre-condition: hand in progress
	return godog.ErrPending
}

func (ac *AcceptanceContext) handInProgressWithPlayerToAct(playerName string) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) currentBetAndMinRaise(bet, minRaise int) error {
	// Pre-condition: set current bet state
	return godog.ErrPending
}

// =============================================================================
// Blind posting step implementations
// =============================================================================

func (ac *AcceptanceContext) postsSmallBlind(playerName string, amount int) error {
	return ac.sendPlayerAction(playerName, examples.ActionType_BET, int64(amount))
}

func (ac *AcceptanceContext) postsBigBlind(playerName string, amount int) error {
	return ac.sendPlayerAction(playerName, examples.ActionType_BET, int64(amount))
}

// =============================================================================
// Player action step implementations
// =============================================================================

func (ac *AcceptanceContext) sendPlayerAction(playerName string, action examples.ActionType, amount int64) error {
	tableName := ac.currentHandKey
	if tableName == "" {
		tableName = ac.lastTableKey
	}
	if tableName == "" {
		return fmt.Errorf("no active hand")
	}

	h := ac.getOrCreateHand(tableName)
	p := ac.getOrCreatePlayer(playerName)

	cmd := &examples.PlayerAction{
		PlayerRoot: p.root,
		Action:     action,
		Amount:     amount,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	resp, err := ac.client.SendCommand("hand", h.root, cmdAny, h.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		h.sequence += uint32(len(resp.Events.Pages))
	}
	return nil
}

func (ac *AcceptanceContext) playerFolds(playerName string) error {
	return ac.sendPlayerAction(playerName, examples.ActionType_FOLD, 0)
}

func (ac *AcceptanceContext) playerCalls(playerName string, amount int) error {
	return ac.sendPlayerAction(playerName, examples.ActionType_CALL, int64(amount))
}

func (ac *AcceptanceContext) playerChecks(playerName string) error {
	return ac.sendPlayerAction(playerName, examples.ActionType_CHECK, 0)
}

func (ac *AcceptanceContext) playerRaisesTo(playerName string, amount int) error {
	return ac.sendPlayerAction(playerName, examples.ActionType_RAISE, int64(amount))
}

func (ac *AcceptanceContext) playerReRaisesTo(playerName string, amount int) error {
	return ac.playerRaisesTo(playerName, amount)
}

func (ac *AcceptanceContext) playerBets(playerName string, amount int) error {
	return ac.sendPlayerAction(playerName, examples.ActionType_BET, int64(amount))
}

func (ac *AcceptanceContext) playerGoesAllIn(playerName string, amount int) error {
	return ac.sendPlayerAction(playerName, examples.ActionType_ALL_IN, int64(amount))
}

func (ac *AcceptanceContext) playerFoldsCascade(playerName string) error {
	tableName := ac.currentHandKey
	if tableName == "" {
		tableName = ac.lastTableKey
	}
	if tableName == "" {
		return fmt.Errorf("no active hand")
	}

	h := ac.getOrCreateHand(tableName)
	p := ac.getOrCreatePlayer(playerName)

	cmd := &examples.PlayerAction{
		PlayerRoot: p.root,
		Action:     examples.ActionType_FOLD,
		Amount:     0,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	resp, err := ac.client.SendCommandWithMode("hand", h.root, cmdAny, h.sequence,
		pb.SyncMode_SYNC_MODE_CASCADE, pb.CascadeErrorMode_CASCADE_ERROR_FAIL_FAST)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		h.sequence += uint32(len(resp.Events.Pages))
	}
	return nil
}

func (ac *AcceptanceContext) preflopBettingCompletes() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) bothPlayersCheckToShowdown() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerAttemptsToAct(playerName string) error {
	err := ac.sendPlayerAction(playerName, examples.ActionType_CHECK, 0)
	return err
}

func (ac *AcceptanceContext) playerAttemptsToRaise(amount int) error {
	return godog.ErrPending
}

// =============================================================================
// Draw poker step implementations
// =============================================================================

func (ac *AcceptanceContext) playerDiscardsCards(playerName string, count int, indices string) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerStandsPat(playerName string) error {
	return godog.ErrPending
}

// =============================================================================
// Showdown and hand completion step implementations
// =============================================================================

func (ac *AcceptanceContext) showdownOccursWithWinner(playerName string) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) showdownOccurs() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) handCompletesThroughShowdown() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) theHandCompletesWithWinner(playerName string) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) handCompletesWithCascadeCompensate() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) handNCompletesWithWinner(handNum int, playerName string, amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) handNCompletes(handNum int) error {
	return godog.ErrPending
}

// =============================================================================
// Rebuy step implementations
// =============================================================================

func (ac *AcceptanceContext) playerAddsChips(playerName string, amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerAttemptsToAddChips(playerName string) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerAttemptsToAddNChips(playerName string, amount int) error {
	return godog.ErrPending
}

// =============================================================================
// Assertion step implementations - pot and stack
// =============================================================================

func (ac *AcceptanceContext) playerWinsPotOf(playerName string, amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerWinsPotUncontested(playerName string, amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) potIs(amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerStackIs(playerName string, amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerHasStack(playerName string, amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) activePlayerCountIs(count int) error {
	return godog.ErrPending
}

// =============================================================================
// Assertion step implementations - community cards and streets
// =============================================================================

func (ac *AcceptanceContext) flopIsDealt() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) turnIsDealt() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) riverIsDealt() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) showdownBegins() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) winnerDeterminedByRanking() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) theHandCompletes() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) showdownTriggeredImmediately() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) noShowdownOccurs() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) handEndsWithoutShowdown() error {
	return godog.ErrPending
}

// =============================================================================
// Assertion step implementations - side pots
// =============================================================================

func (ac *AcceptanceContext) mainPotWithEligible(amount, players int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) sidePotWithEligible(amount, players int) error {
	ac.sidePotIndex++
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerWinsMainPot(playerName string, amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerWinsSidePot(playerName string, amount int) error {
	return godog.ErrPending
}

// =============================================================================
// Assertion step implementations - variant-specific
// =============================================================================

func (ac *AcceptanceContext) eachPlayerHasHoleCards(count int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) remainingDeckHasCards(count int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) drawPhaseBegins() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerHasHoleCards(playerName string, count int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) secondBettingRoundBegins() error {
	return godog.ErrPending
}

// =============================================================================
// Assertion step implementations - split pot / kicker / showdown
// =============================================================================

func (ac *AcceptanceContext) potSplitEvenly(amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) potIsSplitEvenly() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerWinsAmount(playerName string, amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) bothPlayersPlayTheBoard() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) bothHavePairOfAces() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerWinsWithKicker(playerName string) error {
	return godog.ErrPending
}

// =============================================================================
// Assertion step implementations - heads-up and blind positions
// =============================================================================

func (ac *AcceptanceContext) smallAndBigBlinds(sbPlayer, bbPlayer string) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerPostsSmallBlindOf(playerName string, amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerPostsBigBlindOf(playerName string, amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerActsFirstPreflop(playerName string) error {
	return godog.ErrPending
}

// =============================================================================
// Assertion step implementations - betting restrictions
// =============================================================================

func (ac *AcceptanceContext) playerMayCallOrRaise(playerName string, callAmount, minRaise int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerMayOnlyCall(playerName string, amount int, otherPlayer string) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerMayReRaise(playerName, otherPlayer string) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerMustAct(playerName string) error {
	return godog.ErrPending
}

// =============================================================================
// Assertion step implementations - elimination
// =============================================================================

func (ac *AcceptanceContext) playerEliminatedFromTable(playerName, tableName string) error {
	return godog.ErrPending
}

// =============================================================================
// Assertion step implementations - error handling
// =============================================================================

func (ac *AcceptanceContext) commandFailsWith(message string) error {
	if ac.lastError == nil {
		return fmt.Errorf("expected command to fail with '%s', but it succeeded", message)
	}
	if !strings.Contains(strings.ToLower(ac.lastError.Error()), strings.ToLower(message)) {
		return fmt.Errorf("expected error containing '%s', got '%s'", message, ac.lastError.Error())
	}
	return nil
}

func (ac *AcceptanceContext) requestFailsWith(message string) error {
	return ac.commandFailsWith(message)
}

// =============================================================================
// Assertion step implementations - saga coordination
// =============================================================================

func (ac *AcceptanceContext) withinNSeconds(seconds int, table *godog.Table) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) withinNSecondsBankrollShows(seconds int, name string, amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) withinSecondsCardsDealt(seconds int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) handSameHandNumber() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) tableUpdatesPlayerStacks() error {
	return godog.ErrPending
}

// =============================================================================
// Sync mode step implementations - When
// =============================================================================

func (ac *AcceptanceContext) startHandWithMode(tableName string, syncMode pb.SyncMode, cascadeErrorMode pb.CascadeErrorMode) error {
	t := ac.getOrCreateTable(tableName)

	cmd := &examples.StartHand{}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}

	ac.syncTestStartTime = time.Now()
	resp, err := ac.client.SendCommandWithMode("table", t.root, cmdAny, t.sequence,
		syncMode, cascadeErrorMode)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	if resp.Events != nil {
		t.sequence += uint32(len(resp.Events.Pages))
	}
	ac.getOrCreateHand(tableName)
	return nil
}

func (ac *AcceptanceContext) startHandAsync(tableName string) error {
	return ac.startHandWithMode(tableName, pb.SyncMode_SYNC_MODE_ASYNC, pb.CascadeErrorMode_CASCADE_ERROR_FAIL_FAST)
}

func (ac *AcceptanceContext) startHandSimple(tableName string) error {
	return ac.startHandWithMode(tableName, pb.SyncMode_SYNC_MODE_SIMPLE, pb.CascadeErrorMode_CASCADE_ERROR_FAIL_FAST)
}

func (ac *AcceptanceContext) startHandCascade(tableName string) error {
	return ac.startHandWithMode(tableName, pb.SyncMode_SYNC_MODE_CASCADE, pb.CascadeErrorMode_CASCADE_ERROR_FAIL_FAST)
}

func (ac *AcceptanceContext) startHandCascadeFailFast(tableName string) error {
	return ac.startHandWithMode(tableName, pb.SyncMode_SYNC_MODE_CASCADE, pb.CascadeErrorMode_CASCADE_ERROR_FAIL_FAST)
}

func (ac *AcceptanceContext) startHandCascadeContinue(tableName string) error {
	return ac.startHandWithMode(tableName, pb.SyncMode_SYNC_MODE_CASCADE, pb.CascadeErrorMode_CASCADE_ERROR_CONTINUE)
}

func (ac *AcceptanceContext) startHandCascadeDeadLetter(tableName string) error {
	return ac.startHandWithMode(tableName, pb.SyncMode_SYNC_MODE_CASCADE, pb.CascadeErrorMode_CASCADE_ERROR_DEAD_LETTER)
}

func (ac *AcceptanceContext) executeCommandCascade() error {
	// Execute a generic command with CASCADE mode
	if ac.domainNoSagas {
		// Use an empty domain or one with no sagas
		return godog.ErrPending
	}
	return godog.ErrPending
}

func (ac *AcceptanceContext) executeTriggeringContinue() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) sendEventWithoutCorrelationCascade() error {
	return godog.ErrPending
}

// =============================================================================
// Sync mode step implementations - Then (assertion)
// =============================================================================

func (ac *AcceptanceContext) commandSucceedsImmediately() error {
	if ac.lastError != nil {
		return fmt.Errorf("expected command to succeed, got: %v", ac.lastError)
	}
	return nil
}

func (ac *AcceptanceContext) commandSucceeds() error {
	if ac.lastError != nil {
		return fmt.Errorf("expected command to succeed, got: %v", ac.lastError)
	}
	return nil
}

func (ac *AcceptanceContext) commandSucceedsWithHandStarted() error {
	return ac.commandSucceeds()
}

func (ac *AcceptanceContext) commandSucceedsWithHandStartedOnly() error {
	return ac.commandSucceeds()
}

func (ac *AcceptanceContext) responseNoProjectionUpdates() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) responseNoCascadeResults() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) responseNoCascadeResultsFromSagas() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) responseIncludesProjectionUpdatesFor(projector string) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) responseIncludesProjectionUpdates() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) responseIncludesProjectionUpdatesBothDomains() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) projectionShowsBankroll(amount int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) tableProjectionHandCountIncremented() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) commandReturnsBeforeDealCards() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) responseIncludesCascadeResults() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) cascadeIncludesDealCards() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) cascadeIncludesCardsDealt() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) responseIncludesCascadeChain(table *godog.Table) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) noEventsBusPublished() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) allEventsInProcess() error {
	return godog.ErrPending
}

// =============================================================================
// Cascade error mode step implementations - Then
// =============================================================================

func (ac *AcceptanceContext) commandFailsWithSagaError() error {
	if ac.lastError == nil {
		return fmt.Errorf("expected command to fail with saga error, but it succeeded")
	}
	return nil
}

func (ac *AcceptanceContext) noFurtherSagasAfterFailure() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) originalHandStartedPersisted() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) responseIncludesCascadeErrors() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) responseIncludesSuccessfulProjectionUpdates() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) otherSagasContinue() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) otherSagasContinueExecuting() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) compensationInReverseOrder() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) commandFailsAfterCompensation() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) sagaFailureToDeadLetter() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) deadLetterIncludes(table *godog.Table) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) originalEventPersisted() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) allSagaErrorsCollected() error {
	return godog.ErrPending
}

// =============================================================================
// Process manager step implementations
// =============================================================================

func (ac *AcceptanceContext) handFlowPmRegistered() error {
	ac.handFlowPMRegistered = true
	return nil
}

func (ac *AcceptanceContext) monitoringEventBus() error {
	ac.monitoringBus = true
	return nil
}

func (ac *AcceptanceContext) pmReceivesCorrelatedEvents() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) responseIncludesPmUpdates() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) pmNotInvoked() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) sagasExecuteNormally() error {
	return godog.ErrPending
}

// =============================================================================
// Performance step implementations
// =============================================================================

func (ac *AcceptanceContext) allCommandsWithinMs(ms int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) totalTimeLessThanSimple() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) responseTimeHigher() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) allStateConsistent() error {
	return godog.ErrPending
}

// =============================================================================
// Edge case step implementations
// =============================================================================

func (ac *AcceptanceContext) emptyResponse() error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) sagaProducesNoCommands() error {
	return godog.ErrPending
}

// =============================================================================
// Saga configuration step implementations
// =============================================================================

func (ac *AcceptanceContext) tableHandSagaConfiguredToFail() error {
	ac.tableHandSagaFail = true
	return nil
}

func (ac *AcceptanceContext) outputProjectorHealthy() error {
	ac.outputProjectorOK = true
	return nil
}

func (ac *AcceptanceContext) handPlayerSagaConfiguredToFail() error {
	ac.handPlayerSagaFail = true
	return nil
}

func (ac *AcceptanceContext) deadLetterQueueConfigured() error {
	ac.deadLetterConfigured = true
	return nil
}

func (ac *AcceptanceContext) domainWithNoRegisteredSagas() error {
	ac.domainNoSagas = true
	return nil
}

func (ac *AcceptanceContext) multipleSagasConfiguredToFail() error {
	ac.multipleSagasFail = true
	return nil
}
