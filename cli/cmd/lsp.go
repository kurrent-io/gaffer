package cmd

import (
	"io"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/lsp"
)

func newLSPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lsp",
		Short: "Run the gaffer LSP server over stdio",
		Long: "Run the gaffer Language Server Protocol server, " +
			"speaking JSON-RPC over stdin/stdout. Editor extensions " +
			"spawn this subcommand and connect to it as an LSP client.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			server := lsp.NewServer(lsp.ServerOptions{
				Version: version,
			})
			return server.Run(ctx, stdioStream{})
		},
	}
}

// stdioStream adapts os.Stdin / os.Stdout into the io.ReadWriteCloser
// expected by the LSP server. Close is a no-op because the runtime
// owns the underlying file descriptors; the server detects EOF via
// the read side.
type stdioStream struct{}

func (stdioStream) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdioStream) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdioStream) Close() error                { return nil }

// Compile-time interface check.
var _ io.ReadWriteCloser = stdioStream{}
