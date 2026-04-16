//go:build mutation

package tests

import (
	"testing"

	"github.com/gtramontina/ooze"
)

func TestMutation(t *testing.T) {
	ooze.Release(t,
		ooze.WithRepository(".."),
		ooze.WithMinimumThreshold(0.70),
		ooze.Parallel(),
	)
}
