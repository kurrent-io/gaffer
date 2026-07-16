package lsp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

const twoEnvConfig = `[[projection]]
name = "q"
entry = "q.js"
engine_version = 2

[[projection]]
name = "p"
entry = "p.js"
engine_version = 2

[env.local]
connection = "esdb://local:2113"

[env.prod]
connection = "esdb://prod:2113"
`

type recordedWatch struct {
	env         string
	connection  string
	projections []string
}

// watchRecorder returns a fake envWatchFunc that records each start and returns
// immediately (the entry persists in s.watches regardless, which is what
// reconcile tracks). Reads are guarded so wg.Wait can settle before asserting.
func watchRecorder() (envWatchFunc, func() []recordedWatch) {
	var mu sync.Mutex
	var got []recordedWatch
	fn := func(_ context.Context, spec envWatchSpec) {
		mu.Lock()
		got = append(got, recordedWatch{env: spec.env, connection: spec.resolved.Connection, projections: append([]string(nil), spec.projections...)})
		mu.Unlock()
	}
	return fn, func() []recordedWatch {
		mu.Lock()
		defer mu.Unlock()
		return append([]recordedWatch(nil), got...)
	}
}

func watchKeys(s *Server) []string {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	out := make([]string, 0, len(s.watches))
	for k := range s.watches {
		out = append(out, k)
	}
	return out
}

func TestReconcileWatches_StartsOnePerEnvWithSortedProjections(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", twoEnvConfig))
	s := testServer(nil)
	rec, read := watchRecorder()
	s.watchRun = rec
	s.docs.Open(uri, twoEnvConfig)

	s.reconcileWatches(uri)
	s.wg.Wait()

	got := read()
	if len(got) != 2 {
		t.Fatalf("expected one watch per env (2), got %d: %+v", len(got), got)
	}
	byEnv := map[string]recordedWatch{}
	for _, w := range got {
		byEnv[w.env] = w
	}
	if byEnv["local"].connection != "esdb://local:2113" || byEnv["prod"].connection != "esdb://prod:2113" {
		t.Fatalf("watches carried the wrong connections: %+v", got)
	}
	for env, w := range byEnv {
		if len(w.projections) != 2 || w.projections[0] != "p" || w.projections[1] != "q" {
			t.Fatalf("env %q should watch sorted [p q], got %v", env, w.projections)
		}
	}
	if keys := watchKeys(s); len(keys) != 2 {
		t.Fatalf("expected 2 active watch entries, got %v", keys)
	}
}

func TestReconcileWatches_StopsRemovedEnv(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", twoEnvConfig))
	s := testServer(nil)
	rec, _ := watchRecorder()
	s.watchRun = rec
	s.docs.Open(uri, twoEnvConfig)
	s.reconcileWatches(uri)
	s.wg.Wait()

	// Drop the prod env from the buffer and reconcile.
	oneEnv := "[[projection]]\nname = \"p\"\nentry = \"p.js\"\nengine_version = 2\n\n[env.local]\nconnection = \"esdb://local:2113\"\n"
	if _, err := s.docs.Change(uri, oneEnv); err != nil {
		t.Fatal(err)
	}
	s.reconcileWatches(uri)
	s.wg.Wait()

	keys := watchKeys(s)
	if len(keys) != 1 || keys[0] != uri+"\x00local" {
		t.Fatalf("only the local watch should remain, got %v", keys)
	}
}

func TestReconcileWatches_RestartsOnProjectionSetChange(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", twoEnvConfig))
	s := testServer(nil)
	rec, read := watchRecorder()
	s.watchRun = rec
	s.docs.Open(uri, twoEnvConfig)
	s.reconcileWatches(uri)
	s.wg.Wait()

	// Add a projection: every env's watch set changed, so both restart.
	withThird := twoEnvConfig + "\n[[projection]]\nname = \"r\"\nentry = \"r.js\"\nengine_version = 2\n"
	if _, err := s.docs.Change(uri, withThird); err != nil {
		t.Fatal(err)
	}
	s.reconcileWatches(uri)
	s.wg.Wait()

	got := read()
	if len(got) != 4 {
		t.Fatalf("expected 2 initial + 2 restarted watches, got %d: %+v", len(got), got)
	}
	// The two restarts (order between envs is nondeterministic) each carry the
	// new 3-projection set; the two initial watches carried 2.
	threes := 0
	for _, w := range got {
		if len(w.projections) == 3 {
			threes++
		}
	}
	if threes != 2 {
		t.Fatalf("both restarted watches should carry 3 projections, got %+v", got)
	}
}

func TestReconcileWatches_RestartsOnConnectionChange(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", twoEnvConfig))
	s := testServer(nil)
	rec, read := watchRecorder()
	s.watchRun = rec
	s.docs.Open(uri, twoEnvConfig)
	s.reconcileWatches(uri)
	s.wg.Wait()

	const moved = `[[projection]]
name = "q"
entry = "q.js"
engine_version = 2

[[projection]]
name = "p"
entry = "p.js"
engine_version = 2

[env.local]
connection = "esdb://local-moved:2113"

[env.prod]
connection = "esdb://prod:2113"
`
	if _, err := s.docs.Change(uri, moved); err != nil {
		t.Fatal(err)
	}
	s.reconcileWatches(uri)
	s.wg.Wait()

	got := read()
	// local restarts (connection moved); prod unchanged, not restarted.
	local := 0
	for _, w := range got {
		if w.env == "local" {
			local++
		}
	}
	if local != 2 {
		t.Fatalf("local should have started twice (initial + restart), got %d: %+v", local, got)
	}
	var prodConns []string
	for _, w := range got {
		if w.env == "prod" {
			prodConns = append(prodConns, w.connection)
		}
	}
	if len(prodConns) != 1 {
		t.Fatalf("prod should not restart on a local-only change, got %v", prodConns)
	}
}

func TestReconcileWatches_TornDownWhenSurfaceOffOrClosed(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", twoEnvConfig))

	// statusLens disabled: no watches at all.
	s := testServer(nil)
	s.statusLensCapable = false
	rec, read := watchRecorder()
	s.watchRun = rec
	s.docs.Open(uri, twoEnvConfig)
	s.reconcileWatches(uri)
	s.wg.Wait()
	if got := read(); len(got) != 0 {
		t.Fatalf("no watches should start without the statusLens capability, got %+v", got)
	}

	// Enabled, then closed: stopWatches clears them.
	s.statusLensCapable = true
	s.reconcileWatches(uri)
	s.wg.Wait()
	if len(watchKeys(s)) != 2 {
		t.Fatal("precondition: 2 watches after enabling")
	}
	s.stopWatches(uri)
	if keys := watchKeys(s); len(keys) != 0 {
		t.Fatalf("stopWatches should clear every watch, got %v", keys)
	}
}

func TestWatchLoop_BacksOffThenResetsWhenHealthy(t *testing.T) {
	var sleeps []time.Duration
	sleep := func(_ context.Context, d time.Duration) bool {
		sleeps = append(sleeps, d)
		return true
	}
	// Three fast (unhealthy) drops grow the backoff; a healthy drop resets it;
	// then a ctx-cancel (reconnect=false) stops the loop.
	script := []struct{ reconnect, healthy bool }{
		{true, false},
		{true, false},
		{true, false},
		{true, true},
		{false, false},
	}
	i := 0
	once := func(context.Context) (bool, bool) {
		r := script[i]
		i++
		return r.reconnect, r.healthy
	}
	watchLoop(context.Background(), once, sleep)

	want := []time.Duration{
		watchBackoffStart,     // after 1st unhealthy drop
		watchBackoffStart * 2, // grown
		watchBackoffStart * 4, // grown
		watchBackoffStart,     // reset after the healthy drop
	}
	if len(sleeps) != len(want) {
		t.Fatalf("sleep schedule: got %v want %v", sleeps, want)
	}
	for i, d := range want {
		if sleeps[i] != d {
			t.Fatalf("sleep[%d]: got %v want %v (full: %v)", i, sleeps[i], d, sleeps)
		}
	}
}

func TestWatchLoop_BackoffCapsAndStopsOnCtxCancel(t *testing.T) {
	var last time.Duration
	calls := 0
	// sleep returns false (ctx cancelled) after enough calls to reach the cap.
	sleep := func(_ context.Context, d time.Duration) bool {
		last = d
		calls++
		return d < watchBackoffMax // keep going until we've hit the cap
	}
	once := func(context.Context) (bool, bool) { return true, false } // always fast-fail
	watchLoop(context.Background(), once, sleep)

	if last != watchBackoffMax {
		t.Fatalf("backoff should reach the cap, last sleep was %v", last)
	}
}

func TestWatchEnvStreams_DropSignalsReconnect(t *testing.T) {
	var updates atomic.Int64
	watch := func(ctx context.Context, name string, onUpdate func()) error {
		if name == "p" {
			onUpdate()                   // an event arrives
			return errors.New("dropped") // then the subscription drops
		}
		<-ctx.Done() // the others stay up until cancelled
		return ctx.Err()
	}
	got := watchEnvStreams(context.Background(), watch, []string{"p", "q", "r"}, func() { updates.Add(1) })
	if !got {
		t.Fatal("a dropped subscription should signal reconnect (true)")
	}
	if updates.Load() == 0 {
		t.Fatal("onUpdate should have fired for the event before the drop")
	}
}

func TestWatchEnvStreams_CtxCancelSignalsStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	watch := func(ctx context.Context, _ string, _ func()) error {
		<-ctx.Done()
		return ctx.Err()
	}
	done := make(chan bool, 1)
	go func() { done <- watchEnvStreams(ctx, watch, []string{"p", "q"}, func() {}) }()
	cancel()
	if <-done {
		t.Fatal("a ctx cancel should signal stop (false), not reconnect")
	}
}

func TestHandleDidOpen_StartsWatches(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", twoEnvConfig))
	s := testServer(func(_ context.Context, _ string, _ *config.Config, _, env string) envStatus {
		return envStatus{Target: env}
	})
	rec, read := watchRecorder()
	s.watchRun = rec

	req := &jsonrpc2.Request{}
	if err := req.SetParams(DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: twoEnvConfig},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleDidOpen(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()

	if got := read(); len(got) != 2 {
		t.Fatalf("didOpen should start a watch per env, got %+v", got)
	}
	if len(watchKeys(s)) != 2 {
		t.Fatalf("expected 2 active watches after open, got %v", watchKeys(s))
	}
}

func TestHandleDidClose_StopsWatches(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", twoEnvConfig))
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		return envStatus{}
	})
	rec, _ := watchRecorder()
	s.watchRun = rec
	s.docs.Open(uri, twoEnvConfig)
	s.reconcileWatches(uri)
	s.wg.Wait()
	if len(watchKeys(s)) != 2 {
		t.Fatal("precondition: watches active")
	}

	req := &jsonrpc2.Request{}
	if err := req.SetParams(DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleDidClose(req); err != nil {
		t.Fatal(err)
	}
	if keys := watchKeys(s); len(keys) != 0 {
		t.Fatalf("didClose should stop every watch, got %v", keys)
	}
}

func TestReconcileWatches_TornDownOnParseFailure(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", twoEnvConfig))
	s := testServer(nil)
	rec, _ := watchRecorder()
	s.watchRun = rec
	s.docs.Open(uri, twoEnvConfig)
	s.reconcileWatches(uri)
	s.wg.Wait()
	if len(watchKeys(s)) != 2 {
		t.Fatal("precondition: watches active")
	}

	if _, err := s.docs.Change(uri, "[unterminated"); err != nil {
		t.Fatal(err)
	}
	s.reconcileWatches(uri)
	s.wg.Wait()
	if keys := watchKeys(s); len(keys) != 0 {
		t.Fatalf("an unparseable buffer should tear down every watch, got %v", keys)
	}
}

func TestReconcileWatches_NoProjectionsNoWatches(t *testing.T) {
	root := t.TempDir()
	// Envs but zero projections: nothing to watch.
	const envsOnly = "[env.local]\nconnection = \"esdb://local:2113\"\n\n[env.prod]\nconnection = \"esdb://prod:2113\"\n"
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", envsOnly))
	s := testServer(nil)
	rec, read := watchRecorder()
	s.watchRun = rec
	s.docs.Open(uri, envsOnly)
	s.reconcileWatches(uri)
	s.wg.Wait()

	if got := read(); len(got) != 0 {
		t.Fatalf("a config with no projections should hold no watches, got %+v", got)
	}
}

func TestIsJSSource(t *testing.T) {
	cases := map[string]bool{
		"file:///ws/projections/p.js":    true,
		"file:///ws/gaffer.toml":         false,
		"file:///ws/p.ts":                false,
		"file:///ws/node_modules/x/y.js": false,
	}
	for uri, want := range cases {
		if got := isJSSource(uri); got != want {
			t.Errorf("isJSSource(%q) = %v, want %v", uri, got, want)
		}
	}
}

// eventually polls cond until it holds or the deadline passes. Used where a
// debounced callback fires on a real timer (the source-refresh debounce).
func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within 2s")
}

func TestApplyWatchedFileEvents_SourceChangeRecomputesOpenConfigs(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", runtimeCfg))
	var drift_ atomic.Int64
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		drift_.Add(1)
		return envStatus{Entries: []drift.StatusEntry{rtEntry("p", remote.StateRunning)}}
	})
	s.debouncer = newDebouncer(time.Millisecond) // fire the source-refresh promptly
	s.docs.Open(uri, runtimeCfg)

	// A projection source changed on disk (not node_modules). The refresh is
	// debounced, so wait for it to fire.
	s.applyWatchedFileEvents(context.Background(), []FileEvent{
		{URI: pathToURI(root + "/projections/p.js"), Type: FileChangeChanged},
	})
	eventually(t, func() bool { return drift_.Load() > 0 })
	s.wg.Wait()
}

func TestApplyWatchedFileEvents_SourceChangeRecomputesEveryOpenConfig(t *testing.T) {
	uriA := pathToURI(writeWorkspaceFile(t, t.TempDir(), "gaffer.toml", runtimeCfg))
	uriB := pathToURI(writeWorkspaceFile(t, t.TempDir(), "gaffer.toml", runtimeCfg))
	var mu sync.Mutex
	seen := map[string]bool{}
	s := testServer(func(_ context.Context, _ string, _ *config.Config, uri, _ string) envStatus {
		mu.Lock()
		seen[uri] = true
		mu.Unlock()
		return envStatus{Entries: []drift.StatusEntry{rtEntry("p", remote.StateRunning)}}
	})
	s.debouncer = newDebouncer(time.Millisecond)
	s.docs.Open(uriA, runtimeCfg)
	s.docs.Open(uriB, runtimeCfg)

	s.applyWatchedFileEvents(context.Background(), []FileEvent{
		{URI: "file:///ws/shared/p.js", Type: FileChangeChanged},
	})
	eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return seen[uriA] && seen[uriB]
	})
	s.wg.Wait()
}

func TestApplyWatchedFileEvents_SourceDeleteRecomputes(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", runtimeCfg))
	var drift_ atomic.Int64
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		drift_.Add(1)
		return envStatus{}
	})
	s.debouncer = newDebouncer(time.Millisecond)
	s.docs.Open(uri, runtimeCfg)

	// Deleting a projection source is a drift input change (the projection becomes
	// uncompilable), so it must recompute like a create/change.
	s.applyWatchedFileEvents(context.Background(), []FileEvent{
		{URI: pathToURI(root + "/projections/p.js"), Type: FileChangeDeleted},
	})
	eventually(t, func() bool { return drift_.Load() > 0 })
	s.wg.Wait()
}

func TestApplyWatchedFileEvents_IgnoresNodeModules(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", runtimeCfg))
	var drift_ atomic.Int64
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		drift_.Add(1)
		return envStatus{}
	})
	s.debouncer = newDebouncer(time.Millisecond)
	s.docs.Open(uri, runtimeCfg)

	s.applyWatchedFileEvents(context.Background(), []FileEvent{
		{URI: "file:///ws/node_modules/dep/index.js", Type: FileChangeChanged},
	})
	// No source-refresh is scheduled at all, so give any stray timer a moment
	// and confirm nothing fired.
	time.Sleep(20 * time.Millisecond)
	s.wg.Wait()
	if drift_.Load() != 0 {
		t.Fatal("a node_modules .js change must not trigger a refresh")
	}
}

func TestBorrowConn(t *testing.T) {
	s := testServer(nil)

	if _, ok := s.borrowConn("u", "prod"); ok {
		t.Fatal("no watch for the env should not borrow a connection")
	}

	client := &remote.Client{}
	s.watchMu.Lock()
	s.watches[inflightKey("u", "prod")] = &watchEntry{conn: &envConn{client: client}}
	s.watches[inflightKey("u", "staging")] = &watchEntry{conn: &envConn{}} // no client yet
	s.watchMu.Unlock()

	bc, ok := s.borrowConn("u", "prod")
	if !ok || bc.client != client {
		t.Fatalf("should borrow the watch's live client, got ok=%v client=%p", ok, bc.client)
	}
	if _, ok := s.borrowConn("u", "staging"); ok {
		t.Fatal("an env whose watch hasn't connected yet should not borrow")
	}
}

func TestEnvConn_PublishAndClear(t *testing.T) {
	s := testServer(nil)
	c := &envConn{}
	client := &remote.Client{}
	var closed bool

	s.setEnvConn(c, client, nil, func() { closed = true })
	s.watchMu.Lock()
	s.watches[inflightKey("u", "prod")] = &watchEntry{conn: c}
	s.watchMu.Unlock()

	if bc, ok := s.borrowConn("u", "prod"); !ok || bc.client != client {
		t.Fatal("setEnvConn should publish the client for borrowing")
	}

	s.clearEnvConn(c)
	if !closed {
		t.Fatal("clearEnvConn should run cleanup (close the connection)")
	}
	if _, ok := s.borrowConn("u", "prod"); ok {
		t.Fatal("after clearEnvConn the slot should not borrow")
	}
}

func TestOnDefinitionChanged_RecomputesOnlyTheChangedEnv(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", twoEnvConfig))
	var mu sync.Mutex
	deep := map[string]int{}
	runtime := map[string]int{}
	s := testServer(func(_ context.Context, _ string, _ *config.Config, _, env string) envStatus {
		mu.Lock()
		deep[env]++
		mu.Unlock()
		return envStatus{Entries: []drift.StatusEntry{rtEntry("p", remote.StateRunning)}, Target: env}
	})
	s.runtimeFetch = func(_ context.Context, _ string, _ *config.Config, _, env string, cached envStatus) envStatus {
		mu.Lock()
		runtime[env]++
		mu.Unlock()
		return reattachRuntime(cached, nil)
	}
	clk := time.Unix(1000, 0)
	s.statusCache.now = func() time.Time { return clk }
	s.docs.Open(uri, twoEnvConfig)

	s.refreshStatus(uri, true) // prime both envs' verdicts
	s.wg.Wait()
	clk = clk.Add(pollThrottleWindow + time.Second) // let the sibling's runtime poll through

	s.onDefinitionChanged(uri, "local") // a deploy on 'local' only
	s.wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if deep["local"] != 2 {
		t.Fatalf("the changed env should recompute drift, got deep[local]=%d", deep["local"])
	}
	if deep["prod"] != 1 || runtime["prod"] != 1 {
		t.Fatalf("the sibling env should refresh runtime only, got deep[prod]=%d runtime[prod]=%d", deep["prod"], runtime["prod"])
	}
}

func TestRefreshStatus_RecomputeRefusedByInflightSetsForceFull(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", runtimeCfg))
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		return envStatus{}
	})
	s.docs.Open(uri, runtimeCfg)

	// Occupy prod's single-flight slot so the recompute's begin() is refused.
	s.statusCache.begin(uri, "prod")

	s.refreshStatus(uri, true) // driftChanged, but begin refused -> must flag, not lose it
	s.wg.Wait()

	if !s.statusCache.forcedFull(uri, "prod") {
		t.Fatal("a driftChanged refresh refused by single-flight must set forceFull so the next refresh recomputes")
	}
}

func TestOnDefinitionChanged_ForcesFullRecompute(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceFile(t, root, "p.js", "export default {}")
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", runtimeCfg))

	var full, rt atomic.Int64
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		full.Add(1)
		return envStatus{Entries: []drift.StatusEntry{rtEntry("p", remote.StateRunning)}, Target: "prod-cluster"}
	})
	s.runtimeFetch = func(_ context.Context, _ string, _ *config.Config, _, _ string, cached envStatus) envStatus {
		rt.Add(1)
		return reattachRuntime(cached, nil)
	}
	s.docs.Open(uri, runtimeCfg)

	s.refreshStatus(uri, true) // first recompute, caches a verdict
	s.wg.Wait()
	if full.Load() != 1 {
		t.Fatalf("precondition: one drift recompute, got %d", full.Load())
	}

	// A subscription reports a server-side change: must recompute drift for that
	// env even though nothing local changed (driftChanged is false in the refresh
	// onDefinitionChanged issues; the forceFull flag is what drives it).
	s.onDefinitionChanged(uri, "prod")
	s.wg.Wait()

	if full.Load() != 2 || rt.Load() != 0 {
		t.Fatalf("a definition change should force a drift recompute, got full=%d rt=%d", full.Load(), rt.Load())
	}
}
