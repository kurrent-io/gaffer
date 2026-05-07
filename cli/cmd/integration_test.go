package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestDev_FixtureFlag(t *testing.T) {
	// Resolves a named fixture from gaffer.toml via --fixture <name>.
	p := testutil.NewProject(t).
		AddProjection("orders", integrationProjection).
		AddNamedFixture("orders", "happy", integrationFixture).
		Save()
	chdirTo(t, p.Dir)

	root := NewRootCmd()
	root.SetArgs([]string{"dev", "orders", "--fixture", "happy", "--json"})
	root.SetErr(&bytes.Buffer{})

	output := testutil.CaptureStdout(t, func() {
		if err := ExecuteRoot(context.Background(), root); err != nil {
			t.Fatalf("command failed: %v", err)
		}
	})

	lines := testutil.SplitNDJSON(output)
	if len(lines) == 0 {
		t.Fatalf("no output")
	}
	summary := lines[len(lines)-1]
	if summary["type"] != "summary" {
		t.Fatalf("expected summary, got: %v", summary)
	}
	if h, _ := summary["handled"].(float64); h != 3 {
		t.Errorf("handled: got %v, want 3", summary["handled"])
	}
}

func TestDev_FixtureFlag_UnknownName(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("orders", integrationProjection).
		AddNamedFixture("orders", "happy", integrationFixture).
		Save()
	chdirTo(t, p.Dir)

	root := NewRootCmd()
	root.SetArgs([]string{"dev", "orders", "--fixture", "nope"})
	var stderr bytes.Buffer
	root.SetErr(&stderr)

	err := ExecuteRoot(context.Background(), root)
	if err == nil {
		t.Fatal("expected error for unknown fixture")
	}
	if !strings.Contains(err.Error(), "no fixture named \"nope\"") {
		t.Errorf("error should mention the bad name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "happy") {
		t.Errorf("error should list available fixtures, got: %v", err)
	}
}

func TestDev_FixtureFlag_NoneDeclared(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("orders", integrationProjection).
		Save()
	chdirTo(t, p.Dir)

	root := NewRootCmd()
	root.SetArgs([]string{"dev", "orders", "--fixture", "anything"})
	root.SetErr(&bytes.Buffer{})

	err := ExecuteRoot(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "no fixtures declared") {
		t.Fatalf("expected no-fixtures error, got: %v", err)
	}
}

func TestDev_FixtureAndEventsMutuallyExclusive(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("orders", integrationProjection).
		AddNamedFixture("orders", "happy", integrationFixture).
		Save()
	chdirTo(t, p.Dir)

	root := NewRootCmd()
	root.SetArgs([]string{"dev", "orders", "--fixture", "happy", "--events", "fixtures/happy.json"})
	root.SetErr(&bytes.Buffer{})

	err := ExecuteRoot(context.Background(), root)
	if err == nil {
		t.Fatal("expected mutex error")
	}
	if !strings.Contains(err.Error(), "only one of --events or --fixture") {
		t.Errorf("expected mutex error, got: %v", err)
	}
}

func TestDev_NoSource_ErrorMentionsFixture(t *testing.T) {
	// No --fixture, no --events, no connection: error should
	// mention --fixture as a valid option.
	p := testutil.NewProject(t).
		AddProjection("orders", integrationProjection).
		Save()
	chdirTo(t, p.Dir)

	root := NewRootCmd()
	root.SetArgs([]string{"dev", "orders"})
	root.SetErr(&bytes.Buffer{})

	err := ExecuteRoot(context.Background(), root)
	if err == nil {
		t.Fatal("expected no-source error")
	}
	if !strings.Contains(err.Error(), "--fixture") {
		t.Errorf("expected error to mention --fixture, got: %v", err)
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

func TestInfo_JSON_FixturesField(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("orders", integrationProjection).
		AddNamedFixture("orders", "happy", integrationFixture).
		AddNamedFixture("orders", "edge", integrationFixture).
		Save()
	chdirTo(t, p.Dir)

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
		t.Fatalf("failed to parse JSON: %v", err)
	}
	fixtures, ok := info["fixtures"].([]any)
	if !ok {
		t.Fatalf("expected fixtures array, got %T", info["fixtures"])
	}
	if len(fixtures) != 2 {
		t.Fatalf("expected 2 fixtures, got %d", len(fixtures))
	}
	first, ok := fixtures[0].(map[string]any)
	if !ok {
		t.Fatalf("expected fixture object, got %T", fixtures[0])
	}
	// Output is sorted alphabetically; "edge" comes before "happy".
	if first["name"] != "edge" {
		t.Errorf("fixtures[0].name: got %v, want edge", first["name"])
	}
	if first["path"] != "fixtures/edge.json" {
		t.Errorf("fixtures[0].path: got %v, want fixtures/edge.json", first["path"])
	}
}

func TestInfo_JSON_NoFixturesFieldWhenNoneDeclared(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("orders", integrationProjection).
		Save()
	chdirTo(t, p.Dir)

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
		t.Fatalf("failed to parse JSON: %v", err)
	}
	if _, present := info["fixtures"]; present {
		t.Errorf("expected no fixtures field, got %v", info["fixtures"])
	}
}

// 3-arg linkStreamTo always throws via the upstream out-of-bounds-parameters
// bug, so these tests exercise the full compat-error path: runtime tags the
// throw with KnownBugs.LinkStreamToOutOfBoundsParameters.Code, error flows
// through Go bindings, dev's writer renders it with the compatCode field
// (--json) or the "Compat:" block (text).
const compatLinkStreamToProjection = `fromAll().when({
  $any: function (s, e) { linkStreamTo("archive", e.streamId, { reason: "x" }); return s; }
})`

const compatLinkStreamToFixture = `[
  { "eventType": "Trigger", "streamId": "trigger-1", "data": "{}" }
]`

func TestDev_FatalError_CompatCodeRoundTrips(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("archive", compatLinkStreamToProjection).
		AddFixture("archive", compatLinkStreamToFixture).
		Save()
	chdirTo(t, p.Dir)

	root := NewRootCmd()
	root.SetArgs([]string{"dev", "archive", "--events", "fixtures/archive.json", "--json"})
	root.SetErr(&bytes.Buffer{})

	output := testutil.CaptureStdout(t, func() {
		_ = ExecuteRoot(context.Background(), root)
	})

	lines := testutil.SplitNDJSON(output)
	if len(lines) == 0 {
		t.Fatalf("no output, raw: %q", output)
	}

	var fatal map[string]any
	for _, line := range lines {
		if line["type"] == "fatal_error" {
			fatal = line
			break
		}
	}
	if fatal == nil {
		t.Fatalf("expected a fatal_error event in output, got: %v", lines)
	}
	if got := fatal["compatCode"]; got != "compat.linkStreamTo.outOfBoundsParameters" {
		t.Errorf("compatCode: got %v, want compat.linkStreamTo.outOfBoundsParameters", got)
	}
}

func TestDev_FatalError_CompatBlockInText(t *testing.T) {
	p := testutil.NewProject(t).
		AddProjection("archive", compatLinkStreamToProjection).
		AddFixture("archive", compatLinkStreamToFixture).
		Save()
	chdirTo(t, p.Dir)

	root := NewRootCmd()
	root.SetArgs([]string{"dev", "archive", "--events", "fixtures/archive.json"})

	_, stderr := testutil.CaptureStdio(t, func() {
		_ = ExecuteRoot(context.Background(), root)
	})

	if !strings.Contains(stderr, "Compat:") {
		t.Errorf("expected stderr to contain 'Compat:' block, got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "compat.linkStreamTo.outOfBoundsParameters") {
		t.Errorf("expected stderr to contain compat code, got:\n%s", stderr)
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
