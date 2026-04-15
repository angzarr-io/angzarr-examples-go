package handlers

import (
	"testing"

	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"github.com/stretchr/testify/assert"
)

func TestCreateTournament_Guard_RejectsExistingTournament(t *testing.T) {
	state := TournamentState{Name: "Existing"}

	err := guardCreateTournament(state)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestCreateTournament_Guard_AllowsNewTournament(t *testing.T) {
	state := NewTournamentState()

	err := guardCreateTournament(state)

	assert.NoError(t, err)
}

func TestCreateTournament_Validate_RejectsEmptyName(t *testing.T) {
	cmd := &examples.CreateTournament{
		Name:          "",
		BuyIn:         1000,
		StartingStack: 10000,
		MaxPlayers:    100,
	}

	err := validateCreateTournament(cmd)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name")
}

func TestCreateTournament_Validate_RejectsZeroBuyIn(t *testing.T) {
	cmd := &examples.CreateTournament{
		Name:          "Test",
		BuyIn:         0,
		StartingStack: 10000,
		MaxPlayers:    100,
	}

	err := validateCreateTournament(cmd)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}

func TestCreateTournament_Validate_RejectsZeroStartingStack(t *testing.T) {
	cmd := &examples.CreateTournament{
		Name:          "Test",
		BuyIn:         1000,
		StartingStack: 0,
		MaxPlayers:    100,
	}

	err := validateCreateTournament(cmd)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "positive")
}

func TestCreateTournament_Validate_RejectsMaxPlayersLessThan2(t *testing.T) {
	cmd := &examples.CreateTournament{
		Name:          "Test",
		BuyIn:         1000,
		StartingStack: 10000,
		MaxPlayers:    1,
	}

	err := validateCreateTournament(cmd)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max_players")
}

func TestCreateTournament_Compute_SetsAllFields(t *testing.T) {
	cmd := &examples.CreateTournament{
		Name:          "Sunday Million",
		GameVariant:   examples.GameVariant_TEXAS_HOLDEM,
		BuyIn:         1000,
		StartingStack: 10000,
		MaxPlayers:    100,
		MinPlayers:    10,
	}

	event := computeTournamentCreated(cmd)

	assert.Equal(t, "Sunday Million", event.Name)
	assert.Equal(t, examples.GameVariant_TEXAS_HOLDEM, event.GameVariant)
	assert.Equal(t, int64(1000), event.BuyIn)
	assert.Equal(t, int64(10000), event.StartingStack)
	assert.Equal(t, int32(100), event.MaxPlayers)
	assert.Equal(t, int32(10), event.MinPlayers)
	assert.NotNil(t, event.CreatedAt)
}
