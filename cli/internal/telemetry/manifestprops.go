package telemetry

import (
	"maps"
	"slices"

	"github.com/kurrent-io/gaffer/cli/internal/config"
)

// ManifestFeaturesOf inspects a parsed gaffer.toml and returns a
// sorted, deduplicated list of canonical feature labels exercised by
// the manifest. Stamped on dev / mcp / manifest command_invoked
// events as `manifest_features_used`.
//
// Labels are gaffer-side (not raw TOML keys) so renaming a field in
// the TOML schema doesn't silently change wire output. A feature that
// can be declared per-projection (engine_version, quirks_version,
// execution_timeout) registers once if any projection sets it.
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
	if len(c.Env) > 0 {
		features["env"] = struct{}{}
	}
	if c.DatabaseConfig != nil && c.DatabaseConfig.CompilationTimeout != nil {
		features["compilation_timeout"] = struct{}{}
	}
	if c.DatabaseConfig != nil && c.DatabaseConfig.MaxStateSize != nil {
		features["max_state_size"] = struct{}{}
	}
	if len(c.Projection) > 0 {
		features["projections"] = struct{}{}
	}
	// engine_version is per-projection; quirks_version is top-level or
	// per-projection; execution_timeout is [database_config] or a
	// per-projection override. Any declaration marks the feature as in use.
	hasEngineVersion := false
	hasQuirksVersion := c.QuirksVersion != ""
	hasExecutionTimeout := c.DatabaseConfig != nil && c.DatabaseConfig.ExecutionTimeout != nil
	hasFixtures := false
	hasTrackEmitted := false
	for _, p := range c.Projection {
		if p.EngineVersion != nil {
			hasEngineVersion = true
		}
		if p.TrackEmittedStreams != nil {
			hasTrackEmitted = true
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
	if hasTrackEmitted {
		features["track_emitted_streams"] = struct{}{}
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
	return slices.Sorted(maps.Keys(features))
}
