package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

const integrationProjection = `fromCategory('order')
  .foreachStream()
  .when({
    $init: function() { return { count: 0, totalCents: 0 }; },
    OrderPlaced: function(state, event) {
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
	dir := t.TempDir()

	projDir := filepath.Join(dir, "projections")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "orders.js"), []byte(integrationProjection), 0o644); err != nil {
		t.Fatal(err)
	}

	fixtureDir := filepath.Join(dir, "fixtures")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "orders.json"), []byte(integrationFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Projection: []config.Projection{
			{Name: "orders", Entry: "projections/orders.js"},
		},
	}
	if err := config.Save(filepath.Join(dir, "gaffer.toml"), cfg); err != nil {
		t.Fatal(err)
	}

	return dir
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	_ = w.Close()

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	return buf.String()
}

func TestDev_FixtureJSON(t *testing.T) {
	dir := setupIntegrationProject(t)

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	devJSON = false
	devEvents = ""
	devConnection = ""
	devDebug = false
	devDebugPort = 4711

	rootCmd.SetArgs([]string{"dev", "orders", "--events", "fixtures/orders.json", "--json"})
	rootCmd.SetErr(&bytes.Buffer{})

	output := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("command failed: %v", err)
		}
	})

	lines := splitNDJSON(output)
	if len(lines) == 0 {
		t.Fatalf("no output, raw: %q", output)
	}

	summary := lines[len(lines)-1]
	if summary["type"] != "summary" {
		t.Fatalf("expected last line to be summary, got: %v", summary)
	}
	if summary["handled"].(float64) != 3 {
		t.Errorf("handled: got %v, want 3", summary["handled"])
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

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	infoJSON = false

	rootCmd.SetArgs([]string{"info", "orders", "--json"})
	rootCmd.SetErr(&bytes.Buffer{})

	output := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
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
	if info["source"] != "category" {
		t.Errorf("source: got %v, want category", info["source"])
	}
}

func TestEndToEnd_InitScaffoldDev(t *testing.T) {
	dir := t.TempDir()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(orig) }()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	stderr := &bytes.Buffer{}
	rootCmd.SetErr(stderr)

	// 1. init
	initYes = false
	rootCmd.SetArgs([]string{"init", "--yes"})
	captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
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
	scaffoldSource = "all"
	scaffoldPartition = "none"
	scaffoldEmit = false
	rootCmd.SetArgs([]string{"scaffold", "counter"})
	captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("scaffold failed: %v", err)
		}
	})

	if _, err := os.Stat(filepath.Join(dir, "projections", "counter.js")); err != nil {
		t.Fatal("expected projections/counter.js after scaffold")
	}

	// Verify config was updated
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
	devJSON = false
	devEvents = ""
	devConnection = ""
	devDebug = false
	devDebugPort = 4711
	rootCmd.SetArgs([]string{"dev", "counter", "--events", "fixtures/events.json", "--json"})

	output := captureStdout(t, func() {
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("dev failed: %v", err)
		}
	})

	lines := splitNDJSON(output)
	if len(lines) == 0 {
		t.Fatalf("no output from dev")
	}

	summary := lines[len(lines)-1]
	if summary["type"] != "summary" {
		t.Fatalf("expected summary, got: %v", summary)
	}
	total := summary["handled"].(float64) + summary["skipped"].(float64)
	if total != 1 {
		t.Errorf("expected 1 event processed, got handled=%v skipped=%v", summary["handled"], summary["skipped"])
	}
}

func splitNDJSON(s string) []map[string]any {
	var results []map[string]any
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	for dec.More() {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			break
		}
		results = append(results, obj)
	}
	return results
}
