package cliout

import (
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

// DiffJSON is the machine-readable diff shape shared by the CLI's `gaffer diff
// --json` and the editor's gaffer/diffProjection LSP request. left/right name
// each operand (ref, content hash, canonical source); lines is the structured,
// colourable line diff. verdict and changes are the drift verdict and
// per-dimension flags, present only for the default deployed↔local diff - a
// version-to-version diff is a pure source diff.
type DiffJSON struct {
	Name    string            `json:"name"`
	Left    DiffSideJSON      `json:"left"`
	Right   DiffSideJSON      `json:"right"`
	Verdict *DiffVerdictJSON  `json:"verdict,omitempty"`
	Changes *ChangesJSON      `json:"changes,omitempty"`
	Lines   []deploy.DiffLine `json:"lines"`
}

// DiffSideJSON identifies one operand: which side (local / deployed / a specific
// historical version), its content hash, and the canonical source the lines were
// computed from. Hash is omitted when it can't be derived - an invalid local
// definition yields no emit, so it has no hash.
type DiffSideJSON struct {
	Ref    string `json:"ref"`
	Hash   string `json:"hash,omitempty"`
	Source string `json:"source"`
}

// DiffVerdictJSON is the drift verdict, mirroring gaffer status: drift, owner,
// attribution, and provenance. reason carries the compile error when invalid.
type DiffVerdictJSON struct {
	Drift        string      `json:"drift"`
	Owner        string      `json:"owner"`
	Attribution  string      `json:"attribution,omitempty"`
	LastDeployed string      `json:"lastDeployed,omitempty"`
	LastWrite    *LedgerJSON `json:"lastWrite,omitempty"`
	Reason       string      `json:"reason,omitempty"`
}

type ChangesJSON struct {
	Query               bool `json:"query"`
	EngineVersion       bool `json:"engineVersion"`
	Emit                bool `json:"emit"`
	TrackEmittedStreams bool `json:"trackEmittedStreams"`
}

// ComparisonDiffJSON builds the default deployed↔local diff shape from a drift
// comparison: the two sides, the drift verdict, and the structured line diff.
func ComparisonDiffJSON(e drift.Comparison) DiffJSON {
	var deployedQuery, localQuery string
	left := DiffSideJSON{Ref: "deployed"}
	right := DiffSideJSON{Ref: "local"}
	if e.Deployed != nil {
		left.Hash = e.Deployed.Hash()
		left.Source = e.Deployed.CanonicalQuery()
		deployedQuery = e.Deployed.Query
	}
	if e.Local != nil {
		right.Source = e.Local.CanonicalQuery()
		localQuery = e.Local.Query
		// A local hash needs emit, which an invalid (uncompilable) projection
		// can't provide, so omit it; the verdict reports the compile error instead.
		if e.State != drift.Invalid {
			right.Hash = e.Local.Hash()
		}
	}

	j := DiffJSON{
		Name:  e.Name,
		Left:  left,
		Right: right,
		Lines: deploy.LineDiff(deployedQuery, localQuery),
		Verdict: &DiffVerdictJSON{
			Drift:        string(e.State),
			Owner:        string(e.Owner()),
			Attribution:  string(e.Attribution()),
			LastDeployed: LastDeployedJSON(e),
			LastWrite:    BuildLedgerJSON(e),
		},
	}
	if e.State == drift.Invalid && e.LocalErr != nil {
		j.Verdict.Reason = e.LocalErr.Error()
	}
	if e.State == drift.Drifted {
		j.Changes = &ChangesJSON{
			Query:               e.Cmp.QueryDiffers,
			EngineVersion:       e.Cmp.EngineVersionDiffers,
			Emit:                e.Cmp.EmitDiffers,
			TrackEmittedStreams: e.Cmp.TrackEmittedStreamsDiffers,
		}
	}
	return j
}
