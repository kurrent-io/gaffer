// Package updatecheck prints a one-line "upgrade available" hint at
// startup when a newer @kurrent/gaffer is on npm. Notification only -
// no self-install.
//
// Shape:
//
//   - Synchronous: read update-check.json at startup. If the cached
//     latest_version > current, print to stderr immediately. Free.
//   - Async: if the cache is stale (>24h, or current-version drift),
//     spawn a background goroutine that does the HTTP GET and writes
//     the cache. It does NOT print; it only populates the cache for
//     the next run. Mirrors update-notifier / turbo / wrangler.
//   - Best-effort: network failures, parse errors, malformed cache,
//     cache-dir lookup failures - all silent. Update notification
//     must never block startup or leak errors.
//
// Caller responsibilities (deliberately outside the package so a
// future `gaffer update --check` can bypass them):
//
//   - TTY gating. The package will happily print into a pipe; the
//     caller decides whether stderr is interactive enough to warrant
//     the notice.
//   - --no-update-check flag parsing. Pass the resolved bool to Start.
//
// Process-exit Flush mirrors telemetry.Client.Flush: bounded by a
// caller-supplied ctx so the refresh goroutine can't keep the
// process alive past its budget. main.runMain wires the deferred
// Flush with a 2s budget.
package updatecheck

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/envvar"
)

// EnvDisable suppresses the entire update-check pipeline (no print,
// no fetch). Set GAFFER_NO_UPDATE_CHECK=1 for CI / scripted runs
// where the notice would be noise. Read at Start time so test setups
// can flip it per-test via t.Setenv.
const EnvDisable = "GAFFER_NO_UPDATE_CHECK"

// Options configures a Client. Zero-valued fields fall back to
// sensible production defaults; tests inject stubs by populating the
// fields they care about.
type Options struct {
	// Current is the running gaffer version (cmd.Version). Empty
	// disables the client - we can't compute "newer than current"
	// without a baseline.
	Current string

	// Fetcher resolves npm's `latest` dist-tag. Nil falls back to
	// the default NpmFetcher. Production wiring constructs an
	// NpmFetcher with the gaffer-cli/<ver> user agent and passes it
	// here; tests inject stubs.
	Fetcher Fetcher

	// CacheDir overrides DefaultDir(). Empty falls back to
	// DefaultDir(); a resolve failure there disables the client
	// without surfacing the error - same posture as opt-out.
	CacheDir string

	// Stderr is where the notice line is written. Nil falls back to
	// os.Stderr. Tests inject a *bytes.Buffer.
	Stderr io.Writer

	// Now overrides time.Now for IsStale checks. Tests freeze it.
	Now func() time.Time

	// TTL overrides DefaultTTL for the cache freshness check. Zero
	// falls back to DefaultTTL.
	TTL time.Duration
}

// Client coordinates the synchronous notice print and the async
// background refresh. Construct once at process start, call Start
// from the root cobra PersistentPreRunE, and Flush at process exit.
//
// Safe for concurrent use - the mutex serialises wg.Add against the
// close-during-Flush transition exactly like telemetry.Client.
type Client struct {
	current     string
	fetcher     Fetcher
	cacheDir    string
	stderr      io.Writer
	now         func() time.Time
	ttl         time.Duration
	cacheDirErr error // set when DefaultDir resolution failed; disables the client silently

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup

	started sync.Once
}

// New constructs a Client. Always returns a non-nil Client even when
// the cache dir can't be resolved or Current is empty; Start checks
// these and no-ops, which keeps the call sites at main.go and root.go
// free of nil-checks.
func New(opts Options) *Client {
	c := &Client{
		current:  opts.Current,
		fetcher:  opts.Fetcher,
		cacheDir: opts.CacheDir,
		stderr:   opts.Stderr,
		now:      opts.Now,
		ttl:      opts.TTL,
	}
	if c.stderr == nil {
		c.stderr = os.Stderr
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.ttl == 0 {
		c.ttl = DefaultTTL
	}
	if c.fetcher == nil {
		c.fetcher = NpmFetcher{}
	}
	if c.cacheDir == "" {
		dir, err := DefaultDir()
		if err != nil {
			c.cacheDirErr = err
		} else {
			c.cacheDir = dir
		}
	}
	return c
}

// Start prints the notice from cache if applicable and spawns the
// background refresh if the cache is stale. disableFlag wires
// --no-update-check; the GAFFER_NO_UPDATE_CHECK env var is read
// here. TTY gating is the caller's responsibility - by the time
// Start runs, the caller has already decided this is an interactive
// invocation worth notifying.
//
// Nil-safe: Start on a nil Client is a no-op so callers don't need
// to guard. Matches telemetry.Client's nil receiver convention.
func (c *Client) Start(disableFlag bool) {
	if c == nil {
		return
	}
	c.started.Do(func() { c.startOnce(disableFlag) })
}

func (c *Client) startOnce(disableFlag bool) {
	if disableFlag || envvar.IsTruthy(os.Getenv(EnvDisable)) {
		return
	}
	if c.current == "" || c.cacheDirErr != nil {
		// No baseline to compare or no cache to persist to. Disables
		// the client silently per "best-effort" posture.
		return
	}
	cache, _ := LoadCache(c.cacheDir)
	if cache.LatestVersion != "" && IsNewer(cache.LatestVersion, c.current) {
		_, _ = fmt.Fprintln(c.stderr, renderNotice(c.stderr, c.current, cache.LatestVersion))
	}
	if cache.IsStale(c.now(), c.current, c.ttl) {
		c.spawnRefresh()
	}
}

// spawnRefresh kicks off the background goroutine that fetches the
// latest version from npm and writes the cache. The goroutine
// honours the WaitGroup so Flush can drain it at process exit.
//
// The HTTP timeout lives on the Fetcher (NpmFetcher's http.Client.
// Timeout). Flush bounds how long we wait for the goroutine to
// finish - it does NOT bound the goroutine itself, so the fetcher's
// own deadline is what stops a slow connect from leaking the
// goroutine past process exit.
//
// A panic in the goroutine is recovered and dropped - the whole
// point of best-effort is that a buggy fetcher or transport must
// not crash the host process. Errors are swallowed for the same
// reason; the next invocation will refetch.
func (c *Client) spawnRefresh() {
	if !c.tryAddInflight() {
		return
	}
	go func() {
		defer c.wg.Done()
		defer func() { _ = recover() }()

		latest, err := c.fetcher.Latest(context.Background())
		if err != nil {
			return
		}
		_ = SaveCache(c.cacheDir, Cache{
			CheckedAt:          c.now(),
			CheckedWithVersion: c.current,
			LatestVersion:      latest,
		})
	}()
}

// tryAddInflight mirrors telemetry.Client.tryAddInflight: the Add
// happens inside the same mutex Flush uses to flip closed, so a
// successful Add is guaranteed to be observed by Flush's wg.Wait.
// Returns false when Flush has already closed the client - the
// caller drops the work.
func (c *Client) tryAddInflight() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.wg.Add(1)
	return true
}

// Flush waits for the in-flight refresh goroutine (if any) to
// complete or for ctx to expire. Returns ctx.Err() on timeout, nil
// on clean drain. Idempotent.
//
// Bound the wait at the call site with context.WithTimeout - a
// stalled refresh must not keep the process alive. main.runMain
// uses 2s, shorter than telemetry's 5s because update-check is
// less important to drain.
//
// The wg.Wait watcher goroutine spawned below outlives Flush on a
// ctx-timeout return: the OS reaps it on process exit. Same shape
// as telemetry.Client.Flush; the alternative (cancelling the
// underlying work) would mean threading another context into every
// refresh, which is more machinery than the trade-off warrants.
//
// Nil-safe: Flush on a nil Client returns nil immediately.
func (c *Client) Flush(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()

	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("updatecheck flush: %w", ctx.Err())
	}
}

// UpdateAvailable returns the cached latest version if it is strictly
// newer than Current. Returns "" otherwise (no cache, cache not newer,
// no current baseline, or cache dir unresolved). Disk read only - never
// triggers a network fetch, even when the cache is stale. Background
// refresh is the job of Start.
//
// Used by `gaffer manifest` to expose the update signal to editor
// wrappers without re-checking the registry. The --no-update-check
// flag and GAFFER_NO_UPDATE_CHECK env var deliberately do NOT gate
// this: they suppress the printed notice and the network refresh, not
// the consumption of an already-collected signal.
//
// Nil-safe: returns "" on a nil receiver.
func (c *Client) UpdateAvailable() string {
	if c == nil || c.current == "" || c.cacheDirErr != nil {
		return ""
	}
	cache, _ := LoadCache(c.cacheDir)
	if cache.LatestVersion != "" && IsNewer(cache.LatestVersion, c.current) {
		return cache.LatestVersion
	}
	return ""
}

// ctxKey scopes the context lookup so callers can't collide.
type ctxKey struct{}

// WithClient stores c on ctx so PersistentPreRunE can retrieve it
// without threading an extra parameter through cobra. Matches the
// telemetry.WithClient convention.
func WithClient(ctx context.Context, c *Client) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// FromCtx returns the Client stashed by WithClient, or nil if none
// was set. nil-safe Start/Flush mean callers can use the result
// without a nil-check.
func FromCtx(ctx context.Context) *Client {
	c, _ := ctx.Value(ctxKey{}).(*Client)
	return c
}
