package handlers

import (
	"testing"

	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func rebuyState() TournamentState {
	s := NewTournamentState()
	s.Name = "Rebuy Tournament"
	s.Status = examples.TournamentStatus_TOURNAMENT_RUNNING
	s.BuyIn = 1000
	s.CurrentLevel = 2
	s.RebuyConfig = &examples.RebuyConfig{
		Enabled:          true,
		MaxRebuys:        3,
		RebuyLevelCutoff: 4,
		RebuyCost:        1000,
		RebuyChips:       10000,
	}
	s.RegisteredPlayers = map[string]*examples.PlayerRegistration{
		"010203": {PlayerRoot: []byte{1, 2, 3}, RebuysUsed: 0},
	}
	s.PlayersRemaining = 3
	return s
}

func TestProcessRebuy_RejectsNonExistent(t *testing.T) {
	state := NewTournamentState()
	cmd := &examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleProcessRebuy(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestProcessRebuy_RejectsNotRunning(t *testing.T) {
	state := TournamentState{Name: "Test", Status: examples.TournamentStatus_TOURNAMENT_CREATED}
	cmd := &examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleProcessRebuy(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestProcessRebuy_DeniesUnregistered(t *testing.T) {
	state := rebuyState()
	cmd := &examples.ProcessRebuy{PlayerRoot: []byte{9, 9, 9}} // Different player
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleProcessRebuy(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err) // EU-0818: unregistered -> RebuyDenied (event)
	var denied examples.RebuyDenied
	_ = proto.Unmarshal(result.Pages[0].GetEvent().Value, &denied)
	assert.Contains(t, denied.Reason, "not registered")
}

func TestProcessRebuy_DeniesWindowClosed(t *testing.T) {
	state := rebuyState()
	state.CurrentLevel = 5 // Past cutoff of 4
	cmd := &examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleProcessRebuy(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err) // Denial is event, not error
	var denied examples.RebuyDenied
	_ = proto.Unmarshal(result.Pages[0].GetEvent().Value, &denied)
	assert.Contains(t, denied.Reason, "closed")
}

func TestProcessRebuy_DeniesMaxReached(t *testing.T) {
	state := rebuyState()
	state.RegisteredPlayers["010203"].RebuysUsed = 3 // At max
	cmd := &examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleProcessRebuy(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var denied examples.RebuyDenied
	_ = proto.Unmarshal(result.Pages[0].GetEvent().Value, &denied)
	assert.Contains(t, denied.Reason, "Maximum")
}

func TestProcessRebuy_Success(t *testing.T) {
	state := rebuyState()
	cmd := &examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleProcessRebuy(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var processed examples.RebuyProcessed
	_ = proto.Unmarshal(result.Pages[0].GetEvent().Value, &processed)
	assert.Equal(t, int64(1000), processed.RebuyCost)
	assert.Equal(t, int64(10000), processed.ChipsAdded)
	assert.Equal(t, int32(1), processed.RebuyCount)
}

func TestProcessRebuy_IncrementsRebuyCount(t *testing.T) {
	state := rebuyState()
	state.RegisteredPlayers["010203"].RebuysUsed = 2
	cmd := &examples.ProcessRebuy{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleProcessRebuy(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var processed examples.RebuyProcessed
	_ = proto.Unmarshal(result.Pages[0].GetEvent().Value, &processed)
	assert.Equal(t, int32(3), processed.RebuyCount)
}
