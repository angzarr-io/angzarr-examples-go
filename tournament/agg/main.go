// Tournament bounded context gRPC server using functional pattern.
//
// Manages tournament lifecycle: creation, registration, blind levels,
// rebuys, player elimination, and pause/resume.
package main

import (
	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	"github.com/benjaminabbitt/angzarr/examples/go/tournament/agg/handlers"
)

func main() {
	router := angzarr.NewCommandRouter("tournament", handlers.RebuildState).
		On("examples.CreateTournament", handlers.HandleCreateTournament).
		On("examples.OpenRegistration", handlers.HandleOpenRegistration).
		On("examples.CloseRegistration", handlers.HandleCloseRegistration).
		On("examples.EnrollPlayer", handlers.HandleEnrollPlayer).
		On("examples.ProcessRebuy", handlers.HandleProcessRebuy).
		On("examples.AdvanceBlindLevel", handlers.HandleAdvanceBlindLevel).
		On("examples.EliminatePlayer", handlers.HandleEliminatePlayer).
		On("examples.PauseTournament", handlers.HandlePauseTournament).
		On("examples.ResumeTournament", handlers.HandleResumeTournament)

	angzarr.RunCommandHandlerServer("tournament", "50210", router)
}
