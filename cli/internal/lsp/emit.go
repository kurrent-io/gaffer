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
							Envs:         lensEnvs(desc.Environments),
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

// lensEnv is the minimal environment shape a lens command carries: the client
// builds its env picker from the name and default flag alone. Narrower than
// config.EnvDescription on purpose - the full form also carries the header's
// source range, which the client ignores, and a lens payload repeats its env
// list once per projection, so shipping the range would be dead weight scaled by
// projections × envs.
type lensEnv struct {
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
}

func lensEnvs(envs []config.EnvDescription) []lensEnv {
	out := make([]lensEnv, len(envs))
	for i, e := range envs {
		out[i] = lensEnv{Name: e.Name, Default: e.Default}
	}
	return out
}

// projectionPickArgs is the payload for the dropdown lens. Includes
// the available fixture names and environments so the editor extension
// can pop a quick-pick without re-fetching.
type projectionPickArgs struct {
	Name         string    `json:"name"`
	ConfigURI    string    `json:"configURI"`
	FixtureNames []string  `json:"fixtureNames"`
	Envs         []lensEnv `json:"envs,omitempty"`
}

// signInArgs is the Command.Arguments[0] payload for an env-block sign-in
// lens: which env needs authentication, and the declaring gaffer.toml.
type signInArgs struct {
	Env       string `json:"env"`
	ConfigURI string `json:"configURI"`
}

// deployEnvArgs is the Command.Arguments[0] payload for the env-block deploy
// lenses (Preview today, Deploy to come): which env to plan against, and the
// declaring gaffer.toml the client resolves the project root from.
type deployEnvArgs struct {
	Env       string `json:"env"`
	ConfigURI string `json:"configURI"`
}

// actionsEnv is one env in the "Manage..." payload. Beyond the env identity it
// carries the two bits the operate menu needs that lensEnv doesn't: whether the
// env is production (picks the confirm tier) and this projection's runtime state
// on it (picks pause vs resume). State is "" when unknown - not deployed, not yet
// fetched, or sign-in needed - and the client falls back to offering both.
type actionsEnv struct {
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
	// Production is a pointer so the client can tell "known non-production"
	// (false) from "not yet known" (nil/omitted). Never treat unknown as
	// non-production: the editor fails a confirm-tier decision safe when it's nil.
	Production *bool  `json:"production,omitempty"`
	State      string `json:"state,omitempty"`
	// Emits is whether the deployed projection emits streams, so the editor only
	// offers the delete-and-emitted-streams choice when it's meaningful.
	Emits bool `json:"emits,omitempty"`
	// Status flags a non-actionable env for the menu: "auth" (sign-in needed, so
	// the actions collapse to a sign-in) or "unavailable" (a failed read, shown as
	// context but not blocked). Empty when the env resolved, or has no status yet.
	Status string `json:"status,omitempty"`
}

// actionsEnvs builds the per-env cells for one projection's actions lens from the
// cached per-env status: production off the env's fetched status, state off the
// projection's runtime entry. Both stay unknown (production nil, state "") until
// the env's status has resolved.
func actionsEnvs(envs []config.EnvDescription, proj string, statuses map[string]envStatus) []actionsEnv {
	out := make([]actionsEnv, len(envs))
	for i, e := range envs {
		cell := actionsEnv{Name: e.Name, Default: e.Default}
		if st, ok := statuses[e.Name]; ok {
			switch {
			case st.Unauthenticated:
				cell.Status = "auth"
			case st.Err != nil:
				cell.Status = "unavailable"
			default:
				// Resolved: production, state, and emit are meaningful. (An errored
				// or sign-in-needed fetch leaves st.Production at its false zero
				// value, so it must not be read as non-production.)
				prod := st.Production
				cell.Production = &prod
				for j := range st.Entries {
					if st.Entries[j].Name != proj {
						continue
					}
					if st.Entries[j].Runtime != nil && st.Entries[j].Runtime.State != remote.StateUnknown {
						cell.State = string(st.Entries[j].Runtime.State)
					}
					if st.Entries[j].Deployed != nil {
						cell.Emits = st.Entries[j].Deployed.Emit
					}
					break
				}
			}
		}
		out[i] = cell
	}
	return out
}

// projectionActionsArgs is the Command.Arguments[0] payload for the
// per-projection "Manage..." lens: the projection to act on, its declaring
// gaffer.toml, and the configured environments (with production + runtime state)
// so the client can build the env-grouped action menu, pick pause-vs-resume, and
// choose the confirm tier without re-reading the config or re-fetching status.
type projectionActionsArgs struct {
	Name      string       `json:"name"`
	ConfigURI string       `json:"configURI"`
	Envs      []actionsEnv `json:"envs,omitempty"`
}

// emitStatusEnvLenses renders one env-block deploy-status lens per configured
// [env.<name>] with a located header. From the fetched status keyed by env
// name: an unauthenticated env gets a sign-in action; a failed fetch gets a
// muted "status unavailable"; a successful one gets the non-clickable roll-up.
// An env whose fetch is still in flight (in loading) gets a loading
// placeholder; one that's neither cached nor loading renders nothing.
func emitStatusEnvLenses(desc config.Description, uri string, statuses map[string]envStatus, loading map[string]bool) []CodeLens {
	out := []CodeLens{}
	for _, env := range desc.Environments {
		// No source line to anchor on (quoted key, or a sub-table-only
		// declaration) - the scan left the range zero.
		if env.Range == (config.SourceRange{}) {
			continue
		}
		r := rangeToLSP(env.Range)
		st, ok := statuses[env.Name]
		if !ok {
			if loading[env.Name] {
				out = append(out, CodeLens{
					Range:   r,
					Command: &Command{Title: "loading status..."},
					Data:    &CodeLensData{Intent: IntentStatusLoading},
				})
			}
			continue
		}
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
				Command: &Command{Title: "status unavailable", Tooltip: st.Err.Error()},
				Data:    &CodeLensData{Intent: IntentStatusEnv},
			})
		default:
			// Deploy leads the line as the action, ahead of the status roll-up:
			// it's the actionable affordance, and a fixed leading position keeps it
			// in the same place on every env block regardless of the roll-up's
			// length. Offered only when the env is reachable and authenticated (this
			// default case), not while loading or on a fetch error. It opens the
			// deploy plan for the whole project against this env.
			out = append(out, CodeLens{
				Range: r,
				Command: &Command{
					Title:     "Deploy",
					Command:   CommandDeployPreview,
					Arguments: []any{deployEnvArgs{Env: env.Name, ConfigURI: uri}},
				},
				Data: &CodeLensData{Intent: IntentDeployPreview},
			})
			out = append(out, CodeLens{
				Range:   r,
				Command: &Command{Title: statusRollup(st)},
				Data:    &CodeLensData{Intent: IntentStatusEnv},
			})
		}
	}
	return out
}

// emitStatusBadgeLenses renders one per-projection status marker anchored on
// each located, non-diagnostic [[projection]] header, carrying the projection's
// per-environment health (in file order) for the client to paint as a row of
// inline badges. An environment with no usable status contributes its reason
// ("locked" / "error" / "loading") rather than being dropped, so the row's
// length matches the configured environments and the client can distinguish why
// a reading is missing. A projection with no configured environment gets no
// marker. The lens has no command or title: it's a decoration data-carrier the
// client consumes, riding the codeLens channel so it refreshes with the env
// lenses.
func emitStatusBadgeLenses(desc config.Description, statuses map[string]envStatus, loading map[string]bool) []CodeLens {
	out := []CodeLens{}
	for _, p := range desc.Projections {
		if p.Diagnostic != nil {
			continue
		}
		if p.Range == (config.SourceRange{}) {
			continue
		}
		cells := projectionEnvCells(desc, p.Name, statuses, loading)
		if len(cells) == 0 {
			continue
		}
		healths := make([]string, len(cells))
		for i := range cells {
			healths[i] = cells[i].Marker
		}
		out = append(out, CodeLens{
			Range: rangeToLSP(p.Range),
			Data:  &CodeLensData{Intent: IntentStatusBadges, Healths: healths},
		})
	}
	return out
}

// emitActionsLenses renders one "Manage..." lens per located, non-diagnostic
// [[projection]] header - the entry point to the per-projection action menu
// (diff against deployed today; operate / history later). The client decorates
// the plain title with its own icon, like the Debug lenses. Emitted only on the
// status surface (a vscode-only capability) and only when the config declares
// at least one environment, since every action in the menu targets an env. A
// projection with a projection-level diagnostic or no anchorable header gets no
// lens, matching the status badge emitter.
func emitActionsLenses(desc config.Description, uri string, statuses map[string]envStatus) []CodeLens {
	out := []CodeLens{}
	if len(desc.Environments) == 0 {
		return out
	}
	for _, p := range desc.Projections {
		if p.Diagnostic != nil {
			continue
		}
		if p.Range == (config.SourceRange{}) {
			continue
		}
		out = append(out, CodeLens{
			Range: rangeToLSP(p.Range),
			Command: &Command{
				Title:   "Manage...",
				Command: CommandProjectionActions,
				Arguments: []any{
					projectionActionsArgs{Name: p.Name, ConfigURI: uri, Envs: actionsEnvs(desc.Environments, p.Name, statuses)},
				},
			},
			Data: &CodeLensData{Intent: IntentActions},
		})
	}
	return out
}

// statusRollup builds the env-block roll-up text from a fetched status: a count
// per state, led by how many are in sync, then the non-zero attention
// categories, then any orphan/untracked projections - which live on the server
// but not in this config, so they surface nowhere else. A production target is
// flagged up front. There's deliberately no total: the number of projections in
// gaffer.toml isn't the useful signal, the state breakdown is.
//
// The in-config drift states (in sync / changed externally / local ahead / not
// deployed / drifted / invalid) partition the configured set. faulted is a
// runtime state orthogonal to drift, counted independently, so a faulted
// projection also appears in its drift bucket.
func statusRollup(st envStatus) string {
	var inSync, changedExternally, localAhead, notDeployed, drifted, faulted, invalid, orphaned, untracked int
	for i := range st.Entries {
		e := st.Entries[i]
		switch e.Owner() {
		case drift.OwnerInConfig:
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
			case e.State == drift.InSync:
				inSync++
			}
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
	var segs []string
	if st.Production {
		segs = append(segs, "PRODUCTION")
	}
	add := func(n int, label string) {
		if n > 0 {
			segs = append(segs, fmt.Sprintf("%d %s", n, label))
		}
	}
	add(inSync, drift.LabelInSync)
	add(changedExternally, drift.LabelChangedExternally)
	add(localAhead, drift.LabelLocalAhead)
	add(notDeployed, drift.LabelNotDeployed)
	add(faulted, string(remote.StateFaulted))
	add(drifted, drift.LabelDrifted)
	add(invalid, drift.LabelInvalid)
	// "orphan" is a noun here, so it pluralizes; the other categories are
	// adjectival ("2 untracked", "2 drifted") and don't.
	if orphaned > 0 {
		segs = append(segs, fmt.Sprintf("%d %s", orphaned, plural(orphaned, drift.LabelOrphan)))
	}
	add(untracked, drift.LabelUntracked)

	// Nothing to report at all - still name the (production) target.
	if len(segs) == 0 || (st.Production && len(segs) == 1) {
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
							Envs:         lensEnvs(parse.Description.Environments),
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
