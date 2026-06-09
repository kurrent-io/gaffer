package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new gaffer project",
		Long: "Creates a starter gaffer.toml in the current directory. Define an " +
			"environment and add projections with `gaffer scaffold`.",
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitInit(cmd.Context(), telemetry.InitCommandInvokedProperties{Outcome: o})
			})
			return runInit()
		},
	}
	return cmd
}

func runInit() error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := config.InitProject(dir); err != nil {
		return err
	}
	fmt.Println("Initialized gaffer project")
	return nil
}
