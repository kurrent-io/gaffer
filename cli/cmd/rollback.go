package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

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
	prefix, err := remote.NormalizeHashPrefix(hashArg)
	if err != nil {
		return err
	}

	conn, err := connectEnv(opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer conn.cleanup()
	root, r := conn.root, conn.r

	ctx := cmd.Context()
	target, prod := r.OperateTarget(ctx, conn.env, projectionRPCTimeout)

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

	tgt, err := r.FindVersionByHash(ctx, name, prefix)
	if err != nil {
		return err
	}

	currentDesc := current.Descriptor()
	if tgt.Hash == currentDesc.Hash() {
		return renderRollback(cmd.OutOrStdout(), opts.JSON, name, "unchanged", tgt.Hash, target)
	}

	tgtDesc := tgt.Def.Descriptor()
	cmp := deploy.Compare(tgtDesc, currentDesc)
	if err := remote.RollbackRefusal(cmp, tgt.Hash, name); err != nil {
		return err
	}

	if !opts.JSON {
		writeRollbackPreview(cmd.OutOrStdout(), name, currentDesc, tgtDesc, tgt.Hash, cmp)
	}
	if err := confirmOp(opQuestion("Roll back", name, target, prod), opts.Yes, opts.JSON); err != nil {
		return err
	}

	ledger := toolLedger(conn.env, remote.OpRollback, root)
	if err := rpc(ctx, func(ctx context.Context) error {
		return r.Update(ctx, name, tgt.Def.Query, remote.UpdateOptions{Emit: &tgt.Def.Emit, Ledger: &ledger})
	}); err != nil {
		return fmt.Errorf("could not roll back %s: %w", name, err)
	}
	return renderRollback(cmd.OutOrStdout(), opts.JSON, name, "rolled-back", tgt.Hash, target)
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
	tw.write("%s\n", tw.styles.warning.Render(glyphWarning+" code rolls back, state does not: state built by the newer query is kept (gaffer recreate rebuilds from zero)"))
	tw.write("%s\n", tw.styles.warning.Render(glyphWarning+" local files are untouched: gaffer diff will show this as drift until local is reconciled"))
	tw.blank()
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
