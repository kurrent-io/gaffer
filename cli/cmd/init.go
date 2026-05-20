package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/project"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new gaffer project",
		Long:  "Creates gaffer.toml in the current directory.",
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

	fmt.Println("Initialized gaffer project")
	return nil
}
