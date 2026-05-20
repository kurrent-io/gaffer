package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var versionTool = &mcp.Tool{
	Name:        "get_version",
	Description: "Return the gaffer CLI version string. Mirrors `gaffer version`.",
	Annotations: &mcp.ToolAnnotations{
		ReadOnlyHint:   true,
		IdempotentHint: true,
	},
}

type versionInput struct{}

func (s *Server) handleVersion(_ context.Context, _ *mcp.CallToolRequest, _ versionInput) (*mcp.CallToolResult, any, error) {
	return toolResult(map[string]any{"version": s.version}), nil, nil
}
