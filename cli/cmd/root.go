package cmd

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/prompt"
	"github.com/kurrent-io/gaffer/cli/internal/ttyutil"
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

// exitError carries a specific process exit code out to runMain. It composes with
// silent: wrap silent(...) to also keep fang from reprinting a message the command
// already rendered. Used for deploy's CI exit-code contract (see exitWith).
type exitError struct {
	err  error
	code int
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// exitWith tags err with a process exit code. The deploy CI contract is: 0 success
// or no-op, 1 error, 2 changes pending (--dry-run found work), 3 refused by a
// guardrail (confirmation needed but unavailable, or --no-validate on production).
// Wrap silent(...) for a code whose message is already on screen (exit 2); pass a
// plain error for a code whose message fang should print (exit 3).
func exitWith(code int, err error) error { return &exitError{err: err, code: code} }

// ExitCodeFor maps a command error to a process exit code for runMain, which calls
// it only on a non-nil error. An explicit exitWith code wins; otherwise a guardrail
// refusal (confirmation unavailable) is 3 wherever it surfaces - deploy, recreate,
// or an operate verb - so a non-interactive caller can tell "satisfy the gate and
// retry" from a genuine failure. Everything else is 1.
func ExitCodeFor(err error) int {
	var e *exitError
	if errors.As(err, &e) {
		return e.code
	}
	if errors.Is(err, errNeedConfirm) || errors.Is(err, errOperateNeedsConfirm) {
		return 3
	}
	return 1
}

func errorHandler(w io.Writer, styles fang.Styles, err error) {
	var s *silentError
	if errors.As(err, &s) {
		return
	}
	// A prompt the user aborted (Ctrl+C / Esc) is a clean cancellation,
	// not a failure: huh has already restored the terminal, so print
	// nothing.
	if errors.Is(err, prompt.ErrCancelled) {
		return
	}
	var argErr *argCountError
	if errors.As(err, &argErr) {
		// Reuse fang's styled ERROR badge, but print the body as plain
		// indented text rather than through styles.ErrorText: that style
		// reflows to a fixed width and collapses our newline, joining the
		// headline and example onto one line. Printing it ourselves also
		// drops the trailing "." fang appends, which would look mistyped
		// after a runnable example. The example stands in for fang's
		// "Try --help" usage hint.
		body := "  " + strings.ReplaceAll(argErr.Error(), "\n", "\n  ")
		_, _ = io.WriteString(w, styles.ErrorHeader.String()+"\n")
		_, _ = io.WriteString(w, body+"\n\n")
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
			// Update-check has two gates: disable shuts everything
			// off (--no-update-check); quiet suppresses just the
			// stderr notice while still refreshing the cache. We go
			// quiet on non-interactive paths (no TTY) and structured
			// output (manifest, lsp, mcp, --json) because the human-
			// readable card is noise there, but the cache still needs
			// refreshing so `gaffer manifest`'s updateAvailable field
			// stays useful for editor wrappers that only ever invoke
			// gaffer non-interactively. Start is nil-safe when no
			// Client was stashed on ctx - the cmd test harness
			// exercises that branch.
			quiet := !ttyutil.IsTerminal(os.Stderr) || emitsStructuredOutput(cmd)
			updatecheck.FromCtx(cmd.Context()).Start(noUpdateCheck, quiet)
			return nil
		},
	}

	root.PersistentFlags().BoolVar(&noUpdateCheck, "no-update-check", false,
		"Skip the once-per-day check for a newer gaffer release")

	registerHiddenInvocationFlags(root)

	// Group commands by workflow in --help rather than one flat alphabetical
	// list. Order within a group is intentional (e.g. inspect -> sync -> operate),
	// so turn off cobra's alphabetical sort and add in that order.
	cobra.EnableCommandSorting = false

	const (
		grpDevelop     = "develop"
		grpEnvironment = "environment"
		grpTools       = "tools"
	)
	root.AddGroup(
		&cobra.Group{ID: grpDevelop, Title: "Develop locally"},
		&cobra.Group{ID: grpEnvironment, Title: "Deploy & operate"},
		&cobra.Group{ID: grpTools, Title: "Tools & config"},
	)

	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add(grpDevelop, newInitCmd(), newScaffoldCmd(), newDevCmd(), newInfoCmd())
	add(grpEnvironment, newDiffCmd(), newStatusCmd(), newHistoryCmd(), newDeployCmd(), newEnableCmd(), newDisableCmd(), newRecreateCmd(), newDeleteCmd())
	add(grpTools, newAuthCmd(), newConfigCmd(), newMCPCmd(), newLSPCmd(), newVersionCmd())

	// manifest is editor-facing: hidden from help, so it needs no group.
	root.AddCommand(newManifestCmd())

	// The auto-generated help and completion commands have no group by default;
	// put them with the other tooling so nothing dangles above the groups.
	root.SetHelpCommandGroupID(grpTools)
	root.SetCompletionCommandGroupID(grpTools)

	return root
}

// ExecuteRoot runs the given root command via fang. Used by both production
// Execute() and tests so they share the same execution path.
func ExecuteRoot(ctx context.Context, root *cobra.Command) error {
	return fang.Execute(ctx, root, fang.WithoutVersion(), fang.WithErrorHandler(errorHandler))
}

// AnnotationOutput tags a cobra.Command with the kind of output it
// emits. Commands tagged OutputStructured always speak machine-
// readable bytes (manifest's JSON, lsp/mcp's JSON-RPC); commands
// that flip via a --json flag are detected at runtime by
// emitsStructuredOutput.
const AnnotationOutput = "gaffer.output"

// OutputStructured is the AnnotationOutput value declaring that a
// command always emits machine-readable output.
const OutputStructured = "structured"

// emitsStructuredOutput reports whether cmd's invocation produces
// machine-readable output - either because the command (or any of
// its parents) declared AnnotationOutput=OutputStructured at
// construction time or because the user passed --json. The update-
// check stderr notice is suppressed for these so wrappers and pipes
// see only the bytes they asked for.
//
// Parent walk: cobra doesn't inherit annotations, so a structured
// command that ever grows runnable subcommands would silently leak
// the notice unless every leaf re-declares the tag. Walking the
// parent chain means a single declaration on the group covers the
// whole subtree.
func emitsStructuredOutput(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if c.Annotations[AnnotationOutput] == OutputStructured {
			return true
		}
	}
	if v, err := cmd.Flags().GetBool("json"); err == nil && v {
		return true
	}
	return false
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
