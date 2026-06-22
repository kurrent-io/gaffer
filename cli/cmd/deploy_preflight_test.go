package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
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
	renderPreflightFailures(&b, true, 3, []preflightFailure{
		{Name: "a", CompileErr: errors.New("boom")},
		{Name: "b", Diagnostics: []gafferruntime.Diagnostic{{Code: "quirk.x", Message: "bad"}}},
	})

	var got []deployJSON
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
	renderPreflightFailures(&b, false, 3, []preflightFailure{
		{Name: "order-count", CompileErr: errors.New("Unexpected token")},
		{Name: "cart", Diagnostics: []gafferruntime.Diagnostic{{Code: "quirk.x", Message: "faults on the server"}}},
	})
	out := b.String()
	for _, want := range []string{
		"2 of 3 projections have errors",
		"order-count", "Unexpected token",
		"cart", "quirk.x: faults on the server",
		"--force",
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

	failures := runPreflight(p.Dir, p.Cfg, []string{"good", "bad"})
	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1 (the broken projection): %+v", len(failures), failures)
	}
	if failures[0].Name != "bad" || failures[0].CompileErr == nil {
		t.Errorf("failure = %+v, want bad with a compile error", failures[0])
	}
}

func TestRunPreflightAllValid(t *testing.T) {
	const valid = `fromAll().when({ $any: function (s, e) { return s; } })`
	p := testutil.NewProject(t).
		AddProjection("a", valid).
		AddProjection("b", valid).
		Save()

	if failures := runPreflight(p.Dir, p.Cfg, []string{"a", "b"}); len(failures) != 0 {
		t.Errorf("all-valid project should have no failures, got %+v", failures)
	}
}
