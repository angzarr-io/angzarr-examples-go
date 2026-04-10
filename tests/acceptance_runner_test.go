//go:build acceptance

package tests

import (
	"os"
	"testing"

	"github.com/cucumber/godog"
	"github.com/cucumber/godog/colors"
)

var acceptanceOpts = godog.Options{
	Output:      colors.Colored(os.Stdout),
	Format:      "pretty",
	Paths:       []string{"../angzarr-project/features/acceptance"},
	Randomize:   0,
	Concurrency: 1,
	Strict:      false, // Allow pending scenarios without failing
}

func TestAcceptanceFeatures(t *testing.T) {
	suite := godog.TestSuite{
		ScenarioInitializer: InitializeAcceptanceScenario,
		Options:             &acceptanceOpts,
	}

	if suite.Run() != 0 {
		t.Fail()
	}
}

func InitializeAcceptanceScenario(ctx *godog.ScenarioContext) {
	// Acceptance steps use CommandClient (in-process or gRPC)
	InitAcceptanceSteps(ctx)
}
