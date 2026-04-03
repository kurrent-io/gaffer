package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
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

	if s.cfg.FindProjection(input.Name) != nil {
		return toolError("projection %q already exists in gaffer.toml", input.Name), nil, nil
	}

	source := input.Source
	if source == "" {
		source = "all"
	}

	partition := input.Partition
	if partition == "" {
		partition = "none"
	}

	relPath := filepath.Join("projections", input.Name+".js")
	absPath := filepath.Join(s.root, relPath)

	if _, err := os.Stat(absPath); err == nil {
		return toolError("file already exists: %s", relPath), nil, nil
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return toolError("creating directory: %v", err), nil, nil
	}

	content, err := engine.GenerateSource(source, partition, input.Emit)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
		return toolError("writing file: %v", err), nil, nil
	}

	newProj := config.Projection{
		Name:  input.Name,
		Entry: relPath,
	}

	updated := *s.cfg
	updated.Projection = append(updated.Projection, newProj)

	configPath := filepath.Join(s.root, "gaffer.toml")
	if err := config.Save(configPath, &updated); err != nil {
		return toolError("updating gaffer.toml: %v", err), nil, nil
	}

	s.cfg.Projection = updated.Projection

	return toolResult(map[string]any{
		"created": relPath,
		"name":    input.Name,
	}), nil, nil
}

