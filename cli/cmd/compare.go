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
// deploy bounds each projection by it. Shared so the three move together.
const projectionRPCTimeout = 30 * time.Second

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

// connectEnv loads the project, resolves the live env from explicit flags, and
// connects, returning a remote client and a cleanup to defer. Shared by the
// server-touching projection commands (diff, status, and deploy).
func connectEnv(connection, env string) (cfg *config.Config, root string, r *remote.Client, cleanup func(), err error) {
	cfg, root, err = loadProject()
	if err != nil {
		return nil, "", nil, nil, err
	}
	r, cleanup, err = connectResolved(cfg, root, connection, env)
	if err != nil {
		return nil, "", nil, nil, err
	}
	return cfg, root, r, cleanup, nil
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

// driftState is how one projection compares between local config and the server.
// Shared by gaffer diff (one projection) and gaffer status (the overview across
// all projections).
type driftState string

const (
	driftInSync      driftState = "in-sync"
	driftDrifted     driftState = "drifted"
	driftNotDeployed driftState = "not-deployed" // in local config, absent on the server
	driftUntracked   driftState = "untracked"    // on the server, not in local config
	driftInvalid     driftState = "invalid"      // local source doesn't compile; drift is indeterminate
)

// comparison is the result of comparing one projection's local definition
// against what's deployed. Local/Deployed are set when that side exists; Cmp is
// meaningful only when Drifted.
//
// LocalErr is set when the local source failed to compile (State is then
// driftInvalid). Local is still populated with the partial descriptor that needs
// no compile (query, engine version, track-emitted-streams); emit is unknown, so
// Cmp's emit dimension and the hash verdict are not meaningful.
type comparison struct {
	Name     string
	State    driftState
	Cmp      deploy.Comparison
	Local    *deploy.Descriptor
	Deployed *deploy.Descriptor
	LocalErr error
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
		return comparison{Name: name, State: driftUntracked, Deployed: &deployed}, nil
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
		if !notDeployed {
			deployed := deployedDef.Descriptor()
			c.Deployed = &deployed
			c.Cmp = deploy.Compare(partial, deployed)
		}
		return c, nil
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
	return comparison{Name: name, State: state, Cmp: cmp, Local: &local, Deployed: &deployed}, nil
}
