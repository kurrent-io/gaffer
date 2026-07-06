package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/deploy"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
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
		Long: "Recreate a projection on a KurrentDB environment: disable it, delete it (with its state and " +
			"checkpoint streams), then create it fresh from gaffer.toml, reprocessing from zero. The " +
			"create records the same tool metadata deploy stamps (tool and version, source revision, " +
			"acting identity), so gaffer history shows the whole rebuild as a single recreate entry; " +
			"a KurrentDB that predates the feature ignores the metadata and recreate is unaffected.\n\n" +
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

	conn, err := connectEnv(opts.Connection, opts.Env)
	if err != nil {
		return err
	}
	defer conn.cleanup()
	cfg, root, r := conn.cfg, conn.root, conn.r

	def := cfg.FindProjection(name)
	if def == nil {
		return fmt.Errorf("projection %q is not in gaffer.toml; recreate rebuilds from local config", name)
	}
	// Per-projection config errors are deferred past config.Load; catch the named
	// projection's here, before the compile gate, so it fails with the clean
	// config message rather than a downstream compile/session error.
	if err := cfg.ProjectionConfigError(name); err != nil {
		return err
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

	// Build the descriptor by compiling the local source. recreate creates from
	// local config and never reads the deployed definition, so it avoids the $ops
	// stream read that drift.Compare needs - matching delete/enable/disable, which
	// only check existence. A hard compile failure leaves nothing to create from,
	// so refuse even under --no-validate (which only skips the diagnostics gate).
	source, err := engine.ReadSource(root, def.Entry)
	if err != nil {
		return err
	}
	local, err := engine.LocalDescriptor(engine.NewProjection(root, cfg, def, source))
	if err != nil {
		return fmt.Errorf("projection %q does not compile, so there's nothing to recreate it from: %w", name, err)
	}

	target, prod := resolveOperateTarget(ctx, r, opts.Env)
	if err := requireExists(ctx, r, name, target); err != nil {
		return err
	}

	// The production --no-validate guardrail is defined once (shared with deploy) so
	// its message and exit code can't drift.
	if prod && opts.NoValidate {
		return refuseNoValidateOnProd("Recreate", "the projection is", target)
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

	// The tool-metadata stamped on the rebuild's create, so history attributes
	// the recreate to gaffer instead of showing anonymous lifecycle steps.
	ledger := toolLedger(opts.Connection, opts.Env, remote.OpRecreate, cfg, root)

	// The destructive Disable -> Delete -> Create sequence, its ordering and
	// recovery messages, live in internal/deploy (shared in shape with deploy's
	// rebuild); here we only bind each step to the client and its option mapping.
	if err := deploy.Recreate(ctx, name, deploy.RecreateSteps{
		Disable: func(ctx context.Context) error { return r.Disable(ctx, name) },
		Delete: func(ctx context.Context) error {
			return r.Delete(ctx, name, remote.DeleteOptions{
				DeleteStateStream:      true,
				DeleteCheckpointStream: true,
				DeleteEmittedStreams:   opts.DeleteEmitted,
			})
		},
		Create: func(ctx context.Context) error {
			return r.Create(ctx, name, local.Query, remote.CreateOptions{
				EngineVersion:       local.EngineVersion,
				Emit:                local.Emit,
				TrackEmittedStreams: local.TrackEmittedStreams,
				Ledger:              &ledger,
			})
		},
	}); err != nil {
		return err
	}

	return renderOperate(cmd.OutOrStdout(), opts.JSON, name, "recreated", target)
}
