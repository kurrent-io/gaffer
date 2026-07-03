package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
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
}

func (f *fakeWriter) step(name string) error {
	f.calls = append(f.calls, name)
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

func (f *fakeWriter) Disable(_ context.Context, _ string) error       { return f.step("disable") }
func (f *fakeWriter) Reset(_ context.Context, _ string, _ bool) error { return f.step("reset") }
func (f *fakeWriter) Enable(_ context.Context, _ string) error        { return f.step("enable") }

// testLedger is the tool metadata applyAction threads onto each write.
var testLedger = remote.Ledger{Tool: remote.ToolName, ToolVersion: "1.2.3-test", Operation: remote.OpDeploy}

func TestApplyActionCreateMapsOptions(t *testing.T) {
	f := &fakeWriter{}
	local := &deploy.Descriptor{Query: "q", EngineVersion: 1, Emit: true, TrackEmittedStreams: true}
	if err := applyAction(context.Background(), f, "p", drift.ActionCreate, local, testLedger); err != nil {
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
		if err := applyAction(context.Background(), f, "p", drift.ActionUpdate, &deploy.Descriptor{Query: "q", Emit: emit}, testLedger); err != nil {
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
		if err := applyAction(context.Background(), f, "p", action, &deploy.Descriptor{}, testLedger); err != nil {
			t.Fatalf("applyAction(%s): %v", action, err)
		}
		if len(f.calls) != 0 {
			t.Errorf("action %s touched the server: %v", action, f.calls)
		}
	}
}

func TestApplyActionResetSequence(t *testing.T) {
	for _, emit := range []bool{true, false} {
		f := &fakeWriter{}
		if err := applyAction(context.Background(), f, "p", drift.ActionReset, &deploy.Descriptor{Query: "q", Emit: emit}, testLedger); err != nil {
			t.Fatalf("applyAction(reset): %v", err)
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
	err := applyAction(context.Background(), f, "orders", drift.ActionReset, &deploy.Descriptor{Query: "q"}, testLedger)
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
		err := applyAction(context.Background(), f, "orders", drift.ActionReset, &deploy.Descriptor{Query: "q"}, testLedger)
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

func TestDeployNames(t *testing.T) {
	cfg := &config.Config{Projection: []config.Projection{{Name: "a"}, {Name: "b"}}}

	all, err := deployNames(cfg, "")
	if err != nil {
		t.Fatalf("deployNames(all): %v", err)
	}
	if strings.Join(all, ",") != "a,b" {
		t.Errorf("all = %v, want [a b]", all)
	}

	one, err := deployNames(cfg, "b")
	if err != nil || strings.Join(one, ",") != "b" {
		t.Errorf("one = %v, err = %v; want [b]", one, err)
	}

	if _, err := deployNames(cfg, "missing"); err == nil {
		t.Error("deployNames(missing) = nil error; want not-in-config error")
	}
}

func TestJSONSink(t *testing.T) {
	var b bytes.Buffer
	s := &jsonSink{w: &b}
	s.done(drift.Result{Name: "a", Action: drift.ActionCreate})
	s.done(drift.Result{Name: "b", Action: drift.ActionRefuse, Reason: "engine version"})
	s.done(drift.Result{Name: "c", Action: drift.ActionUpdate, Err: errors.New("boom")})
	s.done(drift.Result{Name: "d", Action: drift.ActionUpdate, LogicChange: true}) // continued over a logic change
	if err := s.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}

	var got []cliout.DeployJSON
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b.String())
	}
	want := []cliout.DeployJSON{
		{Name: "a", Outcome: "created"},
		{Name: "b", Outcome: "refused", Reason: "engine version"},
		{Name: "c", Outcome: "failed", Error: "boom"},
		{Name: "d", Outcome: "updated", LogicChange: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results, want %d:\n%s", len(got), len(want), b.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestJSONSinkResetOmitsLogicChange(t *testing.T) {
	// A promoted reset still carries logicChange on the plan item, but the result
	// must not leak it into JSON: a rebuild is signalled by outcome "rebuilt", not
	// logic_change (which means "continued over a logic change").
	var b bytes.Buffer
	s := &jsonSink{w: &b}
	s.done((drift.PlanItem{Name: "e", Action: drift.ActionReset, LogicChange: true}).Result())
	if err := s.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	var got []cliout.DeployJSON
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b.String())
	}
	if len(got) != 1 || got[0].Outcome != "rebuilt" || got[0].LogicChange {
		t.Errorf("reset JSON = %+v, want outcome rebuilt with logic_change false", got)
	}
}

func TestPlainSink(t *testing.T) {
	var b bytes.Buffer
	s := newPlainSink(&b, &b, []string{"alpha", "b"})
	s.done(drift.Result{Name: "alpha", Action: drift.ActionCreate})
	s.done(drift.Result{Name: "b", Action: drift.ActionSkip})
	s.done(drift.Result{Name: "c", Action: drift.ActionRefuse, Reason: "engine version (remote 1, local 2) can't be changed in place"})
	s.done(drift.Result{Name: "d", Action: drift.ActionUpdate, Err: errors.New("boom")})
	if err := s.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		"alpha", "created",
		"skipped (in sync)",
		"refused (engine version (remote 1, local 2) can't be changed in place)",
		"failed: boom",
		"1 created", "0 updated", "1 skipped", "1 refused", "1 failed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output missing %q in:\n%s", want, out)
		}
	}
}

func TestJSONSinkEmpty(t *testing.T) {
	var b bytes.Buffer
	s := &jsonSink{w: &b, results: []cliout.DeployJSON{}}
	if err := s.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if got := strings.TrimSpace(b.String()); got != "[]" {
		t.Errorf("empty json = %q, want []", got)
	}
}

func TestNewDeploySink(t *testing.T) {
	var b bytes.Buffer
	if _, ok := newDeploySink(&b, &b, true, []string{"a"}, context.Background(), func() {}).(*jsonSink); !ok {
		t.Error("--json should select the JSON sink")
	}
	// A buffer is not a terminal, so the non-json path must fall to plain (never
	// the interactive program) in pipes, CI, and tests.
	if _, ok := newDeploySink(&b, &b, false, []string{"a"}, context.Background(), func() {}).(*plainSink); !ok {
		t.Error("non-terminal writer should select the plain sink")
	}
	if interactiveWriter(&b) {
		t.Error("a bytes.Buffer must not read as interactive")
	}
	if initialHeight(&b) != 0 {
		t.Error("a bytes.Buffer has no terminal height")
	}
}

// recordingSink captures the sink event stream so the loop's accounting and
// ordering can be asserted without a renderer.
type recordingSink struct {
	events  []string
	results []drift.Result
}

func (s *recordingSink) start(name string, _, _ int) { s.events = append(s.events, "start:"+name) }
func (s *recordingSink) done(res drift.Result) {
	s.events = append(s.events, "done:"+res.Name)
	s.results = append(s.results, res)
}
func (s *recordingSink) finish() error { return nil }

func TestApplyPlan(t *testing.T) {
	plan := []drift.PlanItem{
		{Name: "a", Action: drift.ActionCreate},
		{Name: "b", Action: drift.ActionRefuse, Reason: "x"},
		{Name: "c", Action: drift.ActionUpdate}, // apply will fail
		{Name: "d", Action: drift.ActionSkip},
		{Name: "e", Err: errors.New("read boom")}, // planning failure
	}
	sink := &recordingSink{}
	var applied []string
	failed := applyPlan(context.Background(), plan, sink, func(item drift.PlanItem) error {
		applied = append(applied, item.Name)
		if item.Name == "c" {
			return errors.New("apply boom")
		}
		return nil
	})

	if failed != 3 { // refuse (b) + apply error (c) + planning error (e)
		t.Errorf("failed = %d, want 3 (refuse + apply error + plan error)", failed)
	}
	// apply runs only for create/update items, never refuse/skip/planning-error.
	if strings.Join(applied, ",") != "a,c" {
		t.Errorf("apply called for %v, want [a c]", applied)
	}
	want := []string{"start:a", "done:a", "start:b", "done:b", "start:c", "done:c", "start:d", "done:d", "start:e", "done:e"}
	if strings.Join(sink.events, ",") != strings.Join(want, ",") {
		t.Errorf("event order = %v, want %v", sink.events, want)
	}
	byName := map[string]drift.Result{}
	for _, r := range sink.results {
		byName[r.Name] = r
	}
	if byName["c"].Err == nil {
		t.Error("c should carry the apply error")
	}
	if byName["e"].Err == nil {
		t.Error("e should carry the planning error")
	}
}

func TestApplyPlanClearsExternalChangeOnFailure(t *testing.T) {
	// A successful apply over an external change keeps external_change; a failed one
	// drops it, since the failed apply overwrote nothing.
	plan := []drift.PlanItem{
		{Name: "ok", Action: drift.ActionUpdate, Cmp: changedServer()},
		{Name: "boom", Action: drift.ActionUpdate, Cmp: changedServer()},
	}
	sink := &recordingSink{}
	applyPlan(context.Background(), plan, sink, func(item drift.PlanItem) error {
		if item.Name == "boom" {
			return errors.New("apply failed")
		}
		return nil
	})
	byName := map[string]drift.Result{}
	for _, r := range sink.results {
		byName[r.Name] = r
	}
	if !byName["ok"].ExternalChange {
		t.Error("a successful apply over an external change should keep external_change")
	}
	if byName["boom"].ExternalChange {
		t.Error("a failed apply overwrote nothing, so external_change should be cleared")
	}
}

func TestApplyPlanStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	plan := []drift.PlanItem{{Name: "a", Action: drift.ActionCreate}, {Name: "b", Action: drift.ActionCreate}, {Name: "c", Action: drift.ActionCreate}}
	sink := &recordingSink{}
	calls := 0
	applyPlan(ctx, plan, sink, func(drift.PlanItem) error {
		calls++
		cancel() // interrupt arrives while the first item is applying
		return nil
	})
	if calls != 1 {
		t.Errorf("applied %d items after cancel, want 1 then stop", calls)
	}
	if strings.Join(sink.events, ",") != "start:a,done:a" {
		t.Errorf("events after cancel = %v, want only the first item", sink.events)
	}
}

func newTestTeaModel(names ...string) teaModel {
	rows := make([]deployRow, len(names))
	index := make(map[string]int, len(names))
	for i, n := range names {
		rows[i] = deployRow{name: n, status: rowPending}
		index[n] = i
	}
	return teaModel{
		spinner:   spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		tw:        newTextWriter(io.Discard, io.Discard),
		nameWidth: maxNameWidth(names),
		rows:      rows,
		index:     index,
	}
}

func TestTeaModelTransitions(t *testing.T) {
	m := newTestTeaModel("alpha", "beta") // height 0 -> window shows everything

	// Both projections are listed from the start, all pending.
	if v := m.View(); !strings.Contains(v, "alpha") || !strings.Contains(v, "beta") {
		t.Errorf("initial View missing a projection:\n%s", v)
	}

	started, _ := m.Update(deployStartMsg{name: "alpha"})
	m = testutil.MustType[teaModel](t, started)
	if m.rows[0].status != rowActive {
		t.Fatalf("alpha status = %v, want active", m.rows[0].status)
	}
	if !strings.Contains(m.View(), "deploying") {
		t.Errorf("active row not rendered:\n%s", m.View())
	}

	// Finishing a row commits it to scrollback (a tea.Println command) and drops
	// it from the live window. Not the last row, so no quit yet.
	finished, cmd := m.Update(deployDoneMsg{res: drift.Result{Name: "alpha", Action: drift.ActionCreate}})
	m = testutil.MustType[teaModel](t, finished)
	if m.committed != 1 || m.counts.created != 1 {
		t.Errorf("after done: committed=%d created=%d", m.committed, m.counts.created)
	}
	if producesQuit(cmd) {
		t.Error("a non-final commit must not quit")
	}
	if v := m.View(); strings.Contains(v, "alpha") {
		t.Errorf("committed row should leave the live window:\n%s", v)
	}
	if v := m.View(); !strings.Contains(v, "beta") || !strings.Contains(v, "1 created") {
		t.Errorf("live window should show the pending row and running summary:\n%s", v)
	}

	// Finishing the last row commits it and quits, in one command, so the final
	// line can't be lost to a quit that races the print.
	last, cmd := m.Update(deployDoneMsg{res: drift.Result{Name: "beta", Action: drift.ActionSkip}})
	m = testutil.MustType[teaModel](t, last)
	if m.committed != 2 {
		t.Errorf("committed = %d after the last row, want 2", m.committed)
	}
	if !producesQuit(cmd) {
		t.Error("the last commit should quit the program")
	}
	// View must end with a trailing newline so its last line is empty: bubbletea
	// erases the final rendered line on quit, so the summary must not be it.
	if v := m.View(); !strings.HasSuffix(v, "\n") {
		t.Errorf("View must end with a sacrificial blank line, got %q", v)
	}
}

func TestTeaModelPaging(t *testing.T) {
	m := newTestTeaModel("alpha", "bravo", "charlie", "delta", "echo", "foxtrot")
	m.height = 7 // liveCap = 7 - 3 = 4 -> 3 rows + a "more" indicator

	v := m.View()
	for _, want := range []string{"alpha", "bravo", "charlie", "… 3 more"} {
		if !strings.Contains(v, want) {
			t.Errorf("paged View missing %q:\n%s", want, v)
		}
	}
	for _, gone := range []string{"delta", "echo", "foxtrot"} {
		if strings.Contains(v, gone) {
			t.Errorf("paged View should hide %q below the window:\n%s", gone, v)
		}
	}
}

func TestTeaModelPagingExactFit(t *testing.T) {
	m := newTestTeaModel("alpha", "bravo", "charlie")
	m.height = 6 // liveCap = 6 - 3 = 3, exactly the row count: no indicator
	v := m.View()
	if strings.Contains(v, "more") {
		t.Errorf("an exact fit should not page:\n%s", v)
	}
	for _, n := range []string{"alpha", "bravo", "charlie"} {
		if !strings.Contains(v, n) {
			t.Errorf("exact-fit View missing %q:\n%s", n, v)
		}
	}
}

// As rows commit to scrollback the hidden count shrinks: the indicator counts
// only what's still below the live window, not the whole backlog.
func TestTeaModelPagingShrinksAfterCommit(t *testing.T) {
	m := newTestTeaModel("alpha", "bravo", "charlie", "delta", "echo", "foxtrot")
	m.height = 6 // liveCap 3 -> 2 rows + indicator

	if v := m.View(); !strings.Contains(v, "… 4 more") {
		t.Errorf("initial hidden count wrong:\n%s", v)
	}
	for _, n := range []string{"alpha", "bravo"} {
		started, _ := m.Update(deployStartMsg{name: n})
		m = testutil.MustType[teaModel](t, started)
		done, _ := m.Update(deployDoneMsg{res: drift.Result{Name: n, Action: drift.ActionCreate}})
		m = testutil.MustType[teaModel](t, done)
	}
	if m.committed != 2 {
		t.Fatalf("committed = %d, want 2", m.committed)
	}
	if v := m.View(); !strings.Contains(v, "… 2 more") {
		t.Errorf("hidden count should shrink to 2 after committing two rows:\n%s", v)
	}
}

// An out-of-order completion holds at the front until the gap fills, then one
// completion flushes the whole contiguous prefix in a single commit.
func TestTeaModelCommitsContiguousPrefix(t *testing.T) {
	m := newTestTeaModel("alpha", "bravo", "charlie")

	early, _ := m.Update(deployDoneMsg{res: drift.Result{Name: "bravo", Action: drift.ActionSkip}})
	m = testutil.MustType[teaModel](t, early)
	if m.committed != 0 {
		t.Errorf("committed = %d; nothing should commit while the front row is unfinished", m.committed)
	}

	flush, cmd := m.Update(deployDoneMsg{res: drift.Result{Name: "alpha", Action: drift.ActionCreate}})
	m = testutil.MustType[teaModel](t, flush)
	if m.committed != 2 {
		t.Errorf("committed = %d; finishing alpha should flush alpha+bravo", m.committed)
	}
	if cmd == nil {
		t.Error("the contiguous flush should emit a tea.Println command")
	}
}

func TestTeaModelResize(t *testing.T) {
	m := newTestTeaModel("alpha", "bravo", "charlie", "delta", "echo", "foxtrot")

	small, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 6})
	m = testutil.MustType[teaModel](t, small)
	if m.height != 6 {
		t.Fatalf("height = %d after resize, want 6", m.height)
	}
	if !strings.Contains(m.View(), "more") {
		t.Errorf("should page at height 6:\n%s", m.View())
	}

	big, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 100})
	m = testutil.MustType[teaModel](t, big)
	if strings.Contains(m.View(), "more") {
		t.Errorf("should not page once the terminal is tall enough:\n%s", m.View())
	}
}

func TestTeaModelCtrlCCancels(t *testing.T) {
	cancelled := false
	m := newTestTeaModel("alpha")
	m.cancel = func() { cancelled = true }

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !cancelled {
		t.Error("Ctrl-C should cancel the deploy context")
	}
	if !producesQuit(cmd) {
		t.Error("Ctrl-C should quit the program")
	}
}

// producesQuit reports whether running cmd yields a tea.Quit, directly or inside
// a tea.Sequence (whose message is a slice of commands) - so a quit assertion
// can't pass on just any non-nil command, and still sees a sequenced quit.
func producesQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); ok {
		return true
	}
	rv := reflect.ValueOf(msg)
	if rv.Kind() == reflect.Slice {
		for i := range rv.Len() {
			if c, ok := rv.Index(i).Interface().(tea.Cmd); ok && producesQuit(c) {
				return true
			}
		}
	}
	return false
}
