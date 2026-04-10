//go:build acceptance

package tests

import (
	"context"
	"fmt"
	"os"
	"time"

	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
)

// grpcClient sends commands to running angzarr coordinators via gRPC.
// Routes by domain to the appropriate coordinator endpoint.
type grpcClient struct {
	conns   map[string]*grpc.ClientConn
	clients map[string]pb.CommandHandlerCoordinatorServiceClient
}

// newGRPCClient creates a gRPC command client with per-domain routing.
// playerAddr is required; TABLE_URL and HAND_URL env vars provide table/hand endpoints.
func newGRPCClient(playerAddr string) (*grpcClient, error) {
	c := &grpcClient{
		conns:   make(map[string]*grpc.ClientConn),
		clients: make(map[string]pb.CommandHandlerCoordinatorServiceClient),
	}

	addrs := map[string]string{
		"player": playerAddr,
		"table":  os.Getenv("TABLE_URL"),
		"hand":   os.Getenv("HAND_URL"),
	}
	// Default table/hand to player addr if not set
	if addrs["table"] == "" {
		addrs["table"] = playerAddr
	}
	if addrs["hand"] == "" {
		addrs["hand"] = playerAddr
	}

	for domain, addr := range addrs {
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("failed to create gRPC client for %s (%s): %w", domain, addr, err)
		}
		c.conns[domain] = conn
		c.clients[domain] = pb.NewCommandHandlerCoordinatorServiceClient(conn)
	}
	return c, nil
}

func (g *grpcClient) SendCommand(domain string, root []byte, command *anypb.Any, sequence uint32) (*pb.CommandResponse, error) {
	return g.SendCommandWithMode(domain, root, command, sequence, pb.SyncMode_SYNC_MODE_SIMPLE, pb.CascadeErrorMode_CASCADE_ERROR_FAIL_FAST)
}

func (g *grpcClient) clientForDomain(domain string) pb.CommandHandlerCoordinatorServiceClient {
	if c, ok := g.clients[domain]; ok {
		return c
	}
	// Fall back to player
	return g.clients["player"]
}

func (g *grpcClient) SendCommandWithMode(domain string, root []byte, command *anypb.Any, sequence uint32, syncMode pb.SyncMode, cascadeErrorMode pb.CascadeErrorMode) (*pb.CommandResponse, error) {
	correlationID := uuid.New().String()
	req := &pb.CommandRequest{
		Command: &pb.CommandBook{
			Cover: &pb.Cover{
				Domain:        domain,
				Root:          &pb.UUID{Value: root},
				CorrelationId: correlationID,
			},
			Pages: []*pb.CommandPage{
				{
					Header: &pb.PageHeader{
						SequenceType: &pb.PageHeader_Sequence{
							Sequence: sequence,
						},
					},
					Payload: &pb.CommandPage_Command{
						Command: command,
					},
				},
			},
		},
		SyncMode:         syncMode,
		CascadeErrorMode: cascadeErrorMode,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return g.clientForDomain(domain).HandleCommand(ctx, req)
}

func (g *grpcClient) Close() {
	for _, conn := range g.conns {
		conn.Close()
	}
}
