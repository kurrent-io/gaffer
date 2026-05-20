package engine

import (
	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
)

// BuildInfoJSON returns the flat JSON-ready map that `gaffer info --json`
// emits. Shared between the CLI command and the MCP `info` tool so both
// surfaces stay shape-identical.
func BuildInfoJSON(proj *Projection, info gafferruntime.ProjectionInfo) map[string]any {
	src := DescribeSource(info)
	out := map[string]any{
		"name":            proj.Def.Name,
		"entry":           proj.Def.Entry,
		"engineVersion":   proj.EngineVersion,
		"source":          src["type"],
		"biState":         info.BiState,
		"producesResults": info.ProducesResults,
		// Always emit dbVersion: null distinguishes unversioned (bugs on)
		// from a real version. Consumers need this signal explicitly.
		"dbVersion": nullable(proj.DbVersion),
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
	if p := DescribePartitioning(info); p != "none" {
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

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
