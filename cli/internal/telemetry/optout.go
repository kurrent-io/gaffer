package telemetry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/userconfig"
)

// optOutEnvVars lists the env vars that disable telemetry when set to
// a truthy value. Order matters for reporting: when multiple are set,
// CheckOptOut names the first found here so the displayed reason is
// stable.
var optOutEnvVars = [...]string{
	"GAFFER_TELEMETRY_OPTOUT",
	"KURRENTDB_TELEMETRY_OPTOUT",
	"DO_NOT_TRACK",
}

// LayerState is the resolved state of one opt-out signal source.
type LayerState int

const (
	// LayerUnset: the signal source has no opinion. For user config,
	// the [telemetry] enabled key is absent. For env vars, none are
	// set. For workspace, no gaffer.toml found or the file doesn't
	// declare a telemetry key.
	LayerUnset LayerState = iota
	// LayerEnabled: the signal source explicitly opts in.
	LayerEnabled
	// LayerDisabled: the signal source explicitly opts out.
	LayerDisabled
)

// String renders the state as a status-line word ("unset" / "enabled" /
// "disabled"). Used by `gaffer config telemetry status`.
func (s LayerState) String() string {
	switch s {
	case LayerEnabled:
		return "enabled"
	case LayerDisabled:
		return "disabled"
	default:
		return "unset"
	}
}

// Layer captures one signal source's resolved state plus structured
// context the status command can render.
//
// Field semantics are uniform across layer kinds:
//
//   - Source: stable layer-kind constant ("user-config" / "env" /
//     "workspace"). Populated always so a renderer can produce per-row
//     headers regardless of state.
//   - Path: filesystem path that supplied the decision, populated only
//     when this layer carries a decision (State != LayerUnset) or an
//     error. User layer's Path is the user config file path; Workspace
//     layer's Path is the discovered gaffer.toml. Empty for Env.
//   - EnvVar: the environment variable that triggered an opt-out.
//     Populated only for Env when State == LayerDisabled.
//   - Value: the raw value that supplied the decision:
//     User/Workspace: "true" / "false"; Env: the raw env value (e.g.
//     "1"). Populated only when State != LayerUnset.
//   - Err: error encountered resolving this layer (malformed user
//     config, unreadable gaffer.toml). State stays LayerUnset on
//     error (fail-open); the status command surfaces Err alongside
//     the surviving layers. The emit gate (Resolved.IsDisabled)
//     ignores errors entirely.
type Layer struct {
	State  LayerState
	Source string
	Path   string
	EnvVar string
	Value  string
	Err    error
}

// Resolved is the layered opt-out resolution: each source's state
// independently, plus an Effective accessor for the any-silences
// answer.
type Resolved struct {
	// User: user-level config's [telemetry] enabled key.
	User Layer
	// Env: env vars (GAFFER_TELEMETRY_OPTOUT / KURRENTDB_TELEMETRY_OPTOUT
	// / DO_NOT_TRACK). When disabled, Source names the first env var
	// found in canonical order.
	Env Layer
	// Workspace: project gaffer.toml's telemetry key. Source is
	// "workspace"; Path points at the gaffer.toml that supplied the
	// value.
	Workspace Layer
}

// Effective returns the rolled-up state across all layers.
//
// Rule: "any-silences". LayerDisabled in any layer wins. Otherwise
// LayerEnabled if any layer is enabled. Otherwise LayerUnset.
//
// Example: a developer who opted in globally
// (User.State=LayerEnabled) but runs in a project whose checked-in
// gaffer.toml says telemetry = false (Workspace.State=LayerDisabled)
// effectively sends no telemetry for that project. This is
// deliberate - it lets a project owner opt their collaborators out
// without a precedence rule that would also let a checked-in
// gaffer.toml re-enable telemetry for someone who turned it off
// globally (the failure mode you can't take back).
func (r Resolved) Effective() LayerState {
	if r.User.State == LayerDisabled || r.Env.State == LayerDisabled || r.Workspace.State == LayerDisabled {
		return LayerDisabled
	}
	if r.User.State == LayerEnabled || r.Env.State == LayerEnabled || r.Workspace.State == LayerEnabled {
		return LayerEnabled
	}
	return LayerUnset
}

// IsDisabled is shorthand for r.Effective() == LayerDisabled. Used by
// the emit gate.
func (r Resolved) IsDisabled() bool { return r.Effective() == LayerDisabled }

// CheckOptOut resolves the three layers from the given sources. Errors
// are not returned at the top level; each Layer carries its own Err.
//
// store: the loaded user config (nil treated as user-layer unset).
// cwd: starting directory for the project gaffer.toml walk (typically
// os.Getwd()).
// homeDir: bound for the project walk - the walk stops before
// inspecting this directory, so a stray ~/gaffer.toml doesn't apply.
// Pass os.UserHomeDir()'s return value (empty string is acceptable;
// the walk then goes to filesystem root).
func CheckOptOut(store *userconfig.Store, cwd, homeDir string) Resolved {
	return checkOptOutWithEnv(store, cwd, homeDir, os.LookupEnv)
}

// checkOptOutWithEnv is the test seam: pass a custom lookup to avoid
// touching process-global env state and to enable t.Parallel.
func checkOptOutWithEnv(store *userconfig.Store, cwd, homeDir string, lookup func(string) (string, bool)) Resolved {
	return Resolved{
		User:      resolveUserLayer(store),
		Env:       resolveEnvLayer(lookup),
		Workspace: resolveWorkspaceLayer(cwd, homeDir),
	}
}

func resolveUserLayer(store *userconfig.Store) Layer {
	if store == nil {
		return Layer{State: LayerUnset, Source: "user-config"}
	}
	t, err := LoadTelemetry(store)
	// If LoadTelemetry surfaced an error, treat the layer as unset
	// (fail-open) but propagate the error so the status command can
	// warn. Specifically: do NOT trust t.Enabled when err != nil -
	// per-field tolerance may have left it nil or populated, but the
	// signal as a whole is unreliable and emitting "User: enabled" or
	// "User: disabled" alongside "user config: parse error" would
	// contradict itself.
	if err != nil {
		return Layer{State: LayerUnset, Source: "user-config", Path: store.Path(), Err: fmt.Errorf("user config: %w", err)}
	}
	if t.Enabled == nil {
		return Layer{State: LayerUnset, Source: "user-config"}
	}
	if *t.Enabled {
		return Layer{State: LayerEnabled, Source: "user-config", Path: store.Path(), Value: "true"}
	}
	return Layer{State: LayerDisabled, Source: "user-config", Path: store.Path(), Value: "false"}
}

func resolveEnvLayer(lookup func(string) (string, bool)) Layer {
	for _, name := range optOutEnvVars {
		v, ok := lookup(name)
		if !ok {
			continue
		}
		if isTruthy(v) {
			return Layer{State: LayerDisabled, Source: "env", EnvVar: name, Value: v}
		}
	}
	return Layer{State: LayerUnset, Source: "env"}
}

func resolveWorkspaceLayer(cwd, homeDir string) Layer {
	if cwd == "" {
		return Layer{State: LayerUnset, Source: "workspace"}
	}
	root := findProjectRootBounded(cwd, homeDir)
	if root == "" {
		return Layer{State: LayerUnset, Source: "workspace"}
	}
	path := filepath.Join(root, project.ConfigFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// findProjectRootBounded confirmed the file existed at
			// stat time, but it's gone now. Most likely the user
			// deleted it mid-run; possible but rare. Treat as unset
			// rather than confusing the renderer with a "file not
			// found" message that looks like absence.
			return Layer{State: LayerUnset, Source: "workspace"}
		}
		return Layer{State: LayerUnset, Source: "workspace", Path: path, Err: fmt.Errorf("workspace: read %s: %w", path, err)}
	}
	var partial struct {
		Telemetry *bool `toml:"telemetry"`
	}
	if err := toml.Unmarshal(data, &partial); err != nil {
		return Layer{State: LayerUnset, Source: "workspace", Path: path, Err: fmt.Errorf("workspace: parse %s: %w", path, err)}
	}
	if partial.Telemetry == nil {
		// gaffer.toml exists but doesn't declare a telemetry key.
		// Path stays empty: the renderer's "workspace: not set" line
		// shouldn't carry a path that didn't carry a decision.
		return Layer{State: LayerUnset, Source: "workspace"}
	}
	if *partial.Telemetry {
		return Layer{State: LayerEnabled, Source: "workspace", Path: path, Value: "true"}
	}
	return Layer{State: LayerDisabled, Source: "workspace", Path: path, Value: "false"}
}

// findProjectRootBounded walks up from start looking for gaffer.toml,
// stopping before it inspects stopAt. Both paths are filepath.Clean'd
// at entry so trailing-slash mismatches don't cause the bound to miss.
//
// Empty start returns "" - callers must supply an absolute start. We
// don't fall back to filepath.Clean("") which would walk cwd; that's
// a programmer error and silent cwd-fallback would surprise.
//
// Empty stopAt walks all the way to the filesystem root. This is
// unsafe for opt-out policy (which specifically wants the bound), so
// resolveWorkspaceLayer's callers should pass os.UserHomeDir()'s
// result. Tests that don't care about the bound pass an empty
// stopAt deliberately.
//
// This walker is telemetry-local rather than living in the project
// package: the bound is opt-out policy ("don't apply a stray
// ~/gaffer.toml"), not project semantics. project.FindRoot stays
// unbounded for build / dev / etc.
//
// Symlinks aren't resolved - callers wanting symlink-correct bounds
// must filepath.EvalSymlinks both arguments first. In practice cwd
// from os.Getwd and homeDir from os.UserHomeDir come back in matching
// resolved form on the same process.
func findProjectRootBounded(start, stopAt string) string {
	if start == "" {
		return ""
	}
	dir := filepath.Clean(start)
	if stopAt != "" {
		stopAt = filepath.Clean(stopAt)
	}
	for {
		if stopAt != "" && dir == stopAt {
			return ""
		}
		if _, err := os.Stat(filepath.Join(dir, project.ConfigFileName)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// isTruthy parses an env-var value with the same permissive rules
// gaffer applies elsewhere: "1", "true", "yes", "on" (case-
// insensitive, whitespace-trimmed). Anything else is false, including
// empty string and "0" / "false" / "no" / "off". Matches the value
// set KurrentDB's CLI uses for KURRENTDB_TELEMETRY_OPTOUT so cross-
// product opt-out behaves uniformly.
func isTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
