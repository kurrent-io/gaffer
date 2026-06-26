package deploy

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// recorder builds named steps that record their order and optionally fail. It
// stands in for the bound remote client so the sequence's ordering and recovery
// messages are testable without a live database.
type recorder struct {
	calls  []string
	failOn string
}

func (r *recorder) step(name string) Step {
	return func(context.Context) error {
		r.calls = append(r.calls, name)
		if r.failOn == name {
			return fmt.Errorf("%s failed", name)
		}
		return nil
	}
}

func (r *recorder) rebuildSteps() RebuildSteps {
	return RebuildSteps{Disable: r.step("disable"), Update: r.step("update"), Reset: r.step("reset"), Enable: r.step("enable")}
}

func (r *recorder) recreateSteps() RecreateSteps {
	return RecreateSteps{Disable: r.step("disable"), Delete: r.step("delete"), Create: r.step("create")}
}

func TestRebuildHappyPath(t *testing.T) {
	r := &recorder{}
	if err := Rebuild(context.Background(), "orders", r.rebuildSteps()); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	// stop -> update (new query) -> reset (rewind) -> start.
	if got := strings.Join(r.calls, ","); got != "disable,update,reset,enable" {
		t.Errorf("rebuild sequence = %q, want disable,update,reset,enable", got)
	}
}

func TestRebuildMidSequenceFailure(t *testing.T) {
	// A step failing must stop the sequence (no later steps run) and name the state
	// the projection is left in, since there's no auto-rollback.
	for _, tc := range []struct {
		failOn    string
		wantCalls string
		wantMsg   string
	}{
		{"disable", "disable", "projection untouched"},
		{"update", "disable,update", "gaffer start orders"},         // stopped on old logic
		{"reset", "disable,update,reset", "gaffer recreate orders"}, // stopped, not rewound
		{"enable", "disable,update,reset,enable", "gaffer start orders"},
	} {
		r := &recorder{failOn: tc.failOn}
		err := Rebuild(context.Background(), "orders", r.rebuildSteps())
		if err == nil {
			t.Fatalf("failOn %s: expected an error", tc.failOn)
		}
		if got := strings.Join(r.calls, ","); got != tc.wantCalls {
			t.Errorf("failOn %s: calls = %q, want %s (should stop at the failure)", tc.failOn, got, tc.wantCalls)
		}
		if !strings.Contains(err.Error(), tc.wantMsg) {
			t.Errorf("failOn %s: error should mention %q, got: %v", tc.failOn, tc.wantMsg, err)
		}
		if !strings.Contains(err.Error(), tc.failOn+" failed") {
			t.Errorf("failOn %s: error should wrap the underlying failure, got: %v", tc.failOn, err)
		}
	}
}

func TestRecreateHappyPath(t *testing.T) {
	r := &recorder{}
	if err := Recreate(context.Background(), "orders", r.recreateSteps()); err != nil {
		t.Fatalf("Recreate: %v", err)
	}
	// stop -> delete (destroy) -> create (rebuild from local).
	if got := strings.Join(r.calls, ","); got != "disable,delete,create" {
		t.Errorf("recreate sequence = %q, want disable,delete,create", got)
	}
}

func TestRecreateMidSequenceFailure(t *testing.T) {
	// The destroy precedes the create, so the recovery message must change once the
	// projection is gone (after delete). Each step stops the sequence at the failure.
	for _, tc := range []struct {
		failOn    string
		wantCalls string
		wantMsg   string
	}{
		{"disable", "disable", "could not stop orders before recreating"},
		{"delete", "disable,delete", "could not delete orders before recreating"},
		{"create", "disable,delete,create", "orders was deleted but recreating it failed"},
	} {
		r := &recorder{failOn: tc.failOn}
		err := Recreate(context.Background(), "orders", r.recreateSteps())
		if err == nil {
			t.Fatalf("failOn %s: expected an error", tc.failOn)
		}
		if got := strings.Join(r.calls, ","); got != tc.wantCalls {
			t.Errorf("failOn %s: calls = %q, want %s (should stop at the failure)", tc.failOn, got, tc.wantCalls)
		}
		if !strings.Contains(err.Error(), tc.wantMsg) {
			t.Errorf("failOn %s: error should mention %q, got: %v", tc.failOn, tc.wantMsg, err)
		}
		if !strings.Contains(err.Error(), tc.failOn+" failed") {
			t.Errorf("failOn %s: error should wrap the underlying failure, got: %v", tc.failOn, err)
		}
	}
}

func TestRecreateCreateFailureNamesBothRecoveries(t *testing.T) {
	// After delete the projection is gone; the create failure must point at both
	// the recreate retry and the deploy fallback.
	r := &recorder{failOn: "create"}
	err := Recreate(context.Background(), "orders", r.recreateSteps())
	if err == nil {
		t.Fatal("expected an error when create fails after delete")
	}
	for _, want := range []string{"gaffer recreate orders", "gaffer deploy orders"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("create-failure recovery missing %q, got: %v", want, err)
		}
	}
}

func TestNilStepRejectedBeforeAnyCall(t *testing.T) {
	// A wiring gap (a nil step) must be refused before any step runs: these
	// sequences are destructive, so a nil caught mid-flight could strand a
	// half-rebuilt projection.
	t.Run("rebuild", func(t *testing.T) {
		r := &recorder{}
		s := r.rebuildSteps()
		s.Reset = nil
		err := Rebuild(context.Background(), "orders", s)
		if err == nil || !strings.Contains(err.Error(), "reset step is not wired") {
			t.Fatalf("want 'reset step is not wired', got: %v", err)
		}
		if len(r.calls) != 0 {
			t.Errorf("no step should run when one is unwired, ran: %v", r.calls)
		}
	})
	t.Run("recreate", func(t *testing.T) {
		r := &recorder{}
		s := r.recreateSteps()
		s.Create = nil
		err := Recreate(context.Background(), "orders", s)
		if err == nil || !strings.Contains(err.Error(), "create step is not wired") {
			t.Fatalf("want 'create step is not wired', got: %v", err)
		}
		if len(r.calls) != 0 {
			t.Errorf("no step should run when one is unwired, ran: %v", r.calls)
		}
	})
}

func TestRecreateReason(t *testing.T) {
	for _, tc := range []struct {
		name            string
		cmp             Comparison
		local, deployed Descriptor
		want            []string
	}{
		{
			name:     "engine version only",
			cmp:      Comparison{EngineVersionDiffers: true},
			local:    Descriptor{EngineVersion: 2},
			deployed: Descriptor{EngineVersion: 1},
			want:     []string{"engine version (remote 1, local 2)", "can't be changed in place", "gaffer recreate"},
		},
		{
			name:     "track emitted streams only",
			cmp:      Comparison{TrackEmittedStreamsDiffers: true},
			local:    Descriptor{TrackEmittedStreams: true},
			deployed: Descriptor{TrackEmittedStreams: false},
			want:     []string{"track emitted streams (remote false, local true)"},
		},
		{
			name:     "both create-time fields",
			cmp:      Comparison{EngineVersionDiffers: true, TrackEmittedStreamsDiffers: true},
			local:    Descriptor{EngineVersion: 2, TrackEmittedStreams: true},
			deployed: Descriptor{EngineVersion: 1, TrackEmittedStreams: false},
			want:     []string{"engine version (remote 1, local 2)", "track emitted streams (remote false, local true)"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := RecreateReason(tc.cmp, tc.local, tc.deployed)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("reason %q missing %q", got, want)
				}
			}
		})
	}
}
