package mcpserver

import (
	"context"
	"embed"
	"fmt"
	"os"
	"strings"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kurrent-io/gaffer/cli/internal/project"
)

//go:embed resources/*.md
var embeddedResources embed.FS

func (s *Server) registerResources() {
	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://project/config",
		Name:        project.ConfigFileName,
		Description: "The project's gaffer.toml configuration file. Shows all projections, their entry points, and project settings.",
		MIMEType:    "application/toml",
	}, s.trackedResource(s.handleConfigResource))

	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://docs/projection-api",
		Name:        "projection-api",
		Description: "Full API reference for KurrentDB projections. Source functions, chain methods, handlers, event envelope, side effects, biState, options.",
		MIMEType:    "text/markdown",
	}, s.trackedResource(staticResource("resources/projection-api.md")))

	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://docs/gotchas",
		Name:        "gotchas",
		Description: "Common mistakes and surprising behavior when writing projections. Read this before writing your first projection.",
		MIMEType:    "text/markdown",
	}, s.trackedResource(staticResource("resources/gotchas.md")))

	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://docs/examples",
		Name:        "examples",
		Description: "Working projection patterns: counter, per-stream aggregation, partitioning, biState, emit, linkTo, deletion, transforms.",
		MIMEType:    "text/markdown",
	}, s.trackedResource(staticResource("resources/examples.md")))

	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://docs/v1-v2-differences",
		Name:        "v1-v2-differences",
		Description: "Behavioral differences between V1 and V2 projection engines. Read when working with V1 projections or migrating.",
		MIMEType:    "text/markdown",
	}, s.trackedResource(staticResource("resources/v1-v2-differences.md")))

	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://docs/db-version-bugs",
		Name:        "db-version-bugs",
		Description: "Catalogue of KurrentDB upstream bugs gaffer reproduces for fidelity. Look here when a fatal error reports a compat.* code, or to see what bugs would fire for a given db_version.",
		MIMEType:    "text/markdown",
	}, s.trackedResource(dbVersionBugsResource))
}

// dbVersionBugsResource auto-generates a markdown reference from the runtime's
// KnownBugs registry. Single source of truth: the C# Sdk.Versioning.KnownBugs
// table flows through gaffer_known_bugs() into this rendering.
func dbVersionBugsResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	bugs, err := gafferruntime.KnownBugs()
	if err != nil {
		return nil, fmt.Errorf("loading known-bugs registry: %w", err)
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     renderDbVersionBugsMarkdown(bugs),
		}},
	}, nil
}

func renderDbVersionBugsMarkdown(bugs []gafferruntime.KnownBug) string {
	var sb strings.Builder
	sb.WriteString("# KurrentDB compat bugs\n\n")
	sb.WriteString("Each entry lists an upstream bug that gaffer reproduces for ")
	sb.WriteString("fidelity. Bugs fire whenever `db_version` is unset (the ")
	sb.WriteString("\"unversioned\" default - matches all KurrentDB quirks) or set ")
	sb.WriteString("to a release earlier than the bug's `fixedIn`. Setting ")
	sb.WriteString("`db_version` to a release that fixed the bug disables ")
	sb.WriteString("reproduction.\n\n")
	if len(bugs) == 0 {
		sb.WriteString("*No bugs registered in the runtime.*\n")
		return sb.String()
	}
	for _, b := range bugs {
		fmt.Fprintf(&sb, "## %s\n\n", b.Code)
		if b.Description != "" {
			fmt.Fprintf(&sb, "%s\n\n", b.Description)
		}
		if b.FixedIn != nil {
			fmt.Fprintf(&sb, "**Fixed in:** KurrentDB %s\n\n", *b.FixedIn)
		} else {
			sb.WriteString("**Fixed in:** *not yet shipped upstream*\n\n")
		}
	}
	return sb.String()
}

func (s *Server) handleConfigResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	data, err := os.ReadFile(project.ConfigPath(s.root))
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
