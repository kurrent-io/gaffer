package mcpserver

import (
	"context"
	"os"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var initTool = &mcp.Tool{
	Name: "init",
	Description: "Initialize a new gaffer project by creating gaffer.toml, so the " +
		"projection tools become available. Targets the server's project directory: " +
		"the --project / GAFFER_PROJECT override if set, otherwise the working " +
		"directory. Fails if a project already exists. After init, list_projections, " +
		"scaffold, run and the rest work on the next call - no restart needed.",
}

type initInput struct{}

func (s *Server) handleInit(_ context.Context, _ *mcp.CallToolRequest, _ initInput) (*mcp.CallToolResult, any, error) {
	// No lock needed: projectOverride is immutable after construction,
	// and InitProject's O_EXCL create is the serialization point - a
	// second concurrent init gets the already-exists error rather than
	// clobbering the first.
	//
	// Refuse if a project is already in scope (walking up from the
	// override or cwd), naming where it was found - init exists to
	// bootstrap a missing project, not to shadow one with a nested copy.
	if root := resolveRoot(s.projectOverride); root != "" {
		return toolError("a gaffer project already exists at %s", root), nil, nil
	}

	dir := s.projectOverride
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return toolError("could not determine the working directory; pass --project or set GAFFER_PROJECT"), nil, nil
		}
		dir = cwd
	}

	path, err := config.InitProject(dir)
	if err != nil {
		return toolError("%v", err), nil, nil
	}

	return toolResult(map[string]any{
		"created": path,
		"root":    dir,
	}), nil, nil
}
