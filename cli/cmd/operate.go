package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/prompt"
	"github.com/kurrent-io/gaffer/cli/internal/remote"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

// operateOpts are the flags shared by the operate-tier verbs (enable / disable /
// delete): the target selectors, --json for machine output, and --yes to skip
// the confirmation on the guarded verbs.
type operateOpts struct {
	Env        string
	Connection string
	JSON       bool
	Yes        bool
}

// errOperateNeedsConfirm is returned when a guarded operate verb can't confirm -
// no terminal (or --json) and no --yes. Fail-closed: a destructive or disruptive
// operation never proceeds unconfirmed.
var errOperateNeedsConfirm = errors.New("needs confirmation but can't prompt: run in a terminal, or pass --yes to proceed non-interactively")

// addEnvFlags adds the target selectors and --json, shared by every operate verb.
func addEnvFlags(cmd *cobra.Command, opts *operateOpts) {
	cmd.Flags().StringVar(&opts.Env, "env", "", "Environment from gaffer.toml")
	cmd.Flags().StringVar(&opts.Connection, "connection", "", "KurrentDB connection string (overrides --env)")
	cmd.Flags().BoolVar(&opts.JSON, "json", false, "Output as JSON")
}

// checkOperable refuses $-prefixed system projections. gaffer doesn't manage
// them (status/diff/deploy exclude them), and disabling or deleting one - say
// $by_category - would break the database's standard projections.
func checkOperable(name string) error {
	if strings.HasPrefix(name, "$") {
		return fmt.Errorf("%q is a system projection, which gaffer does not manage", name)
	}
	return nil
}

// requireExists fails with a friendly message when the named projection isn't on
// the target, so a verb reports "not deployed" rather than a raw RPC error.
func requireExists(ctx context.Context, r *remote.Client, name, target string) error {
	var ok bool
	if err := rpc(ctx, func(ctx context.Context) error {
		var err error
		ok, err = r.Exists(ctx, name)
		return err
	}); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("projection %q is not deployed on %s", name, targetDesc(target))
	}
	return nil
}

// confirmOp gates a guarded operate verb on a yes/no answer. yes skips it; --json
// or a non-terminal can't prompt, so without --yes it fails closed. Returns nil
// to proceed, prompt.ErrCancelled if declined.
func confirmOp(question string, yes, jsonOut bool) error {
	if !jsonOut && prompt.Enabled(yes) {
		ok, err := prompt.Confirm(question, false)
		if err != nil {
			return err
		}
		if !ok {
			return prompt.ErrCancelled
		}
		return nil
	}
	if yes {
		return nil
	}
	return errOperateNeedsConfirm
}

// opQuestion phrases the confirm: the verb, the projection, and (when known) the
// target it lands on, with production named as such so the prompt reads louder.
func opQuestion(verb, name, target string, prod bool) string {
	if where := prodWhere(target, prod); where != "" {
		return fmt.Sprintf("%s %s on %s?", verb, name, where)
	}
	return fmt.Sprintf("%s %s?", verb, name)
}

// operateJSON is the --json shape for an operate verb: the projection and its
// past-tense outcome (enabled / disabled / aborted / deleted).
type operateJSON struct {
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
}

// renderOperate reports a completed operation: a JSON object with --json, else a
// one-line confirmation naming the target when known.
func renderOperate(out io.Writer, jsonOut bool, name, outcome, target string) error {
	if jsonOut {
		b, err := json.Marshal(operateJSON{Name: name, Outcome: outcome})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(b))
		return err
	}
	past := map[string]string{"enabled": "Enabled", "disabled": "Disabled", "aborted": "Aborted", "deleted": "Deleted", "recreated": "Recreated"}[outcome]
	if target != "" {
		_, err := fmt.Fprintf(out, "%s %s on %s.\n", past, name, target)
		return err
	}
	_, err := fmt.Fprintf(out, "%s %s.\n", past, name)
	return err
}

func newEnableCmd() *cobra.Command {
	var opts operateOpts
	cmd := &cobra.Command{
		Use:   "enable <projection>",
		Short: "Enable (start) a projection on an environment",
		Long: "Enable a projection on a KurrentDB environment: start it so it resumes processing " +
			"from its last checkpoint.\n\n" +
			"Acts on what's deployed, named directly, so the projection need not be in gaffer.toml. " +
			"Enabling an already-running projection is a no-op on the server. Pass --json for " +
			"machine-readable output.",
		Example: "  gaffer enable order-count\n" +
			"  gaffer enable order-count --env staging",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			var prod *bool
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitEnable(cmd.Context(), telemetry.EnableCommandInvokedProperties{Outcome: o, ProdTarget: prod})
			})
			do := func(ctx context.Context, c *remote.Client, n string) error { return c.Enable(ctx, n) }
			return runOperate(cmd, args[0], opts, opSpec{verb: "Enable", outcome: "enabled", do: do}, func(p bool) { prod = &p })
		},
	}
	addEnvFlags(cmd, &opts)
	return cmd
}

func newDisableCmd() *cobra.Command {
	var opts operateOpts
	var abort bool
	cmd := &cobra.Command{
		Use:   "disable <projection>",
		Short: "Disable (stop) a projection on an environment",
		Long: "Disable a projection on a KurrentDB environment: stop it so it no longer processes events.\n\n" +
			"By default it writes a final checkpoint, so a later enable resumes from where it " +
			"stopped. --abort skips that checkpoint, so a later enable replays from the last " +
			"persisted one. Disabling is recoverable (enable it again), so it confirms only against " +
			"production; --yes skips that prompt. Acts on what's deployed, named directly, so the " +
			"projection need not be in gaffer.toml. Pass --json for machine-readable output.",
		Example: "  gaffer disable order-count\n" +
			"  gaffer disable order-count --abort --env staging",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			var prod *bool
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitDisable(cmd.Context(), telemetry.DisableCommandInvokedProperties{Outcome: o, ProdTarget: prod})
			})
			verb, outcome := "Disable", "disabled"
			do := func(ctx context.Context, c *remote.Client, n string) error { return c.Disable(ctx, n) }
			if abort {
				verb, outcome = "Abort", "aborted"
				do = func(ctx context.Context, c *remote.Client, n string) error { return c.Abort(ctx, n) }
			}
			// Disabling is recoverable, so it only needs confirmation on production.
			return runOperate(cmd, args[0], opts, opSpec{verb: verb, outcome: outcome, do: do, confirmProdOnly: true}, func(p bool) { prod = &p })
		},
	}
	addEnvFlags(cmd, &opts)
	cmd.Flags().BoolVar(&abort, "abort", false, "Disable without writing a checkpoint (replays since the last one when re-enabled)")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip the production confirmation prompt")
	return cmd
}

func newDeleteCmd() *cobra.Command {
	var opts operateOpts
	var deleteEmitted bool
	cmd := &cobra.Command{
		Use:   "delete <projection>",
		Short: "Delete a projection from an environment",
		Long: "Delete a projection from a KurrentDB environment: remove it along with its state and " +
			"checkpoint streams, leaving any streams it emitted in place.\n\n" +
			"Destructive and not reversible, so it always confirms (louder against production); --yes " +
			"skips the prompt. --delete-emitted also removes the streams the projection wrote, for a " +
			"full clean-up. Acts on what's deployed, named directly, so the projection need not be in " +
			"gaffer.toml. Pass --json for machine-readable output.",
		Example: "  gaffer delete order-count\n" +
			"  gaffer delete order-count --delete-emitted --env staging",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			var prod *bool
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitDelete(cmd.Context(), telemetry.DeleteCommandInvokedProperties{Outcome: o, ProdTarget: prod})
			})
			do := func(ctx context.Context, c *remote.Client, n string) error {
				return c.Delete(ctx, n, remote.DeleteOptions{
					DeleteStateStream:      true,
					DeleteCheckpointStream: true,
					DeleteEmittedStreams:   deleteEmitted,
				})
			}
			return runOperate(cmd, args[0], opts, opSpec{verb: "Delete", outcome: "deleted", do: do, confirmAlways: true, disableFirst: true}, func(p bool) { prod = &p })
		},
	}
	addEnvFlags(cmd, &opts)
	cmd.Flags().BoolVar(&deleteEmitted, "delete-emitted", false, "Also delete the streams the projection emitted")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}

// opSpec is one verb's behaviour for the shared operate flow: how to name it, the
// RPC to run, and when to confirm.
type opSpec struct {
	verb            string // confirm/render verb, e.g. "Disable"
	outcome         string // past-tense outcome, e.g. "disabled"
	do              func(context.Context, *remote.Client, string) error
	confirmAlways   bool // delete: confirm everywhere
	confirmProdOnly bool // disable: confirm only against production
	disableFirst    bool // delete: the server rejects deleting an enabled projection
}

// runOperate is the shared enable/disable flow: connect, resolve the target, check
// the projection exists, confirm per the verb's policy, then run the RPC. setProd,
// when non-nil, is called with the target's production tier once resolved, for the
// caller's command_invoked event; it's left uncalled (so prod_target stays absent,
// matching deploy/rollback/recreate) when the run fails before the target is
// reached.
func runOperate(cmd *cobra.Command, name string, opts operateOpts, spec opSpec, setProd func(bool)) error {
	if err := checkOperable(name); err != nil {
		return err
	}

	conn, err := connectEnv(opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer conn.cleanup()
	r := conn.r

	ctx := cmd.Context()
	target, prod := r.OperateTarget(ctx, conn.env, projectionRPCTimeout)
	if setProd != nil {
		setProd(prod)
	}

	if err := requireExists(ctx, r, name, target); err != nil {
		return err
	}

	if spec.confirmAlways || (spec.confirmProdOnly && prod) {
		if err := confirmOp(opQuestion(spec.verb, name, target, prod), opts.Yes, opts.JSON); err != nil {
			return err
		}
	}

	// The server rejects deleting an enabled projection (stopped is not enough -
	// stop/abort leave it stopped-but-enabled), so disable it first. Each RPC is
	// bounded separately rather than sharing one budget.
	if spec.disableFirst {
		if err := rpc(ctx, func(ctx context.Context) error { return r.Disable(ctx, name) }); err != nil {
			return fmt.Errorf("could not disable %s before deleting: %w", name, err)
		}
	}
	if err := rpc(ctx, func(ctx context.Context) error { return spec.do(ctx, r, name) }); err != nil {
		return fmt.Errorf("could not %s %s: %w", strings.ToLower(spec.verb), name, err)
	}
	return renderOperate(cmd.OutOrStdout(), opts.JSON, name, spec.outcome, target)
}
