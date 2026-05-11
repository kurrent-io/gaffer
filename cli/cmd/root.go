package cmd

import (
	"context"
	"errors"
	"io"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
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
	root := &cobra.Command{
		Use:   "gaffer",
		Short: "Projection toolkit for KurrentDB",
		Long:  "Develop, test, debug, and deploy KurrentDB projections.",
	}

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

// Execute runs the root command via fang for styled help and completions.
func Execute() error {
	return ExecuteRoot(context.Background(), NewRootCmd())
}
