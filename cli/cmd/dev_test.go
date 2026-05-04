package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
	"github.com/spf13/cobra"
)

func TestFinalizeRun_Interrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := engine.NewRunner(engine.RunnerConfig{})
	r.SetFaulted(true)

	var stderr bytes.Buffer
	err := finalizeRun(ctx, false, nil, r, &stderr)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if !strings.Contains(stderr.String(), "Interrupted") {
		t.Errorf("expected Interrupted message, got %q", stderr.String())
	}
	if r.Faulted() {
		t.Error("expected faulted state to be cleared")
	}
}

func TestFinalizeRun_CaughtUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := engine.NewRunner(engine.RunnerConfig{})

	var stderr bytes.Buffer
	err := finalizeRun(ctx, true, nil, r, &stderr)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if stderr.Len() > 0 {
		t.Errorf("expected no output on caught-up, got %q", stderr.String())
	}
}

func TestFinalizeRun_SourceError(t *testing.T) {
	ctx := context.Background()
	r := engine.NewRunner(engine.RunnerConfig{})
	srcErr := errors.New("subscription dropped")

	var stderr bytes.Buffer
	err := finalizeRun(ctx, false, srcErr, r, &stderr)

	if err != srcErr {
		t.Errorf("expected source error returned, got %v", err)
	}
}

func TestCompleteFixtures(t *testing.T) {
	t.Run("returns names for projection with fixtures", func(t *testing.T) {
		p := testutil.NewProject(t).
			AddProjection("orders", "fromAll().when({});").
			AddNamedFixture("orders", "happy", "[]").
			AddNamedFixture("orders", "edge", "[]").
			Save()
		chdirTo(t, p.Dir)

		names, directive := completeFixtures(nil, []string{"orders"}, "")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("directive: got %v, want NoFileComp", directive)
		}
		// Sorted alphabetically (TOML map storage has no order).
		if len(names) != 2 || names[0] != "edge" || names[1] != "happy" {
			t.Errorf("names: got %v, want [edge happy]", names)
		}
	})

	t.Run("empty list when projection has no fixtures", func(t *testing.T) {
		p := testutil.NewProject(t).
			AddProjection("orders", "fromAll().when({});").
			Save()
		chdirTo(t, p.Dir)

		names, directive := completeFixtures(nil, []string{"orders"}, "")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("directive: got %v, want NoFileComp", directive)
		}
		if len(names) != 0 {
			t.Errorf("expected empty names, got %v", names)
		}
	})

	t.Run("no suggestions when projection arg is missing", func(t *testing.T) {
		names, directive := completeFixtures(nil, []string{}, "")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("directive: got %v, want NoFileComp", directive)
		}
		if names != nil {
			t.Errorf("expected nil names, got %v", names)
		}
	})

	t.Run("silent no-suggestions when projection load fails", func(t *testing.T) {
		// Outside any gaffer project: LoadProjection errors. The
		// completer must NOT bubble that as a shell error - it'd
		// spam users typing in random directories.
		chdirTo(t, t.TempDir())
		names, directive := completeFixtures(nil, []string{"unknown"}, "")
		if directive != cobra.ShellCompDirectiveNoFileComp {
			t.Errorf("directive: got %v, want NoFileComp", directive)
		}
		if names != nil {
			t.Errorf("expected nil names on load failure, got %v", names)
		}
	})
}

func TestFinalizeRun_CleanExit(t *testing.T) {
	ctx := context.Background()
	r := engine.NewRunner(engine.RunnerConfig{})

	var stderr bytes.Buffer
	err := finalizeRun(ctx, false, nil, r, &stderr)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if stderr.Len() > 0 {
		t.Errorf("expected no output, got %q", stderr.String())
	}
}
