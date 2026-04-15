package handlers

import (
	"testing"

	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestAdvanceBlindLevel_RejectsNotRunning(t *testing.T) {
	state := TournamentState{Name: "Test", Status: examples.TournamentStatus_TOURNAMENT_CREATED}
	cmd := &examples.AdvanceBlindLevel{}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleAdvanceBlindLevel(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestAdvanceBlindLevel_IncrementsLevel(t *testing.T) {
	state := TournamentState{
		Name:         "Test",
		Status:       examples.TournamentStatus_TOURNAMENT_RUNNING,
		CurrentLevel: 1,
		BlindStructure: []*examples.BlindLevel{
			{Level: 1, SmallBlind: 25, BigBlind: 50},
			{Level: 2, SmallBlind: 50, BigBlind: 100},
			{Level: 3, SmallBlind: 100, BigBlind: 200},
		},
	}
	cmd := &examples.AdvanceBlindLevel{}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleAdvanceBlindLevel(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var event examples.BlindLevelAdvanced
	_ = proto.Unmarshal(result.Pages[0].GetEvent().Value, &event)
	assert.Equal(t, int32(2), event.Level)
	assert.Equal(t, int64(50), event.SmallBlind)
	assert.Equal(t, int64(100), event.BigBlind)
}

func TestAdvanceBlindLevel_CapsAtLastLevel(t *testing.T) {
	state := TournamentState{
		Name:         "Test",
		Status:       examples.TournamentStatus_TOURNAMENT_RUNNING,
		CurrentLevel: 3,
		BlindStructure: []*examples.BlindLevel{
			{Level: 1, SmallBlind: 25, BigBlind: 50},
			{Level: 2, SmallBlind: 50, BigBlind: 100},
			{Level: 3, SmallBlind: 100, BigBlind: 200},
		},
	}
	cmd := &examples.AdvanceBlindLevel{}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleAdvanceBlindLevel(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var event examples.BlindLevelAdvanced
	_ = proto.Unmarshal(result.Pages[0].GetEvent().Value, &event)
	assert.Equal(t, int32(4), event.Level)
	// Should use last level values since we exceeded the structure
	assert.Equal(t, int64(100), event.SmallBlind)
	assert.Equal(t, int64(200), event.BigBlind)
}

func TestEliminatePlayer_RejectsNotRunning(t *testing.T) {
	state := TournamentState{Name: "Test", Status: examples.TournamentStatus_TOURNAMENT_CREATED}
	cmd := &examples.EliminatePlayer{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleEliminatePlayer(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestEliminatePlayer_RejectsUnregistered(t *testing.T) {
	state := TournamentState{
		Name:              "Test",
		Status:            examples.TournamentStatus_TOURNAMENT_RUNNING,
		RegisteredPlayers: make(map[string]*examples.PlayerRegistration),
	}
	cmd := &examples.EliminatePlayer{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleEliminatePlayer(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not registered")
}

func TestEliminatePlayer_SetsFinishPosition(t *testing.T) {
	playerRoot := []byte{1, 2, 3}
	rootHex := "010203"
	state := TournamentState{
		Name:              "Test",
		Status:            examples.TournamentStatus_TOURNAMENT_RUNNING,
		PlayersRemaining:  5,
		RegisteredPlayers: map[string]*examples.PlayerRegistration{rootHex: {}},
	}
	cmd := &examples.EliminatePlayer{PlayerRoot: playerRoot}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleEliminatePlayer(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var event examples.PlayerEliminated
	_ = proto.Unmarshal(result.Pages[0].GetEvent().Value, &event)
	assert.Equal(t, int32(5), event.FinishPosition)
}

func TestPauseTournament_RejectsNotRunning(t *testing.T) {
	state := TournamentState{Name: "Test", Status: examples.TournamentStatus_TOURNAMENT_CREATED}
	cmd := &examples.PauseTournament{Reason: "break"}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandlePauseTournament(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestPauseTournament_SetsReason(t *testing.T) {
	state := TournamentState{Name: "Test", Status: examples.TournamentStatus_TOURNAMENT_RUNNING}
	cmd := &examples.PauseTournament{Reason: "Dinner break"}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandlePauseTournament(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var event examples.TournamentPaused
	_ = proto.Unmarshal(result.Pages[0].GetEvent().Value, &event)
	assert.Equal(t, "Dinner break", event.Reason)
}

func TestResumeTournament_RejectsNotPaused(t *testing.T) {
	state := TournamentState{Name: "Test", Status: examples.TournamentStatus_TOURNAMENT_RUNNING}
	cmd := &examples.ResumeTournament{}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleResumeTournament(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
}

func TestResumeTournament_Success(t *testing.T) {
	state := TournamentState{Name: "Test", Status: examples.TournamentStatus_TOURNAMENT_PAUSED}
	cmd := &examples.ResumeTournament{}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleResumeTournament(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	assert.True(t, result.Pages[0].GetEvent().MessageIs(&examples.TournamentResumed{}))
}
