package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new gaffer project",
		Long:  "Creates gaffer.toml, .gitignore, and .gaffer/ directory in the current directory.",
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitInit(cmd.Context(), telemetry.InitCommandInvokedProperties{Outcome: o})
			})
			return runInit()
		},
	}
	// `-y` is parsed but unused today: bare `gaffer init` already
	// runs non-interactively. The flag is kept so users / scripts
	// that already pass it don't break, and so it's available as the
	// explicit "skip prompts" switch once UI-1461 (Bubbletea forms)
	// makes bare invocation interactive.
	cmd.Flags().BoolP("yes", "y", false, "Accept all defaults, no prompts (currently the only mode)")
	return cmd
}

func runInit() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	configPath := project.ConfigPath(dir)
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("gaffer.toml already exists in %s", dir)
	}

	cfg := &config.Config{EngineVersion: 2}
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
