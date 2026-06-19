package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func TestPlanAction(t *testing.T) {
	drift := func(c deploy.Comparison, local, deployed *deploy.Descriptor) comparison {
		return comparison{State: driftDrifted, Cmp: c, Local: local, Deployed: deployed}
	}
	tests := []struct {
		name       string
		in         comparison
		wantAction deployAction
		wantReason []string // substrings the refuse reason must contain
	}{
		{"not deployed creates", comparison{State: driftNotDeployed}, actCreate, nil},
		{"in sync skips", comparison{State: driftInSync}, actSkip, nil},
		{"query drift updates", drift(deploy.Comparison{QueryDiffers: true}, desc("a", 2, false), desc("b", 2, false)), actUpdate, nil},
		{"emit drift updates", drift(deploy.Comparison{EmitDiffers: true}, desc("a", 2, true), desc("a", 2, false)), actUpdate, nil},
		{
			"engine version drift refuses",
			drift(deploy.Comparison{EngineVersionDiffers: true}, desc("a", 2, false), desc("a", 1, false)),
			actRefuse,
			[]string{"engine version (remote 1, local 2)", "can't be changed in place"},
		},
		{
			"track emitted streams drift refuses",
			drift(deploy.Comparison{TrackEmittedStreamsDiffers: true},
				&deploy.Descriptor{EngineVersion: 1, TrackEmittedStreams: true},
				&deploy.Descriptor{EngineVersion: 1, TrackEmittedStreams: false}),
			actRefuse,
			[]string{"track emitted streams (remote false, local true)"},
		},
		{
			"both create-time fields drift refuses with both",
			drift(deploy.Comparison{EngineVersionDiffers: true, TrackEmittedStreamsDiffers: true},
				&deploy.Descriptor{EngineVersion: 1, TrackEmittedStreams: false},
				&deploy.Descriptor{EngineVersion: 2, TrackEmittedStreams: true}),
			actRefuse,
			[]string{"engine version (remote 2, local 1)", "track emitted streams (remote true, local false)"},
		},
		{"query and emit drift still updates", drift(deploy.Comparison{QueryDiffers: true, EmitDiffers: true}, desc("a", 2, true), desc("b", 2, false)), actUpdate, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, reason := planAction(tt.in)
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
			if len(tt.wantReason) == 0 && reason != "" {
				t.Errorf("reason = %q, want empty", reason)
			}
			for _, want := range tt.wantReason {
				if !strings.Contains(reason, want) {
					t.Errorf("reason %q missing %q", reason, want)
				}
			}
		})
	}
}

// fakeWriter records the last create/update so applyAction's option mapping can
// be asserted without a live database.
type fakeWriter struct {
	creates    int
	updates    int
	createOpts remote.CreateOptions
	updateOpts remote.UpdateOptions
	query      string
	err        error
}

func (f *fakeWriter) Create(_ context.Context, _, query string, opts remote.CreateOptions) error {
	f.creates++
	f.query = query
	f.createOpts = opts
	return f.err
}

func (f *fakeWriter) Update(_ context.Context, _, query string, opts remote.UpdateOptions) error {
	f.updates++
	f.query = query
	f.updateOpts = opts
	return f.err
}

func TestApplyActionCreateMapsOptions(t *testing.T) {
	f := &fakeWriter{}
	local := &deploy.Descriptor{Query: "q", EngineVersion: 1, Emit: true, TrackEmittedStreams: true}
	if err := applyAction(context.Background(), f, "p", actCreate, local); err != nil {
		t.Fatalf("applyAction: %v", err)
	}
	if f.creates != 1 || f.query != "q" {
		t.Fatalf("create not called with query: %+v", f)
	}
	if f.createOpts.EngineVersion != 1 || !f.createOpts.Emit || !f.createOpts.TrackEmittedStreams {
		t.Errorf("create opts = %+v; want EV1 emit+TES true", f.createOpts)
	}
}

func TestApplyActionUpdateAlwaysSendsEmit(t *testing.T) {
	for _, emit := range []bool{true, false} {
		f := &fakeWriter{}
		if err := applyAction(context.Background(), f, "p", actUpdate, &deploy.Descriptor{Query: "q", Emit: emit}); err != nil {
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
	}
}

func TestApplyActionSkipAndRefuseDoNothing(t *testing.T) {
	for _, action := range []deployAction{actSkip, actRefuse} {
		f := &fakeWriter{}
		if err := applyAction(context.Background(), f, "p", action, &deploy.Descriptor{}); err != nil {
			t.Fatalf("applyAction(%s): %v", action, err)
		}
		if f.creates != 0 || f.updates != 0 {
			t.Errorf("action %s touched the server: %+v", action, f)
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
	s.done(deployResult{Name: "a", Action: actCreate})
	s.done(deployResult{Name: "b", Action: actRefuse, Reason: "engine version"})
	s.done(deployResult{Name: "c", Action: actUpdate, Err: errors.New("boom")})
	if err := s.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}

	var got []deployJSON
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b.String())
	}
	want := []deployJSON{
		{Name: "a", Outcome: "created"},
		{Name: "b", Outcome: "refused", Reason: "engine version"},
		{Name: "c", Outcome: "failed", Error: "boom"},
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

func TestPlainSink(t *testing.T) {
	var b bytes.Buffer
	s := newPlainSink(&b, &b, []string{"alpha", "b"})
	s.done(deployResult{Name: "alpha", Action: actCreate})
	s.done(deployResult{Name: "b", Action: actSkip})
	s.done(deployResult{Name: "c", Action: actRefuse, Reason: "engine version (remote 1, local 2) can't be changed in place"})
	s.done(deployResult{Name: "d", Action: actUpdate, Err: errors.New("boom")})
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
	s := &jsonSink{w: &b, results: []deployJSON{}}
	if err := s.finish(); err != nil {
		t.Fatalf("finish: %v", err)
	}
	if got := strings.TrimSpace(b.String()); got != "[]" {
		t.Errorf("empty json = %q, want []", got)
	}
}

func TestNewDeploySink(t *testing.T) {
	var b bytes.Buffer
	if _, ok := newDeploySink(&b, &b, true, []string{"a"}, func() {}).(*jsonSink); !ok {
		t.Error("--json should select the JSON sink")
	}
	// A buffer is not a terminal, so the non-json path must fall to plain (never
	// the interactive program) in pipes, CI, and tests.
	if _, ok := newDeploySink(&b, &b, false, []string{"a"}, func() {}).(*plainSink); !ok {
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
	results []deployResult
}

func (s *recordingSink) start(name string, _, _ int) { s.events = append(s.events, "start:"+name) }
func (s *recordingSink) done(res deployResult) {
	s.events = append(s.events, "done:"+res.Name)
	s.results = append(s.results, res)
}
func (s *recordingSink) finish() error { return nil }

func TestRunDeployLoop(t *testing.T) {
	names := []string{"a", "b", "c", "d"}
	plan := map[string]deployResult{
		"a": {Name: "a", Action: actCreate},
		"b": {Name: "b", Action: actRefuse, Reason: "x"},
		"c": {Name: "c", Action: actUpdate, Err: errors.New("boom")},
		"d": {Name: "d", Action: actSkip},
	}
	sink := &recordingSink{}
	failed := runDeployLoop(context.Background(), names, sink, func(n string) deployResult { return plan[n] })

	if failed != 2 { // refuse (b) + error (c)
		t.Errorf("failed = %d, want 2 (refuse + error)", failed)
	}
	want := []string{"start:a", "done:a", "start:b", "done:b", "start:c", "done:c", "start:d", "done:d"}
	if strings.Join(sink.events, ",") != strings.Join(want, ",") {
		t.Errorf("event order = %v, want %v", sink.events, want)
	}
}

func TestRunDeployLoopStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sink := &recordingSink{}
	calls := 0
	runDeployLoop(ctx, []string{"a", "b", "c"}, sink, func(n string) deployResult {
		calls++
		cancel() // interrupt arrives while the first projection is in flight
		return deployResult{Name: n, Action: actCreate}
	})
	if calls != 1 {
		t.Errorf("did %d projections after cancel, want 1 then stop", calls)
	}
	if strings.Join(sink.events, ",") != "start:a,done:a" {
		t.Errorf("events after cancel = %v, want only the first projection", sink.events)
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
	m = started.(teaModel)
	if m.rows[0].status != rowActive {
		t.Fatalf("alpha status = %v, want active", m.rows[0].status)
	}
	if !strings.Contains(m.View(), "deploying") {
		t.Errorf("active row not rendered:\n%s", m.View())
	}

	// Finishing a row commits it to scrollback (a tea.Println command) and drops
	// it from the live window.
	finished, cmd := m.Update(deployDoneMsg{res: deployResult{Name: "alpha", Action: actCreate}})
	m = finished.(teaModel)
	if m.committed != 1 || m.counts.created != 1 {
		t.Errorf("after done: committed=%d created=%d", m.committed, m.counts.created)
	}
	if cmd == nil {
		t.Error("done should commit the finished row via a tea.Println command")
	}
	if v := m.View(); strings.Contains(v, "alpha") {
		t.Errorf("committed row should leave the live window:\n%s", v)
	}
	if v := m.View(); !strings.Contains(v, "beta") || !strings.Contains(v, "1 created") {
		t.Errorf("live window should show the pending row and running summary:\n%s", v)
	}

	_, cmd = m.Update(deployFinishMsg{})
	if !isQuit(cmd) {
		t.Error("finish should quit the program")
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
		m = started.(teaModel)
		done, _ := m.Update(deployDoneMsg{res: deployResult{Name: n, Action: actCreate}})
		m = done.(teaModel)
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

	early, _ := m.Update(deployDoneMsg{res: deployResult{Name: "bravo", Action: actSkip}})
	m = early.(teaModel)
	if m.committed != 0 {
		t.Errorf("committed = %d; nothing should commit while the front row is unfinished", m.committed)
	}

	flush, cmd := m.Update(deployDoneMsg{res: deployResult{Name: "alpha", Action: actCreate}})
	m = flush.(teaModel)
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
	m = small.(teaModel)
	if m.height != 6 {
		t.Fatalf("height = %d after resize, want 6", m.height)
	}
	if !strings.Contains(m.View(), "more") {
		t.Errorf("should page at height 6:\n%s", m.View())
	}

	big, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 100})
	m = big.(teaModel)
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
	if !isQuit(cmd) {
		t.Error("Ctrl-C should quit the program")
	}
}

// isQuit reports whether cmd is tea.Quit, by running it and checking for the
// QuitMsg - so a quit assertion can't pass on just any non-nil command.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}
