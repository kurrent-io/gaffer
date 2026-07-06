package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/apply"
	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/stamp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type deployApplyInput struct {
	Name               string `json:"name,omitempty" jsonschema:"Projection name; omit to deploy every projection in gaffer.toml."`
	Env                string `json:"env,omitempty" jsonschema:"Environment from gaffer.toml ([env.<name>]); omit for the default env."`
	ResetOnLogicChange bool   `json:"resetOnLogicChange,omitempty" jsonschema:"Rebuild from zero on a logic change instead of continuing from checkpoint. Destroys the accumulated state; an emitting projection re-emits."`
}

var deployApplyTool = &mcp.Tool{
	Name: "deploy_apply",
	Description: "Deploy projections from gaffer.toml to a KurrentDB environment: create the " +
		"ones not yet on the server, update the ones whose definition changed, and skip " +
		"the ones already in sync. Mirrors `gaffer deploy` (every projection must compile " +
		"and pass the diagnostics preflight first; there is no validation bypass) and " +
		"emits the same per-item results as `gaffer deploy --json`, in an envelope echoing " +
		"the resolved env, target, and production. Use deploy_plan first to preview. On a " +
		"production target the deploy asks the human to confirm via the client " +
		"(elicitation); with resetOnLogicChange rebuilds in the plan it always asks, and a " +
		"production confirm requires typing the environment name (rebuilds destroy state). " +
		"Every write is ledger-stamped operation: deploy. A changed engine version or " +
		"emitted-stream tracking is refused per projection (use deploy_recreate).",
	Annotations: destructiveHints(),
}

func (s *Server) handleDeployApply(ctx context.Context, req *mcp.CallToolRequest, in deployApplyInput) (*mcp.CallToolResult, any, error) {
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
		// Nothing configured deploys as nothing to do, matching the CLI's `[]`
		// exit-0 and deploy_plan's shape.
		return toolResult(map[string]any{"results": []cliout.DeployJSON{}, "changes": 0, "failed": 0}), nil, nil
	}

	// The preflight gate, before connecting, exactly like the CLI: compile
	// everything and refuse the whole run on any failure or error-severity
	// diagnostic, so a bad projection can't leave a half-applied set. No
	// bypass exists here - the CLI's --no-validate is an operator's flag,
	// not an agent's. Config-bad projections skip the gate and refuse
	// per-projection in the plan instead, like the CLI.
	if refusal := deployPreflight(ctx, cfg, root, names); refusal != nil {
		return refusal, nil, nil
	}

	client, env, cleanup, err := s.connectRemote(cfg, root, in.Env)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	defer cleanup()

	driftCh := drift.StartConfigDriftCheck(ctx, cfg, root, env.Name, env.Connection)

	plan := drift.PlanAll(ctx, client, cfg, root, names)
	drift.ResolveResets(plan, in.ResetOnLogicChange)

	changes, rebuilds := 0, 0
	var faulted, overwrites []string
	for _, it := range plan {
		if it.Err != nil {
			continue
		}
		if it.Action.Applies() {
			changes++
			if it.Cmp.ExternallyChanged() {
				overwrites = append(overwrites, it.Name)
			}
		}
		if it.Action == drift.ActionReset {
			rebuilds++
		}
		if it.Action == drift.ActionUpdate && it.Faulted {
			faulted = append(faulted, it.Name)
		}
	}

	target, prod := operateTarget(ctx, client, env.Name)

	result := map[string]any{
		"env":        env.Name,
		"target":     target,
		"production": prod,
		"changes":    changes,
	}
	if items := <-driftCh; len(items) > 0 {
		result["configDrift"] = cliout.BuildConfigDriftJSON(items)
	}

	// A plan that changes nothing needs no confirmation and writes nothing;
	// skips and refusals still report, like a real deploy's summary.
	if changes == 0 {
		results := make([]cliout.DeployJSON, 0, len(plan))
		failed := 0
		for _, it := range plan {
			res := it.Result()
			if res.Err != nil || res.Action == drift.ActionRefuse {
				failed++
			}
			results = append(results, cliout.BuildDeployJSON(res))
		}
		result["results"] = results
		result["failed"] = failed
		return toolResult(result), nil, nil
	}

	if r := confirmWrite(ctx, req, deployApplyGate(in, env.Name, target, prod, changes, rebuilds, faulted, overwrites)); r != nil {
		return r, nil, nil
	}

	ledger := stamp.Ledger(env, remote.OpDeploy, s.version, root)
	results := make([]cliout.DeployJSON, 0, len(plan))
	failed := apply.Plan(ctx, plan, client, ledger, nil, func(res drift.Result) {
		results = append(results, cliout.BuildDeployJSON(res))
	})
	result["results"] = results
	result["failed"] = failed
	return toolResult(result), nil, nil
}

// deployApplyGate shapes the confirm for a deploy: the change counts lead the
// consequence, rebuilds put the plan in the no-undo tier (state is destroyed,
// so a production confirm types the environment name - the plan spans
// projections, so there is no single projection name to type), and the
// faulted/overwrite cautions the CLI prints pre-confirm ride along.
func deployApplyGate(in deployApplyInput, envName, target string, prod bool, changes, rebuilds int, faulted, overwrites []string) writeGate {
	plural := "s"
	if changes == 1 {
		plural = ""
	}
	consequence := fmt.Sprintf("Applies %d change%s.", changes, plural)
	if rebuilds > 0 {
		consequence = fmt.Sprintf("Applies %d change%s, including %d rebuild(s) from zero (state destroyed; an emitting projection re-emits).", changes, plural, rebuilds)
	}
	if len(overwrites) > 0 {
		consequence += fmt.Sprintf(" Overwrites out-of-band changes on %s.", strings.Join(overwrites, ", "))
	}
	if len(faulted) > 0 {
		consequence += fmt.Sprintf(" Updating won't clear the fault on %s.", strings.Join(faulted, ", "))
	}

	cli := "gaffer deploy"
	if in.Name != "" {
		cli += " " + shellQuote(in.Name)
	}
	if in.ResetOnLogicChange {
		cli += " --reset-on-logic-change"
	}

	return writeGate{
		Action:      fmt.Sprintf("deploy %d change%s", changes, plural),
		Name:        in.Name,
		Env:         envName,
		Target:      target,
		Production:  prod,
		NoUndo:      rebuilds > 0,
		TypedValue:  envName,
		TypedNoun:   "environment name",
		Consequence: consequence,
		CLI:         cli,
	}
}

// deployPreflight compiles every projection and rejects the run on any
// compile failure or error-severity diagnostic, mirroring the CLI's
// runPreflight semantics. Returns nil when everything is deployable.
func deployPreflight(ctx context.Context, cfg *config.Config, root string, names []string) *mcp.CallToolResult {
	var reasons []string
	for _, name := range names {
		if ctx.Err() != nil {
			return toolError("%v", ctx.Err())
		}
		def := cfg.FindProjection(name)
		// A config-bad projection is refused per-projection in the plan (via
		// drift.Invalid); skip it here so it doesn't fail the all-or-nothing
		// preflight and abort the deploy of the good projections alongside it.
		if cfg.ProjectionConfigError(name) != nil {
			continue
		}
		source, err := engine.ReadSource(root, def.Entry)
		if err != nil {
			reasons = append(reasons, name+": "+err.Error())
			continue
		}
		diags, err := engine.Preflight(engine.NewProjection(root, cfg, def, source))
		if err != nil {
			reasons = append(reasons, name+": "+err.Error())
			continue
		}
		for _, d := range diags {
			reasons = append(reasons, name+": "+d.Code+": "+d.Message)
		}
	}
	if len(reasons) == 0 {
		return nil
	}
	return toolError("preflight failed, nothing was deployed: %s", strings.Join(reasons, "; "))
}
