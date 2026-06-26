package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var versionTool = &mcp.Tool{
	Name:        "get_version",
	Description: "Return the gaffer CLI version string. Mirrors `gaffer version`.",
	Annotations: readOnlyHints(),
}

type versionInput struct{}

type versionOutput struct {
	Version string `json:"version"`
}

func (s *Server) handleVersion(_ context.Context, _ *mcp.CallToolRequest, _ versionInput) (*mcp.CallToolResult, versionOutput, error) {
	out := versionOutput{Version: s.version}
	return toolResult(out), out, nil
}
