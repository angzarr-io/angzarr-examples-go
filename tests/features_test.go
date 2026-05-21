package tests

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
)

var opts = godog.Options{
	Output:      colors.Colored(os.Stdout),
	Format:      "progress",
	Paths:       []string{"../angzarr-project/features/example/unit"},
	Randomize:   0,
	Concurrency: 1,
	Strict:      false, // Allow pending scenarios without failing
}

func TestFeatures(t *testing.T) {
	// Honor GODOG_PATHS / GODOG_FORMAT at run time so a focused agent can
	// scope a run (e.g. GODOG_PATHS=../angzarr-project/features/example/unit/player.feature)
	// without editing this file or the production opts var.
	runOpts := opts
	if raw := os.Getenv("GODOG_PATHS"); raw != "" {
		runOpts.Paths = splitColon(raw)
	}
	if f := os.Getenv("GODOG_FORMAT"); f != "" {
		runOpts.Format = f
	}
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeScenario,
		Options:             &runOpts,
	}

	if suite.Run() != 0 {
		t.Fail()
	}
}

// splitColon mimics strings.Split(s, ":") without pulling the strings
// pkg into this file. Used only by env-var-driven path override above.
func splitColon(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	// Common shared-state assertions (rejection code, rejection field,
	// command-fails-with-status, error-message-contains). Registered
	// FIRST because godog uses first-match-wins; the common variants
	// read from sharedContext.LastError, which every per-aggregate
	// dispatcher updates via SetLastError. This is what makes a single
	// "the command is rejected with code X" step text work for ALL
	// aggregate scenarios — without it, the first per-aggregate variant
	// to register wins for ALL features, including unrelated ones.
	InitCommonSteps(ctx)

	// Game-rules step bindings — REAL implementations driven by the
	// hand/agg/gamerules engine. Registered next so godog uses these
	// real bindings instead of overlapping bindings from hand_steps,
	// acceptance_steps, or pending_steps for the game_rules.feature
	// regexes. Other suites (hand.feature etc.) still get their own
	// scenario-world bindings registered below.
	RegisterGameRulesSteps(ctx)

	// Player aggregate steps
	InitPlayerSteps(ctx)

	// Table aggregate steps
	InitTableSteps(ctx)

	// Hand aggregate steps
	InitHandSteps(ctx)

	// Tournament aggregate steps — registered BEFORE pm/saga/orch so the
	// `^the tournament state has X$` regexes route to the real tournament
	// aggregate state (via the TournamentContext maintained by
	// tournament_steps.go) rather than the PM-scenario TournamentStateHelper
	// snapshot (which is only populated by pm_saga_orch_steps's rebuild
	// helper). godog uses first-match-wins so registration order here is
	// the steering wheel.
	RegisterTournamentSteps(ctx)

	// Saga steps
	RegisterSagaSteps(ctx)

	// Process manager steps
	RegisterPMSteps(ctx)

	// Orchestration PM steps (BuyIn, Registration, Rebuy)
	RegisterOrchestrationSteps(ctx)

	// SagaHandleRequest-router + per-flow BuyInPM/RebuyPM/RegistrationPM
	// + TournamentStateHelper bindings — multi-aggregate orchestration
	// infra exercised by process_manager.feature and saga.feature.
	InitOrchPMSagaSteps(ctx)

	// Projector steps
	RegisterProjectorSteps(ctx)

	// Single-feature pure-math step modules registered AFTER the
	// per-aggregate context modules so their short-form patterns
	// (e.g. `^current_bet is (\d+)$`, `^last_raise_increment is (\d+)$`)
	// do NOT shadow the PM/hand/table variants. Godog uses
	// first-match-wins; raise_tracking.feature steps are unique enough
	// that the PM/hand bindings won't match its scenarios, and the
	// pure-math bindings won't match PM/hand scenarios that share short
	// patterns (PM's `current_bet is N` Given is intercepted by the
	// PM-context binding above).
	InitBettingRoundSteps(ctx)
	InitRaiseTrackingSteps(ctx)

	// (InitCommonSteps moved to top of this function — see comment there.)

	// Pending step registrations for the post-submodule-bump cucumber surface
	// (1011 previously-undefined step regexes; bodies return godog.ErrPending
	// until I-Go-v2a's handler-port pass wires real implementations).
	// Registered LAST so real bindings above take precedence on overlap.
	RegisterPendingSteps(ctx)
}
