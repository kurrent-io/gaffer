package mcpserver

import (
	"context"
	"embed"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed resources/*.md
var embeddedResources embed.FS

func (s *Server) registerResources() {
	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://project/config",
		Name:        "gaffer.toml",
		Description: "The project's gaffer.toml configuration file. Shows all projections, their entry points, and project settings.",
		MIMEType:    "application/toml",
	}, s.handleConfigResource)

	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://docs/projection-api",
		Name:        "projection-api",
		Description: "Full API reference for KurrentDB projections. Source functions, chain methods, handlers, event envelope, side effects, biState, options.",
		MIMEType:    "text/markdown",
	}, staticResource("resources/projection-api.md"))

	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://docs/gotchas",
		Name:        "gotchas",
		Description: "Common mistakes and surprising behavior when writing projections. Read this before writing your first projection.",
		MIMEType:    "text/markdown",
	}, staticResource("resources/gotchas.md"))

	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://docs/examples",
		Name:        "examples",
		Description: "Working projection patterns: counter, per-stream aggregation, partitioning, biState, emit, linkTo, deletion, transforms.",
		MIMEType:    "text/markdown",
	}, staticResource("resources/examples.md"))

	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://docs/v1-v2-differences",
		Name:        "v1-v2-differences",
		Description: "Behavioral differences between V1 and V2 projection engines. Read when working with V1 projections or migrating.",
		MIMEType:    "text/markdown",
	}, staticResource("resources/v1-v2-differences.md"))
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

func staticResource(path string) mcp.ResourceHandler {
	return func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		data, err := embeddedResources.ReadFile(path)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}

		return &mcp.ReadResourceResult{
			Contents: []*mcp.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "text/markdown",
				Text:     string(data),
			}},
		}, nil
	}
}
