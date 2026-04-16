// Projector: Output
//
// Subscribes to player, table, and hand domain events.
// Writes formatted game logs to a file.
//
// Uses the OO-style implementation with ProjectorBase, method-based
// handlers, and fluent registration.
package main

import (
	"fmt"
	"os"
	"time"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	pb "github.com/benjaminabbitt/angzarr/client/go/proto/angzarr"
	"github.com/benjaminabbitt/angzarr/client/go/proto/examples"
)

var logFile *os.File

func getLogFile() *os.File {
	if logFile == nil {
		path := os.Getenv("HAND_LOG_FILE")
		if path == "" {
			path = "hand_log.txt"
		}
		var err error
		logFile, err = os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open log file: %v\n", err)
		}
	}
	return logFile
}

func writeLog(msg string) {
	f := getLogFile()
	if f != nil {
		timestamp := time.Now().Format("2006-01-02T15:04:05.000")
		_, _ = f.WriteString(fmt.Sprintf("[%s] %s\n", timestamp, msg))
	}
}

// docs:start:projector_oo
// OutputProjector writes game events to a log file.
type OutputProjector struct {
	angzarr.ProjectorBase
}

// NewOutputProjector creates a new OutputProjector with registered handlers.
func NewOutputProjector() *OutputProjector {
	p := &OutputProjector{}
	p.Init("output", []string{"player", "table", "hand"})

	// Register projection handlers
	p.Projects(p.ProjectPlayerRegistered)
	p.Projects(p.ProjectFundsDeposited)
	p.Projects(p.ProjectTableCreated)
	p.Projects(p.ProjectPlayerJoined)
	p.Projects(p.ProjectHandStarted)
	p.Projects(p.ProjectCardsDealt)
	p.Projects(p.ProjectBlindPosted)
	p.Projects(p.ProjectActionTaken)
	p.Projects(p.ProjectPotAwarded)
	p.Projects(p.ProjectHandComplete)

	return p
}

func (p *OutputProjector) ProjectPlayerRegistered(event *examples.PlayerRegistered) *pb.Projection {
	writeLog(fmt.Sprintf("PLAYER registered: %s (%s)", event.DisplayName, event.Email))
	return nil
}

func (p *OutputProjector) ProjectFundsDeposited(event *examples.FundsDeposited) *pb.Projection {
	amount := int64(0)
	newBalance := int64(0)
	if event.Amount != nil {
		amount = event.Amount.Amount
	}
	if event.NewBalance != nil {
		newBalance = event.NewBalance.Amount
	}
	writeLog(fmt.Sprintf("PLAYER deposited %d, balance: %d", amount, newBalance))
	return nil
}

func (p *OutputProjector) ProjectTableCreated(event *examples.TableCreated) *pb.Projection {
	writeLog(fmt.Sprintf("TABLE created: %s (%s)", event.TableName, event.GameVariant.String()))
	return nil
}

func (p *OutputProjector) ProjectPlayerJoined(event *examples.PlayerJoined) *pb.Projection {
	playerID := angzarr.BytesToUUIDText(event.PlayerRoot)
	writeLog(fmt.Sprintf("TABLE player %s joined with %d chips", playerID, event.Stack))
	return nil
}

func (p *OutputProjector) ProjectHandStarted(event *examples.HandStarted) *pb.Projection {
	writeLog(fmt.Sprintf("TABLE hand #%d started, %d players, dealer at position %d",
		event.HandNumber, len(event.ActivePlayers), event.DealerPosition))
	return nil
}

func (p *OutputProjector) ProjectCardsDealt(event *examples.CardsDealt) *pb.Projection {
	writeLog(fmt.Sprintf("HAND cards dealt to %d players", len(event.PlayerCards)))
	return nil
}

func (p *OutputProjector) ProjectBlindPosted(event *examples.BlindPosted) *pb.Projection {
	playerID := angzarr.BytesToUUIDText(event.PlayerRoot)
	writeLog(fmt.Sprintf("HAND player %s posted %s blind: %d", playerID, event.BlindType, event.Amount))
	return nil
}

func (p *OutputProjector) ProjectActionTaken(event *examples.ActionTaken) *pb.Projection {
	playerID := angzarr.BytesToUUIDText(event.PlayerRoot)
	writeLog(fmt.Sprintf("HAND player %s: %s %d", playerID, event.Action.String(), event.Amount))
	return nil
}

func (p *OutputProjector) ProjectPotAwarded(event *examples.PotAwarded) *pb.Projection {
	winners := make([]string, len(event.Winners))
	for i, w := range event.Winners {
		winners[i] = fmt.Sprintf("%s wins %d", angzarr.BytesToUUIDText(w.PlayerRoot), w.Amount)
	}
	writeLog(fmt.Sprintf("HAND pot awarded: %v", winners))
	return nil
}

func (p *OutputProjector) ProjectHandComplete(event *examples.HandComplete) *pb.Projection {
	writeLog(fmt.Sprintf("HAND #%d complete", event.HandNumber))
	return nil
}

// docs:end:projector_oo

func main() {
	// Clear log file at startup
	path := os.Getenv("HAND_LOG_FILE")
	if path == "" {
		path = "hand_log.txt"
	}
	os.Remove(path)

	projector := NewOutputProjector()
	angzarr.RunOOProjectorServer("output", "50290", &projector.ProjectorBase)
}
