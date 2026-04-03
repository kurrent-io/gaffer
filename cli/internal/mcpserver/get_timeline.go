package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var getTimelineTool = &mcp.Tool{
	Name:        "get_timeline",
	Description: "Get a compact overview of a range of steps. Returns position, event type, stream ID, status, and flags for each step. Use this to scan for interesting positions, then drill in with get_step.",
}

type getTimelineInput struct {
	From      int64  `json:"from" jsonschema:"Start position (inclusive). Defaults to 1 if 0."`
	To        int64  `json:"to" jsonschema:"End position (inclusive). Defaults to last position if 0."`
	Partition string `json:"partition,omitempty" jsonschema:"Filter to a specific partition key"`
}

func (s *Server) handleGetTimeline(_ context.Context, _ *mcp.CallToolRequest, input getTimelineInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, errResult := s.requireSession()
	if errResult != nil {
		return errResult, nil, nil
	}

	from, to := s.resolveRange(input.From, input.To)

	entries, err := sess.runner.TimelineFiltered(from, to, input.Partition)
	if err != nil {
		return toolError("querying timeline: %v", err), nil, nil
	}

	return toolResult(map[string]any{
		"entries": entries,
	}), nil, nil
}
