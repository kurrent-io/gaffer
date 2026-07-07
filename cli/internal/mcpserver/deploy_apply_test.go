package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

func TestDeployApplyProjectless(t *testing.T) {
	s := newProjectlessServer(t)
	if msg := callToolExpectError(t, s.handleDeployApply, deployApplyInput{}); !strings.Contains(msg, "no gaffer project found") {
		t.Errorf("got %q, want the projectless gate", msg)
	}
}

func TestDeployApplyUnknownName(t *testing.T) {
	s := setupTestProject(t)
	if msg := callToolExpectError(t, s.handleDeployApply, deployApplyInput{Name: "nope"}); !strings.Contains(msg, `projection "nope" is not in gaffer.toml`) {
		t.Errorf("got %q, want the unknown-projection message", msg)
	}
}

// The preflight runs before any connection (the fixture has no envs, so
// reaching resolution would fail differently), refuses the whole run on any
// uncompilable projection, and reports the failures in the standard
// envelope - the shape `gaffer deploy --json` renders them - listing every
// invalid projection so a broken set is fixed in one pass, not
// fix-one-rerun-repeat.
func TestDeployApplyPreflightRefusesBeforeConnecting(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("good", "fromAll().when({ $init() { return {}; } })").
		AddProjection("bad", "fromAll(.when({").
		AddProjection("worse", "when({{{").
		Save()
	s := New(p.Dir, p.Cfg, "test")

	// Deploying everything: the good projection doesn't proceed while
	// siblings fail preflight, and both failures report.
	res := callTool(t, s, deployApplyTool, s.handleDeployApply, deployApplyInput{})
	if res["changes"] != float64(0) || res["failed"] != float64(2) {
		t.Fatalf("envelope = %v, want 0 changes 2 failed", res)
	}
	results, ok := res["results"].([]any)
	if !ok || len(results) != 2 {
		t.Fatalf("results = %v, want both invalid projections listed", res["results"])
	}
	for _, r := range results {
		entry := testutil.MustType[map[string]any](t, r)
		if entry["outcome"] != "invalid" || entry["reason"] == "" {
			t.Errorf("entry = %v, want outcome invalid with a reason", entry)
		}
	}
	// No connection happened, so no target/production to echo.
	if _, ok := res["target"]; ok {
		t.Errorf("target should be omitted before any connection, got %v", res["target"])
	}

	// Scoped to one bad projection, only it reports.
	res = callTool(t, s, deployApplyTool, s.handleDeployApply, deployApplyInput{Name: "bad"})
	if res["failed"] != float64(1) {
		t.Fatalf("scoped envelope = %v, want the single failure", res)
	}
}

// A compiling projection passes preflight and reaches env resolution, which
// rejects an unknown env before any connection.
func TestDeployApplyUnknownEnv(t *testing.T) {
	s := setupTestProjectWithEnv(t)
	if msg := callToolExpectError(t, s.handleDeployApply, deployApplyInput{Name: "order-count", Env: "nope"}); !strings.Contains(msg, `unknown environment "nope"`) {
		t.Errorf("got %q, want the unknown-env message", msg)
	}
}

func TestDeployApplyGate(t *testing.T) {
	ext := drift.Comparison{
		State: drift.Drifted, Ledger: &remote.Ledger{Tool: remote.ToolName},
		Deployed:       &deploy.Descriptor{Query: "a", EngineVersion: 2},
		DeployBaseline: &deploy.Descriptor{Query: "b", EngineVersion: 2},
	}
	plan := []drift.PlanItem{
		{Name: "created", Action: drift.ActionCreate, Cmp: drift.Comparison{Local: &deploy.Descriptor{}}},
		{Name: "rebuilt", Action: drift.ActionReset, Cmp: drift.Comparison{Local: &deploy.Descriptor{Emit: true}}},
		{Name: "overwritten", Action: drift.ActionUpdate, Cmp: ext},
		{Name: "sick", Action: drift.ActionUpdate, Faulted: true, Cmp: drift.Comparison{Local: &deploy.Descriptor{}}},
		{Name: "insync", Action: drift.ActionSkip},
		{Name: "refused", Action: drift.ActionRefuse, Reason: "engine version"},
		{Name: "errored", Err: context.DeadlineExceeded},
	}
	g := deployApplyGate(deployApplyInput{ResetOnLogicChange: true}, "production", "orders-prod", true, plan,
		drift.ConfigDriftResult{Items: []drift.ConfigDrift{{Knob: "max_state_size", Server: 1, Local: 2}}})

	// The rebuild puts the plan in the no-undo tier, and the typed confirm
	// asks for the environment name - a deploy-all has no projection name.
	if !g.NoUndo || g.TypedValue != "production" || g.TypedNoun != "environment name" {
		t.Fatalf("gate tiers = %+v, want no-undo typing the environment name", g)
	}
	if g.Action != "deploy 4 changes" || !g.Production || g.Target != "orders-prod" || g.Env != "production" {
		t.Fatalf("gate = %+v", g)
	}
	for _, want := range []string{
		"Changes created, rebuilt, overwritten, sick.",
		"1 rebuild(s) from zero: state destroyed.",
		"rebuilt re-emit(s) on rebuild and may duplicate.",
		"Overwrites out-of-band changes on overwritten.",
		"Updating won't clear the fault on sick.",
		"The target's [database_config] diverges from gaffer.toml.",
	} {
		if !strings.Contains(g.Consequence, want) {
			t.Errorf("consequence = %q, missing %q", g.Consequence, want)
		}
	}
	if g.CLI != "gaffer deploy --reset-on-logic-change" {
		t.Errorf("cli = %q", g.CLI)
	}

	// A single-name update-only deploy: prod-tier only, name in the CLI.
	single := deployApplyGate(deployApplyInput{Name: "orders"}, "staging", "stage-1", false,
		[]drift.PlanItem{{Name: "orders", Action: drift.ActionUpdate, Cmp: drift.Comparison{Local: &deploy.Descriptor{}}}}, drift.ConfigDriftResult{})
	if single.NoUndo || single.Production || single.Action != "deploy 1 change" || single.CLI != "gaffer deploy 'orders'" {
		t.Fatalf("single gate = %+v", single)
	}

	// A failed drift read tells the confirming human the check didn't run -
	// less information than usual is decision-relevant (UI-1820).
	unchecked := deployApplyGate(deployApplyInput{Name: "orders"}, "staging", "stage-1", false,
		[]drift.PlanItem{{Name: "orders", Action: drift.ActionUpdate, Cmp: drift.Comparison{Local: &deploy.Descriptor{}}}},
		drift.ConfigDriftResult{Err: context.DeadlineExceeded})
	if !strings.Contains(unchecked.Consequence, "The target's [database_config] could not be checked.") {
		t.Errorf("consequence = %q, missing the unchecked-config caution", unchecked.Consequence)
	}
}

func TestCapNames(t *testing.T) {
	if got := capNames([]string{"a", "b"}); got != "a, b" {
		t.Errorf("got %q", got)
	}
	if got := capNames([]string{"a", "b", "c", "d", "e", "f", "g"}); got != "a, b, c, d, e, and 2 more" {
		t.Errorf("got %q", got)
	}
}

// An empty project deploys as nothing to do, not an error.
func TestDeployApplyEmptyProject(t *testing.T) {
	p := testutil.NewProject(t).Save()
	s := New(p.Dir, p.Cfg, "test")
	res := callTool(t, s, deployApplyTool, s.handleDeployApply, deployApplyInput{})
	if res["changes"] != float64(0) || res["failed"] != float64(0) {
		t.Fatalf("empty project = %v, want 0 changes 0 failed", res)
	}
}
