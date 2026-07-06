package mcpserver

import (
	"context"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var deployPlanTool = &mcp.Tool{
	Name: "deploy_plan",
	Description: "Compute what `gaffer deploy` would do to a KurrentDB environment without " +
		"writing anything: per projection, whether it would be created, updated, rebuilt, " +
		"skipped, or refused (with the reason), plus logic-change and external-change " +
		"flags. The response echoes the resolved env, the target server, and whether it " +
		"reports itself as production. faultedUpdates names update targets currently faulted on the server (an " +
		"update won't clear the fault); configDrift reports [database_config] divergence. " +
		"Mirrors `gaffer deploy --dry-run --json`, except there is no preflight gate: a " +
		"projection that doesn't compile is planned as refused with the compile error, " +
		"where a real deploy's preflight would abort with outcome invalid. Read-only: it " +
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
		// Nothing configured plans as nothing to do, matching the CLI's `[]`
		// exit-0 - a parseable answer, not an error to reason about.
		return toolResult(map[string]any{"plan": []cliout.DeployJSON{}, "changes": 0}), nil, nil
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

	driftCh := drift.StartConfigDriftCheck(ctx, cfg, root, env.Name, env.Connection)

	// PlanOne bounds each projection's reads itself, so the loop needs no
	// outer deadline - one stalled projection can't eat the others' budget.
	plan := drift.PlanAll(ctx, client, cfg, root, names)
	drift.ResolveResets(plan, in.ResetOnLogicChange)

	changes := 0
	var faulted []string
	for _, it := range plan {
		if it.Err == nil && it.Action.Applies() {
			changes++
		}
		if it.Err == nil && it.Action == drift.ActionUpdate && it.Faulted {
			faulted = append(faulted, it.Name)
		}
	}

	target, prod := operateTarget(ctx, client, env.Name)
	result := map[string]any{
		"env":        env.Name,
		"target":     target,
		"production": prod,
		// The same per-item array a `gaffer deploy --dry-run --json` emits;
		// outcome is the would-be verdict.
		"plan":    cliout.BuildPlanJSON(plan),
		"changes": changes,
	}
	if len(faulted) > 0 {
		result["faultedUpdates"] = faulted
	}
	if items := <-driftCh; len(items) > 0 {
		result["configDrift"] = cliout.BuildConfigDriftJSON(items)
	}
	return toolResult(result), nil, nil
}
