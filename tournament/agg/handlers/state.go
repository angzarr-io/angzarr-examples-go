// Package handlers implements tournament aggregate command handlers.
//
// The tournament aggregate manages tournament lifecycle, player registrations,
// blind level progression, player eliminations, and the advanced flow
// (hand-for-hand, color-up, rebalance, penalty, bag-and-tag, bounties,
// no-shows, etc.). Handlers follow the guard/validate/compute pattern for
// testability — every state mutation goes through an event applier so the
// aggregate is reconstructible from its event log.
//
// Layout mirrors Py `tournament/agg/handlers.py` + Rs
// `tournament/agg/src/state.rs` and `tournament/agg/src/handlers/*.rs`.
package handlers

import (
	"encoding/hex"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr/v1"
	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
)

// TournamentState represents the current state of a tournament aggregate.
//
// Mirrors `examples-python/main/tournament/agg/state.py::TournamentState`
// + the extended fields on `tournament/agg/handlers.py::_TournamentState`
// (Py registration_open, registration_cutoff_level, payout_structure,
// hand_for_hand_*, level_seconds_remaining, simultaneous_bust_groups).
type TournamentState struct {
	// --- identity / config ---
	TournamentID   string
	Name           string
	GameVariant    examples.GameVariant
	Status         examples.TournamentStatus
	BuyIn          int64
	StartingStack  int64
	MaxPlayers     int32
	MinPlayers     int32
	RebuyConfig    *examples.RebuyConfig
	BlindStructure []*examples.BlindLevel
	CurrentLevel   int32

	// --- player register ---
	RegisteredPlayers map[string]*examples.PlayerRegistration // player_root_hex -> registration
	PlayersRemaining  int32
	TotalPrizePool    int64

	// --- registration lifecycle (separate from Status to support late
	// registration per TDA Rule 30 / WSOP Rule 14) ---
	RegistrationOpen        bool
	RegistrationCutoffLevel int32 // 0 = no auto-close

	// --- payout schedule (WSOP §III-31) ---
	PayoutStructure []*examples.PayoutPosition

	// --- hand-for-hand bookkeeping (TDA RP-8) ---
	HandForHand              bool
	HandForHandRound         int32
	HandForHandPendingTables map[string]bool // table_root_hex -> pending
	HandForHandActiveTables  map[string]bool // table_root_hex -> active
	// Per-level clock countdown. Decremented by HandForHandHandRecorded
	// events (TDA RP-8B/8C).
	LevelSecondsRemaining int32

	// --- penalty register (TDA Rule 71 / WSOP Rule 113) ---
	ActivePenalties map[string]int32 // player_root_hex -> rounds_remaining

	// --- chip ledger (TDA Rule 71D / WSOP Rule 114) ---
	TotalChipsInPlay    int64
	PlayerStacks        map[string]int64 // player_root_hex -> chip count
	DisqualifiedPlayers map[string]bool
	NoShowPlayers       map[string]bool

	// --- day-end ---
	NewHandsHalted bool
	BagSnapshots   []*examples.PlayerBagSnapshot

	// --- mixed-game (HORSE) rotation (TDA RP-18) ---
	MixedGameIndex int32

	// --- bounty tournaments (TDA RP-22 / WSOP Rule 39) ---
	BountyTotals map[string]int64 // eliminator_root_hex -> total_amount

	// --- simultaneous-bust groups recorded during H4H, consumed by
	// CompleteTournament to split the next paid position(s) (TDA RP-8A) ---
	SimultaneousBustGroups []SimultaneousBustGroup

	// --- WSOP Rule 126b — same-table pre-hand stack tiebreak input ---
	LastPreHandStacks map[string]int64 // player_root_hex -> stack at start of last H4H hand
}

// SimultaneousBustGroup is one recorded simultaneous-bust event during H4H.
// Used by CompleteTournament to split the appropriate paid position(s).
type SimultaneousBustGroup struct {
	PlayerRootHexes []string         // sorted for determinism
	SameTable       bool             // WSOP 126b applies when true
	PreHandStacks   map[string]int64 // player_root_hex -> chip count at hand start
}

// NewTournamentState creates an empty TournamentState with maps initialised.
func NewTournamentState() TournamentState {
	return TournamentState{
		CurrentLevel:             1,
		RegisteredPlayers:        make(map[string]*examples.PlayerRegistration),
		HandForHandPendingTables: make(map[string]bool),
		HandForHandActiveTables:  make(map[string]bool),
		ActivePenalties:          make(map[string]int32),
		PlayerStacks:             make(map[string]int64),
		DisqualifiedPlayers:      make(map[string]bool),
		NoShowPlayers:            make(map[string]bool),
		BountyTotals:             make(map[string]int64),
		LastPreHandStacks:        make(map[string]int64),
	}
}

// --- State accessors ---

// Exists returns true once a TournamentCreated event has been applied.
func (s TournamentState) Exists() bool { return s.Name != "" }

// IsRegistrationOpen returns the explicit registration_open flag. Mirrors
// Py's separation of Status vs registration_open so late registration can
// stay open into a Running tournament.
func (s TournamentState) IsRegistrationOpen() bool { return s.RegistrationOpen }

// IsRunning returns true when status == TOURNAMENT_RUNNING.
func (s TournamentState) IsRunning() bool {
	return s.Status == examples.TournamentStatus_TOURNAMENT_RUNNING
}

// IsPaused returns true when status == TOURNAMENT_PAUSED.
func (s TournamentState) IsPaused() bool {
	return s.Status == examples.TournamentStatus_TOURNAMENT_PAUSED
}

// IsCompleted returns true when status == TOURNAMENT_COMPLETED.
func (s TournamentState) IsCompleted() bool {
	return s.Status == examples.TournamentStatus_TOURNAMENT_COMPLETED
}

// IsFull returns true once registered player count reaches MaxPlayers.
func (s TournamentState) IsFull() bool {
	return int32(len(s.RegisteredPlayers)) >= s.MaxPlayers
}

// IsPlayerRegistered returns true when the player root is in the register.
func (s TournamentState) IsPlayerRegistered(playerRootHex string) bool {
	_, exists := s.RegisteredPlayers[playerRootHex]
	return exists
}

// PlayerRebuyCount returns the number of rebuys the player has used.
func (s TournamentState) PlayerRebuyCount(playerRootHex string) int32 {
	reg, exists := s.RegisteredPlayers[playerRootHex]
	if !exists {
		return 0
	}
	return reg.RebuysUsed
}

// IsOnPenalty returns true when the player has rounds remaining.
func (s TournamentState) IsOnPenalty(playerRootHex string) bool {
	_, ok := s.ActivePenalties[playerRootHex]
	return ok
}

// IsDisqualified returns true when the player has been DQ'd.
func (s TournamentState) IsDisqualified(playerRootHex string) bool {
	return s.DisqualifiedPlayers[playerRootHex]
}

// IsNoShow returns true when the player has been marked a no-show.
func (s TournamentState) IsNoShow(playerRootHex string) bool {
	return s.NoShowPlayers[playerRootHex]
}

// MaxBlindLevel returns the highest defined level in the blind structure;
// 0 means no structure was supplied.
func (s TournamentState) MaxBlindLevel() int32 {
	return int32(len(s.BlindStructure))
}

// --- Event appliers (mirror Py apply_* functions; pure mutations on
//                    pointer-receiver state). Each is registered in
//                    stateRouter below. ---

func applyCreated(state *TournamentState, event *examples.TournamentCreated) {
	state.TournamentID = "tournament_" + event.Name
	state.Name = event.Name
	state.GameVariant = event.GameVariant
	state.Status = examples.TournamentStatus_TOURNAMENT_CREATED
	state.BuyIn = event.BuyIn
	state.StartingStack = event.StartingStack
	state.MaxPlayers = event.MaxPlayers
	state.MinPlayers = event.MinPlayers
	state.RebuyConfig = event.RebuyConfig
	state.BlindStructure = event.BlindStructure
	state.CurrentLevel = 1
	state.PlayersRemaining = 0
	state.TotalPrizePool = 0
	state.RegistrationCutoffLevel = event.RegistrationCutoffLevel
	state.PayoutStructure = event.PayoutStructure
}

func applyRegistrationOpened(state *TournamentState, _ *examples.RegistrationOpened) {
	state.RegistrationOpen = true
	if state.Status == examples.TournamentStatus_TOURNAMENT_CREATED {
		state.Status = examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN
	}
}

func applyRegistrationClosed(state *TournamentState, _ *examples.RegistrationClosed) {
	state.RegistrationOpen = false
}

func applyPlayerEnrolled(state *TournamentState, event *examples.TournamentPlayerEnrolled) {
	rootHex := hex.EncodeToString(event.PlayerRoot)
	state.RegisteredPlayers[rootHex] = &examples.PlayerRegistration{
		PlayerRoot:    event.PlayerRoot,
		FeePaid:       event.FeePaid,
		StartingStack: event.StartingStack,
	}
	state.TotalPrizePool += event.FeePaid
	state.PlayersRemaining = int32(len(state.RegisteredPlayers))
	state.PlayerStacks[rootHex] = event.StartingStack
	state.TotalChipsInPlay += event.StartingStack
}

func applyEnrollmentRejected(_ *TournamentState, _ *examples.TournamentEnrollmentRejected) {
	// no state change
}

func applyTournamentStarted(state *TournamentState, _ *examples.TournamentStarted) {
	state.Status = examples.TournamentStatus_TOURNAMENT_RUNNING
}

func applyRebuyProcessed(state *TournamentState, event *examples.RebuyProcessed) {
	rootHex := hex.EncodeToString(event.PlayerRoot)
	if reg, ok := state.RegisteredPlayers[rootHex]; ok {
		reg.RebuysUsed = event.RebuyCount
	}
	state.TotalPrizePool += event.RebuyCost
	state.PlayerStacks[rootHex] += event.ChipsAdded
	state.TotalChipsInPlay += event.ChipsAdded
}

func applyRebuyDenied(_ *TournamentState, _ *examples.RebuyDenied) {
	// no state change
}

func applyBlindAdvanced(state *TournamentState, event *examples.BlindLevelAdvanced) {
	state.CurrentLevel = event.Level
}

func applyPlayerEliminated(state *TournamentState, event *examples.PlayerEliminated) {
	rootHex := hex.EncodeToString(event.PlayerRoot)
	delete(state.RegisteredPlayers, rootHex)
	delete(state.PlayerStacks, rootHex)
	state.PlayersRemaining = int32(len(state.RegisteredPlayers))
}

func applyPaused(state *TournamentState, _ *examples.TournamentPaused) {
	state.Status = examples.TournamentStatus_TOURNAMENT_PAUSED
}

func applyResumed(state *TournamentState, _ *examples.TournamentResumed) {
	state.Status = examples.TournamentStatus_TOURNAMENT_RUNNING
}

func applyCompleted(state *TournamentState, _ *examples.TournamentCompleted) {
	state.Status = examples.TournamentStatus_TOURNAMENT_COMPLETED
}

// --- Advanced appliers (mirror Py advanced apply_* and Rs
//                       tournament/agg/src/state.rs) ---

func applyHandForHandStarted(state *TournamentState, _ *examples.HandForHandStarted) {
	state.HandForHand = true
	state.HandForHandRound = 0
}

func applyHandForHandRoundComplete(state *TournamentState, event *examples.HandForHandRoundComplete) {
	state.HandForHandRound = event.RoundNumber
}

func applyHandForHandEnded(state *TournamentState, _ *examples.HandForHandEnded) {
	state.HandForHand = false
	state.HandForHandRound = 0
	for k := range state.HandForHandActiveTables {
		delete(state.HandForHandActiveTables, k)
	}
	for k := range state.HandForHandPendingTables {
		delete(state.HandForHandPendingTables, k)
	}
}

func applyHandForHandHandRecorded(state *TournamentState, event *examples.HandForHandHandRecorded) {
	// Decrement clock by the (already-capped) per-hand deduction.
	state.LevelSecondsRemaining -= event.ClockSecondsDeducted
	if state.LevelSecondsRemaining < 0 {
		state.LevelSecondsRemaining = 0
	}
}

func applyColorUpCompleted(state *TournamentState, event *examples.ColorUpCompleted) {
	// Conservation: chip-economy delta is encoded on the event itself.
	state.TotalChipsInPlay += event.ChipsAddedByRescue - event.ChipsRemovedByRace
	if state.TotalChipsInPlay < 0 {
		state.TotalChipsInPlay = 0
	}
}

func applyPenaltyIssued(state *TournamentState, event *examples.PenaltyIssued) {
	root := hex.EncodeToString(event.PlayerRoot)
	switch event.Type {
	case "DISQUALIFIED":
		state.DisqualifiedPlayers[root] = true
		delete(state.ActivePenalties, root)
	case "VERBAL_WARNING":
		// Informational; no rounds tracked.
	default:
		state.ActivePenalties[root] = event.Rounds
	}
}

func applyPenaltyRoundsDecremented(state *TournamentState, event *examples.PenaltyRoundsDecremented) {
	root := hex.EncodeToString(event.PlayerRoot)
	if event.RoundsRemaining <= 0 {
		delete(state.ActivePenalties, root)
	} else {
		state.ActivePenalties[root] = event.RoundsRemaining
	}
}

func applyPlayerDisqualified(state *TournamentState, event *examples.PlayerDisqualified) {
	root := hex.EncodeToString(event.PlayerRoot)
	state.DisqualifiedPlayers[root] = true
	delete(state.RegisteredPlayers, root)
	delete(state.PlayerStacks, root)
	state.TotalChipsInPlay -= event.ChipsRemoved
	if state.TotalChipsInPlay < 0 {
		state.TotalChipsInPlay = 0
	}
	state.PlayersRemaining = int32(len(state.RegisteredPlayers))
}

func applyBountyAwarded(state *TournamentState, event *examples.BountyAwarded) {
	elim := hex.EncodeToString(event.EliminatorRoot)
	state.BountyTotals[elim] += event.Amount
}

func applyNoShowDetected(state *TournamentState, event *examples.NoShowDetected) {
	root := hex.EncodeToString(event.PlayerRoot)
	state.NoShowPlayers[root] = true
	delete(state.RegisteredPlayers, root)
	delete(state.PlayerStacks, root)
	state.TotalChipsInPlay -= event.ChipsRemoved
	if state.TotalChipsInPlay < 0 {
		state.TotalChipsInPlay = 0
	}
	state.PlayersRemaining = int32(len(state.RegisteredPlayers))
}

func applyNewHandsHalted(state *TournamentState, _ *examples.NewHandsHalted) {
	state.NewHandsHalted = true
}

func applyBagAndTagComplete(state *TournamentState, event *examples.BagAndTagComplete) {
	state.BagSnapshots = event.Snapshots
}

func applyMixedGameVariantRotated(state *TournamentState, event *examples.MixedGameVariantRotated) {
	state.GameVariant = event.ToVariant
	state.MixedGameIndex++
}

func applyPlayerReEntered(state *TournamentState, event *examples.PlayerReEntered) {
	root := hex.EncodeToString(event.PlayerRoot)
	// Re-entry mechanics (TDA Rule 8B): forfeited chips removed from play
	// THEN fresh starting stack added. Net delta = added - forfeited.
	state.TotalChipsInPlay = state.TotalChipsInPlay - event.ChipsForfeited + event.ChipsAdded
	if state.TotalChipsInPlay < 0 {
		state.TotalChipsInPlay = 0
	}
	state.RegisteredPlayers[root] = &examples.PlayerRegistration{
		PlayerRoot:    event.PlayerRoot,
		StartingStack: event.ChipsAdded,
	}
	state.PlayerStacks[root] = event.ChipsAdded
	state.PlayersRemaining = int32(len(state.RegisteredPlayers))
}

func applyAbsentBlindAdvanced(state *TournamentState, event *examples.AbsentBlindAdvanced) {
	// Stack delta moves from absent → lone player. Chip ledger unchanged.
	absentHex := hex.EncodeToString(event.AbsentPlayerRoot)
	loneHex := hex.EncodeToString(event.LonePlayerRoot)
	state.PlayerStacks[absentHex] -= event.StackDelta
	state.PlayerStacks[loneHex] += event.StackDelta
}

func applySeatRedrawTriggered(_ *TournamentState, _ *examples.SeatRedrawTriggered) {
	// Pure notification — actual reseating is handled cross-domain.
}

func applyPlayerMovedTables(_ *TournamentState, _ *examples.PlayerMovedTables) {
	// Per-table tracking lives in the table aggregate; chip totals unchanged.
}

func applySimultaneousBustsRecorded(state *TournamentState, event *examples.SimultaneousBustsRecorded) {
	hexes := make([]string, 0, len(event.PlayerRoots))
	for _, r := range event.PlayerRoots {
		hexes = append(hexes, hex.EncodeToString(r))
	}
	stacks := make(map[string]int64, len(event.PreHandStacks))
	for k, v := range event.PreHandStacks {
		stacks[k] = v
	}
	state.SimultaneousBustGroups = append(state.SimultaneousBustGroups, SimultaneousBustGroup{
		PlayerRootHexes: hexes,
		SameTable:       event.SameTable,
		PreHandStacks:   stacks,
	})
	// Snapshot for WSOP-126b tiebreak.
	for k, v := range stacks {
		state.LastPreHandStacks[k] = v
	}
}

// --- stateRouter wires every applier so RebuildState replays correctly. ---

var stateRouter = angzarr.NewStateRouter(NewTournamentState).
	On(applyCreated).
	On(applyRegistrationOpened).
	On(applyRegistrationClosed).
	On(applyPlayerEnrolled).
	On(applyEnrollmentRejected).
	On(applyTournamentStarted).
	On(applyRebuyProcessed).
	On(applyRebuyDenied).
	On(applyBlindAdvanced).
	On(applyPlayerEliminated).
	On(applyPaused).
	On(applyResumed).
	On(applyCompleted).
	On(applyHandForHandStarted).
	On(applyHandForHandRoundComplete).
	On(applyHandForHandEnded).
	On(applyHandForHandHandRecorded).
	On(applyColorUpCompleted).
	On(applyPenaltyIssued).
	On(applyPenaltyRoundsDecremented).
	On(applyPlayerDisqualified).
	On(applyBountyAwarded).
	On(applyNoShowDetected).
	On(applyNewHandsHalted).
	On(applyBagAndTagComplete).
	On(applyMixedGameVariantRotated).
	On(applyPlayerReEntered).
	On(applyAbsentBlindAdvanced).
	On(applySeatRedrawTriggered).
	On(applyPlayerMovedTables).
	On(applySimultaneousBustsRecorded)

// RebuildState rebuilds tournament state from event history.
func RebuildState(eventBook *pb.EventBook) TournamentState {
	if eventBook == nil {
		return NewTournamentState()
	}
	state := NewTournamentState()
	for _, page := range eventBook.Pages {
		event := page.GetEvent()
		if event != nil {
			stateRouter.ApplySingle(&state, event)
		}
	}
	return state
}
