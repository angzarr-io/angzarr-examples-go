//go:build acceptance

package tests

import (
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"google.golang.org/protobuf/types/known/anypb"
)

// CommandClient abstracts how commands are sent to the angzarr system.
// In unit mode (no PLAYER_URL) it dispatches to in-process handlers.
// In acceptance mode (PLAYER_URL set) it sends commands via gRPC.
type CommandClient interface {
	// SendCommand sends a command to the given domain for the given root aggregate.
	// root is the raw UUID bytes; sequence is the event sequence for optimistic concurrency.
	SendCommand(domain string, root []byte, command *anypb.Any, sequence uint32) (*pb.CommandResponse, error)
	// SendCommandWithMode sends a command with explicit sync mode and cascade error mode.
	SendCommandWithMode(domain string, root []byte, command *anypb.Any, sequence uint32, syncMode pb.SyncMode, cascadeErrorMode pb.CascadeErrorMode) (*pb.CommandResponse, error)
	// Close releases any resources held by the client.
	Close()
}
