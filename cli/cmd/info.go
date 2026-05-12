package cmd

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

func newInfoCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "info [projection]",
		Short: "Show projection details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer func() {
				telemetry.EmitInfo(cmd.Context(), telemetry.InfoCommandInvokedProperties{
					Outcome: outcomeFor(retErr),
				})
			}()
			return runInfo(cmd, args[0], asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func runInfo(cmd *cobra.Command, name string, asJSON bool) error {
	proj, err := engine.LoadProjection(name)
	if err != nil {
		return err
	}

	session, info, err := engine.CreateSession(proj, false)
	if err != nil {
		return handleSessionError(cmd, err)
	}
	defer session.Destroy()

	if asJSON {
		return writeInfoJSON(proj, info)
	}

	tw := newTextWriter(os.Stdout, os.Stderr)
	tw.WriteInfo(proj.Def.Name, info, proj.EngineVersion, proj.DbVersion)
	return nil
}

func writeInfoJSON(proj *engine.Projection, info gafferruntime.ProjectionInfo) error {
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
		"dbVersion": nullableString(proj.DbVersion),
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

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
