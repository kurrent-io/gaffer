package cmd

import (
	"encoding/json"
	"os"

	gafferruntime "github.com/kurrent-io/gaffer/bindings/go"
	"github.com/kurrent-io/gaffer/cli/internal/projection"
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

	ctx, err := loadProjection(args[0])
	if err != nil {
		return err
	}

	session, err := gafferruntime.NewSession(ctx.Source, projection.BuildSessionOptions(ctx.Config, ctx.Proj, false))
	if err != nil {
		return handleSessionError(cmd, err)
	}
	defer session.Destroy()

	info := projection.GetInfo(session)

	if infoJSON {
		return writeInfoJSON(ctx, info)
	}

	tw := newTextWriter(os.Stdout)
	tw.WriteInfo(ctx.Proj.Name, info, ctx.Engine)
	return nil
}

func writeInfoJSON(ctx *projectionContext, info projection.Info) error {
	out := map[string]any{
		"name":            ctx.Proj.Name,
		"entry":           ctx.Proj.Entry,
		"engine":          ctx.Engine,
		"source":          infoSource(info),
		"biState":         info.IsBiState,
		"producesResults": info.ProducesResults,
	}
	if len(info.Categories) > 0 {
		out["categories"] = info.Categories
	}
	if len(info.Streams) > 0 {
		out["streams"] = info.Streams
	}
	if len(info.Events) > 0 {
		out["events"] = info.Events
	}
	if info.ByStreams {
		out["partitioning"] = "per-stream"
	} else if info.ByCustomPartitions {
		out["partitioning"] = "custom"
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
