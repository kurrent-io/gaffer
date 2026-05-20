//go:build integration

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func setupLiveTestProject(t *testing.T, suffix string) (*Server, *kurrentdb.Client) {
	t.Helper()

	connStr := testutil.ConnectionString()
	dbConfig, err := kurrentdb.ParseConnectionString(connStr)
	if err != nil {
		t.Fatal(err)
	}
	dbConfig.Logger = kurrentdb.NoopLogging()

	client, err := kurrentdb.NewClient(dbConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	projSource := fmt.Sprintf(`fromCategory('inttest%s')
  .foreachStream()
  .when({
    $init() { return { count: 0 }; },
    Ping(s, e) { s.count++; return s; }
  })
`, suffix)

	p := testutil.NewProject(t).
		WithConnection(connStr).
		AddProjection("counter", projSource).
		Save()

	s := New(p.Dir, p.Cfg, "test", testManifest())
	t.Cleanup(func() {
		s.mu.Lock()
		s.closeSession()
		s.mu.Unlock()
	})
	return s, client
}

func writeTestEvents(t *testing.T, client *kurrentdb.Client, stream string, count int) {
	t.Helper()
	events := make([]kurrentdb.EventData, count)
	for i := range events {
		events[i] = kurrentdb.EventData{
			EventID:     uuid.New(),
			EventType:   "Ping",
			ContentType: kurrentdb.ContentTypeJson,
			Data:        []byte(fmt.Sprintf(`{"seq":%d}`, i)),
		}
	}
	_, err := client.AppendToStream(context.Background(), stream, kurrentdb.AppendToStreamOptions{}, events...)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLive_RunAndInspect(t *testing.T) {
	suffix := testutil.TestSuffix()
	s, client := setupLiveTestProject(t, suffix)

	stream := fmt.Sprintf("inttest%s-1", suffix)
	writeTestEvents(t, client, stream, 3)

	// run blocks until caught_up
	result := callTool(t, s, runTool, s.handleRun, runInput{Name: "counter"})
	if result["caughtUp"] != true {
		t.Fatalf("expected caughtUp=true, got %v", result)
	}
	if result["processed"].(float64) < 3 {
		t.Fatalf("expected at least 3 processed, got %v", result["processed"])
	}

	// Inspect history
	timeline := callTool(t, s, getTimelineTool, s.handleGetTimeline, getTimelineInput{})
	entries := timeline["entries"].([]any)
	if len(entries) < 3 {
		t.Fatalf("expected at least 3 timeline entries, got %d", len(entries))
	}

	// Check first step
	step := callTool(t, s, getStepTool, s.handleGetStep, getStepInput{Step: int64(entries[0].(map[string]any)["step"].(float64))})
	if step["eventType"] != "Ping" {
		t.Errorf("expected eventType=Ping, got %v", step["eventType"])
	}

	// Get state
	state := callTool(t, s, getStateTool, s.handleGetState, getStateInput{})
	if state["partitions"] == nil {
		t.Error("expected partitions in state")
	}
}

func TestLive_WithBreakpoint(t *testing.T) {
	suffix := testutil.TestSuffix()
	s, client := setupLiveTestProject(t, suffix)

	stream := fmt.Sprintf("inttest%s-1", suffix)
	writeTestEvents(t, client, stream, 5)

	// run blocks until first breakpoint hit
	result := callTool(t, s, runTool, s.handleRun, runInput{
		Name:        "counter",
		Breakpoints: []breakpointInput{{Line: 4}},
	})

	if result["paused"] != true {
		t.Fatalf("expected paused=true, got %v", result)
	}

	// Evaluate while paused
	evalResult := callTool(t, s, evaluateTool, s.handleEvaluate, evaluateInput{Expression: "1+1"})
	if evalResult["value"] != "2" {
		t.Errorf("expected value=2, got %v", evalResult["value"])
	}

	// Continue past breakpoints - each debug_continue blocks until next break or completion
	for i := 0; i < 20; i++ {
		contResult := callTool(t, s, debugContinueTool, s.handleDebugContinue, debugContinueInput{})
		if contResult["caughtUp"] == true {
			if contResult["processed"].(float64) < 3 {
				t.Fatalf("expected at least 3 processed, got %v", contResult["processed"])
			}
			return
		}
		if contResult["paused"] != true {
			t.Fatalf("expected paused or caughtUp, got %v", contResult)
		}
	}

	t.Fatal("never reached caught_up status")
}

func TestListEvents(t *testing.T) {
	suffix := testutil.TestSuffix()
	s, client := setupLiveTestProject(t, suffix)

	stream := fmt.Sprintf("inttest%s-1", suffix)
	writeTestEvents(t, client, stream, 3)

	var result *mcp.CallToolResult
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		result, _, err = s.handleListEvents(context.Background(), nil, listEventsInput{Name: "counter"})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError {
			var check map[string]any
			_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &check)
			if check["totalSampled"].(float64) >= 3 {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &data); err != nil {
		t.Fatal(err)
	}

	if data["projection"] != "counter" {
		t.Errorf("expected projection=counter, got %v", data["projection"])
	}

	types := data["eventTypes"].([]any)
	found := false
	for _, et := range types {
		entry := et.(map[string]any)
		if entry["eventType"] == "Ping" {
			found = true
			if entry["count"].(float64) < 3 {
				t.Errorf("expected at least 3 Ping events, got %v", entry["count"])
			}
			break
		}
	}
	if !found {
		t.Error("Ping event type not found in list_events results")
	}
}
