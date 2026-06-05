package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/config"
	"github.com/kurrent-io/gaffer/cli/internal/prompt"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

func newInitCmd() *cobra.Command {
	var yes bool
	var engineVersion int

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new gaffer project",
		Long:  "Creates gaffer.toml in the current directory.",
		RunE: func(cmd *cobra.Command, args []string) (retErr error) {
			defer oneShotDefer(&retErr, func(o telemetry.Outcome) {
				telemetry.EmitInit(cmd.Context(), telemetry.InitCommandInvokedProperties{Outcome: o})
			})
			return runInit(cmd, yes, engineVersion)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip prompts and accept defaults")
	cmd.Flags().IntVar(&engineVersion, "engine-version", config.DefaultEngineVersion,
		"Projection engine version written to gaffer.toml (1 or 2)")
	return cmd
}

func runInit(cmd *cobra.Command, yes bool, engineVersion int) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	if prompt.Enabled(yes) {
		// Gaps model: prompt only for what wasn't passed. The flag has
		// a default, so Changed distinguishes "explicitly set" from
		// "left at default".
		if !cmd.Flags().Changed("engine-version") {
			engineVersion, err = promptEngineVersion(engineVersion)
			if err != nil {
				return err
			}
		}
		ok, err := prompt.Confirm(
			fmt.Sprintf("Initialize gaffer project with engine_version = %d?", engineVersion), true)
		if err != nil {
			return err
		}
		if !ok {
			return prompt.ErrCancelled
		}
	}

	if _, err := config.InitProject(dir, engineVersion); err != nil {
		return err
	}
	fmt.Println("Initialized gaffer project")
	return nil
}

// promptEngineVersion asks which engine version to write, pre-selecting
// current. Returns the chosen 1 or 2.
func promptEngineVersion(current int) (int, error) {
	choice, err := prompt.Select(
		"Projection engine version",
		[]prompt.Option{
			{Label: "2 (recommended)", Value: "2"},
			{Label: "1", Value: "1"},
		},
		fmt.Sprint(current),
	)
	if err != nil {
		return 0, err
	}
	if choice == "1" {
		return 1, nil
	}
	return 2, nil
}
