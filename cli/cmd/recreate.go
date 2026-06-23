package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/remote"
)

type recreateOpts struct {
	operateOpts
	NoValidate    bool
	DeleteEmitted bool
}

func newRecreateCmd() *cobra.Command {
	var opts recreateOpts
	cmd := &cobra.Command{
		Use:   "recreate <projection>",
		Short: "Destroy and rebuild a projection from local config",
		Long: "Recreate a projection on a KurrentDB environment: stop it, delete it (with its state and " +
			"checkpoint streams), then create it fresh from gaffer.toml, reprocessing from zero.\n\n" +
			"For a change deploy can't apply in place (engine version or track-emitted-streams, both " +
			"create-only), or a clean-slate rebuild of a wedged projection an in-place reset can't fix. " +
			"The projection must be in gaffer.toml (recreate builds from local config) and already " +
			"deployed.\n\n" +
			"Destructive and not reversible, so it always confirms (louder against production); --yes " +
			"skips the prompt. It compiles the projection first, before anything is deleted, so a broken " +
			"local definition can't leave you with nothing to rebuild; --no-validate skips that check, " +
			"though production refuses it. " +
			"--delete-emitted also removes the streams the projection wrote (off by default; reprocessing " +
			"otherwise re-emits and may duplicate into them). Pass --json for machine-readable output.",
		Example: "  gaffer recreate order-count\n" +
			"  gaffer recreate order-count --env staging",
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRecreate(cmd, args[0], opts)
		},
	}
	addEnvFlags(cmd, &opts.operateOpts)
	cmd.Flags().BoolVar(&opts.NoValidate, "no-validate", false, "Skip the preflight compile check and recreate anyway")
	cmd.Flags().BoolVar(&opts.DeleteEmitted, "delete-emitted", false, "Also delete the streams the projection emitted")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip the confirmation prompt")
	return cmd
}

func runRecreate(cmd *cobra.Command, name string, opts recreateOpts) error {
	if err := checkOperable(name); err != nil {
		return err
	}

	cfg, root, r, cleanup, err := connectEnv(opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer cleanup()

	if cfg.FindProjection(name) == nil {
		return fmt.Errorf("projection %q is not in gaffer.toml; recreate rebuilds from local config", name)
	}

	ctx := cmd.Context()

	// Compile gate before any destructive step: Delete happens before Create, so a
	// local that won't compile (or carries error-severity diagnostics) must be
	// caught before we delete, or the projection is gone with nothing to rebuild.
	// --no-validate skips it - the operator's risk to take, mirroring deploy.
	if !opts.NoValidate {
		if failures := runPreflight(ctx, root, cfg, []string{name}); len(failures) > 0 {
			if err := renderPreflightFailures(cmd.OutOrStdout(), opts.JSON, 1, failures); err != nil {
				return err
			}
			return silent(fmt.Errorf("preflight failed: %s has errors", name))
		}
	}

	cmpCtx, cmpCancel := context.WithTimeout(ctx, projectionRPCTimeout)
	cmp, err := compareProjection(cmpCtx, r, cfg, root, name)
	cmpCancel()
	if err != nil {
		return err
	}
	target, prod := resolveOperateTarget(ctx, r, opts.Env)

	// A hard compile failure leaves no descriptor to create from, so refuse even
	// under --no-validate (which only bypasses the diagnostics gate, not a query
	// that won't compile).
	if cmp.State == driftInvalid {
		return fmt.Errorf("projection %q does not compile, so there's nothing to recreate it from: %w", name, cmp.LocalErr)
	}
	if cmp.State == driftNotDeployed {
		return fmt.Errorf("projection %q is not deployed on %s; run gaffer deploy to create it", name, targetDesc(target))
	}
	local := cmp.Local
	if local == nil {
		// Unreachable: only an untracked projection has a nil Local, and that's
		// rejected by the gaffer.toml check above. Guard so a future change to
		// compareProjection can't silently delete with nothing to recreate.
		return fmt.Errorf("projection %q has no local definition to recreate from", name)
	}

	if prod && opts.NoValidate {
		return silent(fmt.Errorf("--no-validate is not allowed on production %s: it skips the preflight compile check. Recreate without it so the projection is validated first", targetDesc(target)))
	}

	// An emitting projection re-emits on the rebuild, duplicating into its target
	// streams, unless those streams are wiped first. Surface this before the
	// confirm (and on the --yes path, where the operator is least watching).
	if local.Emit && !opts.DeleteEmitted && !opts.JSON {
		w := newTextWriter(cmd.ErrOrStderr(), cmd.ErrOrStderr())
		w.write("%s %s\n", w.styles.warning.Render("⚠"),
			w.styles.warning.Render(name+" emits; recreating re-emits and may duplicate - pass --delete-emitted for a clean rebuild"))
	}

	if err := confirmOp(opQuestion("Recreate", name, target, prod), opts.Yes, opts.JSON); err != nil {
		return err
	}

	// Disable -> Delete -> Create, each RPC bounded separately. The destroy
	// precedes the create, so a failure after Delete leaves the projection gone:
	// name the recovery rather than a bare error.
	if err := rpc(ctx, func(ctx context.Context) error { return r.Disable(ctx, name) }); err != nil {
		return fmt.Errorf("could not stop %s before recreating: %w", name, err)
	}
	if err := rpc(ctx, func(ctx context.Context) error {
		return r.Delete(ctx, name, remote.DeleteOptions{
			DeleteStateStream:      true,
			DeleteCheckpointStream: true,
			DeleteEmittedStreams:   opts.DeleteEmitted,
		})
	}); err != nil {
		return fmt.Errorf("could not delete %s before recreating: %w", name, err)
	}
	if err := rpc(ctx, func(ctx context.Context) error {
		return r.Create(ctx, name, local.Query, remote.CreateOptions{
			EngineVersion:       local.EngineVersion,
			Emit:                local.Emit,
			TrackEmittedStreams: local.TrackEmittedStreams,
		})
	}); err != nil {
		return fmt.Errorf("%s was deleted but recreating it failed - re-run gaffer recreate %s, or gaffer deploy %s: %w", name, name, name, err)
	}

	return renderOperate(cmd.OutOrStdout(), opts.JSON, name, "recreated", target)
}
