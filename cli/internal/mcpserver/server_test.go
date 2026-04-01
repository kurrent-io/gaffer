package mcpserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const testProjection = `fromCategory('order')
  .foreachStream()
  .when({
    $init: function() {
      return { count: 0, totalCents: 0 };
    },
    OrderPlaced: function(state, event) {
      state.count++;
      state.totalCents += event.data.cents;
      return state;
    },
    OrderShipped: function(state, event) {
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
    $init: function() { return {}; },
    $any: function(state, event) {
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
			{Name: "order-count", Entry: "projections/order-count.js"},
			{Name: "broken", Entry: "projections/broken.js"},
		},
	}

	configPath := filepath.Join(dir, "gaffer.toml")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	s = New(dir, cfg)
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
	callToolExpectError(t, s.handleGetStep, getStepInput{Position: 1})
}

// --- Inspection tools ---

func TestGetStep(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})

	result := callTool(t, s, getStepTool, s.handleGetStep, getStepInput{Position: 1})
	if result["eventType"] != "OrderPlaced" {
		t.Errorf("expected eventType=OrderPlaced, got %v", result["eventType"])
	}
	if result["streamId"] != "order-1" {
		t.Errorf("expected streamId=order-1, got %v", result["streamId"])
	}
}

func TestGetStep_NoSession(t *testing.T) {
	s := setupTestProject(t)
	callToolExpectError(t, s.handleGetStep, getStepInput{Position: 1})
}

func TestGetStep_InvalidPosition(t *testing.T) {
	s := setupTestProject(t)
	callTool(t, s, runTool, s.handleRun, runInput{Name: "order-count", Events: "fixtures/orders.json"})
	callToolExpectError(t, s.handleGetStep, getStepInput{Position: 999})
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
	callToolExpectError(t, s.handleGetStep, getStepInput{Position: 1})
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
	if len(result.Contents[0].Text) < 100 {
		t.Error("expected substantial doc content")
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
	if len(text) < 500 {
		t.Error("expected substantial prompt content")
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
	if len(text) < 500 {
		t.Error("expected substantial prompt content")
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
