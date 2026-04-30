package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/scaffold"
	"github.com/spf13/cobra"
)

type scaffoldOpts struct {
	Source    string
	Partition string
	Emit      bool
}

func newScaffoldCmd() *cobra.Command {
	opts := &scaffoldOpts{}

	cmd := &cobra.Command{
		Use:   "scaffold [name]",
		Short: "Add a new projection to the project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScaffold(args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.Source, "source", "all", "Event source (all, stream:name, category:name)")
	cmd.Flags().StringVar(&opts.Partition, "partition", "none", "Partitioning (none, per-stream)")
	cmd.Flags().BoolVar(&opts.Emit, "emit", false, "Enable emit/linkTo")
	return cmd
}

func runScaffold(name string, opts *scaffoldOpts) error {
	root := project.FindRoot()
	if root == "" {
		return project.ErrNotInProject
	}

	cfg, err := config.Load(filepath.Join(root, "gaffer.toml"))
	if err != nil {
		return err
	}

	result, err := scaffold.Scaffold(root, cfg, name, opts.Source, opts.Partition, opts.Emit)
	if err != nil {
		return err
	}

	fmt.Printf("Created %s\n", result.RelPath)
	return nil
}
