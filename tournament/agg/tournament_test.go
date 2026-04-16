package main

import (
	"testing"

	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// newTestTournament creates a Tournament with the given prior events for testing.
func newTestTournament(events ...proto.Message) *Tournament {
	return NewTournament(buildEventBook(events...))
}

// buildEventBook constructs an EventBook from a list of proto events.
func buildEventBook(events ...proto.Message) *pb.EventBook {
	if len(events) == 0 {
		return &pb.EventBook{}
	}
	var pages []*pb.EventPage
	for i, event := range events {
		eventAny, _ := anypb.New(event)
		pages = append(pages, &pb.EventPage{
			Header: &pb.PageHeader{
				SequenceType: &pb.PageHeader_Sequence{Sequence: uint32(i + 1)},
			},
			Payload: &pb.EventPage_Event{
				Event: eventAny,
			},
		})
	}
	return &pb.EventBook{
		Cover: &pb.Cover{Domain: "tournament"},
		Pages: pages,
	}
}

// --- CreateTournament Tests ---

func TestHandleCreateTournament_RejectsExistingTournament(t *testing.T) {
	tournament := newTestTournament(&examples.TournamentCreated{Name: "Existing"})

	_, err := tournament.HandleCreateTournament(&examples.CreateTournament{
		Name:          "Another",
		BuyIn:         1000,
		StartingStack: 10000,
		MaxPlayers:    100,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestHandleCreateTournament_RejectsEmptyName(t *testing.T) {
	tournament := newTestTournament()

	_, err := tournament.HandleCreateTournament(&examples.CreateTournament{
		Name:          "",
		BuyIn:         1000,
		StartingStack: 10000,
		MaxPlayers:    100,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestHandleCreateTournament_RejectsZeroBuyIn(t *testing.T) {
	tournament := newTestTournament()

	_, err := tournament.HandleCreateTournament(&examples.CreateTournament{
		Name:          "Test",
		BuyIn:         0,
		StartingStack: 10000,
		MaxPlayers:    100,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}

func TestHandleCreateTournament_RejectsZeroStartingStack(t *testing.T) {
	tournament := newTestTournament()

	_, err := tournament.HandleCreateTournament(&examples.CreateTournament{
		Name:          "Test",
		BuyIn:         1000,
		StartingStack: 0,
		MaxPlayers:    100,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}

func TestHandleCreateTournament_RejectsMaxPlayersLessThan2(t *testing.T) {
	tournament := newTestTournament()

	_, err := tournament.HandleCreateTournament(&examples.CreateTournament{
		Name:          "Test",
		BuyIn:         1000,
		StartingStack: 10000,
		MaxPlayers:    1,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max_players")
}

func TestHandleCreateTournament_Success(t *testing.T) {
	tournament := newTestTournament()

	event, err := tournament.HandleCreateTournament(&examples.CreateTournament{
		Name:          "Sunday Million",
		GameVariant:   examples.GameVariant_TEXAS_HOLDEM,
		BuyIn:         1000,
		StartingStack: 10000,
		MaxPlayers:    100,
		MinPlayers:    10,
	})

	require.NoError(t, err)
	require.NotNil(t, event)
	assert.Equal(t, "Sunday Million", event.Name)
	assert.Equal(t, examples.GameVariant_TEXAS_HOLDEM, event.GameVariant)
	assert.Equal(t, int64(1000), event.BuyIn)
	assert.Equal(t, int64(10000), event.StartingStack)
	assert.Equal(t, int32(100), event.MaxPlayers)
	assert.Equal(t, int32(10), event.MinPlayers)
	assert.NotNil(t, event.CreatedAt)
}

// --- OpenRegistration Tests ---

func TestHandleOpenRegistration_RejectsNonExistent(t *testing.T) {
	tournament := newTestTournament()

	_, err := tournament.HandleOpenRegistration(&examples.OpenRegistration{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestHandleOpenRegistration_RejectsAlreadyOpen(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test"},
		&examples.RegistrationOpened{},
	)

	_, err := tournament.HandleOpenRegistration(&examples.OpenRegistration{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already open")
}

func TestHandleOpenRegistration_RejectsRunning(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test"},
		&examples.TournamentStarted{},
	)

	_, err := tournament.HandleOpenRegistration(&examples.OpenRegistration{})

	assert.Error(t, err)
}

func TestHandleOpenRegistration_Success(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test"},
	)

	event, err := tournament.HandleOpenRegistration(&examples.OpenRegistration{})

	require.NoError(t, err)
	require.NotNil(t, event)
	assert.NotNil(t, event.OpenedAt)
}

// --- CloseRegistration Tests ---

func TestHandleCloseRegistration_RejectsNotOpen(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test"},
	)

	_, err := tournament.HandleCloseRegistration(&examples.CloseRegistration{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not open")
}

func TestHandleCloseRegistration_IncludesTotalRegistrations(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test", MaxPlayers: 100, BuyIn: 1000, StartingStack: 10000},
		&examples.RegistrationOpened{},
		&examples.TournamentPlayerEnrolled{PlayerRoot: []byte{1}, FeePaid: 1000, StartingStack: 10000},
		&examples.TournamentPlayerEnrolled{PlayerRoot: []byte{2}, FeePaid: 1000, StartingStack: 10000},
		&examples.TournamentPlayerEnrolled{PlayerRoot: []byte{3}, FeePaid: 1000, StartingStack: 10000},
	)

	event, err := tournament.HandleCloseRegistration(&examples.CloseRegistration{})

	require.NoError(t, err)
	assert.Equal(t, int32(3), event.TotalRegistrations)
}

// --- EnrollPlayer Tests ---

func TestHandleEnrollPlayer_RejectsClosedRegistration(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test", MaxPlayers: 100},
	)

	result, err := tournament.HandleEnrollPlayer(&examples.EnrollPlayer{PlayerRoot: []byte{1, 2, 3}})

	require.NoError(t, err) // Rejection is an event, not an error
	rejected, ok := result.(*examples.TournamentEnrollmentRejected)
	require.True(t, ok)
	assert.Equal(t, "closed", rejected.Reason)
}

func TestHandleEnrollPlayer_RejectsFull(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test", MaxPlayers: 2, BuyIn: 1000, StartingStack: 10000},
		&examples.RegistrationOpened{},
		&examples.TournamentPlayerEnrolled{PlayerRoot: []byte{1}, FeePaid: 1000, StartingStack: 10000},
		&examples.TournamentPlayerEnrolled{PlayerRoot: []byte{2}, FeePaid: 1000, StartingStack: 10000},
	)

	result, err := tournament.HandleEnrollPlayer(&examples.EnrollPlayer{PlayerRoot: []byte{3}})

	require.NoError(t, err)
	rejected, ok := result.(*examples.TournamentEnrollmentRejected)
	require.True(t, ok)
	assert.Equal(t, "full", rejected.Reason)
}

func TestHandleEnrollPlayer_RejectsDuplicate(t *testing.T) {
	playerRoot := []byte{1, 2, 3}
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test", MaxPlayers: 100, BuyIn: 1000, StartingStack: 10000},
		&examples.RegistrationOpened{},
		&examples.TournamentPlayerEnrolled{PlayerRoot: playerRoot, FeePaid: 1000, StartingStack: 10000},
	)

	result, err := tournament.HandleEnrollPlayer(&examples.EnrollPlayer{PlayerRoot: playerRoot})

	require.NoError(t, err)
	rejected, ok := result.(*examples.TournamentEnrollmentRejected)
	require.True(t, ok)
	assert.Equal(t, "already_registered", rejected.Reason)
}

func TestHandleEnrollPlayer_Success(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test", MaxPlayers: 100, BuyIn: 1000, StartingStack: 10000},
		&examples.RegistrationOpened{},
	)

	result, err := tournament.HandleEnrollPlayer(&examples.EnrollPlayer{PlayerRoot: []byte{1, 2, 3}})

	require.NoError(t, err)
	enrolled, ok := result.(*examples.TournamentPlayerEnrolled)
	require.True(t, ok)
	assert.Equal(t, int64(1000), enrolled.FeePaid)
	assert.Equal(t, int64(10000), enrolled.StartingStack)
	assert.Equal(t, int32(1), enrolled.RegistrationNumber)
}

// --- ProcessRebuy Tests ---

func rebuyTournament() *Tournament {
	return newTestTournament(
		&examples.TournamentCreated{
			Name:          "Rebuy Tournament",
			MaxPlayers:    100,
			BuyIn:         1000,
			StartingStack: 10000,
			RebuyConfig: &examples.RebuyConfig{
				Enabled:          true,
				MaxRebuys:        3,
				RebuyLevelCutoff: 4,
				RebuyCost:        1000,
				RebuyChips:       10000,
			},
			BlindStructure: []*examples.BlindLevel{
				{Level: 1, SmallBlind: 25, BigBlind: 50},
			},
		},
		&examples.RegistrationOpened{},
		&examples.TournamentPlayerEnrolled{PlayerRoot: []byte{1, 2, 3}, FeePaid: 1000, StartingStack: 10000},
		&examples.RegistrationClosed{},
		&examples.TournamentStarted{},
	)
}

func TestHandleProcessRebuy_RejectsNonExistent(t *testing.T) {
	tournament := newTestTournament()

	_, err := tournament.HandleProcessRebuy(&examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestHandleProcessRebuy_RejectsNotRunning(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test"},
	)

	_, err := tournament.HandleProcessRebuy(&examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestHandleProcessRebuy_RejectsUnregistered(t *testing.T) {
	tournament := rebuyTournament()

	_, err := tournament.HandleProcessRebuy(&examples.ProcessRebuy{PlayerRoot: []byte{9, 9, 9}})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestHandleProcessRebuy_DeniesWindowClosed(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{
			Name:          "Rebuy Tournament",
			MaxPlayers:    100,
			BuyIn:         1000,
			StartingStack: 10000,
			RebuyConfig: &examples.RebuyConfig{
				Enabled:          true,
				MaxRebuys:        3,
				RebuyLevelCutoff: 4,
				RebuyCost:        1000,
				RebuyChips:       10000,
			},
		},
		&examples.RegistrationOpened{},
		&examples.TournamentPlayerEnrolled{PlayerRoot: []byte{1, 2, 3}, FeePaid: 1000, StartingStack: 10000},
		&examples.RegistrationClosed{},
		&examples.TournamentStarted{},
		&examples.BlindLevelAdvanced{Level: 5}, // Past cutoff of 4
	)

	result, err := tournament.HandleProcessRebuy(&examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}})

	require.NoError(t, err) // Denial is event, not error
	denied, ok := result.(*examples.RebuyDenied)
	require.True(t, ok)
	assert.Equal(t, "window_closed", denied.Reason)
}

func TestHandleProcessRebuy_DeniesMaxReached(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{
			Name:          "Rebuy Tournament",
			MaxPlayers:    100,
			BuyIn:         1000,
			StartingStack: 10000,
			RebuyConfig: &examples.RebuyConfig{
				Enabled:          true,
				MaxRebuys:        3,
				RebuyLevelCutoff: 4,
				RebuyCost:        1000,
				RebuyChips:       10000,
			},
		},
		&examples.RegistrationOpened{},
		&examples.TournamentPlayerEnrolled{PlayerRoot: []byte{1, 2, 3}, FeePaid: 1000, StartingStack: 10000},
		&examples.RegistrationClosed{},
		&examples.TournamentStarted{},
		&examples.RebuyProcessed{PlayerRoot: []byte{1, 2, 3}, RebuyCount: 3}, // At max
	)

	result, err := tournament.HandleProcessRebuy(&examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}})

	require.NoError(t, err)
	denied, ok := result.(*examples.RebuyDenied)
	require.True(t, ok)
	assert.Equal(t, "max_reached", denied.Reason)
}

func TestHandleProcessRebuy_Success(t *testing.T) {
	tournament := rebuyTournament()

	result, err := tournament.HandleProcessRebuy(&examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}})

	require.NoError(t, err)
	processed, ok := result.(*examples.RebuyProcessed)
	require.True(t, ok)
	assert.Equal(t, int64(1000), processed.RebuyCost)
	assert.Equal(t, int64(10000), processed.ChipsAdded)
	assert.Equal(t, int32(1), processed.RebuyCount)
}

func TestHandleProcessRebuy_IncrementsRebuyCount(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{
			Name:          "Rebuy Tournament",
			MaxPlayers:    100,
			BuyIn:         1000,
			StartingStack: 10000,
			RebuyConfig: &examples.RebuyConfig{
				Enabled:          true,
				MaxRebuys:        3,
				RebuyLevelCutoff: 4,
				RebuyCost:        1000,
				RebuyChips:       10000,
			},
		},
		&examples.RegistrationOpened{},
		&examples.TournamentPlayerEnrolled{PlayerRoot: []byte{1, 2, 3}, FeePaid: 1000, StartingStack: 10000},
		&examples.RegistrationClosed{},
		&examples.TournamentStarted{},
		&examples.RebuyProcessed{PlayerRoot: []byte{1, 2, 3}, RebuyCount: 2}, // 2 prior rebuys
	)

	result, err := tournament.HandleProcessRebuy(&examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}})

	require.NoError(t, err)
	processed, ok := result.(*examples.RebuyProcessed)
	require.True(t, ok)
	assert.Equal(t, int32(3), processed.RebuyCount)
}

// --- AdvanceBlindLevel Tests ---

func TestHandleAdvanceBlindLevel_RejectsNotRunning(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test"},
	)

	_, err := tournament.HandleAdvanceBlindLevel(&examples.AdvanceBlindLevel{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestHandleAdvanceBlindLevel_IncrementsLevel(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{
			Name:       "Test",
			MaxPlayers: 100,
			BlindStructure: []*examples.BlindLevel{
				{Level: 1, SmallBlind: 25, BigBlind: 50},
				{Level: 2, SmallBlind: 50, BigBlind: 100},
				{Level: 3, SmallBlind: 100, BigBlind: 200},
			},
		},
		&examples.TournamentStarted{},
		&examples.BlindLevelAdvanced{Level: 1},
	)

	event, err := tournament.HandleAdvanceBlindLevel(&examples.AdvanceBlindLevel{})

	require.NoError(t, err)
	assert.Equal(t, int32(2), event.Level)
	assert.Equal(t, int64(50), event.SmallBlind)
	assert.Equal(t, int64(100), event.BigBlind)
}

func TestHandleAdvanceBlindLevel_CapsAtLastLevel(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{
			Name:       "Test",
			MaxPlayers: 100,
			BlindStructure: []*examples.BlindLevel{
				{Level: 1, SmallBlind: 25, BigBlind: 50},
				{Level: 2, SmallBlind: 50, BigBlind: 100},
				{Level: 3, SmallBlind: 100, BigBlind: 200},
			},
		},
		&examples.TournamentStarted{},
		&examples.BlindLevelAdvanced{Level: 3},
	)

	event, err := tournament.HandleAdvanceBlindLevel(&examples.AdvanceBlindLevel{})

	require.NoError(t, err)
	assert.Equal(t, int32(4), event.Level)
	assert.Equal(t, int64(100), event.SmallBlind)
	assert.Equal(t, int64(200), event.BigBlind)
}

// --- EliminatePlayer Tests ---

func TestHandleEliminatePlayer_RejectsNotRunning(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test"},
	)

	_, err := tournament.HandleEliminatePlayer(&examples.EliminatePlayer{PlayerRoot: []byte{1, 2, 3}})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestHandleEliminatePlayer_RejectsUnregistered(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test", MaxPlayers: 100},
		&examples.TournamentStarted{},
	)

	_, err := tournament.HandleEliminatePlayer(&examples.EliminatePlayer{PlayerRoot: []byte{1, 2, 3}})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestHandleEliminatePlayer_SetsFinishPosition(t *testing.T) {
	playerRoot := []byte{1, 2, 3}
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test", MaxPlayers: 100, BuyIn: 1000, StartingStack: 10000},
		&examples.RegistrationOpened{},
		&examples.TournamentPlayerEnrolled{PlayerRoot: playerRoot, FeePaid: 1000, StartingStack: 10000},
		&examples.TournamentPlayerEnrolled{PlayerRoot: []byte{4, 5, 6}, FeePaid: 1000, StartingStack: 10000},
		&examples.TournamentPlayerEnrolled{PlayerRoot: []byte{7, 8, 9}, FeePaid: 1000, StartingStack: 10000},
		&examples.RegistrationClosed{},
		&examples.TournamentStarted{},
	)

	event, err := tournament.HandleEliminatePlayer(&examples.EliminatePlayer{PlayerRoot: playerRoot})

	require.NoError(t, err)
	// PlayersRemaining should be 3 (3 enrolled)
	assert.Equal(t, int32(3), event.FinishPosition)
}

// --- PauseTournament Tests ---

func TestHandlePauseTournament_RejectsNotRunning(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test"},
	)

	_, err := tournament.HandlePauseTournament(&examples.PauseTournament{Reason: "break"})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestHandlePauseTournament_SetsReason(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test"},
		&examples.TournamentStarted{},
	)

	event, err := tournament.HandlePauseTournament(&examples.PauseTournament{Reason: "Dinner break"})

	require.NoError(t, err)
	assert.Equal(t, "Dinner break", event.Reason)
}

// --- ResumeTournament Tests ---

func TestHandleResumeTournament_RejectsNotPaused(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test"},
		&examples.TournamentStarted{},
	)

	_, err := tournament.HandleResumeTournament(&examples.ResumeTournament{})

	assert.Error(t, err)
}

func TestHandleResumeTournament_Success(t *testing.T) {
	tournament := newTestTournament(
		&examples.TournamentCreated{Name: "Test"},
		&examples.TournamentStarted{},
		&examples.TournamentPaused{},
	)

	event, err := tournament.HandleResumeTournament(&examples.ResumeTournament{})

	require.NoError(t, err)
	assert.NotNil(t, event.ResumedAt)
}
