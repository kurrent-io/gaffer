package mcpserver

import (
	"context"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerResources() {
	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://project/config",
		Name:        "gaffer.toml",
		Description: "The project's gaffer.toml configuration file. Shows all projections, their entry points, and project settings.",
		MIMEType:    "application/toml",
	}, s.handleConfigResource)
}

func (s *Server) handleConfigResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	data, err := os.ReadFile(filepath.Join(s.root, "gaffer.toml"))
	if err != nil {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: "application/toml",
			Text:     string(data),
		}},
	}, nil
}
