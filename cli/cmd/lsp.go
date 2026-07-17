package cmd

import (
	"io"
	"os"
	"os/signal"

	"github.com/spf13/cobra"

	"github.com/kurrent-io/gaffer/cli/internal/lsp"
	"github.com/kurrent-io/gaffer/cli/internal/telemetry"
)

func newLSPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Run the gaffer LSP server over stdio",
		Long: "Run the gaffer Language Server Protocol server, " +
			"speaking JSON-RPC over stdin/stdout. Editor extensions " +
			"spawn this subcommand and connect to it as an LSP client.",
		Annotations: map[string]string{AnnotationOutput: OutputStructured},
		RunE: func(cmd *cobra.Command, _ []string) error {
			// `defer tx.End(ctx)` must be direct - see DevTx.End
			// for why a wrapping closure breaks recover().
			tx := telemetry.BeginLSP(cmd.Context())
			defer tx.End(cmd.Context())

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer stop()

			server := lsp.NewServer(lsp.ServerOptions{
				Version: Version,
			})
			runErr := server.Run(ctx, stdioStream{})

			// Drain after Run returns. Inline handlers finish before the
			// read loop exits; offloaded ones (see offloadBlocking) run
			// through spawn, so Run's wait-group drain awaits them too -
			// the atomic loads see final values. Single-goroutine Tx
			// contract holds: setters fire on the main goroutine.
			stats := server.Stats()
			tx.SetCodeLensRequestCount(stats.CodeLensRequestCount)
			tx.SetDiagnosticPublishCount(stats.DiagnosticPublishCount)
			tx.SetDiffRequestCount(stats.DiffRequestCount)

			tx.SetOutcome(classifyLSPOutcome(runErr))
			return runErr
		},
	}
	// vscode-languageclient unconditionally appends --stdio when the
	// transport is stdio (which is also its default). Accept it as
	// a no-op so the spawn doesn't fail with "Unknown flag". Hidden
	// so it doesn't appear in --help and falsely advertise a
	// transport switch (passing --stdio=false still uses stdio).
	cmd.Flags().Bool("stdio", true, "")
	if err := cmd.Flags().MarkHidden("stdio"); err != nil {
		panic(err)
	}
	return cmd
}

// stdioStream adapts os.Stdin / os.Stdout into the io.ReadWriteCloser
// expected by the LSP server. Close closes os.Stdin so a Read
// blocked on the read loop unblocks - the server's Run path
// drives disconnect by calling conn.Close() on `exit` and on
// ctx-cancel (SIGINT), and that flow then waits on
// DisconnectNotify(). Without Close unblocking the read side,
// DisconnectNotify never fires under real stdio (test pipes
// close their own end so the bug only surfaces in production).
type stdioStream struct{}

func (stdioStream) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdioStream) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdioStream) Close() error                { return os.Stdin.Close() }

// Compile-time interface check.
var _ io.ReadWriteCloser = stdioStream{}
