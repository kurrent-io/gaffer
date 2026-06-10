// Path fields on MCP tool inputs are interpreted relative to the
// project root. Other tools that take a file path should follow the
// same rule for consistency.

package mcpserver

import (
	"context"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/scaffold"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var scaffoldTool = &mcp.Tool{
	Name: "scaffold",
	Description: "Create a new projection at <path>, relative to the project root. " +
		"Generates the source file and adds it to gaffer.toml. " +
		"Path must end in a supported extension (" +
		strings.Join(scaffold.ListExtensions(), ", ") + ") " +
		"and stay inside the project root. Mirror existing entries in gaffer.toml " +
		"when picking a location; if there is no convention yet, `projections/<name>.js` " +
		"is a reasonable default.",
}

type scaffoldInput struct {
	Path          string `json:"path" jsonschema:"Projection file path, relative to the project root. Must end in a supported extension (e.g. .js)."`
	Name          string `json:"name,omitempty" jsonschema:"Projection name in gaffer.toml. Defaults to the file's basename without extension."`
	Source        string `json:"source,omitempty" jsonschema:"Event source: 'all' (default), 'stream:name', or 'category:name'"`
	Partition     string `json:"partition,omitempty" jsonschema:"Partitioning: 'none' (default) or 'per-stream'"`
	Emit          bool   `json:"emit,omitempty" jsonschema:"Include emit/linkTo example in template"`
	EngineVersion int    `json:"engine_version,omitempty" jsonschema:"Projection engine version: 1 or 2. Defaults to 2; use 1 only for legacy compatibility."`
}

func (s *Server) handleScaffold(_ context.Context, _ *mcp.CallToolRequest, input scaffoldInput) (*mcp.CallToolResult, any, error) {
	cfg, root, r := s.requireProject()
	if r != nil {
		return r, nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	source := input.Source
	if source == "" {
		source = "all"
	}
	partition := input.Partition
	if partition == "" {
		partition = "none"
	}
	engineVersion := input.EngineVersion
	if engineVersion == 0 {
		engineVersion = config.DefaultEngineVersion
	}

	// scaffold.Scaffold owns path validation, name defaulting, and the
	// engine_version 1-or-2 check; the handler just routes the JSON
	// shape into the call.
	result, err := scaffold.Scaffold(root, cfg, input.Name, input.Path, source, partition, input.Emit, engineVersion)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	return toolResult(map[string]any{
		"created": result.RelPath,
		"name":    result.Name,
	}), nil, nil
}
