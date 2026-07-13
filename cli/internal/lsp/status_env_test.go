package lsp

import (
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func inConfig(state drift.State) drift.StatusEntry {
	return drift.StatusEntry{Comparison: drift.Comparison{State: state}}
}

// changedExternally is a drifted, in-config projection whose deployed definition
// no longer matches gaffer's last-deployed baseline (a server-side change).
func changedExternally() drift.StatusEntry {
	return drift.StatusEntry{Comparison: drift.Comparison{
		State:          drift.Drifted,
		Ledger:         &remote.Ledger{Tool: remote.ToolName},
		Deployed:       &deploy.Descriptor{Query: "a"},
		DeployBaseline: &deploy.Descriptor{Query: "b"},
	}}
}

// localAhead is a drifted, in-config projection whose deployed definition still
// matches gaffer's baseline - only the local side moved.
func localAhead() drift.StatusEntry {
	return drift.StatusEntry{Comparison: drift.Comparison{
		State:          drift.Drifted,
		Ledger:         &remote.Ledger{Tool: remote.ToolName},
		Deployed:       &deploy.Descriptor{Query: "a"},
		DeployBaseline: &deploy.Descriptor{Query: "a"},
	}}
}

func untrackedEntry(tool string) drift.StatusEntry {
	return drift.StatusEntry{Comparison: drift.Comparison{
		State:  drift.Untracked,
		Ledger: &remote.Ledger{Tool: tool},
	}}
}

func TestStatusRollup(t *testing.T) {
	faulted := drift.StatusEntry{
		Comparison: drift.Comparison{State: drift.InSync},
		Runtime:    &remote.Status{State: remote.StateFaulted},
	}
	for _, tc := range []struct {
		name string
		st   envStatus
		want string
	}{
		{"all in sync leads with the in-sync count, no total", envStatus{Entries: []drift.StatusEntry{inConfig(drift.InSync), inConfig(drift.InSync)}}, "2 in sync"},
		{"singular", envStatus{Entries: []drift.StatusEntry{inConfig(drift.InSync)}}, "1 in sync"},
		{"prod prefix", envStatus{Production: true, Entries: []drift.StatusEntry{inConfig(drift.InSync)}}, "PRODUCTION · 1 in sync"},
		{
			"in sync leads, then attention categories in order",
			envStatus{Entries: []drift.StatusEntry{
				inConfig(drift.InSync), changedExternally(), localAhead(), inConfig(drift.NotDeployed), inConfig(drift.Drifted), inConfig(drift.Invalid),
			}},
			"1 in sync · 1 changed externally · 1 local ahead · 1 not deployed · 1 drifted · 1 invalid",
		},
		{"faulted counted independently of drift", envStatus{Entries: []drift.StatusEntry{faulted}}, "1 in sync · 1 faulted"},
		{
			"drifted and faulted on one projection count in both dimensions",
			envStatus{Entries: []drift.StatusEntry{{
				Comparison: drift.Comparison{State: drift.Drifted},
				Runtime:    &remote.Status{State: remote.StateFaulted},
			}}},
			"1 faulted · 1 drifted",
		},
		{
			"orphan and untracked appended after the in-config counts",
			envStatus{Entries: []drift.StatusEntry{inConfig(drift.InSync), untrackedEntry(remote.ToolName), untrackedEntry("Other Tool")}},
			"1 in sync · 1 orphan · 1 untracked",
		},
		{
			"orphans pluralize",
			envStatus{Entries: []drift.StatusEntry{untrackedEntry(remote.ToolName), untrackedEntry(remote.ToolName)}},
			"2 orphans",
		},
		{"only anomalies, no configured", envStatus{Entries: []drift.StatusEntry{untrackedEntry(remote.ToolName)}}, "1 orphan"},
		{"empty", envStatus{}, "no projections"},
		{"empty prod", envStatus{Production: true}, "PRODUCTION · no projections"},
	} {
		if got := statusRollup(tc.st); got != tc.want {
			t.Errorf("%s: statusRollup() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestEmitStatusEnvLenses(t *testing.T) {
	const uri = "file:///ws/gaffer.toml"
	desc := config.Description{Environments: []config.EnvDescription{
		{Name: "prod", Range: config.SourceRange{StartLine: 5, EndLine: 5}},
		{Name: "staging", Range: config.SourceRange{StartLine: 9, EndLine: 9}},
		{Name: "quoted"}, // no located header (zero range)
	}}

	t.Run("unauthenticated emits a sign-in lens with args", func(t *testing.T) {
		statuses := map[string]envStatus{"prod": {Unauthenticated: true}}
		lenses := emitStatusEnvLenses(desc, uri, statuses, nil)
		if len(lenses) != 1 {
			t.Fatalf("expected 1 lens, got %d", len(lenses))
		}
		l := lenses[0]
		if l.Data == nil || l.Data.Intent != IntentSignIn {
			t.Errorf("intent: %+v", l.Data)
		}
		if l.Command == nil || l.Command.Command != CommandSignIn {
			t.Fatalf("command: %+v", l.Command)
		}
		args, ok := l.Command.Arguments[0].(signInArgs)
		if !ok || args.Env != "prod" || args.ConfigURI != uri {
			t.Errorf("args: %+v", l.Command.Arguments)
		}
		// rangeToLSP converts the 1-indexed SourceRange to the 0-indexed LSP wire.
		if l.Range.Start.Line != 4 {
			t.Errorf("range line: %d want 4 (0-indexed for source line 5)", l.Range.Start.Line)
		}
	})

	t.Run("error emits a muted unavailable lens with the reason in the tooltip", func(t *testing.T) {
		statuses := map[string]envStatus{"prod": {Err: errStub{}}}
		lenses := emitStatusEnvLenses(desc, uri, statuses, nil)
		if len(lenses) != 1 || lenses[0].Data.Intent != IntentStatusEnv {
			t.Fatalf("lenses: %+v", lenses)
		}
		if lenses[0].Command.Title != "status unavailable" || lenses[0].Command.Command != "" {
			t.Errorf("command: %+v", lenses[0].Command)
		}
		if lenses[0].Command.Tooltip != "boom" {
			t.Errorf("tooltip: got %q want the error reason", lenses[0].Command.Tooltip)
		}
	})

	t.Run("data emits the non-clickable roll-up as plain label text", func(t *testing.T) {
		st := envStatus{Entries: []drift.StatusEntry{inConfig(drift.InSync)}, Target: "prod-cluster"}
		statuses := map[string]envStatus{"prod": st}
		lenses := emitStatusEnvLenses(desc, uri, statuses, nil)
		if len(lenses) != 1 || lenses[0].Data.Intent != IntentStatusEnv {
			t.Fatalf("lenses: %+v", lenses)
		}
		// Empty command -> the client renders a non-clickable span; no tooltip
		// on the healthy roll-up (it's just label text).
		if lenses[0].Command.Title != statusRollup(st) || lenses[0].Command.Command != "" {
			t.Errorf("command: %+v", lenses[0].Command)
		}
		if lenses[0].Command.Tooltip != "" {
			t.Errorf("tooltip: got %q want none on the roll-up", lenses[0].Command.Tooltip)
		}
	})

	t.Run("pending env (no cache entry) and zero-range env are skipped", func(t *testing.T) {
		// Only staging has a landed status; prod is pending, quoted has no range.
		statuses := map[string]envStatus{"staging": {Entries: []drift.StatusEntry{inConfig(drift.InSync)}}}
		lenses := emitStatusEnvLenses(desc, uri, statuses, nil)
		if len(lenses) != 1 {
			t.Fatalf("expected only staging, got %d lenses", len(lenses))
		}
		if lenses[0].Range.Start.Line != 8 {
			t.Errorf("expected staging at 0-indexed line 8 (source line 9), got %d", lenses[0].Range.Start.Line)
		}
	})

	t.Run("empty status map emits nothing", func(t *testing.T) {
		if lenses := emitStatusEnvLenses(desc, uri, nil, nil); len(lenses) != 0 {
			t.Fatalf("expected no lenses, got %+v", lenses)
		}
	})

	t.Run("in-flight env emits a loading placeholder", func(t *testing.T) {
		// prod has no cached status yet but is being fetched; staging is neither.
		lenses := emitStatusEnvLenses(desc, uri, nil, map[string]bool{"prod": true})
		if len(lenses) != 1 || lenses[0].Data.Intent != IntentStatusLoading {
			t.Fatalf("expected one loading lens, got %+v", lenses)
		}
		if lenses[0].Command.Title != "loading status..." || lenses[0].Command.Command != "" {
			t.Errorf("command: %+v", lenses[0].Command)
		}
	})
}

type errStub struct{}

func (errStub) Error() string { return "boom" }
