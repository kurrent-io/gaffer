package cmd

import (
	"encoding/json"
	"os"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info [projection]",
	Short: "Show projection details",
	Args:  cobra.ExactArgs(1),
	RunE:  runInfo,
}

var infoJSON bool

func init() {
	infoCmd.Flags().BoolVar(&infoJSON, "json", false, "Output as JSON")
}

func runInfo(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	proj, err := engine.LoadProjection(args[0])
	if err != nil {
		return err
	}

	session, info, err := engine.CreateSession(proj, false)
	if err != nil {
		return handleSessionError(cmd, err)
	}
	defer session.Destroy()

	if infoJSON {
		return writeInfoJSON(proj, info)
	}

	tw := newTextWriter(os.Stdout)
	tw.WriteInfo(proj.Def.Name, info, proj.Engine)
	return nil
}

func writeInfoJSON(proj *engine.Projection, info gafferruntime.QuerySources) error {
	src := engine.DescribeSource(info)
	out := map[string]any{
		"name":            proj.Def.Name,
		"entry":           proj.Def.Entry,
		"engine":          proj.Engine,
		"source":          src["type"],
		"biState":         info.IsBiState,
		"producesResults": info.ProducesResults,
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

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
