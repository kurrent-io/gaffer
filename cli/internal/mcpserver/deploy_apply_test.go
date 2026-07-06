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
// reaching resolution would fail differently) and one uncompilable
// projection refuses the whole run.
func TestDeployApplyPreflightRefusesBeforeConnecting(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("good", "fromAll().when({ $init() { return {}; } })").
		AddProjection("bad", "fromAll(.when({").
		Save()
	s := New(p.Dir, p.Cfg, "test")

	msg := callToolExpectError(t, s.handleDeployApply, deployApplyInput{Name: "bad"})
	if !strings.Contains(msg, "preflight failed, nothing was deployed") || !strings.Contains(msg, "bad:") {
		t.Errorf("got %q, want the preflight refusal naming the projection", msg)
	}

	// Deploying everything hits the same gate: the good projection doesn't
	// proceed while a sibling fails preflight.
	msg = callToolExpectError(t, s.handleDeployApply, deployApplyInput{})
	if !strings.Contains(msg, "preflight failed, nothing was deployed") {
		t.Errorf("got %q, want the all-or-nothing preflight refusal", msg)
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
	g := deployApplyGate(deployApplyInput{ResetOnLogicChange: true}, "production", "orders-prod", true, plan, true)

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
		[]drift.PlanItem{{Name: "orders", Action: drift.ActionUpdate, Cmp: drift.Comparison{Local: &deploy.Descriptor{}}}}, false)
	if single.NoUndo || single.Production || single.Action != "deploy 1 change" || single.CLI != "gaffer deploy 'orders'" {
		t.Fatalf("single gate = %+v", single)
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
