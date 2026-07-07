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
		"the resolved env, target, and production; a non-zero `failed` means some items " +
		"failed, were refused, or failed preflight (their result carries the reason or " +
		"error - a preflight failure reports every invalid projection as outcome " +
		"invalid, with nothing deployed and no target resolved). Use " +
		"deploy_plan first to preview. On a " +
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
	// per-projection in the plan instead, like the CLI. Failures come back
	// in the standard envelope as outcome-invalid items (the same shape
	// `gaffer deploy --json` renders them), every failing projection listed;
	// target/production are omitted - nothing connected, so nothing is known.
	if failures := deployPreflight(ctx, cfg, root, names); len(failures) > 0 {
		result := map[string]any{
			"changes": 0,
			"failed":  len(failures),
			"results": failures,
		}
		if env, err := cfg.ResolveEnv(in.Env); err == nil {
			result["env"] = env.Name
		}
		return toolResult(result), nil, nil
	}

	client, env, cleanup, err := s.connectRemote(cfg, root, in.Env)
	if err != nil {
		return toolError("%v", err), nil, nil
	}
	defer cleanup()

	driftCh := drift.StartConfigDriftCheck(ctx, cfg, root, env)

	plan := drift.PlanAll(ctx, client, cfg, root, names)
	drift.ResolveResets(plan, in.ResetOnLogicChange)

	if ctx.Err() != nil {
		return toolError("%v; nothing was deployed", ctx.Err()), nil, nil
	}

	changes := 0
	var faulted []string
	for _, it := range plan {
		if it.Err != nil {
			continue
		}
		if it.Action.Applies() {
			changes++
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
	if len(faulted) > 0 {
		result["faultedUpdates"] = faulted
	}
	driftRes := <-driftCh
	if driftRes.Err != nil {
		// A failed node-config read must not present as "in sync" (UI-1820).
		result["configDriftError"] = driftRes.Err.Error()
	} else if len(driftRes.Items) > 0 {
		result["configDrift"] = cliout.BuildConfigDriftJSON(driftRes.Items)
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

	if r := confirmWrite(ctx, req, deployApplyGate(in, env.Name, target, prod, plan, driftRes)); r != nil {
		return r, nil, nil
	}

	ledger := stamp.Ledger(env, remote.OpDeploy, s.version, root)
	results := make([]cliout.DeployJSON, 0, len(plan))
	failed := apply.Plan(ctx, plan, client, ledger, nil, func(res drift.Result) {
		results = append(results, cliout.BuildDeployJSON(res))
	})
	result["results"] = results
	result["failed"] = failed
	if len(results) < len(plan) {
		// A cancellation stopped the loop; say so rather than shipping a
		// truncated result that reads as a complete run.
		result["interrupted"] = true
	}
	return toolResult(result), nil, nil
}

// deployApplyGate shapes the confirm for a deploy. The human at the elicit
// is the one link the gate exists not to trust the agent over, so the
// consequence names the changed projections (capped), not just counts, and
// carries the cautions the CLI prints pre-confirm: rebuilds (which put the
// plan in the no-undo tier - state is destroyed, so a production confirm
// types the environment name; the plan spans projections, so there is no
// single projection name to type), out-of-band overwrites, faulted update
// targets, emitting rebuilds, and a diverging [database_config].
func deployApplyGate(in deployApplyInput, envName, target string, prod bool, plan []drift.PlanItem, dr drift.ConfigDriftResult) writeGate {
	changes, rebuilds := 0, 0
	var changed, faulted, overwrites, emitting []string
	for _, it := range plan {
		if it.Err != nil {
			continue
		}
		if it.Action.Applies() {
			changes++
			changed = append(changed, it.Name)
			if it.Cmp.ExternallyChanged() {
				overwrites = append(overwrites, it.Name)
			}
		}
		if it.Action == drift.ActionReset {
			rebuilds++
			if it.Cmp.Local != nil && it.Cmp.Local.Emit {
				emitting = append(emitting, it.Name)
			}
		}
		if it.Action == drift.ActionUpdate && it.Faulted {
			faulted = append(faulted, it.Name)
		}
	}

	plural := "s"
	if changes == 1 {
		plural = ""
	}
	consequence := fmt.Sprintf("Changes %s.", capNames(changed))
	if rebuilds > 0 {
		consequence += fmt.Sprintf(" %d rebuild(s) from zero: state destroyed.", rebuilds)
	}
	if len(emitting) > 0 {
		consequence += fmt.Sprintf(" %s re-emit(s) on rebuild and may duplicate.", capNames(emitting))
	}
	if len(overwrites) > 0 {
		consequence += fmt.Sprintf(" Overwrites out-of-band changes on %s.", capNames(overwrites))
	}
	if len(faulted) > 0 {
		consequence += fmt.Sprintf(" Updating won't clear the fault on %s.", capNames(faulted))
	}
	if dr.Err != nil {
		// The human decides with less information than usual - say so.
		consequence += " The target's [database_config] could not be checked."
	} else if len(dr.Items) > 0 {
		consequence += " The target's [database_config] diverges from gaffer.toml."
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

// capNames joins names for a single-line confirm, capping the list so a
// large deploy doesn't scroll the question off the dialog.
func capNames(names []string) string {
	const max = 5
	if len(names) <= max {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, and %d more", strings.Join(names[:max], ", "), len(names)-max)
}

// deployPreflight compiles every projection and collects the ones that
// can't be deployed - a compile failure or an error-severity diagnostic -
// mirroring the CLI's runPreflight semantics, as the outcome-invalid items
// `gaffer deploy --json` renders. Every failing projection is reported, not
// just the first, so a broken set is fixed in one pass. Empty when
// everything is deployable.
func deployPreflight(ctx context.Context, cfg *config.Config, root string, names []string) []cliout.DeployJSON {
	var failures []cliout.DeployJSON
	invalid := func(name string, reasons ...string) {
		failures = append(failures, cliout.DeployJSON{Name: name, Outcome: "invalid", Reason: strings.Join(reasons, "; ")})
	}
	for _, name := range names {
		if ctx.Err() != nil {
			break
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
			invalid(name, err.Error())
			continue
		}
		diags, err := engine.Preflight(engine.NewProjection(root, cfg, def, source))
		if err != nil {
			invalid(name, err.Error())
			continue
		}
		if len(diags) > 0 {
			reasons := make([]string, 0, len(diags))
			for _, d := range diags {
				reasons = append(reasons, d.Code+": "+d.Message)
			}
			invalid(name, reasons...)
		}
	}
	return failures
}
