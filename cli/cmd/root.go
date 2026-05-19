package cmd

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/updatecheck"
)

// silentError wraps an error that has already been printed to stderr by the
// command itself. fang's error handler skips it to avoid duplicate output.
type silentError struct{ err error }

func (e *silentError) Error() string { return e.err.Error() }
func (e *silentError) Unwrap() error { return e.err }

// silent wraps err so fang won't print it. Use when the command has already
// shown the user a more useful message.
func silent(err error) error { return &silentError{err: err} }

func errorHandler(w io.Writer, styles fang.Styles, err error) {
	var s *silentError
	if errors.As(err, &s) {
		return
	}
	fang.DefaultErrorHandler(w, styles, err)
}

// NewRootCmd returns a fresh root command tree with all subcommands wired up.
// Production code uses Execute(); tests construct a fresh tree per test for
// isolation.
func NewRootCmd() *cobra.Command {
	var noUpdateCheck bool

	root := &cobra.Command{
		Use:   "gaffer",
		Short: "Projection toolkit for KurrentDB",
		Long:  "Develop, test, debug, and deploy KurrentDB projections.",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// Update-check is best-effort: skip when stderr isn't a
			// TTY (extension-spawned lsp/mcp, piped stderr, CI runs).
			// Start is nil-safe when no Client was stashed on ctx -
			// the cmd test harness exercises that branch.
			updatecheck.FromCtx(cmd.Context()).Start(noUpdateCheck || !isatty.IsTerminal(os.Stderr.Fd()))
			return nil
		},
	}

	root.PersistentFlags().BoolVar(&noUpdateCheck, "no-update-check", false,
		"Skip the once-per-day check for a newer gaffer release")

	registerHiddenInvocationFlags(root)

	root.AddCommand(newVersionCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newScaffoldCmd())
	root.AddCommand(newDevCmd())
	root.AddCommand(newManifestCmd())
	root.AddCommand(newInfoCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newLSPCmd())
	root.AddCommand(newConfigCmd())

	return root
}

// ExecuteRoot runs the given root command via fang. Used by both production
// Execute() and tests so they share the same execution path.
func ExecuteRoot(ctx context.Context, root *cobra.Command) error {
	return fang.Execute(ctx, root, fang.WithoutVersion(), fang.WithErrorHandler(errorHandler))
}

// registerHiddenInvocationFlags declares --invoker-id / --invoked-by /
// --invoked-via on root as hidden persistent flags. The values are
// parsed pre-cobra by telemetry.PeekInvocationFlags so the Client can
// stamp them at construction time (notice suppression needs to know
// before identity mint runs); cobra still needs to know the flags
// exist or it rejects them as unknown when a subcommand is invoked.
// The bound vars are sinks - nothing reads them after parse.
func registerHiddenInvocationFlags(root *cobra.Command) {
	var invokerID, invokedBy, invokedVia string
	flags := root.PersistentFlags()
	flags.StringVar(&invokerID, "invoker-id", "", "")
	flags.StringVar(&invokedBy, "invoked-by", "", "")
	flags.StringVar(&invokedVia, "invoked-via", "", "")
	// MarkHidden only errors if the named flag doesn't exist, which
	// is a programmer bug here. Panic so a future rename gets caught
	// loudly in tests rather than silently un-hiding the flag.
	mustHide := func(name string) {
		if err := flags.MarkHidden(name); err != nil {
			panic(err)
		}
	}
	mustHide("invoker-id")
	mustHide("invoked-by")
	mustHide("invoked-via")
}

// Execute runs the root command via fang for styled help and
// completions. ctx is propagated to subcommands via cobra's
// ExecuteContext; main passes a ctx that carries the per-process
// telemetry Client.
func Execute(ctx context.Context) error {
	return ExecuteRoot(ctx, NewRootCmd())
}
