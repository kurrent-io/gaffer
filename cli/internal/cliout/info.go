// Package cliout owns JSON shapes adopted by both the CLI and the MCP
// server: the `info` / get_projection_info envelope, the `manifest` /
// get_manifest envelope, and the small formatting helpers they share.
// Surfaces that opt in get drift protection for free; surfaces that
// don't (e.g. the streaming `dev --json` writer) still build their own
// shapes inline.
package cliout

import (
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

// BuildInfoJSON returns the flat JSON-ready map that `gaffer info --json`
// emits. Used by both the CLI command and the MCP get_projection_info
// tool so the two surfaces stay shape-identical.
//
// Returns map[string]any (not a typed struct) because the output is a
// conditional union: seven fields are present only when the projection
// declares them (categories, streams, events, partitioning,
// diagnostics, fixtures) and would each need a typed omitempty tag.
// A map keeps the conditional shape readable; readers that need a
// typed view can decode the result downstream.
func BuildInfoJSON(proj *engine.Projection, info gafferruntime.ProjectionInfo) map[string]any {
	src := engine.DescribeSource(info)
	out := map[string]any{
		"name":            proj.Def.Name,
		"entry":           proj.Def.Entry,
		"engineVersion":   proj.EngineVersion,
		"source":          src["type"],
		"biState":         info.BiState,
		"producesResults": info.ProducesResults,
		// Always emit dbVersion: null distinguishes unversioned (bugs on)
		// from a real version. Consumers need this signal explicitly.
		"dbVersion": NullableString(proj.DbVersion),
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
