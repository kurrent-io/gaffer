package cmd

import "github.com/spf13/cobra"

// newConfigCmd is the top-level group for managing gaffer's user-
// level configuration. Right now it hosts only `config telemetry`;
// future config-related subcommands (default editor preferences,
// project templates, etc.) belong here too.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage gaffer's user configuration",
		Long: "Read or change gaffer's user-level settings.\n" +
			"\n" +
			"Settings live at $XDG_CONFIG_HOME/gaffer/config.toml (on macOS,\n" +
			"~/Library/Application Support/gaffer/config.toml; on Windows,\n" +
			"%AppData%/gaffer/config.toml). The GAFFER_CONFIG_DIR environment\n" +
			"variable overrides the default location.",
	}
	cmd.AddCommand(newConfigTelemetryCmd())
	return cmd
}
