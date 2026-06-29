package cmd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// projectionRPCTimeout bounds a single projection management call. The
// projections subsystem replies with nothing while it is still starting, so an
// unbounded call would hang; diff and status bound the whole command by it,
// deploy bounds each projection by it. Defined once in internal/deploy (the
// rebuild/recreate sequences there bound each step by it) so they all move
// together.
const projectionRPCTimeout = deploy.RPCTimeout

// loadProject finds the project root and loads gaffer.toml, without connecting.
// Lets a command run local-only work (deploy's preflight compile) before
// touching the server.
func loadProject() (cfg *config.Config, root string, err error) {
	root = project.FindRoot()
	if root == "" {
		return nil, "", project.ErrNotInProject
	}
	cfg, err = config.Load(project.ConfigPath(root))
	if err != nil {
		return nil, "", err
	}
	return cfg, root, nil
}

// liveConn is a connected live target: the loaded project (cfg/root), the remote
// client, and the cleanup to defer. Returned as a struct rather than a five-value
// tuple so callers take the fields they need without positional discards.
type liveConn struct {
	cfg     *config.Config
	root    string
	r       *remote.Client
	cleanup func()
}

// connectEnv loads the project, resolves the live env from explicit flags, and
// connects. Shared by the server-touching projection commands (diff, status,
// recreate, and the operate verbs).
func connectEnv(connection, env string) (liveConn, error) {
	cfg, root, err := loadProject()
	if err != nil {
		return liveConn{}, err
	}
	r, cleanup, err := connectResolved(cfg, root, connection, env)
	if err != nil {
		return liveConn{}, err
	}
	return liveConn{cfg: cfg, root: root, r: r, cleanup: cleanup}, nil
}

// connectResolved resolves the live env from explicit flags against an
// already-loaded config and connects. Split from connectEnv so deploy can load
// the config, run preflight locally, then connect only once the projections are
// known to be deployable.
func connectResolved(cfg *config.Config, root, connection, env string) (r *remote.Client, cleanup func(), err error) {
	resolved, err := resolveLiveEnv(connection, env, cfg)
	if err != nil {
		return nil, nil, err
	}
	if resolved.Connection == "" {
		return nil, nil, errors.New("no environment: mark a default [env.<name>], pass --env, or pass --connection")
	}
	client, _, err := engine.Connect(resolved.Connection, root, resolved.Name, resolved.OAuth, resolved.Cert)
	if err != nil {
		return nil, nil, err
	}
	return remote.New(client), func() { _ = client.Close() }, nil
}

// refuseNoValidateOnProd is the single source of the production --no-validate
// guardrail, shared by deploy and recreate so the message and exit code stay in
// lockstep. --no-validate skips the preflight compile gate; production never
// accepts it, so a prod deploy/recreate always validates first. exitWith(3) is
// the guardrail-refusal code, and (unlike a silent wrap) lets fang print the
// reason instead of swallowing it. verb names the command ("Deploy"/"Recreate")
// and subject reads in "so <subject> are/is validated first".
func refuseNoValidateOnProd(verb, subject, target string) error {
	return exitWith(3, fmt.Errorf("--no-validate is not allowed on production %s: it skips the preflight compile check. %s without it so %s validated first",
		targetDesc(target), verb, subject))
}

// driftState is how one projection compares between local config and the server.
// Shared by gaffer diff (one projection) and gaffer status (the overview across
// all projections).
type driftState string

const (
	driftInSync      driftState = "in-sync"
	driftDrifted     driftState = "drifted"
	driftNotDeployed driftState = "not-deployed" // in local config, absent on the server
	driftUntracked   driftState = "untracked"    // on the server, not in local config
	driftInvalid     driftState = "invalid"      // local definition unusable (compile or config error); drift indeterminate
)

// comparison is the result of comparing one projection's local definition
// against what's deployed. Local/Deployed are set when that side exists; Cmp is
// meaningful only when Drifted.
//
// LocalErr is set when the local definition is unusable - it failed to compile,
// or carries a per-projection config error (State is then driftInvalid). Local
// holds the partial descriptor (query, engine version, track-emitted-streams)
// when it could be built; it may be nil for a config error that leaves nothing to
// read (e.g. a bad entry). Emit is unknown, so Cmp's emit dimension and the hash
// verdict are not meaningful.
type comparison struct {
	Name     string
	State    driftState
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

// ownership is who a deployed projection belongs to, derived from local-config
// membership and the ledger. Orthogonal to driftState: an in-config projection is
// in-config whatever its drift, and only an untracked one can be orphan or foreign.
type ownership string

const (
	ownerInConfig ownership = "in-config" // present in local gaffer.toml
	ownerOrphan   ownership = "orphan"    // not in config, carries gaffer's tool metadata → deletion candidate
	ownerForeign  ownership = "foreign"   // not in config, carries another tool's metadata → leave it alone
	ownerUnknown  ownership = "unknown"   // not in config, no readable tool entry (metadata-less server, pre-metadata deploy, or unreadable)
)

// owner classifies a deployed projection. Only an untracked one can be orphan /
// foreign / unknown; everything in local config is in-config. "foreign" is asserted
// only when a non-gaffer tool entry is actually present - a projection with no
// readable tool entry is unknown (we can't tell, so it degrades to plain untracked),
// never foreign, so an old server or a pre-metadata gaffer deploy isn't mislabelled.
func (c comparison) owner() ownership {
	switch {
	case c.State != driftUntracked:
		return ownerInConfig
	case c.Ledger == nil:
		return ownerUnknown
	case c.Ledger.Tool == remote.ToolName:
		return ownerOrphan
	default:
		return ownerForeign
	}
}

// attribution explains a drifted projection - who or what caused the deployed
// definition to differ from local. Meaningful only for a drifted projection with a
// readable ledger; otherwise attrNone (render degrades to a plain "drifted").
type attribution string

const (
	attrNone          attribution = ""                // not drifted, or no ledger to attribute from
	attrLocalAhead    attribution = "local-ahead"     // deployed still matches my last gaffer deploy - local moved ahead
	attrChangedByTool attribution = "changed-by-tool" // deployed matches another tool's deploy - local differs
	attrChangedServer attribution = "changed-server"  // deployed differs from the latest tool entry - changed on the server since
)

// attribution compares the current deployed definition against the latest tool
// entry's: matching means nothing changed the definition on the server since that
// write (only the local side moved, or that tool is the author); differing means a
// later write changed it. The latest tool entry being gaffer's vs another tool's
// then says whether the in-sync-with-server case is "I edited local" or "another
// tool deployed this".
func (c comparison) attribution() attribution {
	if c.State != driftDrifted || c.Ledger == nil || c.DeployBaseline == nil || c.Deployed == nil {
		return attrNone
	}
	if c.Deployed.Hash() == c.DeployBaseline.Hash() {
		if c.Ledger.Tool == remote.ToolName {
			return attrLocalAhead
		}
		return attrChangedByTool
	}
	return attrChangedServer
}

// ledgerJSON is the machine view of the latest tool entry behind a deployed
// projection - the --json `lastWrite`, the tool attribution (who) behind an owner
// or attribution verdict. The when is the top-level `lastDeployed`, which is event
// time and present with or without a tool entry, so it's not duplicated here.
type ledgerJSON struct {
	Tool  string `json:"tool"`
	Actor string `json:"actor,omitempty"`
}

func (c comparison) lastWrite() *ledgerJSON {
	if c.Ledger == nil {
		return nil
	}
	return &ledgerJSON{Tool: c.Ledger.Tool, Actor: c.Ledger.Actor}
}

// lastDeployTime is when the projection was last deployed for display: the tool
// entry's time when there's a ledger (the deploy, not a later lifecycle write),
// else the deployed definition's write time. Zero when not deployed. Always event
// time - the convention stores no timestamp; the deploy date is never in metadata.
func (c comparison) lastDeployTime() time.Time {
	if c.Ledger != nil {
		return c.Ledger.Time
	}
	return c.DeployedAt
}

// lastDeployedJSON is lastDeployTime formatted for --json, or "" (omitted) when
// not deployed.
func (c comparison) lastDeployedJSON() string {
	if at := c.lastDeployTime(); !at.IsZero() {
		return at.Format(time.RFC3339)
	}
	return ""
}

// withLedger records the deployed projection's latest tool entry on the comparison:
// the entry on success; LedgerErr on an unreadable entry (flag the row, keep going);
// nothing on no-entry or a vanished projection (degrade to pre-ledger behaviour). A
// transport error aborts, like any other read failure.
func withLedger(ctx context.Context, r *remote.Client, c comparison) (comparison, error) {
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
		return comparison{}, err
	}
	return c, nil
}

// compareProjection compares one projection's local definition against what's
// deployed. A name absent from local config but present on the server is
// untracked; absent from both is an error.
func compareProjection(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string) (comparison, error) {
	def := cfg.FindProjection(name)
	if def == nil {
		deployedDef, err := r.Read(ctx, name)
		if errors.Is(err, remote.ErrNotFound) {
			return comparison{}, fmt.Errorf("projection %q is not in gaffer.toml or deployed on the server", name)
		}
		if err != nil {
			return comparison{}, err
		}
		deployed := deployedDef.Descriptor()
		return withLedger(ctx, r, comparison{Name: name, State: driftUntracked, Deployed: &deployed, DeployedAt: deployedDef.Time})
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
		return comparison{Name: name, State: driftInvalid, LocalErr: cfgErr}, nil
	}

	source, err := engine.ReadSource(root, def.Entry)
	if err != nil {
		return comparison{}, err
	}
	proj := engine.NewProjection(root, cfg, def, source)
	local, localErr := engine.LocalDescriptor(proj)

	deployedDef, err := r.Read(ctx, name)
	notDeployed := errors.Is(err, remote.ErrNotFound)
	if err != nil && !notDeployed {
		return comparison{}, err
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
		c := comparison{Name: name, State: driftInvalid, Local: &partial, LocalErr: localErr}
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
		return comparison{Name: name, State: driftNotDeployed, Local: &local}, nil
	}

	deployed := deployedDef.Descriptor()
	cmp := deploy.Compare(local, deployed)
	state := driftInSync
	if !cmp.InSync() {
		state = driftDrifted
	}
	return withLedger(ctx, r, comparison{Name: name, State: state, Cmp: cmp, Local: &local, Deployed: &deployed, DeployedAt: deployedDef.Time})
}
