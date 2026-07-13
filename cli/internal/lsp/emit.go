package lsp

import (
	"fmt"
	"iter"
	"path/filepath"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
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
	target := liveDebugTarget(desc.Environments)
	for _, p := range desc.Projections {
		if p.Diagnostic != nil {
			// Projection-level issue (missing name, missing entry,
			// duplicate name, escape) is non-actionable - render no
			// lenses for this projection. The diagnostic surfaces
			// the problem.
			continue
		}
		// Projection-level Debug lens - the one-click "go live" action.
		// Emitted only when there's an unambiguous live target (a
		// default env, or the sole env); otherwise the user picks an
		// environment via "Debug from..." and clicking a bare Debug
		// would have nothing to resolve to.
		if target != "" {
			out = append(out, CodeLens{
				Range: rangeToLSP(p.Range),
				Command: &Command{
					Title:   "Debug",
					Command: CommandDebugProjection,
					Arguments: []any{
						projectionArgs{Name: p.Name, ConfigURI: uri, Env: target},
					},
				},
				Data: &CodeLensData{Intent: IntentDebug},
			})
		}

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
					Arguments: []any{
						projectionArgs{Name: p.Name, ConfigURI: uri, Fixture: fx.Name},
					},
				},
				Data: &CodeLensData{Intent: IntentDebug},
			})
		}
		if len(validNames) > 0 || len(desc.Environments) > 0 {
			out = append(out, CodeLens{
				Range: rangeToLSP(p.Range),
				Command: &Command{
					Title:   "Debug from...",
					Command: CommandDebugProjectionPick,
					Arguments: []any{
						projectionPickArgs{
							Name:         p.Name,
							ConfigURI:    uri,
							FixtureNames: validNames,
							Envs:         desc.Environments,
						},
					},
				},
				Data: &CodeLensData{Intent: IntentDebugChoose},
			})
		}
	}
	return out
}

// liveDebugTarget returns the env name a one-click Debug should run
// against, or "" when there's no unambiguous live target - in which case
// the projection-level Debug lens is suppressed in favour of "Debug
// from...". The target is the single default env, or the sole configured
// env when none is marked default. Two or more envs with no default is
// ambiguous; so is more than one default (an invalid config the loose
// describe path doesn't reject - emitting a one-click Debug there would
// target an arbitrary env and fault at launch).
func liveDebugTarget(envs []config.EnvDescription) string {
	var defaults []string
	for _, e := range envs {
		if e.Default {
			defaults = append(defaults, e.Name)
		}
	}
	if len(defaults) == 1 {
		return defaults[0]
	}
	if len(defaults) == 0 && len(envs) == 1 {
		return envs[0].Name
	}
	return ""
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
	// Env is the environment to run live against, set on the
	// projection-level Debug lens (the resolved live target). Empty on
	// per-fixture lenses, which run a fixture rather than connecting.
	Env string `json:"env,omitempty"`
}

// projectionPickArgs is the payload for the dropdown lens. Includes
// the available fixture names and environments so the editor extension
// can pop a quick-pick without re-fetching.
type projectionPickArgs struct {
	Name         string                  `json:"name"`
	ConfigURI    string                  `json:"configURI"`
	FixtureNames []string                `json:"fixtureNames"`
	Envs         []config.EnvDescription `json:"envs,omitempty"`
}

// signInArgs is the Command.Arguments[0] payload for an env-block sign-in
// lens: which env needs authentication, and the declaring gaffer.toml.
type signInArgs struct {
	Env       string `json:"env"`
	ConfigURI string `json:"configURI"`
}

// emitStatusEnvLenses renders one env-block deploy-status lens per configured
// [env.<name>] with a located header, from the fetched status keyed by env
// name. An unauthenticated env gets a sign-in action; a failed fetch gets a
// muted "status unavailable"; a successful one gets the non-clickable roll-up.
// An env with no cached entry yet (fetch in flight) renders nothing - it pops
// in when the fetch lands and the client re-requests.
func emitStatusEnvLenses(desc config.Description, uri string, statuses map[string]envStatus) []CodeLens {
	out := []CodeLens{}
	if len(statuses) == 0 {
		return out
	}
	for _, env := range desc.Environments {
		// No source line to anchor on (quoted key, or a sub-table-only
		// declaration) - the scan left the range zero.
		if env.Range == (config.SourceRange{}) {
			continue
		}
		st, ok := statuses[env.Name]
		if !ok {
			continue
		}
		r := rangeToLSP(env.Range)
		switch {
		case st.Unauthenticated:
			out = append(out, CodeLens{
				Range: r,
				Command: &Command{
					Title:     "Sign in",
					Command:   CommandSignIn,
					Arguments: []any{signInArgs{Env: env.Name, ConfigURI: uri}},
				},
				Data: &CodeLensData{Intent: IntentSignIn},
			})
		case st.Err != nil:
			out = append(out, CodeLens{
				Range:   r,
				Command: &Command{Title: "status unavailable"},
				Data:    &CodeLensData{Intent: IntentStatusEnv},
			})
		default:
			out = append(out, CodeLens{
				Range:   r,
				Command: &Command{Title: statusRollup(st)},
				Data:    &CodeLensData{Intent: IntentStatusEnv},
			})
		}
	}
	return out
}

// statusRollup builds the env-block roll-up text from a fetched status: the
// in-config projection count, then the non-zero attention categories (or "in
// sync" when every in-config projection is clean), then any orphan/untracked
// projections - which live on the server but not in this config, so they
// surface nowhere else. A production target is flagged up front.
func statusRollup(st envStatus) string {
	var configured, changedExternally, localAhead, notDeployed, drifted, faulted, invalid, orphaned, untracked int
	for i := range st.Entries {
		e := st.Entries[i]
		switch e.Owner() {
		case drift.OwnerInConfig:
			configured++
			switch {
			case e.ExternallyChanged():
				changedExternally++
			case e.Attribution() == drift.AttrLocalAhead:
				localAhead++
			case e.State == drift.NotDeployed:
				notDeployed++
			case e.State == drift.Drifted:
				drifted++
			case e.State == drift.Invalid:
				invalid++
			}
			// Faulted is a runtime state orthogonal to drift, so it's counted
			// independently of the drift verdict above.
			if e.Runtime != nil && e.Runtime.State == remote.StateFaulted {
				faulted++
			}
		case drift.OwnerOrphan:
			orphaned++
		default: // foreign / unknown ownership both read as plain untracked
			untracked++
		}
	}

	// Labels are single-sourced from drift.Verdict's vocabulary (and
	// remote.StateFaulted), so a rename there lands here too.
	var issues []string
	add := func(n int, label string) {
		if n > 0 {
			issues = append(issues, fmt.Sprintf("%d %s", n, label))
		}
	}
	add(changedExternally, drift.LabelChangedExternally)
	add(localAhead, drift.LabelLocalAhead)
	add(notDeployed, drift.LabelNotDeployed)
	add(faulted, string(remote.StateFaulted))
	add(drifted, drift.LabelDrifted)
	add(invalid, drift.LabelInvalid)

	var segs []string
	if st.Production {
		segs = append(segs, "PRODUCTION")
	}
	if configured > 0 {
		segs = append(segs, fmt.Sprintf("%d %s", configured, plural(configured, "projection")))
		if len(issues) > 0 {
			segs = append(segs, issues...)
		} else {
			segs = append(segs, drift.LabelInSync)
		}
	}
	if orphaned > 0 {
		// "orphan" is a noun here, so it pluralizes; the other categories are
		// adjectival ("2 untracked", "2 drifted") and don't.
		segs = append(segs, fmt.Sprintf("%d %s", orphaned, plural(orphaned, drift.LabelOrphan)))
	}
	if untracked > 0 {
		segs = append(segs, fmt.Sprintf("%d %s", untracked, drift.LabelUntracked))
	}
	// Nothing configured and no anomalies - still name the (production) target.
	if configured == 0 && orphaned == 0 && untracked == 0 {
		segs = append(segs, "no projections")
	}
	return strings.Join(segs, " · ")
}

// plural appends an "s" to word unless n is exactly 1.
func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
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
// Path comparison goes through samePath so case-insensitive
// filesystems (Windows NTFS, macOS APFS default) match
// regardless of the client's chosen casing. We don't follow
// symlinks - a user opening a symlink path for an entry script
// that's resolved through EvalSymlinks in describe.go won't
// match here. Acceptable for V0.
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
		if !samePath(p.EntryAbsPath, target) {
			continue
		}
		tomlURI := pathToURI(parse.Description.ConfigFile)
		liveTarget := liveDebugTarget(parse.Description.Environments)
		if liveTarget != "" {
			out = append(out, CodeLens{
				Range: zeroRange,
				Command: &Command{
					Title:   `Debug "` + p.Name + `"`,
					Command: CommandDebugProjection,
					Arguments: []any{
						projectionArgs{Name: p.Name, ConfigURI: tomlURI, Env: liveTarget},
					},
				},
				Data: &CodeLensData{Intent: IntentDebug},
			})
		}
		validNames := make([]string, 0, len(p.Fixtures))
		for _, fx := range p.Fixtures {
			if fx.Diagnostic != nil {
				continue
			}
			validNames = append(validNames, fx.Name)
		}
		if len(validNames) > 0 || len(parse.Description.Environments) > 0 {
			out = append(out, CodeLens{
				Range: zeroRange,
				Command: &Command{
					Title:   `Debug "` + p.Name + `" from...`,
					Command: CommandDebugProjectionPick,
					Arguments: []any{
						projectionPickArgs{
							Name:         p.Name,
							ConfigURI:    tomlURI,
							FixtureNames: validNames,
							Envs:         parse.Description.Environments,
						},
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
