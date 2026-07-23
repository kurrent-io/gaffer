package cliout

import (
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// HistoryJSON is the machine shape for one classified version. outOfBand
// is the out-of-band flag (a non-gaffer write after gaffer began managing the
// projection); kind is the classification; the tool fields are present only when
// the version carried metadata. Shared by `gaffer history --json` and the MCP
// deploy_history tool.
type HistoryJSON struct {
	Version       int64              `json:"version"`
	Time          string             `json:"time"`
	ContentHash   string             `json:"contentHash"`
	Kind          string             `json:"kind"`
	Enabled       bool               `json:"enabled"`
	OutOfBand     bool               `json:"outOfBand"`
	StateChange   bool               `json:"stateChange,omitempty"`
	Deleted       bool               `json:"deleted,omitempty"`
	Tool          string             `json:"tool,omitempty"`
	ToolVersion   string             `json:"toolVersion,omitempty"`
	Operation     string             `json:"operation,omitempty"`
	Actor         string             `json:"actor,omitempty"`
	Revision      string             `json:"revision,omitempty"`
	ConfigChanges []ConfigChangeJSON `json:"configChanges,omitempty"`
}

// ConfigChangeJSON is one tuning knob a reconfigured version moved, with its
// before/after values in display form.
type ConfigChangeJSON struct {
	Knob string `json:"knob"`
	From string `json:"from"`
	To   string `json:"to"`
}

// BuildHistoryJSON maps classified versions - newest first, uncollapsed (one
// entry per stream write) - to their machine shape.
func BuildHistoryJSON(versions []remote.ClassifiedVersion) []HistoryJSON {
	out := make([]HistoryJSON, 0, len(versions))
	for _, cv := range versions {
		j := HistoryJSON{
			Version:     cv.Number,
			ContentHash: cv.ContentHash,
			Kind:        string(cv.Kind),
			Enabled:     cv.Enabled(),
			OutOfBand:   cv.OutOfBand(),
			StateChange: cv.StateChange(),
			Deleted:     cv.Deleted,
		}
		if cv.Definition != nil && !cv.Definition.Time.IsZero() {
			j.Time = cv.Definition.Time.Format(time.RFC3339)
		}
		if cv.Ledger != nil {
			j.Tool = cv.Ledger.Tool
			j.ToolVersion = cv.Ledger.ToolVersion
			j.Operation = cv.Ledger.Operation
			j.Actor = cv.Ledger.Actor
			j.Revision = cv.Ledger.Revision
		}
		for _, cc := range cv.ConfigChanges {
			j.ConfigChanges = append(j.ConfigChanges, ConfigChangeJSON{Knob: cc.Label, From: cc.From, To: cc.To})
		}
		out = append(out, j)
	}
	return out
}
