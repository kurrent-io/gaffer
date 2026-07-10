package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
)

type statusOpts struct {
	Env        string
	Connection string
	JSON       bool
}

func newStatusCmd() *cobra.Command {
	var opts statusOpts
	cmd := &cobra.Command{
		Use:   "status [projection]",
		Short: "Show the state of projections on an environment",
		Long: "Show the runtime state of projections on a KurrentDB environment and how they\n" +
			"compare to local config.\n\n" +
			"With no argument, lists every local and deployed projection: running, stopped,\n" +
			"aborted, or faulted, progress, and whether each is in sync, drifted, not deployed,\n" +
			"untracked, or invalid (local definition doesn't compile or has a config error). Name\n" +
			"a projection for its detail. Pass --json for machine output.\n\n" +
			"An aborted projection was stopped without a final checkpoint, so resuming it\n" +
			"reprocesses from the last checkpoint written (re-emitting, for an emitting\n" +
			"projection). The server reports this only while it holds the projection in memory,\n" +
			"so it reverts to stopped after a restart. The absence of aborted is not proof of a\n" +
			"clean pause.\n\n" +
			"When a projection carries deploy metadata, status shows when and via which tool\n" +
			"it was last deployed. A projection on the server but not in gaffer.toml shows as\n" +
			"an orphan when gaffer deployed it (a deletion candidate), otherwise as plain\n" +
			"untracked with its deploying tool named. A drifted projection is marked local\n" +
			"ahead (you edited local since deploying) or changed externally (a tool or a\n" +
			"direct write changed the server since). Naming a projection adds the deployer\n" +
			"and source revision.\n\n" +
			"The last-deploy date comes from the event itself, so it shows even without\n" +
			"metadata, where it is the time of the last write. Against a server without the\n" +
			"metadata, status falls back to plain untracked or drifted.\n\n" +
			"When gaffer.toml declares a [database_config], status also checks the target\n" +
			"node's live engine settings and warns on a divergence, since the fixtures and\n" +
			"local runs assumed the declared values. Advisory only: when the node's options\n" +
			"can't be read (no HTTP surface, auth refusal), status warns that the check\n" +
			"couldn't run instead of failing or reporting a false \"in sync\".",
		Example: "  gaffer status\n" +
			"  gaffer status order-count --env staging",
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runStatus(cmd, name, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment from gaffer.toml")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "KurrentDB connection string (overrides --env)")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as JSON")
	return cmd
}

func runStatus(cmd *cobra.Command, name string, opts statusOpts) error {
	conn, err := connectEnv(opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer conn.cleanup()
	cfg, root, r := conn.cfg, conn.root, conn.r

	// remote calls block until their context deadline if the projections
	// subsystem doesn't respond, so bound them rather than hang the command.
	ctx, cancel := context.WithTimeout(cmd.Context(), projectionRPCTimeout)
	defer cancel()

	// The [database_config] drift check runs in the background so its HTTP
	// round-trip overlaps the status RPCs; drained before rendering.
	driftCh := drift.StartConfigDriftCheck(cmd.Context(), cfg, root, conn.env)

	// For --json, resolve the target identity concurrently with the status reads
	// (like the drift check above), so the self-describing envelope adds no wall
	// time over the status RPCs and its own bounded $server-info read isn't
	// squeezed by whatever budget the projection reads left. An unreadable
	// server-info falls back to the env name and its opt-in.
	type targetInfo struct {
		name string
		prod bool
	}
	var targetCh chan targetInfo
	if opts.JSON {
		targetCh = make(chan targetInfo, 1)
		go func() {
			name, prod := r.OperateTarget(cmd.Context(), conn.env, projectionRPCTimeout)
			targetCh <- targetInfo{name, prod}
		}()
	}

	// statusJSON emits the machine report, naming the env, the server, and its
	// production tier - the same self-describing shape the MCP deploy_status tool
	// returns.
	statusJSON := func(entries []drift.StatusEntry) error {
		ti := <-targetCh
		return renderStatusJSON(cmd.OutOrStdout(), entries, <-driftCh, conn.env.Name, ti.name, &ti.prod)
	}

	if name != "" {
		entry, err := drift.StatusOne(ctx, r, cfg, root, name)
		if err != nil {
			return err
		}
		if opts.JSON {
			return statusJSON([]drift.StatusEntry{entry})
		}
		newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr()).WriteStatus(entry)
		writeConfigDriftWarnings(cmd.ErrOrStderr(), <-driftCh)
		return nil
	}

	entries, err := drift.StatusAll(ctx, r, cfg, root)
	if err != nil {
		return err
	}
	if opts.JSON {
		return statusJSON(entries)
	}
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No projections to show.")
		writeConfigDriftWarnings(cmd.ErrOrStderr(), <-driftCh)
		return nil
	}
	newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr()).WriteStatusTable(entries)
	writeConfigDriftWarnings(cmd.ErrOrStderr(), <-driftCh)
	return nil
}

func renderStatusJSON(w io.Writer, entries []drift.StatusEntry, dr drift.ConfigDriftResult, env, target string, production *bool) error {
	report := cliout.BuildStatusReport(entries, dr)
	report.Env, report.Target, report.Production = env, target, production
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
