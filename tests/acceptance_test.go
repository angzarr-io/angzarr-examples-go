//go:build acceptance

package tests

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	angzarr "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	examples "github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// playerURL returns the player aggregate coordinator address from environment or default.
func playerURL() string {
	if url := os.Getenv("PLAYER_URL"); url != "" {
		return url
	}
	return "localhost:1310"
}

// newProtoUUID generates a new random UUID in the angzarr proto format.
func newProtoUUID() *angzarr.UUID {
	id := uuid.New()
	return &angzarr.UUID{Value: id[:]}
}

// packCommand wraps a protobuf message into an Any with the given type name.
func packCommand(msg proto.Message, typeName string) *anypb.Any {
	data, err := proto.Marshal(msg)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal command: %v", err))
	}
	return &anypb.Any{
		TypeUrl: "type.googleapis.com/" + typeName,
		Value:   data,
	}
}

// makeCommandRequest creates a CommandRequest at sequence 0.
func makeCommandRequest(domain string, root *angzarr.UUID, command *anypb.Any) *angzarr.CommandRequest {
	return makeCommandRequestAtSeq(domain, root, command, 0)
}

// makeCommandRequestAtSeq creates a CommandRequest at the given sequence number.
func makeCommandRequestAtSeq(domain string, root *angzarr.UUID, command *anypb.Any, sequence uint32) *angzarr.CommandRequest {
	correlationID := uuid.New().String()
	return &angzarr.CommandRequest{
		Command: &angzarr.CommandBook{
			Cover: &angzarr.Cover{
				Domain:        domain,
				Root:          root,
				CorrelationId: correlationID,
			},
			Pages: []*angzarr.CommandPage{
				{
					Header: &angzarr.PageHeader{
						SequenceType: &angzarr.PageHeader_Sequence{
							Sequence: sequence,
						},
					},
					Payload: &angzarr.CommandPage_Command{
						Command: command,
					},
				},
			},
		},
		SyncMode: angzarr.SyncMode_SYNC_MODE_SIMPLE,
	}
}

// getPlayerClient creates a gRPC connection and returns a coordinator service client.
func getPlayerClient(t *testing.T) (angzarr.CommandHandlerCoordinatorServiceClient, *grpc.ClientConn) {
	t.Helper()
	url := playerURL()
	conn, err := grpc.NewClient(url,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to create gRPC client for %s: %v", url, err)
	}
	return angzarr.NewCommandHandlerCoordinatorServiceClient(conn), conn
}

// sendPlayerCommand sends a command request to the player aggregate and returns the response.
func sendPlayerCommand(t *testing.T, request *angzarr.CommandRequest) (*angzarr.CommandResponse, error) {
	t.Helper()
	client, conn := getPlayerClient(t)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return client.HandleCommand(ctx, request)
}

func TestPlayerAggregateConnectivity(t *testing.T) {
	url := playerURL()
	t.Logf("Connecting to player aggregate at %s", url)

	conn, err := grpc.NewClient(url,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("Failed to create gRPC client for %s: %v", url, err)
	}
	defer conn.Close()

	// Verify the connection can be established by making a lightweight call.
	// The connection is lazy, so we force it with a short context.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn.Connect()
	for conn.GetState().String() != "READY" {
		if !conn.WaitForStateChange(ctx, conn.GetState()) {
			t.Fatalf("Failed to connect to player aggregate at %s: context expired in state %s", url, conn.GetState())
		}
	}

	t.Logf("Successfully connected to player aggregate at %s", url)
}

func TestRegisterPlayer(t *testing.T) {
	playerID := newProtoUUID()
	playerIDHex := hex.EncodeToString(playerID.Value)
	t.Logf("Registering player with ID: %s", playerIDHex)

	cmd := &examples.RegisterPlayer{
		DisplayName: "AcceptanceTestPlayer",
		Email:       fmt.Sprintf("test-%s@example.com", playerIDHex[:8]),
		PlayerType:  examples.PlayerType_HUMAN,
	}

	request := makeCommandRequest(
		"player",
		playerID,
		packCommand(cmd, "examples.RegisterPlayer"),
	)

	resp, err := sendPlayerCommand(t, request)
	if err != nil {
		t.Fatalf("RegisterPlayer failed: %v", err)
	}

	if resp.Events == nil {
		t.Fatal("Response should contain events")
	}
	if resp.Events.Cover == nil {
		t.Fatal("Event book should have a cover")
	}
	if len(resp.Events.Pages) == 0 {
		t.Fatal("Should have at least one event")
	}

	t.Logf("Successfully registered player, got %d event(s)", len(resp.Events.Pages))
}

func TestRegisterAndDeposit(t *testing.T) {
	playerID := newProtoUUID()
	playerIDHex := hex.EncodeToString(playerID.Value)
	t.Logf("Test: Register and deposit for player %s", playerIDHex)

	// Register a new player
	registerCmd := &examples.RegisterPlayer{
		DisplayName: "DepositTestPlayer",
		Email:       fmt.Sprintf("deposit-%s@example.com", playerIDHex[:8]),
		PlayerType:  examples.PlayerType_HUMAN,
	}

	registerRequest := makeCommandRequest(
		"player",
		playerID,
		packCommand(registerCmd, "examples.RegisterPlayer"),
	)

	_, err := sendPlayerCommand(t, registerRequest)
	if err != nil {
		t.Fatalf("Registration should succeed: %v", err)
	}
	t.Log("Player registered successfully")

	// Now deposit funds (sequence 1 since registration was sequence 0).
	// Retry with backoff because the aggregate may not have finished
	// processing the registration event yet (eventual consistency).
	depositCmd := &examples.DepositFunds{
		Amount: &examples.Currency{
			Amount:       1000,
			CurrencyCode: "USD",
		},
	}

	const maxAttempts = 30
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		depositRequest := makeCommandRequestAtSeq(
			"player",
			playerID,
			packCommand(depositCmd, "examples.DepositFunds"),
			1, // Sequence 1 after registration
		)

		resp, err := sendPlayerCommand(t, depositRequest)
		if err != nil {
			t.Logf("DepositFunds attempt %d/%d failed: %v", attempt, maxAttempts, err)
			lastErr = err
			time.Sleep(time.Duration(500*attempt) * time.Millisecond)
			continue
		}

		if resp.Events == nil {
			t.Fatal("Response should contain events")
		}
		if len(resp.Events.Pages) == 0 {
			t.Fatal("Should have deposited event")
		}
		t.Logf("Successfully deposited funds, got %d event(s)", len(resp.Events.Pages))
		lastErr = nil
		break
	}
	if lastErr != nil {
		t.Fatalf("DepositFunds failed after %d attempts: %v", maxAttempts, lastErr)
	}
}

func TestDuplicateRegistrationFails(t *testing.T) {
	playerID := newProtoUUID()
	playerIDHex := hex.EncodeToString(playerID.Value)
	t.Logf("Test: Duplicate registration for player %s", playerIDHex)

	cmd := &examples.RegisterPlayer{
		DisplayName: "DuplicateTestPlayer",
		Email:       fmt.Sprintf("dup-%s@example.com", playerIDHex[:8]),
		PlayerType:  examples.PlayerType_HUMAN,
	}

	// First registration should succeed
	request1 := makeCommandRequest(
		"player",
		playerID,
		packCommand(cmd, "examples.RegisterPlayer"),
	)

	_, err := sendPlayerCommand(t, request1)
	if err != nil {
		t.Fatalf("First registration should succeed: %v", err)
	}
	t.Log("First registration succeeded")

	// Second registration with same ID should fail
	request2 := makeCommandRequest(
		"player",
		playerID,
		packCommand(cmd, "examples.RegisterPlayer"),
	)

	_, err = sendPlayerCommand(t, request2)
	if err == nil {
		t.Fatal("Duplicate registration should have failed")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("Expected gRPC status error, got: %v", err)
	}

	t.Logf("Duplicate registration correctly rejected: %s", st.Code())

	if st.Code() != codes.AlreadyExists && st.Code() != codes.FailedPrecondition {
		t.Fatalf("Expected AlreadyExists or FailedPrecondition, got %s", st.Code())
	}
}
