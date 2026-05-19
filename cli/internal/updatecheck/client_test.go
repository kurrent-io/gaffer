package updatecheck

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubFetcher records calls and returns a canned response. Tests
// inject one rather than hitting the real registry.
type stubFetcher struct {
	mu      sync.Mutex
	latest  string
	err     error
	delay   time.Duration
	calls   int
	gotCtxs []context.Context
}

func (f *stubFetcher) Latest(ctx context.Context) (string, error) {
	f.mu.Lock()
	f.calls++
	f.gotCtxs = append(f.gotCtxs, ctx)
	f.mu.Unlock()
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return f.latest, f.err
}

func (f *stubFetcher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// newTestClient builds a Client wired with predictable defaults for
// tests: fresh tmp cache dir, frozen time, captured stderr, fetcher
// returning latest. Returns the client and the captured stderr. Tests
// that exercise EnvDisable opt in via t.Setenv themselves; this helper
// never touches the env so tests stay composable.
func newTestClient(t *testing.T, current string, fetcher Fetcher) (*Client, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	c := New(Options{
		Current:  current,
		Fetcher:  fetcher,
		CacheDir: t.TempDir(),
		Stderr:   buf,
		Now:      func() time.Time { return time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC) },
	})
	return c, buf
}

func TestStart_EnvDisable_NoPrintNoFetch(t *testing.T) {
	t.Setenv(EnvDisable, "1")
	fetcher := &stubFetcher{latest: "0.2.0"}
	c, buf := newTestClient(t, "0.1.3", fetcher)
	if err := SaveCache(c.cacheDir, Cache{
		CheckedAt:          time.Now().Add(-time.Hour),
		CheckedWithVersion: "0.1.3",
		LatestVersion:      "0.2.0",
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	c.Start(false)
	flushOrFail(t, c, time.Second)
	if buf.Len() != 0 {
		t.Errorf("env-disabled printed: %q", buf.String())
	}
	if fetcher.callCount() != 0 {
		t.Errorf("env-disabled fetched %d times, want 0", fetcher.callCount())
	}
}

func TestStart_FlagDisable_NoPrintNoFetch(t *testing.T) {
	fetcher := &stubFetcher{latest: "0.2.0"}
	c, buf := newTestClient(t, "0.1.3", fetcher)
	if err := SaveCache(c.cacheDir, Cache{
		CheckedAt:          time.Now().Add(-time.Hour),
		CheckedWithVersion: "0.1.3",
		LatestVersion:      "0.2.0",
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	c.Start(true)
	flushOrFail(t, c, time.Second)
	if buf.Len() != 0 {
		t.Errorf("flag-disabled printed: %q", buf.String())
	}
	if fetcher.callCount() != 0 {
		t.Errorf("flag-disabled fetched %d times, want 0", fetcher.callCount())
	}
}

func TestStart_EmptyCache_NoPrintButFetches(t *testing.T) {
	fetcher := &stubFetcher{latest: "0.2.0"}
	c, buf := newTestClient(t, "0.1.3", fetcher)
	c.Start(false)
	flushOrFail(t, c, time.Second)
	if buf.Len() != 0 {
		t.Errorf("fresh-install printed when cache was empty: %q", buf.String())
	}
	if fetcher.callCount() != 1 {
		t.Errorf("fresh-install fetched %d times, want 1", fetcher.callCount())
	}
	// And the fetch should have populated the cache for next time.
	cache, _ := LoadCache(c.cacheDir)
	if cache.LatestVersion != "0.2.0" {
		t.Errorf("cache.LatestVersion = %q after refresh, want 0.2.0", cache.LatestVersion)
	}
	if cache.CheckedWithVersion != "0.1.3" {
		t.Errorf("cache.CheckedWithVersion = %q, want 0.1.3", cache.CheckedWithVersion)
	}
}

func TestStart_FreshCacheWithNewerVersion_PrintsAndSkipsFetch(t *testing.T) {
	fetcher := &stubFetcher{latest: "0.2.0"}
	c, buf := newTestClient(t, "0.1.3", fetcher)
	if err := SaveCache(c.cacheDir, Cache{
		CheckedAt:          c.now().Add(-time.Hour),
		CheckedWithVersion: "0.1.3",
		LatestVersion:      "0.2.0",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c.Start(false)
	flushOrFail(t, c, time.Second)
	if !strings.Contains(buf.String(), "gaffer 0.2.0 available") {
		t.Errorf("missing notice in stderr: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "you have 0.1.3") {
		t.Errorf("notice missing current version: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "npm install -g @kurrent/gaffer@latest") {
		t.Errorf("notice missing install hint: %q", buf.String())
	}
	if fetcher.callCount() != 0 {
		t.Errorf("fresh cache should not refetch, got %d calls", fetcher.callCount())
	}
}

func TestStart_FreshCacheWithSameVersion_NoPrint(t *testing.T) {
	fetcher := &stubFetcher{latest: "0.1.3"}
	c, buf := newTestClient(t, "0.1.3", fetcher)
	if err := SaveCache(c.cacheDir, Cache{
		CheckedAt:          c.now().Add(-time.Hour),
		CheckedWithVersion: "0.1.3",
		LatestVersion:      "0.1.3",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c.Start(false)
	flushOrFail(t, c, time.Second)
	if buf.Len() != 0 {
		t.Errorf("up-to-date printed: %q", buf.String())
	}
}

// TestStart_StaleCache_PrintsCachedValueThenRefetches ensures we
// surface the value already on disk (snappy notice) rather than the
// freshly-fetched one. The async refresh updates the cache for next
// time but must NOT influence the printed line for this invocation.
func TestStart_StaleCache_PrintsCachedValueThenRefetches(t *testing.T) {
	fetcher := &stubFetcher{latest: "0.3.0"}
	c, buf := newTestClient(t, "0.1.3", fetcher)
	if err := SaveCache(c.cacheDir, Cache{
		CheckedAt:          c.now().Add(-48 * time.Hour),
		CheckedWithVersion: "0.1.3",
		LatestVersion:      "0.2.0",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c.Start(false)
	flushOrFail(t, c, time.Second)
	if !strings.Contains(buf.String(), "0.2.0 available") {
		t.Errorf("notice should print the cached value at startup time, got %q", buf.String())
	}
	if strings.Contains(buf.String(), "0.3.0") {
		t.Errorf("notice leaked the freshly fetched value into the current run: %q", buf.String())
	}
	if fetcher.callCount() != 1 {
		t.Errorf("stale cache should refetch, got %d calls", fetcher.callCount())
	}
	cache, _ := LoadCache(c.cacheDir)
	if cache.LatestVersion != "0.3.0" {
		t.Errorf("cache after refresh = %q, want 0.3.0", cache.LatestVersion)
	}
}

func TestStart_VersionDriftStale_Refetches(t *testing.T) {
	fetcher := &stubFetcher{latest: "0.2.0"}
	c, buf := newTestClient(t, "0.1.4", fetcher)
	// Cache says it was written by 0.1.3 - user has upgraded since.
	// Within TTL but version-stale.
	if err := SaveCache(c.cacheDir, Cache{
		CheckedAt:          c.now().Add(-time.Hour),
		CheckedWithVersion: "0.1.3",
		LatestVersion:      "0.2.0",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c.Start(false)
	flushOrFail(t, c, time.Second)
	if !strings.Contains(buf.String(), "0.2.0 available") {
		t.Errorf("missing notice: %q", buf.String())
	}
	if fetcher.callCount() != 1 {
		t.Errorf("version-drift cache should refetch, got %d calls", fetcher.callCount())
	}
}

func TestStart_EmptyCurrent_NoOp(t *testing.T) {
	fetcher := &stubFetcher{latest: "0.2.0"}
	c, buf := newTestClient(t, "", fetcher)
	c.Start(false)
	flushOrFail(t, c, time.Second)
	if buf.Len() != 0 {
		t.Errorf("empty current printed: %q", buf.String())
	}
	if fetcher.callCount() != 0 {
		t.Errorf("empty current fetched %d times, want 0", fetcher.callCount())
	}
}

// TestStart_FetchErrorPreservesPriorCache: a refresh that fails must
// not overwrite a previously-good cache entry, otherwise a transient
// outage would clobber the data the next invocation relies on for
// its synchronous-print branch.
func TestStart_FetchErrorPreservesPriorCache(t *testing.T) {
	fetcher := &stubFetcher{err: errors.New("registry down")}
	c, buf := newTestClient(t, "0.1.3", fetcher)
	// Seed a stale cache so the refresh path is exercised.
	prior := Cache{
		CheckedAt:          c.now().Add(-48 * time.Hour),
		CheckedWithVersion: "0.1.3",
		LatestVersion:      "0.2.0",
	}
	if err := SaveCache(c.cacheDir, prior); err != nil {
		t.Fatalf("seed: %v", err)
	}
	c.Start(false)
	flushOrFail(t, c, time.Second)
	// The print path used the cached "0.2.0" - allowed.
	if !strings.Contains(buf.String(), "0.2.0 available") {
		t.Errorf("expected cached notice despite fetch failure, got %q", buf.String())
	}
	// But the on-disk cache must still be the prior value, untouched.
	cache, _ := LoadCache(c.cacheDir)
	if cache.LatestVersion != "0.2.0" {
		t.Errorf("fetch error clobbered cache: LatestVersion = %q, want 0.2.0", cache.LatestVersion)
	}
	if !cache.CheckedAt.Equal(prior.CheckedAt) {
		t.Errorf("fetch error updated CheckedAt: got %v, want %v", cache.CheckedAt, prior.CheckedAt)
	}
}

type panicFetcher struct{}

func (panicFetcher) Latest(context.Context) (string, error) {
	panic("simulated transport panic")
}

func TestStart_PanicInFetcherDoesNotCrash(t *testing.T) {
	c, _ := newTestClient(t, "0.1.3", panicFetcher{})
	c.Start(false)
	// If the panic escaped the goroutine, Flush would either deadlock
	// or the test process would have already exited. Bounded wait
	// gives us a clean assertion.
	if err := c.Flush(ctxTimeout(t, time.Second)); err != nil {
		t.Errorf("Flush after panicking fetcher: %v", err)
	}
}

// TestStart_RepeatedCalls_RunOnce makes sure a second Start after the
// background refresh has already populated the cache does not fire a
// second fetch even though the new cache is now non-stale. Without the
// sync.Once, the second call would still no-op via the staleness
// check; this test guards against future refactors that drop the
// staleness check thinking sync.Once handles it.
func TestStart_RepeatedCalls_RunOnce(t *testing.T) {
	fetcher := &stubFetcher{latest: "0.2.0"}
	c, _ := newTestClient(t, "0.1.3", fetcher)
	c.Start(false)
	flushOrFail(t, c, time.Second)
	if fetcher.callCount() != 1 {
		t.Fatalf("first Start fetched %d times, want 1", fetcher.callCount())
	}
	// Wipe the cache file. If startOnce ran again it'd find an empty
	// cache and refetch; sync.Once prevents that.
	c.cacheDir = t.TempDir()
	c.Start(false)
	if err := c.Flush(ctxTimeout(t, time.Second)); err != nil {
		t.Fatalf("second Flush: %v", err)
	}
	if fetcher.callCount() != 1 {
		t.Errorf("Start re-ran despite sync.Once, fetcher called %d times", fetcher.callCount())
	}
}

// TestStart_FetcherReceivesUsableContext asserts the goroutine passes
// a real context.Context (not nil) to the fetcher. We rely on the
// fetcher's own HTTP timeout to bound the call - so the contract is
// just "context is valid", not "context has a deadline".
func TestStart_FetcherReceivesUsableContext(t *testing.T) {
	fetcher := &stubFetcher{latest: "0.2.0"}
	c, _ := newTestClient(t, "0.1.3", fetcher)
	c.Start(false)
	flushOrFail(t, c, time.Second)
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	if len(fetcher.gotCtxs) != 1 {
		t.Fatalf("fetcher got %d contexts, want 1", len(fetcher.gotCtxs))
	}
	if fetcher.gotCtxs[0] == nil {
		t.Error("fetcher received nil context")
	}
}

func TestFlush_NilClient(t *testing.T) {
	var c *Client
	if err := c.Flush(context.Background()); err != nil {
		t.Errorf("Flush on nil = %v, want nil", err)
	}
}

func TestStart_NilClient(t *testing.T) {
	var c *Client
	// Must not panic.
	c.Start(false)
}

func TestFlush_TimeoutOnSlowFetcher(t *testing.T) {
	fetcher := &stubFetcher{latest: "0.2.0", delay: 500 * time.Millisecond}
	c, _ := newTestClient(t, "0.1.3", fetcher)
	c.Start(false)
	err := c.Flush(ctxTimeout(t, 50*time.Millisecond))
	if err == nil {
		t.Error("Flush returned nil error on timeout")
	}
}

func TestFlush_AfterCloseSecondCallReturnsImmediately(t *testing.T) {
	fetcher := &stubFetcher{latest: "0.2.0"}
	c, _ := newTestClient(t, "0.1.3", fetcher)
	c.Start(false)
	if err := c.Flush(ctxTimeout(t, time.Second)); err != nil {
		t.Fatalf("first Flush: %v", err)
	}
	if err := c.Flush(ctxTimeout(t, time.Second)); err != nil {
		t.Errorf("second Flush: %v", err)
	}
}

func TestWithClient_RoundTrip(t *testing.T) {
	c := New(Options{Current: "0.1.3", CacheDir: t.TempDir(), Stderr: &bytes.Buffer{}})
	ctx := WithClient(context.Background(), c)
	if got := FromCtx(ctx); got != c {
		t.Errorf("FromCtx = %v, want %v", got, c)
	}
}

func TestFromCtx_Missing(t *testing.T) {
	if got := FromCtx(context.Background()); got != nil {
		t.Errorf("FromCtx on bare context = %v, want nil", got)
	}
}

// flushOrFail runs Flush with a bounded ctx; the test fails on
// non-nil result. Used by tests that expect no in-flight work or a
// fast refresh.
func flushOrFail(t *testing.T, c *Client, d time.Duration) {
	t.Helper()
	if err := c.Flush(ctxTimeout(t, d)); err != nil {
		t.Fatalf("Flush: %v", err)
	}
}

func ctxTimeout(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}
