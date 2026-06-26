package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

const infoValidSource = `fromAll().when({ $any: function (s, e) { return s; } })`

// An invalid projection shows the degraded body (name + reason), the same
// rendering as diff, instead of a fang ERROR badge - but still exits non-zero
// (a single-projection command that couldn't do its job). The error is silent so
// fang doesn't re-print what's already shown.
func TestRunInfoDegradesOnConfigError(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("good", infoValidSource).
		AddProjection("bad", infoValidSource).
		Save()
	// Make "bad" config-invalid: an out-of-range engine_version. The source itself
	// compiles - this is a config error, not a compile error.
	for i := range p.Cfg.Projection {
		if p.Cfg.Projection[i].Name == "bad" {
			p.Cfg.Projection[i].EngineVersion = new(5)
		}
	}
	p.Save()
	t.Chdir(p.Dir)

	var err error
	out := testutil.CaptureStdout(t, func() { err = runInfo("bad", false) })
	if err == nil {
		t.Fatal("info on an invalid projection should exit non-zero")
	}
	var s *silentError
	if !errors.As(err, &s) {
		t.Errorf("expected a silent error (degraded body already shown, no fang re-print), got %v", err)
	}
	// The non-zero return is what classifies the telemetry outcome: an invalid
	// projection is a user error, not a success.
	if got := outcomeFor(err); got != telemetry.OutcomeUserError {
		t.Errorf("invalid info should record user_error telemetry, got %s", got)
	}
	for _, want := range []string{"bad", "invalid local definition", "must be 1 or 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("degraded info missing %q in:\n%s", want, out)
		}
	}
}

// track_emitted_streams on engine_version 2 is no longer a config error - it's a
// V2-incompatibility diagnostic. So info compiles, shows the full analysis, and
// reports the diagnostic, rather than degrading. It still exits 0: info displays
// diagnostics, it doesn't fail on them (deploy/recreate preflight do that).
func TestRunInfoShowsAnalysisForTrackEmittedStreamsOnV2(t *testing.T) {
	p := testutil.NewProject(t).AddProjection("tes", infoValidSource).Save()
	for i := range p.Cfg.Projection {
		if p.Cfg.Projection[i].Name == "tes" {
			p.Cfg.Projection[i].EngineVersion = new(2)
			p.Cfg.Projection[i].TrackEmittedStreams = new(true)
		}
	}
	p.Save()
	t.Chdir(p.Dir)

	var err error
	out := testutil.CaptureStdout(t, func() { err = runInfo("tes", false) })
	if err != nil {
		t.Fatalf("info on a track_emitted_streams v2 projection should compile, got %v", err)
	}
	for _, want := range []string{"tes", "v2", "quirk.trackEmittedStreams.unsupportedOnV2"} {
		if !strings.Contains(out, want) {
			t.Errorf("info missing %q in:\n%s", want, out)
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
