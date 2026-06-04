package telemetry

import (
	"sort"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

// ManifestFeaturesOf inspects a parsed gaffer.toml and returns a
// sorted, deduplicated list of canonical feature labels exercised by
// the manifest. Stamped on dev / mcp / manifest command_invoked
// events as `manifest_features_used`.
//
// Labels are gaffer-side (not raw TOML keys) so renaming a field in
// the TOML schema doesn't silently change wire output. Per-projection
// overrides count toward the same top-level label - e.g. one
// projection setting `engine_version` registers the `engine_version`
// feature whether or not the top-level key is set.
//
// Privacy: section *presence* only; no values, names, or paths cross
// this boundary.
//
// Extend deliberately as new capabilities ship. The introspector
// lives here (not on *config.Config) because the label set is part of
// the telemetry contract, not the config's API.
func ManifestFeaturesOf(c *config.Config) []string {
	if c == nil {
		return nil
	}
	features := map[string]struct{}{}
	if c.Connection != "" {
		features["connection"] = struct{}{}
	}
	if c.CompilationTimeout != nil {
		features["compilation_timeout"] = struct{}{}
	}
	if len(c.Projection) > 0 {
		features["projections"] = struct{}{}
	}
	// engine_version, quirks_version, execution_timeout can live at top
	// level OR on individual projections. Either declaration marks
	// the feature as in use.
	hasEngineVersion := c.EngineVersion != nil
	hasQuirksVersion := c.QuirksVersion != ""
	hasExecutionTimeout := c.ExecutionTimeout != nil
	hasFixtures := false
	for _, p := range c.Projection {
		if p.EngineVersion != nil {
			hasEngineVersion = true
		}
		if p.QuirksVersion != "" {
			hasQuirksVersion = true
		}
		if p.ExecutionTimeout != nil {
			hasExecutionTimeout = true
		}
		if len(p.Fixtures) > 0 {
			hasFixtures = true
		}
	}
	if hasEngineVersion {
		features["engine_version"] = struct{}{}
	}
	if hasQuirksVersion {
		features["quirks_version"] = struct{}{}
	}
	if hasExecutionTimeout {
		features["execution_timeout"] = struct{}{}
	}
	if hasFixtures {
		features["fixtures"] = struct{}{}
	}
	out := make([]string, 0, len(features))
	for k := range features {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
