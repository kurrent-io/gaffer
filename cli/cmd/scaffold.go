// Path args on the CLI are always interpreted relative to the
// current working directory. Other commands that take a file path
// should follow the same rule for consistency.

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/pathutil"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/scaffold"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

type scaffoldOpts struct {
	Name      string
	Source    string
	Partition string
	Emit      bool
}

func newScaffoldCmd() *cobra.Command {
	opts := &scaffoldOpts{}

	cmd := &cobra.Command{
		Use:   "scaffold <path>",
		Short: "Add a new projection to the project",
		Long: "Create a projection at <path>. The path is resolved relative to the " +
			"current directory and must end in a supported extension (" +
			strings.Join(scaffold.ListExtensions(), ", ") + "). " +
			"The projection's gaffer.toml key defaults to the file's basename; pass --name to override.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitScaffold(cmd.Context(), telemetry.ScaffoldCommandInvokedProperties{Outcome: o})
			})
			return runScaffold(args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Name, "name", "", "Projection name in gaffer.toml (defaults to the file's basename)")
	cmd.Flags().StringVar(&opts.Source, "source", "all", "Event source (all, stream:name, category:name)")
	cmd.Flags().StringVar(&opts.Partition, "partition", "none", "Partitioning (none, per-stream)")
	cmd.Flags().BoolVar(&opts.Emit, "emit", false, "Enable emit/linkTo")
	return cmd
}

func runScaffold(pathArg string, opts *scaffoldOpts) error {
	root := project.FindRoot()
	if root == "" {
		return project.ErrNotInProject
	}

	cfg, err := config.Load(project.ConfigPath(root))
	if err != nil {
		return err
	}

	relPath, err := resolveScaffoldRelPath(pathArg, root)
	if err != nil {
		return err
	}

	// Name defaulting lives in scaffold.Scaffold so the CLI and the
	// MCP tool share the rule. Pass through whatever the user gave
	// us, empty or not.
	result, err := scaffold.Scaffold(root, cfg, opts.Name, relPath, opts.Source, opts.Partition, opts.Emit)
	if err != nil {
		return err
	}

	fmt.Printf("Created %s\n", result.RelPath)
	return nil
}

// resolveScaffoldRelPath converts a cwd-relative or absolute path
// arg into a project-root-relative form. Both sides are resolved
// through symlinks (via pathutil.ResolveAncestorSymlinks, which
// handles the case where the leaf doesn't exist yet) so a symlinked
// checkout doesn't lexically appear "outside" when it's the same
// project on disk. If the resolved input still lands outside root,
// error with the user's original argument echoed - the downstream
// scaffold validator would otherwise complain about the derived
// "../..." string the user never typed.
func resolveScaffoldRelPath(pathArg, root string) (string, error) {
	abs := pathArg
	if !filepath.IsAbs(abs) {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolving cwd: %w", err)
		}
		abs = filepath.Join(cwd, abs)
	}
	abs = filepath.Clean(abs)

	resolvedRoot, err := pathutil.ResolveAncestorSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolving project root: %w", err)
	}
	resolvedAbs, err := pathutil.ResolveAncestorSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", pathArg, err)
	}

	rel, err := filepath.Rel(resolvedRoot, resolvedAbs)
	if err != nil {
		return "", fmt.Errorf("resolving %q against project root: %w", pathArg, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf(
			"projection path %q is outside the project root",
			pathArg,
		)
	}
	return rel, nil
}
