package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
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
		EngineVersion: 2,
		Projection: []config.Projection{
			{Name: "order-count", Entry: "projections/order-count.js"},
			{Name: "broken", Entry: "projections/broken.js"},
		},
	}

	configPath := filepath.Join(dir, "gaffer.toml")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	s = New(dir, cfg, "test")
	return s
}

func callTool[In any](t *testing.T, s *Server, tool *mcp.Tool, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error), input In) map[string]any {
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

func callToolExpectError[In any](t *testing.T, handler func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, any, error), input In) string {
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
	result := callTool(t, s, scaffoldTool, s.handleScaffold, scaffoldInput{Name: "new-proj"})

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

func TestScaffold_Duplicate(t *testing.T) {
	s := setupTestProject(t)
	callToolExpectError(t, s.handleScaffold, scaffoldInput{Name: "order-count"})
}

func TestScaffold_PathTraversal(t *testing.T) {
	s := setupTestProject(t)
	callToolExpectError(t, s.handleScaffold, scaffoldInput{Name: "../escape"})
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

func TestResourceDbVersionBugs(t *testing.T) {
	result, err := dbVersionBugsResource(context.Background(), &mcp.ReadResourceRequest{
		Params: &mcp.ReadResourceParams{URI: "gaffer://docs/db-version-bugs"},
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
	for _, want := range []string{"# KurrentDB compat bugs", "unversioned", "db_version"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected resource to contain %q\n--- got ---\n%s", want, text)
		}
	}
	// Every registry code is present as a heading.
	for _, code := range []string{
		"compat.linkStreamTo.outOfBoundsParameters",
		"compat.log.multiParam",
		"compat.event.bodyCast",
		"compat.biState.stringSlot",
		"compat.serialize.nonFinite",
	} {
		if !strings.Contains(text, "## "+code) {
			t.Errorf("expected resource to include heading for %q", code)
		}
	}
	// Today every entry has FixedIn = nil; the rendering shows "not yet
	// shipped upstream" rather than a version. Once upstream ships, that
	// flips to "Fixed in: KurrentDB X".
	if !strings.Contains(text, "not yet shipped upstream") {
		t.Errorf("expected at least one 'not yet shipped upstream' line")
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

func TestRenderDbVersionBugsMarkdown_FixedInRendering(t *testing.T) {
	// The "Fixed in: KurrentDB X" branch is dead at the registry level
	// today. Test it directly.
	fixed := "26.1.1"
	bugs := []gafferruntime.KnownBug{{
		Code:        "compat.test.synthetic",
		Description: "Test description.",
		FixedIn:     &fixed,
	}}
	out := renderDbVersionBugsMarkdown(bugs)
	if !strings.Contains(out, "## compat.test.synthetic") {
		t.Error("expected synthetic heading")
	}
	if !strings.Contains(out, "**Fixed in:** KurrentDB 26.1.1") {
		t.Errorf("expected fixed-in line, got:\n%s", out)
	}
}

func TestRenderDbVersionBugsMarkdown_EmptyRegistry(t *testing.T) {
	out := renderDbVersionBugsMarkdown(nil)
	if !strings.Contains(out, "No bugs registered") {
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
