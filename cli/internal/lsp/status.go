package lsp

import (
	"context"
	"errors"
	"maps"
	"path/filepath"
	"strings"
	"sync"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/target"
)

// envStatus is the fetched deploy status for one (config, env): the
// per-projection drift entries and the resolved target, or a marker that the
// env needs sign-in / the fetch failed. Exactly one of the three shapes is set
// per fetch - populated Entries, Unauthenticated, or Err.
type envStatus struct {
	Entries    []drift.StatusEntry
	Target     string
	Production bool
	// Unauthenticated is set when the dial returned an auth-required error: the
	// env is reachable but the user needs to sign in. Drives the sign-in
	// affordance rather than a generic failure.
	Unauthenticated bool
	// Err is any other fetch failure (transport, invalid config, a projection
	// read). Kept so the surface can degrade visibly rather than silently.
	Err error
}

// statusFetchFunc fetches one env's status. Injected onto the Server so the
// cache / single-flight orchestration is testable without a live KurrentDB.
type statusFetchFunc func(ctx context.Context, root string, cfg *config.Config, envName string) envStatus

// statusCache holds fetched status keyed by config URI then env name, with
// single-flight dedupe of concurrent fetches for the same (uri, env). All
// access is guarded by mu.
type statusCache struct {
	mu       sync.Mutex
	byURI    map[string]map[string]envStatus
	inflight map[string]struct{}
}

func newStatusCache() *statusCache {
	return &statusCache{
		byURI:    map[string]map[string]envStatus{},
		inflight: map[string]struct{}{},
	}
}

// inflightKey joins uri and env with a NUL, which can't appear in either, so
// distinct (uri, env) pairs never collide.
func inflightKey(uri, env string) string { return uri + "\x00" + env }

// begin marks (uri, env) as fetching. Returns false when a fetch is already in
// flight for that pair, so the caller skips a duplicate.
func (c *statusCache) begin(uri, env string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := inflightKey(uri, env)
	if _, ok := c.inflight[k]; ok {
		return false
	}
	c.inflight[k] = struct{}{}
	return true
}

// store records a completed fetch and clears the in-flight marker.
func (c *statusCache) store(uri, env string, st envStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.byURI[uri]
	if m == nil {
		m = map[string]envStatus{}
		c.byURI[uri] = m
	}
	m[env] = st
	delete(c.inflight, inflightKey(uri, env))
}

// release clears an in-flight marker without recording a result, for when a
// fetch couldn't be queued (Run wound down) and a later session should retry.
func (c *statusCache) release(uri, env string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.inflight, inflightKey(uri, env))
}

// get returns a copy of the cached env statuses for uri (nil if none). The copy
// keeps callers off the lock while they read.
func (c *statusCache) get(uri string) map[string]envStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.byURI[uri]
	if m == nil {
		return nil
	}
	out := make(map[string]envStatus, len(m))
	maps.Copy(out, m)
	return out
}

// drop clears any cached status and in-flight markers for uri, called when the
// document closes. A fetch still running for uri will re-record on completion;
// harmless, since no surface reads a closed document.
func (c *statusCache) drop(uri string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byURI, uri)
	for k := range c.inflight {
		if strings.HasPrefix(k, uri+"\x00") {
			delete(c.inflight, k)
		}
	}
}

// refreshStatus re-fetches every env's deploy status for the gaffer.toml at uri,
// updating the cache and asking the client to re-render as each env lands. Each
// env is fetched in its own wg-tracked goroutine bounded by runCtx; single-flight
// skips an env already being fetched. A strict config load failure (an invalid
// gaffer.toml) leaves the cache untouched - the loose parse already surfaces the
// problem as diagnostics, and there's no reachable target to read from.
func (s *Server) refreshStatus(uri string) {
	if !isGafferConfig(uri) {
		return
	}
	path := uriToPath(uri)
	if path == "" {
		return
	}
	cfg, err := config.Load(path)
	if err != nil {
		return
	}
	root := filepath.Dir(path)
	for _, env := range cfg.EnvNames() {
		if !s.statusCache.begin(uri, env) {
			continue
		}
		if !s.spawnWithCtx(func(ctx context.Context) {
			s.statusCache.store(uri, env, s.statusFetch(ctx, root, cfg, env))
			// Status landed; ask the client to re-request lenses so the env
			// surface re-renders with the fresh state.
			s.requestCodeLensRefresh()
		}) {
			// Run wound down before we could spawn - release the marker so a
			// later session isn't blocked from retrying this env.
			s.statusCache.release(uri, env)
		}
	}
}

// fetchEnvStatus is the default statusFetchFunc: dial one env, read every
// projection's drift + runtime state, and resolve the target. Dials fresh and
// closes per call (like the CLI and the MCP deploy tools) - a language server
// holding a live connection would just go stale between refreshes.
func fetchEnvStatus(ctx context.Context, root string, cfg *config.Config, envName string) envStatus {
	resolved, err := cfg.ResolveEnv(envName)
	if err != nil {
		return envStatus{Err: err}
	}
	client, _, err := engine.Connect(root, resolved)
	if err != nil {
		// Only a connect-time auth failure (a missing/expired token the dial
		// can't satisfy) classifies as needs-sign-in. A token that passes the
		// dial but is rejected at RPC time surfaces as a generic Err - no
		// sign-in affordance - matching the MCP deploy tools.
		var authErr *target.AuthRequiredError
		if errors.As(err, &authErr) {
			return envStatus{Unauthenticated: true}
		}
		return envStatus{Err: err}
	}
	defer func() { _ = client.Close() }()
	r := remote.New(client)

	// Management calls block until their deadline if the projections subsystem
	// is still starting, so bound the reads rather than hang the fetch.
	rctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()
	entries, err := drift.StatusAll(rctx, r, cfg, root)
	if err != nil {
		return envStatus{Err: err}
	}
	// OperateTarget gets the parent ctx, not rctx: it applies its own
	// RPCTimeout, and passing the already-consumed rctx would starve its
	// $server-info read after StatusAll ate the budget (falling back to the
	// env name / opt-in). Matches the MCP deploy tools.
	target, prod := r.OperateTarget(ctx, resolved, deploy.RPCTimeout)
	return envStatus{Entries: entries, Target: target, Production: prod}
}
