package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

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
			"With no argument, lists every local and deployed projection: running, stopped or\n" +
			"faulted, progress, and whether each is in sync, drifted, not deployed, untracked,\n" +
			"or invalid (local definition doesn't compile or has a config error). Name a\n" +
			"projection for its detail. Pass --json for machine output.\n\n" +
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
			"local runs assumed the declared values. Advisory only: a server that doesn't\n" +
			"expose its options (or refuses the read) skips the check silently.",
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
	resolved, _ := resolveLiveEnv(opts.Connection, opts.Env, cfg)
	driftCh := drift.StartConfigDriftCheck(cmd.Context(), cfg, root, resolved.Name, resolved.Connection)

	if name != "" {
		entry, err := drift.StatusOne(ctx, r, cfg, root, name)
		if err != nil {
			return err
		}
		if opts.JSON {
			return renderStatusJSON(cmd.OutOrStdout(), []drift.StatusEntry{entry}, <-driftCh)
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
		return renderStatusJSON(cmd.OutOrStdout(), entries, <-driftCh)
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

type statusJSON struct {
	Name         string             `json:"name"`
	Drift        string             `json:"drift"`
	Owner        string             `json:"owner"`
	Attribution  string             `json:"attribution,omitempty"`
	LastDeployed string             `json:"lastDeployed,omitempty"`
	LastWrite    *ledgerJSON        `json:"lastWrite,omitempty"`
	Runtime      *statusRuntimeJSON `json:"runtime,omitempty"`
	// Error is the compile error, present only when drift is invalid, so a
	// machine consumer sees why a projection is invalid, not just that it is.
	Error string `json:"error,omitempty"`
}

type statusRuntimeJSON struct {
	State       string  `json:"state"`
	Progress    float32 `json:"progress"`
	Position    string  `json:"position,omitempty"`
	FaultReason string  `json:"faultReason,omitempty"`
}

// statusReportJSON is the status --json envelope: the per-projection entries,
// plus the env-level [database_config] drift so a machine consumer (the VS
// Code extension's status surface) sees the target's engine configuration
// diverging without a second call. configDrift is omitted when clean, not
// declared, or unreadable - absence is "nothing to report", not "in sync".
type statusReportJSON struct {
	Projections []statusJSON      `json:"projections"`
	ConfigDrift []configDriftJSON `json:"configDrift,omitempty"`
}

func renderStatusJSON(w io.Writer, entries []drift.StatusEntry, items []drift.ConfigDrift) error {
	out := make([]statusJSON, 0, len(entries))
	for _, e := range entries {
		j := statusJSON{Name: e.Name, Drift: string(e.State), Owner: string(e.Owner()), Attribution: string(e.Attribution()), LastDeployed: lastDeployedJSON(e.Comparison), LastWrite: lastWrite(e.Comparison)}
		if e.State == drift.Invalid && e.LocalErr != nil {
			j.Error = e.LocalErr.Error()
		}
		if e.Runtime != nil {
			j.Runtime = &statusRuntimeJSON{
				State:       string(e.Runtime.State),
				Progress:    e.Runtime.Progress,
				Position:    e.Runtime.Position,
				FaultReason: e.Runtime.FaultReason,
			}
		}
		out = append(out, j)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	report := statusReportJSON{Projections: out}
	if len(items) > 0 {
		report.ConfigDrift = configDriftToJSON(items)
	}
	return enc.Encode(report)
}
