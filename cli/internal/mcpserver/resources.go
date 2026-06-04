package mcpserver

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/kurrent-io/gaffer/cli/internal/project"
)

//go:embed resources/*.md resources/diagnostics.gen.json
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
		URI:         "gaffer://docs/quirks",
		Name:        "quirks",
		Description: "Catalogue of KurrentDB upstream quirks gaffer reproduces for fidelity. Look here when a fatal error reports a quirk.* code, or to see what quirks would fire for a given quirks_version.",
		MIMEType:    "text/markdown",
	}, s.trackedResource(quirksResource))

	// cli/TELEMETRY.md is the public telemetry contract for the CLI;
	// `just cli _resources` copies it to telemetry-info.gen.md (gitignored)
	// before any go build/test/lint, so the embedded copy is always a
	// build-step replica of the canonical file rather than a tracked
	// duplicate that can drift.
	s.mcp.AddResource(&mcp.Resource{
		URI:         "gaffer://telemetry/info",
		Name:        "telemetry-info",
		Description: "Public telemetry notice. What gaffer collects, what it does not, how to opt out, and how to request data deletion. Read before answering a user's telemetry question.",
		MIMEType:    "text/markdown",
	}, s.trackedResource(staticResource("resources/telemetry-info.gen.md")))
}

// diagnosticDoc mirrors one entry in the build-generated diagnostics catalogue
// (resources/diagnostics.gen.json), produced from the C# DiagnosticCatalog - the
// single source of truth (see the runtime's DiagnosticsArtifactTests). No runtime
// FFI dependency: the catalogue is embedded at build time.
type diagnosticDoc struct {
	Code     string  `json:"code"`
	Class    string  `json:"class"`
	Severity string  `json:"severity"`
	Message  string  `json:"message"`
	Docs     string  `json:"docs"`
	FixedIn  *string `json:"fixedIn"`
}

// quirksResource renders the quirk entries of the embedded diagnostics catalogue
// as markdown.
func quirksResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	raw, err := embeddedResources.ReadFile("resources/diagnostics.gen.json")
	if err != nil {
		return nil, fmt.Errorf("reading diagnostics catalogue: %w", err)
	}
	var docs []diagnosticDoc
	if err := json.Unmarshal(raw, &docs); err != nil {
		return nil, fmt.Errorf("parsing diagnostics catalogue: %w", err)
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      req.Params.URI,
			MIMEType: "text/markdown",
			Text:     renderQuirksMarkdown(docs),
		}},
	}, nil
}

func renderQuirksMarkdown(docs []diagnosticDoc) string {
	var sb strings.Builder
	sb.WriteString("# KurrentDB compat quirks\n\n")
	sb.WriteString("Each entry lists an upstream quirk that gaffer reproduces for ")
	sb.WriteString("fidelity. Quirks fire whenever `quirks_version` is unset (the ")
	sb.WriteString("\"unversioned\" default - matches all KurrentDB quirks) or set ")
	sb.WriteString("to a release earlier than the quirk's `fixedIn`. Setting ")
	sb.WriteString("`quirks_version` to a release that fixed the quirk disables ")
	sb.WriteString("reproduction.\n\n")
	rendered := 0
	for _, d := range docs {
		if d.Class != "quirk" {
			continue
		}
		rendered++
		fmt.Fprintf(&sb, "## %s\n\n", d.Code)
		body := d.Docs
		if body == "" {
			body = d.Message
		}
		if body != "" {
			fmt.Fprintf(&sb, "%s\n\n", body)
		}
		if d.FixedIn != nil {
			fmt.Fprintf(&sb, "**Fixed in:** KurrentDB %s\n\n", *d.FixedIn)
		} else {
			sb.WriteString("**Fixed in:** *not yet shipped upstream*\n\n")
		}
	}
	if rendered == 0 {
		sb.WriteString("*No quirks registered.*\n")
	}
	return sb.String()
}

func (s *Server) handleConfigResource(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	// Raw file read, not loadProject(): the config resource must expose
	// the manifest even when it fails to parse or validate - that's
	// exactly when an assistant needs to read it to debug.
	root := s.projectRoot()
	if root == "" {
		return nil, mcp.ResourceNotFoundError(req.Params.URI)
	}

	data, err := os.ReadFile(project.ConfigPath(root))
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
