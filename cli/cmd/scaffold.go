package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/scaffold"
	"github.com/spf13/cobra"
)

var scaffoldCmd = &cobra.Command{
	Use:   "scaffold [name]",
	Short: "Add a new projection to the project",
	Args:  cobra.ExactArgs(1),
	RunE:  runScaffold,
}

var (
	scaffoldSource    string
	scaffoldPartition string
	scaffoldEmit      bool
)

func init() {
	scaffoldCmd.Flags().StringVar(&scaffoldSource, "source", "all", "Event source (all, stream:name, category:name)")
	scaffoldCmd.Flags().StringVar(&scaffoldPartition, "partition", "none", "Partitioning (none, per-stream)")
	scaffoldCmd.Flags().BoolVar(&scaffoldEmit, "emit", false, "Enable emit/linkTo")
}

func runScaffold(cmd *cobra.Command, args []string) error {
	name := args[0]

	root := project.FindRoot()
	if root == "" {
		return project.ErrNotInProject
	}

	cfg, err := config.Load(filepath.Join(root, "gaffer.toml"))
	if err != nil {
		return err
	}

	result, err := scaffold.Scaffold(root, cfg, name, scaffoldSource, scaffoldPartition, scaffoldEmit)
	if err != nil {
		return err
	}

	fmt.Printf("Created %s\n", result.RelPath)
	return nil
}
