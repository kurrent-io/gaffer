// Package cliout owns JSON shapes that span the CLI and MCP surfaces:
// `gaffer info --json` / get_projection_info, `gaffer manifest` /
// get_manifest, and the small helpers they share. Keeping a single home
// for these contracts prevents the surfaces from drifting.
package cliout

import (
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

// BuildInfoJSON returns the flat JSON-ready map that `gaffer info --json`
// emits. Used by both the CLI command and the MCP get_projection_info
// tool so the two surfaces stay shape-identical.
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
