package lsp

import (
	"net/url"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

// emitCodeLenses converts a config.Description into the LSP code
// lenses to render. Three kinds today:
//
//   - One projection-level Debug lens per [[projection]] header.
//   - One "Debug from fixture..." dropdown lens per projection that
//     has at least one valid fixture.
//   - One Debug lens per valid fixture's source line (dotted-key
//     form only - inline-table fixtures fall back to projection-
//     header range, which would collide with the dropdown lens, so
//     they're served by the dropdown only).
//
// Invalid fixtures and parse errors are diagnostics, not lenses
// (per the lens contract: every lens is actionable). Projection-
// level diagnostics suppress the projection's lenses entirely - a
// projection missing its name or entry isn't actionable.
//
// uri is the client's request URI, threaded through verbatim into
// the lens's configURI argument. Re-deriving it from
// desc.ConfigFile would risk subtle encoding mismatches (the
// client's URI string is the canonical handle, not a path the
// server transformed).
func emitCodeLenses(desc config.Description, uri string) []CodeLens {
	out := []CodeLens{}
	for _, p := range desc.Projections {
		if p.Diagnostic != nil {
			// Projection-level issue (missing name, missing entry,
			// duplicate name, escape) is non-actionable - render no
			// lenses for this projection. The diagnostic surfaces
			// the problem.
			continue
		}
		// Projection-level Debug lens.
		out = append(out, CodeLens{
			Range: rangeToLSP(p.Range),
			Command: &Command{
				Title:   "Debug",
				Command: CommandDebugProjection,
				Arguments: []interface{}{
					projectionArgs{Name: p.Name, ConfigURI: uri},
				},
			},
			Data: &CodeLensData{Intent: IntentDebug},
		})

		// Per-fixture lenses (valid only). Suppressed when the
		// fixture's range collapses to the projection-header range
		// (inline-table or [projection.fixtures] table form, where
		// no per-key source line exists) - the dropdown serves as
		// the entry point for those, no need to stack a redundant
		// per-fixture lens on the same line.
		validNames := make([]string, 0, len(p.Fixtures))
		for _, fx := range p.Fixtures {
			if fx.Diagnostic != nil {
				continue
			}
			validNames = append(validNames, fx.Name)
			if fx.Range == p.Range {
				continue
			}
			out = append(out, CodeLens{
				Range: rangeToLSP(fx.Range),
				Command: &Command{
					Title:   "Debug",
					Command: CommandDebugProjection,
					Arguments: []interface{}{
						projectionArgs{Name: p.Name, ConfigURI: uri, Fixture: fx.Name},
					},
				},
				Data: &CodeLensData{Intent: IntentDebug},
			})
		}
		if len(validNames) > 0 {
			out = append(out, CodeLens{
				Range: rangeToLSP(p.Range),
				Command: &Command{
					Title:   "Debug from fixture...",
					Command: CommandDebugProjectionPick,
					Arguments: []interface{}{
						projectionPickArgs{Name: p.Name, ConfigURI: uri, FixtureNames: validNames},
					},
				},
				Data: &CodeLensData{Intent: IntentDebugChoose},
			})
		}
	}
	return out
}

// projectionArgs is the Command.Arguments[0] payload for a
// projection-level or per-fixture Debug lens. Editor extensions
// receive this verbatim and pass it to their debug-launch handler.
//
// ConfigURI is a file:// URI (not a filesystem path) - matches
// LSP convention and what editor URI primitives expect.
type projectionArgs struct {
	Name      string `json:"name"`
	ConfigURI string `json:"configURI"`
	Fixture   string `json:"fixture,omitempty"`
}

// projectionPickArgs is the payload for the dropdown lens. Includes
// the available fixture names so the editor extension can pop a
// quick-pick without re-fetching.
type projectionPickArgs struct {
	Name         string   `json:"name"`
	ConfigURI    string   `json:"configURI"`
	FixtureNames []string `json:"fixtureNames"`
}

// emitDiagnostics flattens a Description's per-element diagnostics
// into a single slice for textDocument/publishDiagnostics. File-
// level diagnostics, projection diagnostics, and fixture
// diagnostics all flow through here.
func emitDiagnostics(desc config.Description) []LSPDiagnostic {
	out := []LSPDiagnostic{}
	for _, d := range desc.Diagnostics {
		out = append(out, configDiagToLSP(d))
	}
	for _, p := range desc.Projections {
		if p.Diagnostic != nil {
			out = append(out, configDiagToLSP(*p.Diagnostic))
		}
		for _, fx := range p.Fixtures {
			if fx.Diagnostic != nil {
				out = append(out, configDiagToLSP(*fx.Diagnostic))
			}
		}
	}
	return out
}

// configDiagToLSP wraps a config.Diagnostic into the LSP wire shape.
// Severity is always Error for V1 - every rule we emit is something
// the user needs to fix. If we ever add Warning-level rules
// (deprecated fields, stylistic), branch on Rule here.
func configDiagToLSP(d config.Diagnostic) LSPDiagnostic {
	return LSPDiagnostic{
		Range:    rangeToLSP(d.Range),
		Severity: DiagnosticSeverityError,
		Code:     d.Rule,
		Source:   "gaffer",
		Message:  d.Message,
	}
}

// rangeToLSP converts the config package's 1-indexed-line / 0-
// indexed-col SourceRange into the LSP wire format (0-indexed
// throughout). Negative inputs are clamped to 0; the upstream
// scanner doesn't produce them but defending against an unexpected
// regression is cheap.
func rangeToLSP(r config.SourceRange) Range {
	return Range{
		Start: Position{
			Line:      max0(r.StartLine - 1),
			Character: max0(r.StartCol),
		},
		End: Position{
			Line:      max0(r.EndLine - 1),
			Character: max0(r.EndCol),
		},
	}
}

func max0(x int) int {
	if x < 0 {
		return 0
	}
	return x
}

// pathToURI converts an absolute filesystem path to a file:// URI.
// Uses url.URL.String so the encoding matches what LSP clients
// produce - e.g. spaces become `%20`, `:` in path segments stays
// `:`. Hand-concatenating "file://" + EscapedPath would produce a
// non-canonical form that diverges from the client's URI string,
// breaking map-key lookups in the document store.
//
// V1 is Linux-only at the editor / LSP layer; Windows would need
// `file:///C:/...` shaping but no editor extension on Windows is
// in scope yet.
func pathToURI(path string) string {
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// uriToPath strips the file:// scheme and returns the absolute
// filesystem path. Returns the input unchanged if it doesn't look
// like a file URI - lets callers pass through raw paths during
// tests without needing a separate code path.
func uriToPath(uri string) string {
	if !strings.HasPrefix(uri, "file://") {
		return uri
	}
	u, err := url.Parse(uri)
	if err != nil {
		return uri
	}
	return u.Path
}
