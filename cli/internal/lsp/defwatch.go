package lsp

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// Definition-watch reconnect backoff: how long to wait before re-dialing after a
// dial failure or a dropped subscription, doubling up to the cap. A dropped
// watch means a missed deploy until it reconnects, so the cap stays modest.
// watchHealthyDuration is how long a connection must stay up before a later drop
// resets the backoff: a connection that drops sooner is treated as fast-failing
// and keeps backing off, so a bad-permission or flapping env can't spin.
const (
	watchBackoffStart    = 1 * time.Second
	watchBackoffMax      = 30 * time.Second
	watchHealthyDuration = 10 * time.Second
	// maxWatchConns caps the total held subscription connections across all open
	// configs, a backstop against a pathological many-configs-open case. Past it,
	// an env gets no watch (its drift falls back to poll-driven, stale until the
	// next save); connections stay bounded. Generous - a real workspace holds a
	// handful.
	maxWatchConns = 24
)

// envConn is one env's live subscription connection, published by its watch
// goroutine so the fetch path can borrow it (see borrowConn) instead of dialing
// fresh. Set on a successful dial, cleared and closed on drop/teardown. All
// access is guarded by watchMu.
type envConn struct {
	client  *remote.Client
	authInv *engine.AuthInvalidation
	cleanup func()
}

// envWatchSpec is the immutable input to one env's watch goroutine. conn is the
// goroutine's own connection slot, also referenced by its watchEntry so the
// fetch path can borrow it; a stale goroutine writes its detached slot, which no
// borrower reads.
type envWatchSpec struct {
	uri, env, root string
	resolved       config.ResolvedEnv
	projections    []string
	conn           *envConn
}

// envWatchFunc dials and holds one env's connection, subscribed to its
// projections' definition streams, until ctx is cancelled. The default is
// Server.runEnvWatch; tests inject a fake to exercise reconcileWatches without a
// live KurrentDB.
type envWatchFunc func(ctx context.Context, spec envWatchSpec)

// watchEntry is one live per-env definition watch: the cancel that stops it, the
// inputs it was started with (so reconcileWatches can tell when a config edit
// made it stale and restart it), and the connection slot borrowers read.
type watchEntry struct {
	cancel      context.CancelFunc
	connection  string
	projections []string // sorted; the streams this watch subscribes to
	conn        *envConn
}

// reconcileWatches brings the live definition-stream subscriptions for a
// gaffer.toml in line with its current parse. For each configured env it holds
// one connection subscribed to every projection's $projections-<name> stream;
// when any fires (a deploy or lifecycle write landed server-side) the env's
// drift verdict is recomputed and the lenses refresh, so the surface tracks
// out-of-editor deploys without polling drift. The poll borrows the same
// connection for its runtime reads.
//
// Called after a gaffer.toml parse updates (open/edit) and on close (where the
// parse is gone, so every watch for the URI is torn down). Envs added start a
// watch, envs removed stop theirs, and an env whose connection or projection set
// changed is restarted.
func (s *Server) reconcileWatches(uri string) {
	desired := s.desiredWatches(uri)

	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	// desiredWatches read the buffer before we took the lock, so a didClose racing
	// this reconcile could have run stopWatches in between. Re-check under the lock:
	// if the config closed, drop desired so the stop loop tears down anything this
	// reconcile would otherwise repopulate (and stopWatches, serialized on watchMu,
	// clears whatever we start if the close lands mid-loop).
	if _, open := s.docs.Get(uri); !open {
		desired = nil
	}
	prefix := uriKeyPrefix(uri)
	// Stop watches that are gone or stale.
	for key, entry := range s.watches {
		env, ok := strings.CutPrefix(key, prefix)
		if !ok {
			continue
		}
		d, want := desired[env]
		if !want || d.connection != entry.connection || !slices.Equal(d.projections, entry.projections) {
			entry.cancel()
			delete(s.watches, key)
		}
	}
	// Start watches that are missing.
	for env, d := range desired {
		key := inflightKey(uri, env)
		if _, ok := s.watches[key]; ok {
			continue
		}
		if len(s.watches) >= maxWatchConns {
			if !s.watchCapWarned {
				s.watchCapWarned = true
				log.Printf("lsp: definition-watch connection cap (%d) reached; further envs poll for drift instead", maxWatchConns)
			}
			continue
		}
		_, runCtx := s.snapshotRunState()
		if runCtx == nil {
			return // Run wound down; nothing to start
		}
		ctx, cancel := context.WithCancel(runCtx)
		spec := envWatchSpec{uri: uri, env: env, root: d.root, resolved: d.resolved, projections: d.projections, conn: &envConn{}}
		started := s.spawn(func() {
			defer cancel()
			s.watchRun(ctx, spec)
		})
		if !started {
			cancel()
			return
		}
		s.watches[key] = &watchEntry{cancel: cancel, connection: d.connection, projections: d.projections, conn: spec.conn}
	}
}

// desiredEnvWatch is what one env's watch should subscribe to.
type desiredEnvWatch struct {
	root        string
	resolved    config.ResolvedEnv
	connection  string
	projections []string // sorted, non-empty names
}

// desiredWatches computes the intended per-env watches for a gaffer.toml from its
// in-memory buffer, via the same load the fetch path uses. Returns empty when the
// surface is off, the URI isn't a tracked config, or the buffer doesn't parse -
// so reconcileWatches tears everything down in those cases.
func (s *Server) desiredWatches(uri string) map[string]desiredEnvWatch {
	cfg, root, load := s.loadStatusConfig(uri)
	if load != loadOK {
		return nil
	}
	projections := make([]string, 0, len(cfg.Projection))
	for i := range cfg.Projection {
		if name := cfg.Projection[i].Name; name != "" {
			projections = append(projections, name)
		}
	}
	if len(projections) == 0 {
		return nil // nothing to watch; don't hold connections for an empty config
	}
	slices.Sort(projections)
	out := make(map[string]desiredEnvWatch)
	for _, env := range cfg.EnvNames() {
		resolved, err := cfg.ResolveEnv(env)
		if err != nil {
			continue // can't dial an env we can't resolve; the fetch surfaces the error
		}
		out[env] = desiredEnvWatch{
			root:        root,
			resolved:    resolved,
			connection:  resolved.Connection,
			projections: projections,
		}
	}
	return out
}

// stopWatches cancels every watch for a URI. Called when a config closes.
func (s *Server) stopWatches(uri string) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	prefix := uriKeyPrefix(uri)
	for key, entry := range s.watches {
		if strings.HasPrefix(key, prefix) {
			entry.cancel()
			delete(s.watches, key)
		}
	}
}

// borrowConn returns the env's live subscription connection for a read, or
// ok=false when none is up (no watch, or the watch is between reconnects), in
// which case the caller dials a fresh one. The returned release is a no-op: the
// watch owns the connection's lifecycle, a borrower never closes it.
//
// A borrower uses the client after this returns and unlocks. If the watch drops
// and clearEnvConn closes the client mid-read, the borrower's RPC errors (a
// transient "status unavailable") and the next refresh recovers - the gRPC
// client tolerates a close racing an in-flight call, so it's an error, never a
// panic. Accepted rather than refcounted: a reconnect is rare and self-healing.
func (s *Server) borrowConn(uri, env string) (borrowedConn, bool) {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	e := s.watches[inflightKey(uri, env)]
	if e == nil || e.conn == nil || e.conn.client == nil {
		return borrowedConn{}, false
	}
	return borrowedConn{client: e.conn.client, authInv: e.conn.authInv, release: func() {}}, true
}

// setEnvConn publishes a watch's freshly-dialed connection so borrowers can use
// it. clearEnvConn unpublishes and closes it, outside the lock so a slow Close
// doesn't stall borrowers.
func (s *Server) setEnvConn(c *envConn, client *remote.Client, authInv *engine.AuthInvalidation, cleanup func()) {
	s.watchMu.Lock()
	c.client, c.authInv, c.cleanup = client, authInv, cleanup
	s.watchMu.Unlock()
}

func (s *Server) clearEnvConn(c *envConn) {
	s.watchMu.Lock()
	cleanup := c.cleanup
	c.client, c.authInv, c.cleanup = nil, nil, nil
	s.watchMu.Unlock()
	if cleanup != nil {
		cleanup()
	}
}

// runEnvWatch holds one connection to an env, publishes it for the fetch path to
// borrow, and subscribes to every projection's definition stream on it,
// recomputing the env's drift when any fires. It dials, syncs current state,
// watches until a drop or ctx cancel, then reconnects with backoff. Returns only
// when ctx is cancelled (the watch was stopped or Run is tearing down). It is the
// default s.watchRun; tests inject a fake.
//
// A panic guard keeps a crash deep in the subscription receive path from taking
// down the whole language server (mirroring the fetch path's safeFetch),
// scrubbing the connection out of the log.
func (s *Server) runEnvWatch(ctx context.Context, spec envWatchSpec) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("lsp: definition watch for env %q panicked: %s", spec.env, scrubConnection(fmt.Sprint(r), spec.root, spec.resolved))
		}
	}()
	watchLoop(ctx, func(ctx context.Context) (reconnect, healthy bool) {
		r, authInv, cleanup, err := remote.DialWithAuth(spec.root, spec.resolved)
		if err != nil {
			// Can't connect (unreachable, or needs sign-in): reconnect later. The
			// fetch path surfaces the sign-in/error state meanwhile.
			return true, false
		}
		s.setEnvConn(spec.conn, r, authInv, cleanup)
		defer s.clearEnvConn(spec.conn)
		// Subscribing starts from the stream's end, so re-sync current drift now
		// to close the gap between the last read and this subscription.
		s.onDefinitionChanged(spec.uri, spec.env)
		start := s.statusCache.now()
		onUpdate := func() { s.onDefinitionChanged(spec.uri, spec.env) }
		dropped := watchEnvStreams(ctx, r.WatchDefinition, spec.projections, onUpdate)
		return dropped, s.statusCache.now().Sub(start) >= watchHealthyDuration
	}, sleepCtx)
}

// watchLoop drives an env watch, reconnecting on drop with exponential backoff.
// once dials, watches, and reports whether to reconnect (a drop, vs ctx cancel)
// and whether the connection stayed healthy long enough to treat the drop as
// transient. Backoff resets only on a healthy connection, so an env that keeps
// failing fast (bad permissions, a flapping stream) backs off to the cap instead
// of hammering the cluster once per interval. Injectable sleep so the backoff
// schedule is testable without real time.
func watchLoop(ctx context.Context, once watchOnce, sleep func(context.Context, time.Duration) bool) {
	backoff := watchBackoffStart
	for ctx.Err() == nil {
		reconnect, healthy := once(ctx)
		if !reconnect {
			return // ctx cancelled: the watch was stopped
		}
		if healthy {
			backoff = watchBackoffStart
		}
		if !sleep(ctx, backoff) {
			return
		}
		if !healthy {
			backoff = nextBackoff(backoff)
		}
	}
}

// watchOnce dials and watches an env's definition streams once. reconnect is
// true on a drop (the caller should reconnect), false on ctx cancel (stop).
// healthy is true if the connection lasted long enough to reset backoff.
type watchOnce func(ctx context.Context) (reconnect, healthy bool)

// watchStreamFunc subscribes to one projection's definition stream, calling
// onUpdate per change until ctx is cancelled or it drops. remote.Client's
// WatchDefinition satisfies it; tests pass a fake.
type watchStreamFunc func(ctx context.Context, name string, onUpdate func()) error

// watchEnvStreams runs one watch per projection on the shared connection until
// the first drops or ctx is cancelled, then stops the rest. Returns true if a
// subscription dropped (reconnect), false on ctx cancel.
func watchEnvStreams(ctx context.Context, watch watchStreamFunc, projections []string, onUpdate func()) bool {
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	var wg sync.WaitGroup
	firstExit := make(chan struct{}, 1)
	for _, name := range projections {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_ = watch(subCtx, name, onUpdate)
			select {
			case firstExit <- struct{}{}:
			default:
			}
		}(name)
	}
	select {
	case <-firstExit:
	case <-ctx.Done():
	}
	subCancel()
	wg.Wait()
	return ctx.Err() == nil
}

// onDefinitionChanged fires when an env's deployed definition may have moved (a
// subscription event, or a reconnect syncing from the stream's end). It flags
// that env for a drift recompute - a server-side change has no local signal, so
// only this forces it - then refreshes. The refresh is not driftChanged (no
// local edit): the flagged env recomputes drift, its siblings just refresh
// runtime.
func (s *Server) onDefinitionChanged(uri, env string) {
	s.statusCache.markForceFull(uri, env)
	s.refreshStatus(uri, false)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > watchBackoffMax {
		return watchBackoffMax
	}
	return d
}
