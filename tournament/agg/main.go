// Tournament bounded context gRPC server using OO pattern.
//
// This command handler uses the OO-style pattern with embedded CommandHandlerBase,
// method-based handlers, and fluent registration. Manages tournament lifecycle:
// creation, registration, blind levels, rebuys, player elimination, and pause/resume.
package main

import angzarr "github.com/benjaminabbitt/angzarr/client/go"

func main() {
	angzarr.RunOOCommandHandlerServer[TournamentState, *Tournament]("tournament", "50210", NewTournament)
}
