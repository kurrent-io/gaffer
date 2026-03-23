package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new gaffer project",
	Long:  "Creates gaffer.toml, .gitignore, and .gaffer/ directory in the current directory.",
	RunE:  runInit,
}

var initYes bool

func init() {
	initCmd.Flags().BoolVarP(&initYes, "yes", "y", false, "Accept all defaults, no prompts")
}

func runInit(cmd *cobra.Command, args []string) error {
	if !initYes {
		return fmt.Errorf("interactive mode not yet supported, use --yes / -y")
	}

	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	configPath := filepath.Join(dir, "gaffer.toml")
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("gaffer.toml already exists in %s", dir)
	}

	cfg := &config.Config{}
	if err := config.Save(configPath, cfg); err != nil {
		return err
	}

	if err := ensureGitignoreEntries(filepath.Join(dir, ".gitignore"), []string{
		".env",
		".env.*",
		".gaffer/",
	}); err != nil {
		return fmt.Errorf("updating .gitignore: %w", err)
	}

	gafferDir := filepath.Join(dir, ".gaffer")
	if err := os.MkdirAll(gafferDir, 0o755); err != nil {
		return fmt.Errorf("creating .gaffer/: %w", err)
	}

	fmt.Println("Initialized gaffer project")
	return nil
}

func ensureGitignoreEntries(path string, entries []string) error {
	var existing string
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}

	var toAdd []string
	for _, entry := range entries {
		if !strings.Contains(existing, entry) {
			toAdd = append(toAdd, entry)
		}
	}

	if len(toAdd) == 0 {
		return nil
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	if existing != "" && !strings.HasSuffix(existing, "\n") {
		if _, err := f.WriteString("\n"); err != nil {
			return err
		}
	}

	for _, entry := range toAdd {
		if _, err := f.WriteString(entry + "\n"); err != nil {
			return err
		}
	}

	return nil
}
