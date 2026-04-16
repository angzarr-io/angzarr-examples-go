package handlers

import (
	"time"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func transferFundsGuard(state PlayerState) error {
	if !state.Exists() {
		return angzarr.NewCommandRejectedError("Player does not exist")
	}
	return nil
}

func transferFundsValidate(cmd *examples.TransferFunds) (int64, error) {
	amount := int64(0)
	if cmd.Amount != nil {
		amount = cmd.Amount.Amount
	}
	if amount <= 0 {
		return 0, angzarr.NewInvalidArgumentError("amount must be positive")
	}
	if cmd.Reason == "" {
		return 0, angzarr.NewInvalidArgumentError("reason is required")
	}
	return amount, nil
}

func transferFundsCompute(cmd *examples.TransferFunds, state PlayerState, amount int64, toPlayerRoot []byte) *examples.FundsTransferred {
	newBalance := state.Bankroll + amount
	return &examples.FundsTransferred{
		FromPlayerRoot: cmd.FromPlayerRoot,
		ToPlayerRoot:   toPlayerRoot,
		Amount:         cmd.Amount,
		HandRoot:       cmd.HandRoot,
		Reason:         cmd.Reason,
		NewBalance:     &examples.Currency{Amount: newBalance, CurrencyCode: "CHIPS"},
		TransferredAt:  timestamppb.New(time.Now()),
	}
}

// HandleTransferFunds handles the TransferFunds command (receive funds from pot award).
func HandleTransferFunds(
	commandBook *pb.CommandBook,
	commandAny *anypb.Any,
	state PlayerState,
	seq uint32,
) (*pb.EventBook, error) {
	var cmd examples.TransferFunds
	if err := proto.Unmarshal(commandAny.Value, &cmd); err != nil {
		return nil, err
	}

	if err := transferFundsGuard(state); err != nil {
		return nil, err
	}
	amount, err := transferFundsValidate(&cmd)
	if err != nil {
		return nil, err
	}

	// The recipient's root is the aggregate root from the command book cover
	var toPlayerRoot []byte
	if commandBook.Cover != nil && commandBook.Cover.Root != nil {
		toPlayerRoot = commandBook.Cover.Root.Value
	}

	event := transferFundsCompute(&cmd, state, amount, toPlayerRoot)

	eventAny, err := anypb.New(event)
	if err != nil {
		return nil, err
	}

	return angzarr.NewEventBook(commandBook.Cover, seq, eventAny), nil
}
