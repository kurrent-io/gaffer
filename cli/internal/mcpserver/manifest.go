package mcpserver

import (
	"context"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var manifestTool = &mcp.Tool{
	Name: "get_manifest",
	Description: "Show which CLI subcommands and flags this gaffer build " +
		"supports. Mirrors `gaffer manifest`.",
	Annotations: &mcp.ToolAnnotations{
		ReadOnlyHint:   true,
		IdempotentHint: true,
	},
}

type manifestInput struct{}

func (s *Server) handleManifest(_ context.Context, _ *mcp.CallToolRequest, _ manifestInput) (*mcp.CallToolResult, cliout.Manifest, error) {
	return toolResult(s.manifest), s.manifest, nil
}
