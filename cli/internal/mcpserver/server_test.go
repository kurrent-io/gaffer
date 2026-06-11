package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/history"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testProjection = `fromCategory('order')
  .foreachStream()
  .when({
    $init() {
      return { count: 0, totalCents: 0 };
    },
    OrderPlaced(state, event) {
      state.count++;
      state.totalCents += event.data.cents;
      return state;
    },
    OrderShipped(state, event) {
      state.shipped = true;
      return state;
    }
  })
`

const testFixture = `[
  { "eventType": "OrderPlaced", "streamId": "order-1", "data": "{\"cents\": 2999}" },
  { "eventType": "OrderPlaced", "streamId": "order-2", "data": "{\"cents\": 4999}" },
  { "eventType": "OrderShipped", "streamId": "order-1", "data": "{}" },
  { "eventType": "OrderPlaced", "streamId": "order-1", "data": "{\"cents\": 1500}" },
  { "eventType": "OrderPlaced", "streamId": "order-3", "data": "{\"cents\": 9999}" }
]`

const testBrokenProjection = `fromAll().when({
    $init() { return {}; },
    $any(state, event) {
        throw new Error("intentional error");
    }
})`

func setupTestProject(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	var s *Server
	t.Cleanup(func() {
		if s != nil {
			s.mu.Lock()
			s.closeSession()
			s.mu.Unlock()
		}
	})

	projDir := filepath.Join(dir, "projections")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(projDir, "order-count.js"), []byte(testProjection), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "broken.js"), []byte(testBrokenProjection), 0o644); err != nil {
		t.Fatal(err)
	}

	fixtureDir := filepath.Join(dir, "fixtures")
	if err := os.MkdirAll(fixtureDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, "orders.json"), []byte(testFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Projection: []config.Projection{
			{Name: "order-count", Entry: "projections/order-count.js", EngineVersion: ptr(2)},
			{Name: "broken", Entry: "projections/broken.js", EngineVersion: ptr(2)},
		},
	}

	configPath := filepath.Join(dir, "gaffer.toml")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	s = New(dir, cfg, "test")
	return s
}

// setupTestProjectWithEnv is setupTestProject plus a default [env.local],
// so the server-touching tools have an environment to resolve - used to
// prove the per-call env arg reaches resolution.
func setupTestProjectWithEnv(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	projDir := filepath.Join(dir, "projections")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "order-count.js"), []byte(testProjection), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Env: map[string]config.Env{
			"local": {Connection: "kurrentdb://localhost:2113?tls=false", Default: true},
		},
		Projection: []config.Projection{
			{Name: "order-count", Entry: "projections/order-count.js", EngineVersion: ptr(2)},
		},
	}
	if err := config.Save(filepath.Join(dir, "gaffer.toml"), cfg); err != nil {
		t.Fatal(err)
	}
	s := New(dir, cfg, "test")
	t.Cleanup(func() {
		s.mu.Lock()
		s.closeSession()
		s.mu.Unlock()
	})
	return s
}

// An unknown env name is rejected at resolution, before any connection
// attempt, proving the tool's env arg is threaded to ResolveEnv rather
// than silently falling back to the default env.
func TestListEvents_UnknownEnv(t *testing.T) {
	s := setupTestProjectWithEnv(t)
	msg := callToolExpectError(t, s.handleListEvents, listEventsInput{Name: "order-count", Env: "ghost"})
	if !strings.Contains(msg, "unknown environment") || !strings.Contains(msg, "ghost") {
		t.Errorf("expected unknown-env error naming 'ghost', got %q", msg)
	}
}

func TestRun_LiveUnknownEnv(t *testing.T) {
	s := setupTestProjectWithEnv(t)
	msg := callToolExpectError(t, s.handleRun, runInput{Name: "order-count", Env: "ghost"})
	if !strings.Contains(msg, "unknown environment") || !strings.Contains(msg, "ghost") {
		t.Errorf("expected unknown-env error naming 'ghost', got %q", msg)
	}
}

func callTool[In, Out any](t *testing.T, s *Server, tool *mcp.Tool, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error), input In) map[string]any {
	t.Helper()
	result, _, err := handler(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("%s returned protocol error: %v", tool.Name, err)
	}
	if result.IsError {
		t.Fatalf("%s returned tool error: %s", tool.Name, result.Content[0].(*mcp.TextContent).Text)
	}
	var data map[string]any
	text := result.Content[0].(*mcp.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("%s: failed to parse result JSON: %v", tool.Name, err)
	}
	return data
}

func callToolExpectError[In, Out any](t *testing.T, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error), input In) string {
	t.Helper()
	result, _, err := handler(context.Background(), nil, input)
	if err != nil {
		t.Fatalf("expected tool error, got protocol error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected tool error, got success")
	}
	return result.Content[0].(*mcp.TextContent).Text
}

// --- List projections ---

func TestListProjections(t *testing.T) {
	s := setupTestProject(t)
	result := callTool(t, s, listProjectionsTool, s.handleListProjections, listProjectionsInput{})

	projections := result["projections"].([]any)
	if len(projections) != 2 {
		t.Fatalf("expected 2 projections, got %d", len(projections))
	}
}

// --- Validate ---

func TestValidate_Valid(t *testing.T) {
	s := setupTestProject(t)
	result := callTool(t, s, validateTool, s.handleValidate, validateInput{Name: "order-count"})

	if result["valid"] != true {
		t.Fatal("expected valid=true")
	}
	if result["partitioning"] != "byStream" {
		t.Errorf("expected partitioning=byStream, got %v", result["partitioning"])
	}
}

func TestValidate_NotFound(t *testing.T) {
	s := setupTestProject(t)
	msg := callToolExpectError(t, s.handleValidate, validateInput{Name: "nonexistent"})
	if msg == "" {
		t.Fatal("expected error message")
	}
}

// --- Run fixture mode ---

func TestRun_Fixture(t *testing.T) {
	s := setupTestProject(t)
	result := callTool(t, s, runTool, s.handleRun, runInput{
		Name:   "order-count",
		Events: "fixtures/orders.json",
	})

	if result["completed"] != true {
		t.Errorf("expected completed=true, got %v", result["completed"])
	}
	if result["processed"].(float64) != 5 {
		t.Errorf("expected processed=5, got %v", result["processed"])
	}

	partitions := result["partitions"].(map[string]any)
	if len(partitions) != 3 {
		t.Errorf("expected 3 partitions, got %d", len(partitions))
	}
}

func TestRun_FixtureError(t *testing.T) {
	s := setupTestProject(t)
	result := callTool(t, s, runTool, s.handleRun, runInput{
		Name:   "broken",
		Events: "fixtures/orders.json",
	})

	if result["lastError"] == nil {
		t.Error("expected lastError")
	}
}

func TestRun_MissingEvents(t *testing.T) {
	s := setupTestProject(t)
	msg := callToolExpectError(t, s.handleRun, runInput{
		Name:   "order-count",
		Events: "fixtures/nonexistent.json",
	})
	if msg == "" {
		t.Fatal("expected error")
	}
}

// --- Session lifecycle ---

func TestRun_ReplacesSession(t *testing.T) {
	s := setupTestProject(t)

	r1 := callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})
	if r1["completed"] != true {
		t.Fatal("expected first run to complete")
	}

	r2 := callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})
	if r2["completed"] != true {
		t.Fatal("expected second run to complete")
	}
}

func TestStop(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})
	callTool(t, s, stopTool, s.handleStop, stopInput{})

	// After stop, inspection tools should error
	callToolExpectError(t, s.handleGetStep, getStepInput{Step: 1})
}

// --- Inspection tools ---

func TestGetStep(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})

	result := callTool(t, s, getStepTool, s.handleGetStep, getStepInput{Step: 1})
	if result["eventType"] != "OrderPlaced" {
		t.Errorf("expected eventType=OrderPlaced, got %v", result["eventType"])
	}
	if result["streamId"] != "order-1" {
		t.Errorf("expected streamId=order-1, got %v", result["streamId"])
	}
}

func TestFormatStep_PromotesDiagnostics(t *testing.T) {
	withDiag := &history.Step{
		Index:      1,
		EventJSON:  `{"eventType":"Tick","streamId":"s-1"}`,
		ResultJSON: `{"status":"processed","diagnostics":[{"code":"quirk.serialize.rawString","message":"m","severity":2,"range":null}]}`,
		EventType:  "Tick",
		Status:     "processed",
	}
	diags, ok := formatStep(withDiag)["diagnostics"].([]any)
	if !ok || len(diags) != 1 {
		t.Fatalf("expected 1 promoted diagnostic, got %v", formatStep(withDiag)["diagnostics"])
	}
	if code := diags[0].(map[string]any)["code"]; code != "quirk.serialize.rawString" {
		t.Errorf("promoted diagnostic code = %v, want quirk.serialize.rawString", code)
	}

	noDiag := &history.Step{Index: 1, ResultJSON: `{"status":"processed","diagnostics":[]}`}
	if _, present := formatStep(noDiag)["diagnostics"]; present {
		t.Error("expected diagnostics key omitted when none fired")
	}
}

func TestGetStep_NoSession(t *testing.T) {
	s := setupTestProject(t)
	callToolExpectError(t, s.handleGetStep, getStepInput{Step: 1})
}

func TestGetStep_InvalidStep(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})
	callToolExpectError(t, s.handleGetStep, getStepInput{Step: 999})
}

func TestGetTimeline(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})

	result := callTool(t, s, getTimelineTool, s.handleGetTimeline, getTimelineInput{})

	entries := result["entries"].([]any)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}
}

func TestGetTimeline_PartitionFilter(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})

	result := callTool(t, s, getTimelineTool, s.handleGetTimeline, getTimelineInput{Partition: "order-1"})

	entries := result["entries"].([]any)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries for order-1, got %d", len(entries))
	}
}

func TestGetHistory(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})

	result := callTool(t, s, getHistoryTool, s.handleGetHistory, getHistoryInput{From: 1, To: 3})

	steps := result["steps"].([]any)
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	if result["after"] == nil {
		t.Error("expected after state")
	}
}

func TestGetState(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})

	result := callTool(t, s, getStateTool, s.handleGetState, getStateInput{})
	if result["partitions"] == nil {
		t.Error("expected partitions in state")
	}

	result = callTool(t, s, getStateTool, s.handleGetState, getStateInput{Partition: "order-1"})
	if result["partition"] != "order-1" {
		t.Errorf("expected partition=order-1, got %v", result["partition"])
	}
}

// --- Scaffold ---

func TestScaffold(t *testing.T) {
	s := setupTestProject(t)
	result := callTool(t, s, scaffoldTool, s.handleScaffold, scaffoldInput{
		Path: "projections/new-proj.js",
	})

	if result["name"] != "new-proj" {
		t.Errorf("expected name=new-proj, got %v", result["name"])
	}

	// File should exist
	path := filepath.Join(s.root, result["created"].(string))
	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected file at %s: %v", path, err)
	}

	// Config should have new projection
	if s.cfg.FindProjection("new-proj") == nil {
		t.Error("expected new-proj in config")
	}
}

func TestScaffold_EngineVersion(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, scaffoldTool, s.handleScaffold, scaffoldInput{
		Path:          "projections/legacy.js",
		EngineVersion: 1,
	})

	proj := s.cfg.FindProjection("legacy")
	if proj == nil || proj.EngineVersion == nil || *proj.EngineVersion != 1 {
		t.Fatalf("expected engine_version 1, got %v", proj)
	}
}

func TestScaffold_EngineVersionDefaultsTo2(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, scaffoldTool, s.handleScaffold, scaffoldInput{Path: "projections/default-ev.js"})

	proj := s.cfg.FindProjection("default-ev")
	if proj == nil || proj.EngineVersion == nil || *proj.EngineVersion != config.DefaultEngineVersion {
		t.Fatalf("expected default engine_version %d, got %v", config.DefaultEngineVersion, proj)
	}
}

func TestScaffold_EngineVersionInvalid(t *testing.T) {
	s := setupTestProject(t)
	msg := callToolExpectError(t, s.handleScaffold, scaffoldInput{
		Path:          "projections/bad.js",
		EngineVersion: 5,
	})
	if !strings.Contains(msg, "engine_version") {
		t.Errorf("expected engine_version error, got %q", msg)
	}
}

func TestScaffold_ExplicitName(t *testing.T) {
	// Caller picks a file name distinct from the gaffer.toml key.
	s := setupTestProject(t)
	result := callTool(t, s, scaffoldTool, s.handleScaffold, scaffoldInput{
		Path: "projections/totals.js",
		Name: "order-totals",
	})

	if result["name"] != "order-totals" {
		t.Errorf("expected name=order-totals, got %v", result["name"])
	}
	if s.cfg.FindProjection("order-totals") == nil {
		t.Error("expected order-totals in config")
	}
}

func TestScaffold_Duplicate(t *testing.T) {
	s := setupTestProject(t)
	callToolExpectError(t, s.handleScaffold, scaffoldInput{
		Path: "projections/order-count.js",
	})
}

func TestScaffold_PathTraversal(t *testing.T) {
	s := setupTestProject(t)
	callToolExpectError(t, s.handleScaffold, scaffoldInput{Path: "../escape.js"})
}

func TestScaffold_AbsolutePath(t *testing.T) {
	// MCP doesn't have the CLI's cwd-resolution wrapper; the
	// validator is the only line of defence against absolute paths.
	s := setupTestProject(t)
	callToolExpectError(t, s.handleScaffold, scaffoldInput{Path: "/etc/escape.js"})
}

func TestScaffold_WindowsDrivePath(t *testing.T) {
	// LLMs trained on Windows paths can hit a Linux-hosted MCP
	// server. The drive-letter prefix has to be rejected regardless
	// of host OS - filepath.IsAbs on Linux won't catch it.
	s := setupTestProject(t)
	callToolExpectError(t, s.handleScaffold, scaffoldInput{Path: "C:\\tmp\\counter.js"})
}

func TestScaffold_MissingPath(t *testing.T) {
	// scaffold.Scaffold's validator owns the "path is required"
	// error; the MCP handler is a passthrough.
	s := setupTestProject(t)
	callToolExpectError(t, s.handleScaffold, scaffoldInput{Name: "no-path"})
}

func TestScaffold_BackslashPath(t *testing.T) {
	// LLM trained on Windows paths sends a backslashed path; the
	// validator normalises it to slash-form before storing.
	s := setupTestProject(t)
	result := callTool(t, s, scaffoldTool, s.handleScaffold, scaffoldInput{
		Path: "projections\\winproj.js",
	})
	if result["created"] != "projections/winproj.js" {
		t.Errorf("created: got %v, want projections/winproj.js", result["created"])
	}
}

// --- Debug (break_at via run) ---

func TestRun_BreakAt(t *testing.T) {
	s := setupTestProject(t)

	// run with break_at now blocks until breakpoint is hit
	result := callTool(t, s, runTool, s.handleRun, runInput{
		Name:    "order-count",
		Events:  "fixtures/orders.json",
		BreakAt: 3,
	})

	if result["paused"] != true {
		t.Fatalf("expected paused=true, got %v", result["paused"])
	}

	evalResult := callTool(t, s, evaluateTool, s.handleEvaluate, evaluateInput{Expression: "event.eventType"})
	if evalResult["value"] != "\"OrderShipped\"" {
		t.Errorf("expected OrderShipped, got %v", evalResult["value"])
	}

	// debug_continue now blocks until next break or completion
	contResult := callTool(t, s, debugContinueTool, s.handleDebugContinue, debugContinueInput{})
	if contResult["completed"] != true {
		t.Errorf("expected completed=true, got %v", contResult["completed"])
	}
}

func TestRun_Breakpoints(t *testing.T) {
	s := setupTestProject(t)

	// run with breakpoints blocks until first breakpoint
	result := callTool(t, s, runTool, s.handleRun, runInput{
		Name:        "order-count",
		Events:      "fixtures/orders.json",
		Breakpoints: []breakpointInput{{Line: 9}},
	})

	if result["paused"] != true {
		t.Fatalf("expected paused=true, got %v", result["paused"])
	}

	// Continue past all breakpoints until completed
	for i := 0; i < 20; i++ {
		contResult := callTool(t, s, debugContinueTool, s.handleDebugContinue, debugContinueInput{})
		if contResult["completed"] == true {
			return
		}
		if contResult["paused"] != true {
			t.Fatalf("expected paused or completed, got %v", contResult)
		}
	}

	t.Fatal("never reached completed status")
}

// --- Evaluate without session ---

func TestEvaluate_NoSession(t *testing.T) {
	s := setupTestProject(t)
	callToolExpectError(t, s.handleEvaluate, evaluateInput{Expression: "1+1"})
}

func TestEvaluate_NotPaused(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})
	callToolExpectError(t, s.handleEvaluate, evaluateInput{Expression: "1+1"})
}

// --- Step tools ---

func TestStepOver(t *testing.T) {
	s := setupTestProject(t)

	runResult := callTool(t, s, runTool, s.handleRun, runInput{
		Name:    "order-count",
		Events:  "fixtures/orders.json",
		BreakAt: 3,
	})

	if runResult["paused"] != true {
		t.Fatalf("expected paused=true from run, got %v", runResult["paused"])
	}

	// Step over should advance to the next line and return debug context
	stepResult := callTool(t, s, stepOverTool, s.handleStepOver, debugStepInput{})

	if stepResult["paused"] == true {
		bp := stepResult["breakpoint"].(map[string]any)
		if bp["reason"] != "step" {
			t.Errorf("expected reason=step, got %v", bp["reason"])
		}
	}
}

func TestStepInto(t *testing.T) {
	s := setupTestProject(t)

	runResult := callTool(t, s, runTool, s.handleRun, runInput{
		Name:    "order-count",
		Events:  "fixtures/orders.json",
		BreakAt: 3,
	})

	if runResult["paused"] != true {
		t.Fatalf("expected paused=true from run, got %v", runResult["paused"])
	}

	stepResult := callTool(t, s, stepIntoTool, s.handleStepInto, debugStepInput{})

	if stepResult["paused"] == true {
		bp := stepResult["breakpoint"].(map[string]any)
		if bp["reason"] != "step" {
			t.Errorf("expected reason=step, got %v", bp["reason"])
		}
	} else if stepResult["completed"] != true {
		t.Fatalf("expected paused=true or completed=true, got %v", stepResult)
	}
}

func TestStepOut(t *testing.T) {
	s := setupTestProject(t)

	runResult := callTool(t, s, runTool, s.handleRun, runInput{
		Name:    "order-count",
		Events:  "fixtures/orders.json",
		BreakAt: 3,
	})

	if runResult["paused"] != true {
		t.Fatalf("expected paused=true from run, got %v", runResult["paused"])
	}

	stepResult := callTool(t, s, stepOutTool, s.handleStepOut, debugStepInput{})

	if stepResult["paused"] == true {
		bp := stepResult["breakpoint"].(map[string]any)
		if bp["reason"] != "step" {
			t.Errorf("expected reason=step, got %v", bp["reason"])
		}
	} else if stepResult["completed"] != true {
		t.Fatalf("expected paused=true or completed=true, got %v", stepResult)
	}
}

// --- get_state unknown partition ---

func TestGetState_UnknownPartition(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})

	result := callTool(t, s, getStateTool, s.handleGetState, getStateInput{Partition: "nonexistent"})
	if result["partition"] != "nonexistent" {
		t.Errorf("expected partition=nonexistent, got %v", result["partition"])
	}
	if _, hasState := result["state"]; hasState {
		t.Error("expected no state key for unknown partition")
	}
}

// --- closeSession while paused ---

func TestStop_WhilePaused(t *testing.T) {
	s := setupTestProject(t)

	result := callTool(t, s, runTool, s.handleRun, runInput{
		Name:    "order-count",
		Events:  "fixtures/orders.json",
		BreakAt: 2,
	})

	if result["paused"] != true {
		t.Fatalf("expected paused=true, got %v", result["paused"])
	}

	// Stop should cleanly tear down the paused session
	callTool(t, s, stopTool, s.handleStop, stopInput{})

	// Session should be gone
	callToolExpectError(t, s.handleGetStep, getStepInput{Step: 1})
}

// TestConcurrentRunStop exercises the session-teardown races: a run
// handler parked at a breakpoint while a concurrent stop/run tears the
// session down. Before the fix, closeSession dropped s.mu and re-read the
// shared s.session (nil-deref / double-Destroy), and the parked handler
// woke to call into the destroyed native session (a process-fatal panic).
// The handlers are invoked directly here, so the trackedTool recover
// backstop is not in play - a residual panic surfaces through the per-
// goroutine recover. Run with -race to also catch the s.session data race.
func TestConcurrentRunStop(t *testing.T) {
	s := setupTestProject(t)

	guard := func(fn func()) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("handler panicked: %v", r)
			}
		}()
		fn()
	}
	run := func(in runInput) {
		guard(func() { _, _, _ = s.handleRun(context.Background(), nil, in) })
	}
	stop := func() {
		guard(func() { _, _, _ = s.handleStop(context.Background(), nil, stopInput{}) })
	}

	for i := 0; i < 100; i++ {
		var wg sync.WaitGroup
		wg.Add(3)
		go func() {
			defer wg.Done()
			run(runInput{Name: "order-count", Events: "fixtures/orders.json", BreakAt: 2})
		}()
		go func() { defer wg.Done(); run(runInput{Name: "order-count", Events: "fixtures/orders.json"}) }()
		go func() { defer wg.Done(); stop() }()
		wg.Wait()
	}
}

// --- Resources ---

func TestResourceConfig(t *testing.T) {
	s := setupTestProject(t)
	result, err := s.handleConfigResource(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "gaffer://project/config"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Contents) != 1 {
		t.Fatal("expected 1 resource content")
	}
	if result.Contents[0].Text == "" {
		t.Error("expected non-empty config content")
	}
}

func TestResourceDocs(t *testing.T) {
	handler := staticResource("resources/projection-api.md")
	result, err := handler(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "gaffer://docs/projection-api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Contents) != 1 {
		t.Fatal("expected 1 resource content")
	}
	text := result.Contents[0].Text
	for _, want := range []string{"Projection API Reference", "fromAll()", "fromStream(", "when"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected projection-api doc to contain %q", want)
		}
	}
}

func TestResourceQuirks(t *testing.T) {
	result, err := quirksResource(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "gaffer://docs/quirks"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Contents) != 1 {
		t.Fatal("expected 1 resource content")
	}
	if got := result.Contents[0].MIMEType; got != "text/markdown" {
		t.Errorf("expected text/markdown, got %q", got)
	}
	text := result.Contents[0].Text
	// Top heading + intro framing.
	for _, want := range []string{"# KurrentDB compat quirks", "unversioned", "quirks_version"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected resource to contain %q\n--- got ---\n%s", want, text)
		}
	}
	// Every registry code is present as a heading.
	for _, code := range []string{
		"quirk.linkStreamTo.outOfBoundsParameters",
		"quirk.log.multiParam",
		"quirk.event.bodyCast",
		"quirk.serialize.rawString",
		"quirk.serialize.nonFinite",
	} {
		if !strings.Contains(text, "## "+code) {
			t.Errorf("expected resource to include heading for %q", code)
		}
	}
	// Quirks without an upstream fix (e.g. log.multiParam) render "not yet
	// shipped upstream"; quirks fixed upstream (bodyCast / nonFinite in 26.2.0)
	// render the version.
	if !strings.Contains(text, "not yet shipped upstream") {
		t.Errorf("expected at least one 'not yet shipped upstream' line")
	}
	if !strings.Contains(text, "KurrentDB 26.2.0") {
		t.Errorf("expected a 'Fixed in: KurrentDB 26.2.0' line for a fixed quirk")
	}
}

func TestResourceTelemetryInfo(t *testing.T) {
	// telemetry-info.gen.md is produced by `just cli _resources` from
	// cli/TELEMETRY.md before any go build/test; if this test fails
	// with "file not found" the recipe didn't run.
	handler := staticResource("resources/telemetry-info.gen.md")
	result, err := handler(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "gaffer://telemetry/info"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Contents) != 1 {
		t.Fatal("expected 1 resource content")
	}
	if got := result.Contents[0].MIMEType; got != "text/markdown" {
		t.Errorf("expected text/markdown, got %q", got)
	}
	text := result.Contents[0].Text
	for _, want := range []string{"Usage telemetry", "How to opt out", "How to delete your data", "privacy@kurrent.io"} {
		if !strings.Contains(text, want) {
			t.Errorf("telemetry-info resource missing %q", want)
		}
	}
}

func TestRenderQuirksMarkdown_FixedInRendering(t *testing.T) {
	// The "Fixed in: KurrentDB X" branch is dead at the catalogue level
	// today. Test it directly.
	fixed := "26.1.1"
	docs := []diagnosticDoc{{
		Code:    "quirk.test.synthetic",
		Class:   "quirk",
		Message: "Test description.",
		FixedIn: &fixed,
	}}
	out := renderQuirksMarkdown(docs)
	if !strings.Contains(out, "## quirk.test.synthetic") {
		t.Error("expected synthetic heading")
	}
	if !strings.Contains(out, "**Fixed in:** KurrentDB 26.1.1") {
		t.Errorf("expected fixed-in line, got:\n%s", out)
	}
}

func TestRenderQuirksMarkdown_EmptyRegistry(t *testing.T) {
	out := renderQuirksMarkdown(nil)
	if !strings.Contains(out, "No quirks registered") {
		t.Errorf("expected empty-registry hint, got:\n%s", out)
	}
}

// --- Prompts ---

func TestWriteProjectionPrompt(t *testing.T) {
	s := setupTestProject(t)
	result, err := s.handleWriteProjectionPrompt(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{
			Arguments: map[string]string{
				"requirements": "count all OrderPlaced events",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 1 {
		t.Fatal("expected 1 message")
	}
	text := result.Messages[0].Content.(*mcp.TextContent).Text
	for _, want := range []string{
		"count all OrderPlaced events",
		"## Requirements",
		"## Workflow",
		"Projection API Reference",
		"fromAll()",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected write-projection prompt to contain %q", want)
		}
	}
}

func TestFixProjectionPrompt(t *testing.T) {
	s := setupTestProject(t)
	result, err := s.handleFixProjectionPrompt(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{
			Arguments: map[string]string{
				"name":    "order-count",
				"problem": "state resets on every event",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := result.Messages[0].Content.(*mcp.TextContent).Text
	for _, want := range []string{
		"Fix the projection `order-count`",
		"## Problem",
		"state resets on every event",
		"fromCategory('order')",
		"Projection API Reference",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("expected fix-projection prompt to contain %q", want)
		}
	}
}

func TestFixProjectionPrompt_NotFound(t *testing.T) {
	s := setupTestProject(t)
	_, err := s.handleFixProjectionPrompt(context.Background(), &mcp.GetPromptRequest{
		Params: &mcp.GetPromptParams{
			Arguments: map[string]string{
				"name": "nonexistent",
			},
		},
	})
	if err == nil {
		t.Error("expected error for unknown projection")
	}
}

// --- Pure helpers ---

func TestExtractState(t *testing.T) {
	state := extractState(`{"state":{"count":5},"partition":"p-1"}`)
	if string(state) != `{"count":5}` {
		t.Errorf("got %s, want {\"count\":5}", state)
	}
}

func TestExtractState_NoState(t *testing.T) {
	state := extractState(`{"partition":"p-1"}`)
	if state != nil {
		t.Errorf("expected nil, got %s", state)
	}
}

func TestExtractState_InvalidJSON(t *testing.T) {
	state := extractState(`not json`)
	if state != nil {
		t.Errorf("expected nil, got %s", state)
	}
}

func TestErrorHint(t *testing.T) {
	if h := errorHint("execution-timeout"); h == "" {
		t.Error("expected hint for execution-timeout")
	}
	if h := errorHint("handler-error"); h == "" {
		t.Error("expected hint for handler-error")
	}
	if h := errorHint("state-serialization-error"); h == "" {
		t.Error("expected hint for state-serialization-error")
	}
	if h := errorHint("unknown-code"); h != "" {
		t.Errorf("expected no hint for unknown code, got %q", h)
	}
}

func TestFormatStep(t *testing.T) {
	step := &history.Step{
		Index:      3,
		EventType:  "OrderPlaced",
		StreamID:   "order-1",
		Status:     "processed",
		Partition:  "order-1",
		EventJSON:  `{"eventType":"OrderPlaced"}`,
		ResultJSON: `{"status":"processed"}`,
	}

	result := formatStep(step)
	if result["step"] != int64(3) {
		t.Errorf("step: got %v", result["step"])
	}
	if result["eventType"] != "OrderPlaced" {
		t.Errorf("eventType: got %v", result["eventType"])
	}
	if result["streamId"] != "order-1" {
		t.Errorf("streamId: got %v", result["streamId"])
	}
	if result["event"] == nil {
		t.Error("expected parsed event")
	}
	if result["result"] == nil {
		t.Error("expected parsed result")
	}
}

// --- Session replacement isolation ---

func TestRun_ReplacesSession_FreshHistory(t *testing.T) {
	s := setupTestProject(t)

	r1 := callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})
	if r1["processed"].(float64) != 5 {
		t.Fatalf("first run: expected 5 processed, got %v", r1["processed"])
	}

	r2 := callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})
	if r2["processed"].(float64) != 5 {
		t.Errorf("second run: expected 5 processed (fresh), got %v", r2["processed"])
	}
}

// --- resolveRange ---

func TestResolveRange(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})

	// fixture has 5 events, so history range is 1..5
	tests := []struct {
		name     string
		from, to int64
		wantFrom int64
		wantTo   int64
	}{
		{"zeros default to full range", 0, 0, 1, 5},
		{"negative from clamps to min", -5, 3, 1, 3},
		{"negative to clamps to max", 1, -1, 1, 5},
		{"both negative", -1, -1, 1, 5},
		{"from greater than to clamps to equal", 4, 2, 4, 4},
		{"normal range", 2, 4, 2, 4},
		{"from below min clamps up", 0, 3, 1, 3},
		{"single step", 3, 3, 3, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFrom, gotTo := s.resolveRange(tt.from, tt.to)
			if gotFrom != tt.wantFrom || gotTo != tt.wantTo {
				t.Errorf("resolveRange(%d, %d) = (%d, %d), want (%d, %d)",
					tt.from, tt.to, gotFrom, gotTo, tt.wantFrom, tt.wantTo)
			}
		})
	}
}

// --- Info ---

func TestInfo_ExplicitName(t *testing.T) {
	s := setupTestProject(t)
	result := callTool(t, s, infoTool, s.handleInfo, infoInput{Name: "order-count"})

	if result["name"] != "order-count" {
		t.Errorf("expected name=order-count, got %v", result["name"])
	}
	if result["entry"] != "projections/order-count.js" {
		t.Errorf("expected entry=projections/order-count.js, got %v", result["entry"])
	}
	if result["partitioning"] != "byStream" {
		t.Errorf("expected partitioning=byStream, got %v", result["partitioning"])
	}
}

func TestInfo_NotFound(t *testing.T) {
	s := setupTestProject(t)
	msg := callToolExpectError(t, s.handleInfo, infoInput{Name: "nonexistent"})
	if !strings.Contains(msg, "not found") {
		t.Errorf("expected 'not found' in error, got %q", msg)
	}
}

func TestInfo_RequiresNameWhenMultiple(t *testing.T) {
	s := setupTestProject(t)
	msg := callToolExpectError(t, s.handleInfo, infoInput{})
	if !strings.Contains(msg, "name required") {
		t.Errorf("expected 'name required' in error, got %q", msg)
	}
}

// writeManifest overwrites the project's gaffer.toml on disk. Handlers
// reload the manifest per call, so a test that wants the server to see a
// particular config writes it here rather than mutating s.cfg.
func writeManifest(t *testing.T, root string, cfg *config.Config) {
	t.Helper()
	if err := config.Save(filepath.Join(root, "gaffer.toml"), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestInfo_ErrorsWhenNoProjections(t *testing.T) {
	s := setupTestProject(t)
	writeManifest(t, s.root, &config.Config{})

	msg := callToolExpectError(t, s.handleInfo, infoInput{})
	if !strings.Contains(msg, "no projections configured") {
		t.Errorf("expected 'no projections configured' in error, got %q", msg)
	}
}

func TestInfo_DefaultsWhenSingleProjection(t *testing.T) {
	s := setupTestProject(t)
	writeManifest(t, s.root, &config.Config{
		Projection: []config.Projection{{Name: "order-count", Entry: "projections/order-count.js", EngineVersion: ptr(2)}},
	})

	result := callTool(t, s, infoTool, s.handleInfo, infoInput{})
	if result["name"] != "order-count" {
		t.Errorf("expected default to order-count, got %v", result["name"])
	}
}

// --- Version ---

func TestVersion(t *testing.T) {
	s := setupTestProject(t)
	result := callTool(t, s, versionTool, s.handleVersion, versionInput{})
	if result["version"] != "test" {
		t.Errorf("expected version=test, got %v", result["version"])
	}
}

// --- Project-less startup ---

// newProjectlessServer builds a server with no project and chdirs to
// an empty temp dir so the lazy cwd walk finds no gaffer.toml above
// it - the server is genuinely project-less.
func newProjectlessServer(t *testing.T) *Server {
	t.Helper()
	t.Chdir(t.TempDir())
	return New("", nil, "test")
}

func TestInstructionsFor(t *testing.T) {
	if got := instructionsFor(&config.Config{}); !strings.Contains(got, "list_projections to see what exists") {
		t.Errorf("project instructions missing workflow text: %q", got)
	}
	if got := instructionsFor(nil); !strings.Contains(got, "No gaffer project is loaded") {
		t.Errorf("project-less instructions missing no-project text: %q", got)
	}
}

func TestProjectlessProjectToolsGated(t *testing.T) {
	s := newProjectlessServer(t)

	const want = "no gaffer project found"
	checks := []struct {
		name string
		msg  string
	}{
		{"list_projections", callToolExpectError(t, s.handleListProjections, listProjectionsInput{})},
		{"validate", callToolExpectError(t, s.handleValidate, validateInput{Name: "x"})},
		{"info", callToolExpectError(t, s.handleInfo, infoInput{})},
		{"scaffold", callToolExpectError(t, s.handleScaffold, scaffoldInput{Path: "projections/x.js"})},
		{"run", callToolExpectError(t, s.handleRun, runInput{Name: "x"})},
		{"list_events", callToolExpectError(t, s.handleListEvents, listEventsInput{Name: "x"})},
	}
	for _, c := range checks {
		if !strings.Contains(c.msg, want) {
			t.Errorf("%s: got %q, want substring %q", c.name, c.msg, want)
		}
	}
}

func TestProjectlessVersionWorks(t *testing.T) {
	s := newProjectlessServer(t)
	result := callTool(t, s, versionTool, s.handleVersion, versionInput{})
	if result["version"] != "test" {
		t.Errorf("expected version=test, got %v", result["version"])
	}
}

func TestProjectlessConfigResourceNotFound(t *testing.T) {
	s := newProjectlessServer(t)
	_, err := s.handleConfigResource(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "gaffer://project/config"},
	})
	if err == nil {
		t.Fatal("expected ResourceNotFound for project-less config resource, got nil")
	}
}

func TestProjectlessDocsResourceWorks(t *testing.T) {
	_ = newProjectlessServer(t)
	handler := staticResource("resources/projection-api.md")
	result, err := handler(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "gaffer://docs/projection-api"},
	})
	if err != nil {
		t.Fatalf("docs resource should work without a project: %v", err)
	}
	if len(result.Contents) != 1 || result.Contents[0].Text == "" {
		t.Error("expected non-empty projection-api doc content")
	}
}

// TestProjectlessLazyResolveAfterInit covers the mid-session pickup: a
// project-less server resolves the project on the next tool call once a
// gaffer.toml appears in the cwd, with no restart.
func TestProjectlessLazyResolveAfterInit(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s := New("", nil, "test")

	if msg := callToolExpectError(t, s.handleListProjections, listProjectionsInput{}); !strings.Contains(msg, "no gaffer project found") {
		t.Fatalf("expected gating before init, got %q", msg)
	}

	cfg := &config.Config{
		Projection: []config.Projection{{Name: "order-count", Entry: "projections/order-count.js", EngineVersion: ptr(2)}},
	}
	if err := config.Save(filepath.Join(dir, "gaffer.toml"), cfg); err != nil {
		t.Fatal(err)
	}

	result := callTool(t, s, listProjectionsTool, s.handleListProjections, listProjectionsInput{})
	projs, ok := result["projections"].([]any)
	if !ok || len(projs) != 1 {
		t.Fatalf("expected 1 projection after init, got %v", result["projections"])
	}
}

// TestProjectlessReloadAfterInit confirms a server that started without a
// project picks up a gaffer.toml created mid-session, and that a later
// edit to it is reflected on the next call - the per-call reload, on the
// project-less resolve path.
func TestProjectlessReloadAfterInit(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s := New("", nil, "test")

	cfgPath := filepath.Join(dir, "gaffer.toml")
	if err := config.Save(cfgPath, &config.Config{
		Projection: []config.Projection{{Name: "order-count", Entry: "projections/order-count.js", EngineVersion: ptr(2)}},
	}); err != nil {
		t.Fatal(err)
	}
	first := callTool(t, s, listProjectionsTool, s.handleListProjections, listProjectionsInput{})
	if projs, ok := first["projections"].([]any); !ok || len(projs) != 1 {
		t.Fatalf("expected 1 projection after init, got %v", first["projections"])
	}

	if err := config.Save(cfgPath, &config.Config{
		Projection: []config.Projection{
			{Name: "order-count", Entry: "projections/order-count.js", EngineVersion: ptr(2)},
			{Name: "totals", Entry: "projections/totals.js", EngineVersion: ptr(2)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	second := callTool(t, s, listProjectionsTool, s.handleListProjections, listProjectionsInput{})
	if projs, ok := second["projections"].([]any); !ok || len(projs) != 2 {
		t.Fatalf("expected reload to show 2 projections after edit, got %v", second["projections"])
	}
}

// TestReloadPicksUpManifestEdits is the core UI-1636 guard: editing
// gaffer.toml during a running session is visible to the next tool call,
// no restart. Before the per-call reload, list_projections read a config
// cached at startup and never saw the edit.
func TestReloadPicksUpManifestEdits(t *testing.T) {
	s := setupTestProject(t)

	before := callTool(t, s, listProjectionsTool, s.handleListProjections, listProjectionsInput{})
	if projs, ok := before["projections"].([]any); !ok || len(projs) != 2 {
		t.Fatalf("expected 2 projections at start, got %v", before["projections"])
	}

	writeManifest(t, s.root, &config.Config{
		Projection: []config.Projection{{Name: "order-count", Entry: "projections/order-count.js", EngineVersion: ptr(2)}},
	})

	after := callTool(t, s, listProjectionsTool, s.handleListProjections, listProjectionsInput{})
	if projs, ok := after["projections"].([]any); !ok || len(projs) != 1 {
		t.Fatalf("expected reload to reflect the edit (1 projection), got %v", after["projections"])
	}
}

// TestReloadSurfacesInvalidManifest covers the other half of UI-1636: a
// manifest that goes invalid mid-session returns a load error on the next
// call rather than silently serving the last good config.
func TestReloadSurfacesInvalidManifest(t *testing.T) {
	s := setupTestProject(t)

	_ = callTool(t, s, listProjectionsTool, s.handleListProjections, listProjectionsInput{})

	invalid := "[[projection]]\nname = \"p\"\nentry = \"p.js\"\nengine_version = 5\n"
	if err := os.WriteFile(filepath.Join(s.root, "gaffer.toml"), []byte(invalid), 0o644); err != nil {
		t.Fatal(err)
	}

	msg := callToolExpectError(t, s.handleListProjections, listProjectionsInput{})
	if !strings.Contains(msg, "loading gaffer.toml") {
		t.Errorf("expected manifest load error after invalid edit, got %q", msg)
	}
}

// TestProjectlessInvalidManifestSurfaces confirms a gaffer.toml that
// exists but fails to load is surfaced (not treated as "no project"),
// and is not cached, so a fix is picked up on retry.
func TestProjectlessInvalidManifestSurfaces(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s := New("", nil, "test")

	invalid := "[[projection]]\nname = \"p\"\nentry = \"p.js\"\nengine_version = 5\n"
	if err := os.WriteFile(filepath.Join(dir, "gaffer.toml"), []byte(invalid), 0o644); err != nil {
		t.Fatal(err)
	}
	if msg := callToolExpectError(t, s.handleListProjections, listProjectionsInput{}); !strings.Contains(msg, "loading gaffer.toml") {
		t.Errorf("expected manifest load error, got %q", msg)
	}
}

// TestConfigResourceReadsInvalidManifest confirms the config resource
// raw-reads gaffer.toml even when it fails to parse/validate - inspecting
// a broken manifest is exactly when the resource is useful.
func TestConfigResourceReadsInvalidManifest(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s := New("", nil, "test")

	raw := "[[projection]]\nname = \"p\"\nentry = \"p.js\"\nengine_version = 5\n# present but invalid\n"
	if err := os.WriteFile(filepath.Join(dir, "gaffer.toml"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := s.handleConfigResource(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "gaffer://project/config"},
	})
	if err != nil {
		t.Fatalf("config resource should read a present-but-invalid manifest: %v", err)
	}
	if len(result.Contents) != 1 || !strings.Contains(result.Contents[0].Text, "engine_version = 5") {
		t.Errorf("expected raw manifest content, got %v", result.Contents)
	}
}

// --- Project override (--project / GAFFER_PROJECT) ---

func writeProject(t *testing.T, dir string) {
	t.Helper()
	cfg := &config.Config{
		Projection: []config.Projection{{Name: "order-count", Entry: "projections/order-count.js", EngineVersion: ptr(2)}},
	}
	if err := config.Save(filepath.Join(dir, "gaffer.toml"), cfg); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeOverride(t *testing.T) {
	if got := normalizeOverride(""); got != "" {
		t.Errorf("empty override should stay empty, got %q", got)
	}
	if got := normalizeOverride("relative/dir"); !filepath.IsAbs(got) {
		t.Errorf("expected absolute path, got %q", got)
	}
}

func TestProjectOverrideResolvesOutsideCwd(t *testing.T) {
	projDir := t.TempDir()
	writeProject(t, projDir)
	t.Chdir(t.TempDir()) // cwd has no gaffer.toml

	s, err := NewFromProjectRoot("test", projDir)
	if err != nil {
		t.Fatalf("NewFromProjectRoot with override: %v", err)
	}
	if s.Config() == nil {
		t.Fatal("expected eager project load via --project override")
	}
	result := callTool(t, s, listProjectionsTool, s.handleListProjections, listProjectionsInput{})
	if projs, ok := result["projections"].([]any); !ok || len(projs) != 1 {
		t.Fatalf("expected 1 projection via override, got %v", result["projections"])
	}
}

func TestProjectOverrideWalksUpFromSubdir(t *testing.T) {
	projDir := t.TempDir()
	writeProject(t, projDir)
	sub := filepath.Join(projDir, "nested", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	s, err := NewFromProjectRoot("test", sub)
	if err != nil {
		t.Fatalf("NewFromProjectRoot with subdir override: %v", err)
	}
	if s.Config() == nil {
		t.Fatal("expected override to walk up from a subdirectory to the project root")
	}
}

func TestProjectOverrideMissingNamesPathInError(t *testing.T) {
	override := t.TempDir() // exists, no gaffer.toml above it
	t.Chdir(t.TempDir())

	s, err := NewFromProjectRoot("test", override)
	if err != nil {
		t.Fatalf("NewFromProjectRoot: %v", err)
	}
	if s.Config() != nil {
		t.Fatal("expected project-less server for an override without gaffer.toml")
	}
	msg := callToolExpectError(t, s.handleListProjections, listProjectionsInput{})
	if !strings.Contains(msg, override) || !strings.Contains(msg, "GAFFER_PROJECT") {
		t.Errorf("expected error to name the override path and env var, got %q", msg)
	}
}

// --- init tool ---

func TestInitToolCreatesProjectInCwd(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s := New("", nil, "test")

	result := callTool(t, s, initTool, s.handleInit, initInput{})
	if want := filepath.Join(dir, "gaffer.toml"); result["created"] != want {
		t.Errorf("created = %v, want %s", result["created"], want)
	}

	// Lazy resolution picks up the freshly-created project on the next call.
	listed := callTool(t, s, listProjectionsTool, s.handleListProjections, listProjectionsInput{})
	if projs, ok := listed["projections"].([]any); !ok || len(projs) != 0 {
		t.Fatalf("expected an empty projection list after init, got %v", listed["projections"])
	}
}

func TestInitToolRefusesExistingProject(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeProject(t, dir)
	s := New("", nil, "test")

	if msg := callToolExpectError(t, s.handleInit, initInput{}); !strings.Contains(msg, "already exists") {
		t.Errorf("expected an already-exists error, got %q", msg)
	}
}

func TestInitToolRefusesProjectInParent(t *testing.T) {
	parent := t.TempDir()
	writeProject(t, parent)
	sub := filepath.Join(parent, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub) // no gaffer.toml here, but one exists in the parent

	s := New("", nil, "test")
	if msg := callToolExpectError(t, s.handleInit, initInput{}); !strings.Contains(msg, parent) {
		t.Errorf("expected refusal to name the parent project root %s, got %q", parent, msg)
	}
}

func TestInitToolTargetsOverride(t *testing.T) {
	target := t.TempDir() // where the project should land
	t.Chdir(t.TempDir())  // cwd is elsewhere and empty

	s, err := NewFromProjectRoot("test", target)
	if err != nil {
		t.Fatal(err)
	}

	result := callTool(t, s, initTool, s.handleInit, initInput{})
	if result["root"] != target {
		t.Errorf("init root = %v, want override %s", result["root"], target)
	}
	if _, err := os.Stat(filepath.Join(target, "gaffer.toml")); err != nil {
		t.Errorf("expected gaffer.toml at the override dir: %v", err)
	}
}

// --- started_in_project telemetry ---

func TestStartedInProject(t *testing.T) {
	if New("", nil, "test").StartedInProject() {
		t.Error("project-less construction should report StartedInProject() == false")
	}
	if !New(t.TempDir(), &config.Config{}, "test").StartedInProject() {
		t.Error("in-project construction should report StartedInProject() == true")
	}

	t.Chdir(t.TempDir()) // no gaffer.toml
	projectless, err := NewFromProjectRoot("test", "")
	if err != nil {
		t.Fatal(err)
	}
	if projectless.StartedInProject() {
		t.Error("NewFromProjectRoot with no project should report false")
	}

	proj := t.TempDir()
	writeProject(t, proj)
	inProject, err := NewFromProjectRoot("test", proj)
	if err != nil {
		t.Fatal(err)
	}
	if !inProject.StartedInProject() {
		t.Error("NewFromProjectRoot with a project should report true")
	}
}

// StartedInProject reflects startup state only - a project resolved
// lazily mid-session must not flip it.
func TestStartedInProjectStableAcrossLazyResolve(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s := New("", nil, "test")

	writeProject(t, dir)
	_ = callTool(t, s, listProjectionsTool, s.handleListProjections, listProjectionsInput{})

	if s.StartedInProject() {
		t.Error("StartedInProject() must stay false after a lazy resolve")
	}
}

func ptr[T any](v T) *T { return &v }
