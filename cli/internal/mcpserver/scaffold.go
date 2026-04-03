package mcpserver

import (
	"context"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/scaffold"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var scaffoldTool = &mcp.Tool{
	Name:        "scaffold",
	Description: "Create a new projection in the project. Generates the source file and adds it to gaffer.toml.",
}

type scaffoldInput struct {
	Name      string `json:"name" jsonschema:"Projection name"`
	Source    string `json:"source,omitempty" jsonschema:"Event source: 'all' (default), 'stream:name', or 'category:name'"`
	Partition string `json:"partition,omitempty" jsonschema:"Partitioning: 'none' (default) or 'per-stream'"`
	Emit      bool   `json:"emit,omitempty" jsonschema:"Include emit/linkTo example in template"`
}

func (s *Server) handleScaffold(_ context.Context, _ *mcp.CallToolRequest, input scaffoldInput) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if input.Name == "" {
		return toolError("name is required"), nil, nil
	}

	if strings.Contains(input.Name, "/") || strings.Contains(input.Name, "\\") || strings.Contains(input.Name, "..") {
		return toolError("invalid projection name: %q", input.Name), nil, nil
	}

	source := input.Source
	if source == "" {
		source = "all"
	}

	partition := input.Partition
	if partition == "" {
		partition = "none"
	}

	result, err := scaffold.Scaffold(s.root, s.cfg, input.Name, source, partition, input.Emit)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	return toolResult(map[string]any{
		"created": result.RelPath,
		"name":    result.Name,
	}), nil, nil
}
