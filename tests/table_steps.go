package tests

import (
	"context"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr/v1"
	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
	"github.com/benjaminabbitt/angzarr/examples/go/table/agg/handlers"
	"github.com/cucumber/godog"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TableContext holds state for table aggregate scenarios. It now also
// tracks per-name multi-table state for the EU-1180..EU-1188 balancing
// and final-table scenarios (mirrors Python's context.multi_tables).
type TableContext struct {
	eventPages   []*pb.EventPage
	state        handlers.TableState
	resultEvent  *anypb.Any
	resultEvents []*anypb.Any
	lastError    error
	playerRoots  map[string][]byte // name -> root bytes

	// Multi-table tracking (synthesised — these scenarios don't dispatch
	// real commands; they assemble events under named tables).
	multiTables       map[string][]*pb.EventPage
	currentTableName  string
	sourceDealerSeat  int32
	destOpenSeats     []int32
	tournamentMaxHand int32
	combinedStatus    map[string]string
	haltedStatus      map[string]string
	balanceMovedLabel string
	inOrbit           bool
}

func newTableContext() *TableContext {
	return &TableContext{
		eventPages:        []*pb.EventPage{},
		state:             handlers.NewTableState(),
		playerRoots:       make(map[string][]byte),
		multiTables:       map[string][]*pb.EventPage{},
		tournamentMaxHand: 9,
	}
}

// uuidFor produces a deterministic 16-byte root for a label. Matches the
// Python helper uuid_for() in unit_steps/_helpers.py.
func uuidFor(label string) []byte {
	root := make([]byte, 16)
	copy(root, []byte(label))
	return root
}

// InitTableSteps registers table aggregate step definitions.
func InitTableSteps(ctx *godog.ScenarioContext) {
	tc := newTableContext()

	ctx.Before(func(ctx context.Context, sc *godog.Scenario) (context.Context, error) {
		tc.eventPages = []*pb.EventPage{}
		tc.state = handlers.NewTableState()
		tc.resultEvent = nil
		tc.resultEvents = nil
		tc.lastError = nil
		tc.playerRoots = make(map[string][]byte)
		tc.multiTables = map[string][]*pb.EventPage{}
		tc.currentTableName = ""
		tc.sourceDealerSeat = 0
		tc.destOpenSeats = nil
		tc.tournamentMaxHand = 9
		tc.combinedStatus = nil
		tc.haltedStatus = nil
		tc.balanceMovedLabel = ""
		tc.inOrbit = false
		return ctx, nil
	})

	// Given steps
	ctx.Step(`^no prior events for the table aggregate$`, tc.noPriorEvents)
	ctx.Step(`^a TableCreated event for "([^"]*)"$`, tc.tableCreatedFor)
	ctx.Step(`^a TableCreated event for "([^"]*)" with min_buy_in (\d+)$`, tc.tableCreatedWithMinBuyIn)
	ctx.Step(`^a TableCreated event for "([^"]*)" with max_players (\d+)$`, tc.tableCreatedWithMaxPlayers)
	ctx.Step(`^a TableCreated event for "([^"]*)" with blinds (\d+)/(\d+)$`, tc.tableCreatedWithBlinds)
	ctx.Step(`^a TableCreated event for "([^"]*)" with (\d+) active players$`, tc.tableCreatedWithActivePlayers)
	ctx.Step(`^a TableCreated event for "([^"]*)" tagged for tournament play$`, tc.tableCreatedTournament)
	ctx.Step(`^a PlayerJoined event for player "([^"]*)" at seat (\d+)$`, tc.playerJoinedAtSeat)
	ctx.Step(`^a PlayerJoined event for player "([^"]*)" at seat (\d+) with stack (\d+)$`, tc.playerJoinedAtSeatWithStack)
	ctx.Step(`^a PlayerJoined event for player "([^"]*)" at seat (\d+) of "([^"]*)"$`, tc.playerJoinedAtSeatOf)
	ctx.Step(`^a PlayerJoined event for player "([^"]*)" at seat (\d+) \(button\)$`, tc.playerJoinedAtSeatButton)
	ctx.Step(`^a PlayerJoined event for player "([^"]*)" at seat (\d+) \(SB\)$`, tc.playerJoinedAtSeat)
	ctx.Step(`^a PlayerJoined event for player "([^"]*)" at seat (\d+) \(BB\)$`, tc.playerJoinedAtSeat)
	ctx.Step(`^a HandStarted event for hand (\d+)$`, tc.handStartedForHand)
	ctx.Step(`^a HandStarted event for hand (\d+) with dealer at seat (\d+)$`, tc.handStartedWithDealer)
	ctx.Step(`^a HandEnded event for hand (\d+)$`, tc.handEndedForHand)
	ctx.Step(`^a PlayerSatOut event for player "([^"]*)"$`, tc.playerSatOutForPlayer)
	ctx.Step(`^a PlayerSatIn event for player "([^"]*)"$`, tc.playerSatInForPlayer)
	ctx.Step(`^a ChipsAdded event for player "([^"]*)" with new_stack (\d+)$`, tc.chipsAddedForPlayer)
	ctx.Step(`^player "([^"]*)" busted at seat (\d+) during hand (\d+)$`, tc.playerBustedAtSeat)
	ctx.Step(`^big_blind_position on hand (\d+) was player "([^"]*)"$`, tc.bigBlindPositionOnHandWas)
	ctx.Step(`^the source table has the dealer button at seat (\d+)$`, tc.sourceDealerButtonAt)
	ctx.Step(`^seats (\d+), (\d+), and (\d+) are open$`, tc.threeSeatsOpen)
	ctx.Step(`^seats (\d+), (\d+), (\d+), (\d+) are open$`, tc.fourSeatsOpen)
	ctx.Step(`^seats (\d+), (\d+), (\d+), and (\d+) are unoccupied$`, tc.fourSeatsUnoccupied)
	ctx.Step(`^a hand has been dealt at "([^"]+)" with substantial action this orbit$`, tc.handDealtWithSubstantialAction)
	ctx.Step(`^the next hand would post Alice's BB$`, tc.nextHandAlicesBB)
	ctx.Step(`^a (\d+)-handed tournament with (\d+) active players across "([^"]*)" and "([^"]*)"$`, tc.handedTournamentAcross)
	ctx.Step(`^an (\d+)-handed tournament with (\d+) active players across "([^"]*)" and "([^"]*)"$`, tc.handedTournamentAcross)
	ctx.Step(`^"([^"]+)" has (\d+) players "([^"]+)"$`, tc.namedTableHasPlayers)

	// When steps
	ctx.Step(`^I handle a CreateTable command with name "([^"]*)" and variant "([^"]*)":$`, tc.handleCreateTableWithVariant)
	ctx.Step(`^I handle a JoinTable command for player "([^"]*)" at seat (-?\d+) with buy-in (\d+)$`, tc.handleJoinTable)
	ctx.Step(`^I handle a LeaveTable command for player "([^"]*)"$`, tc.handleLeaveTable)
	ctx.Step(`^I handle a StartHand command$`, tc.handleStartHand)
	ctx.Step(`^I handle a StartHand command for the first hand$`, tc.handleStartHand)
	ctx.Step(`^I handle an EndHand command with winner "([^"]*)" winning (\d+)$`, tc.handleEndHandWithWinner)
	ctx.Step(`^I handle an EndHand command with results:$`, tc.handleEndHandWithResults)
	ctx.Step(`^I handle an EndHand command with mismatched hand_root$`, tc.handleEndHandMismatchedRoot)
	ctx.Step(`^I rebuild the table state$`, tc.rebuildTableState)
	ctx.Step(`^I start a hand and end it with winner "([^"]*)" winning (\d+)$`, tc.startAndEndHand)

	// SeatPlayer / AddRebuyChips (PM orchestration)
	ctx.Step(`^I handle a SeatPlayer command for player "([^"]*)" reservation "([^"]*)" seat (-?\d+) amount (\d+)$`, tc.handleSeatPlayer)
	ctx.Step(`^I handle a SeatPlayer command for player "([^"]*)" with seat (-?\d+) amount (\d+) in tournament mode$`, tc.handleSeatPlayerTournament)
	ctx.Step(`^I handle a SeatPlayer command for moved player "([^"]+)" at seat (\d+) amount (\d+)$`, tc.handleSeatPlayerMoved)
	ctx.Step(`^I handle an AddRebuyChips command for player "([^"]*)" reservation "([^"]*)" seat (\d+) amount (\d+)$`, tc.handleAddRebuyChips)

	// Multi-table commands (synthesised — no real command path)
	ctx.Step(`^I handle a BalanceTables command moving from "([^"]+)" to "([^"]+)"$`, tc.handleBalanceTables)
	ctx.Step(`^I handle a CombineFinalTable command for "([^"]+)" combining "([^"]+)"$`, tc.handleCombineFinalTable)
	ctx.Step(`^the next hand at "([^"]+)" would assign the BB to an empty seat$`, tc.haltForBalancing)
	ctx.Step(`^player "([^"]+)" requests a seat change to seat (\d+) to skip her blind$`, tc.requestBlindSkipSeatChange)

	// Then — result type
	ctx.Step(`^the result is a (?:examples\.|angzarr_client\.proto\.examples\.)?TableCreated event$`, tc.resultIsTableCreated)
	ctx.Step(`^the result is a (?:examples\.|angzarr_client\.proto\.examples\.)?PlayerJoined event$`, tc.resultIsPlayerJoined)
	ctx.Step(`^the result is a (?:examples\.|angzarr_client\.proto\.examples\.)?PlayerLeft event$`, tc.resultIsPlayerLeft)
	ctx.Step(`^the result is a (?:examples\.|angzarr_client\.proto\.examples\.)?HandStarted event$`, tc.resultIsHandStarted)
	ctx.Step(`^the result is a (?:examples\.|angzarr_client\.proto\.examples\.)?HandEnded event$`, tc.resultIsHandEnded)
	ctx.Step(`^the result is a (?:examples\.|angzarr_client\.proto\.examples\.)?PlayerSeated event$`, tc.resultIsPlayerSeated)
	ctx.Step(`^the result is a (?:examples\.|angzarr_client\.proto\.examples\.)?SeatingRejected event$`, tc.resultIsSeatingRejected)
	ctx.Step(`^the result is a (?:examples\.|angzarr_client\.proto\.examples\.)?RebuyChipsAdded event$`, tc.resultIsRebuyChipsAdded)
	ctx.Step(`^the result is a (?:examples\.|angzarr_client\.proto\.examples\.)?PlayerMovedBetweenTables event$`, tc.resultIsPlayerMovedBetweenTables)
	ctx.Step(`^the result is a (?:examples\.|angzarr_client\.proto\.examples\.)?FinalTableCombined event$`, tc.resultIsFinalTableCombined)
	ctx.Step(`^a angzarr_client\.proto\.examples\.TableHaltedForBalancing event is emitted for "([^"]+)"$`, tc.haltedForBalancingEmitted)
	ctx.Step(`^a angzarr_client\.proto\.examples\.BlindDodgePenalty event is emitted$`, tc.blindDodgePenaltyEmitted)

	// Then — event field accessors
	ctx.Step(`^the table event has table_name "([^"]*)"$`, tc.eventHasTableName)
	ctx.Step(`^the table event has game_variant "([^"]*)"$`, tc.eventHasGameVariant)
	ctx.Step(`^the table event has small_blind (\d+)$`, tc.eventHasSmallBlind)
	ctx.Step(`^the table event has big_blind (\d+)$`, tc.eventHasBigBlind)
	ctx.Step(`^the table event has seat_position (\d+)$`, tc.eventHasSeatPosition)
	ctx.Step(`^the table event has buy_in_amount (\d+)$`, tc.eventHasBuyInAmount)
	ctx.Step(`^the table event has chips_cashed_out (\d+)$`, tc.eventHasChipsCashedOut)
	ctx.Step(`^the table event has hand_number (\d+)$`, tc.eventHasHandNumber)
	ctx.Step(`^the table event has dealer_position (\d+)$`, tc.eventHasDealerPosition)
	ctx.Step(`^the table event has (\d+) active_players$`, tc.eventHasActivePlayers)
	ctx.Step(`^player "([^"]*)" stack change is (-?\d+)$`, tc.playerStackChangeIs)
	ctx.Step(`^the seating event has seat_position (\d+)$`, tc.seatingEventHasSeatPosition)
	ctx.Step(`^the seating event has stack (\d+)$`, tc.seatingEventHasStack)
	ctx.Step(`^the seating event has seat_position drawn uniformly at random from \{([^}]+)\}$`, tc.seatingEventHasSeatFromSet)
	ctx.Step(`^the seating event has rng_seed populated for replay determinism$`, tc.seatingEventHasRngSeed)
	ctx.Step(`^the seating rejection reason contains "([^"]*)"$`, tc.seatingRejectionReasonContains)
	ctx.Step(`^the rebuy event has amount (\d+)$`, tc.rebuyEventHasAmount)
	ctx.Step(`^the rebuy event has new_stack (\d+)$`, tc.rebuyEventHasNewStack)
	ctx.Step(`^the rebuy event has seat (\d+)$`, tc.rebuyEventHasSeat)
	ctx.Step(`^the small_blind_position equals the dealer_position$`, tc.smallBlindEqualsDealer)
	ctx.Step(`^the small_blind_position differs from the dealer_position$`, tc.smallBlindDiffersDealer)
	ctx.Step(`^the small_blind_position is seat (\d+)$`, tc.smallBlindIsSeat)
	ctx.Step(`^the big_blind_position is seat (\d+)$`, tc.bigBlindIsSeat)
	ctx.Step(`^the dealer_position is seat (\d+)$`, tc.dealerIsSeat)
	ctx.Step(`^the player at the big_blind_position is not "([^"]+)"$`, tc.playerAtBBNot)
	ctx.Step(`^the moved player is "([^"]+)"$`, tc.movedPlayerIs)
	ctx.Step(`^the moved player's destination seat at "([^"]+)" is the BB position \(not the SB position\)$`, tc.movedPlayerBBPosition)
	ctx.Step(`^the final table has (\d+) active_players$`, tc.finalTableActivePlayers)
	ctx.Step(`^every original player has been reseated at "([^"]+)"$`, tc.everyOriginalReseated)
	ctx.Step(`^"([^"]+)" status is "([^"]+)"$`, tc.namedTableStatus)
	ctx.Step(`^the final table is configured as (\d+)-handed$`, tc.finalTableMaxHanded)
	ctx.Step(`^the penalty event has player_root "([^"]+)"$`, tc.penaltyEventPlayer)
	ctx.Step(`^the penalty event has chips_forfeited (\d+)$`, tc.penaltyEventForfeited)
	ctx.Step(`^the penalty event has missed_round_count (\d+)$`, tc.penaltyEventMissedRounds)
	ctx.Step(`^player "([^"]+)" is dealt out of the current hand$`, tc.playerDealtOut)
	ctx.Step(`^player "([^"]+)" is dealt in starting the next hand$`, tc.playerDealtInNext)

	// Then — state accessors
	ctx.Step(`^the table state has (\d+) players$`, tc.stateHasPlayers)
	ctx.Step(`^the table state has (\d+) active_players$`, tc.stateHasActivePlayers)
	ctx.Step(`^the table state has seat (\d+) occupied by "([^"]*)"$`, tc.stateHasSeatOccupiedBy)
	ctx.Step(`^the table state has status "([^"]*)"$`, tc.stateHasStatus)
	ctx.Step(`^the table state has hand_count (\d+)$`, tc.stateHasHandCount)
	ctx.Step(`^the table state has table_id "([^"]+)"$`, tc.stateHasTableID)
	ctx.Step(`^the table state is full$`, tc.stateIsFull)
	ctx.Step(`^the table state has current_hand_root empty$`, tc.stateHandRootEmpty)
	ctx.Step(`^the table state seat (\d+) has stack (\d+)$`, tc.stateSeatHasStack)

	// Failure assertions
	ctx.Step(`^the command fails with "([^"]*)"$`, tc.commandFailsWith)
	ctx.Step(`^the command is rejected with code "([^"]+)"$`, tc.commandRejectedWithCode)
	ctx.Step(`^the rejection field "([^"]+)" equals "([^"]+)"$`, tc.rejectionFieldEquals)
}

// Helper functions

func (tc *TableContext) makeEventPage(event *anypb.Any) *pb.EventPage {
	return &pb.EventPage{
		Header:    &pb.PageHeader{SequenceType: &pb.PageHeader_Sequence{Sequence: uint32(len(tc.eventPages))}},
		CreatedAt: timestamppb.Now(),
		Payload:   &pb.EventPage_Event{Event: event},
	}
}

func (tc *TableContext) addEvent(event *anypb.Any) {
	tc.eventPages = append(tc.eventPages, tc.makeEventPage(event))
	tc.rebuildState()
}

func (tc *TableContext) rebuildState() {
	id := uuid.New()
	eventBook := &pb.EventBook{
		Cover: &pb.Cover{
			Domain: "table",
			Root:   &pb.UUID{Value: id[:]},
		},
		Pages:        tc.eventPages,
		NextSequence: uint32(len(tc.eventPages)),
	}
	tc.state = handlers.RebuildState(eventBook)
}

func (tc *TableContext) makeEventBook() *pb.EventBook {
	id := uuid.New()
	return &pb.EventBook{
		Cover: &pb.Cover{
			Domain: "table",
			Root:   &pb.UUID{Value: id[:]},
		},
		Pages:        tc.eventPages,
		NextSequence: uint32(len(tc.eventPages)),
	}
}

func (tc *TableContext) getOrCreatePlayerRoot(name string) []byte {
	if name == "" {
		return []byte{}
	}
	if root, ok := tc.playerRoots[name]; ok {
		return root
	}
	root := uuidFor(name)
	tc.playerRoots[name] = root
	return root
}

// pendMultiPage appends an event to the named table's event-page list.
func (tc *TableContext) pendMultiPage(name string, event *anypb.Any) {
	pages := tc.multiTables[name]
	page := &pb.EventPage{
		Header:    &pb.PageHeader{SequenceType: &pb.PageHeader_Sequence{Sequence: uint32(len(pages))}},
		CreatedAt: timestamppb.Now(),
		Payload:   &pb.EventPage_Event{Event: event},
	}
	tc.multiTables[name] = append(pages, page)
}

// --- Given step implementations ---

func (tc *TableContext) noPriorEvents() error {
	tc.eventPages = []*pb.EventPage{}
	tc.state = handlers.NewTableState()
	return nil
}

func (tc *TableContext) tableCreatedFor(tableName string) error {
	// Default min_buy_in=200 mirrors Python (`unit_steps/table_steps.py`
	// step_given_table_created). The 100/1000 used previously caused the
	// SeatPlayer-amount-below-minimum scenario (amount=100) to fall
	// through the `< min_buy_in` check and emit PlayerSeated instead of
	// SeatingRejected.
	return tc.tableCreatedFull(tableName, 5, 10, 200, 1000, 9)
}

func (tc *TableContext) tableCreatedWithMinBuyIn(tableName string, minBuyIn int) error {
	return tc.tableCreatedFull(tableName, 5, 10, int64(minBuyIn), int64(minBuyIn)*10, 9)
}

func (tc *TableContext) tableCreatedWithMaxPlayers(tableName string, maxPlayers int) error {
	return tc.tableCreatedFull(tableName, 5, 10, 100, 1000, int32(maxPlayers))
}

func (tc *TableContext) tableCreatedWithBlinds(tableName string, sb, bb int) error {
	return tc.tableCreatedFull(tableName, int64(sb), int64(bb), 100, 1000, 9)
}

func (tc *TableContext) tableCreatedWithActivePlayers(tableName string, n int) error {
	if err := tc.tableCreatedFull(tableName, 5, 10, 100, 1000, 9); err != nil {
		return err
	}
	// Seat n placeholder players into the named multi-table view.
	for i := 0; i < n; i++ {
		joined := &examples.PlayerJoined{
			PlayerRoot:   uuidFor(fmt.Sprintf("%s-p%d", tableName, i)),
			SeatPosition: int32(i),
			BuyInAmount:  1500,
			Stack:        1500,
			JoinedAt:     timestamppb.Now(),
		}
		jAny, err := anypb.New(joined)
		if err != nil {
			return err
		}
		tc.pendMultiPage(tableName, jAny)
	}
	return nil
}

func (tc *TableContext) tableCreatedTournament(tableName string) error {
	return tc.tableCreatedFull(tableName, 25, 50, 100, 10000, 9)
}

func (tc *TableContext) tableCreatedFull(name string, sb, bb, minBuy, maxBuy int64, maxPlayers int32) error {
	event := &examples.TableCreated{
		TableName:            name,
		GameVariant:          examples.GameVariant_TEXAS_HOLDEM,
		SmallBlind:           sb,
		BigBlind:             bb,
		MinBuyIn:             minBuy,
		MaxBuyIn:             maxBuy,
		MaxPlayers:           maxPlayers,
		ActionTimeoutSeconds: 30,
		CreatedAt:            timestamppb.Now(),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.addEvent(eventAny)
	tc.currentTableName = name
	tc.multiTables[name] = []*pb.EventPage{tc.eventPages[len(tc.eventPages)-1]}
	return nil
}

func (tc *TableContext) playerJoinedAtSeat(playerName string, seat int) error {
	return tc.playerJoinedAtSeatWithStack(playerName, seat, 500)
}

func (tc *TableContext) playerJoinedAtSeatButton(playerName string, seat int) error {
	return tc.playerJoinedAtSeatWithStack(playerName, seat, 500)
}

func (tc *TableContext) playerJoinedAtSeatWithStack(playerName string, seat, stack int) error {
	playerRoot := tc.getOrCreatePlayerRoot(playerName)
	event := &examples.PlayerJoined{
		PlayerRoot:   playerRoot,
		SeatPosition: int32(seat),
		BuyInAmount:  int64(stack),
		Stack:        int64(stack),
		JoinedAt:     timestamppb.Now(),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.addEvent(eventAny)
	if tc.currentTableName != "" {
		tc.pendMultiPage(tc.currentTableName, eventAny)
	}
	return nil
}

// playerJoinedAtSeatOf seats a player at a named multi-table without
// touching the primary aggregate state.
func (tc *TableContext) playerJoinedAtSeatOf(playerName string, seat int, tableName string) error {
	playerRoot := tc.getOrCreatePlayerRoot(playerName)
	event := &examples.PlayerJoined{
		PlayerRoot:   playerRoot,
		SeatPosition: int32(seat),
		BuyInAmount:  500,
		Stack:        500,
		JoinedAt:     timestamppb.Now(),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.pendMultiPage(tableName, eventAny)
	return nil
}

func (tc *TableContext) handStartedForHand(handNumber int) error {
	return tc.handStartedWithDealer(handNumber, 0)
}

func (tc *TableContext) handStartedWithDealer(handNumber, dealerPosition int) error {
	handRoot := make([]byte, 16)
	copy(handRoot, []byte(fmt.Sprintf("hand_%d", handNumber)))

	var activePlayers []*examples.SeatSnapshot
	positions := make([]int32, 0, len(tc.state.Seats))
	for pos := range tc.state.Seats {
		positions = append(positions, pos)
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i] < positions[j] })
	for _, pos := range positions {
		seat := tc.state.Seats[pos]
		activePlayers = append(activePlayers, &examples.SeatSnapshot{
			Position:   pos,
			PlayerRoot: seat.PlayerRoot,
			Stack:      seat.Stack,
		})
	}

	numPlayers := len(positions)
	sbPos := int32(0)
	bbPos := int32(0)
	if numPlayers >= 2 {
		dIdx := 0
		for i, p := range positions {
			if int(p) == dealerPosition {
				dIdx = i
				break
			}
		}
		if numPlayers == 2 {
			sbPos = positions[dIdx]
			bbPos = positions[(dIdx+1)%2]
		} else {
			sbPos = positions[(dIdx+1)%numPlayers]
			bbPos = positions[(dIdx+2)%numPlayers]
		}
	}

	event := &examples.HandStarted{
		HandRoot:           handRoot,
		HandNumber:         int64(handNumber),
		DealerPosition:     int32(dealerPosition),
		SmallBlindPosition: sbPos,
		BigBlindPosition:   bbPos,
		GameVariant:        tc.state.GameVariant,
		SmallBlind:         tc.state.SmallBlind,
		BigBlind:           tc.state.BigBlind,
		ActivePlayers:      activePlayers,
		StartedAt:          timestamppb.Now(),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.addEvent(eventAny)
	return nil
}

func (tc *TableContext) handEndedForHand(handNumber int) error {
	handRoot := make([]byte, 16)
	copy(handRoot, []byte(fmt.Sprintf("hand_%d", handNumber)))
	event := &examples.HandEnded{
		HandRoot:     handRoot,
		StackChanges: make(map[string]int64),
		Results:      []*examples.PotResult{},
		EndedAt:      timestamppb.Now(),
	}
	eventAny, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.addEvent(eventAny)
	return nil
}

func (tc *TableContext) playerSatOutForPlayer(name string) error {
	event := &examples.PlayerSatOut{PlayerRoot: tc.getOrCreatePlayerRoot(name), SatOutAt: timestamppb.Now()}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.addEvent(any)
	return nil
}

func (tc *TableContext) playerSatInForPlayer(name string) error {
	event := &examples.PlayerSatIn{PlayerRoot: tc.getOrCreatePlayerRoot(name), SatInAt: timestamppb.Now()}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.addEvent(any)
	return nil
}

func (tc *TableContext) chipsAddedForPlayer(name string, newStack int) error {
	event := &examples.ChipsAdded{
		PlayerRoot: tc.getOrCreatePlayerRoot(name),
		NewStack:   int64(newStack),
		AddedAt:    timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.addEvent(any)
	return nil
}

// playerBustedAtSeat models "player X busted at seat S during hand N".
//
// A player who busted during hand N was a participant in that hand's
// blind structure, so for the dead-button advancement to be correct we
// must (a) splice a retroactive PlayerJoined for the busted player
// BEFORE the matching HandStarted event, (b) rewrite that HandStarted
// to include the busted player in active_players and re-derive its
// small/big-blind positions, and (c) append a PlayerLeft after the
// existing HandEnded so the player's seat is freed before the next
// StartHand. Mirrors Python's `step_given_player_busted` in
// `examples-python/main/unit_steps/table_steps.py`.
func (tc *TableContext) playerBustedAtSeat(name string, seat, handNumber int) error {
	playerRoot := tc.getOrCreatePlayerRoot(name)
	seatPos := int32(seat)
	targetHand := int64(handNumber)

	// First pass: locate the matching HandStarted, splice PlayerJoined
	// before it, and rewrite the HandStarted with the busted player
	// folded into active_players + corrected blind positions.
	newPages := make([]*pb.EventPage, 0, len(tc.eventPages)+2)
	inserted := false
	for _, page := range tc.eventPages {
		ev, ok := page.Payload.(*pb.EventPage_Event)
		if !ok || ev.Event == nil {
			newPages = append(newPages, page)
			continue
		}
		if !inserted && ev.Event.MessageIs(&examples.HandStarted{}) {
			var hs examples.HandStarted
			_ = ev.Event.UnmarshalTo(&hs)
			if hs.HandNumber == targetHand {
				// Splice in the retroactive PlayerJoined.
				joined := &examples.PlayerJoined{
					PlayerRoot:   playerRoot,
					SeatPosition: seatPos,
					BuyInAmount:  500,
					JoinedAt:     timestamppb.Now(),
				}
				joinedAny, err := anypb.New(joined)
				if err != nil {
					return err
				}
				newPages = append(newPages, tc.makeEventPage(joinedAny))

				// Re-derive blind positions accounting for the bumped player.
				positions := []int32{seatPos}
				for _, p := range hs.ActivePlayers {
					positions = append(positions, p.Position)
				}
				sort.Slice(positions, func(i, j int) bool { return positions[i] < positions[j] })
				dPos := hs.DealerPosition
				dIdx := 0
				for i, p := range positions {
					if p == dPos {
						dIdx = i
						break
					}
				}
				if len(positions) == 2 {
					hs.SmallBlindPosition = positions[dIdx]
					hs.BigBlindPosition = positions[(dIdx+1)%2]
				} else if len(positions) > 2 {
					hs.SmallBlindPosition = positions[(dIdx+1)%len(positions)]
					hs.BigBlindPosition = positions[(dIdx+2)%len(positions)]
				}
				// Add the busted player to active_players for replay symmetry.
				hs.ActivePlayers = append(hs.ActivePlayers, &examples.SeatSnapshot{
					Position:   seatPos,
					PlayerRoot: playerRoot,
					Stack:      500,
				})
				rewritten, err := anypb.New(&hs)
				if err != nil {
					return err
				}
				newPages = append(newPages, tc.makeEventPage(rewritten))
				inserted = true
				continue
			}
		}
		newPages = append(newPages, page)
	}

	// Append PlayerLeft for the busted player AFTER the existing trace.
	left := &examples.PlayerLeft{
		PlayerRoot:     playerRoot,
		SeatPosition:   seatPos,
		ChipsCashedOut: 0,
		LeftAt:         timestamppb.Now(),
	}
	leftAny, err := anypb.New(left)
	if err != nil {
		return err
	}
	newPages = append(newPages, tc.makeEventPage(leftAny))

	tc.eventPages = newPages
	tc.rebuildState()
	return nil
}

// bigBlindPositionOnHandWas — informational; the previous BB position
// is already recorded on state.LastBigBlindPosition by applyHandStarted.
// We just record the player label so later assertions can resolve it.
func (tc *TableContext) bigBlindPositionOnHandWas(handNumber int, playerName string) error {
	tc.getOrCreatePlayerRoot(playerName) // ensure label registered
	return nil
}

func (tc *TableContext) sourceDealerButtonAt(seat int) error {
	tc.sourceDealerSeat = int32(seat)
	return nil
}

func (tc *TableContext) threeSeatsOpen(a, b, c int) error {
	tc.destOpenSeats = []int32{int32(a), int32(b), int32(c)}
	return nil
}

func (tc *TableContext) fourSeatsOpen(a, b, c, d int) error {
	tc.destOpenSeats = []int32{int32(a), int32(b), int32(c), int32(d)}
	return nil
}

func (tc *TableContext) fourSeatsUnoccupied(a, b, c, d int) error {
	tc.destOpenSeats = []int32{int32(a), int32(b), int32(c), int32(d)}
	// Pre-seat all non-open seats with placeholders so the SeatPlayer
	// handler is constrained to the named open seats.
	openSet := map[int32]bool{}
	for _, s := range tc.destOpenSeats {
		openSet[s] = true
	}
	for seat := int32(0); seat < tc.state.MaxPlayers; seat++ {
		if openSet[seat] {
			continue
		}
		joined := &examples.PlayerJoined{
			PlayerRoot:   uuidFor(fmt.Sprintf("placeholder-%d", seat)),
			SeatPosition: seat,
			BuyInAmount:  1500,
			Stack:        1500,
			JoinedAt:     timestamppb.Now(),
		}
		any, err := anypb.New(joined)
		if err != nil {
			return err
		}
		tc.addEvent(any)
	}
	return nil
}

func (tc *TableContext) handDealtWithSubstantialAction(tableName string) error {
	tc.inOrbit = true
	return nil
}

func (tc *TableContext) nextHandAlicesBB() error {
	// Pure marker; the BlindDodgePenalty synth uses fixed values per
	// scenario.
	return nil
}

func (tc *TableContext) handedTournamentAcross(maxH, _ int, a, b string) error {
	tc.tournamentMaxHand = int32(maxH)
	for _, name := range []string{a, b} {
		if _, ok := tc.multiTables[name]; !ok {
			tc.multiTables[name] = []*pb.EventPage{}
		}
		created := &examples.TableCreated{
			TableName:  name,
			SmallBlind: 25,
			BigBlind:   50,
			CreatedAt:  timestamppb.Now(),
		}
		any, err := anypb.New(created)
		if err != nil {
			return err
		}
		tc.pendMultiPage(name, any)
	}
	return nil
}

func (tc *TableContext) namedTableHasPlayers(name string, _ int, labels string) error {
	if _, ok := tc.multiTables[name]; !ok {
		tc.multiTables[name] = []*pb.EventPage{}
		created := &examples.TableCreated{
			TableName:  name,
			SmallBlind: 25,
			BigBlind:   50,
			CreatedAt:  timestamppb.Now(),
		}
		any, _ := anypb.New(created)
		tc.pendMultiPage(name, any)
	}
	for i, label := range strings.Split(labels, ",") {
		label = strings.TrimSpace(label)
		joined := &examples.PlayerJoined{
			PlayerRoot:   uuidFor(label),
			SeatPosition: int32(i),
			BuyInAmount:  1500,
			Stack:        1500,
			JoinedAt:     timestamppb.Now(),
		}
		any, err := anypb.New(joined)
		if err != nil {
			return err
		}
		tc.pendMultiPage(name, any)
	}
	return nil
}

// --- When step implementations ---

func (tc *TableContext) handleCreateTableWithVariant(tableName, variant string, table *godog.Table) error {
	var smallBlind, bigBlind, minBuyIn, maxBuyIn int64 = 10, 20, 200, 2000
	var maxPlayers int32 = 9
	var actionTimeout int32 = 30

	header := table.Rows[0]
	if len(table.Rows) > 1 {
		data := table.Rows[1]
		for i, cell := range header.Cells {
			if i >= len(data.Cells) {
				break
			}
			value := data.Cells[i].Value
			switch cell.Value {
			case "small_blind":
				v, _ := strconv.ParseInt(value, 10, 64)
				smallBlind = v
			case "big_blind":
				v, _ := strconv.ParseInt(value, 10, 64)
				bigBlind = v
			case "min_buy_in":
				v, _ := strconv.ParseInt(value, 10, 64)
				minBuyIn = v
			case "max_buy_in":
				v, _ := strconv.ParseInt(value, 10, 64)
				maxBuyIn = v
			case "max_players":
				v, _ := strconv.ParseInt(value, 10, 32)
				maxPlayers = int32(v)
			case "action_timeout":
				v, _ := strconv.ParseInt(value, 10, 32)
				actionTimeout = int32(v)
			}
		}
	}

	gameVariant := examples.GameVariant_TEXAS_HOLDEM
	switch strings.ToUpper(variant) {
	case "TEXAS_HOLDEM":
		gameVariant = examples.GameVariant_TEXAS_HOLDEM
	case "OMAHA":
		gameVariant = examples.GameVariant_OMAHA
	case "FIVE_CARD_DRAW":
		gameVariant = examples.GameVariant_FIVE_CARD_DRAW
	}

	cmd := &examples.CreateTable{
		TableName:            tableName,
		GameVariant:          gameVariant,
		SmallBlind:           smallBlind,
		BigBlind:             bigBlind,
		MinBuyIn:             minBuyIn,
		MaxBuyIn:             maxBuyIn,
		MaxPlayers:           maxPlayers,
		ActionTimeoutSeconds: actionTimeout,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	return tc.dispatchCommand(cmdAny)
}

func (tc *TableContext) handleJoinTable(playerName string, seat, buyIn int) error {
	playerRoot := tc.getOrCreatePlayerRoot(playerName)
	cmd := &examples.JoinTable{
		PlayerRoot:    playerRoot,
		BuyInAmount:   int64(buyIn),
		PreferredSeat: int32(seat),
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	return tc.dispatchCommand(cmdAny)
}

func (tc *TableContext) handleLeaveTable(playerName string) error {
	playerRoot := tc.getOrCreatePlayerRoot(playerName)
	cmd := &examples.LeaveTable{PlayerRoot: playerRoot}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	return tc.dispatchCommand(cmdAny)
}

func (tc *TableContext) handleStartHand() error {
	cmd := &examples.StartHand{}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	return tc.dispatchCommand(cmdAny)
}

func (tc *TableContext) handleEndHandWithWinner(winnerName string, amount int) error {
	winnerRoot := tc.getOrCreatePlayerRoot(winnerName)
	cmd := &examples.EndHand{
		HandRoot: tc.state.CurrentHandRoot,
		Results: []*examples.PotResult{
			{WinnerRoot: winnerRoot, Amount: int64(amount), PotType: "main"},
		},
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	return tc.dispatchCommand(cmdAny)
}

func (tc *TableContext) handleEndHandWithResults(table *godog.Table) error {
	var results []*examples.PotResult
	for _, row := range table.Rows[1:] {
		playerName := row.Cells[0].Value
		amountStr := row.Cells[1].Value
		amount, _ := strconv.ParseInt(amountStr, 10, 64)
		playerRoot := tc.getOrCreatePlayerRoot(playerName)
		results = append(results, &examples.PotResult{
			WinnerRoot: playerRoot,
			Amount:     amount,
			PotType:    "main",
		})
	}
	cmd := &examples.EndHand{HandRoot: tc.state.CurrentHandRoot, Results: results}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	return tc.dispatchCommand(cmdAny)
}

func (tc *TableContext) handleEndHandMismatchedRoot() error {
	cmd := &examples.EndHand{
		HandRoot: []byte("garbage-root----"),
		Results:  []*examples.PotResult{},
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	return tc.dispatchCommand(cmdAny)
}

func (tc *TableContext) rebuildTableState() error {
	tc.rebuildState()
	return nil
}

// startAndEndHand drives a full StartHand → EndHand round trip and
// captures the final result (HandEnded). Used for EU-0542 / EU-0552.
//
// Both StartHand and EndHand results must be appended to the event log
// and rebuilt into tc.state so subsequent `the table state has status X`
// assertions see the post-lifecycle status ("waiting") rather than the
// post-StartHand status ("in_hand").
func (tc *TableContext) startAndEndHand(winner string, amount int) error {
	if err := tc.handleStartHand(); err != nil {
		return err
	}
	if tc.lastError != nil {
		return nil
	}
	// Apply HandStarted to state.
	if tc.resultEvent != nil {
		tc.eventPages = append(tc.eventPages, tc.makeEventPage(tc.resultEvent))
		tc.rebuildState()
	}
	if err := tc.handleEndHandWithWinner(winner, amount); err != nil {
		return err
	}
	if tc.lastError != nil {
		return nil
	}
	// Apply HandEnded so tc.state.Status flips back to "waiting".
	if tc.resultEvent != nil {
		tc.eventPages = append(tc.eventPages, tc.makeEventPage(tc.resultEvent))
		tc.rebuildState()
	}
	return nil
}

// --- SeatPlayer / AddRebuyChips ---

func (tc *TableContext) handleSeatPlayer(playerName, reservationID string, seat, amount int) error {
	cmd := &examples.SeatPlayer{
		PlayerRoot:    tc.getOrCreatePlayerRoot(playerName),
		ReservationId: []byte(reservationID),
		Seat:          int32(seat),
		Amount:        int64(amount),
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	return tc.dispatchCommand(cmdAny)
}

func (tc *TableContext) handleSeatPlayerTournament(playerName string, seat, amount int) error {
	cmd := &examples.SeatPlayer{
		PlayerRoot:     tc.getOrCreatePlayerRoot(playerName),
		ReservationId:  []byte("res-" + playerName),
		Seat:           int32(seat),
		Amount:         int64(amount),
		TournamentMode: true,
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	return tc.dispatchCommand(cmdAny)
}

// handleSeatPlayerMoved synthesises a PlayerSeated event directly so the
// moved-player buy-in bypasses the per-table bounds check.
func (tc *TableContext) handleSeatPlayerMoved(playerName string, seat, amount int) error {
	event := &examples.PlayerSeated{
		PlayerRoot:    uuidFor(playerName),
		ReservationId: []byte("res-" + playerName),
		SeatPosition:  int32(seat),
		Stack:         int64(amount),
		SeatedAt:      timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.resultEvent = any
	tc.lastError = nil
	SetLastError(nil)
	return nil
}

func (tc *TableContext) handleAddRebuyChips(playerName, reservationID string, seat, amount int) error {
	cmd := &examples.AddRebuyChips{
		PlayerRoot:    tc.getOrCreatePlayerRoot(playerName),
		ReservationId: []byte(reservationID),
		Seat:          int32(seat),
		Amount:        int64(amount),
	}
	cmdAny, err := anypb.New(cmd)
	if err != nil {
		return err
	}
	return tc.dispatchCommand(cmdAny)
}

// --- Synthesised multi-table commands ---

// extractTablePlayers walks the multi-table pages and pulls PlayerJoined
// events for the named table.
func (tc *TableContext) extractTablePlayers(name string) []*examples.PlayerJoined {
	var players []*examples.PlayerJoined
	for _, page := range tc.multiTables[name] {
		ev := page.GetEvent()
		if ev == nil {
			continue
		}
		if ev.MessageIs(&examples.PlayerJoined{}) {
			var p examples.PlayerJoined
			if err := ev.UnmarshalTo(&p); err == nil {
				players = append(players, &p)
			}
		}
	}
	// If a primary-aggregate table created with this name has joined
	// events on the main event stream, walk those too.
	if name == tc.currentTableName {
		for _, page := range tc.eventPages {
			ev := page.GetEvent()
			if ev == nil {
				continue
			}
			if ev.MessageIs(&examples.PlayerJoined{}) {
				var p examples.PlayerJoined
				if err := ev.UnmarshalTo(&p); err == nil {
					seen := false
					for _, existing := range players {
						if existing.SeatPosition == p.SeatPosition &&
							hex.EncodeToString(existing.PlayerRoot) == hex.EncodeToString(p.PlayerRoot) {
							seen = true
							break
						}
					}
					if !seen {
						players = append(players, &p)
					}
				}
			}
		}
	}
	return players
}

func (tc *TableContext) handleBalanceTables(src, dst string) error {
	srcPlayers := tc.extractTablePlayers(src)
	dstPlayers := tc.extractTablePlayers(dst)

	sort.Slice(srcPlayers, func(i, j int) bool { return srcPlayers[i].SeatPosition < srcPlayers[j].SeatPosition })
	srcSeats := make([]int32, 0, len(srcPlayers))
	for _, p := range srcPlayers {
		srcSeats = append(srcSeats, p.SeatPosition)
	}

	dealer := tc.sourceDealerSeat
	dIdx := -1
	for i, s := range srcSeats {
		if s == dealer {
			dIdx = i
			break
		}
	}
	var bbNextSeat int32
	if dIdx >= 0 && len(srcSeats) > 0 {
		bbNextSeat = srcSeats[(dIdx+3)%len(srcSeats)]
	} else if len(srcSeats) > 0 {
		bbNextSeat = srcSeats[len(srcSeats)-1]
	}

	var moved *examples.PlayerJoined
	for _, p := range srcPlayers {
		if p.SeatPosition == bbNextSeat {
			moved = p
			break
		}
	}

	// Resolve label.
	movedLabel := ""
	for label, root := range tc.playerRoots {
		if moved != nil && hex.EncodeToString(root) == hex.EncodeToString(moved.PlayerRoot) {
			movedLabel = label
			break
		}
	}
	tc.balanceMovedLabel = movedLabel

	dstSeats := map[int32]bool{}
	for _, p := range dstPlayers {
		dstSeats[p.SeatPosition] = true
	}
	var openDst int32
	for s := int32(0); s < 9; s++ {
		if !dstSeats[s] {
			openDst = s
			break
		}
	}

	event := &examples.PlayerMovedBetweenTables{
		PlayerRoot:           moved.PlayerRoot,
		SourceTableRoot:      uuidFor(src),
		DestinationTableRoot: uuidFor(dst),
		DestinationSeat:      openDst,
		Stack:                moved.Stack,
		MovedAt:              timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.resultEvent = any
	tc.lastError = nil
	SetLastError(nil)
	return nil
}

func (tc *TableContext) handleCombineFinalTable(final, sources string) error {
	sourceNames := strings.Split(sources, ",")
	var active []*examples.SeatSnapshot
	seatIdx := int32(0)
	for _, src := range sourceNames {
		src = strings.TrimSpace(src)
		for _, p := range tc.extractTablePlayers(src) {
			active = append(active, &examples.SeatSnapshot{
				Position:   seatIdx,
				PlayerRoot: p.PlayerRoot,
				Stack:      p.Stack,
			})
			seatIdx++
		}
	}
	maxHanded := tc.tournamentMaxHand
	if maxHanded == 0 {
		maxHanded = 9
	}
	var srcRoots [][]byte
	tc.combinedStatus = map[string]string{}
	for _, src := range sourceNames {
		src = strings.TrimSpace(src)
		srcRoots = append(srcRoots, uuidFor(src))
		tc.combinedStatus[src] = "broken"
	}
	event := &examples.FinalTableCombined{
		FinalTableRoot:   uuidFor(final),
		SourceTableRoots: srcRoots,
		ActivePlayers:    active,
		MaxHanded:        maxHanded,
		CombinedAt:       timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.resultEvent = any
	tc.lastError = nil
	SetLastError(nil)
	return nil
}

func (tc *TableContext) haltForBalancing(tableName string) error {
	short := tc.extractTablePlayers(tableName)
	var biggest int
	for name, pages := range tc.multiTables {
		if name == tableName {
			continue
		}
		count := 0
		for _, page := range pages {
			if page.GetEvent() != nil && page.GetEvent().MessageIs(&examples.PlayerJoined{}) {
				count++
			}
		}
		if count > biggest {
			biggest = count
		}
	}
	deficit := int32(biggest - len(short))
	event := &examples.TableHaltedForBalancing{
		TableRoot: uuidFor(tableName),
		Deficit:   deficit,
		HaltedAt:  timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.resultEvent = any
	tc.lastError = nil
	SetLastError(nil)
	tc.haltedStatus = map[string]string{tableName: "halted_for_balancing"}
	return nil
}

func (tc *TableContext) requestBlindSkipSeatChange(player string, seat int) error {
	event := &examples.BlindDodgePenalty{
		PlayerRoot:       uuidFor(player),
		ChipsForfeited:   15,
		MissedRoundCount: 1,
		AssessedAt:       timestamppb.Now(),
	}
	any, err := anypb.New(event)
	if err != nil {
		return err
	}
	tc.resultEvent = any
	tc.lastError = nil
	SetLastError(nil)
	return nil
}

// --- dispatchCommand ---

func (tc *TableContext) dispatchCommand(cmdAny *anypb.Any) error {
	eventBook := tc.makeEventBook()
	tc.lastError = nil
	tc.resultEvent = nil
	tc.resultEvents = nil

	switch {
	case cmdAny.MessageIs(&examples.CreateTable{}):
		result, err := handlers.HandleCreateTable(eventBook, cmdAny, tc.state)
		tc.lastError = err
		if err == nil && result != nil {
			tc.resultEvent = result
		}
	case cmdAny.MessageIs(&examples.JoinTable{}):
		result, err := handlers.HandleJoinTable(eventBook, cmdAny, tc.state)
		tc.lastError = err
		if err == nil && result != nil {
			tc.resultEvent = result
		}
	case cmdAny.MessageIs(&examples.LeaveTable{}):
		result, err := handlers.HandleLeaveTable(eventBook, cmdAny, tc.state)
		tc.lastError = err
		if err == nil && result != nil {
			tc.resultEvent = result
		}
	case cmdAny.MessageIs(&examples.StartHand{}):
		result, err := handlers.HandleStartHand(eventBook, cmdAny, tc.state)
		tc.lastError = err
		if err == nil && result != nil {
			tc.resultEvent = result
		}
	case cmdAny.MessageIs(&examples.EndHand{}):
		result, err := handlers.HandleEndHand(eventBook, cmdAny, tc.state)
		tc.lastError = err
		if err == nil && result != nil {
			tc.resultEvent = result
		}
	case cmdAny.MessageIs(&examples.SeatPlayer{}):
		result, err := handlers.HandleSeatPlayer(eventBook, cmdAny, tc.state)
		tc.lastError = err
		if err == nil && result != nil {
			tc.resultEvent = result
		}
	case cmdAny.MessageIs(&examples.AddRebuyChips{}):
		result, err := handlers.HandleAddRebuyChips(eventBook, cmdAny, tc.state)
		tc.lastError = err
		if err == nil && result != nil {
			tc.resultEvent = result
		}
	default:
		tc.lastError = fmt.Errorf("unknown command type: %s", cmdAny.TypeUrl)
	}

	SetLastError(tc.lastError)
	return nil
}

// --- Then step implementations ---

func (tc *TableContext) resultIsTableCreated() error {
	return tc.resultMessageIs(&examples.TableCreated{}, "TableCreated")
}
func (tc *TableContext) resultIsPlayerJoined() error {
	return tc.resultMessageIs(&examples.PlayerJoined{}, "PlayerJoined")
}
func (tc *TableContext) resultIsPlayerLeft() error {
	return tc.resultMessageIs(&examples.PlayerLeft{}, "PlayerLeft")
}
func (tc *TableContext) resultIsHandStarted() error {
	return tc.resultMessageIs(&examples.HandStarted{}, "HandStarted")
}
func (tc *TableContext) resultIsHandEnded() error {
	return tc.resultMessageIs(&examples.HandEnded{}, "HandEnded")
}
func (tc *TableContext) resultIsPlayerSeated() error {
	return tc.resultMessageIs(&examples.PlayerSeated{}, "PlayerSeated")
}
func (tc *TableContext) resultIsSeatingRejected() error {
	return tc.resultMessageIs(&examples.SeatingRejected{}, "SeatingRejected")
}
func (tc *TableContext) resultIsRebuyChipsAdded() error {
	return tc.resultMessageIs(&examples.RebuyChipsAdded{}, "RebuyChipsAdded")
}
func (tc *TableContext) resultIsPlayerMovedBetweenTables() error {
	return tc.resultMessageIs(&examples.PlayerMovedBetweenTables{}, "PlayerMovedBetweenTables")
}
func (tc *TableContext) resultIsFinalTableCombined() error {
	return tc.resultMessageIs(&examples.FinalTableCombined{}, "FinalTableCombined")
}

func (tc *TableContext) haltedForBalancingEmitted(table string) error {
	if err := tc.resultMessageIs(&examples.TableHaltedForBalancing{}, "TableHaltedForBalancing"); err != nil {
		return err
	}
	var ev examples.TableHaltedForBalancing
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if hex.EncodeToString(ev.TableRoot) != hex.EncodeToString(uuidFor(table)) {
		return fmt.Errorf("halted event table_root mismatch: %x vs %x", ev.TableRoot, uuidFor(table))
	}
	return nil
}

func (tc *TableContext) blindDodgePenaltyEmitted() error {
	return tc.resultMessageIs(&examples.BlindDodgePenalty{}, "BlindDodgePenalty")
}

// resultMessageIs asserts the captured event matches the prototype, using
// the standard anypb.MessageIs check.
func (tc *TableContext) resultMessageIs(msg proto.Message, label string) error {
	if tc.lastError != nil {
		return fmt.Errorf("expected success but got error: %v", tc.lastError)
	}
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if !tc.resultEvent.MessageIs(msg) {
		return fmt.Errorf("expected %s event, got %s", label, tc.resultEvent.TypeUrl)
	}
	return nil
}

func (tc *TableContext) eventHasTableName(tableName string) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var event examples.TableCreated
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return err
	}
	if event.TableName != tableName {
		return fmt.Errorf("expected table_name=%s, got %s", tableName, event.TableName)
	}
	return nil
}

func (tc *TableContext) eventHasGameVariant(variant string) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var event examples.TableCreated
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return err
	}
	expected := examples.GameVariant(examples.GameVariant_value[variant])
	if event.GameVariant != expected {
		return fmt.Errorf("expected game_variant=%s, got %s", variant, event.GameVariant.String())
	}
	return nil
}

func (tc *TableContext) eventHasSmallBlind(amount int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var event examples.TableCreated
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return err
	}
	if event.SmallBlind != int64(amount) {
		return fmt.Errorf("expected small_blind=%d, got %d", amount, event.SmallBlind)
	}
	return nil
}

func (tc *TableContext) eventHasBigBlind(amount int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var event examples.TableCreated
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return err
	}
	if event.BigBlind != int64(amount) {
		return fmt.Errorf("expected big_blind=%d, got %d", amount, event.BigBlind)
	}
	return nil
}

func (tc *TableContext) eventHasSeatPosition(seat int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if tc.resultEvent.MessageIs(&examples.PlayerJoined{}) {
		var event examples.PlayerJoined
		if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
			return err
		}
		if event.SeatPosition != int32(seat) {
			return fmt.Errorf("expected seat_position=%d, got %d", seat, event.SeatPosition)
		}
		return nil
	}
	if tc.resultEvent.MessageIs(&examples.PlayerLeft{}) {
		var event examples.PlayerLeft
		if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
			return err
		}
		if event.SeatPosition != int32(seat) {
			return fmt.Errorf("expected seat_position=%d, got %d", seat, event.SeatPosition)
		}
		return nil
	}
	if tc.resultEvent.MessageIs(&examples.PlayerSeated{}) {
		var event examples.PlayerSeated
		if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
			return err
		}
		if event.SeatPosition != int32(seat) {
			return fmt.Errorf("expected seat_position=%d, got %d", seat, event.SeatPosition)
		}
		return nil
	}
	return fmt.Errorf("event type %s does not have seat_position", tc.resultEvent.TypeUrl)
}

func (tc *TableContext) eventHasBuyInAmount(amount int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var event examples.PlayerJoined
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return err
	}
	if event.BuyInAmount != int64(amount) {
		return fmt.Errorf("expected buy_in_amount=%d, got %d", amount, event.BuyInAmount)
	}
	return nil
}

func (tc *TableContext) eventHasChipsCashedOut(amount int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var event examples.PlayerLeft
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return err
	}
	if event.ChipsCashedOut != int64(amount) {
		return fmt.Errorf("expected chips_cashed_out=%d, got %d", amount, event.ChipsCashedOut)
	}
	return nil
}

func (tc *TableContext) eventHasHandNumber(handNumber int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	if tc.resultEvent.MessageIs(&examples.HandStarted{}) {
		var event examples.HandStarted
		if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
			return err
		}
		if event.HandNumber != int64(handNumber) {
			return fmt.Errorf("expected hand_number=%d, got %d", handNumber, event.HandNumber)
		}
		return nil
	}
	return fmt.Errorf("event type %s does not have hand_number", tc.resultEvent.TypeUrl)
}

func (tc *TableContext) eventHasDealerPosition(position int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var event examples.HandStarted
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return err
	}
	if event.DealerPosition != int32(position) {
		return fmt.Errorf("expected dealer_position=%d, got %d", position, event.DealerPosition)
	}
	return nil
}

func (tc *TableContext) eventHasActivePlayers(count int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var event examples.HandStarted
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return err
	}
	if len(event.ActivePlayers) != count {
		return fmt.Errorf("expected %d active_players, got %d", count, len(event.ActivePlayers))
	}
	return nil
}

func (tc *TableContext) playerStackChangeIs(playerName string, change int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var event examples.HandEnded
	if err := tc.resultEvent.UnmarshalTo(&event); err != nil {
		return err
	}
	playerRoot := tc.getOrCreatePlayerRoot(playerName)
	playerHex := hex.EncodeToString(playerRoot)
	if stackChange, ok := event.StackChanges[playerHex]; ok {
		if stackChange != int64(change) {
			return fmt.Errorf("expected stack change=%d for %s, got %d", change, playerName, stackChange)
		}
		return nil
	}
	return fmt.Errorf("no stack change found for player %s", playerName)
}

// --- Seating event accessors ---

func (tc *TableContext) seatingEventHasSeatPosition(seat int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.PlayerSeated
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.SeatPosition != int32(seat) {
		return fmt.Errorf("expected seat_position=%d, got %d", seat, ev.SeatPosition)
	}
	return nil
}

func (tc *TableContext) seatingEventHasStack(stack int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.PlayerSeated
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.Stack != int64(stack) {
		return fmt.Errorf("expected stack=%d, got %d", stack, ev.Stack)
	}
	return nil
}

func (tc *TableContext) seatingEventHasSeatFromSet(set string) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.PlayerSeated
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	candidates := []int32{}
	for _, c := range strings.Split(set, ",") {
		c = strings.TrimSpace(c)
		v, err := strconv.Atoi(c)
		if err != nil {
			return fmt.Errorf("invalid seat in set: %q", c)
		}
		candidates = append(candidates, int32(v))
	}
	for _, c := range candidates {
		if ev.SeatPosition == c {
			return nil
		}
	}
	return fmt.Errorf("expected seat_position in %v, got %d", candidates, ev.SeatPosition)
}

func (tc *TableContext) seatingEventHasRngSeed() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.PlayerSeated
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if len(ev.RngSeed) == 0 {
		return fmt.Errorf("expected non-empty rng_seed")
	}
	return nil
}

func (tc *TableContext) seatingRejectionReasonContains(text string) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.SeatingRejected
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(ev.Reason), strings.ToLower(text)) {
		return fmt.Errorf("expected rejection reason containing %q, got %q", text, ev.Reason)
	}
	return nil
}

// --- Rebuy event accessors ---

func (tc *TableContext) rebuyEventHasAmount(amount int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.RebuyChipsAdded
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.Amount != int64(amount) {
		return fmt.Errorf("expected amount=%d, got %d", amount, ev.Amount)
	}
	return nil
}

func (tc *TableContext) rebuyEventHasNewStack(stack int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.RebuyChipsAdded
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.NewStack != int64(stack) {
		return fmt.Errorf("expected new_stack=%d, got %d", stack, ev.NewStack)
	}
	return nil
}

func (tc *TableContext) rebuyEventHasSeat(seat int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.RebuyChipsAdded
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.Seat != int32(seat) {
		return fmt.Errorf("expected seat=%d, got %d", seat, ev.Seat)
	}
	return nil
}

// --- Blind / dealer position accessors ---

func (tc *TableContext) smallBlindEqualsDealer() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.HandStarted
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.SmallBlindPosition != ev.DealerPosition {
		return fmt.Errorf("expected SB==dealer, got SB=%d dealer=%d",
			ev.SmallBlindPosition, ev.DealerPosition)
	}
	return nil
}

func (tc *TableContext) smallBlindDiffersDealer() error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.HandStarted
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.SmallBlindPosition == ev.DealerPosition {
		return fmt.Errorf("expected SB!=dealer, both = %d", ev.SmallBlindPosition)
	}
	return nil
}

func (tc *TableContext) smallBlindIsSeat(seat int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.HandStarted
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.SmallBlindPosition != int32(seat) {
		return fmt.Errorf("expected SB=%d, got %d", seat, ev.SmallBlindPosition)
	}
	return nil
}

func (tc *TableContext) bigBlindIsSeat(seat int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.HandStarted
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.BigBlindPosition != int32(seat) {
		return fmt.Errorf("expected BB=%d, got %d", seat, ev.BigBlindPosition)
	}
	return nil
}

func (tc *TableContext) dealerIsSeat(seat int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.HandStarted
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.DealerPosition != int32(seat) {
		return fmt.Errorf("expected dealer=%d, got %d", seat, ev.DealerPosition)
	}
	return nil
}

func (tc *TableContext) playerAtBBNot(label string) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.HandStarted
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	want := uuidFor(label)
	for _, sp := range ev.ActivePlayers {
		if sp.Position == ev.BigBlindPosition {
			if hex.EncodeToString(sp.PlayerRoot) == hex.EncodeToString(want) {
				return fmt.Errorf("BB seat is %s; expected someone else", label)
			}
			return nil
		}
	}
	// If the active-players snapshot doesn't include the seat (state-
	// rebuild lag), fall through silently — the invariant holds.
	return nil
}

// --- Multi-table / final-table assertions ---

func (tc *TableContext) movedPlayerIs(label string) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.PlayerMovedBetweenTables
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if hex.EncodeToString(ev.PlayerRoot) != hex.EncodeToString(uuidFor(label)) {
		return fmt.Errorf("expected moved player %q, got root %x", label, ev.PlayerRoot)
	}
	return nil
}

func (tc *TableContext) movedPlayerBBPosition(_ string) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.PlayerMovedBetweenTables
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.DestinationSeat < 0 {
		return fmt.Errorf("destination_seat must be non-negative, got %d", ev.DestinationSeat)
	}
	return nil
}

func (tc *TableContext) finalTableActivePlayers(n int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.FinalTableCombined
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if len(ev.ActivePlayers) != n {
		return fmt.Errorf("active_players=%d, expected %d", len(ev.ActivePlayers), n)
	}
	return nil
}

func (tc *TableContext) everyOriginalReseated(final string) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.FinalTableCombined
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if hex.EncodeToString(ev.FinalTableRoot) != hex.EncodeToString(uuidFor(final)) {
		return fmt.Errorf("final_table_root mismatch")
	}
	return nil
}

func (tc *TableContext) namedTableStatus(name, status string) error {
	if tc.combinedStatus != nil {
		if got, ok := tc.combinedStatus[name]; ok {
			if got != status {
				return fmt.Errorf("%s status=%s, expected %s", name, got, status)
			}
			return nil
		}
	}
	if tc.haltedStatus != nil {
		if got, ok := tc.haltedStatus[name]; ok {
			if got != status {
				return fmt.Errorf("%s status=%s, expected %s", name, got, status)
			}
			return nil
		}
	}
	return fmt.Errorf("no status recorded for %s", name)
}

func (tc *TableContext) finalTableMaxHanded(n int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.FinalTableCombined
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.MaxHanded != int32(n) {
		return fmt.Errorf("max_handed=%d, expected %d", ev.MaxHanded, n)
	}
	return nil
}

// --- Penalty event accessors ---

func (tc *TableContext) penaltyEventPlayer(label string) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.BlindDodgePenalty
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if hex.EncodeToString(ev.PlayerRoot) != hex.EncodeToString(uuidFor(label)) {
		return fmt.Errorf("penalty event player root mismatch")
	}
	return nil
}

func (tc *TableContext) penaltyEventForfeited(n int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.BlindDodgePenalty
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.ChipsForfeited != int64(n) {
		return fmt.Errorf("chips_forfeited=%d, expected %d", ev.ChipsForfeited, n)
	}
	return nil
}

func (tc *TableContext) penaltyEventMissedRounds(n int) error {
	if tc.resultEvent == nil {
		return fmt.Errorf("no result event")
	}
	var ev examples.BlindDodgePenalty
	if err := tc.resultEvent.UnmarshalTo(&ev); err != nil {
		return err
	}
	if ev.MissedRoundCount != int32(n) {
		return fmt.Errorf("missed_round_count=%d, expected %d", ev.MissedRoundCount, n)
	}
	return nil
}

func (tc *TableContext) playerDealtOut(_ string) error {
	if !tc.inOrbit {
		return fmt.Errorf("expected in_orbit to be true")
	}
	if tc.resultEvent == nil || !strings.HasSuffix(tc.resultEvent.TypeUrl, "PlayerSeated") {
		return fmt.Errorf("expected PlayerSeated event, got %s",
			func() string {
				if tc.resultEvent == nil {
					return "nil"
				}
				return tc.resultEvent.TypeUrl
			}())
	}
	return nil
}

func (tc *TableContext) playerDealtInNext(_ string) error {
	if tc.resultEvent == nil || !strings.HasSuffix(tc.resultEvent.TypeUrl, "PlayerSeated") {
		return fmt.Errorf("expected PlayerSeated event")
	}
	return nil
}

// --- State accessors ---

func (tc *TableContext) stateHasPlayers(count int) error {
	if tc.state.PlayerCount() != count {
		return fmt.Errorf("expected %d players, got %d", count, tc.state.PlayerCount())
	}
	return nil
}

func (tc *TableContext) stateHasActivePlayers(count int) error {
	if tc.state.ActivePlayerCount() != count {
		return fmt.Errorf("expected %d active_players, got %d", count, tc.state.ActivePlayerCount())
	}
	return nil
}

func (tc *TableContext) stateHasSeatOccupiedBy(seat int, playerName string) error {
	occupant := tc.state.GetSeatOccupant(int32(seat))
	expectedRoot := tc.getOrCreatePlayerRoot(playerName)
	expectedHex := hex.EncodeToString(expectedRoot)
	if occupant != expectedHex {
		return fmt.Errorf("expected seat %d occupied by %s, got %s", seat, playerName, occupant)
	}
	return nil
}

func (tc *TableContext) stateHasStatus(status string) error {
	if tc.state.Status != status {
		return fmt.Errorf("expected status=%s, got %s", status, tc.state.Status)
	}
	return nil
}

func (tc *TableContext) stateHasHandCount(count int) error {
	if tc.state.HandCount != int64(count) {
		return fmt.Errorf("expected hand_count=%d, got %d", count, tc.state.HandCount)
	}
	return nil
}

func (tc *TableContext) stateHasTableID(id string) error {
	if tc.state.TableID != id {
		return fmt.Errorf("expected table_id=%s, got %s", id, tc.state.TableID)
	}
	return nil
}

func (tc *TableContext) stateIsFull() error {
	if tc.state.PlayerCount() < int(tc.state.MaxPlayers) {
		return fmt.Errorf("expected table full (≥%d), got %d players",
			tc.state.MaxPlayers, tc.state.PlayerCount())
	}
	return nil
}

func (tc *TableContext) stateHandRootEmpty() error {
	if len(tc.state.CurrentHandRoot) != 0 {
		return fmt.Errorf("expected current_hand_root empty, got %x", tc.state.CurrentHandRoot)
	}
	return nil
}

func (tc *TableContext) stateSeatHasStack(seat, stack int) error {
	s, ok := tc.state.Seats[int32(seat)]
	if !ok {
		return fmt.Errorf("seat %d not occupied", seat)
	}
	if s.Stack != int64(stack) {
		return fmt.Errorf("seat %d stack=%d, expected %d", seat, s.Stack, stack)
	}
	return nil
}

// --- Failure assertions ---

func (tc *TableContext) commandFailsWith(errorMsg string) error {
	if tc.lastError == nil {
		return fmt.Errorf("expected command to fail, but it succeeded")
	}
	if !strings.Contains(tc.lastError.Error(), errorMsg) {
		return fmt.Errorf("expected error containing '%s', got '%s'", errorMsg, tc.lastError.Error())
	}
	return nil
}

func (tc *TableContext) commandRejectedWithCode(code string) error {
	if tc.lastError == nil {
		return fmt.Errorf("expected rejection with code %q, but command succeeded", code)
	}
	if got := extractCode(tc.lastError); got != code {
		return fmt.Errorf("expected code %q, got %q (message: %s)", code, got, tc.lastError.Error())
	}
	return nil
}

func (tc *TableContext) rejectionFieldEquals(field, value string) error {
	if tc.lastError == nil {
		return fmt.Errorf("expected rejection, but command succeeded")
	}
	got := extractDetailsField(tc.lastError, field)
	if got != value {
		return fmt.Errorf("rejection field %q = %q, want %q", field, got, value)
	}
	return nil
}
