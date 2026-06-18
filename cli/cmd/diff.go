package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

type diffOpts struct {
	Env        string
	Connection string
	JSON       bool
}

// diffState is how a projection compares between local config and the server.
type diffState string

const (
	stateInSync      diffState = "in-sync"
	stateDrifted     diffState = "drifted"
	stateNotDeployed diffState = "not-deployed" // in local config, absent on the server
	stateUntracked   diffState = "untracked"    // on the server, not in local config
)

// diffEntry is the comparison result for one projection. Local/Deployed are set
// when that side exists; Cmp is meaningful only when Drifted.
type diffEntry struct {
	Name     string
	State    diffState
	Cmp      deploy.Comparison
	Local    *deploy.Descriptor
	Deployed *deploy.Descriptor
}

func newDiffCmd() *cobra.Command {
	var opts diffOpts
	cmd := &cobra.Command{
		Use:   "diff [projection]",
		Short: "Show how local projections differ from what's deployed",
		Example: "  gaffer diff\n" +
			"  gaffer diff order-count --env staging",
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			return runDiff(cmd, name, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment from gaffer.toml to compare against")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "KurrentDB connection string (overrides --env)")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as JSON")
	return cmd
}

func runDiff(cmd *cobra.Command, name string, opts diffOpts) error {
	root := project.FindRoot()
	if root == "" {
		return project.ErrNotInProject
	}
	cfg, err := config.Load(project.ConfigPath(root))
	if err != nil {
		return err
	}

	env, err := resolveLiveEnv(opts.Connection, opts.Env, cfg)
	if err != nil {
		return err
	}
	if env.Connection == "" {
		return errors.New("no environment to compare against: mark a default [env.<name>], pass --env, or pass --connection")
	}

	client, _, err := engine.Connect(env.Connection, root, env.Name, env.OAuth, env.Cert)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	entries, err := computeDiff(cmd.Context(), remote.New(client), cfg, root, name)
	if err != nil {
		return err
	}

	if opts.JSON {
		return renderDiffJSON(cmd.OutOrStdout(), entries)
	}
	renderDiffText(cmd.OutOrStdout(), entries)
	for _, e := range entries {
		if e.State == stateDrifted && e.Cmp.QueryDiffers {
			if err := openSourceDiff(e.Name, e.Deployed.CanonicalQuery(), e.Local.CanonicalQuery(), cmd.OutOrStdout(), cmd.ErrOrStderr()); err != nil {
				return err
			}
		}
	}
	return nil
}

// computeDiff compares one named projection, or every local projection plus any
// untracked ones deployed on the server.
func computeDiff(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string) ([]diffEntry, error) {
	if name != "" {
		entry, err := compareProjection(ctx, r, cfg, root, name)
		if err != nil {
			return nil, err
		}
		return []diffEntry{entry}, nil
	}

	local := map[string]bool{}
	var entries []diffEntry
	for i := range cfg.Projection {
		pname := cfg.Projection[i].Name
		local[pname] = true
		entry, err := compareProjection(ctx, r, cfg, root, pname)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	deployed, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	var untracked []string
	for _, s := range deployed {
		if !local[s.Name] {
			untracked = append(untracked, s.Name)
		}
	}
	sort.Strings(untracked)
	for _, n := range untracked {
		entries = append(entries, diffEntry{Name: n, State: stateUntracked})
	}
	return entries, nil
}

func compareProjection(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string) (diffEntry, error) {
	def := cfg.FindProjection(name)
	if def == nil {
		return diffEntry{}, fmt.Errorf("projection %q not found in gaffer.toml", name)
	}
	source, err := engine.ReadSource(root, def.Entry)
	if err != nil {
		return diffEntry{}, err
	}
	local, err := engine.LocalDescriptor(engine.NewProjection(root, cfg, def, source))
	if err != nil {
		return diffEntry{}, err
	}

	deployedDef, err := r.Read(ctx, name)
	if errors.Is(err, remote.ErrNotFound) {
		return diffEntry{Name: name, State: stateNotDeployed, Local: &local}, nil
	}
	if err != nil {
		return diffEntry{}, err
	}

	deployed := deployedDef.Descriptor()
	cmp := deploy.Compare(local, deployed)
	state := stateInSync
	if !cmp.InSync() {
		state = stateDrifted
	}
	return diffEntry{Name: name, State: state, Cmp: cmp, Local: &local, Deployed: &deployed}, nil
}

func renderDiffText(w io.Writer, entries []diffEntry) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	for _, e := range entries {
		switch e.State {
		case stateInSync:
			p("%s: in sync\n", e.Name)
		case stateNotDeployed:
			p("%s: not deployed\n", e.Name)
		case stateUntracked:
			p("%s: untracked (deployed, not in gaffer.toml)\n", e.Name)
		case stateDrifted:
			p("%s: drifted (%s)\n", e.Name, strings.Join(driftReasons(e), ", "))
		}
	}
}

// driftReasons describes each changed dimension as deployed -> local.
func driftReasons(e diffEntry) []string {
	var rs []string
	if e.Cmp.QueryDiffers {
		rs = append(rs, "query")
	}
	if e.Cmp.EngineVersionDiffers {
		rs = append(rs, fmt.Sprintf("engine version %d -> %d", e.Deployed.EngineVersion, e.Local.EngineVersion))
	}
	if e.Cmp.EmitDiffers {
		rs = append(rs, fmt.Sprintf("emit %t -> %t", e.Deployed.Emit, e.Local.Emit))
	}
	if e.Cmp.TrackEmittedStreamsDiffers {
		rs = append(rs, fmt.Sprintf("track emitted streams %t -> %t", e.Deployed.TrackEmittedStreams, e.Local.TrackEmittedStreams))
	}
	return rs
}

type diffJSON struct {
	Name         string     `json:"name"`
	State        string     `json:"state"`
	LocalHash    string     `json:"localHash,omitempty"`
	DeployedHash string     `json:"deployedHash,omitempty"`
	Drift        *driftJSON `json:"drift,omitempty"`
}

type driftJSON struct {
	Query               bool `json:"query"`
	EngineVersion       bool `json:"engineVersion"`
	Emit                bool `json:"emit"`
	TrackEmittedStreams bool `json:"trackEmittedStreams"`
}

func renderDiffJSON(w io.Writer, entries []diffEntry) error {
	out := make([]diffJSON, 0, len(entries))
	for _, e := range entries {
		j := diffJSON{Name: e.Name, State: string(e.State)}
		if e.Local != nil {
			j.LocalHash = e.Local.Hash()
		}
		if e.Deployed != nil {
			j.DeployedHash = e.Deployed.Hash()
		}
		if e.State == stateDrifted {
			j.Drift = &driftJSON{
				Query:               e.Cmp.QueryDiffers,
				EngineVersion:       e.Cmp.EngineVersionDiffers,
				Emit:                e.Cmp.EmitDiffers,
				TrackEmittedStreams: e.Cmp.TrackEmittedStreamsDiffers,
			}
		}
		out = append(out, j)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
