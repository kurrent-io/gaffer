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
	Name        string `json:"name"`
	Drift       string `json:"drift"`
	Owner       string `json:"owner"`
	Attribution string `json:"attribution,omitempty"`
	// Hash is the deployed definition's content hash - what's actually running
	// on the server - so a consumer can pin the deployed version or match it to a
	// history entry without a second call. Omitted when the projection isn't
	// deployed (there's nothing on the server to hash).
	Hash         string             `json:"hash,omitempty"`
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
// or attribution verdict. toolVersion and revision pin what deployed it: the
// tool's version, and the project's source revision (a git commit, +changes when
// the tree was dirty). Both omitted when the entry didn't record them. The when
// is the top-level `lastDeployed`, which is event time and present with or
// without a tool entry, so it's not duplicated here.
type LedgerJSON struct {
	Tool        string `json:"tool"`
	ToolVersion string `json:"toolVersion,omitempty"`
	Revision    string `json:"revision,omitempty"`
	Actor       string `json:"actor,omitempty"`
}

// StatusReportJSON is the status envelope: the per-projection entries, plus
// the env-level [database_config] drift so a machine consumer (the VS Code
// extension's status surface, an MCP agent) sees the target's engine
// configuration diverging without a second call. configDrift is omitted when
// clean, not declared, or unreadable - absence is "nothing to report", not
// "in sync".
type StatusReportJSON struct {
	// Env, Target, and Production make each response self-describing in
	// multi-env workflows: the resolved environment name, the server's
	// self-reported cluster name (falling back to the env name), and whether
	// the target gates as production (the server's own declaration OR the env's
	// production opt-in) - a consumer reading status alone can tell it is
	// pointed at a prod database before deciding to mutate. Set by both the CLI
	// and the MCP tool.
	Env         string            `json:"env,omitempty"`
	Target      string            `json:"target,omitempty"`
	Production  *bool             `json:"production,omitempty"`
	Projections []StatusJSON      `json:"projections"`
	ConfigDrift []ConfigDriftJSON `json:"configDrift,omitempty"`
	// ConfigDriftError says the [database_config] drift check could not read
	// the node's live options (auth refusal, unreachable HTTP surface, ...),
	// so an absent configDrift must not be read as "in sync" (UI-1820).
	ConfigDriftError string `json:"configDriftError,omitempty"`
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
func BuildStatusReport(entries []drift.StatusEntry, dr drift.ConfigDriftResult) StatusReportJSON {
	out := make([]StatusJSON, 0, len(entries))
	for _, e := range entries {
		j := StatusJSON{
			Name:         e.Name,
			Drift:        string(e.State),
			Owner:        string(e.Owner()),
			Attribution:  string(e.Attribution()),
			Hash:         deployedHashJSON(e.Comparison),
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
	// Enforce the result's Items/Err mutual exclusion at the render too, so
	// a mis-constructed result can't emit ambiguous machine output.
	if dr.Err != nil {
		report.ConfigDriftError = dr.Err.Error()
	} else if len(dr.Items) > 0 {
		report.ConfigDrift = BuildConfigDriftJSON(dr.Items)
	}
	return report
}

// BuildLedgerJSON is the comparison's latest tool entry for machine output,
// nil when it carries none.
func BuildLedgerJSON(c drift.Comparison) *LedgerJSON {
	if c.Ledger == nil {
		return nil
	}
	return &LedgerJSON{
		Tool:        c.Ledger.Tool,
		ToolVersion: c.Ledger.ToolVersion,
		Revision:    c.Ledger.Revision,
		Actor:       c.Ledger.Actor,
	}
}

// deployedHashJSON is the deployed definition's content hash, or "" (omitted)
// when the projection isn't deployed.
func deployedHashJSON(c drift.Comparison) string {
	if c.Deployed != nil {
		return c.Deployed.Hash()
	}
	return ""
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
