package mcpserver

import (
	"context"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var deployPlanTool = &mcp.Tool{
	Name: "deploy_plan",
	Description: "Compute what `gaffer deploy` would do to a KurrentDB environment without " +
		"writing anything. verdict is the whole-plan headline - in-sync (nothing to do), " +
		"deployable (changes pending, none blocked), or blocked (something can't be " +
		"deployed). Per projection, outcome is what an apply would produce: created, " +
		"updated, rebuilt, skipped, refused (a recreate is needed - recreate is then true, " +
		"see deploy_recreate), or invalid (the local definition won't run - reason carries " +
		"the compile or config error). Items also flag logicChange, externalChange (with " +
		"externalChangeTool when another tool made the change), faulted (an update over a " +
		"faulted projection), and emittingReset (a rebuild that re-emits). The response " +
		"echoes the resolved env, the target server, and whether it is a production target " +
		"(declared by the server itself, or by production = true on the env). configDrift " +
		"reports [database_config] divergence, or configDriftError the reason the node's " +
		"config couldn't be read (never both). Mirrors `gaffer deploy --dry-run --json`, " +
		"except it runs no fault-severity diagnostics check, so a projection that compiles " +
		"but would fault plans as its would-be write rather than invalid. Read-only: it " +
		"never applies the plan - deploying is the CLI's job.",
	Annotations: readOnlyHints(),
}

type deployPlanInput struct {
	Name               string `json:"name,omitempty" jsonschema:"Projection name; omit to plan every projection in gaffer.toml."`
	Env                string `json:"env,omitempty" jsonschema:"Environment from gaffer.toml ([env.<name>]); omit for the default env."`
	ResetOnLogicChange bool   `json:"resetOnLogicChange,omitempty" jsonschema:"Plan a rebuild from zero for each logic-change update, as deploy --reset-on-logic-change would."`
}

func (s *Server) handleDeployPlan(ctx context.Context, _ *mcp.CallToolRequest, in deployPlanInput) (*mcp.CallToolResult, any, error) {
	cfg, root, r := s.requireProject()
	if r != nil {
		return r, nil, nil
	}

	var names []string
	if in.Name != "" {
		if cfg.FindProjection(in.Name) == nil {
			return toolError("projection %q is not in gaffer.toml; call list_projections to discover names", in.Name), nil, nil
		}
		names = []string{in.Name}
	} else {
		for i := range cfg.Projection {
			names = append(names, cfg.Projection[i].Name)
		}
	}
	if len(names) == 0 {
		// Nothing configured plans as nothing to do - the same in-sync envelope a
		// real plan produces, so a consumer parses one shape.
		return toolResult(cliout.BuildPlanReport(nil, drift.ConfigDriftResult{})), nil, nil
	}

	// No s.mu: planning compiles the local definitions into throwaway engine
	// sessions that never touch s.session (see deploy_status), and holding
	// the server mutex across per-projection remote reads - up to the RPC
	// timeout each - would block every session tool behind network latency.
	client, env, cleanup, err := s.connectRemote(cfg, root, in.Env)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	defer cleanup()

	driftCh := drift.StartConfigDriftCheck(ctx, cfg, root, env)

	// PlanOne bounds each projection's reads itself, so the loop needs no
	// outer deadline - one stalled projection can't eat the others' budget.
	plan := drift.PlanAll(ctx, client, cfg, root, names)
	drift.ResolveResets(plan, in.ResetOnLogicChange)

	// The shared envelope the CLI's --dry-run emits: the verdict, change count,
	// per-item plan (recreate/faulted/emittingReset flags and all), and the
	// [database_config] divergence. OperateTarget overlaps the drift check drained
	// inside BuildPlanReport. Unlike the CLI, deploy_plan runs no fault-diagnostics
	// validation, so a projection that compiles but would fault plans as its
	// would-be write, not invalid.
	target, prod := client.OperateTarget(ctx, env, deploy.RPCTimeout)
	report := cliout.BuildPlanReport(plan, <-driftCh)
	report.Env = env.Name
	report.Target, report.Production = target, &prod
	return toolResult(report), nil, nil
}
