package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/engine"
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
