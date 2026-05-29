package mcpserver

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *Server) registerPrompts() {
	s.mcp.AddPrompt(&mcp.Prompt{
		Name:        "write-projection",
		Description: "Write a new KurrentDB projection. Pre-loads the API reference, gotchas, examples, and project context.",
		Arguments: []*mcp.PromptArgument{
			{Name: "requirements", Description: "What the projection should do", Required: true},
		},
	}, s.handleWriteProjectionPrompt)

	s.mcp.AddPrompt(&mcp.Prompt{
		Name:        "fix-projection",
		Description: "Fix a broken or incorrect projection. Pre-loads the projection source, error context, and API reference.",
		Arguments: []*mcp.PromptArgument{
			{Name: "name", Description: "Projection name from gaffer.toml", Required: true},
			{Name: "problem", Description: "What's wrong or what the expected behavior should be", Required: false},
		},
	}, s.handleFixProjectionPrompt)
}

func (s *Server) handleWriteProjectionPrompt(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	requirements := req.Params.Arguments["requirements"]

	// Best-effort: the docs render with or without a project. When
	// one is loaded, fold in its config and the v1 reference.
	cfg, root, _ := s.project()

	apiRef := mustReadEmbed("resources/projection-api.md")
	gotchas := mustReadEmbed("resources/gotchas.md")
	examples := mustReadEmbed("resources/examples.md")

	var sb strings.Builder
	sb.WriteString("Write a KurrentDB projection using gaffer.\n\n")
	sb.WriteString("## Requirements\n\n")
	sb.WriteString(requirements)
	sb.WriteString("\n\n")
	sb.WriteString("## Workflow\n\n")
	sb.WriteString("1. Call `list_projections` to see existing projections\n")
	sb.WriteString("2. Call `list_events` to discover event types and their data shapes\n")
	sb.WriteString("3. Call `scaffold` to create the projection file and config entry\n")
	sb.WriteString("4. Edit the source file with the handler logic\n")
	sb.WriteString("5. Call `run` with fixture events to test\n")
	sb.WriteString("6. Call `get_timeline` to scan results, `get_step` to inspect specific events\n")
	sb.WriteString("7. Iterate until the output is correct\n\n")

	if cfg != nil {
		if configData, err := os.ReadFile(project.ConfigPath(root)); err == nil {
			sb.WriteString("## Project config\n\n```toml\n")
			sb.Write(configData)
			sb.WriteString("```\n\n")
		}
	}

	sb.WriteString("---\n\n")
	sb.Write(apiRef)
	sb.WriteString("\n\n---\n\n")
	sb.Write(gotchas)
	sb.WriteString("\n\n---\n\n")
	sb.Write(examples)

	if cfg != nil && hasV1Projections(cfg) {
		sb.WriteString("\n\n---\n\n")
		sb.Write(mustReadEmbed("resources/v1-v2-differences.md"))
	}

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Write a projection: %s", requirements),
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: sb.String()}},
		},
	}, nil
}

func (s *Server) handleFixProjectionPrompt(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	name := req.Params.Arguments["name"]
	problem := req.Params.Arguments["problem"]

	cfg, root, err := s.project()
	if err != nil {
		return nil, fmt.Errorf("loading gaffer.toml: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("no gaffer project found - fix-projection needs a project; run gaffer init first")
	}

	proj := cfg.FindProjection(name)
	if proj == nil {
		return nil, fmt.Errorf("projection %q not found in gaffer.toml", name)
	}

	source, err := engine.ReadSource(root, proj.Entry)
	if err != nil {
		return nil, err
	}

	apiRef := mustReadEmbed("resources/projection-api.md")
	gotchas := mustReadEmbed("resources/gotchas.md")

	var sb strings.Builder
	fmt.Fprintf(&sb, "Fix the projection `%s`.\n\n", name)

	if problem != "" {
		sb.WriteString("## Problem\n\n")
		sb.WriteString(problem)
		sb.WriteString("\n\n")
	}

	sb.WriteString("## Workflow\n\n")
	sb.WriteString("1. Read the source below and identify the issue\n")
	sb.WriteString("2. Call `run` with fixture events to reproduce the problem\n")
	sb.WriteString("3. Call `get_timeline` and `get_step` to find where it goes wrong\n")
	sb.WriteString("4. Call `debug` with `break_at` to inspect state at the failing event\n")
	sb.WriteString("5. Call `evaluate` to test expressions while paused\n")
	sb.WriteString("6. Fix the source and re-run to verify\n\n")

	fmt.Fprintf(&sb, "## Source (`%s`)\n\n```javascript\n", proj.Entry)
	sb.WriteString(source)
	sb.WriteString("```\n\n---\n\n")
	sb.Write(apiRef)
	sb.WriteString("\n\n---\n\n")
	sb.Write(gotchas)

	if cfg.EffectiveEngineVersion(proj) == 1 {
		sb.WriteString("\n\n---\n\n")
		sb.Write(mustReadEmbed("resources/v1-v2-differences.md"))
	}

	return &mcp.GetPromptResult{
		Description: fmt.Sprintf("Fix projection: %s", name),
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: sb.String()}},
		},
	}, nil
}

func hasV1Projections(cfg *config.Config) bool {
	for _, proj := range cfg.Projection {
		if cfg.EffectiveEngineVersion(&proj) == 1 {
			return true
		}
	}
	return false
}

func mustReadEmbed(path string) []byte {
	data, err := embeddedResources.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("missing embedded resource: %s", path))
	}
	return data
}
