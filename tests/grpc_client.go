//go:build acceptance

//go:build acceptance

package tests

import (
	"context"
	"fmt"
	"time"

	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/anypb"
)

// grpcClient sends commands to a running angzarr coordinator via gRPC.
type grpcClient struct {
	conn   *grpc.ClientConn
	client pb.CommandHandlerCoordinatorServiceClient
}

// newGRPCClient creates a gRPC command client connected to the given address.
func newGRPCClient(addr string) (*grpcClient, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC client for %s: %w", addr, err)
	}
	return &grpcClient{
		conn:   conn,
		client: pb.NewCommandHandlerCoordinatorServiceClient(conn),
	}, nil
}

func (g *grpcClient) SendCommand(domain string, root []byte, command *anypb.Any, sequence uint32) (*pb.CommandResponse, error) {
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
		SyncMode: pb.SyncMode_SYNC_MODE_SIMPLE,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return g.client.HandleCommand(ctx, req)
}

func (g *grpcClient) Close() {
	if g.conn != nil {
		g.conn.Close()
	}
}
