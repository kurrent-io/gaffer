package cmd

import (
	"github.com/kurrent-io/gaffer/cli/internal/mcpserver"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start an MCP server for AI agent integration",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv, err := mcpserver.NewFromProjectRoot()
			if err != nil {
				return err
			}

			return srv.Run(cmd.Context())
		},
	}
}
