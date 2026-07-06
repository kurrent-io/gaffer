package apply

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// fakeWriter records calls so applyAction's option mapping and the reset
// sequence can be asserted without a live database. calls records the method
// order; failOn makes the named method (create/update/disable/reset/enable)
// return an error, for mid-sequence failure tests.
type fakeWriter struct {
	creates    int
	updates    int
	createOpts remote.CreateOptions
	updateOpts remote.UpdateOptions
	query      string
	calls      []string
	failOn     string
	err        error
	onCall     func() // fired on every step, for cancellation tests
}

func (f *fakeWriter) step(name string) error {
	f.calls = append(f.calls, name)
	if f.onCall != nil {
		f.onCall()
	}
	if f.failOn == name {
		return fmt.Errorf("%s failed", name)
	}
	return f.err
}

func (f *fakeWriter) Create(_ context.Context, _, query string, opts remote.CreateOptions) error {
	f.creates++
	f.query = query
	f.createOpts = opts
	return f.step("create")
}

func (f *fakeWriter) Update(_ context.Context, _, query string, opts remote.UpdateOptions) error {
	f.updates++
	f.query = query
	f.updateOpts = opts
	return f.step("update")
}

func (f *fakeWriter) Disable(_ context.Context, _ string) error { return f.step("disable") }

func (f *fakeWriter) Reset(_ context.Context, _ string, _ bool) error { return f.step("reset") }

func (f *fakeWriter) Enable(_ context.Context, _ string) error { return f.step("enable") }

// testLedger is the tool metadata applyAction threads onto each write.
var testLedger = remote.Ledger{Tool: remote.ToolName, ToolVersion: "1.2.3-test", Operation: remote.OpDeploy}

func TestApplyActionCreateMapsOptions(t *testing.T) {
	f := &fakeWriter{}
	local := &deploy.Descriptor{Query: "q", EngineVersion: 1, Emit: true, TrackEmittedStreams: true}
	if err := Action(context.Background(), f, "p", drift.ActionCreate, local, testLedger); err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if f.creates != 1 || f.query != "q" {
		t.Fatalf("create not called with query: %+v", f)
	}
	if f.createOpts.EngineVersion != 1 || !f.createOpts.Emit || !f.createOpts.TrackEmittedStreams {
		t.Errorf("create opts = %+v; want EV1 emit+TES true", f.createOpts)
	}
	if f.createOpts.Ledger == nil || *f.createOpts.Ledger != testLedger {
		t.Errorf("create ledger = %+v; want %+v threaded through", f.createOpts.Ledger, testLedger)
	}
}

func TestApplyActionUpdateAlwaysSendsEmit(t *testing.T) {
	for _, emit := range []bool{true, false} {
		f := &fakeWriter{}
		if err := Action(context.Background(), f, "p", drift.ActionUpdate, &deploy.Descriptor{Query: "q", Emit: emit}, testLedger); err != nil {
			t.Fatalf("applyAction: %v", err)
		}
		if f.updates != 1 {
			t.Fatalf("update not called: %+v", f)
		}
		if f.updateOpts.Emit == nil {
			t.Fatal("update Emit is nil; must always be sent explicitly")
		}
		if *f.updateOpts.Emit != emit {
			t.Errorf("update Emit = %v, want %v", *f.updateOpts.Emit, emit)
		}
		if f.updateOpts.Ledger == nil || *f.updateOpts.Ledger != testLedger {
			t.Errorf("update ledger = %+v; want %+v threaded through", f.updateOpts.Ledger, testLedger)
		}
	}
}

func TestApplyActionSkipAndRefuseDoNothing(t *testing.T) {
	for _, action := range []drift.Action{drift.ActionSkip, drift.ActionRefuse} {
		f := &fakeWriter{}
		if err := Action(context.Background(), f, "p", action, &deploy.Descriptor{}, testLedger); err != nil {
			t.Fatalf("Action(%s): %v", action, err)
		}
		if len(f.calls) != 0 {
			t.Errorf("action %s touched the server: %v", action, f.calls)
		}
	}
}

func TestApplyActionResetSequence(t *testing.T) {
	for _, emit := range []bool{true, false} {
		f := &fakeWriter{}
		if err := Action(context.Background(), f, "p", drift.ActionReset, &deploy.Descriptor{Query: "q", Emit: emit}, testLedger); err != nil {
			t.Fatalf("Action(reset): %v", err)
		}
		// stop → update (new query) → reset (rewind) → start.
		if strings.Join(f.calls, ",") != "disable,update,reset,enable" {
			t.Errorf("reset sequence = %v, want disable,update,reset,enable", f.calls)
		}
		if f.updateOpts.Emit == nil || *f.updateOpts.Emit != emit {
			t.Errorf("reset update Emit = %v, want %v sent explicitly", f.updateOpts.Emit, emit)
		}
		if f.updateOpts.Ledger == nil || *f.updateOpts.Ledger != testLedger {
			t.Errorf("reset update ledger = %+v; want %+v stamped on the reset's update", f.updateOpts.Ledger, testLedger)
		}
	}
}

func TestApplyActionResetEnableFailure(t *testing.T) {
	// Enable fails after the reset already wiped state: the error must name the
	// recovery, since there's no auto-rollback.
	f := &fakeWriter{failOn: "enable"}
	err := Action(context.Background(), f, "orders", drift.ActionReset, &deploy.Descriptor{Query: "q"}, testLedger)
	if err == nil {
		t.Fatal("expected an error when enable fails after reset")
	}
	if !strings.Contains(err.Error(), "gaffer enable orders") {
		t.Errorf("error should name the recovery (gaffer enable orders), got: %v", err)
	}
}

func TestApplyActionResetMidSequenceFailure(t *testing.T) {
	// A step failing must stop the sequence (no later steps run) and name the
	// state the projection is left in, since there's no auto-rollback.
	for _, tc := range []struct {
		failOn    string
		wantCalls string
		wantMsg   string
	}{
		{"disable", "disable", "projection untouched"},
		{"update", "disable,update", "gaffer enable orders"},        // stopped on old logic
		{"reset", "disable,update,reset", "gaffer recreate orders"}, // stopped, not rewound
	} {
		f := &fakeWriter{failOn: tc.failOn}
		err := Action(context.Background(), f, "orders", drift.ActionReset, &deploy.Descriptor{Query: "q"}, testLedger)
		if err == nil {
			t.Fatalf("failOn %s: expected an error", tc.failOn)
		}
		if strings.Join(f.calls, ",") != tc.wantCalls {
			t.Errorf("failOn %s: calls = %v, want %s (should stop at the failure)", tc.failOn, f.calls, tc.wantCalls)
		}
		if !strings.Contains(err.Error(), tc.wantMsg) {
			t.Errorf("failOn %s: error should mention %q, got: %v", tc.failOn, tc.wantMsg, err)
		}
	}
}

// recorder captures the Plan callbacks so the loop's accounting and event
// order are assertable.
type recorder struct {
	events  []string
	results []drift.Result
}

func (r *recorder) start(name string, _, _ int) { r.events = append(r.events, "start:"+name) }
func (r *recorder) done(res drift.Result) {
	r.events = append(r.events, "done:"+res.Name)
	r.results = append(r.results, res)
}

// item builds a plan item whose apply has a descriptor to send.
func item(name string, action drift.Action) drift.PlanItem {
	return drift.PlanItem{Name: name, Action: action, Cmp: drift.Comparison{Local: &deploy.Descriptor{Query: "q", EngineVersion: 2}}}
}

func TestPlan(t *testing.T) {
	plan := []drift.PlanItem{
		item("a", drift.ActionCreate),
		{Name: "b", Action: drift.ActionRefuse, Reason: "x"},
		item("c", drift.ActionUpdate), // apply will fail
		{Name: "d", Action: drift.ActionSkip},
		{Name: "e", Err: errors.New("read boom")}, // planning failure
	}
	rec := &recorder{}
	f := &fakeWriter{failOn: "update"} // only "c" updates
	failed := Plan(context.Background(), plan, f, testLedger, rec.start, rec.done)

	// b refused, c failed, e errored at planning: three count as failed.
	if failed != 3 {
		t.Fatalf("failed = %d, want 3", failed)
	}
	if f.creates != 1 || f.updates != 1 {
		t.Fatalf("writer saw creates=%d updates=%d, want 1 and 1 (skip/refuse/error apply nothing)", f.creates, f.updates)
	}
	var outcomes []string
	for _, res := range rec.results {
		outcomes = append(outcomes, res.Name+":"+res.Outcome())
	}
	want := "a:created,b:refused,c:failed,d:skipped,e:failed"
	if strings.Join(outcomes, ",") != want {
		t.Fatalf("outcomes = %v, want %s", outcomes, want)
	}
	// onStart fires for every item - skip, refuse, and planning-error
	// included (the interactive sink spins its active line on it) - and
	// strictly precedes each item's done.
	wantEvents := "start:a,done:a,start:b,done:b,start:c,done:c,start:d,done:d,start:e,done:e"
	if got := strings.Join(rec.events, ","); got != wantEvents {
		t.Fatalf("events = %s, want %s", got, wantEvents)
	}
	// The ledger threads through to the writes.
	if f.createOpts.Ledger == nil || *f.createOpts.Ledger != testLedger {
		t.Errorf("create ledger = %+v, want %+v", f.createOpts.Ledger, testLedger)
	}
}

func TestPlanClearsExternalChangeOnFailure(t *testing.T) {
	ext := func(name string) drift.PlanItem {
		it := item(name, drift.ActionUpdate)
		it.Cmp.State = drift.Drifted
		it.Cmp.Ledger = &remote.Ledger{Tool: remote.ToolName}
		it.Cmp.Deployed = &deploy.Descriptor{Query: "a", EngineVersion: 2}
		it.Cmp.DeployBaseline = &deploy.Descriptor{Query: "b", EngineVersion: 2}
		return it
	}
	rec := &recorder{}
	f := &fakeWriter{failOn: "update"}
	Plan(context.Background(), []drift.PlanItem{ext("boom")}, f, testLedger, rec.start, rec.done)
	if len(rec.results) != 1 || rec.results[0].Err == nil {
		t.Fatalf("results = %+v, want the failed update", rec.results)
	}
	if rec.results[0].ExternalChange {
		t.Error("a failed apply overwrote nothing, so externalChange must be cleared")
	}

	// The successful half: an apply that lands over an external change keeps
	// the flag, so the caller can caution that it overwrote.
	rec = &recorder{}
	Plan(context.Background(), []drift.PlanItem{ext("ok")}, &fakeWriter{}, testLedger, rec.start, rec.done)
	if len(rec.results) != 1 || rec.results[0].Err != nil {
		t.Fatalf("results = %+v, want the successful update", rec.results)
	}
	if !rec.results[0].ExternalChange {
		t.Error("a successful apply over an external change must keep the flag")
	}
}

func TestPlanStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	plan := []drift.PlanItem{item("a", drift.ActionCreate), item("b", drift.ActionCreate), item("c", drift.ActionCreate)}
	rec := &recorder{}
	calls := 0
	f := &fakeWriter{}
	f.onCall = func() {
		calls++
		if calls == 1 {
			cancel()
		}
	}
	Plan(ctx, plan, f, testLedger, rec.start, rec.done)
	if calls != 1 {
		t.Fatalf("writer calls = %d, want the loop to stop after the cancel", calls)
	}
	// The in-flight item still reports its outcome; nothing later starts.
	if got := strings.Join(rec.events, ","); got != "start:a,done:a" {
		t.Fatalf("events = %s, want start:a,done:a only", got)
	}
}

func TestPlanNilOnDonePanicsBeforeWriting(t *testing.T) {
	f := &fakeWriter{}
	defer func() {
		if recover() == nil {
			t.Fatal("nil onDone must panic - silently dropping results would hide what a deploy did")
		}
		if len(f.calls) != 0 {
			t.Fatalf("the panic must fire before any write, got calls %v", f.calls)
		}
	}()
	Plan(context.Background(), []drift.PlanItem{item("a", drift.ActionCreate)}, f, testLedger, nil, nil)
}
