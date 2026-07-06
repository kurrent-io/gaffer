package cliout

import (
	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

// DeployJSON is the machine shape for one projection's deploy verdict.
// outcome is the verdict: created, updated, rebuilt, skipped, refused, or
// failed from a deploy run, or invalid when the preflight gate rejected it
// before any server write. reason is set for refused and invalid, error for
// failed. logicChange marks an "updated" outcome that continued over a
// changed query (state kept), so CI can alert on it; a rebuild surfaces as
// outcome "rebuilt" instead. externalChange marks an apply whose deployed
// definition had been changed outside gaffer since its last deploy (so the
// apply overwrote that change), again so CI can alert. Shared by
// `gaffer deploy --json` (including --dry-run, where outcome is the would-be
// verdict) and the MCP deploy_plan tool.
type DeployJSON struct {
	Name           string `json:"name"`
	Outcome        string `json:"outcome"`
	LogicChange    bool   `json:"logicChange,omitempty"`
	ExternalChange bool   `json:"externalChange,omitempty"`
	Reason         string `json:"reason,omitempty"`
	Error          string `json:"error,omitempty"`
}

// BuildDeployJSON maps one deploy result to its machine shape.
func BuildDeployJSON(r drift.Result) DeployJSON {
	j := DeployJSON{Name: r.Name, Outcome: r.Outcome(), LogicChange: r.LogicChange, ExternalChange: r.ExternalChange, Reason: r.Reason}
	if r.Err != nil {
		j.Error = r.Err.Error()
	}
	return j
}

// BuildPlanJSON maps a computed plan to the deploy JSON shape, each item's
// outcome being the verdict an apply would produce - the same array a
// `gaffer deploy --dry-run --json` emits.
func BuildPlanJSON(plan []drift.PlanItem) []DeployJSON {
	out := make([]DeployJSON, 0, len(plan))
	for _, it := range plan {
		out = append(out, BuildDeployJSON(it.Result()))
	}
	return out
}
