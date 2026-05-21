package handlers

import (
	"testing"

	"github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/examples/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr_client/proto/angzarr/v1"
)

func makeCommandBook() *pb.CommandBook {
	return &pb.CommandBook{
		Cover: &pb.Cover{Domain: "tournament"},
	}
}

func TestOpenRegistration_RejectsNonExistent(t *testing.T) {
	state := NewTournamentState()
	cmd := &examples.OpenRegistration{}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleOpenRegistration(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestOpenRegistration_RejectsAlreadyOpen(t *testing.T) {
	state := NewTournamentState()
	state.Name = "Test"
	state.Status = examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN
	state.RegistrationOpen = true
	cmd := &examples.OpenRegistration{}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleOpenRegistration(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already open")
}

func TestOpenRegistration_RejectsRunning(t *testing.T) {
	state := NewTournamentState()
	state.Name = "Test"
	state.Status = examples.TournamentStatus_TOURNAMENT_RUNNING
	cmd := &examples.OpenRegistration{}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleOpenRegistration(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
}

func TestOpenRegistration_Success(t *testing.T) {
	state := NewTournamentState()
	state.Name = "Test"
	state.Status = examples.TournamentStatus_TOURNAMENT_CREATED
	cmd := &examples.OpenRegistration{}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleOpenRegistration(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Pages, 1)
	assert.True(t, result.Pages[0].GetEvent().MessageIs(&examples.RegistrationOpened{}))
}

func TestCloseRegistration_RejectsNotOpen(t *testing.T) {
	state := NewTournamentState()
	state.Name = "Test"
	state.Status = examples.TournamentStatus_TOURNAMENT_CREATED
	cmd := &examples.CloseRegistration{}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleCloseRegistration(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not open")
}

func TestCloseRegistration_IncludesTotalRegistrations(t *testing.T) {
	state := NewTournamentState()
	state.Name = "Test"
	state.Status = examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN
	state.RegistrationOpen = true
	state.RegisteredPlayers = map[string]*examples.PlayerRegistration{"a": {}, "b": {}, "c": {}}
	cmd := &examples.CloseRegistration{}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleCloseRegistration(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var event examples.RegistrationClosed
	_ = proto.Unmarshal(result.Pages[0].GetEvent().Value, &event)
	assert.Equal(t, int32(3), event.TotalRegistrations)
}

func TestEnrollPlayer_RejectsEmptyPlayerRoot(t *testing.T) {
	state := NewTournamentState()
	state.Name = "Test"
	state.Status = examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN
	state.RegistrationOpen = true
	cmd := &examples.EnrollPlayer{PlayerRoot: []byte{}}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleEnrollPlayer(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err) // emits event, not error
	var rejected examples.TournamentEnrollmentRejected
	_ = result.Pages[0].GetEvent().UnmarshalTo(&rejected)
	assert.Contains(t, rejected.Reason, "player_root")
}

func TestEnrollPlayer_RejectsClosedRegistration(t *testing.T) {
	state := NewTournamentState()
	state.Name = "Test"
	state.Status = examples.TournamentStatus_TOURNAMENT_CREATED
	cmd := &examples.EnrollPlayer{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleEnrollPlayer(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var rejected examples.TournamentEnrollmentRejected
	_ = result.Pages[0].GetEvent().UnmarshalTo(&rejected)
	assert.Contains(t, rejected.Reason, "not open")
}

func TestEnrollPlayer_RejectsFull(t *testing.T) {
	state := NewTournamentState()
	state.Name = "Test"
	state.Status = examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN
	state.RegistrationOpen = true
	state.MaxPlayers = 2
	state.RegisteredPlayers = map[string]*examples.PlayerRegistration{"a": {}, "b": {}}
	cmd := &examples.EnrollPlayer{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleEnrollPlayer(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var rejected examples.TournamentEnrollmentRejected
	_ = result.Pages[0].GetEvent().UnmarshalTo(&rejected)
	assert.Contains(t, rejected.Reason, "full")
}

func TestEnrollPlayer_RejectsDuplicate(t *testing.T) {
	playerRoot := []byte{1, 2, 3}
	rootHex := "010203"
	state := NewTournamentState()
	state.Name = "Test"
	state.Status = examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN
	state.RegistrationOpen = true
	state.MaxPlayers = 100
	state.RegisteredPlayers = map[string]*examples.PlayerRegistration{rootHex: {}}
	cmd := &examples.EnrollPlayer{PlayerRoot: playerRoot}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleEnrollPlayer(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var rejected examples.TournamentEnrollmentRejected
	_ = result.Pages[0].GetEvent().UnmarshalTo(&rejected)
	assert.Contains(t, rejected.Reason, "already registered")
}

func TestEnrollPlayer_Success(t *testing.T) {
	state := NewTournamentState()
	state.Name = "Test"
	state.Status = examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN
	state.RegistrationOpen = true
	state.MaxPlayers = 100
	state.BuyIn = 1000
	state.StartingStack = 10000
	cmd := &examples.EnrollPlayer{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleEnrollPlayer(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var enrolled examples.TournamentPlayerEnrolled
	_ = result.Pages[0].GetEvent().UnmarshalTo(&enrolled)
	assert.Equal(t, int64(1000), enrolled.FeePaid)
	assert.Equal(t, int64(10000), enrolled.StartingStack)
	assert.Equal(t, int32(1), enrolled.RegistrationNumber)
}
