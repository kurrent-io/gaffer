package cmd

import (
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

const infoValidSource = `fromAll().when({ $any: function (s, e) { return s; } })`

// An invalid projection degrades to its name + the reason (exit 0), the same as
// diff/status, rather than a hard fang error.
func TestRunInfoDegradesOnConfigError(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("good", infoValidSource).
		AddProjection("bad", infoValidSource).
		Save()
	// Make "bad" config-invalid: track_emitted_streams on engine_version 2 (the
	// default). The source itself compiles - this is a config error, not a compile
	// error.
	for i := range p.Cfg.Projection {
		if p.Cfg.Projection[i].Name == "bad" {
			p.Cfg.Projection[i].TrackEmittedStreams = testutil.Ptr(true)
		}
	}
	p.Save()
	t.Chdir(p.Dir)

	out := testutil.CaptureStdout(t, func() {
		if err := runInfo("bad", false); err != nil {
			t.Fatalf("info on a config-bad projection should degrade, not error: %v", err)
		}
	})
	for _, want := range []string{"bad", "invalid local definition", "track_emitted_streams is only valid with engine_version 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("degraded info missing %q in:\n%s", want, out)
		}
	}
}

func TestRunInfoValid(t *testing.T) {
	p := testutil.NewProject(t).AddProjection("good", infoValidSource).Save()
	t.Chdir(p.Dir)

	out := testutil.CaptureStdout(t, func() {
		if err := runInfo("good", false); err != nil {
			t.Fatalf("info on a valid projection: %v", err)
		}
	})
	if !strings.Contains(out, "good") {
		t.Errorf("expected the projection name in info output, got:\n%s", out)
	}
}
