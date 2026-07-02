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

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

func newRollbackCmd() *cobra.Command {
	var opts operateOpts
	cmd := &cobra.Command{
		Use:   "rollback <projection> <hash>",
		Short: "Roll a projection back to a version from its history",
		Long: "Roll a projection back to a prior version from its history: redeploy that version's " +
			"definition (query and emit) in place, stamped as a rollback in the deploy ledger.\n\n" +
			"The target is named by its content hash, from gaffer history's hash column; any unique " +
			"prefix of at least 4 characters works. Rolling back changes only the deployed " +
			"definition: processing continues from the current checkpoint, so state built by the " +
			"newer query is kept (rebuild from zero with gaffer recreate if it must go), and your " +
			"local files are untouched, so gaffer diff shows the rollback as drift until you " +
			"reconcile local. A version whose engine version or emitted-stream tracking differs " +
			"from what's deployed can't be applied in place; update local config and use gaffer " +
			"recreate instead.\n\n" +
			"Acts on what's deployed, named directly, so the projection need not be in gaffer.toml. " +
			"It always confirms, showing the change as a diff first (louder against production); " +
			"--yes skips the prompt. Pass --json for machine-readable output.",
		Example: "  gaffer rollback order-count 23e1fa6\n" +
			"  gaffer rollback order-count 23e1fa6 --env staging",
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRollback(cmd, args[0], args[1], opts)
		},
	}
	addEnvFlags(cmd, &opts)
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}

func runRollback(cmd *cobra.Command, name, hashArg string, opts operateOpts) error {
	if err := checkOperable(name); err != nil {
		return err
	}
	prefix, err := normalizeHashPrefix(hashArg)
	if err != nil {
		return err
	}

	conn, err := connectEnv(opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer conn.cleanup()
	cfg, root, r := conn.cfg, conn.root, conn.r

	ctx := cmd.Context()
	target, prod := resolveOperateTarget(ctx, r, opts.Env)

	var current *remote.Definition
	if err := rpc(ctx, func(ctx context.Context) error {
		var err error
		current, err = r.Read(ctx, name)
		return err
	}); err != nil {
		if errors.Is(err, remote.ErrNotFound) {
			return fmt.Errorf("projection %q is not deployed on %s", name, targetDesc(target))
		}
		return err
	}

	tgt, err := findByHash(ctx, r, name, prefix)
	if err != nil {
		return err
	}

	currentDesc := current.Descriptor()
	if tgt.hash == currentDesc.Hash() {
		return renderRollback(cmd.OutOrStdout(), opts.JSON, name, "unchanged", tgt.hash, target)
	}

	tgtDesc := tgt.def.Descriptor()
	cmp := deploy.Compare(tgtDesc, currentDesc)
	if err := rollbackRefusal(cmp, tgt.hash, name); err != nil {
		return err
	}

	if !opts.JSON {
		writeRollbackPreview(cmd.OutOrStdout(), name, currentDesc, tgtDesc, tgt.hash, cmp)
	}
	if err := confirmOp(opQuestion("Roll back", name, target, prod), opts.Yes, opts.JSON); err != nil {
		return err
	}

	ledger := toolLedger(opts.Connection, opts.Env, remote.OpRollback, cfg, root)
	if err := rpc(ctx, func(ctx context.Context) error {
		return r.Update(ctx, name, tgt.def.Query, remote.UpdateOptions{Emit: &tgt.def.Emit, Ledger: &ledger})
	}); err != nil {
		return fmt.Errorf("could not roll back %s: %w", name, err)
	}
	return renderRollback(cmd.OutOrStdout(), opts.JSON, name, "rolled-back", tgt.hash, target)
}

// writeRollbackPreview shows what the rollback would change before the confirm:
// the hash movement, any emit flip, the query diff, and the two cautions - state
// is not rolled back with the code, and local files stay as they are.
func writeRollbackPreview(out io.Writer, name string, current, target deploy.Descriptor, targetHash string, cmp deploy.Comparison) {
	tw := newTextWriter(out, out)
	tw.write("Rolling back %s: %s → %s\n", name,
		tw.styles.dim.Render(shortHash(current.Hash())), tw.styles.label.Render(shortHash(targetHash)))
	tw.blank()
	if cmp.EmitDiffers {
		tw.write("  emit %s → %s\n", enabledStr(current.Emit), enabledStr(target.Emit))
	}
	if cmp.QueryDiffers {
		tw.WriteQueryDiff(deploy.LineDiff(current.Query, target.Query))
	}
	tw.blank()
	tw.write("%s\n", tw.styles.warning.Render("⚠ code rolls back, state does not: state built by the newer query is kept (gaffer recreate rebuilds from zero)"))
	tw.write("%s\n", tw.styles.warning.Render("⚠ local files are untouched: gaffer diff will show this as drift until local is reconciled"))
	tw.blank()
}

// rollbackRefusal refuses a target that differs from the deployed projection in
// a create-only dimension: rollback redeploys in place via Update, which carries
// only the query and emit, so an engine version or emitted-stream tracking
// change can't be applied. Nil when the target is applyable.
func rollbackRefusal(cmp deploy.Comparison, hash, name string) error {
	if !cmp.EngineVersionDiffers && !cmp.TrackEmittedStreamsDiffers {
		return nil
	}
	dim := "engine version"
	if !cmp.EngineVersionDiffers {
		dim = "emitted-stream tracking"
	}
	return fmt.Errorf("version %s differs from the deployed projection in %s, which rollback can't change in place; update local config and use `gaffer recreate %s`",
		shortHash(hash), dim, name)
}

// rollbackTarget is what a hash prefix resolved to in the projection's history:
// the definition to redeploy and its full content hash.
type rollbackTarget struct {
	def  *remote.Definition
	hash string
}

// findByHash scans the projection's whole history for content whose hash matches
// the prefix. The same content at several versions is one match - the hash is the
// identity - so only a prefix straddling two different contents is ambiguous.
// Pages newest-first like the interactive timeline, bounded per read by the
// history hard cap and overall by the stream itself.
func findByHash(ctx context.Context, r *remote.Client, name, prefix string) (*rollbackTarget, error) {
	matches := map[string]*remote.Definition{}
	before := int64(-1)
	for {
		var versions []remote.Version
		if err := rpc(ctx, func(ctx context.Context) error {
			var err error
			versions, _, err = r.ReadHistory(ctx, name, before, 0)
			return err
		}); err != nil {
			return nil, err
		}
		if len(versions) == 0 {
			break
		}
		matchHashes(versions, prefix, matches)
		oldest := versions[len(versions)-1].Number
		if oldest <= 0 {
			break
		}
		before = oldest
	}
	return resolveHashMatches(matches, prefix, name)
}

// matchHashes collects the distinct contents in a page whose hash carries the
// prefix. Tombstones have no content of their own and are skipped.
func matchHashes(versions []remote.Version, prefix string, matches map[string]*remote.Definition) {
	for _, v := range versions {
		if v.Deleted || v.Definition == nil {
			continue
		}
		h := v.Definition.Descriptor().Hash()
		if strings.HasPrefix(h, prefix) {
			if _, ok := matches[h]; !ok {
				matches[h] = v.Definition
			}
		}
	}
}

// resolveHashMatches turns the collected matches into the single target, or the
// not-found / ambiguous-prefix error.
func resolveHashMatches(matches map[string]*remote.Definition, prefix, name string) (*rollbackTarget, error) {
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no version matching %q in the history of %s", prefix, name)
	case 1:
		for h, d := range matches {
			return &rollbackTarget{def: d, hash: h}, nil
		}
	}
	hashes := make([]string, 0, len(matches))
	for h := range matches {
		hashes = append(hashes, shortHash(h))
	}
	sort.Strings(hashes)
	return nil, fmt.Errorf("%q matches %d different versions (%s); give more characters", prefix, len(matches), strings.Join(hashes, ", "))
}

// normalizeHashPrefix validates the hash argument: lowercase hex, at least 4
// characters so a stray character can't match half the history, at most a full
// 64-character hash.
func normalizeHashPrefix(s string) (string, error) {
	p := strings.ToLower(s)
	if len(p) < 4 {
		return "", fmt.Errorf("hash prefix %q is too short; give at least 4 characters", s)
	}
	if len(p) > 64 {
		return "", fmt.Errorf("hash prefix %q is longer than a full content hash", s)
	}
	for _, c := range p {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", fmt.Errorf("hash prefix %q is not hexadecimal", s)
		}
	}
	return p, nil
}

// rollbackJSON is the --json shape: the projection, the outcome (rolled-back, or
// unchanged when the target is already deployed), and the full target hash.
type rollbackJSON struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Hash    string `json:"hash"`
}

// renderRollback reports the result: a JSON object with --json, else one line
// naming the target when known.
func renderRollback(out io.Writer, jsonOut bool, name, outcome, hash, target string) error {
	if jsonOut {
		b, err := json.Marshal(rollbackJSON{Name: name, Outcome: outcome, Hash: hash})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(b))
		return err
	}
	if outcome == "unchanged" {
		_, err := fmt.Fprintf(out, "%s is already at %s.\n", name, shortHash(hash))
		return err
	}
	if target != "" {
		_, err := fmt.Fprintf(out, "Rolled back %s to %s on %s.\n", name, shortHash(hash), target)
		return err
	}
	_, err := fmt.Fprintf(out, "Rolled back %s to %s.\n", name, shortHash(hash))
	return err
}