package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

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
	// logicChange (which means "continued over a logic change").
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
		t.Errorf("reset JSON = %+v, want outcome rebuilt with logicChange false", got)
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
