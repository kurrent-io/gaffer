package cmd

import (
	"errors"
	"fmt"

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

// liveConn is a connected live target: the loaded project (cfg/root), the
// resolved env it connected to, the remote client, and the cleanup to defer.
// Returned as a struct rather than a six-value tuple so callers take the
// fields they need without positional discards.
type liveConn struct {
	cfg     *config.Config
	root    string
	env     config.ResolvedEnv
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
	r, resolved, cleanup, err := connectResolved(cfg, root, connection, env)
	if err != nil {
		return liveConn{}, err
	}
	return liveConn{cfg: cfg, root: root, env: resolved, r: r, cleanup: cleanup}, nil
}

// connectResolved resolves the live env from explicit flags against an
// already-loaded config and connects, returning the resolved env alongside the
// client so callers don't re-derive it. Split from connectEnv so deploy can
// load the config, run preflight locally, then connect only once the
// projections are known to be deployable.
func connectResolved(cfg *config.Config, root, connection, env string) (r *remote.Client, resolved config.ResolvedEnv, cleanup func(), err error) {
	resolved, err = resolveLiveEnv(connection, env, cfg)
	if err != nil {
		return nil, config.ResolvedEnv{}, nil, err
	}
	if resolved.Connection == "" {
		return nil, config.ResolvedEnv{}, nil, errors.New("no environment: mark a default [env.<name>], pass --env, or pass --connection")
	}
	client, _, err := engine.Connect(root, resolved)
	if err != nil {
		return nil, config.ResolvedEnv{}, nil, err
	}
	return remote.New(client), resolved, func() { _ = client.Close() }, nil
}

// refuseNoValidateOnProd is the single source of the production --no-validate
// guardrail, shared by deploy and recreate so the message and exit code stay in
// lockstep. --no-validate skips validation (deploy's plan validation, recreate's
// pre-delete compile gate); production never accepts it, so a prod deploy/recreate
// always validates first. exitWith(3) is the guardrail-refusal code, and (unlike a
// silent wrap) lets fang print the reason instead of swallowing it. verb names the
// command ("Deploy"/"Recreate") and subject reads in "so <subject> are/is
// validated first".
func refuseNoValidateOnProd(verb, subject, target string) error {
	return exitWith(3, fmt.Errorf("--no-validate is not allowed on production %s: it skips validation. %s without it so %s validated first",
		targetDesc(target), verb, subject))
}
