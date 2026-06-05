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
	"github.com/kurrent-io/gaffer/cli/internal/prompt"
	"github.com/kurrent-io/gaffer/cli/internal/scaffold"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

type scaffoldOpts struct {
	Name      string
	Source    string
	Partition string
	Emit      bool
	Yes       bool
}

func newScaffoldCmd() *cobra.Command {
	opts := &scaffoldOpts{}

	cmd := &cobra.Command{
		Use:   "scaffold <path>",
		Short: "Add a new projection to the project",
		Long: "Create a projection at <path>. The path is resolved relative to the " +
			"current directory and must end in a supported extension (" +
			strings.Join(scaffold.ListExtensions(), ", ") + "). " +
			"The projection's gaffer.toml key defaults to the file's basename; pass --name to override. " +
			"Run without <path> on a terminal to be prompted for the path and options.",
		Example: "gaffer scaffold ./projections/order.js",
		Args:    maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitScaffold(cmd.Context(), telemetry.ScaffoldCommandInvokedProperties{Outcome: o})
			})
			return runScaffold(cmd, args, opts)
		},
	}
	cmd.Flags().StringVar(&opts.Name, "name", "", "Projection name in gaffer.toml (defaults to the file's basename)")
	cmd.Flags().StringVar(&opts.Source, "source", "all", "Event source (all, stream:name, category:name)")
	cmd.Flags().StringVar(&opts.Partition, "partition", "none", "Partitioning (none, per-stream)")
	cmd.Flags().BoolVar(&opts.Emit, "emit", false, "Enable emit/linkTo")
	cmd.Flags().BoolVarP(&opts.Yes, "yes", "y", false, "Skip prompts and accept defaults")
	return cmd
}

func runScaffold(cmd *cobra.Command, args []string, opts *scaffoldOpts) error {
	root := project.FindRoot()
	if root == "" {
		return project.ErrNotInProject
	}

	cfg, err := config.Load(project.ConfigPath(root))
	if err != nil {
		return err
	}

	interactive := prompt.Enabled(opts.Yes)
	pathArg, err := resolveRequiredArg(cmd, args, interactive, func() (string, error) {
		p, err := prompt.Input("Projection file path", "", "./projections/order.js", validateScaffoldPath)
		return strings.TrimSpace(p), err
	})
	if err != nil {
		return err
	}

	if interactive {
		if err := promptScaffoldOptions(cmd, opts); err != nil {
			return err
		}
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

// promptScaffoldOptions fills the option gaps the user didn't pass via
// flags - any of source / partition / emit. No summary confirm:
// Ctrl-C/Esc aborts if you change your mind. opts is mutated in place.
func promptScaffoldOptions(cmd *cobra.Command, opts *scaffoldOpts) error {
	if !cmd.Flags().Changed("source") {
		if err := promptSource(opts); err != nil {
			return err
		}
	}

	if !cmd.Flags().Changed("partition") {
		// Filter on the chosen source: per-stream partitioning is invalid
		// with a single stream, so don't offer it. With only "none" left
		// there's nothing to ask.
		partitionOpts := partitionOptionsFor(opts.Source)
		if len(partitionOpts) == 1 {
			opts.Partition = partitionOpts[0].Value
		} else {
			part, err := prompt.Select("Partitioning", partitionOpts, opts.Partition)
			if err != nil {
				return err
			}
			opts.Partition = part
		}
	}

	if !cmd.Flags().Changed("emit") {
		emit, err := prompt.Confirm("Enable emit/linkTo?", opts.Emit)
		if err != nil {
			return err
		}
		opts.Emit = emit
	}

	return nil
}

// promptSource asks for the event source. "all" needs no follow-up;
// stream / category each take a name that's folded into the
// "stream:<name>" / "category:<name>" form scaffold.GenerateSource
// expects.
func promptSource(opts *scaffoldOpts) error {
	kind, err := prompt.Select("Event source", []prompt.Option{
		{Label: "All events", Value: "all"},
		{Label: "A single stream", Value: "stream"},
		{Label: "A category", Value: "category"},
	}, sourceKind(opts.Source))
	if err != nil {
		return err
	}
	if kind == "all" {
		opts.Source = "all"
		return nil
	}
	title := strings.ToUpper(kind[:1]) + kind[1:] + " name"
	name, err := prompt.Input(title, "", "orders", func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("a %s name is required", kind)
		}
		return nil
	})
	if err != nil {
		return err
	}
	opts.Source = kind + ":" + strings.TrimSpace(name)
	return nil
}

// partitionOptionsFor returns the partition choices valid for source.
// A single-stream source (fromStream) can't use per-stream partitioning
// (foreachStream), so only "none" is offered; every other source gets
// both. Mirrors the rule GenerateSource enforces.
func partitionOptionsFor(source string) []prompt.Option {
	opts := []prompt.Option{{Label: "None", Value: "none"}}
	if sourceKind(source) != "stream" {
		opts = append(opts, prompt.Option{Label: "Per stream", Value: "per-stream"})
	}
	return opts
}

// sourceKind maps a source value back to its prompt option so an
// explicit default (or a --source not passed) pre-highlights sensibly.
func sourceKind(source string) string {
	switch {
	case strings.HasPrefix(source, "stream:"):
		return "stream"
	case strings.HasPrefix(source, "category:"):
		return "category"
	default:
		return "all"
	}
}

// validateScaffoldPath gives immediate feedback in the prompt on the one
// rule that's cheap to check here - the extension allowlist. Path
// resolution and the no-escape / collision checks still run downstream in
// resolveScaffoldRelPath and scaffold.Scaffold.
func validateScaffoldPath(p string) error {
	if strings.TrimSpace(p) == "" {
		return fmt.Errorf("a path is required")
	}
	if !scaffold.IsSupported(filepath.Ext(p)) {
		return fmt.Errorf("path must end in %s", strings.Join(scaffold.ListExtensions(), ", "))
	}
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
