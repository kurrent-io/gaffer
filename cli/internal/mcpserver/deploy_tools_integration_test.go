//go:build integration

package mcpserver

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

// setupDeployToolsProject builds a project whose single projection has a
// unique name, so runs against the shared integration server can't collide,
// and returns the server plus a remote client for out-of-band writes.
func setupDeployToolsProject(t *testing.T) (*Server, *remote.Client, string) {
	t.Helper()
	suffix := testutil.TestSuffix()
	name := "depltool" + suffix

	projSource := fmt.Sprintf(`fromCategory('depltool%s')
  .foreachStream()
  .when({
    $init() { return { count: 0 }; },
    Ping(s, e) { s.count++; return s; }
  })
`, suffix)

	p := testutil.NewProject(t).
		WithConnection(testutil.ConnectionString()).
		AddProjection(name, projSource).
		Save()

	s := New(p.Dir, p.Cfg, "test")
	t.Cleanup(func() {
		s.mu.Lock()
		s.closeSession()
		s.mu.Unlock()
	})

	client, _, cleanup, err := s.connectRemote(p.Cfg, p.Dir, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	return s, client, name
}

func TestIntegration_DeployTools(t *testing.T) {
	s, client, name := setupDeployToolsProject(t)

	// Before anything is deployed: the plan creates, status reads not-deployed,
	// history has nothing to read.
	plan := callTool(t, s, deployPlanTool, s.handleDeployPlan, deployPlanInput{Name: name})
	items := plan["plan"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 plan item, got %d", len(items))
	}
	if outcome := items[0].(map[string]any)["outcome"]; outcome != "created" {
		t.Fatalf("expected outcome=created, got %v (item %v)", outcome, items[0])
	}
	if plan["changes"].(float64) != 1 {
		t.Fatalf("expected changes=1, got %v", plan["changes"])
	}
	if plan["verdict"] != "deployable" {
		t.Fatalf("expected verdict=deployable for a pending create, got %v", plan["verdict"])
	}

	if plan["env"] != "default" {
		t.Fatalf("expected the resolved env echoed, got %v", plan["env"])
	}

	status := callTool(t, s, deployStatusTool, s.handleDeployStatus, deployStatusInput{Name: name})
	if status["env"] != "default" {
		t.Fatalf("expected the resolved env echoed, got %v", status["env"])
	}
	if status["production"] != false || status["target"] == "" {
		t.Fatalf("expected target and production echoed, got target=%v production=%v", status["target"], status["production"])
	}
	projs := status["projections"].([]any)
	if len(projs) != 1 {
		t.Fatalf("expected 1 status entry, got %d", len(projs))
	}
	if d := projs[0].(map[string]any)["drift"]; d != "not-deployed" {
		t.Fatalf("expected drift=not-deployed, got %v", d)
	}

	if msg := callToolExpectError(t, s.handleDeployHistory, deployHistoryInput{Name: name}); msg == "" {
		t.Fatal("expected a not-deployed error from deploy_history")
	}

	// Create the projection out of band (no gaffer metadata), then the tools
	// see a live, untracked-by-hash definition: status carries runtime, the
	// plan proposes an update or skip (never create), and history classifies
	// the metadata-less first version as created.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Create(ctx, name, "fromAll().when({})", remote.CreateOptions{EngineVersion: 2}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = client.Disable(ctx, name)
		_ = client.Delete(ctx, name, remote.DeleteOptions{})
	})

	status = callTool(t, s, deployStatusTool, s.handleDeployStatus, deployStatusInput{Name: name})
	entry := status["projections"].([]any)[0].(map[string]any)
	if d := entry["drift"]; d != "drifted" {
		t.Fatalf("expected drift=drifted after the out-of-band create, got %v", d)
	}
	if entry["runtime"] == nil {
		t.Fatal("expected runtime state for a deployed projection")
	}

	plan = callTool(t, s, deployPlanTool, s.handleDeployPlan, deployPlanInput{Name: name})
	if outcome := plan["plan"].([]any)[0].(map[string]any)["outcome"]; outcome != "updated" {
		t.Fatalf("expected outcome=updated for a drifted projection, got %v", outcome)
	}

	hist := callTool(t, s, deployHistoryTool, s.handleDeployHistory, deployHistoryInput{Name: name})
	if hist["env"] != "default" {
		t.Fatalf("expected the resolved env echoed, got %v", hist["env"])
	}
	versions := hist["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(versions))
	}
	v0 := versions[0].(map[string]any)
	if v0["version"].(float64) != 0 || v0["kind"] != "created" {
		t.Fatalf("expected version 0 kind=created, got %v", v0)
	}
	if v0["contentHash"] == "" {
		t.Fatal("expected a content hash on the created version")
	}
	if hist["total"].(float64) != 1 {
		t.Fatalf("expected total=1, got %v", hist["total"])
	}

	// A second write, then page with limit 1: the head page carries the total
	// and classifies its last entry against the trimmed baseline (the update
	// is metadata-less, so a correct baseline reads it as edited externally,
	// not the no-baseline "rewritten" fallback); the before page is strictly
	// older and, not being a head read, omits the total.
	if err := client.Update(ctx, name, "fromAll().when({ $init() { return {}; } })", remote.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}

	hist = callTool(t, s, deployHistoryTool, s.handleDeployHistory, deployHistoryInput{Name: name, Limit: 1})
	versions = hist["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("head page: expected 1 entry under limit=1, got %d", len(versions))
	}
	head := versions[0].(map[string]any)
	if head["version"].(float64) != 1 || head["kind"] != "edited-externally" {
		t.Fatalf("head page entry = %v, want version 1 classified against the baseline", head)
	}
	if hist["total"].(float64) != 2 {
		t.Fatalf("head page: expected total=2, got %v", hist["total"])
	}

	before := int64(1)
	hist = callTool(t, s, deployHistoryTool, s.handleDeployHistory, deployHistoryInput{Name: name, Limit: 1, Before: &before})
	versions = hist["versions"].([]any)
	if len(versions) != 1 || versions[0].(map[string]any)["version"].(float64) != 0 {
		t.Fatalf("before page: expected just version 0, got %v", versions)
	}
	if _, ok := hist["total"]; ok {
		t.Fatalf("before page: total should be omitted on a paged read, got %v", hist["total"])
	}
}
