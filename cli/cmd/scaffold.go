package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/engine"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/spf13/cobra"
)

var scaffoldCmd = &cobra.Command{
	Use:   "scaffold [name]",
	Short: "Add a new projection to the project",
	Args:  cobra.ExactArgs(1),
	RunE:  runScaffold,
}

var (
	scaffoldLang      string
	scaffoldSource    string
	scaffoldPartition string
	scaffoldEmit      bool
)

func init() {
	scaffoldCmd.Flags().StringVar(&scaffoldLang, "lang", "js", "Language (js)")
	scaffoldCmd.Flags().StringVar(&scaffoldSource, "source", "all", "Event source (all, stream:name, category:name)")
	scaffoldCmd.Flags().StringVar(&scaffoldPartition, "partition", "none", "Partitioning (none, per-stream)")
	scaffoldCmd.Flags().BoolVar(&scaffoldEmit, "emit", false, "Enable emit/linkTo")
}

func runScaffold(cmd *cobra.Command, args []string) error {
	name := args[0]

	root := project.FindRoot()
	if root == "" {
		return fmt.Errorf("not in a gaffer project (no gaffer.toml found)")
	}

	cfg, err := config.Load(filepath.Join(root, "gaffer.toml"))
	if err != nil {
		return err
	}

	result, err := engine.Scaffold(root, cfg, name, scaffoldSource, scaffoldPartition, scaffoldEmit)
	if err != nil {
		return err
	}

	fmt.Printf("Created %s\n", result.RelPath)
	return nil
}

func langExtension(lang string) string {
	switch lang {
	case "ts":
		return ".ts"
	default:
		return ".js"
	}
}

