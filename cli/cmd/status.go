package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

type statusOpts struct {
	Env        string
	Connection string
	JSON       bool
}

// statusEntry is one projection's runtime state plus how it compares to local.
// runtime is nil when the projection isn't deployed.
type statusEntry struct {
	comparison
	runtime *remote.Status
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
			"projection for its detail. Pass --json for machine output.",
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
	cfg, root, r, cleanup, err := connectEnv(opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer cleanup()

	// remote calls block until their context deadline if the projections
	// subsystem doesn't respond, so bound them rather than hang the command.
	ctx, cancel := context.WithTimeout(cmd.Context(), projectionRPCTimeout)
	defer cancel()

	if name != "" {
		entry, err := statusOne(ctx, r, cfg, root, name)
		if err != nil {
			return err
		}
		if opts.JSON {
			return renderStatusJSON(cmd.OutOrStdout(), []statusEntry{entry})
		}
		newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr()).WriteStatus(entry)
		return nil
	}

	entries, err := collectStatus(ctx, r, cfg, root)
	if err != nil {
		return err
	}
	if opts.JSON {
		return renderStatusJSON(cmd.OutOrStdout(), entries)
	}
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No projections to show.")
		return nil
	}
	newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr()).WriteStatusTable(entries)
	return nil
}

// statusOne resolves a single projection's drift and (if deployed) runtime state.
func statusOne(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string) (statusEntry, error) {
	cmp, err := compareProjection(ctx, r, cfg, root, name)
	if err != nil {
		return statusEntry{}, err
	}
	e := statusEntry{comparison: cmp}
	// Fetch runtime state only when the projection exists on the server. Gating on
	// Deployed (not the drift state) covers driftInvalid: a projection that won't
	// compile and isn't deployed has no runtime to ask for.
	if cmp.Deployed != nil {
		st, err := r.Status(ctx, name)
		if err != nil && !errors.Is(err, remote.ErrNotFound) {
			return statusEntry{}, err
		}
		e.runtime = st
	}
	return e, nil
}

// collectStatus reconciles every local and deployed projection into status
// entries: tracked (runtime + drift), not-deployed (local only), and untracked
// (deployed, not in config).
func collectStatus(ctx context.Context, r *remote.Client, cfg *config.Config, root string) ([]statusEntry, error) {
	deployed, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]remote.Status, len(deployed))
	for i := range deployed {
		byName[deployed[i].Name] = deployed[i]
	}

	local := make(map[string]bool, len(cfg.Projection))
	var entries []statusEntry
	for i := range cfg.Projection {
		name := cfg.Projection[i].Name
		local[name] = true
		cmp, err := compareProjection(ctx, r, cfg, root, name)
		if err != nil {
			return nil, err
		}
		e := statusEntry{comparison: cmp}
		if rt, ok := byName[name]; ok {
			e.runtime = &rt
		}
		entries = append(entries, e)
	}

	var untracked []string
	for i := range deployed {
		if !local[deployed[i].Name] {
			untracked = append(untracked, deployed[i].Name)
		}
	}
	slices.Sort(untracked)
	for _, n := range untracked {
		rt := byName[n]
		entries = append(entries, statusEntry{
			comparison: comparison{Name: n, State: driftUntracked},
			runtime:    &rt,
		})
	}
	return entries, nil
}

type statusJSON struct {
	Name    string             `json:"name"`
	Drift   string             `json:"drift"`
	Runtime *statusRuntimeJSON `json:"runtime,omitempty"`
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

func renderStatusJSON(w io.Writer, entries []statusEntry) error {
	out := make([]statusJSON, 0, len(entries))
	for _, e := range entries {
		j := statusJSON{Name: e.Name, Drift: string(e.State)}
		if e.State == driftInvalid && e.LocalErr != nil {
			j.Error = e.LocalErr.Error()
		}
		if e.runtime != nil {
			j.Runtime = &statusRuntimeJSON{
				State:       string(e.runtime.State),
				Progress:    e.runtime.Progress,
				Position:    e.runtime.Position,
				FaultReason: e.runtime.FaultReason,
			}
		}
		out = append(out, j)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
