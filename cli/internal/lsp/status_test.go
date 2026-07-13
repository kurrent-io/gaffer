package lsp

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

func TestStatusCache_SingleFlight(t *testing.T) {
	c := newStatusCache()
	if !c.begin("u", "prod") {
		t.Fatal("first begin should succeed")
	}
	if c.begin("u", "prod") {
		t.Fatal("second begin while in flight should be refused")
	}
	if !c.begin("u", "staging") {
		t.Fatal("a distinct env should begin independently")
	}
	c.store("u", "prod", envStatus{Target: "t"})
	if !c.begin("u", "prod") {
		t.Fatal("begin should succeed once store cleared the marker")
	}
}

func TestStatusCache_StoreGetReturnsCopy(t *testing.T) {
	c := newStatusCache()
	c.store("u", "prod", envStatus{Target: "prod-cluster", Production: true})
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
	if !c.begin("u", "prod") {
		t.Fatal("release should clear the in-flight marker")
	}
	c.store("u", "prod", envStatus{})
	c.store("u2", "x", envStatus{})
	c.begin("u", "staging") // in flight, never stored

	c.drop("u")
	if c.get("u") != nil {
		t.Fatal("drop should clear byURI for u")
	}
	if !c.begin("u", "staging") {
		t.Fatal("drop should clear u's in-flight markers")
	}
	if c.get("u2") == nil {
		t.Fatal("drop must not touch other uris")
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
	return s
}

func TestRefreshStatus_PopulatesCachePerEnv(t *testing.T) {
	root := t.TempDir()
	cfgPath := writeWorkspaceFile(t, root, "gaffer.toml", `[env.local]
connection = "esdb://localhost:2113"
default = true

[env.prod]
connection = "esdb://prod:2113"
`)
	uri := pathToURI(cfgPath)

	var mu sync.Mutex
	seen := map[string]int{}
	s := testServer(func(_ context.Context, gotRoot string, _ *config.Config, env string) envStatus {
		mu.Lock()
		seen[env]++
		mu.Unlock()
		if gotRoot != root {
			t.Errorf("root: got %q want %q", gotRoot, root)
		}
		return envStatus{Target: env + "-cluster"}
	})

	s.refreshStatus(uri)
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
	root := t.TempDir()
	cfgPath := writeWorkspaceFile(t, root, "gaffer.toml", `[env.prod]
connection = "esdb://prod:2113"
`)
	uri := pathToURI(cfgPath)

	var calls atomic.Int64
	release := make(chan struct{})
	s := testServer(func(context.Context, string, *config.Config, string) envStatus {
		calls.Add(1)
		<-release // hold the fetch in flight
		return envStatus{}
	})

	s.refreshStatus(uri) // marks prod in-flight and spawns the (blocked) fetch
	s.refreshStatus(uri) // prod still in flight -> skipped
	close(release)
	s.wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 fetch (single-flight), got %d", got)
	}
}

func TestRefreshStatus_InvalidConfigIsNoop(t *testing.T) {
	root := t.TempDir()
	cfgPath := writeWorkspaceFile(t, root, "gaffer.toml", "[unterminated")
	uri := pathToURI(cfgPath)

	var fetched atomic.Bool
	s := testServer(func(context.Context, string, *config.Config, string) envStatus {
		fetched.Store(true)
		return envStatus{}
	})

	s.refreshStatus(uri)
	s.wg.Wait()

	if fetched.Load() {
		t.Fatal("an invalid config should not trigger a fetch")
	}
	if s.statusCache.get(uri) != nil {
		t.Fatal("cache should stay empty for an invalid config")
	}
}

func TestRefreshStatus_NonConfigURIIsNoop(t *testing.T) {
	var fetched atomic.Bool
	s := testServer(func(context.Context, string, *config.Config, string) envStatus {
		fetched.Store(true)
		return envStatus{}
	})

	s.refreshStatus("file:///x/notgaffer.toml")
	s.wg.Wait()

	if fetched.Load() {
		t.Fatal("a non-config uri should not trigger a fetch")
	}
}
