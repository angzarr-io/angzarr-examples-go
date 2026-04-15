package handlers

import (
	"testing"

	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
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
	state := TournamentState{
		Name:   "Test",
		Status: examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN,
	}
	cmd := &examples.OpenRegistration{}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleOpenRegistration(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already open")
}

func TestOpenRegistration_RejectsRunning(t *testing.T) {
	state := TournamentState{
		Name:   "Test",
		Status: examples.TournamentStatus_TOURNAMENT_RUNNING,
	}
	cmd := &examples.OpenRegistration{}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleOpenRegistration(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
}

func TestOpenRegistration_Success(t *testing.T) {
	state := TournamentState{
		Name:   "Test",
		Status: examples.TournamentStatus_TOURNAMENT_CREATED,
	}
	cmd := &examples.OpenRegistration{}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleOpenRegistration(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Len(t, result.Pages, 1)
	assert.True(t, result.Pages[0].GetEvent().MessageIs(&examples.RegistrationOpened{}))
}

func TestCloseRegistration_RejectsNotOpen(t *testing.T) {
	state := TournamentState{
		Name:   "Test",
		Status: examples.TournamentStatus_TOURNAMENT_CREATED,
	}
	cmd := &examples.CloseRegistration{}
	cmdAny, _ := anypb.New(cmd)

	_, err := HandleCloseRegistration(makeCommandBook(), cmdAny, state, 0)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not open")
}

func TestCloseRegistration_IncludesTotalRegistrations(t *testing.T) {
	state := TournamentState{
		Name:              "Test",
		Status:            examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN,
		RegisteredPlayers: map[string]*examples.PlayerRegistration{"a": {}, "b": {}, "c": {}},
	}
	cmd := &examples.CloseRegistration{}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleCloseRegistration(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var event examples.RegistrationClosed
	_ = proto.Unmarshal(result.Pages[0].GetEvent().Value, &event)
	assert.Equal(t, int32(3), event.TotalRegistrations)
}

func TestEnrollPlayer_RejectsClosedRegistration(t *testing.T) {
	state := TournamentState{
		Name:              "Test",
		Status:            examples.TournamentStatus_TOURNAMENT_CREATED,
		RegisteredPlayers: make(map[string]*examples.PlayerRegistration),
	}
	cmd := &examples.EnrollPlayer{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleEnrollPlayer(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err) // Rejection is an event, not an error
	var rejected examples.TournamentEnrollmentRejected
	_ = result.Pages[0].GetEvent().UnmarshalTo(&rejected)
	assert.Equal(t, "closed", rejected.Reason)
}

func TestEnrollPlayer_RejectsFull(t *testing.T) {
	state := TournamentState{
		Name:              "Test",
		Status:            examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN,
		MaxPlayers:        2,
		RegisteredPlayers: map[string]*examples.PlayerRegistration{"a": {}, "b": {}},
	}
	cmd := &examples.EnrollPlayer{PlayerRoot: []byte{1, 2, 3}}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleEnrollPlayer(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var rejected examples.TournamentEnrollmentRejected
	_ = result.Pages[0].GetEvent().UnmarshalTo(&rejected)
	assert.Equal(t, "full", rejected.Reason)
}

func TestEnrollPlayer_RejectsDuplicate(t *testing.T) {
	playerRoot := []byte{1, 2, 3}
	rootHex := "010203"
	state := TournamentState{
		Name:              "Test",
		Status:            examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN,
		MaxPlayers:        100,
		RegisteredPlayers: map[string]*examples.PlayerRegistration{rootHex: {}},
	}
	cmd := &examples.EnrollPlayer{PlayerRoot: playerRoot}
	cmdAny, _ := anypb.New(cmd)

	result, err := HandleEnrollPlayer(makeCommandBook(), cmdAny, state, 0)

	require.NoError(t, err)
	var rejected examples.TournamentEnrollmentRejected
	_ = result.Pages[0].GetEvent().UnmarshalTo(&rejected)
	assert.Equal(t, "already_registered", rejected.Reason)
}

func TestEnrollPlayer_Success(t *testing.T) {
	state := TournamentState{
		Name:              "Test",
		Status:            examples.TournamentStatus_TOURNAMENT_REGISTRATION_OPEN,
		MaxPlayers:        100,
		BuyIn:             1000,
		StartingStack:     10000,
		RegisteredPlayers: make(map[string]*examples.PlayerRegistration),
	}
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
