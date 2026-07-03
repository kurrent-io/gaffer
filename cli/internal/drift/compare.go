// Package drift computes how projections' local definitions compare to
// what's deployed on an environment: the per-projection drift verdict
// behind gaffer status and gaffer diff, the deploy plan derived from
// those verdicts, and the [database_config] divergence check. It is the
// read side of deployment - nothing here writes to the server - shared
// by the CLI commands and the MCP server's deploy tools.
package drift

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// State is how one projection compares between local config and the server.
// Shared by gaffer diff (one projection) and gaffer status (the overview across
// all projections).
type State string

const (
	InSync      State = "in-sync"
	Drifted     State = "drifted"
	NotDeployed State = "not-deployed" // in local config, absent on the server
	Untracked   State = "untracked"    // on the server, not in local config
	Invalid     State = "invalid"      // local definition unusable (compile or config error); drift indeterminate
)

// Comparison is the result of comparing one projection's local definition
// against what's deployed. Local/Deployed are set when that side exists; Cmp is
// meaningful only when Drifted.
//
// LocalErr is set when the local definition is unusable - it failed to compile,
// or carries a per-projection config error (State is then Invalid). Local
// holds the partial descriptor (query, engine version, track-emitted-streams)
// when it could be built; it may be nil for a config error that leaves nothing to
// read (e.g. a bad entry). Emit is unknown, so Cmp's emit dimension and the hash
// verdict are not meaningful.
type Comparison struct {
	Name     string
	State    State
	Cmp      deploy.Comparison
	Local    *deploy.Descriptor
	Deployed *deploy.Descriptor
	LocalErr error

	// Ledger is the latest tool-metadata entry on the deployed definition - any
	// tool, not just gaffer (the ownership marker is Tool == remote.ToolName). nil
	// when the projection isn't deployed, carries no tool entry, or the entry was
	// unreadable. LedgerErr is set only for an unreadable entry (ErrMalformedLedger),
	// so render can flag that one projection without aborting; a plain absent entry
	// leaves both nil and degrades silently.
	Ledger    *remote.Ledger
	LedgerErr error
	// DeployBaseline is the definition stamped on the latest tool entry - what that
	// tool deployed. Drift attribution compares the current Deployed definition
	// against it to tell a local-only edit from a change made on the server since.
	// nil when there's no readable ledger. Not to be confused with the lastDeployed
	// JSON field: this is the baseline definition, that is a timestamp.
	DeployBaseline *deploy.Descriptor
	// DeployedAt is when the deployed definition was last written (the
	// $ProjectionUpdated event's time), available with or without a ledger. Zero
	// when not deployed. It's the last-deploy time status falls back to for a
	// projection carrying no tool metadata.
	DeployedAt time.Time
}

// Ownership is who a deployed projection belongs to, derived from local-config
// membership and the ledger. Orthogonal to State: an in-config projection is
// in-config whatever its drift, and only an untracked one can be orphan or foreign.
type Ownership string

const (
	OwnerInConfig Ownership = "in-config" // present in local gaffer.toml
	OwnerOrphan   Ownership = "orphan"    // not in config, carries gaffer's tool metadata → deletion candidate
	OwnerForeign  Ownership = "foreign"   // not in config, carries another tool's metadata → leave it alone
	OwnerUnknown  Ownership = "unknown"   // not in config, no readable tool entry (metadata-less server, pre-metadata deploy, or unreadable)
)

// Owner classifies a deployed projection. Only an untracked one can be orphan /
// foreign / unknown; everything in local config is in-config. "foreign" is asserted
// only when a non-gaffer tool entry is actually present - a projection with no
// readable tool entry is unknown (we can't tell, so it degrades to plain untracked),
// never foreign, so an old server or a pre-metadata gaffer deploy isn't mislabelled.
func (c Comparison) Owner() Ownership {
	switch {
	case c.State != Untracked:
		return OwnerInConfig
	case c.Ledger == nil:
		return OwnerUnknown
	case c.Ledger.Tool == remote.ToolName:
		return OwnerOrphan
	default:
		return OwnerForeign
	}
}

// Attribution explains a drifted projection - who or what caused the deployed
// definition to differ from local. Meaningful only for a drifted projection with a
// readable ledger; otherwise AttrNone (render degrades to a plain "drifted").
type Attribution string

const (
	AttrNone          Attribution = ""                // not drifted, or no ledger to attribute from
	AttrLocalAhead    Attribution = "local-ahead"     // deployed still matches my last gaffer deploy - local moved ahead
	AttrChangedByTool Attribution = "changed-by-tool" // deployed matches another tool's deploy - local differs
	AttrChangedServer Attribution = "changed-server"  // deployed differs from the latest tool entry - changed on the server since
)

// Attribution compares the current deployed definition against the latest tool
// entry's: matching means nothing changed the definition on the server since that
// write (only the local side moved, or that tool is the author); differing means a
// later write changed it. The latest tool entry being gaffer's vs another tool's
// then says whether the in-sync-with-server case is "I edited local" or "another
// tool deployed this".
func (c Comparison) Attribution() Attribution {
	if c.State != Drifted || c.Ledger == nil || c.DeployBaseline == nil || c.Deployed == nil {
		return AttrNone
	}
	if c.Deployed.Hash() == c.DeployBaseline.Hash() {
		if c.Ledger.Tool == remote.ToolName {
			return AttrLocalAhead
		}
		return AttrChangedByTool
	}
	return AttrChangedServer
}

// ExternallyChanged reports whether the deployed definition was changed outside
// gaffer since gaffer last deployed it - a metadata-less/direct write
// (changed-server) or another tool's deploy (changed-by-tool). deploy surfaces
// this so an update doesn't silently revert an out-of-band change without saying
// so. AttrLocalAhead (server still holds gaffer's last deploy) and AttrNone (no
// ledger to tell) are not external changes.
func (c Comparison) ExternallyChanged() bool {
	switch c.Attribution() {
	case AttrChangedServer, AttrChangedByTool:
		return true
	default:
		return false
	}
}

// LastDeployTime is when the projection was last deployed for display: the tool
// entry's time when there's a ledger (the deploy, not a later lifecycle write),
// else the deployed definition's write time. Zero when not deployed. Always event
// time - the convention stores no timestamp; the deploy date is never in metadata.
func (c Comparison) LastDeployTime() time.Time {
	if c.Ledger != nil {
		return c.Ledger.Time
	}
	return c.DeployedAt
}

// withLedger records the deployed projection's latest tool entry on the comparison:
// the entry on success; LedgerErr on an unreadable entry (flag the row, keep going);
// nothing on no-entry or a vanished projection (degrade to pre-ledger behaviour). A
// transport error aborts, like any other read failure.
func withLedger(ctx context.Context, r *remote.Client, c Comparison) (Comparison, error) {
	l, def, err := r.ReadLedger(ctx, c.Name)
	switch {
	case err == nil:
		c.Ledger = l
		if def != nil {
			d := def.Descriptor()
			c.DeployBaseline = &d
		}
	case errors.Is(err, remote.ErrNoLedger), errors.Is(err, remote.ErrNotFound):
		// No tool entry, or the projection vanished between the read and here - both
		// degrade to pre-ledger behaviour, not an error.
	case errors.Is(err, remote.ErrMalformedLedger):
		c.LedgerErr = err
	default:
		return Comparison{}, err
	}
	return c, nil
}

// Compare compares one projection's local definition against what's
// deployed. A name absent from local config but present on the server is
// untracked; absent from both is an error.
func Compare(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string) (Comparison, error) {
	def := cfg.FindProjection(name)
	if def == nil {
		deployedDef, err := r.Read(ctx, name)
		if errors.Is(err, remote.ErrNotFound) {
			// Wrap ErrNotFound so a caller can tell this apart: gaffer diff <name>
			// reports it, but gaffer status treats a projection that vanished between
			// its List and this read as a benign race and skips it.
			return Comparison{}, fmt.Errorf("%w: %q is not in gaffer.toml or deployed on the server", remote.ErrNotFound, name)
		}
		if err != nil {
			return Comparison{}, err
		}
		deployed := deployedDef.Descriptor()
		return withLedger(ctx, r, Comparison{Name: name, State: Untracked, Deployed: &deployed, DeployedAt: deployedDef.Time})
	}

	// A per-projection config error (a missing/invalid engine_version, a bad entry
	// path, a malformed fixture, ...) means there's no valid local definition. Surface it as invalid,
	// like a compile failure, so status/diff degrade and deploy refuses for this
	// projection alone rather than failing the whole command. Best-effort fill the
	// local/deployed columns for the diff; a bad entry leaves nothing to read, so
	// Local may stay nil (the invalid renderers tolerate that).
	if cfgErr := cfg.ProjectionConfigError(name); cfgErr != nil {
		// There's no valid local definition, so nothing to compile or compare - and
		// nothing safe to read, since the error may be that the entry escapes the
		// project root. Surface invalid with just the reason; the invalid renderers
		// handle a nil Local/Deployed. (No comparison means no misleading "in sync"
		// from an unset Cmp.)
		return Comparison{Name: name, State: Invalid, LocalErr: cfgErr}, nil
	}

	source, err := engine.ReadSource(root, def.Entry)
	if err != nil {
		return Comparison{}, err
	}
	proj := engine.NewProjection(root, cfg, def, source)
	local, localErr := engine.LocalDescriptor(proj)

	deployedDef, err := r.Read(ctx, name)
	notDeployed := errors.Is(err, remote.ErrNotFound)
	if err != nil && !notDeployed {
		return Comparison{}, err
	}

	// A local compile failure is a per-projection condition, not a command
	// failure: report it as invalid and keep going, so the rest of a status
	// overview still renders and a diff still shows source + config fields. The
	// partial descriptor carries everything that needs no compile; emit is
	// unknown, so there's no in-sync/drifted verdict. This absorbs every
	// session-creation failure (in practice a compile error), not only syntax
	// errors - the same classification preflight makes.
	if localErr != nil {
		partial := engine.PartialDescriptor(proj)
		c := Comparison{Name: name, State: Invalid, Local: &partial, LocalErr: localErr}
		if notDeployed {
			return c, nil
		}
		deployed := deployedDef.Descriptor()
		c.Deployed = &deployed
		c.DeployedAt = deployedDef.Time
		c.Cmp = deploy.Compare(partial, deployed)
		// A broken local definition doesn't change who deployed what's on the server,
		// so still read the ledger - the provenance is useful context for the fix.
		return withLedger(ctx, r, c)
	}

	if notDeployed {
		return Comparison{Name: name, State: NotDeployed, Local: &local}, nil
	}

	deployed := deployedDef.Descriptor()
	cmp := deploy.Compare(local, deployed)
	state := InSync
	if !cmp.InSync() {
		state = Drifted
	}
	return withLedger(ctx, r, Comparison{Name: name, State: state, Cmp: cmp, Local: &local, Deployed: &deployed, DeployedAt: deployedDef.Time})
}
