package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
)

const integrationProjection = `fromCategory('order')
  .foreachStream()
  .when({
    $init() { return { count: 0, totalCents: 0 }; },
    OrderPlaced(state, event) {
      state.count++;
      state.totalCents += event.data.cents;
      return state;
    }
  })
`

const integrationFixture = `[
  { "eventType": "OrderPlaced", "streamId": "order-1", "data": "{\"cents\": 2999}" },
  { "eventType": "OrderPlaced", "streamId": "order-2", "data": "{\"cents\": 4999}" },
  { "eventType": "OrderPlaced", "streamId": "order-1", "data": "{\"cents\": 1500}" }
]`

func setupIntegrationProject(t *testing.T) string {
	t.Helper()
	p := testutil.NewProject(t).
		AddProjection("orders", integrationProjection).
		AddFixture("orders", integrationFixture).
		Save()
	return p.Dir
}

func chdirTo(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

func TestDev_FixtureJSON(t *testing.T) {
	dir := setupIntegrationProject(t)
	chdirTo(t, dir)

	root := NewRootCmd()
	root.SetArgs([]string{"dev", "orders", "--events", "fixtures/orders.json", "--json"})
	root.SetErr(&bytes.Buffer{})

	output := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("command failed: %v", err)
		}
	})

	lines := testutil.SplitNDJSON(output)
	if len(lines) == 0 {
		t.Fatalf("no output, raw: %q", output)
	}

	summary := lines[len(lines)-1]
	if summary["type"] != "summary" {
		t.Fatalf("expected last line to be summary, got: %v", summary)
	}
	handled, ok := summary["handled"].(float64)
	if !ok {
		t.Fatalf("expected handled to be float64, got %T", summary["handled"])
	}
	if handled != 3 {
		t.Errorf("handled: got %v, want 3", handled)
	}

	partitions, ok := summary["partitions"].(map[string]any)
	if !ok {
		t.Fatalf("expected partitions map, got: %T", summary["partitions"])
	}
	if len(partitions) != 2 {
		t.Errorf("expected 2 partitions, got %d", len(partitions))
	}
}

func TestInfo_JSON(t *testing.T) {
	dir := setupIntegrationProject(t)
	chdirTo(t, dir)

	root := NewRootCmd()
	root.SetArgs([]string{"info", "orders", "--json"})
	root.SetErr(&bytes.Buffer{})

	output := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("command failed: %v", err)
		}
	})

	var info map[string]any
	if err := json.Unmarshal([]byte(output), &info); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %q", err, output)
	}
	if info["name"] != "orders" {
		t.Errorf("name: got %v, want orders", info["name"])
	}
	if info["source"] != "categories" {
		t.Errorf("source: got %v, want categories", info["source"])
	}
}

func TestEndToEnd_InitScaffoldDev(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir)

	// 1. init
	initRoot := NewRootCmd()
	initRoot.SetArgs([]string{"init", "--yes"})
	initRoot.SetErr(&bytes.Buffer{})
	testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), initRoot); err != nil {
			t.Fatalf("init failed: %v", err)
		}
	})

	if _, err := os.Stat(filepath.Join(dir, "gaffer.toml")); err != nil {
		t.Fatal("expected gaffer.toml after init")
	}
	if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
		t.Fatal("expected .gitignore after init")
	}

	// 2. scaffold
	scaffoldRoot := NewRootCmd()
	scaffoldRoot.SetArgs([]string{"scaffold", "counter"})
	scaffoldRoot.SetErr(&bytes.Buffer{})
	testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), scaffoldRoot); err != nil {
			t.Fatalf("scaffold failed: %v", err)
		}
	})

	if _, err := os.Stat(filepath.Join(dir, "projections", "counter.js")); err != nil {
		t.Fatal("expected projections/counter.js after scaffold")
	}

	cfg, err := config.Load(filepath.Join(dir, "gaffer.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FindProjection("counter") == nil {
		t.Fatal("expected counter in config after scaffold")
	}

	// 3. Write a fixture file
	fixtureDir := filepath.Join(dir, "fixtures")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "events.json"), []byte(`[
		{"eventType":"Ping","streamId":"s-1","data":"{}"}
	]`), 0o644); err != nil {
		t.Fatal(err)
	}

	// 4. dev with fixture
	devRoot := NewRootCmd()
	devRoot.SetArgs([]string{"dev", "counter", "--events", "fixtures/events.json", "--json"})
	devRoot.SetErr(&bytes.Buffer{})

	output := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), devRoot); err != nil {
			t.Fatalf("dev failed: %v", err)
		}
	})

	lines := testutil.SplitNDJSON(output)
	if len(lines) == 0 {
		t.Fatalf("no output from dev")
	}

	summary := lines[len(lines)-1]
	if summary["type"] != "summary" {
		t.Fatalf("expected summary, got: %v", summary)
	}
	handled2, ok := summary["handled"].(float64)
	if !ok {
		t.Fatalf("expected handled to be float64, got %T", summary["handled"])
	}
	skipped, ok := summary["skipped"].(float64)
	if !ok {
		t.Fatalf("expected skipped to be float64, got %T", summary["skipped"])
	}
	if total := handled2 + skipped; total != 1 {
		t.Errorf("expected 1 event processed, got handled=%v skipped=%v", handled2, skipped)
	}
}
