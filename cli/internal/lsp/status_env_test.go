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

	t.Run("data emits a leading clickable Deploy then the roll-up label", func(t *testing.T) {
		st := envStatus{Entries: []drift.StatusEntry{inConfig(drift.InSync)}, Target: "prod-cluster"}
		statuses := map[string]envStatus{"prod": st}
		lenses := emitStatusEnvLenses(desc, uri, statuses, nil)
		if len(lenses) != 2 {
			t.Fatalf("expected deploy + roll-up, got %d: %+v", len(lenses), lenses)
		}
		// Deploy leads the line, roll-up follows.
		preview, rollup := lenses[0], lenses[1]
		// Empty command -> the client renders a non-clickable span; no tooltip
		// on the healthy roll-up (it's just label text).
		if rollup.Data.Intent != IntentStatusEnv || rollup.Command.Title != statusRollup(st) || rollup.Command.Command != "" {
			t.Errorf("roll-up command: %+v", rollup.Command)
		}
		if rollup.Command.Tooltip != "" {
			t.Errorf("tooltip: got %q want none on the roll-up", rollup.Command.Tooltip)
		}
		if preview.Data.Intent != IntentDeployPreview || preview.Command.Command != CommandDeployPreview {
			t.Fatalf("preview command: %+v", preview.Command)
		}
		args, ok := preview.Command.Arguments[0].(deployEnvArgs)
		if !ok || args.Env != "prod" || args.ConfigURI != uri {
			t.Errorf("preview args: %+v", preview.Command.Arguments)
		}
		// Both anchored on the same env-block line.
		if preview.Range.Start.Line != rollup.Range.Start.Line {
			t.Errorf("preview range %d != roll-up range %d", preview.Range.Start.Line, rollup.Range.Start.Line)
		}
	})

	t.Run("pending env (no cache entry) and zero-range env are skipped", func(t *testing.T) {
		// Only staging has a landed status; prod is pending, quoted has no range.
		statuses := map[string]envStatus{"staging": {Entries: []drift.StatusEntry{inConfig(drift.InSync)}}}
		lenses := emitStatusEnvLenses(desc, uri, statuses, nil)
		// staging lands as its roll-up + preview, both on its line.
		if len(lenses) != 2 {
			t.Fatalf("expected only staging (roll-up + preview), got %d lenses", len(lenses))
		}
		for _, l := range lenses {
			if l.Range.Start.Line != 8 {
				t.Errorf("expected staging at 0-indexed line 8 (source line 9), got %d", l.Range.Start.Line)
			}
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

func TestEmitStatusBadgeLenses(t *testing.T) {
	desc := config.Description{
		Projections: []config.ProjectionDescription{
			{Name: "checkout", Range: config.SourceRange{StartLine: 5, EndLine: 5}},
			{Name: "orders", Range: config.SourceRange{StartLine: 9, EndLine: 9}},
			{Name: "bad", Range: config.SourceRange{StartLine: 12, EndLine: 12}, Diagnostic: &config.Diagnostic{Message: "x"}},
		},
		Environments: []config.EnvDescription{{Name: "prod"}, {Name: "staging"}},
	}

	healthsByLine := func(lenses []CodeLens) map[int][]string {
		out := map[int][]string{}
		for _, l := range lenses {
			if l.Command != nil {
				t.Errorf("a status lens carries no command: %+v", l.Command)
			}
			if l.Data == nil || l.Data.Intent != IntentStatusBadges {
				t.Fatalf("intent: %+v", l.Data)
			}
			out[l.Range.Start.Line] = l.Data.Healths
		}
		return out
	}

	t.Run("per-env healths in config order, anchored on the header, diagnostic skipped", func(t *testing.T) {
		statuses := map[string]envStatus{
			"prod":    {Entries: []drift.StatusEntry{named("checkout", drift.InSync, remote.StateRunning), named("orders", drift.InSync, remote.StateRunning)}},
			"staging": {Entries: []drift.StatusEntry{named("checkout", drift.Drifted, remote.StateRunning), named("orders", drift.InSync, remote.StateRunning)}},
		}
		lenses := emitStatusBadgeLenses(desc, statuses, nil)
		if len(lenses) != 2 {
			t.Fatalf("expected 2 status lenses (bad is diagnostic), got %d: %+v", len(lenses), lenses)
		}
		byLine := healthsByLine(lenses)
		// checkout (source line 5 -> LSP 4): prod green, staging drifted -> orange.
		if got := byLine[4]; len(got) != 2 || got[0] != "green" || got[1] != "orange" {
			t.Errorf("checkout healths: got %v want [green orange]", got)
		}
		// orders (source line 9 -> LSP 8): green in both envs.
		if got := byLine[8]; len(got) != 2 || got[0] != "green" || got[1] != "green" {
			t.Errorf("orders healths: got %v want [green green]", got)
		}
	})

	t.Run("unknown envs carry their reason, keeping the row aligned", func(t *testing.T) {
		statuses := map[string]envStatus{"prod": {Unauthenticated: true}, "staging": {Err: errStub{}}}
		lenses := emitStatusBadgeLenses(desc, statuses, nil)
		// prod needs sign-in (locked), staging failed (error); the row still has
		// one entry per configured env.
		byLine := healthsByLine(lenses)
		if got := byLine[4]; len(got) != 2 || got[0] != "locked" || got[1] != "error" {
			t.Errorf("unknown-reason healths: got %v want [locked error]", got)
		}
	})

	t.Run("a projection with no located header emits no marker", func(t *testing.T) {
		unlocated := config.Description{
			Projections:  []config.ProjectionDescription{{Name: "checkout"}}, // zero range
			Environments: []config.EnvDescription{{Name: "prod"}},
		}
		statuses := map[string]envStatus{"prod": {Entries: []drift.StatusEntry{named("checkout", drift.InSync, remote.StateRunning)}}}
		if lenses := emitStatusBadgeLenses(unlocated, statuses, nil); len(lenses) != 0 {
			t.Fatalf("a projection with no header range should get no marker, got %+v", lenses)
		}
	})

	t.Run("a projection with no configured envs emits no marker", func(t *testing.T) {
		noEnvs := config.Description{
			Projections: []config.ProjectionDescription{{Name: "checkout", Range: config.SourceRange{StartLine: 5, EndLine: 5}}},
		}
		if lenses := emitStatusBadgeLenses(noEnvs, nil, nil); len(lenses) != 0 {
			t.Fatalf("no envs -> no marker, got %+v", lenses)
		}
	})

	t.Run("faulted reads red for that env", func(t *testing.T) {
		statuses := map[string]envStatus{
			"prod":    {Entries: []drift.StatusEntry{named("checkout", drift.InSync, remote.StateFaulted)}},
			"staging": {Entries: []drift.StatusEntry{named("checkout", drift.InSync, remote.StateRunning)}},
		}
		byLine := healthsByLine(emitStatusBadgeLenses(desc, statuses, nil))
		if got := byLine[4]; len(got) != 2 || got[0] != "red" || got[1] != "green" {
			t.Errorf("faulted-in-prod healths: got %v want [red green]", got)
		}
	})
}

func TestEmitActionsLenses(t *testing.T) {
	const uri = "file:///w/gaffer.toml"
	envs := []config.EnvDescription{{Name: "prod"}, {Name: "local"}}
	desc := config.Description{
		Projections: []config.ProjectionDescription{
			{Name: "checkout", Range: config.SourceRange{StartLine: 5, EndLine: 5}},
			{Name: "orders", Range: config.SourceRange{StartLine: 9, EndLine: 9}},
			{Name: "bad", Range: config.SourceRange{StartLine: 12, EndLine: 12}, Diagnostic: &config.Diagnostic{Message: "x"}},
			{Name: "unlocated"}, // zero range
		},
		Environments: envs,
	}

	t.Run("one lens per located, non-diagnostic projection", func(t *testing.T) {
		lenses := emitActionsLenses(desc, uri, nil)
		if len(lenses) != 2 {
			t.Fatalf("expected 2 actions lenses (bad diagnostic + unlocated skipped), got %d: %+v", len(lenses), lenses)
		}
		byLine := map[int]CodeLens{}
		for _, l := range lenses {
			byLine[l.Range.Start.Line] = l
		}
		// checkout on source line 5 -> LSP line 4; orders on 9 -> 8.
		for _, want := range []struct {
			line int
			name string
		}{{4, "checkout"}, {8, "orders"}} {
			l, ok := byLine[want.line]
			if !ok {
				t.Fatalf("no actions lens on line %d", want.line)
			}
			if l.Data == nil || l.Data.Intent != IntentActions {
				t.Errorf("intent on line %d: %+v", want.line, l.Data)
			}
			if l.Command == nil || l.Command.Title != "Manage..." || l.Command.Command != CommandProjectionActions {
				t.Fatalf("command on line %d: %+v", want.line, l.Command)
			}
			args, ok := l.Command.Arguments[0].(projectionActionsArgs)
			if !ok {
				t.Fatalf("args type on line %d: %T", want.line, l.Command.Arguments[0])
			}
			if args.Name != want.name || args.ConfigURI != uri {
				t.Errorf("args on line %d: got %+v", want.line, args)
			}
			if len(args.Envs) != 2 || args.Envs[0].Name != "prod" || args.Envs[1].Name != "local" {
				t.Errorf("args envs on line %d: got %+v", want.line, args.Envs)
			}
		}
	})

	t.Run("no configured envs -> no lenses", func(t *testing.T) {
		noEnvs := config.Description{
			Projections: []config.ProjectionDescription{{Name: "checkout", Range: config.SourceRange{StartLine: 5, EndLine: 5}}},
		}
		if lenses := emitActionsLenses(noEnvs, uri, nil); len(lenses) != 0 {
			t.Fatalf("no envs -> no actions lens, got %+v", lenses)
		}
	})

	t.Run("carries production and per-projection runtime state", func(t *testing.T) {
		statuses := map[string]envStatus{
			"prod": {Production: true, Entries: []drift.StatusEntry{
				{Comparison: drift.Comparison{Name: "checkout", Deployed: &deploy.Descriptor{Emit: true}}, Runtime: &remote.Status{State: remote.StateRunning}},
			}},
			"local": {Entries: []drift.StatusEntry{
				{Comparison: drift.Comparison{Name: "checkout"}, Runtime: &remote.Status{State: remote.StateStopped}},
			}},
		}
		lenses := emitActionsLenses(desc, uri, statuses)
		var checkout projectionActionsArgs
		for _, l := range lenses {
			if a, ok := l.Command.Arguments[0].(projectionActionsArgs); ok && a.Name == "checkout" {
				checkout = a
			}
		}
		byEnv := map[string]actionsEnv{}
		for _, e := range checkout.Envs {
			byEnv[e.Name] = e
		}
		if byEnv["prod"].Production == nil || !*byEnv["prod"].Production ||
			byEnv["prod"].State != "running" || !byEnv["prod"].Emits {
			t.Errorf("prod env cell: got %+v, want production + running + emits", byEnv["prod"])
		}
		if byEnv["local"].Production == nil || *byEnv["local"].Production ||
			byEnv["local"].State != "stopped" || byEnv["local"].Emits {
			t.Errorf("local env cell: got %+v, want known non-prod + stopped + no emit", byEnv["local"])
		}
		// orders has no status entry -> empty state, no production.
		var orders projectionActionsArgs
		for _, l := range lenses {
			if a, ok := l.Command.Arguments[0].(projectionActionsArgs); ok && a.Name == "orders" {
				orders = a
			}
		}
		for _, e := range orders.Envs {
			if e.State != "" {
				t.Errorf("orders env %q should have empty state, got %q", e.Name, e.State)
			}
		}
	})

	t.Run("StateUnknown runtime is emitted as empty state", func(t *testing.T) {
		// The client reads "" as indeterminate and offers both pause and resume;
		// forwarding the raw "unknown" would hide pause, so it must normalise to "".
		statuses := map[string]envStatus{
			"prod": {Production: true, Entries: []drift.StatusEntry{
				{Comparison: drift.Comparison{Name: "checkout"}, Runtime: &remote.Status{State: remote.StateUnknown}},
			}},
		}
		lenses := emitActionsLenses(desc, uri, statuses)
		var checkout projectionActionsArgs
		for _, l := range lenses {
			if a, ok := l.Command.Arguments[0].(projectionActionsArgs); ok && a.Name == "checkout" {
				checkout = a
			}
		}
		for _, e := range checkout.Envs {
			if e.Name == "prod" && e.State != "" {
				t.Errorf("StateUnknown should emit empty state, got %q", e.State)
			}
		}
	})

	t.Run("production is unknown (nil) until the fetch resolves", func(t *testing.T) {
		// An errored fetch, a sign-in-needed fetch, and an env with no cached
		// status must all read as unknown production - never as non-production -
		// so the editor fails the confirm-tier decision safe.
		statuses := map[string]envStatus{
			"prod":  {Err: errStub{}, Production: true},
			"local": {Unauthenticated: true},
		}
		lenses := emitActionsLenses(desc, uri, statuses)
		var checkout projectionActionsArgs
		for _, l := range lenses {
			if a, ok := l.Command.Arguments[0].(projectionActionsArgs); ok && a.Name == "checkout" {
				checkout = a
			}
		}
		byEnv := map[string]actionsEnv{}
		for _, e := range checkout.Envs {
			if e.Production != nil {
				t.Errorf("env %q: production should be nil (unknown) on an unresolved fetch, got %v", e.Name, *e.Production)
			}
			byEnv[e.Name] = e
		}
		// The unresolved fetches surface their reason as status, for the menu.
		if byEnv["prod"].Status != "unavailable" {
			t.Errorf("errored env status: got %q want unavailable", byEnv["prod"].Status)
		}
		if byEnv["local"].Status != "auth" {
			t.Errorf("sign-in-needed env status: got %q want auth", byEnv["local"].Status)
		}
	})
}

type errStub struct{}

func (errStub) Error() string { return "boom" }
