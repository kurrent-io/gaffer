package lsp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/target"
)

// pollThrottleWindow is the minimum gap between runtime-only fetches for one
// (config, env). A runtime-only refresh (a poll tick, or the sign-in nudge's
// already-authenticated envs) is skipped when a fetch for that pair started
// within this window, so a poll landing right behind another refresh - a
// sign-in completing, a save - doesn't fire a redundant read. Kept comfortably
// below the client's poll interval (see the vscode poller) so scheduled polls
// always pass; only sub-window bursts are absorbed. A fetch already in flight
// is handled by single-flight (begin); this covers the just-completed case
// single-flight can't see. Full fetches (local changed, first fetch, a
// previously-failed env) bypass it - they must always run.
const pollThrottleWindow = 3 * time.Second

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

// statusFetchFunc recomputes one env's full status (drift verdict + runtime).
// Injected onto the Server so the cache / single-flight orchestration is
// testable without a live KurrentDB. uri identifies the config so the fetch can
// borrow that env's live subscription connection instead of dialing fresh.
type statusFetchFunc func(ctx context.Context, root string, cfg *config.Config, uri, envName string) envStatus

// statusCache holds fetched status keyed by config URI then env name, with
// single-flight dedupe of concurrent fetches for the same (uri, env). A per-URI
// generation counter, bumped on every drop, lets a fetch that was already in
// flight when its document closed (or its config went invalid) discard its
// late result instead of resurrecting the cache. All access is guarded by mu.
//
// lastStart records when the most recent fetch for a (uri, env) began, so
// pollThrottleWindow can drop redundant runtime-only polls. now is the clock,
// overridable in tests.
type statusCache struct {
	mu        sync.Mutex
	byURI     map[string]map[string]envStatus
	inflight  map[string]struct{}
	gen       map[string]uint64
	lastStart map[string]time.Time
	// forceFull marks a (uri, env) whose deployed definition changed server-side
	// (a subscription reported a $ProjectionUpdated). A local edit arrives as a
	// driftChanged refresh, but a server-side deploy has no local signal, so this
	// flag is what forces the drift recompute. Held separately from the cached
	// verdict so an in-flight runtime-only store can't clobber it; consumed when a
	// drift recompute begins.
	forceFull map[string]bool
	now       func() time.Time
}

func newStatusCache() *statusCache {
	return &statusCache{
		byURI:     map[string]map[string]envStatus{},
		inflight:  map[string]struct{}{},
		gen:       map[string]uint64{},
		lastStart: map[string]time.Time{},
		forceFull: map[string]bool{},
		now:       time.Now,
	}
}

// uriKeyPrefix is the NUL-terminated prefix every per-env key for a URI shares,
// so a prefix scan selects exactly that URI's entries. NUL can't appear in a URI
// or a TOML env key, so keys never collide across URIs or envs.
func uriKeyPrefix(uri string) string { return uri + "\x00" }

// inflightKey is the per-(uri, env) key used across the status cache and the
// definition-watch map.
func inflightKey(uri, env string) string { return uriKeyPrefix(uri) + env }

// begin marks (uri, env) as fetching and returns the URI's current generation
// to hand back to store. The bool is false when a fetch is already in flight for
// that pair, so the caller skips a duplicate.
func (c *statusCache) begin(uri, env string) (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	k := inflightKey(uri, env)
	if _, ok := c.inflight[k]; ok {
		return 0, false
	}
	c.inflight[k] = struct{}{}
	c.lastStart[k] = c.now()
	return c.gen[uri], true
}

// recentlyFetched reports whether a fetch for (uri, env) began within window of
// now, so a runtime-only refresh can be throttled (see pollThrottleWindow).
func (c *statusCache) recentlyFetched(uri, env string, window time.Duration) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	last, ok := c.lastStart[inflightKey(uri, env)]
	if !ok {
		return false
	}
	return c.now().Sub(last) < window
}

// markForceFull records that (uri, env)'s deployed definition changed
// server-side, so the next refresh recomputes its drift verdict in full. Sticky
// until consumed by a full fetch (see forcedFull / consumeForceFull), so a
// concurrent runtime-only store can't drop the signal and a raced deploy is at
// worst reflected on the next poll rather than lost.
func (c *statusCache) markForceFull(uri, env string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.forceFull[inflightKey(uri, env)] = true
}

// forcedFull reports whether (uri, env) is flagged for a full recompute.
func (c *statusCache) forcedFull(uri, env string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.forceFull[inflightKey(uri, env)]
}

// consumeForceFull clears the flag, called when a full fetch for (uri, env)
// begins. A deploy landing after this re-sets it, so it isn't lost.
func (c *statusCache) consumeForceFull(uri, env string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.forceFull, inflightKey(uri, env))
}

// store records a completed fetch and clears the in-flight marker. The result is
// dropped (cache not written) when gen no longer matches the URI's generation -
// the document was closed, or its config reloaded, while this fetch was running,
// so its data is stale and must not resurrect the cache.
func (c *statusCache) store(uri, env string, gen uint64, st envStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen[uri] {
		// Stale (the doc closed or its config reloaded since this fetch began):
		// discard, and leave the in-flight marker alone - it was cleared by the
		// drop that bumped the generation, and any current-generation fetch owns
		// its own marker, which this stale store must not clear.
		return
	}
	delete(c.inflight, inflightKey(uri, env))
	m := c.byURI[uri]
	if m == nil {
		m = map[string]envStatus{}
		c.byURI[uri] = m
	}
	m[env] = st
}

// inFlightEnvs returns the set of env names currently being fetched for uri, so
// the surface can show a loading placeholder for them.
func (c *statusCache) inFlightEnvs(uri string) map[string]bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	prefix := uriKeyPrefix(uri)
	out := map[string]bool{}
	for k := range c.inflight {
		if env, ok := strings.CutPrefix(k, prefix); ok {
			out[env] = true
		}
	}
	return out
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

// drop clears any cached status and in-flight markers for uri, and bumps the
// URI's generation so a fetch still running for it discards its result instead
// of repopulating the cache. Called when the document closes or its config
// reload fails.
func (c *statusCache) drop(uri string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen[uri]++
	delete(c.byURI, uri)
	prefix := uriKeyPrefix(uri)
	for k := range c.inflight {
		if strings.HasPrefix(k, prefix) {
			delete(c.inflight, k)
		}
	}
	for k := range c.lastStart {
		if strings.HasPrefix(k, prefix) {
			delete(c.lastStart, k)
		}
	}
	for k := range c.forceFull {
		if strings.HasPrefix(k, prefix) {
			delete(c.forceFull, k)
		}
	}
}

// configLoad is the outcome of loadStatusConfig.
type configLoad int

const (
	loadSkip     configLoad = iota // surface off, not a config, or no tracked buffer
	loadParseErr                   // the buffer doesn't parse
	loadOK
)

// loadStatusConfig parses the in-memory gaffer.toml buffer for the status
// surface, returning the config and its project root. It is the shared guard for
// the fetch path and the definition-watch reconcile: both need the same
// opt-in / is-config / has-buffer / parses checks off the same buffer, and
// coupling them here keeps the two from drifting. The three-way result lets each
// caller handle a parse failure differently (the fetch drops cached status; the
// watch reconcile tears its subscriptions down).
func (s *Server) loadStatusConfig(uri string) (*config.Config, string, configLoad) {
	if !s.statusLensEnabled() || !isGafferConfig(uri) {
		return nil, "", loadSkip
	}
	path := uriToPath(uri)
	if path == "" {
		return nil, "", loadSkip
	}
	state, ok := s.docs.Get(uri)
	if !ok {
		return nil, "", loadSkip
	}
	cfg, err := config.Parse([]byte(state.Content))
	if err != nil {
		return nil, "", loadParseErr
	}
	return cfg, filepath.Dir(path), loadOK
}

// refreshStatus re-fetches the gaffer.toml's deploy status for every env,
// updating the cache and asking the client to re-render as each env lands. Each
// env is fetched in its own wg-tracked goroutine bounded by runCtx; single-flight
// skips an env already being fetched.
//
// driftChanged is what the caller knows that the server can't infer: whether a
// *local* drift input changed (a save or a watched-file edit). It doesn't decide
// the tier on its own - the server still picks per env:
//   - recompute the drift verdict (fetchEnvStatus: recompile local, read the
//     deployed definition + ledger, compare, and read runtime) when driftChanged
//     is set, a subscription reported a server-side deploy (forceFull), or the env
//     has no reusable verdict yet (never fetched, or the last fetch needed
//     sign-in / failed).
//   - refresh runtime only (fetchEnvRuntime: one List reattached onto the cached
//     verdict) otherwise - the poll's cheap path, throttled by pollThrottleWindow.
//
// The config is parsed from the client's in-memory buffer, so it reflects what
// the user is looking at. A buffer that doesn't parse drops cached status for the
// URI so the surface clears rather than showing stale data (the loose parse
// already surfaces the problem as diagnostics).
func (s *Server) refreshStatus(uri string, driftChanged bool) {
	cfg, root, load := s.loadStatusConfig(uri)
	switch load {
	case loadSkip:
		// Surface off, not a config, or no tracked buffer - nothing to do.
		return
	case loadParseErr:
		// A buffer edited into an invalid state: drop cached status so the surface
		// clears rather than showing stale data (the loose parse already surfaces
		// the problem as diagnostics).
		s.statusCache.drop(uri)
		s.requestCodeLensRefresh()
		return
	}

	cached := s.statusCache.get(uri)

	type job struct {
		env         string
		gen         uint64
		runtimeOnly bool
		cached      envStatus
	}
	var jobs []job
	for _, env := range cfg.EnvNames() {
		st, ok := cached[env]
		// A cached verdict is reusable when the last fetch produced one: not
		// sign-in-needed, not errored. Recompute drift when the caller signalled a
		// local change (driftChanged), a subscription reported a deploy (forceFull),
		// or there's no verdict to reuse yet; otherwise refresh only live runtime.
		reusable := ok && !st.Unauthenticated && st.Err == nil
		recompute := driftChanged || !reusable || s.statusCache.forcedFull(uri, env)
		if !recompute && s.statusCache.recentlyFetched(uri, env, pollThrottleWindow) {
			// A fetch for this env began within the throttle window; skip this
			// redundant runtime-only poll rather than fire another read.
			continue
		}
		gen, began := s.statusCache.begin(uri, env)
		if !began {
			// A fetch is already in flight. If we needed a drift recompute, that
			// in-flight fetch might be a runtime-only poll that won't reflect the
			// change, so flag the env: the next refresh upgrades to a recompute
			// rather than losing the local edit (symmetric with the deploy path).
			if recompute {
				s.statusCache.markForceFull(uri, env)
			}
			continue
		}
		if recompute {
			// This fetch recomputes drift against current state, so clear any pending
			// force flag now. A change landing after this re-sets it, so it isn't lost.
			s.statusCache.consumeForceFull(uri, env)
		}
		jobs = append(jobs, job{env: env, gen: gen, runtimeOnly: !recompute, cached: st})
	}
	if len(jobs) == 0 {
		return
	}

	// Coalesce the per-env fetches into a single codeLens refresh: the last
	// fetch to settle (or fail to spawn) fires it, so an N-env config triggers
	// one workspace re-request rather than N.
	var remaining atomic.Int64
	remaining.Store(int64(len(jobs)))
	fireWhenDone := func() {
		if remaining.Add(-1) == 0 {
			s.requestCodeLensRefresh()
		}
	}
	for _, j := range jobs {
		if !s.spawnWithCtx(func(ctx context.Context) {
			var st envStatus
			if j.runtimeOnly {
				st = s.safeFetch(ctx, root, cfg, j.env, func(ctx context.Context) envStatus {
					return s.runtimeFetch(ctx, root, cfg, uri, j.env, j.cached)
				})
			} else {
				st = s.safeFetch(ctx, root, cfg, j.env, func(ctx context.Context) envStatus {
					return s.statusFetch(ctx, root, cfg, uri, j.env)
				})
			}
			s.statusCache.store(uri, j.env, j.gen, st)
			fireWhenDone()
		}) {
			// Run wound down before we could spawn - release the marker so a
			// later session isn't blocked from retrying this env, and still count
			// the job so the last one that did spawn fires the refresh.
			s.statusCache.release(uri, j.env)
			fireWhenDone()
		}
	}
}

// safeFetch runs fetch with a panic guard, so a crash deep in a dependency (e.g.
// a nil-deref in the KurrentDB client on an unready projection subsystem)
// surfaces as a "status unavailable" lens instead of taking down the whole
// language server via an unrecovered goroutine panic. Shared by the full and
// runtime-only fetch paths.
func (s *Server) safeFetch(ctx context.Context, root string, cfg *config.Config, env string, fetch func(context.Context) envStatus) (st envStatus) {
	defer func() {
		if r := recover(); r != nil {
			// Scrub the panic value against the env's connection before logging,
			// in case it embedded one. Scrub both the raw connection and the
			// ${VAR}-expanded form the dial actually used - a panic from deep in
			// the client carries the expanded string, not the toml literal.
			// Expansion alone rather than full Resolve: a target that refuses to
			// resolve (an unparseable connection under OAuth host binding) still
			// expands, so its secret still gets masked. Best-effort: an env that
			// won't even expand scrubs to a no-op.
			msg := fmt.Sprint(r)
			if resolved, rerr := cfg.ResolveEnv(env); rerr == nil {
				msg = scrubConnection(msg, root, resolved)
			}
			log.Printf("lsp: status fetch for env %q panicked: %s", env, msg)
			// Keep the error generic - it's surfaced in the lens tooltip, so it
			// must not carry the (unscrubbed) panic value.
			st = envStatus{Err: errors.New("status read failed unexpectedly")}
		}
	}()
	return fetch(ctx)
}

// scrubConnection masks an env's connection secret out of a message before it's
// logged. It scrubs both the raw connection and the ${VAR}-expanded form the
// dial actually used - a panic or error from deep in the client carries the
// expanded string, not the toml literal. Best-effort: an env that won't expand
// scrubs to a no-op. Shared by the fetch panic guard and the watch panic guard.
func scrubConnection(msg, root string, resolved config.ResolvedEnv) string {
	msg = target.ScrubConnection(msg, resolved.Connection)
	if conn, err := target.ExpandConnection(root, resolved); err == nil {
		msg = target.ScrubConnection(msg, conn)
	}
	return msg
}

// borrowedConn is a client for reading one env: either the env's live
// subscription connection (borrowed - release drops the borrow count so the
// watch can close it) or a fresh throwaway dial (release closes it). Either way
// the caller must release exactly once when done. authInv reports whether a read
// on the connection hit an auth rejection; it may be nil for a connection with
// no auth.
type borrowedConn struct {
	client  *remote.Client
	authInv *engine.AuthInvalidation
	release func()
}

// envClient returns a client for reading one env, preferring the env's live
// subscription connection (see borrowConn) and dialing a fresh one only when no
// watch connection is up. A returned error is the dial failure; callers classify
// AuthRequiredError as sign-in-needed.
func (s *Server) envClient(root string, cfg *config.Config, uri, env string) (borrowedConn, error) {
	if bc, ok := s.borrowConn(uri, env); ok {
		return bc, nil
	}
	resolved, err := cfg.ResolveEnv(env)
	if err != nil {
		return borrowedConn{}, err
	}
	r, authInv, cleanup, err := remote.DialWithAuth(root, resolved)
	if err != nil {
		return borrowedConn{}, err
	}
	return borrowedConn{client: r, authInv: authInv, release: cleanup}, nil
}

// dialErrStatus classifies a dial/connect failure: a missing or locked token
// that the dial can't satisfy needs sign-in (Unauthenticated); anything else is
// a generic Err.
func dialErrStatus(err error) envStatus {
	var authErr *target.AuthRequiredError
	if errors.As(err, &authErr) {
		return envStatus{Unauthenticated: true}
	}
	return envStatus{Err: err}
}

// fetchEnvStatus is the default statusFetchFunc: recompute one env's full status.
// It reads every projection's drift + runtime state and resolves the target, on
// the env's live subscription connection when one is up (else a fresh dial). A
// connect-time or read-time auth failure surfaces as sign-in-needed.
func (s *Server) fetchEnvStatus(ctx context.Context, root string, cfg *config.Config, uri, envName string) envStatus {
	bc, err := s.envClient(root, cfg, uri, envName)
	if err != nil {
		return dialErrStatus(err)
	}
	defer bc.release()

	// Management calls block until their deadline if the projections subsystem
	// is still starting, so bound the reads rather than hang the fetch.
	rctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()
	entries, err := drift.StatusAll(rctx, bc.client, cfg, root)
	// A stored OAuth token the IdP rejected (invalid_grant) trips the auth flag
	// on the read - the credential is dead, not merely unreachable, so surface
	// sign-in rather than a generic "unavailable". A generic RPC rejection (a
	// valid token lacking a role) leaves the flag untripped and stays a generic Err.
	//
	// authInv is shared with the watch and any other borrower of this connection,
	// and Tripped() is sticky: once any read on it hits invalid_grant, borrowers
	// report Unauthenticated until the watch reconnects with a fresh one. That's
	// correct (the credential is dead), if briefly pessimistic for a read that
	// itself succeeded.
	if bc.authInv != nil && bc.authInv.Tripped() {
		return envStatus{Unauthenticated: true}
	}
	if err != nil {
		return envStatus{Err: err}
	}
	resolved, err := cfg.ResolveEnv(envName)
	if err != nil {
		return envStatus{Err: err}
	}
	// OperateTarget gets the parent ctx, not rctx: it applies its own RPCTimeout,
	// and passing the already-consumed rctx would starve its $server-info read
	// after StatusAll ate the budget. Matches the MCP deploy tools.
	target, prod := bc.client.OperateTarget(ctx, resolved, deploy.RPCTimeout)
	return envStatus{Entries: entries, Target: target, Production: prod}
}

// statusRuntimeFunc refreshes one env's live runtime state, reusing the drift
// verdict from a prior full status read. Injected onto the Server so the cheap
// poll path is testable without a live KurrentDB, mirroring statusFetchFunc.
type statusRuntimeFunc func(ctx context.Context, root string, cfg *config.Config, uri, envName string, cached envStatus) envStatus

// fetchEnvRuntime is the default statusRuntimeFunc: the cheap poll path. It reads
// live runtime state with a single List (on the env's live connection when one
// is up) and reattaches it onto the cached comparison entries by name - no
// recompile, no per-projection definition/ledger reads, no $server-info. The
// drift verdict and resolved target carry over from the cached full read.
//
// A projection missing from the fresh List keeps its cached runtime rather than
// dropping to nil: a vanished projection is a server-side change, picked up when
// the drift verdict is recomputed. Dial/read failures degrade the env the same
// way a full read does, so a poll that hits an expired token flips the surface
// to "sign in" rather than showing stale live state.
func (s *Server) fetchEnvRuntime(ctx context.Context, root string, cfg *config.Config, uri, envName string, cached envStatus) envStatus {
	bc, err := s.envClient(root, cfg, uri, envName)
	if err != nil {
		return dialErrStatus(err)
	}
	defer bc.release()

	rctx, cancel := context.WithTimeout(ctx, deploy.RPCTimeout)
	defer cancel()
	live, err := bc.client.List(rctx)
	if bc.authInv != nil && bc.authInv.Tripped() {
		return envStatus{Unauthenticated: true}
	}
	if err != nil {
		return envStatus{Err: err}
	}
	return reattachRuntime(cached, live)
}

// reattachRuntime returns the cached env status with each entry's live runtime
// refreshed from live (matched by projection name), carrying the cached drift
// verdict, target, and production flag over unchanged. An entry whose projection
// is absent from live keeps its cached runtime (see fetchEnvRuntime). live never
// adds entries: a projection appearing on the server but not in the cached
// verdict is a server-side change picked up on the next full refresh.
func reattachRuntime(cached envStatus, live []remote.Status) envStatus {
	byName := make(map[string]remote.Status, len(live))
	for i := range live {
		byName[live[i].Name] = live[i]
	}
	entries := make([]drift.StatusEntry, len(cached.Entries))
	copy(entries, cached.Entries)
	for i := range entries {
		if rt, ok := byName[entries[i].Name]; ok {
			rt := rt
			entries[i].Runtime = &rt
		}
	}
	return envStatus{Entries: entries, Target: cached.Target, Production: cached.Production}
}
