package lsp

import (
	"iter"
	"path/filepath"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

// validProjections yields each (parse, projection) pair across
// every cached parse where the projection has no header-level
// diagnostic. Skips non-actionable projections so callers don't
// have to repeat the `if p.Diagnostic != nil { continue }` dance.
func validProjections(parses []parseResult) iter.Seq2[parseResult, config.ProjectionDescription] {
	return func(yield func(parseResult, config.ProjectionDescription) bool) {
		for _, parse := range parses {
			for _, p := range parse.Description.Projections {
				if p.Diagnostic != nil {
					continue
				}
				if !yield(parse, p) {
					return
				}
			}
		}
	}
}

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

// emitEntryScriptLenses returns Debug lenses for a non-toml URI
// (typically a projection's entry .js) by scanning every cached
// parse for a projection whose resolved entry path matches.
//
// One lens per matching projection - a single .js file can be
// the entry for multiple projections (e.g. a shared handler used
// by separate fixtures bundles). Each lens is anchored at line 0
// since the entry script doesn't have a meaningful "projection
// header" line of its own. The projection name is woven into
// every lens title so stacked lenses on the same line stay
// distinguishable; toml-side lenses don't need this because the
// projection name is visible in the surrounding source.
//
// Path comparison is syntactic on filepath.Clean output. V1 is
// Linux-only and we don't follow symlinks - a user opening a
// symlink path for an entry script that's resolved through
// EvalSymlinks in describe.go won't match here. Acceptable for
// V1; revisit when other-OS support lands.
//
// uri is the URI the client opened. We compare absolute paths
// because clients sometimes encode URIs with trailing slashes,
// extra encoding, etc.; uriToPath canonicalises both sides.
func emitEntryScriptLenses(parses []parseResult, uri string) []CodeLens {
	target := uriToPath(uri)
	if target == "" {
		return []CodeLens{}
	}
	out := []CodeLens{}
	zeroRange := Range{Start: Position{Line: 0, Character: 0}, End: Position{Line: 0, Character: 0}}
	for parse, p := range validProjections(parses) {
		if p.EntryAbsPath != target {
			continue
		}
		tomlURI := pathToURI(parse.Description.ConfigFile)
		out = append(out, CodeLens{
			Range: zeroRange,
			Command: &Command{
				Title:   `Debug "` + p.Name + `"`,
				Command: CommandDebugProjection,
				Arguments: []interface{}{
					projectionArgs{Name: p.Name, ConfigURI: tomlURI},
				},
			},
			Data: &CodeLensData{Intent: IntentDebug},
		})
		validNames := make([]string, 0, len(p.Fixtures))
		for _, fx := range p.Fixtures {
			if fx.Diagnostic != nil {
				continue
			}
			validNames = append(validNames, fx.Name)
		}
		if len(validNames) > 0 {
			out = append(out, CodeLens{
				Range: zeroRange,
				Command: &Command{
					Title:   `Debug "` + p.Name + `" from fixture...`,
					Command: CommandDebugProjectionPick,
					Arguments: []interface{}{
						projectionPickArgs{Name: p.Name, ConfigURI: tomlURI, FixtureNames: validNames},
					},
				},
				Data: &CodeLensData{Intent: IntentDebugChoose},
			})
		}
	}
	return out
}

// emitWorkspaceSymbols turns every cached projection into a
// SymbolInformation. Used for both Cmd+T navigation in the editor
// and as the data source for the extension's QuickPick. We emit
// one symbol per valid projection; projections with a header-level
// diagnostic (missing name, escape) are skipped so the result is
// guaranteed actionable.
//
// Container name is the relative-style toml filename so editors
// that group symbols by container display "checkout - gaffer.toml"
// rather than the raw absolute path.
func emitWorkspaceSymbols(parses []parseResult) []SymbolInformation {
	out := []SymbolInformation{}
	for parse, p := range validProjections(parses) {
		out = append(out, SymbolInformation{
			Name: p.Name,
			Kind: SymbolKindFunction,
			Location: Location{
				URI:   pathToURI(parse.Description.ConfigFile),
				Range: rangeToLSP(p.Range),
			},
			ContainerName: filepath.Base(parse.Description.ConfigFile),
		})
	}
	return out
}

// emitDiagnostics flattens a Description's per-element diagnostics
// into a single slice for textDocument/publishDiagnostics. File-
// level diagnostics, projection diagnostics, and fixture
// diagnostics all flow through here.
func emitDiagnostics(desc config.Description) []lspDiagnostic {
	out := []lspDiagnostic{}
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
func configDiagToLSP(d config.Diagnostic) lspDiagnostic {
	return lspDiagnostic{
		Range:    rangeToLSP(d.Range),
		Severity: diagnosticSeverityError,
		Code:     d.Rule,
		Source:   "gaffer",
		Message:  d.Message,
	}
}
