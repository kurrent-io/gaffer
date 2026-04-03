package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var stopTool = &mcp.Tool{
	Name:        "stop",
	Description: "Stop and tear down the active session.",
}

type stopInput struct{}

func (s *Server) handleStop(_ context.Context, _ *mcp.CallToolRequest, _ stopInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.session == nil {
		return toolError("no active session"), nil, nil
	}

	s.closeSession()
	return toolResult(map[string]any{"stopped": true}), nil, nil
}
