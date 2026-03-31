//go:build integration

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kurrent-io/KurrentDB-Client-Go/kurrentdb"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func testSuffix() string {
	return strings.ReplaceAll(uuid.New().String(), "-", "")[:12]
}

func connectionString() string {
	if s := os.Getenv("KURRENTDB_URL"); s != "" {
		return s
	}
	return "kurrentdb://localhost:2113?tls=false"
}

func setupLiveTestProject(t *testing.T, suffix string) (*Server, *kurrentdb.Client) {
	t.Helper()

	connStr := connectionString()
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

	dir := t.TempDir()
	projDir := filepath.Join(dir, "projections")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}

	projSource := fmt.Sprintf(`fromCategory('inttest-%s')
  .foreachStream()
  .when({
    $init: function() { return { count: 0 }; },
    Ping: function(s, e) { s.count++; return s; }
  })
`, suffix)

	if err := os.WriteFile(filepath.Join(projDir, "counter.js"), []byte(projSource), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Connection: connStr,
		Projection: []config.Projection{
			{Name: "counter", Entry: "projections/counter.js"},
		},
	}

	configPath := filepath.Join(dir, "gaffer.toml")
	if err := config.Save(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	s := New(dir, cfg)
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
	suffix := testSuffix()
	s, client := setupLiveTestProject(t, suffix)

	stream := fmt.Sprintf("inttest-%s-1", suffix)
	writeTestEvents(t, client, stream, 3)

	result := callTool(t, s, runTool, s.handleRun, runInput{Name: "counter"})
	if result["mode"] != "live" {
		t.Fatalf("expected mode=live, got %v", result["mode"])
	}

	// Wait for events to be processed
	var status map[string]any
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status = callTool(t, s, statusTool, s.handleStatus, statusInput{})
		if status["processed"].(float64) >= 3 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if status["processed"].(float64) < 3 {
		t.Fatalf("expected at least 3 processed, got %v", status["processed"])
	}

	// Inspect history
	timeline := callTool(t, s, getTimelineTool, s.handleGetTimeline, getTimelineInput{})
	entries := timeline["entries"].([]any)
	if len(entries) < 3 {
		t.Fatalf("expected at least 3 timeline entries, got %d", len(entries))
	}

	// Check first step
	step := callTool(t, s, getStepTool, s.handleGetStep, getStepInput{Position: int64(entries[0].(map[string]any)["position"].(float64))})
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
	suffix := testSuffix()
	s, client := setupLiveTestProject(t, suffix)

	stream := fmt.Sprintf("inttest-%s-1", suffix)
	writeTestEvents(t, client, stream, 5)

	result := callTool(t, s, runTool, s.handleRun, runInput{
		Name:        "counter",
		Breakpoints: []breakpointInput{{Line: 4}},
	})

	if result["debug"] != true {
		t.Fatalf("expected debug=true, got %v", result["debug"])
	}

	waitForStatus(t, s, "breakpoint_hit", 15*time.Second)

	// Evaluate while paused - use 1+1 since state may not be in scope at $init
	evalResult := callTool(t, s, evaluateTool, s.handleEvaluate, evaluateInput{Expression: "1+1"})
	if evalResult["value"] != "2" {
		t.Errorf("expected value=2, got %v", evalResult["value"])
	}

	// Continue past breakpoints until we've processed enough events
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		status := callTool(t, s, statusTool, s.handleStatus, statusInput{})
		if status["processed"].(float64) >= 3 {
			break
		}
		if status["status"] == "breakpoint_hit" {
			callTool(t, s, debugContinueTool, s.handleDebugContinue, debugContinueInput{})
		}
		time.Sleep(100 * time.Millisecond)
	}

	status := callTool(t, s, statusTool, s.handleStatus, statusInput{})
	if status["processed"].(float64) < 3 {
		t.Fatalf("expected at least 3 processed, got %v", status["processed"])
	}
}

func TestListEvents(t *testing.T) {
	suffix := testSuffix()
	s, client := setupLiveTestProject(t, suffix)

	stream := fmt.Sprintf("inttest-%s-1", suffix)
	writeTestEvents(t, client, stream, 3)

	// Poll until events are readable (propagation delay)
	var result *mcp.CallToolResult
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		result, _, err = s.handleListEvents(context.Background(), nil, listEventsInput{Limit: 1000})
		if err != nil {
			t.Fatal(err)
		}
		var check map[string]any
		_ = json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &check)
		if check["totalSampled"].(float64) >= 3 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &data); err != nil {
		t.Fatal(err)
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
