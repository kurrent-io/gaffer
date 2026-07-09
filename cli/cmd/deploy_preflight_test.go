package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func TestPreflightFailureReasons(t *testing.T) {
	compile := preflightFailure{Name: "a", CompileErr: errors.New("Unexpected token (3:5)")}
	if got := compile.reasons(); len(got) != 1 || got[0] != "Unexpected token (3:5)" {
		t.Errorf("compile reasons = %v, want the error message", got)
	}

	diag := preflightFailure{Name: "b", Diagnostics: []gafferruntime.Diagnostic{
		{Code: "quirk.linkStreamTo.outOfBoundsParameters", Message: "crashes the engine"},
		{Code: "quirk.foo.bar", Message: "also bad"},
	}}
	got := diag.reasons()
	want := []string{
		"quirk.linkStreamTo.outOfBoundsParameters: crashes the engine",
		"quirk.foo.bar: also bad",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("diagnostic reasons = %v, want %v", got, want)
	}
}

func TestRenderPreflightFailuresJSON(t *testing.T) {
	var b bytes.Buffer
	if err := renderPreflightFailures(&b, true, 3, []preflightFailure{
		{Name: "a", CompileErr: errors.New("boom")},
		{Name: "b", Diagnostics: []gafferruntime.Diagnostic{{Code: "quirk.x", Message: "bad"}}},
	}); err != nil {
		t.Fatalf("renderPreflightFailures: %v", err)
	}

	var got []cliout.DeployJSON
	if err := json.Unmarshal(b.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, b.String())
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2:\n%s", len(got), b.String())
	}
	for _, e := range got {
		if e.Outcome != "invalid" {
			t.Errorf("%s outcome = %q, want invalid", e.Name, e.Outcome)
		}
	}
	if got[0].Reason != "boom" || got[1].Reason != "quirk.x: bad" {
		t.Errorf("reasons = %q, %q", got[0].Reason, got[1].Reason)
	}
}

func TestRenderPreflightFailuresText(t *testing.T) {
	var b bytes.Buffer
	if err := renderPreflightFailures(&b, false, 3, []preflightFailure{
		{Name: "order-count", CompileErr: errors.New("Unexpected token")},
		{Name: "cart", Diagnostics: []gafferruntime.Diagnostic{{Code: "quirk.x", Message: "faults on the server"}}},
	}); err != nil {
		t.Fatalf("renderPreflightFailures: %v", err)
	}
	out := b.String()
	for _, want := range []string{
		"2 of 3 projections have errors",
		"order-count", "Unexpected token",
		"cart", "quirk.x: faults on the server",
		"--no-validate",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q in:\n%s", want, out)
		}
	}
}

func TestRunPreflight(t *testing.T) {
	const valid = `fromAll().when({ $init: function () { return { n: 0 }; }, $any: function (s, e) { s.n++; return s; } })`
	const broken = `fromAll().when({ $any: function (s, e) { return ` // syntax error

	p := testutil.NewProject(t).
		AddProjection("good", valid).
		AddProjection("bad", broken).
		Save()

	failures := runPreflight(context.Background(), p.Dir, p.Cfg, []string{"good", "bad"})
	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1 (the broken projection): %+v", len(failures), failures)
	}
	if failures[0].Name != "bad" || failures[0].CompileErr == nil {
		t.Errorf("failure = %+v, want bad with a compile error", failures[0])
	}
}

// A projection that compiles but carries an error-severity diagnostic
// (track_emitted_streams on v2) is a preflight failure too - the gate that lets
// deploy/recreate refuse it before any write, distinct from a compile error.
func TestRunPreflightErrorDiagnostic(t *testing.T) {
	const valid = `fromAll().when({ $any: function (s, e) { return s; } })`
	p := testutil.NewProject(t).AddProjection("tes", valid).Save()
	for i := range p.Cfg.Projection {
		if p.Cfg.Projection[i].Name == "tes" {
			p.Cfg.Projection[i].EngineVersion = new(2)
			p.Cfg.Projection[i].TrackEmittedStreams = new(true)
		}
	}
	p.Save()

	failures := runPreflight(context.Background(), p.Dir, p.Cfg, []string{"tes"})
	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1 (the v2 track_emitted_streams projection): %+v", len(failures), failures)
	}
	if failures[0].Name != "tes" || failures[0].CompileErr != nil {
		t.Fatalf("failure = %+v, want tes with no compile error (a diagnostic)", failures[0])
	}
	found := false
	for _, d := range failures[0].Diagnostics {
		if d.Code == "quirk.trackEmittedStreams.unsupportedOnV2" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the V2-incompatibility diagnostic, got %+v", failures[0].Diagnostics)
	}
}

func TestValidatePlan(t *testing.T) {
	// track_emitted_streams on v2 compiles but carries an error-severity
	// diagnostic - it would fault on the server.
	const valid = `fromAll().when({ $any: function (s, e) { return s; } })`
	p := testutil.NewProject(t).AddProjection("tes", valid).Save()
	for i := range p.Cfg.Projection {
		if p.Cfg.Projection[i].Name == "tes" {
			p.Cfg.Projection[i].EngineVersion = new(2)
			p.Cfg.Projection[i].TrackEmittedStreams = new(true)
		}
	}
	p.Save()

	// An applying item that would fault is folded to invalid with its reason.
	applying := []drift.PlanItem{{Name: "tes", Action: drift.ActionCreate}}
	validatePlan(context.Background(), p.Dir, p.Cfg, applying)
	if applying[0].Action != drift.ActionInvalid || applying[0].Reason == "" {
		t.Errorf("applying item = %+v; want invalid with a reason", applying[0])
	}

	// A skip isn't being written, so it's never validated even if it would fault.
	skip := []drift.PlanItem{{Name: "tes", Action: drift.ActionSkip}}
	validatePlan(context.Background(), p.Dir, p.Cfg, skip)
	if skip[0].Action != drift.ActionSkip {
		t.Errorf("skip item = %+v; want left untouched", skip[0])
	}
}

func TestRefuseInvalidPlan(t *testing.T) {
	// A plan with nothing invalid proceeds (nil), so the caller applies.
	clean := []drift.PlanItem{{Name: "a", Action: drift.ActionCreate}}
	if err := refuseInvalidPlan(io.Discard, clean, false); err != nil {
		t.Errorf("clean plan should not refuse, got %v", err)
	}

	blocked := []drift.PlanItem{
		{Name: "a", Action: drift.ActionCreate},
		{Name: "bad", Action: drift.ActionInvalid, Reason: "won't compile"},
	}
	// Text: names the invalid projection and its reason, exits 1.
	var text bytes.Buffer
	if got := exitCodeOf(refuseInvalidPlan(&text, blocked, false)); got != 1 {
		t.Fatalf("invalid plan should exit 1, got %d", got)
	}
	if !strings.Contains(text.String(), "bad") || !strings.Contains(text.String(), "won't compile") {
		t.Errorf("refusal should name the invalid projection and reason:\n%s", text.String())
	}
	// JSON: an array of just the invalid items' verdicts.
	var jbuf bytes.Buffer
	_ = refuseInvalidPlan(&jbuf, blocked, true)
	var arr []cliout.DeployJSON
	if err := json.Unmarshal(jbuf.Bytes(), &arr); err != nil {
		t.Fatalf("json: %v\n%s", err, jbuf.String())
	}
	if len(arr) != 1 || arr[0].Name != "bad" || arr[0].Outcome != "invalid" {
		t.Errorf("json = %+v; want one invalid item", arr)
	}
}

func TestRunPreflightAllValid(t *testing.T) {
	const valid = `fromAll().when({ $any: function (s, e) { return s; } })`
	p := testutil.NewProject(t).
		AddProjection("a", valid).
		AddProjection("b", valid).
		Save()

	if failures := runPreflight(context.Background(), p.Dir, p.Cfg, []string{"a", "b"}); len(failures) != 0 {
		t.Errorf("all-valid project should have no failures, got %+v", failures)
	}
}

// A cancelled context short-circuits the loop before compiling: the broken
// projection that would otherwise fail is never reached.
func TestRunPreflightStopsOnCancel(t *testing.T) {
	const broken = `fromAll().when({ $any: function (s, e) { return `
	p := testutil.NewProject(t).AddProjection("bad", broken).Save()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if failures := runPreflight(ctx, p.Dir, p.Cfg, []string{"bad"}); len(failures) != 0 {
		t.Errorf("a cancelled preflight should compile nothing, got %+v", failures)
	}
}
