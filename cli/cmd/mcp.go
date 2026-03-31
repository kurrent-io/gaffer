package cmd

import (
	"github.com/kurrent-io/gaffer/cli/internal/mcpserver"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start an MCP server for AI agent integration",
	RunE:  runMCP,
}

func runMCP(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	srv, err := mcpserver.NewFromProjectRoot()
	if err != nil {
		return err
	}

	return srv.Run(cmd.Context())
}
