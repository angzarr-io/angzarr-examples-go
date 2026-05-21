package tests

import (
	"context"
	"errors"
	"fmt"
	"strings"

	angzarr "github.com/benjaminabbitt/angzarr/client/go"
	"github.com/cucumber/godog"
)

// CommonContext holds shared state across scenarios.
//
// Centralising rejection state here lets cross-aggregate assertion steps
// (e.g. "the command is rejected with code X", "the rejection field Y
// equals Z") observe the most recently raised error regardless of which
// per-aggregate context produced it. Without this, when table.feature
// invokes a Table handler that fails, a player_steps-registered
// assertion would mistakenly read PlayerContext.lastRejection (always
// nil in that scenario) and report "got success".
type CommonContext struct {
	LastError error
}

var sharedContext = &CommonContext{}

// SetLastError stores the last error for assertion in common steps. All
// per-aggregate dispatchers should call this immediately after any
// handler invocation so cross-cutting assertions see a single source of
// truth.
func SetLastError(err error) {
	sharedContext.LastError = err
}

// InitCommonSteps registers common step definitions shared across aggregates
func InitCommonSteps(ctx *godog.ScenarioContext) {
	// Reset before each scenario
	ctx.Before(func(c context.Context, sc *godog.Scenario) (context.Context, error) {
		sharedContext.LastError = nil
		return c, nil
	})

	// Shared step for checking command failures (used across all aggregates)
	ctx.Step(`^the command fails with status "([^"]*)"$`, commandFailsWithStatus)
	ctx.Step(`^the error message contains "([^"]*)"$`, errorMessageContains)
	// Cross-aggregate rejection assertions — read from sharedContext.
	// Registered here so that ANY aggregate's command failure can be
	// asserted with the same step text. Per-aggregate variants still
	// exist on the context types for backward compatibility but they
	// also delegate to sharedContext when their own state is empty.
	ctx.Step(`^the command is rejected with code "([^"]+)"$`, commonCommandRejectedWithCode)
	ctx.Step(`^the rejection field "([^"]+)" equals "([^"]+)"$`, commonRejectionFieldEquals)
}

func commandFailsWithStatus(status string) error {
	if sharedContext.LastError == nil {
		return fmt.Errorf("expected command to fail with status %s, but it succeeded", status)
	}
	// Check if it's a CommandRejectedError (value type, not pointer)
	var cmdErr angzarr.CommandRejectedError
	if !errors.As(sharedContext.LastError, &cmdErr) {
		return fmt.Errorf("expected CommandRejectedError, got %T: %v", sharedContext.LastError, sharedContext.LastError)
	}
	if cmdErr.StatusCode != status {
		return fmt.Errorf("expected status %s, got %s", status, cmdErr.StatusCode)
	}
	return nil
}

func errorMessageContains(text string) error {
	if sharedContext.LastError == nil {
		return fmt.Errorf("expected an error but got success")
	}
	errMsg := strings.ToLower(sharedContext.LastError.Error())
	if !strings.Contains(errMsg, strings.ToLower(text)) {
		return fmt.Errorf("expected error to contain '%s', got '%s'", text, sharedContext.LastError.Error())
	}
	return nil
}

// commonCommandRejectedWithCode asserts the last cross-aggregate command
// failure carries the given SCREAMING_SNAKE code. Inspects
// sharedContext.LastError so it works for any aggregate.
func commonCommandRejectedWithCode(code string) error {
	if sharedContext.LastError == nil {
		return fmt.Errorf("expected rejection with code %q but got success", code)
	}
	var cmdErr angzarr.CommandRejectedError
	if !errors.As(sharedContext.LastError, &cmdErr) {
		return fmt.Errorf("expected CommandRejectedError, got %T: %v", sharedContext.LastError, sharedContext.LastError)
	}
	got := cmdErr.Code
	if got == "" {
		got = inferCodeFromMessage(cmdErr.Message)
	}
	if got != code {
		return fmt.Errorf("expected rejection code %q, got %q (message=%q)", code, got, cmdErr.Message)
	}
	return nil
}

// commonRejectionFieldEquals asserts that the structured Details map on
// the last cross-aggregate rejection carries field=value. Falls back to
// inferDetail for legacy single-string rejections that pre-date the
// Details migration.
func commonRejectionFieldEquals(field, value string) error {
	if sharedContext.LastError == nil {
		return fmt.Errorf("expected a rejection but got success")
	}
	var cmdErr angzarr.CommandRejectedError
	if !errors.As(sharedContext.LastError, &cmdErr) {
		return fmt.Errorf("expected CommandRejectedError, got %T: %v", sharedContext.LastError, sharedContext.LastError)
	}
	got, ok := cmdErr.Details[field]
	if !ok {
		got = inferDetail(cmdErr.Message, field)
	}
	if got != value {
		return fmt.Errorf("expected rejection field %q=%q, got %q", field, value, got)
	}
	return nil
}
