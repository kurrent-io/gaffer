package cmd

import (
	"fmt"
	"strings"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
)

func (tw *textWriter) WriteInfo(proj *engine.Projection, info gafferruntime.ProjectionInfo) {
	tw.heading(proj.Def.Name)

	if info.AllStreams {
		tw.detail("Source", "$all")
	} else if len(info.Categories) > 0 {
		tw.detail("Source", "category "+strings.Join(info.Categories, ", "))
	} else if len(info.Streams) > 0 {
		tw.detail("Source", "streams "+strings.Join(info.Streams, ", "))
	}

	if info.ByStreams {
		tw.detail("Partitioning", "per stream")
	} else if info.ByCustomPartitions {
		tw.detail("Partitioning", "custom key")
	}

	if len(info.Events) > 0 {
		tw.detail("Events", strings.Join(info.Events, ", "))
	}

	if info.BiState {
		tw.detail("BiState", "yes")
	}
	if info.ProducesResults {
		tw.detail("Produces results", "yes")
	}
	if info.EmitsEvents {
		tw.detail("Emits events", "yes")
	}

	if proj.EngineVersion != 0 {
		tw.detail("Engine", fmt.Sprintf("v%d", proj.EngineVersion))
	}

	if proj.QuirksVersion != "" {
		tw.detail("Quirks", proj.QuirksVersion)
	} else {
		tw.detail("Quirks", "unversioned (matching all KurrentDB quirks)")
	}

	tw.blank()

	for _, d := range info.Diagnostics {
		tw.writeDiagnostic(d)
		if strings.HasPrefix(d.Code, "quirk.") {
			tw.compileQuirks = append(tw.compileQuirks, d.Code)
		}
	}
}
