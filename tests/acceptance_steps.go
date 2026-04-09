//go:build acceptance

package tests

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"sync"

	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"github.com/cucumber/godog"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/anypb"
)

// AcceptanceContext holds shared state for acceptance test scenarios.
type AcceptanceContext struct {
	mu        sync.Mutex
	client    CommandClient
	players   map[string]*playerRecord // name -> player record
	tables    map[string]*tableRecord  // name -> table record
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
	return t
}

// InitAcceptanceSteps registers acceptance step definitions that use CommandClient.
func InitAcceptanceSteps(ctx *godog.ScenarioContext) {
	ac := newAcceptanceContext()

	ctx.Before(func(c context.Context, sc *godog.Scenario) (context.Context, error) {
		ac.players = make(map[string]*playerRecord)
		ac.tables = make(map[string]*tableRecord)
		ac.lastError = nil
		ac.lastResp = nil
		return c, nil
	})

	ctx.After(func(c context.Context, sc *godog.Scenario, err error) (context.Context, error) {
		return c, nil
	})

	// Background
	ctx.Step(`^the poker system is running in standalone mode$`, ac.systemRunning)

	// Player steps
	ctx.Step(`^I register player "([^"]*)" with email "([^"]*)"$`, ac.registerPlayer)
	ctx.Step(`^I deposit (\d+) chips to player "([^"]*)"$`, ac.depositChips)
	ctx.Step(`^player "([^"]*)" has bankroll (\d+)$`, ac.playerHasBankroll)
	ctx.Step(`^player "([^"]*)" has available balance (\d+)$`, ac.playerHasAvailableBalance)
	ctx.Step(`^player "([^"]*)" has reserved funds (\d+)$`, ac.playerHasReservedFunds)
	ctx.Step(`^registered players with bankroll:$`, ac.registeredPlayersWithBankroll)

	// Table steps
	ctx.Step(`^I create a Texas Hold\'em table "([^"]*)" with blinds (\d+)/(\d+)$`, ac.createTexasHoldemTable)
	ctx.Step(`^player "([^"]*)" joins table "([^"]*)" at seat (\d+) with buy-in (\d+)$`, ac.playerJoinsTable)
	ctx.Step(`^table "([^"]*)" has (\d+) seated players?$`, ac.tableHasSeatedPlayers)
	ctx.Step(`^a table "([^"]*)" with seated players:$`, ac.tableWithSeatedPlayers)
	ctx.Step(`^player "([^"]*)" leaves table "([^"]*)"$`, ac.playerLeavesTable)
}

func (ac *AcceptanceContext) systemRunning() error {
	// In-process mode is always "running"; for gRPC mode, the system must be up.
	return nil
}

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
	p.sequence++
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

	resp, err := ac.client.SendCommand("player", p.root, cmdAny, p.sequence)
	ac.lastResp = resp
	ac.lastError = err
	if err != nil {
		return nil
	}
	p.sequence++
	return nil
}

func (ac *AcceptanceContext) playerHasBankroll(name string, expected int) error {
	if ac.lastError != nil {
		return fmt.Errorf("previous command failed: %v", ac.lastError)
	}
	// For in-process client: rebuild state from aggregate history.
	// For gRPC client: the response events tell us the current state.
	// Check the last response events for deposit/register events to extract balance.
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
	// Fallback: send a no-op query or accept (for in-process, we can check state).
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerHasAvailableBalance(name string, expected int) error {
	// Available balance = bankroll - reserved. For now, check response events.
	return godog.ErrPending
}

func (ac *AcceptanceContext) playerHasReservedFunds(name string, expected int) error {
	return godog.ErrPending
}

func (ac *AcceptanceContext) registeredPlayersWithBankroll(table *godog.Table) error {
	for _, row := range table.Rows[1:] { // Skip header
		name := row.Cells[0].Value
		bankroll := parseInt64(row.Cells[1].Value)

		// Register
		if err := ac.registerPlayer(name, fmt.Sprintf("%s@example.com", name)); err != nil {
			return err
		}
		if ac.lastError != nil {
			return fmt.Errorf("failed to register %s: %v", name, ac.lastError)
		}

		// Deposit
		if err := ac.depositChips(int(bankroll), name); err != nil {
			return err
		}
		if ac.lastError != nil {
			return fmt.Errorf("failed to deposit for %s: %v", name, ac.lastError)
		}
	}
	return nil
}

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
	t.sequence++
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
	t.sequence++
	return nil
}

func (ac *AcceptanceContext) tableHasSeatedPlayers(tableName string, count int) error {
	// For in-process: rebuild table state. For gRPC: query projection.
	return godog.ErrPending
}

func (ac *AcceptanceContext) tableWithSeatedPlayers(tableName string, table *godog.Table) error {
	// Determine the minimum stack to set min_buy_in appropriately.
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

	// Create table with min_buy_in that accommodates all stacks.
	t := ac.getOrCreateTable(tableName)
	cmd := &examples.CreateTable{
		TableName:            tableName,
		GameVariant:          examples.GameVariant_TEXAS_HOLDEM,
		SmallBlind:           5,
		BigBlind:             10,
		MinBuyIn:             minStack, // Allow the smallest stack as buy-in
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
	t.sequence++

	for _, row := range table.Rows[1:] {
		name := row.Cells[0].Value
		seat := int(parseInt32(row.Cells[1].Value))
		stack := int(parseInt64(row.Cells[2].Value))

		// Register player if not already registered
		if _, exists := ac.players[name]; !exists {
			if err := ac.registerPlayer(name, fmt.Sprintf("%s@example.com", name)); err != nil {
				return err
			}
			if ac.lastError != nil {
				return fmt.Errorf("failed to register %s: %v", name, ac.lastError)
			}
			// Deposit enough funds
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
	t.sequence++
	return nil
}

// Ensure hex import is used.
func init() {
	_ = hex.EncodeToString([]byte{})
}
