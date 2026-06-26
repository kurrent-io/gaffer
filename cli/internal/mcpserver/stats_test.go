package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStats_ZeroBeforeActivity confirms the Stats() snapshot
// returns zeros when no tool or resource has been dispatched yet.
// Critical for `gaffer mcp` invocations that exit before any
// client connects - the envelope must emit with valid (zero)
// counts, not nil pointers.
func TestStats_ZeroBeforeActivity(t *testing.T) {
	s := setupTestProject(t)
	got := s.Stats()
	if got.ToolCallCount != 0 {
		t.Errorf("ToolCallCount = %d, want 0", got.ToolCallCount)
	}
	if got.ResourceReadCount != 0 {
		t.Errorf("ResourceReadCount = %d, want 0", got.ResourceReadCount)
	}
}

// TestTrackedTool_BumpsCounter wraps a known handler via
// trackedTool and confirms the counter increments per call. The
// wrapper is what `mcp.AddTool` receives in server.go, so this
// pins the registration shape that drains in `gaffer mcp`'s
// RunE at tx.End time.
func TestTrackedTool_BumpsCounter(t *testing.T) {
	s := setupTestProject(t)
	wrapped := trackedTool(s, s.handleListProjections)

	for i := range 3 {
		_, _, err := wrapped(context.Background(), nil, listProjectionsInput{})
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := s.Stats().ToolCallCount; got != 3 {
		t.Errorf("ToolCallCount = %d, want 3", got)
	}
	if got := s.Stats().ResourceReadCount; got != 0 {
		t.Errorf("ResourceReadCount = %d, want 0 (tool path doesn't bump resources)", got)
	}
}

// TestTrackedResource_BumpsCounter is the resource-side mirror
// of the tool counter test.
func TestTrackedResource_BumpsCounter(t *testing.T) {
	s := setupTestProject(t)
	wrapped := s.trackedResource(s.handleConfigResource)

	req := &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "gaffer://project/config"}}
	for i := range 2 {
		if _, err := wrapped(context.Background(), req); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := s.Stats().ResourceReadCount; got != 2 {
		t.Errorf("ResourceReadCount = %d, want 2", got)
	}
	if got := s.Stats().ToolCallCount; got != 0 {
		t.Errorf("ToolCallCount = %d, want 0 (resource path doesn't bump tools)", got)
	}
}

// TestTrackedTool_BumpsEvenOnHandlerError confirms the counter
// records attempts, not successes. A user mistyping a projection
// name should still appear in tool_call_count - failed calls are
// usage data too.
func TestTrackedTool_BumpsEvenOnHandlerError(t *testing.T) {
	s := setupTestProject(t)
	wrapped := trackedTool(s, s.handleValidate)
	_, _, _ = wrapped(context.Background(), nil, validateInput{Name: "does-not-exist"})
	if got := s.Stats().ToolCallCount; got != 1 {
		t.Errorf("ToolCallCount = %d, want 1 even on tool-error result", got)
	}
}
