package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// diffState is how one projection compares between local config and the server.
// The overview across all projections (and bulk untracked detection) is gaffer
// status; diff is the source-level comparison of a single projection.
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
		Use:   "diff <projection>",
		Short: "Show how a projection differs from what's deployed",
		Long: "Compare a projection's local definition against what's deployed on KurrentDB.\n\n" +
			"Reports one of four states: in sync, drifted, not deployed (local only), or " +
			"untracked (on the server but absent from gaffer.toml). When the query differs, " +
			"the source is shown in an external diff viewer (git diff --no-index by default; " +
			"set GAFFER_EXTERNAL_DIFF to override). Pass --json for machine-readable output.",
		Example: "  gaffer diff order-count\n" +
			"  gaffer diff order-count --env staging",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, args[0], opts)
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

	entry, err := compareProjection(cmd.Context(), remote.New(client), cfg, root, name)
	if err != nil {
		return err
	}

	if opts.JSON {
		return renderDiffJSON(cmd.OutOrStdout(), entry)
	}
	renderDiffText(cmd.OutOrStdout(), entry)
	if entry.State == stateDrifted && entry.Cmp.QueryDiffers {
		return openSourceDiff(entry.Name, entry.Deployed.CanonicalQuery(), entry.Local.CanonicalQuery(), cmd.OutOrStdout(), cmd.ErrOrStderr())
	}
	return nil
}

// compareProjection compares one projection's local definition against what's
// deployed. A name absent from local config but present on the server is
// untracked; absent from both is an error.
func compareProjection(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string) (diffEntry, error) {
	def := cfg.FindProjection(name)
	if def == nil {
		deployedDef, err := r.Read(ctx, name)
		if errors.Is(err, remote.ErrNotFound) {
			return diffEntry{}, fmt.Errorf("projection %q is not in gaffer.toml or deployed on the server", name)
		}
		if err != nil {
			return diffEntry{}, err
		}
		deployed := deployedDef.Descriptor()
		return diffEntry{Name: name, State: stateUntracked, Deployed: &deployed}, nil
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

func renderDiffText(w io.Writer, e diffEntry) {
	switch e.State {
	case stateInSync:
		_, _ = fmt.Fprintf(w, "%s: in sync\n", e.Name)
	case stateNotDeployed:
		_, _ = fmt.Fprintf(w, "%s: not deployed (local only)\n", e.Name)
	case stateUntracked:
		_, _ = fmt.Fprintf(w, "%s: untracked (deployed, not in gaffer.toml)\n", e.Name)
	case stateDrifted:
		_, _ = fmt.Fprintf(w, "%s: drifted (%s)\n", e.Name, strings.Join(driftReasons(e), ", "))
	}
}

// driftReasons names each changed dimension, labelling values remote (deployed)
// vs local so the direction is unambiguous. The query change itself is shown by
// the source viewer.
func driftReasons(e diffEntry) []string {
	var rs []string
	if e.Cmp.QueryDiffers {
		rs = append(rs, "query")
	}
	if e.Cmp.EngineVersionDiffers {
		rs = append(rs, fmt.Sprintf("engine version: remote=%d local=%d", e.Deployed.EngineVersion, e.Local.EngineVersion))
	}
	if e.Cmp.EmitDiffers {
		rs = append(rs, fmt.Sprintf("emit: remote=%t local=%t", e.Deployed.Emit, e.Local.Emit))
	}
	if e.Cmp.TrackEmittedStreamsDiffers {
		rs = append(rs, fmt.Sprintf("track emitted streams: remote=%t local=%t", e.Deployed.TrackEmittedStreams, e.Local.TrackEmittedStreams))
	}
	return rs
}

// diffJSON is the --json shape for one projection. State is one of in-sync,
// drifted, not-deployed, untracked.
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

func renderDiffJSON(w io.Writer, e diffEntry) error {
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
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(j)
}
