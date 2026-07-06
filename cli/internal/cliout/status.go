package cliout

import (
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

// StatusJSON is the machine shape for one projection's status: the drift
// verdict with its ownership/attribution refinements, the last-deploy
// provenance, and the runtime state when deployed. Shared by
// `gaffer status --json` and the MCP deploy_status tool.
type StatusJSON struct {
	Name         string             `json:"name"`
	Drift        string             `json:"drift"`
	Owner        string             `json:"owner"`
	Attribution  string             `json:"attribution,omitempty"`
	LastDeployed string             `json:"lastDeployed,omitempty"`
	LastWrite    *LedgerJSON        `json:"lastWrite,omitempty"`
	Runtime      *StatusRuntimeJSON `json:"runtime,omitempty"`
	// Reason is the compile or config error, present only when drift is
	// invalid, so a machine consumer sees why a projection is invalid, not
	// just that it is. Named like the deploy verdict's reason - both explain
	// a verdict - where error is reserved for a failed operation.
	Reason string `json:"reason,omitempty"`
}

// StatusRuntimeJSON is the deployed projection's live state.
type StatusRuntimeJSON struct {
	State       string  `json:"state"`
	Progress    float32 `json:"progress"`
	Position    string  `json:"position,omitempty"`
	FaultReason string  `json:"faultReason,omitempty"`
}

// LedgerJSON is the machine view of the latest tool entry behind a deployed
// projection - the `lastWrite`, the tool attribution (who) behind an owner
// or attribution verdict. The when is the top-level `lastDeployed`, which is
// event time and present with or without a tool entry, so it's not duplicated
// here.
type LedgerJSON struct {
	Tool  string `json:"tool"`
	Actor string `json:"actor,omitempty"`
}

// StatusReportJSON is the status envelope: the per-projection entries, plus
// the env-level [database_config] drift so a machine consumer (the VS Code
// extension's status surface, an MCP agent) sees the target's engine
// configuration diverging without a second call. configDrift is omitted when
// clean, not declared, or unreadable - absence is "nothing to report", not
// "in sync".
type StatusReportJSON struct {
	// Env is the resolved environment name, set by the MCP tool so each
	// response is self-describing in multi-env workflows. The CLI leaves it
	// empty (omitted) - the invocation already names the env there.
	Env         string            `json:"env,omitempty"`
	Projections []StatusJSON      `json:"projections"`
	ConfigDrift []ConfigDriftJSON `json:"configDrift,omitempty"`
}

// ConfigDriftJSON is one [database_config] divergence in machine output: the
// gaffer.toml knob name with the server's and the local declared values, in
// the knob's native unit (milliseconds for the timeouts, bytes for
// max_state_size).
type ConfigDriftJSON struct {
	Knob   string `json:"knob"`
	Server int64  `json:"server"`
	Local  int64  `json:"local"`
}

// BuildStatusReport assembles the shared status envelope from the collected
// entries and the config-drift check's result.
func BuildStatusReport(entries []drift.StatusEntry, items []drift.ConfigDrift) StatusReportJSON {
	out := make([]StatusJSON, 0, len(entries))
	for _, e := range entries {
		j := StatusJSON{
			Name:         e.Name,
			Drift:        string(e.State),
			Owner:        string(e.Owner()),
			Attribution:  string(e.Attribution()),
			LastDeployed: LastDeployedJSON(e.Comparison),
			LastWrite:    BuildLedgerJSON(e.Comparison),
		}
		if e.State == drift.Invalid && e.LocalErr != nil {
			j.Reason = e.LocalErr.Error()
		}
		if e.Runtime != nil {
			j.Runtime = &StatusRuntimeJSON{
				State:       string(e.Runtime.State),
				Progress:    e.Runtime.Progress,
				Position:    e.Runtime.Position,
				FaultReason: e.Runtime.FaultReason,
			}
		}
		out = append(out, j)
	}
	report := StatusReportJSON{Projections: out}
	if len(items) > 0 {
		report.ConfigDrift = BuildConfigDriftJSON(items)
	}
	return report
}

// BuildLedgerJSON is the comparison's latest tool entry for machine output,
// nil when it carries none.
func BuildLedgerJSON(c drift.Comparison) *LedgerJSON {
	if c.Ledger == nil {
		return nil
	}
	return &LedgerJSON{Tool: c.Ledger.Tool, Actor: c.Ledger.Actor}
}

// LastDeployedJSON is the comparison's last-deploy time formatted for machine
// output, or "" (omitted) when not deployed.
func LastDeployedJSON(c drift.Comparison) string {
	if at := c.LastDeployTime(); !at.IsZero() {
		return at.Format(time.RFC3339)
	}
	return ""
}

// BuildConfigDriftJSON maps the config-drift items to their machine shape.
func BuildConfigDriftJSON(items []drift.ConfigDrift) []ConfigDriftJSON {
	out := make([]ConfigDriftJSON, 0, len(items))
	for _, d := range items {
		out = append(out, ConfigDriftJSON{Knob: d.Knob, Server: d.Server, Local: d.Local})
	}
	return out
}
