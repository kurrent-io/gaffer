package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/cliout"
	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/drift"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

type diffOpts struct {
	Env        string
	Connection string
	Left       string
	Right      string
	JSON       bool
}

func newDiffCmd() *cobra.Command {
	var opts diffOpts
	cmd := &cobra.Command{
		Use:   "diff <projection>",
		Short: "Compare two versions of a projection",
		Long: "By default gaffer diff compares the local definition against what's deployed " +
			"on KurrentDB; --left and --right pick any two versions to compare instead.\n\n" +
			"Each side is one of: local (the definition in gaffer.toml), deployed (what's " +
			"live now), or a content-hash prefix (a past version from the projection's " +
			"history; resolving a hash costs a history read). The default is " +
			"--left deployed --right local.\n\n" +
			"The default deployed-vs-local diff reports one of five states: in sync, drifted, " +
			"not deployed (local only), untracked (on the server but absent from gaffer.toml), " +
			"or invalid. Invalid means the local definition can't be used - it doesn't compile, " +
			"or has a config error such as track_emitted_streams on engine version 2; the source " +
			"and config still diff where possible, but emit is unknown. When deploy metadata is " +
			"present, a drifted projection is attributed as local ahead (you edited local since " +
			"deploying) or changed externally (a tool or a direct write changed the server since). " +
			"An untracked projection is shown as an orphan when gaffer deployed it, otherwise as " +
			"plain untracked. A version-to-version diff (any --left/--right other than the default) " +
			"is a pure source diff with no verdict.\n\n" +
			"When the query differs, the source diff is rendered inline: every line of both sides " +
			"with the changes marked, and the span that changed within a line highlighted. Set " +
			"GAFFER_EXTERNAL_DIFF to open an external viewer instead (e.g. git diff, delta, difft).\n\n" +
			"Pass --json for machine-readable output: the two sides (ref, hash, canonical source), " +
			"the structured line diff, and (for the default deployed-vs-local diff) the drift " +
			"verdict, owner, and provenance.",
		Example: "  gaffer diff order-count\n" +
			"  gaffer diff order-count --env staging\n" +
			"  gaffer diff order-count --left 9f2a1c --right local",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(cmd, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment from gaffer.toml to compare against")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "KurrentDB connection string (overrides --env)")
	cmd.Flags().StringVar(&opts.Left, "left", "deployed", "Left (base) side: local, deployed, or a content-hash prefix")
	cmd.Flags().StringVar(&opts.Right, "right", "local", "Right (compared) side: local, deployed, or a content-hash prefix")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as JSON")
	return cmd
}

// refKind is which version a --left/--right value names.
type refKind int

const (
	refDeployed refKind = iota
	refLocal
	refHash
)

// diffRef is a parsed --left/--right value. hash is the normalised prefix, set
// only for refHash.
type diffRef struct {
	kind refKind
	hash string
}

func parseDiffRef(s string) (diffRef, error) {
	switch s {
	case "deployed":
		return diffRef{kind: refDeployed}, nil
	case "local":
		return diffRef{kind: refLocal}, nil
	default:
		p, err := remote.NormalizeHashPrefix(s)
		if err != nil {
			return diffRef{}, fmt.Errorf("invalid ref %q: use 'local', 'deployed', or a content-hash prefix (%w)", s, err)
		}
		return diffRef{kind: refHash, hash: p}, nil
	}
}

func runDiff(cmd *cobra.Command, name string, opts diffOpts) error {
	left, err := parseDiffRef(opts.Left)
	if err != nil {
		return err
	}
	right, err := parseDiffRef(opts.Right)
	if err != nil {
		return err
	}

	conn, err := connectEnv(opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer conn.cleanup()
	cfg, root, r := conn.cfg, conn.root, conn.r

	// remote calls block until their context deadline if the projections
	// subsystem doesn't respond, so bound the read rather than hang the command.
	ctx, cancel := context.WithTimeout(cmd.Context(), projectionRPCTimeout)
	defer cancel()

	// The default deployed↔local diff carries the full drift verdict (owner,
	// attribution, provenance) and the one-sided/invalid rendering; every other
	// combination is a pure source diff between two versions.
	if left.kind == refDeployed && right.kind == refLocal {
		return runComparisonDiff(ctx, cmd, r, cfg, root, name, opts.JSON)
	}
	return runVersionDiff(ctx, cmd, r, cfg, root, name, left, right, opts.JSON)
}

// runComparisonDiff is the default deployed↔local diff: the drift comparison, its
// verdict, and the inline (or external) source diff - today's gaffer diff.
func runComparisonDiff(ctx context.Context, cmd *cobra.Command, r *remote.Client, cfg *config.Config, root, name string, asJSON bool) error {
	entry, err := drift.Compare(ctx, r, cfg, root, name)
	if err != nil {
		return err
	}

	if asJSON {
		return renderDiffJSON(cmd.OutOrStdout(), entry)
	}
	tw := newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	tw.WriteDiff(entry)
	// The query is read from source, not compiled, so the source diff is still
	// worth showing when the local projection is invalid (its whole point is
	// comparing source to what's deployed). Both sides must exist and differ.
	if entry.Cmp.QueryDiffers && entry.Deployed != nil && entry.Local != nil && (entry.State == drift.Drifted || entry.State == drift.Invalid) {
		if argv, ok := externalDiffCommand(os.Getenv); ok {
			return openSourceDiff(argv, entry.Name, "deployed", entry.Deployed.CanonicalQuery(), "local", entry.Local.CanonicalQuery(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		}
		tw.blank()
		tw.WriteQueryDiff(deploy.LineDiff(entry.Deployed.Query, entry.Local.Query))
	}
	return nil
}

// runVersionDiff is a pure source diff between two arbitrary versions - no drift
// verdict (that's meaningful only for deployed↔local). Both sides must resolve;
// a missing side is an error rather than a one-sided diff.
func runVersionDiff(ctx context.Context, cmd *cobra.Command, r *remote.Client, cfg *config.Config, root, name string, left, right diffRef, asJSON bool) error {
	ls, err := resolveDiffSide(ctx, r, cfg, root, name, left)
	if err != nil {
		return err
	}
	rs, err := resolveDiffSide(ctx, r, cfg, root, name, right)
	if err != nil {
		return err
	}
	lines := deploy.LineDiff(ls.json.Source, rs.json.Source)

	if asJSON {
		return encodeDiffJSON(cmd.OutOrStdout(), diffJSON{Name: name, Left: ls.json, Right: rs.json, Lines: lines})
	}
	tw := newTextWriter(cmd.OutOrStdout(), cmd.ErrOrStderr())
	tw.heading(name)
	tw.status(tw.styles.muted.Render(ls.label + " → " + rs.label))
	if argv, ok := externalDiffCommand(os.Getenv); ok {
		return openSourceDiff(argv, name, ls.label, ls.json.Source, rs.label, rs.json.Source, cmd.OutOrStdout(), cmd.ErrOrStderr())
	}
	tw.blank()
	tw.WriteQueryDiff(lines)
	return nil
}

// resolvedSide is one operand resolved to its JSON shape plus a short label for
// text output and external-diff filenames.
type resolvedSide struct {
	json  diffSideJSON
	label string
}

// resolveDiffSide reads one side of a version diff. deployed and a hash come from
// the server; local is built from gaffer.toml. A local definition that doesn't
// compile still yields its source (from the partial descriptor) but no hash -
// emit is unknown, so the hash would be misleading.
func resolveDiffSide(ctx context.Context, r *remote.Client, cfg *config.Config, root, name string, ref diffRef) (resolvedSide, error) {
	switch ref.kind {
	case refDeployed:
		def, err := r.Read(ctx, name)
		if errors.Is(err, remote.ErrNotFound) {
			return resolvedSide{}, fmt.Errorf("%q is not deployed", name)
		}
		if err != nil {
			return resolvedSide{}, err
		}
		d := def.Descriptor()
		return resolvedSide{json: diffSideJSON{Ref: "deployed", Hash: d.Hash(), Source: d.CanonicalQuery()}, label: "deployed"}, nil
	case refHash:
		m, err := r.FindVersionByHash(ctx, name, ref.hash)
		if err != nil {
			return resolvedSide{}, err
		}
		d := m.Def.Descriptor()
		return resolvedSide{json: diffSideJSON{Ref: "version", Hash: m.Hash, Source: d.CanonicalQuery()}, label: shortRef(m.Hash)}, nil
	default: // refLocal
		d, hashKnown, err := localDiffDescriptor(cfg, root, name)
		if err != nil {
			return resolvedSide{}, err
		}
		side := diffSideJSON{Ref: "local", Source: d.CanonicalQuery()}
		if hashKnown {
			side.Hash = d.Hash()
		}
		return resolvedSide{json: side, label: "local"}, nil
	}
}

// localDiffDescriptor builds the local side's descriptor from gaffer.toml,
// reporting whether the hash is trustworthy. A compile failure leaves the hash
// untrustworthy (emit unknown) but still returns the source to diff.
func localDiffDescriptor(cfg *config.Config, root, name string) (deploy.Descriptor, bool, error) {
	def := cfg.FindProjection(name)
	if def == nil {
		return deploy.Descriptor{}, false, fmt.Errorf("%q is not in gaffer.toml", name)
	}
	if cfgErr := cfg.ProjectionConfigError(name); cfgErr != nil {
		return deploy.Descriptor{}, false, cfgErr
	}
	source, err := engine.ReadSource(root, def.Entry)
	if err != nil {
		return deploy.Descriptor{}, false, err
	}
	proj := engine.NewProjection(root, cfg, def, source)
	d, err := engine.LocalDescriptor(proj)
	if err != nil {
		return engine.PartialDescriptor(proj), false, nil
	}
	return d, true, nil
}

// shortRef abbreviates a content hash for a human label (git-style leading 7).
func shortRef(hash string) string {
	if len(hash) >= 7 {
		return hash[:7]
	}
	return hash
}

// diffJSON is the --json shape. left/right name each operand (ref, content hash,
// canonical source); lines is the structured, colourable line diff. verdict and
// changes are the drift verdict and per-dimension flags, present only for the
// default deployed↔local diff - a version-to-version diff is a pure source diff.
type diffJSON struct {
	Name    string            `json:"name"`
	Left    diffSideJSON      `json:"left"`
	Right   diffSideJSON      `json:"right"`
	Verdict *diffVerdictJSON  `json:"verdict,omitempty"`
	Changes *changesJSON      `json:"changes,omitempty"`
	Lines   []deploy.DiffLine `json:"lines"`
}

// diffSideJSON identifies one operand: which side (local / deployed / a specific
// historical version), its content hash, and the canonical source the lines were
// computed from. Hash is omitted when it can't be derived - an invalid local
// definition yields no emit, so it has no hash.
type diffSideJSON struct {
	Ref    string `json:"ref"`
	Hash   string `json:"hash,omitempty"`
	Source string `json:"source"`
}

// diffVerdictJSON is the drift verdict, mirroring gaffer status: drift, owner,
// attribution, and provenance. reason carries the compile error when invalid.
type diffVerdictJSON struct {
	Drift        string             `json:"drift"`
	Owner        string             `json:"owner"`
	Attribution  string             `json:"attribution,omitempty"`
	LastDeployed string             `json:"lastDeployed,omitempty"`
	LastWrite    *cliout.LedgerJSON `json:"lastWrite,omitempty"`
	Reason       string             `json:"reason,omitempty"`
}

type changesJSON struct {
	Query               bool `json:"query"`
	EngineVersion       bool `json:"engineVersion"`
	Emit                bool `json:"emit"`
	TrackEmittedStreams bool `json:"trackEmittedStreams"`
}

// renderDiffJSON emits the default deployed↔local diff: the two sides, the drift
// verdict, and the structured line diff.
func renderDiffJSON(w io.Writer, e drift.Comparison) error {
	return encodeDiffJSON(w, comparisonDiffJSON(e))
}

func comparisonDiffJSON(e drift.Comparison) diffJSON {
	var deployedQuery, localQuery string
	left := diffSideJSON{Ref: "deployed"}
	right := diffSideJSON{Ref: "local"}
	if e.Deployed != nil {
		left.Hash = e.Deployed.Hash()
		left.Source = e.Deployed.CanonicalQuery()
		deployedQuery = e.Deployed.Query
	}
	if e.Local != nil {
		right.Source = e.Local.CanonicalQuery()
		localQuery = e.Local.Query
		// A local hash needs emit, which an invalid (uncompilable) projection
		// can't provide, so omit it; the verdict reports the compile error instead.
		if e.State != drift.Invalid {
			right.Hash = e.Local.Hash()
		}
	}

	j := diffJSON{
		Name:  e.Name,
		Left:  left,
		Right: right,
		Lines: deploy.LineDiff(deployedQuery, localQuery),
		Verdict: &diffVerdictJSON{
			Drift:        string(e.State),
			Owner:        string(e.Owner()),
			Attribution:  string(e.Attribution()),
			LastDeployed: cliout.LastDeployedJSON(e),
			LastWrite:    cliout.BuildLedgerJSON(e),
		},
	}
	if e.State == drift.Invalid && e.LocalErr != nil {
		j.Verdict.Reason = e.LocalErr.Error()
	}
	if e.State == drift.Drifted {
		j.Changes = &changesJSON{
			Query:               e.Cmp.QueryDiffers,
			EngineVersion:       e.Cmp.EngineVersionDiffers,
			Emit:                e.Cmp.EmitDiffers,
			TrackEmittedStreams: e.Cmp.TrackEmittedStreamsDiffers,
		}
	}
	return j
}

func encodeDiffJSON(w io.Writer, j diffJSON) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(j)
}
