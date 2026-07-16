package lsp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

// runtimeCfg is a one-env, one-projection config, enough to drive refreshStatus.
const runtimeCfg = `[[projection]]
name = "p"
entry = "p.js"
engine_version = 2

[env.prod]
connection = "esdb://prod:2113"
`

func rtEntry(name string, st remote.State) drift.StatusEntry {
	return drift.StatusEntry{
		Comparison: drift.Comparison{Name: name, State: drift.InSync},
		Runtime:    &remote.Status{Name: name, State: st},
	}
}

// runtimeTestServer wires a status server with counting drift/runtime fetchers
// and a controllable clock, plus an open gaffer.toml. The drift fetcher returns
// one running projection; the runtime fetcher reattaches a stopped state so a
// runtime-only refresh is observably distinct from a drift recompute.
func runtimeTestServer(t *testing.T, drift_, rt *atomic.Int64) (*Server, string, *time.Time) {
	t.Helper()
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", runtimeCfg))

	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		drift_.Add(1)
		return envStatus{Entries: []drift.StatusEntry{rtEntry("p", remote.StateRunning)}, Target: "prod-cluster"}
	})
	s.runtimeFetch = func(_ context.Context, _ string, _ *config.Config, _, _ string, cached envStatus) envStatus {
		rt.Add(1)
		return reattachRuntime(cached, []remote.Status{{Name: "p", State: remote.StateStopped}})
	}
	clk := time.Unix(1000, 0)
	s.statusCache.now = func() time.Time { return clk }
	s.docs.Open(uri, runtimeCfg)
	return s, uri, &clk
}

func TestRefreshStatus_PollReusesDriftVerdict(t *testing.T) {
	var drift_, rt atomic.Int64
	s, uri, clk := runtimeTestServer(t, &drift_, &rt)

	s.refreshStatus(uri, true) // first: recompute drift + runtime
	s.wg.Wait()

	*clk = clk.Add(pollThrottleWindow + time.Second) // clear the throttle window
	s.refreshStatus(uri, false)                      // a poll: runtime only, reuse the verdict
	s.wg.Wait()

	if drift_.Load() != 1 || rt.Load() != 1 {
		t.Fatalf("expected 1 drift recompute + 1 runtime-only, got drift=%d rt=%d", drift_.Load(), rt.Load())
	}
	got := s.statusCache.get(uri)["prod"]
	if got.Target != "prod-cluster" {
		t.Fatalf("runtime-only should carry the cached target over: %+v", got)
	}
	if len(got.Entries) != 1 || got.Entries[0].Runtime == nil || got.Entries[0].Runtime.State != remote.StateStopped {
		t.Fatalf("runtime-only should refresh live state to stopped: %+v", got.Entries)
	}
}

func TestRefreshStatus_DriftChangedRecomputesEvenWithCachedVerdict(t *testing.T) {
	var drift_, rt atomic.Int64
	s, uri, clk := runtimeTestServer(t, &drift_, &rt)

	s.refreshStatus(uri, true) // caches a verdict
	s.wg.Wait()

	*clk = clk.Add(pollThrottleWindow + time.Second)
	s.refreshStatus(uri, true) // a local change: recompute drift, not runtime-only
	s.wg.Wait()

	if drift_.Load() != 2 || rt.Load() != 0 {
		t.Fatalf("driftChanged should recompute drift even with a cached verdict, got drift=%d rt=%d", drift_.Load(), rt.Load())
	}
}

func TestRefreshStatus_ThrottlesRuntimeOnlyWithinWindow(t *testing.T) {
	var drift_, rt atomic.Int64
	s, uri, clk := runtimeTestServer(t, &drift_, &rt)

	s.refreshStatus(uri, true) // recompute drift, stamps the fetch start
	s.wg.Wait()

	s.refreshStatus(uri, false) // same instant: runtime-only candidate, throttled
	s.wg.Wait()
	if rt.Load() != 0 {
		t.Fatalf("a runtime-only poll within the throttle window should be skipped, got rt=%d", rt.Load())
	}

	*clk = clk.Add(pollThrottleWindow + time.Second)
	s.refreshStatus(uri, false) // window elapsed: runtime-only fires
	s.wg.Wait()
	if drift_.Load() != 1 || rt.Load() != 1 {
		t.Fatalf("expected 1 drift recompute + 1 runtime-only after the window, got drift=%d rt=%d", drift_.Load(), rt.Load())
	}
}

func TestRefreshStatus_UnauthenticatedEnvRecomputes(t *testing.T) {
	var drift_, rt atomic.Int64
	root := t.TempDir()
	uri := pathToURI(writeWorkspaceFile(t, root, "gaffer.toml", runtimeCfg))

	s := testServer(func(context.Context, string, *config.Config, string, string) envStatus {
		drift_.Add(1)
		return envStatus{Unauthenticated: true} // env needs sign-in - no reusable verdict
	})
	s.runtimeFetch = func(context.Context, string, *config.Config, string, string, envStatus) envStatus {
		rt.Add(1)
		return envStatus{}
	}
	clk := time.Unix(1000, 0)
	s.statusCache.now = func() time.Time { return clk }
	s.docs.Open(uri, runtimeCfg)

	s.refreshStatus(uri, true)
	s.wg.Wait()

	clk = clk.Add(pollThrottleWindow + time.Second)
	s.refreshStatus(uri, false) // a poll, but the env has no reusable verdict -> recompute
	s.wg.Wait()

	if drift_.Load() != 2 || rt.Load() != 0 {
		t.Fatalf("an unauthenticated env should recompute, never runtime-only, got drift=%d rt=%d", drift_.Load(), rt.Load())
	}
}

func TestReattachRuntime(t *testing.T) {
	cached := envStatus{
		Entries:    []drift.StatusEntry{rtEntry("p", remote.StateRunning), rtEntry("q", remote.StateRunning)},
		Target:     "prod-cluster",
		Production: true,
	}
	// p flips to stopped; q is absent from the live list (vanished).
	got := reattachRuntime(cached, []remote.Status{{Name: "p", State: remote.StateStopped}})

	if got.Target != "prod-cluster" || !got.Production {
		t.Fatalf("reattach should carry target/production over: %+v", got)
	}
	byName := map[string]drift.StatusEntry{}
	for _, e := range got.Entries {
		byName[e.Name] = e
	}
	if got.Entries == nil || len(byName) != 2 {
		t.Fatalf("reattach should keep every cached entry: %+v", got.Entries)
	}
	if byName["p"].Runtime.State != remote.StateStopped {
		t.Fatalf("p's runtime should refresh to stopped: %+v", byName["p"].Runtime)
	}
	if byName["q"].Runtime.State != remote.StateRunning {
		t.Fatalf("q vanished from live and should keep its cached runtime: %+v", byName["q"].Runtime)
	}
	if cached.Entries[0].Runtime.State != remote.StateRunning {
		t.Fatal("reattach must not mutate the cached entries")
	}
}
