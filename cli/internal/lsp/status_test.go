package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sourcegraph/jsonrpc2"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func TestStatusCache_SingleFlight(t *testing.T) {
	c := newStatusCache()
	gen, ok := c.begin("u", "prod")
	if !ok {
		t.Fatal("first begin should succeed")
	}
	if _, ok := c.begin("u", "prod"); ok {
		t.Fatal("second begin while in flight should be refused")
	}
	if _, ok := c.begin("u", "staging"); !ok {
		t.Fatal("a distinct env should begin independently")
	}
	c.store("u", "prod", gen, envStatus{Target: "t"})
	if _, ok := c.begin("u", "prod"); !ok {
		t.Fatal("begin should succeed once store cleared the marker")
	}
}

func TestStatusCache_StoreGetReturnsCopy(t *testing.T) {
	c := newStatusCache()
	c.store("u", "prod", 0, envStatus{Target: "prod-cluster", Production: true})
	got := c.get("u")
	if got == nil || got["prod"].Target != "prod-cluster" || !got["prod"].Production {
		t.Fatalf("get: %+v", got)
	}
	// Mutating the returned map must not touch the cache.
	got["prod"] = envStatus{}
	if c.get("u")["prod"].Target != "prod-cluster" {
		t.Fatal("get must return a copy")
	}
	if c.get("missing") != nil {
		t.Fatal("missing uri should be nil")
	}
}

func TestStatusCache_ReleaseAndDrop(t *testing.T) {
	c := newStatusCache()
	c.begin("u", "prod")
	c.release("u", "prod")
	if _, ok := c.begin("u", "prod"); !ok {
		t.Fatal("release should clear the in-flight marker")
	}
	c.store("u", "prod", 0, envStatus{})
	c.store("u2", "x", 0, envStatus{})
	c.begin("u", "staging") // in flight, never stored

	c.drop("u")
	if c.get("u") != nil {
		t.Fatal("drop should clear byURI for u")
	}
	if _, ok := c.begin("u", "staging"); !ok {
		t.Fatal("drop should clear u's in-flight markers")
	}
	if c.get("u2") == nil {
		t.Fatal("drop must not touch other uris")
	}
}

func TestStatusCache_DropDiscardsInFlightStore(t *testing.T) {
	c := newStatusCache()
	// A fetch begins, capturing the current generation.
	gen, ok := c.begin("u", "prod")
	if !ok {
		t.Fatal("begin should succeed")
	}
	// The document closes (or its config reloads) while the fetch runs.
	c.drop("u")
	// The late store must be discarded, not resurrect the cache.
	c.store("u", "prod", gen, envStatus{Target: "stale"})
	if c.get("u") != nil {
		t.Fatalf("a stale store after drop must not repopulate the cache: %+v", c.get("u"))
	}
	// A fresh fetch (post-drop generation) stores normally.
	gen2, _ := c.begin("u", "prod")
	c.store("u", "prod", gen2, envStatus{Target: "fresh"})
	if got := c.get("u"); got == nil || got["prod"].Target != "fresh" {
		t.Fatalf("a post-drop fetch should store: %+v", got)
	}
}

// testServer builds a Server with a live run context so spawnWithCtx queues
// work, and the client refresh gate left off so requestCodeLensRefresh is an
// inert no-op (no conn needed).
func testServer(fetch statusFetchFunc) *Server {
	s := NewServer(ServerOptions{})
	ctx := context.Background()
	s.runCtxFn = func() context.Context { return ctx }
	s.statusFetch = fetch
	s.statusLensCapable = true // these tests exercise the status surface
	// No-op the definition watch by default: the runtime watchRun dials and
	// reconnects forever, which would wedge any test that goes through the parse
	// path and then waits on the wg (runCtx here never cancels). Reconcile tests
	// override this with a recorder.
	s.watchRun = func(context.Context, envWatchSpec) {}
	return s
}

func TestRefreshStatus_PopulatesCachePerEnv(t *testing.T) {
	const content = `[env.local]
connection = "esdb://localhost:2113"
default = true

[env.prod]
connection = "esdb://prod:2113"
`
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", content))

	var mu sync.Mutex
	seen := map[string]int{}
	s := testServer(func(_ context.Context, gotRoot string, _ *config.Config, _, env string) envStatus {
		mu.Lock()
		seen[env]++
		mu.Unlock()
		if gotRoot != root {
			t.Errorf("root: got %q want %q", gotRoot, root)
		}
		return envStatus{Target: env + "-cluster"}
	})
	s.docs.Open(uri, content)

	s.refreshStatus(uri, true)
	s.wg.Wait()

	got := s.statusCache.get(uri)
	if len(got) != 2 || got["local"].Target != "local-cluster" || got["prod"].Target != "prod-cluster" {
		t.Fatalf("cache: %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["local"] != 1 || seen["prod"] != 1 {
		t.Fatalf("fetch call counts: %+v", seen)
	}
}

func TestRefreshStatus_SingleFlightAcrossCalls(t *testing.T) {
	const content = "[env.prod]\nconnection = \"esdb://prod:2113\"\n"
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", content))

	var calls atomic.Int64
	release := make(chan struct{})
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		calls.Add(1)
		<-release // hold the fetch in flight
		return envStatus{}
	})
	s.docs.Open(uri, content)

	s.refreshStatus(uri, true) // marks prod in-flight and spawns the (blocked) fetch
	s.refreshStatus(uri, true) // prod still in flight -> skipped
	close(release)
	s.wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 fetch (single-flight), got %d", got)
	}
}

func TestRefreshStatus_InvalidConfigIsNoop(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", "[unterminated"))

	var fetched atomic.Bool
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		fetched.Store(true)
		return envStatus{}
	})
	s.docs.Open(uri, "[unterminated")

	s.refreshStatus(uri, true)
	s.wg.Wait()

	if fetched.Load() {
		t.Fatal("an invalid config should not trigger a fetch")
	}
	if s.statusCache.get(uri) != nil {
		t.Fatal("cache should stay empty for an invalid config")
	}
}

func TestRefreshStatus_ParseFailureDropsCachedStatus(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", "[unterminated"))

	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		return envStatus{}
	})
	s.docs.Open(uri, "[unterminated")
	// A prior successful fetch left status cached for this uri.
	s.statusCache.store(uri, "prod", 0, envStatus{Target: "old"})
	if s.statusCache.get(uri) == nil {
		t.Fatal("precondition: cache should be populated")
	}

	s.refreshStatus(uri, true)
	s.wg.Wait()

	if s.statusCache.get(uri) != nil {
		t.Fatal("a buffer that no longer parses should drop the stale cached status")
	}
}

func TestRefreshStatus_NonConfigURIIsNoop(t *testing.T) {
	var fetched atomic.Bool
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		fetched.Store(true)
		return envStatus{}
	})

	s.refreshStatus("file:///x/notgaffer.toml", true)
	s.wg.Wait()

	if fetched.Load() {
		t.Fatal("a non-config uri should not trigger a fetch")
	}
}

// envOnlyConfig is a minimal on-disk gaffer.toml with one env, enough to drive
// config.Load in the trigger handlers.
const envOnlyConfig = "[env.prod]\nconnection = \"esdb://prod:2113\"\n"

func TestRefreshStatus_RecoversFetchPanic(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", envOnlyConfig))

	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		panic("boom")
	})
	s.docs.Open(uri, envOnlyConfig)

	s.refreshStatus(uri, true)
	s.wg.Wait() // must return - a recovered panic, not a process crash

	got := s.statusCache.get(uri)
	if got == nil || got["prod"].Err == nil {
		t.Fatalf("a panicking fetch should be recorded as an error, not crash: %+v", got)
	}
}

func TestSafeStatusFetch_ScrubsExpandedConnectionFromPanicLog(t *testing.T) {
	t.Setenv("STATUS_TEST_PW", "s3cr3t")
	root := t.TempDir()
	const cfgSrc = "[env.prod]\nconnection = \"kurrentdb://user:${STATUS_TEST_PW}@host:2113\"\n"
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", cfgSrc))

	// A crash deep in the client carries the ${VAR}-expanded connection, not
	// the toml literal - so scrubbing only the raw connection would leak the
	// secret. This guards the expanded-form scrub in safeFetch.
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		panic("dial failed: kurrentdb://user:s3cr3t@host:2113")
	})
	s.docs.Open(uri, cfgSrc)

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })

	s.refreshStatus(uri, true)
	s.wg.Wait()

	if strings.Contains(buf.String(), "s3cr3t") {
		t.Fatalf("expanded connection secret leaked into the panic log: %q", buf.String())
	}
}

func TestHandleDidOpen_TriggersStatusFetch(t *testing.T) {
	root := t.TempDir()
	cfgPath := writeWorkspaceFile(t, root, "gaffer.toml", envOnlyConfig)
	uri := pathToURI(cfgPath)

	var fetched atomic.Bool
	s := testServer(func(_ context.Context, _ string, _ *config.Config, _, env string) envStatus {
		fetched.Store(true)
		return envStatus{Target: env + "-cluster"}
	})

	req := &jsonrpc2.Request{}
	if err := req.SetParams(DidOpenTextDocumentParams{
		TextDocument: TextDocumentItem{URI: uri, Text: envOnlyConfig},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleDidOpen(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()

	if !fetched.Load() {
		t.Fatal("didOpen should trigger a status fetch")
	}
	if s.statusCache.get(uri) == nil {
		t.Fatal("cache should be populated after open")
	}
}

func TestHandleRefreshStatus_TriggersFetch(t *testing.T) {
	root := t.TempDir()
	cfgPath := writeWorkspaceFile(t, root, "gaffer.toml", envOnlyConfig)
	uri := pathToURI(cfgPath)

	s := testServer(func(_ context.Context, _ string, _ *config.Config, _, env string) envStatus {
		return envStatus{Target: env + "-cluster"}
	})
	s.docs.Open(uri, envOnlyConfig)

	req := &jsonrpc2.Request{}
	if err := req.SetParams(RefreshStatusParams{URI: uri}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleRefreshStatus(req); err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()

	if got := s.statusCache.get(uri); got == nil || got["prod"].Target != "prod-cluster" {
		t.Fatalf("refresh should populate the cache: %+v", got)
	}
}

func TestHandleDidClose_DropsStatus(t *testing.T) {
	uri := "file:///ws/gaffer.toml"
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		return envStatus{}
	})
	s.statusCache.store(uri, "prod", 0, envStatus{Target: "prod-cluster"})

	req := &jsonrpc2.Request{}
	if err := req.SetParams(DidCloseTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleDidClose(req); err != nil {
		t.Fatal(err)
	}

	if s.statusCache.get(uri) != nil {
		t.Fatal("didClose should drop the cached status for the uri")
	}
}

func TestHandleDidSave_TriggersStatusFetch(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", envOnlyConfig))

	var fetched atomic.Bool
	s := testServer(func(_ context.Context, _ string, _ *config.Config, _, env string) envStatus {
		fetched.Store(true)
		return envStatus{Target: env + "-cluster"}
	})
	s.docs.Open(uri, envOnlyConfig)

	req := &jsonrpc2.Request{}
	if err := req.SetParams(DidSaveTextDocumentParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleDidSave(req); err != nil {
		t.Fatal(err)
	}
	s.wg.Wait()

	if !fetched.Load() {
		t.Fatal("didSave should trigger a status fetch")
	}
	if s.statusCache.get(uri) == nil {
		t.Fatal("cache should be populated after save")
	}
}

func TestRefreshStatus_DisabledWithoutStatusLensCapability(t *testing.T) {
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", envOnlyConfig))

	var fetched atomic.Bool
	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		fetched.Store(true)
		return envStatus{}
	})
	s.statusLensCapable = false // client did not opt into the status surface
	s.docs.Open(uri, envOnlyConfig)

	s.refreshStatus(uri, true)
	s.wg.Wait()

	if fetched.Load() {
		t.Fatal("status should not be fetched for a client that didn't opt in")
	}
	if s.statusCache.get(uri) != nil {
		t.Fatal("cache should stay empty without the statusLens capability")
	}
}

func TestHandleInitialize_StatusLensOptIn(t *testing.T) {
	s := NewServer(ServerOptions{})
	req := &jsonrpc2.Request{}
	if err := req.SetParams(InitializeParams{
		InitOptions: json.RawMessage(`{"statusLens":true}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.handleInitialize(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if !s.statusLensCapable {
		t.Fatal("the statusLens init option should enable the status surface")
	}
}
