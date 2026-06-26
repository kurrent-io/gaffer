package dap

import (
	"testing"

	godap "github.com/google/go-dap"
)

func TestStats_ZeroBeforeActivity(t *testing.T) {
	// Construct a zero-value Server directly; the counter bumps
	// happen in dispatch before any I/O, so we don't need to
	// stand up a listener or open the send channel.
	s := &Server{}
	got := s.Stats()
	if got != (Stats{}) {
		t.Errorf("Stats = %+v, want zero", got)
	}
}

func TestStats_BreakpointsBumpedOnSetBreakpoints(t *testing.T) {
	s := &Server{}
	for range 4 {
		s.dispatch(&godap.SetBreakpointsRequest{})
	}
	if got := s.Stats().BreakpointCount; got != 4 {
		t.Errorf("BreakpointCount = %d, want 4", got)
	}
}

func TestStats_StepsCoverNextStepInStepOut(t *testing.T) {
	// All three step variants feed the same counter - the schema
	// (StepCount) doesn't break out next/in/out, and editor
	// behaviour treats them as a single "step" category.
	s := &Server{}
	s.dispatch(&godap.NextRequest{})
	s.dispatch(&godap.StepInRequest{})
	s.dispatch(&godap.StepOutRequest{})
	if got := s.Stats().StepCount; got != 3 {
		t.Errorf("StepCount = %d, want 3 (next+in+out)", got)
	}
}

func TestStats_PauseAndRestartTracked(t *testing.T) {
	s := &Server{}
	s.dispatch(&godap.PauseRequest{})
	s.dispatch(&godap.PauseRequest{})
	s.dispatch(&godap.RestartRequest{})
	stats := s.Stats()
	if stats.PauseCount != 2 {
		t.Errorf("PauseCount = %d, want 2", stats.PauseCount)
	}
	if stats.RestartCount != 1 {
		t.Errorf("RestartCount = %d, want 1", stats.RestartCount)
	}
}

func TestStats_OtherRequestsDoNotBumpAnyCounter(t *testing.T) {
	// Sanity: requests outside the tracked set (continue,
	// threads, stack-trace, etc) leave all counters zero.
	// Catches a future refactor that accidentally bumps the
	// wrong counter from an unrelated dispatch branch.
	s := &Server{}
	s.dispatch(&godap.ContinueRequest{})
	s.dispatch(&godap.ThreadsRequest{})
	s.dispatch(&godap.StackTraceRequest{})
	if got := s.Stats(); got != (Stats{}) {
		t.Errorf("Stats = %+v, want zero (untracked requests)", got)
	}
}
