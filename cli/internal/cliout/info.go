// Package cliout owns JSON shapes adopted by more than one gaffer
// surface: the `info` envelope (used by `gaffer info --json`, the MCP
// `get_projection_info` tool, and the `gaffer dev --json` stream via
// BuildInfoCore), plus the small formatting helpers they share.
// Surfaces that opt in get drift protection for free.
package cliout

import (
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

// BuildInfoCore returns the subset of projection-info keys that every
// "this is projection X" surface emits: name, source, quirksVersion,
// engineVersion, and the conditional event-/category-/stream-/
// partitioning-/diagnostics-shaped fields. The streaming
// `gaffer dev --json` writer uses this directly so its info envelope
// stays in step with `info --json`'s body without inheriting the
// configuration-time fields (entry, fixtures, biState, producesResults)
// that the stream form omits.
func BuildInfoCore(proj *engine.Projection, info gafferruntime.ProjectionInfo) map[string]any {
	src := engine.DescribeSource(info)
	out := map[string]any{
		"name":          proj.Def.Name,
		"engineVersion": proj.EngineVersion,
		"source":        src["type"],
		// Always emit quirksVersion: null distinguishes unversioned (quirks on)
		// from a real version. Consumers need this signal explicitly.
		"quirksVersion": NullableString(proj.QuirksVersion),
	}
	if cats, ok := src["categories"]; ok {
		out["categories"] = cats
	}
	if streams, ok := src["streams"]; ok {
		out["streams"] = streams
	}
	if len(info.Events) > 0 {
		out["events"] = info.Events
	}
	if p := engine.DescribePartitioning(info); p != "none" {
		out["partitioning"] = p
	}
	if len(info.Diagnostics) > 0 {
		out["diagnostics"] = info.Diagnostics
	}
	return out
}

// BuildInfoJSON returns the flat JSON-ready map that `gaffer info --json`
// emits, and that the MCP get_projection_info tool returns. Built by
// taking the shared BuildInfoCore subset and adding the configuration-
// time fields (entry, biState, producesResults, fixtures) that callers
// inspecting a configured projection want to see.
//
// Returns map[string]any (not a typed struct) because the output is a
// conditional union: six fields are present only when the projection
// declares them (categories, streams, events, partitioning,
// diagnostics, fixtures) and would each need a typed omitempty tag.
// A map keeps the conditional shape readable; readers that need a
// typed view can decode the result downstream.
func BuildInfoJSON(proj *engine.Projection, info gafferruntime.ProjectionInfo) map[string]any {
	out := BuildInfoCore(proj, info)
	out["entry"] = proj.Def.Entry
	out["biState"] = info.BiState
	out["producesResults"] = info.ProducesResults
	if len(proj.Def.Fixtures) > 0 {
		names := proj.Def.FixtureNames()
		fixtures := make([]map[string]any, len(names))
		for i, name := range names {
			fixtures[i] = map[string]any{
				"name": name,
				"path": proj.Def.Fixtures[name],
			}
		}
		out["fixtures"] = fixtures
	}
	return out
}

// NullableString returns s when non-empty or nil otherwise, so JSON
// output distinguishes "unset" from explicitly-empty.
func NullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
