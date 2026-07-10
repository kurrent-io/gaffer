package cliout

import (
	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

// DeployJSON is the machine shape for one projection's deploy verdict.
// outcome is the verdict: created, updated, rebuilt, skipped, or failed from a
// deploy run; refused when a valid projection can't be applied in place (a
// recreate is needed - recreate is then true); or invalid when the local
// definition won't run. reason explains a refused/invalid verdict, error a
// failed one. logicChange marks an "updated" outcome that continued over a
// changed query (state kept), so CI can alert on it; a rebuild surfaces as
// outcome "rebuilt" instead. externalChange marks an apply whose deployed
// definition had been changed outside gaffer since its last deploy (so the
// apply overwrote that change), with externalChangeTool naming the tool behind
// it when another tool made the change. faulted marks an update over a
// currently-faulted projection, and emittingReset a rebuild whose projection
// emits (reprocessing re-emits) - both plan-phase cautions. Shared by
// `gaffer deploy --json` (including --dry-run, where outcome is the would-be
// verdict) and the MCP deploy_plan tool.
type DeployJSON struct {
	Name               string `json:"name"`
	Outcome            string `json:"outcome"`
	Recreate           bool   `json:"recreate,omitempty"`
	LogicChange        bool   `json:"logicChange,omitempty"`
	ExternalChange     bool   `json:"externalChange,omitempty"`
	ExternalChangeTool string `json:"externalChangeTool,omitempty"`
	Faulted            bool   `json:"faulted,omitempty"`
	EmittingReset      bool   `json:"emittingReset,omitempty"`
	Reason             string `json:"reason,omitempty"`
	Error              string `json:"error,omitempty"`
}

// BuildDeployJSON maps one deploy result to its machine shape. The faulted and
// emittingReset flags are plan-phase metadata not carried on a Result, so they
// stay unset here and are filled by BuildPlanJSON from the plan item.
func BuildDeployJSON(r drift.Result) DeployJSON {
	j := DeployJSON{
		Name:    r.Name,
		Outcome: r.Outcome(),
		// A refusal now means only recreate-required (an invalid local reads as
		// outcome "invalid"), so the outcome word is the recreate discriminator.
		Recreate:           r.Action == drift.ActionRefuse,
		LogicChange:        r.LogicChange,
		ExternalChange:     r.ExternalChange,
		ExternalChangeTool: r.ExternalChangeTool,
		Reason:             r.Reason,
	}
	if r.Err != nil {
		j.Error = r.Err.Error()
	}
	return j
}

// BuildPlanJSON maps a computed plan to the deploy JSON shape, each item's
// outcome being the verdict an apply would produce - the same array a
// `gaffer deploy --dry-run --json` emits under `plan`.
func BuildPlanJSON(plan []drift.PlanItem) []DeployJSON {
	out := make([]DeployJSON, 0, len(plan))
	for _, it := range plan {
		j := BuildDeployJSON(it.Result())
		j.Faulted = it.Faulted
		j.EmittingReset = it.Action == drift.ActionReset && it.Cmp.Local != nil && it.Cmp.Local.Emit
		out = append(out, j)
	}
	return out
}

// PlanReportJSON is the deploy plan envelope: the per-projection plan, a
// top-level verdict for what a real deploy would do (in-sync / deployable /
// blocked), the change count, and the env-level [database_config] drift - the
// deploy counterpart to StatusReportJSON. Shared by
// `gaffer deploy --dry-run --json` and the MCP deploy_plan tool. Env, Target and
// Production are set by the caller (the resolved environment name, the server's
// self-reported cluster name, and whether the target gates as production), so a
// consumer reading the plan alone knows where it lands. configDrift is omitted
// when clean, not declared, or unreadable; configDriftError carries the reason a
// declared check couldn't run, so an absent configDrift isn't read as "in sync".
type PlanReportJSON struct {
	Env              string            `json:"env,omitempty"`
	Target           string            `json:"target,omitempty"`
	Production       *bool             `json:"production,omitempty"`
	Verdict          string            `json:"verdict"`
	Changes          int               `json:"changes"`
	Plan             []DeployJSON      `json:"plan"`
	ConfigDrift      []ConfigDriftJSON `json:"configDrift,omitempty"`
	ConfigDriftError string            `json:"configDriftError,omitempty"`
}

// BuildPlanReport assembles the plan envelope from a computed plan and the
// config-drift check's result. Env/Target/Production are left for the caller to
// fill. Changes counts the items an apply would write (create/update/rebuild).
func BuildPlanReport(plan []drift.PlanItem, dr drift.ConfigDriftResult) PlanReportJSON {
	changes := 0
	for _, it := range plan {
		if it.Err == nil && it.Action.Applies() {
			changes++
		}
	}
	report := PlanReportJSON{
		Verdict: drift.PlanVerdict(plan),
		Changes: changes,
		Plan:    BuildPlanJSON(plan),
	}
	// The same Items/Err mutual exclusion BuildStatusReport enforces, so a
	// mis-constructed result can't emit ambiguous machine output.
	if dr.Err != nil {
		report.ConfigDriftError = dr.Err.Error()
	} else if len(dr.Items) > 0 {
		report.ConfigDrift = BuildConfigDriftJSON(dr.Items)
	}
	return report
}
