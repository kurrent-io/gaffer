package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var manifestTool = &mcp.Tool{
	Name: "manifest",
	Description: "Show which CLI subcommands and flags this gaffer build " +
		"supports. Mirrors `gaffer manifest`.",
}

type manifestInput struct{}

func (s *Server) handleManifest(_ context.Context, _ *mcp.CallToolRequest, _ manifestInput) (*mcp.CallToolResult, any, error) {
	return toolResult(s.manifest), nil, nil
}
